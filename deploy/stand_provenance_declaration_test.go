// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

// stand_provenance_declaration_test.go — ревизию дерева несёт КАЖДЫЙ образ, и её
// проставляет КАЖДАЯ сборка.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЭТО ЛОВИТ И ПОЧЕМУ ЭТОГО НЕ ВИДНО БЕЗ ГЕЙТА
//
// «Спроси стенд, что он исполняет» — первое действие при разборе красного. Ответ
// даёт величина, зашитая в образ СБОРКОЙ; вывести её из ИМЕНИ образа нельзя —
// имя говорит, что собрали, а не из чего, и совпадают они лишь пока никто не
// пересобирал под прежним тегом.
//
// Дыра тихая с обеих сторон:
//
//   * образ без величины поднимается, работает и проходит все проверки. Узнать о
//     нём можно только тогда, когда ответ уже понадобился, — то есть посреди
//     разбора красного, когда цена времени максимальна;
//   * сборка, забывшая передать величину, тоже зелена: `ARG` с пустым умолчанием
//     не роняет `docker build`. Замер 2026-08-25: сборка сервисов в конвейере не
//     передавала ничего, и образы управляемого кластера ревизии не несли вовсе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ДВА НОСИТЕЛЯ И ПОЧЕМУ ОНИ НЕ РАЗЪЕДУТСЯ
//
// Клеймо читается реестром и демоном сборки; файл — единственное, что читается у
// РАБОТАЮЩЕГО контейнера одним `kubectl exec`, без демона хоста и без реестра, то
// есть одинаково на kind и на управляемом кластере. Оба берут величину из ОДНОГО
// `ARG` в ОДНОМ Dockerfile — разъехаться нечему by construction, и гейт требует
// именно этого, а не «оба присутствуют».
//
// Путь величины объявлен ОДИН раз — в читателе (deploy/scripts/stand-provenance.sh).
// Гейт вычитывает его оттуда и сверяет с тем, что пишут Dockerfile'ы: выписанная
// здесь копия разошлась бы с читателем молча, и первым это заметил бы тот, кому
// провенанс понадобился.
//
// ─────────────────────────────────────────────────────────────────────────────
// СПИСКА ИСКЛЮЧЕНИЙ У ГЕЙТА НЕТ — И ЭТО РЕШЕНИЕ
//
// Первая редакция выводила из-под правила вызовы, чей `-f` называет несуществующий
// путь: «он и так ничего не соберёт». Довод сам себя опроверг на замере — среди
// девяти рецептов, собирающих `kacho-<svc>:dev`, четыре называли пути прежней
// топологии, а ещё четыре собирали из негодного контекста, и разделить их
// «резолвится путь» не получалось: признак пропускал вторую половину и оправдывал
// первую. Всякий список прощённых — место, куда следующая сборка без величины
// попадёт незамеченной.
//
// Поэтому правило одно и без изъятий: вызов, называющий Dockerfile, передаёт
// величину. Что при этом четыре рецепта названы путями, которых в дереве нет, —
// ОТДЕЛЬНЫЙ предмет, и он не закрыт здесь: величина им передана, путь остался
// чужой заботой.

// ─────────────────────────────────────────────────────────────────────────────
// ИМЯ ДОВОДА — КОНТРАКТ С ФАЙЛОМ СБОРКИ, И ГЕЙТ СВЕРЯЕТ ЕГО, А НЕ ТОЛЬКО НАЛИЧИЕ
//
// Пока имя было ОДНО и выписано здесь константой, половинчатое переименование
// краснело само собой: непереименованная половина не находила своего имени. Как
// только имён стало два (переход, см. ниже), эта случайная защита исчезает —
// вызов «передаёт что-то из набора», файл «объявляет что-то из набора», и
// расхождение между ними проходит молча. Docker непотреблённый довод выбрасывает
// БЕЗ слова, `ARG` остаётся при пустом умолчании, образ уезжает без ревизии, а
// сборка зелена: класс «значение, которое пишут и не читают».
//
// Поэтому гейт сверяет СООТВЕТСТВИЕ: у вызова, чью цель `-f` можно разрешить в
// дереве, имя переданного довода обязано совпасть с именем, которое объявляет
// ЭТА цель. Где цель собрана из переменной оболочки (`-f $$d/Dockerfile`) или
// матрицы конвейера, соответствие не проверяемо by construction — такие вызовы
// НЕ прощаются молча, а считаются и называются числом: «ноль находок» обязано
// быть отличимо от «ноль прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ИМЁН ДВА, И ВТОРОЕ ИСТЕКАЕТ САМО
//
// Каноническое имя не называет продукт: величиной помечает свой образ каждый
// продукт дерева, а называющее одного из них имя переименовывается при каждом
// следующем выносе. Прежнее имя принимается, ПОКА ХОТЬ ОДИН Dockerfile дерева его
// объявляет, — сегодня это файл службы доступа, которая переименовывается в своём
// репозитории. Как только объявивших ноль, приём прежнего имени становится
// НАХОДКОЙ: послабление, которому нечего прощать, — это место, куда следующая
// сборка без величины попадёт незамеченной.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// canonicalProvenanceArg — имя довода, под которым величина ездит в образ.
// Названо предметом, а предмет — внешним стандартом: довод кормит аннотацию
// `org.opencontainers.image.revision`, и потому имя ВЫВОДИМО из ключа клейма,
// а не выбрано вкусом. Объявление величины — provenance.mk.
const canonicalProvenanceArg = "OCI_IMAGE_REVISION"

