// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// moduleknowsnoedge.go — анализатор «модуль не знает своего края».
//
// # Предмет
//
// Соединение открывает ПОТРЕБИТЕЛЬ: край сам открывает поток к модулю и сам
// читает журналы курсором. Тогда ребро остаётся потребитель→владелец,
// ацикличность цела, а каждый модуль поднимается там, где края нет вовсе.
//
// Обратное ребро нарушало это дважды — самим ребром и отказом старта: адрес края
// был ОБЯЗАТЕЛЬНОЙ ручкой, поэтому модуль не поднимался без края. Второе и делает
// вынос модуля отдельным продуктом невыразимым.
//
// # Популяция — ВСЕ модули, и перечень ВЫВОДИТСЯ
//
// Прежняя редакция (`iamknowsnoedge.go`) судила ровно один модуль: приставка
// ручки была зашита как `KANAME_GATEWAY_INTERNAL`. Про остальные шесть она не
// утверждала ничего — то есть свойство держалось у одного модуля из семи.
//
// Перечень модулей выводится обходом каталога сервисов, а не выписывается:
// рукописный список расходится с деревом молча. Приставка ручки выводится из
// имени модуля (`KACHO_<МОДУЛЬ>_GATEWAY`), а не берётся константой.
//
// # Что судится — ТРИ половины, и любых двух мало
//
//  1. ТИП. Прод-файл модуля не импортирует ни контракт края
//     (`pkg/api/kacho/cloud/apigateway`), ни его код (`kacho/gateway/`). Импорт —
//     узел синтаксического дерева, поэтому упоминание имени пакета в комментарии
//     находкой не является by construction.
//  2. РУЧКА. Ни прод-код модуля, ни его чарт не объявляют ручки адреса края
//     (`KACHO_<МОДУЛЬ>_GATEWAY*`). Половина без второй ничего не держит: снятый
//     импорт при живой ручке оставляет отказ старта, а снятая ручка при живом
//     импорте оставляет ребро.
//  3. АДРЕС. Ни прод-код модуля, ни его чарт не называют край ЦЕЛЬЮ ДОЗВОНА
//     (`http://api-gateway…:8080`, `nc -z api-gateway… 8080`). Заведена задачей
//     продукта #1576; распознаватель и его законные близнецы —
//     `moduleknowsnoedgeaddress.go`.
//
// ЗДЕСЬ СТОЯЛО «ДВЕ половины», и вместе с ним — довод, что адрес края в обход
// конвенции ручек держит половина ТИПА: «набрать типизированный вызов края без
// его контракта нечем». Довод верен для ТИПИЗИРОВАННОГО gRPC и ложен для голого
// HTTP: `http.Get` контракта не требует. Инъекция показала молчание анализатора и
// на сыром адресе в коде, и на посадке, ЖДУЩЕЙ край, — то есть ровно на «модуль
// не поднимается там, где края нет вовсе», второй половине предмета из шапки.
//
// # ЗАКОННЫЕ СЛУЧАИ, названные поимённо
//
// Предпосылка «модуль про край не пишет» верна для ОДНОГО модуля и ЛОЖНА для
// семи: на расширенной популяции модуль называет край законно, и не однажды.
// Каждый такой случай назван здесь и закрыт законным близнецом в пробе — иначе
// первая же ложная находка отключила бы гейт целиком.
//
//   - НАПРАВЛЕНИЕ край→модуль. Селектор пода края в сетевой политике
//     (`networkPolicy.apiGatewayPodSelector` у nlb) разрешает краю ходить К
//     сервису. Это вход, а не выход, и он обязан остаться.
//   - КРУГ ЗАКОННЫХ ОТПРАВИТЕЛЕЙ. `trustedForwarderSANs` со SPIFFE-именем края
//     сужает, кто вправе говорить за пользователя. Правило безопасности требует
//     этот круг НЕПУСТЫМ и пиненным по фактическим отправителям — то есть модуль
//     обязан знать край по ИМЕНИ. Имя личности входящего пира не есть адрес
//     исходящего вызова.
//   - ЛИЧНОСТЬ ВЫЗЫВАЮЩЕГО в коде. Короткое имя службы края (`"api-gateway"`) в
//     политике вызывающего — та же полоса: решение «кого я впускаю».
//   - КЛЮЧИ САМОГО КРАЯ. `KACHO_API_GATEWAY_*` принадлежат краю (он держит адреса
//     модулей) и упоминаются в прозе модулей. Приставка модуля выводится как
//     `KACHO_<МОДУЛЬ>_GATEWAY`, поэтому пересечься с ними она не может: модуля с
//     именем `api` не существует. Предпосылка проверяется — при появлении такого
//     модуля анализатор ОТКАЗЫВАЕТ, а не молчит.
//   - РЕСУРС `Gateway` У VPC. Домен vpc владеет ресурсом Gateway, отчего
//     `vpc_gateway`, `vpc.gateway`, каталог `api/gateway` и его импорт живут в
//     прод-коде десятками. Общее у них с краем — только слово. Поэтому судится
//     приставка `KACHO_<МОДУЛЬ>_GATEWAY` и точный путь импорта, а НИКОГДА не
//     слово «gateway» в любом написании.
//   - ПРОЗА. Комментарий и страница документации, называющие край, законны и
//     обязаны остаться: правило, из которого вынули причину, снимет следующий.
//
// # ГРАНИЦЫ, названные вслух
//
// В Go имя ручки судится как СТРОКОВЫЙ ЛИТЕРАЛ (узел дерева) — проза о ручке
// законна. В шаблоне чарта дерева нет: до подстановки это не YAML, и разобрать
// его нечем. Поэтому там снимается комментарий (от `#` до конца строки) и
// судится остаток. Это приближение, и оно названо: `#` внутри цитированного
// значения был бы прочитан как начало комментария. Такой строки в чартах модулей
// нет, а цена точного разбора — свой парсер шаблонов.
//
// Вторая граница — ИМЯ ручки. Судится каноническая форма `KACHO_<МОДУЛЬ>_GATEWAY`;
// ручка, названная в обход конвенции (`..._EDGE_ADDR` и подобное), этой половиной
// не ловится. Её держат половина ТИПА, пока вызов типизирован, и половина АДРЕСА,
// пока в значении стоит цель дозвона; голый вызов по адресу, СОБРАННОМУ в рантайме
// из переменных, не держит ничто — остаток назван в `moduleknowsnoedgeaddress.go`.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// edgeContractImportMarker — по чему узнаётся контракт края в пути импорта.
const edgeContractImportMarker = "pkg/api/kacho/cloud/apigateway"

