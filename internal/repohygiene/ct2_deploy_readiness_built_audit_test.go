// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ct2_deploy_readiness_built_audit_test.go — судящие функции двух гейтов
// готовности. Лежат в _test.go намеренно: снятие комментариев YAML описано в
// дереве ОДИН раз (`commentLineRe`, соседний гейт полосы чартов), и оно
// объявлено в тестовом файле. Своя копия того же выражения здесь была бы вторым
// местом об одном предикате — тем самым классом, который корпус и ловит.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// readinessRoutes — маршруты диагностической поверхности, о которых судит гейт.
// Перечень закрыт намеренно: это ИМЕНА ДВУХ ВОПРОСОВ («жив ли процесс» и «может
// ли он обслуживать») плюс признак того, что поверхность вообще поднята.
const (
	routeLive    = "/healthz"
	routeReady   = "/readyz"
	routeMetrics = "/metrics"
)

// readinessFinding — один сервис, у которого готовность не построена.
type readinessFinding struct {
	Service string
	Why     string
}

// readinessCensus — объём осмотренного, ПО ОСЯМ. Одно суммарное число скрыло бы
// ровно тот случай, ради которого гейт заведён: «сервисов восемь, готовность
// строят пять» и «сервисов пять, готовность строят пять» дают одинаковый ноль
// находок только по второй половине.
type readinessCensus struct {
	Services      int // сервисов найдено обходом
	WithMetrics   int // из них поднимают диагностическую поверхность
	ServingReady  int // из них регистрируют /readyz
	WithNamedDeps int // из них строят хотя бы одну ИМЕНОВАННУЮ проверку зависимости
	FilesRead     int // не-тестовых файлов Go, разобранных обходом
}

// routeSite — одна регистрация маршрута: где и каким выражением задан обработчик.
type routeSite struct {
	File    string
	Handler string
}

// serviceSurface — то, что обход узнал об одном сервисе.
type serviceSurface struct {
	Routes    map[string][]routeSite // маршрут → его регистрации
	NamedDeps int                    // литералов вида `{Name: …, Check: …}`
	DepsFile  string                 // где встретился первый такой литерал
}