// legacyProvenanceArg — прежнее имя. Принимается, пока его объявляет хоть один
// Dockerfile дерева; ноль объявивших делает приём находкой (см. шапку).
const legacyProvenanceArg = "KACHO_IMAGE_REVISION"

// knownProvenanceArgs — порядок значим: первое совпадение считается объявленным.
var knownProvenanceArgs = []string{canonicalProvenanceArg, legacyProvenanceArg}

func argDeclRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^ARG\s+` + name + `\b`)
}

// revisionPathFromReader — путь величины, ПРОЧИТАННЫЙ у читателя. Единственное
// объявление в дереве; здесь его копии нет намеренно.
func revisionPathFromReader(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("scripts/stand-provenance.sh")
	if err != nil {
		t.Fatalf("читатель провенанса не прочитан: %v", err)
	}
	m := regexp.MustCompile(`(?m)^REVISION_PATH="([^"]+)"`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("в scripts/stand-provenance.sh не найдено объявление REVISION_PATH — " +
			"сверять Dockerfile'ы не с чем, и «сходится» здесь означало бы «не прочитано»")
	}
	return string(m[1])
}

// productDockerfiles — перечень образов продукта, ВЫВЕДЕННЫЙ из дерева.
// Выписанный отстал бы на первом же новом сервисе или модуле консоли.
func productDockerfiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, pattern := range []string{
		"../gateway/Dockerfile",
		"../services/*/Dockerfile",
		"../ui-future/*/Dockerfile",
	} {
		hits, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("обход %s: %v", pattern, err)
		}
		out = append(out, hits...)
	}
	if len(out) == 0 {
		t.Fatal("образов продукта не найдено — гейт, не прочитавший предмет, обязан падать, а не зеленеть")
	}
	sort.Strings(out)
	return out
}

// checkDockerfile — находки по одному Dockerfile и ИМЯ довода, которое он
// объявляет. Чистая функция над текстом: проба инъекции кормит её синтетикой, не
// трогая дерево.
//
// Имя определяется ПЕРВЫМ, и остальное сверяется с НИМ: файл, объявивший одно имя
// и выводящий клеймо из другого, прошёл бы проверку, принимающую любое известное
// имя в каждой строке по отдельности.
func checkDockerfile(body, revisionPath string) (findings []string, arg string) {
	var declared []string
	for _, name := range knownProvenanceArgs {
		if argDeclRe(name).MatchString(body) {
			declared = append(declared, name)
		}
	}
	switch len(declared) {
	case 0:
		findings = append(findings, "не объявлен ARG "+canonicalProvenanceArg)
		return findings, ""
	case 1:
		arg = declared[0]
	default:
		// Два имени одной величины в одном файле — два места об одном предмете.
		// Разойдутся они молча: клеймо возьмёт одно, файл ревизии другое.
		findings = append(findings, "объявлены ДВА имени одной величины ("+
			strings.Join(declared, ", ")+") — у довода одно имя, иначе клеймо и файл разъедутся")
		arg = declared[0]
	}

	labelRe := regexp.MustCompile(`org\.opencontainers\.image\.revision="\$\{?` + arg + `\}?"`)
	if !labelRe.MatchString(body) {
		findings = append(findings,
			"клеймо org.opencontainers.image.revision не выводится из $"+arg)
	}

	// Файл пишется ИЗ ТОГО ЖЕ аргумента и ПО ТОМУ ЖЕ пути, что читает читатель.
	writeRe := regexp.MustCompile(`\$\{?` + arg + `\}?"?\s*>\s*` + regexp.QuoteMeta(revisionPath))
	if !writeRe.MatchString(body) {
		findings = append(findings,
			"величина не записывается в "+revisionPath+" из $"+arg)
	}

	// Последний USER образа обязан остаться непривилегированным: семейство
	// консоли поднимается до root ради записи величины, и незакрытый подъём
	// оставил бы контейнер работать под root — цена провенанса стала бы выше
	// его пользы.
	users := regexp.MustCompile(`(?m)^USER\s+(\S+)`).FindAllStringSubmatch(body, -1)
	if len(users) == 0 {
		findings = append(findings, "образ не объявляет USER — финальный пользователь неизвестен")
	} else {
		last := users[len(users)-1][1]
		if last == "root" || last == "0" {
			findings = append(findings, "последний USER — "+last+": подъём до root не закрыт")
		}
	}
	return findings, arg
}