// edgeCodeImportMarker — по чему узнаётся КОД края (не контракт). Приставка
// полная и с завершающей косой чертой намеренно: каталог ресурса Gateway у vpc
// лежит по пути `.../kacho/api/gateway` и под неё не подпадает.
const edgeCodeImportMarker = "github.com/PRO-Robotech/kacho/gateway/"

// edgeOwnKnobPrefix — приставка ключей САМОГО края. Модуль, чья выведенная
// приставка совпала бы с ней, сделал бы анализатор ложным: край держит адреса
// модулей законно. Совпадение — отказ, а не молчание.
const edgeOwnKnobPrefix = "KACHO_API_GATEWAY"

// ModuleKnowsNoEdgeOptions — посадка анализатора.
type ModuleKnowsNoEdgeOptions struct {
	// Root — корень дерева.
	Root string
	// ServicesDir — каталог, ЧЬИ ПОДКАТАЛОГИ и есть перечень модулей. Перечень
	// выводится обходом, а не выписывается.
	ServicesDir string
	// ChartDirTemplates — шаблоны каталогов чарта относительно Root; `%s` —
	// имя модуля. Несуществующий каталог пропускается, но найденные считаются
	// переписью: опечатка в шаблоне обязана быть видна нулём, а не тишиной.
	ChartDirTemplates []string
}

// ModuleKnowsNoEdgeCensus — объём осмотренного по каждой оси. Печатается всегда:
// «ноль находок» обязано быть отличимо от «ноль прочитанного».
type ModuleKnowsNoEdgeCensus struct {
	Modules    []string
	ChartDirs  int
	GoFiles    int
	GoImports  int
	GoLiterals int
	ChartFiles int
	ChartLines int
	// EdgeNamedGo и EdgeNamedChart — сколько литералов Go и строк чарта НАЗВАЛИ
	// край и потому были осмотрены половиной АДРЕСА. Обе печатаются: расширение
	// распознавателя обязано менять ОСМОТРЕННОЕ, иначе оно холостое, а падение
	// этих величин до нуля означает, что половина перестала видеть свой предмет.
	EdgeNamedGo    int
	EdgeNamedChart int
}