// auditReadinessBuilt — судящая функция гейта «готовность СТРОИТСЯ, а не
// подменяется живостью».
//
// # Что судится
//
// Разобранное дерево, а не текст файла: про готовность в этих корнях написано
// много прозы, и поиск по подстроке засчитал бы объяснение за реализацию
// (`testing.md` §«Гейт на класс», п.4).
//
// Три оси, и каждая ловит СВОЮ форму «формы без содержания»:
//
//   - ОСЬ А — готовности нет вовсе: поверхность поднята (`/metrics`
//     зарегистрирован), а `/readyz` не регистрируется ничем. Тогда чарту нечего
//     пробировать, и в слот готовности неизбежно едет живость;
//   - ОСЬ Б — живость В СЛОТЕ готовности: `/readyz` зарегистрирован тем же
//     выражением обработчика, что и `/healthz`. Два имени одного ответа: под
//     объявляет себя готовым ровно тогда, когда процесс жив;
//   - ОСЬ В — готовность без предмета: `/readyz` есть, а ни одной ИМЕНОВАННОЙ
//     проверки зависимости сервис не строит. Обработчик, отвечающий безусловным
//     200, отличается от живости только адресом.
//
// # Как узнаётся именованная проверка
//
// По СТРУКТУРЕ литерала (поля `Name` и `Check`), а не по имени типа. Имя типа
// сегодня общее (`health.Checker`), но привязка к нему сделала бы гейт слепым к
// сервису, у которого носитель свой: iam строит те же две величины типом своего
// транспортного слоя, и гейт, судящий по имени пакета, объявил бы его
// нарушителем, ничего не нарушившего.
//
// # Чего гейт НЕ судит, названо прямо
//
//   - КРАЙ (`gateway/`) — вне популяции. Его готовность строится не из
//     именованных зависимостей, а из перечня соседей, к которым он проксирует:
//     механизм другой, и требовать от него формы сервисов значило бы судить не
//     тот предмет. Граница проведена по КАТЕГОРИИ (край против сервиса), а не
//     перечислением имён;
//   - ЧТО ИМЕННО проверяет каждая зависимость. «Пингует ли этот чекер ту базу,
//     которую сервис читает» — вопрос обзора, машинного предиката у него нет;
//   - ЧЕМ ПРОБИРУЕТ ЧАРТ. Это вторая полоса того же механизма, и её сверяет
//     `TestReadinessProbeAsksWhatTheServiceActuallyServes`. Здесь — только то,
//     что сервису есть что предъявить.
func auditReadinessBuilt(root string, relFiles []string) ([]readinessFinding, readinessCensus, error) {
	var cen readinessCensus
	surfaces := map[string]*serviceSurface{}
	fset := token.NewFileSet()

	for _, rel := range relFiles {
		slashed := filepath.ToSlash(rel)
		parts := strings.Split(slashed, "/")
		if len(parts) < 3 || parts[0] != "services" {
			continue
		}
		svc := parts[1]
		if _, seen := surfaces[svc]; !seen {
			surfaces[svc] = &serviceSurface{Routes: map[string][]routeSite{}}
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		file, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, perr)
		}
		cen.FilesRead++
		collectSurface(fset, file, slashed, surfaces[svc])
	}

	names := make([]string, 0, len(surfaces))
	for svc := range surfaces {
		names = append(names, svc)
	}
	sort.Strings(names)
	cen.Services = len(names)

	var findings []readinessFinding
	for _, svc := range names {
		s := surfaces[svc]
		hasMetrics := len(s.Routes[routeMetrics]) > 0
		ready := s.Routes[routeReady]
		live := s.Routes[routeLive]
		if hasMetrics {
			cen.WithMetrics++
		}
		if len(ready) > 0 {
			cen.ServingReady++
		}
		if s.NamedDeps > 0 {
			cen.WithNamedDeps++
		}

		// ОСЬ А.
		if hasMetrics && len(ready) == 0 {
			findings = append(findings, readinessFinding{Service: svc, Why: fmt.Sprintf(
				"диагностическая поверхность поднята (%s зарегистрирован), а %s не регистрируется "+
					"ничем: готовности у сервиса НЕ СУЩЕСТВУЕТ. Чарту нечего пробировать, поэтому в "+
					"слот готовности едет живость — под объявляет себя готовым до того, как способен "+
					"ответить, и трафик приходит в отказ. Образец — services/nlb/cmd/kacho-loadbalancer/observability.go",
				routeMetrics, routeReady)})
			continue
		}
		if len(ready) == 0 {
			continue
		}

		// ОСЬ Б.
		liveHandlers := map[string]string{}
		for _, site := range live {
			liveHandlers[site.Handler] = site.File
		}
		var distinct bool
		for _, site := range ready {
			if _, same := liveHandlers[site.Handler]; !same {
				distinct = true
			}
		}
		if len(live) > 0 && !distinct {
			findings = append(findings, readinessFinding{Service: svc, Why: fmt.Sprintf(
				"%s и %s зарегистрированы ОДНИМ И ТЕМ ЖЕ выражением обработчика (%s, %s): это два "+
					"имени одного ответа. Готовность обязана знать про зависимости, живость — только "+
					"про процесс; иначе под готов ровно тогда, когда жив",
				routeReady, routeLive, ready[0].Handler, ready[0].File)})
			continue
		}

		// ОСЬ В.
		if s.NamedDeps == 0 {
			findings = append(findings, readinessFinding{Service: svc, Why: fmt.Sprintf(
				"%s зарегистрирован (%s), но ни одной ИМЕНОВАННОЙ проверки зависимости сервис не "+
					"строит: литерала с полями Name и Check в дереве сервиса нет. Обработчик без "+
					"предмета отличается от живости только адресом — форма есть, содержания нет",
				routeReady, ready[0].File)})
		}
	}
	return findings, cen, nil
}