// legacyAcceptanceFinding — САМОИСТЕЧЕНИЕ приёма прежнего имени. Приём существует
// ради образов, которые прежнее имя ещё объявляют; ноль таких означает, что
// прощать больше нечего, — и тогда приём есть слепая зона, а не совместимость.
//
// Вынесено чистой функцией, чтобы проба инъекции подавала ей перепись, а не
// подделывала дерево: доказательство, требующее сборки настоящего дерева без
// службы доступа, не воспроизводимо.
func legacyAcceptanceFinding(byName map[string]int) string {
	if byName[legacyProvenanceArg] != 0 {
		return ""
	}
	return "приём прежнего имени ARG " + legacyProvenanceArg + " потерял предмет — ни один " +
		"Dockerfile дерева его не объявляет. Снимите его здесь и переходную пару доводов в " +
		"provenance.mk, .github/workflows/docker-build.yml и " +
		".github/scripts/kaname-chart-boots.sh: послабление, которому нечего прощать, — " +
		"место, куда следующая сборка без величины попадёт незамеченной"
}

func TestEveryProductImageCarriesTheTreeRevision(t *testing.T) {
	revisionPath := revisionPathFromReader(t)
	files := productDockerfiles(t)

	byName := map[string]int{}
	total := 0
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		total++
		findings, arg := checkDockerfile(string(body), revisionPath)
		for _, finding := range findings {
			t.Errorf("НАХОДКА: %s — %s", f, finding)
		}
		if arg != "" {
			byName[arg]++
		}
	}

	if finding := legacyAcceptanceFinding(byName); finding != "" {
		t.Errorf("НАХОДКА: %s", finding)
	}

	t.Logf("осмотрено: образов продукта %d, из них под каноническим именем %s — %d, "+
		"под прежним %s — %d, путь величины «%s» (прочитан у читателя)",
		total, canonicalProvenanceArg, byName[canonicalProvenanceArg],
		legacyProvenanceArg, byName[legacyProvenanceArg], revisionPath)
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА СБОРКИ
// ─────────────────────────────────────────────────────────────────────────────

// buildInvocation — один вызов сборки, приведённый к одной строке.
type buildInvocation struct {
	site string // файл
	text string // текст вызова
}

var buildCmdRe = regexp.MustCompile(`docker\s+(buildx\s+)?build\b`)