// ModuleKnowsNoEdgeFinding — одно нарушение с координатой и именем модуля.
type ModuleKnowsNoEdgeFinding struct {
	Module string
	Path   string
	Line   int
	What   string
	// Lane — ПОЛОСА, которой находка принадлежит (см. константы EdgeLane*).
	// Нужна инъекции: «роняет только проверяемое» доказуемо лишь атрибуцией, а
	// разбор полосы по тексту находки был бы вторым предикатом о том же.
	Lane string
}

func (f ModuleKnowsNoEdgeFinding) String() string {
	return fmt.Sprintf("%s: %s:%d — %s", f.Module, f.Path, f.Line, f.What)
}

// EdgeAddressKnobPrefixFor выводит приставку ручки адреса края из имени модуля.
// Экспортируется, чтобы проба называла ту же величину, что и анализатор: вторая
// копия предиката разошлась бы с первой молча.
func EdgeAddressKnobPrefixFor(module string) string {
	up := strings.ToUpper(strings.ReplaceAll(module, "-", "_"))
	return "KACHO_" + up + "_GATEWAY"
}

// AuditModuleKnowsNoEdge обходит дерево и возвращает находки с переписью.
func AuditModuleKnowsNoEdge(opts ModuleKnowsNoEdgeOptions, log io.Writer) ([]ModuleKnowsNoEdgeFinding, ModuleKnowsNoEdgeCensus, error) {
	var (
		findings []ModuleKnowsNoEdgeFinding
		census   ModuleKnowsNoEdgeCensus
	)

	modules, err := deriveModules(filepath.Join(opts.Root, opts.ServicesDir))
	if err != nil {
		return nil, census, err
	}
	census.Modules = modules

	for _, module := range modules {
		knob := EdgeAddressKnobPrefixFor(module)

		// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Приставка модуля не вправе накрыть
		// ключи самого края: край держит адреса модулей законно, и тогда каждая
		// проза о нём стала бы находкой. Отказ громче молчания.
		if strings.HasPrefix(knob, edgeOwnKnobPrefix) {
			return nil, census, fmt.Errorf(
				"модуль %q выводит приставку ручки %q, накрывающую ключи самого края %q: "+
					"предпосылка анализатора нарушена, судить эту приставку нельзя",
				module, knob, edgeOwnKnobPrefix)
		}

		modFindings, merr := auditModuleGo(opts, module, knob, &census)
		if merr != nil {
			return nil, census, merr
		}
		findings = append(findings, modFindings...)

		chartFindings, cerr := auditModuleCharts(opts, module, knob, &census)
		if cerr != nil {
			return nil, census, cerr
		}
		findings = append(findings, chartFindings...)
	}

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: модулей осмотрено %d (%s) · файлов Go прочитано %d · импортов разобрано %d · "+
				"строковых литералов разобрано %d · каталогов чарта найдено %d · файлов чарта прочитано %d · "+
				"строк чарта осмотрено %d · из них НАЗВАЛИ край: литералов Go %d, строк чарта %d · находок %d\n",
			len(census.Modules), strings.Join(census.Modules, ", "),
			census.GoFiles, census.GoImports, census.GoLiterals,
			census.ChartDirs, census.ChartFiles, census.ChartLines,
			census.EdgeNamedGo, census.EdgeNamedChart, len(findings))
	}
	return findings, census, nil
}

// deriveModules выводит перечень модулей из дерева. Пустой перечень — ошибка:
// «ноль модулей» неотличимо от «ноль прочитанного», а вердикт на пустом обходе
// беспредметен.
func deriveModules(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("перечень модулей из %s: %w", dir, err)
	}
	var modules []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			modules = append(modules, e.Name())
		}
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("в %s ноль модулей — обход беспредметен", dir)
	}
	sort.Strings(modules)
	return modules, nil
}