// collectSurface набирает по одному файлу: регистрации маршрутов диагностической
// поверхности и литералы именованных проверок зависимостей.
func collectSurface(fset *token.FileSet, file *ast.File, rel string, out *serviceSurface) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if len(node.Args) < 2 {
				return true
			}
			lit, ok := node.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			raw, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			route := routeOf(raw)
			if route == "" {
				return true
			}
			out.Routes[route] = append(out.Routes[route], routeSite{
				File:    rel,
				Handler: ct2RenderExpr(fset, node.Args[len(node.Args)-1]),
			})
		case *ast.CompositeLit:
			if !hasNamedCheckFields(node) {
				return true
			}
			out.NamedDeps++
			if out.DepsFile == "" {
				out.DepsFile = rel
			}
		}
		return true
	})
}

// routeOf снимает с образца маршрута приставку метода (`GET /readyz`) и
// отвечает каноническим маршрутом либо пустой строкой.
//
// Приставка снимается ЗДЕСЬ, в одном месте: обе формы записи законны и обе живут
// в дереве (`mux.Handle("GET /readyz", …)` и `mux.HandleFunc("/readyz", …)`).
// Распознаватель, знающий одну, объявил бы половину дерева не имеющей готовности
// — не нарушением, а невидимкой (`testing.md` §«Гейт на класс», п.7).
func routeOf(raw string) string {
	pattern := strings.TrimSpace(raw)
	if i := strings.LastIndex(pattern, " "); i >= 0 {
		pattern = strings.TrimSpace(pattern[i+1:])
	}
	switch pattern {
	case routeLive, routeReady, routeMetrics:
		return pattern
	}
	return ""
}

// hasNamedCheckFields — литерал объявляет ИМЕНОВАННУЮ проверку зависимости:
// несёт поля `Name` и `Check` сразу. Судится структура, а не имя типа.
func hasNamedCheckFields(lit *ast.CompositeLit) bool {
	var name, check bool
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Name":
			name = true
		case "Check":
			check = true
		}
	}
	return name && check
}