// joinContinuations — склеивает строки, перенесённые обратной косой.
func joinContinuations(body string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmed, `\`) {
			cur.WriteString(strings.TrimSuffix(trimmed, `\`))
			cur.WriteString(" ")
			continue
		}
		cur.WriteString(trimmed)
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// makeBuildInvocations — вызовы `docker build` в рецепте (комментарии отброшены:
// гейт, считающий совпадения в комментарии, покраснел бы на собственном
// объяснении).
func makeBuildInvocations(site, body string) []buildInvocation {
	var out []buildInvocation
	for _, line := range joinContinuations(body) {
		code := line
		if i := strings.Index(code, "#"); i >= 0 && strings.TrimSpace(code[:i]) == "" {
			continue // строка целиком комментарий
		}
		if !buildCmdRe.MatchString(code) || !strings.Contains(code, "Dockerfile") {
			continue
		}
		out = append(out, buildInvocation{site: site, text: code})
	}
	return out
}

// workflowStep — минимум полей шага, нужный гейту.
type workflowStep struct {
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

type workflowFile struct {
	Jobs map[string]struct {
		Steps []workflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// workflowBuildInvocations — вызовы сборки в процессе конвейера. Читается
// РАЗОБРАННЫЙ YAML: имя действия встречается и в комментариях, и проверка по
// подстроке краснела бы на объяснении рядом с собой.
func workflowBuildInvocations(t *testing.T, site, body string) []buildInvocation {
	t.Helper()
	var wf workflowFile
	if err := yaml.Unmarshal([]byte(body), &wf); err != nil {
		t.Fatalf("разбор %s: %v", site, err)
	}
	var out []buildInvocation
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			switch {
			case strings.HasPrefix(step.Uses, "docker/build-push-action"):
				text := step.Uses
				for _, k := range []string{"build-args", "file", "tags"} {
					if v, ok := step.With[k]; ok {
						text += " " + k + "=" + toText(v)
					}
				}
				out = append(out, buildInvocation{site: site, text: text})
			case buildCmdRe.MatchString(step.Run) && strings.Contains(step.Run, "Dockerfile"):
				for _, inv := range makeBuildInvocations(site, step.Run) {
					out = append(out, inv)
				}
			}
		}
	}
	return out
}

func toText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		b, _ := yaml.Marshal(x)
		return string(b)
	}
}

// buildSites — файлы, зовущие сборку. ВЫВОДЯТСЯ из дерева.
func buildSites(t *testing.T) (makefiles, workflows []string) {
	t.Helper()
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		base := info.Name()
		isMake := base == "Makefile" || strings.HasSuffix(base, ".mk")
		isFlow := strings.HasPrefix(filepath.ToSlash(path), "../.github/workflows/") &&
			(strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml"))
		if !isMake && !isFlow {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil || !buildCmdRe.Match(raw) && !strings.Contains(string(raw), "docker/build-push-action") {
			return nil
		}
		if isMake {
			makefiles = append(makefiles, path)
		} else {
			workflows = append(workflows, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}
	sort.Strings(makefiles)
	sort.Strings(workflows)
	if len(makefiles)+len(workflows) == 0 {
		t.Fatal("вызовов сборки в дереве не найдено — предмет гейта исчез, и это находка, а не чистота")
	}
	return makefiles, workflows
}

// namesPassedBy — ИМЕНА доводов, которые вызов передаёт: буквально либо через
// переменную, чьё объявление их несёт. Вторая форма нужна рецепту стенда: там
// аргументы собраны в одну переменную, и требовать буквального имени в каждой
// строке значило бы требовать копий.
func namesPassedBy(text string, carriers map[string]map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, name := range knownProvenanceArgs {
		if strings.Contains(text, name) {
			out[name] = true
		}
	}
	for v, names := range carriers {
		if !strings.Contains(text, v) {
			continue
		}
		for name := range names {
			out[name] = true
		}
	}
	return out
}

// dockerfileArgOf — имя довода, объявленное целью вызова. Второй результат —
// разрешилась ли цель вовсе: путь, собранный из переменной оболочки или матрицы
// конвейера, не разрешается by construction, и это НЕ находка, а граница.
func dockerfileArgOf(site, text string) (arg string, resolved bool) {
	m := regexp.MustCompile(`(?:-f|file=)\s*"?([^\s"]*Dockerfile[^\s"]*)"?`).FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	rel := m[1]
	if strings.ContainsAny(rel, "$*{") {
		return "", false // путь собран из переменной либо матрицы
	}
	base := filepath.Dir(site)
	// `cd <X> && docker build …` меняет базу пути. Берём ПОСЛЕДНИЙ переход перед
	// сборкой: рецепты стенда пишут `( cd ../ui-future && docker build … )`.
	if i := buildCmdRe.FindStringIndex(text); i != nil {
		for _, cd := range regexp.MustCompile(`cd\s+([^\s&;]+)`).FindAllStringSubmatchIndex(text[:i[0]], -1) {
			base = filepath.Join(base, text[cd[2]:cd[3]])
		}
	}
	// Процессы конвейера исполняются из корня дерева, а не из каталога своего файла.
	if strings.Contains(filepath.ToSlash(site), "/.github/workflows/") {
		base = ".."
	}
	path := filepath.Join(base, rel)
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, name := range knownProvenanceArgs {
		if argDeclRe(name).MatchString(string(body)) {
			return name, true
		}
	}
	// Файл есть, а величины не объявляет — это находка СТОРОНЫ ОБРАЗА, и её уже
	// назвал TestEveryProductImageCarriesTheTreeRevision. Здесь сверять нечего.
	return "", false
}

var makeVarDeclRe = regexp.MustCompile(`^([A-Z_][A-Z0-9_]*)\s*[:?]?=`)
var makeIncludeRe = regexp.MustCompile(`^-?include\s+(\S+)`)

// revisionCarryingVars — переменные, чьё ОБЪЯВЛЕНИЕ содержит величину, ВКЛЮЧАЯ
// объявленные во включаемых файлах. Значение — набор ИМЁН, которые переменная
// несёт: переходная пара несёт два, и сверка соответствия обязана знать какие.
//
// Включение обходится, а не игнорируется, и это существенно: величина объявлена
// ОДИН раз на дерево (provenance.mk), поэтому рецепт, судимый только по
// собственному тексту, выглядел бы нарушителем ровно за то, что не держит копии
// предиката, — гейт толкал бы к тому, против чего заведён. Обратная сторона тоже
// верна: включение файла, величины не объявляющего, ничего не оправдывает.
// Возвращает носители И находки; о находках отчитывается ВЫЗЫВАЮЩИЙ. Функция,
// роняющая прогон сама, непроверяема: пробе инъекции пришлось бы падать, чтобы
// доказать, что она работает.
func revisionCarryingVars(path string, depth int) (carriers map[string]map[string]bool, findings []string) {
	carriers = map[string]map[string]bool{}
	if depth > 4 {
		return carriers, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return carriers, nil
	}
	dir := filepath.Dir(path)
	for _, line := range joinContinuations(string(raw)) {
		trimmed := strings.TrimSpace(line)
		if m := makeIncludeRe.FindStringSubmatch(trimmed); m != nil {
			included := filepath.Join(dir, m[1])
			if _, err := os.Stat(included); err != nil {
				findings = append(findings, path+" включает «"+m[1]+"», которого нет — "+
					"включение в пустоту не приносит ни величины, ни отказа")
				continue
			}
			c, f := revisionCarryingVars(included, depth+1)
			for v, names := range c {
				if carriers[v] == nil {
					carriers[v] = map[string]bool{}
				}
				for n := range names {
					carriers[v][n] = true
				}
			}
			findings = append(findings, f...)
			continue
		}
		var carried []string
		for _, name := range knownProvenanceArgs {
			if strings.Contains(line, name) {
				carried = append(carried, name)
			}
		}
		if len(carried) == 0 {
			continue
		}
		if m := makeVarDeclRe.FindStringSubmatch(trimmed); m != nil {
			v := "$(" + m[1] + ")"
			if carriers[v] == nil {
				carriers[v] = map[string]bool{}
			}
			for _, n := range carried {
				carriers[v][n] = true
			}
		}
	}
	return carriers, findings
}

func TestEveryImageBuildPassesTheTreeRevision(t *testing.T) {
	makefiles, workflows := buildSites(t)

	var seen, checked, unresolved int
	judge := func(inv buildInvocation, carriers map[string]map[string]bool) {
		seen++
		passed := namesPassedBy(inv.text, carriers)
		if len(passed) == 0 {
			t.Errorf("НАХОДКА: %s — сборка не передаёт --build-arg %s=…; образ уедет без ревизии, "+
				"и провенанс стенда ответит «не установлена»", inv.site, canonicalProvenanceArg)
			return
		}
		want, resolved := dockerfileArgOf(inv.site, inv.text)
		if !resolved {
			unresolved++
			return
		}
		checked++
		if !passed[want] {
			names := make([]string, 0, len(passed))
			for n := range passed {
				names = append(names, n)
			}
			sort.Strings(names)
			t.Errorf("НАХОДКА: %s — сборка передаёт довод под именем %s, а её цель объявляет ARG %s; "+
				"docker непотреблённый довод выбрасывает МОЛЧА, ARG остаётся пустым, и образ уедет "+
				"без ревизии при зелёной сборке", inv.site, strings.Join(names, "+"), want)
		}
	}

	for _, f := range makefiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		carriers, findings := revisionCarryingVars(f, 0)
		for _, finding := range findings {
			t.Errorf("НАХОДКА: %s", finding)
		}
		for _, inv := range makeBuildInvocations(f, string(raw)) {
			judge(inv, carriers)
		}
	}
	for _, f := range workflows {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		for _, inv := range workflowBuildInvocations(t, f, string(raw)) {
			judge(inv, nil)
		}
	}

	if seen == 0 {
		t.Fatal("вызовов сборки ноль — «все передают величину» здесь означало бы «ни один не прочитан»")
	}
	// Соответствие имён проверено НЕ у всех вызовов, и это надо назвать числом:
	// цель, собранная из переменной оболочки, не разрешается by construction.
	if checked == 0 {
		t.Error("НАХОДКА: ни у одного вызова цель не разрешилась — сверка имён не выполнена ни " +
			"разу, и «соответствие держится» здесь означало бы «не прочитано»")
	}
	t.Logf("осмотрено: рецептов %d, процессов конвейера %d, вызовов сборки %d; "+
		"соответствие имени сверено у %d, цель не разрешается у %d",
		len(makefiles), len(workflows), seen, checked, unresolved)
}