// auditModuleGo судит прод-код одного модуля: импорты (тип) и строковые литералы
// (адрес).
func auditModuleGo(opts ModuleKnowsNoEdgeOptions, module, knob string, census *ModuleKnowsNoEdgeCensus) ([]ModuleKnowsNoEdgeFinding, error) {
	// Обход СОБИРАЕТ ПУТИ, а чтение идёт ПОСЛЕ него — не ради вкуса.
	//
	// Чтение внутри обхода берёт путь, который обход только что увидел, и между
	// «увидел» и «прочитал» дерево может смениться: подменённая ссылка уводит
	// чтение за пределы осматриваемого поддерева, и гейт выносит вердикт о чужом
	// файле. Собрав пути и прочитав их отдельно, мы этого класса лишаемся by
	// construction, а не оговоркой.
	goDir := filepath.Join(opts.Root, opts.ServicesDir, module)
	var goFiles []string
	err := filepath.WalkDir(goDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		goFiles = append(goFiles, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	var findings []ModuleKnowsNoEdgeFinding
	for _, path := range goFiles {
		census.GoFiles++
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return nil, fmt.Errorf("разбор %s: %w", path, perr)
		}
		rel, _ := filepath.Rel(opts.Root, path)

		for _, imp := range file.Imports {
			census.GoImports++
			ip, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			switch {
			case strings.Contains(ip, edgeContractImportMarker):
				findings = append(findings, ModuleKnowsNoEdgeFinding{
					Module: module, Path: rel, Line: fset.Position(imp.Pos()).Line,
					What: "импорт контракта края " + ip + " — модуль типизирован своим потребителем",
					Lane: EdgeLaneType,
				})
			case strings.Contains(ip, edgeCodeImportMarker):
				findings = append(findings, ModuleKnowsNoEdgeFinding{
					Module: module, Path: rel, Line: fset.Position(imp.Pos()).Line,
					What: "импорт кода края " + ip + " — модуль собирается только вместе с краем",
					Lane: EdgeLaneType,
				})
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			census.GoLiterals++
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if strings.Contains(v, knob) {
				findings = append(findings, ModuleKnowsNoEdgeFinding{
					Module: module, Path: rel, Line: fset.Position(lit.Pos()).Line,
					What: "ручка адреса края " + v + " — модуль не поднимется там, где края нет",
					Lane: EdgeLaneKnob,
				})
			}
			// Половина АДРЕСА: литерал, назвавший край, осматривается на ЦЕЛЬ
			// ДОЗВОНА. Имя без схемы и порта целью не является и молчит.
			if strings.Contains(v, edgeHostToken) {
				census.EdgeNamedGo++
				for _, target := range edgeDialTargets(v, false) {
					findings = append(findings, ModuleKnowsNoEdgeFinding{
						Module: module, Path: rel, Line: fset.Position(lit.Pos()).Line,
						What: "цель дозвона до края " + target + " — исходящее ребро модуль→край, " +
							"то есть цикл: край уже зовёт модуль",
						Lane: EdgeLaneAddress,
					})
				}
			}
			return true
		})
	}
	return findings, nil
}

// auditModuleCharts судит посадку одного модуля: объявленную ручку адреса края.
func auditModuleCharts(opts ModuleKnowsNoEdgeOptions, module, knob string, census *ModuleKnowsNoEdgeCensus) ([]ModuleKnowsNoEdgeFinding, error) {
	var chartFiles []string
	for _, tpl := range opts.ChartDirTemplates {
		dir := filepath.Join(opts.Root, fmt.Sprintf(tpl, module))
		if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
			continue
		}
		census.ChartDirs++
		werr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			switch filepath.Ext(path) {
			case ".yaml", ".yml", ".tpl":
				chartFiles = append(chartFiles, path)
			}
			return nil
		})
		if werr != nil {
			return nil, werr
		}
	}

	var findings []ModuleKnowsNoEdgeFinding
	for _, path := range chartFiles {
		census.ChartFiles++
		raw, rerr := os.ReadFile(path) // #nosec G304 -- путь собран обходом объявленного поддерева
		if rerr != nil {
			return nil, rerr
		}
		rel, _ := filepath.Rel(opts.Root, path)
		for i, line := range strings.Split(string(raw), "\n") {
			census.ChartLines++
			if idx := strings.Index(line, "#"); idx >= 0 {
				line = line[:idx]
			}
			if strings.Contains(line, knob) {
				findings = append(findings, ModuleKnowsNoEdgeFinding{
					Module: module, Path: rel, Line: i + 1,
					What: "чарт объявляет ручку адреса края — посадка обязывает модуль знать потребителя",
					Lane: EdgeLaneKnob,
				})
			}
			// Половина АДРЕСА. Порт через пробел разрешён ТОЛЬКО здесь: в чарте
			// это форма оболочечной пробы (`nc -z <хост> <порт>`), которой посадка
			// ЖДЁТ край, а в литерале Go число после имени было бы прозой.
			if strings.Contains(line, edgeHostToken) {
				census.EdgeNamedChart++
				for _, target := range edgeDialTargets(line, true) {
					findings = append(findings, ModuleKnowsNoEdgeFinding{
						Module: module, Path: rel, Line: i + 1,
						What: "посадка называет цель дозвона до края " + target +
							" — модуль не поднимется там, где края нет вовсе",
						Lane: EdgeLaneAddress,
					})
				}
			}
		}
	}
	return findings, nil
}