// renderExpr печатает выражение обработчика так, как оно записано в исходнике.
func ct2RenderExpr(fset *token.FileSet, expr ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, expr); err != nil {
		return "<не печатается>"
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// probeSlotFinding — один шаблон развёртывания, чей слот живости спрашивает
// вопрос готовности.
type probeSlotFinding struct {
	Template string
	Why      string
}

// probeSlotCensus — объём осмотренного по осям.
type probeSlotCensus struct {
	Templates      int // шаблонов развёртывания прочитано
	WithReadySlot  int // из них спрашивают готовность по /readyz
	WithLiveSlot   int // из них несут слот живости вообще
	LiveAsksReady  int // из них живость спрашивает вопрос готовности
	CommentsPruned int // строк-комментариев снято до разбора
}

// auditLivenessSlotStaysLiveness — судящая функция гейта «в слоте ЖИВОСТИ не
// стоит вопрос ГОТОВНОСТИ».
//
// # Зеркало предмета #1655, и оно ломает иначе
//
// Живость в слоте готовности даёт под, рапортующий Ready, когда обслуживать не
// может. ОБРАТНАЯ подмена — готовность в слоте живости — даёт другое и худшее:
// блип зависимости (перекат соседа, короткая недоступность базы) читается
// kubelet'ом как смерть процесса, и под ПЕРЕЗАПУСКАЕТСЯ. Под нагрузкой это шторм
// перезапусков: каждая реплика уходит в рестарт по внешней причине, которая сама
// бы прошла, и восстановление откладывается ровно тем, что должно было помочь.
//
// # Почему отдельный гейт, а не строка в соседнем
//
// Соседний гейт (`TestReadinessProbeAsksWhatTheServiceActuallyServes`) сверяет
// ПОЛОСУ ГОТОВНОСТИ: что чарт спрашивает то, что сервис отдаёт. О слоте живости
// он не утверждает ничего и не должен: это другое измерение того же механизма, и
// свести их в одно утверждение значило бы получить проверку, которая краснеет,
// не называя, какая из двух полос разошлась.
//
// # Что судится и что нет
//
// Судится ОБЪЯВЛЕНИЕ чарта, а не отрендеренный манифест: рендер требует helm,
// которого в харнессе нет, и проба, зависящая от него, просто не исполнялась бы.
// Комментарии снимаются ДО разбора — над пробами стоят абзацы, объясняющие
// разведение живости и готовности, и они называют `/readyz` словами.
//
// НЕ судится, ЧТО спрашивает живость: `/healthz`, открытый сокет и команда в
// контейнере — все три законные ответы на вопрос «жив ли процесс». Запрещён
// ровно один ответ — вопрос готовности.
func auditLivenessSlotStaysLiveness(root string, templates []string) ([]probeSlotFinding, probeSlotCensus, error) {
	var cen probeSlotCensus
	var findings []probeSlotFinding

	for _, rel := range templates {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		body := string(raw)
		cen.CommentsPruned += len(commentLineRe.FindAllString(body, -1))
		body = commentLineRe.ReplaceAllString(body, "")
		cen.Templates++

		ready := probePathsInSlot(body, "readinessProbe")
		live := probePathsInSlot(body, "livenessProbe")
		if slotAsks(ready, routeReady) {
			cen.WithReadySlot++
		}
		if len(live) > 0 {
			cen.WithLiveSlot++
		}
		if !slotAsks(live, routeReady) {
			continue
		}
		cen.LiveAsksReady++
		findings = append(findings, probeSlotFinding{Template: rel, Why: fmt.Sprintf(
			"слот ЖИВОСТИ спрашивает %s — вопрос ГОТОВНОСТИ. Живость обязана зависеть только от "+
				"процесса: иначе блип зависимости (перекат соседа, короткая недоступность базы) "+
				"читается как смерть процесса, и под ПЕРЕЗАПУСКАЕТСЯ вместо того чтобы выйти из "+
				"ротации. Под нагрузкой это шторм перезапусков по причине, которая прошла бы сама. "+
				"Живости полагается %s, открытый сокет или команда в контейнере", routeReady, routeLive)})
	}
	return findings, cen, nil
}

// probePathsInSlot возвращает пути, которые названы внутри блока с указанным
// именем. Пустая строка в списке — проба без httpGet (открытый сокет либо
// команда): законный ответ о живости и не ответ о готовности.
//
// Разбор идёт ПО ОТСТУПУ: блок пробы — последующие строки с бо́льшим отступом.
// Первая строка с отступом не больше закрывает блок, поэтому путь соседнего
// слота в чужой ответ не попадает.
func probePathsInSlot(body, slot string) []string {
	re := regexp.MustCompile(`(?m)^([ \t]*)` + regexp.QuoteMeta(slot) + `:[ \t]*$`)
	lines := strings.Split(body, "\n")
	var out []string
	for i, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		path := ""
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			if len(cur)-len(strings.TrimLeft(cur, " \t")) <= indent {
				break
			}
			if p := strings.TrimSpace(cur); strings.HasPrefix(p, "path:") {
				path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(p, "path:")), `"'`)
			}
		}
		out = append(out, path)
	}
	return out
}

// slotAsks — спрашивает ли хоть одна проба слота названный путь. Путь
// сравнивается по началу: `/readyz?verbose` — тот же вопрос.
func slotAsks(paths []string, route string) bool {
	for _, p := range paths {
		if strings.HasPrefix(p, route) {
			return true
		}
	}
	return false
}
