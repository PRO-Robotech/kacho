// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanvars_test.go — гейт связности newman-наборов: каждая `{{переменная}}`, которую
// использует коллекция, обязана кем-то выставляться.
//
// Зачем. Postman НЕ ругается на неразрешённую переменную — он подставляет её ЛИТЕРАЛОМ.
// Наружу это вылезает бессмысленным симптомом далеко от причины:
//   - `invalid resource id '{{lifeId}}'` → 400 вместо ожидаемого 404;
//   - `getaddrinfo ENOTFOUND {{internalbaseurl}}`;
//   - `expected [] to include undefined`.
//
// Автор такого кейса видит «баг продукта», хотя продукт ни при чём. Хуже: кейс может
// быть ЗЕЛЁНЫМ по неверной причине, если его ожидания случайно совпадут с ответом на
// мусорный запрос.
//
// Реальный случай (2026-07-16): `list-filter-d` требовал jwtSubnetSubsetViewer /
// listFilterProjectId / subnetVisibleId / subnetHiddenId. Его docstring прямо утверждал
// «Pre-conditions готовит tests/authz-fixtures/setup.sh… Setup патчит env-файл, добавляя
// …» — а setup их НИКОГДА не добавлял. Док противоречил коду, тест был сломан с момента
// написания и никто этого не замечал: падение выглядело как продуктовый 401/undefined.
//
// Гейт статический (никакого стенда) и точный: на 6 сервисах даёт 0 ложных
// срабатываний — переменные, выставляемые скриптами коллекции (`pm.environment.set`),
// учитываются как определённые.
//
// ЧТО СЧИТАЕТСЯ ИСТОЧНИКОМ. Отслеживаемый файл окружения набора (в дереве это
// `…/environments/local.postman_environment.template.json` — из него прогон
// материализует рабочий env), `pm.*.set` в самой коллекции и слоты, которые харнесс
// передаёт через `--env-var` (runtimeVars). Запись, сделанную посевом в
// `local.postman_environment.json`, гейт источником НЕ считает и считать не может:
// этот файл объявлен игнорируемым (в него ложатся живые предъявительские токены),
// в коммите его нет, и вердикт по нему был бы свойством чужого рабочего каталога.
// Поэтому переменная, которую посев выставляет в рантайме, обязана быть ОБЪЯВЛЕНА
// в отслеживаемом шаблоне — значение посев допишет.
package artifactgates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// runtimeVars — выставляются харнессом при запуске (deploy/scripts/newman-e2e.sh
// передаёт их через --env-var), а не env-файлом. Не считаются пропущенными.
var runtimeVars = map[string]bool{
	"baseUrl":         true,
	"internalBaseUrl": true,
	"runId":           true,
}

// knownGaps — ИЗВЕСТНЫЕ, отслеживаемые пробелы фикстур. Не «чтобы гейт молчал», а чтобы
// он защищал от НОВЫХ случаев, пока чинится старый: без allowlist'а пришлось бы либо
// держать CI красным, либо не ставить гейт вовсе.
//
// Ключ — "<сервис>/<переменная>" (ровно та форма, которой гейт ниже адресует находку),
// значение — ссылка на тикет.
//
// Исключение самоистекающее: TestKnownGapsStillHaveSubject падает, как только переменная
// перестаёт быть находкой — её засеяли, либо кейс, который её требовал, убрали. Запись,
// пережившая свой фикс, — не безобидный мусор: она НАВСЕГДА выводит переменную из-под
// гейта, и следующий настоящий пробел по ней проходит незамеченным. Пустеет — удаляется
// вместе с этой картой.
var knownGaps = map[string]string{
	// Пусто. Пробелы list-filter-d (kacho-vpc) — listFilterProjectId, subnetVisibleId,
	// subnetHiddenId по PRO-Robotech/kacho#1 — закрыты посевом: переменные приезжают в
	// env-файл набора. Записи сняты 2026-07-29, когда гейт срока годности показал, что
	// предмета у них больше нет (пережили свой фикс, 73b394ab).
}

var (
	varRe = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)
	setRe = regexp.MustCompile(`(?:environment|collectionVariables|globals)\.set\(\s*['"]([A-Za-z_][A-Za-z0-9_]*)`)
)

// newmanScan — разбор наборов: что коллекции используют и что при этом ниоткуда не
// берётся. Ключи обеих карт — "<сервис>/<переменная>", ровно как в knownGaps.
//
// knownGaps здесь НЕ применяется намеренно. Гейт и проверка срока годности исключений
// обязаны считать находки ОДНИМ предикатом: если бы срок годности мерился уже
// отфильтрованным набором, исключённая запись всегда подтверждала бы сама себя и не
// смогла бы истечь никогда.
type newmanScan struct {
	used        map[string]bool // переменная встречается в коллекции набора
	unsourced   map[string]bool // ...и её никто не выставляет: env, скрипт, runtime
	collections int             // сколько файлов коллекций реально прочитано
	suites      int             // ...из скольких наборов
}

// newmanSuiteSlot — к какому набору и к какой его части относится путь ИЗ ИНДЕКСА.
//
// Разбор по сегментам, а не по шаблону пути: сегмент — то же, чем набор адресуется
// в дереве, и он переживает смену разделителя и вложенность. `ok=false` означает
// «путь к наборам отношения не имеет», а не «набора нет».
func newmanSuiteSlot(rel string) (suite, kind string, ok bool) {
	if !strings.HasSuffix(rel, ".json") {
		return "", "", false
	}
	parts := strings.Split(rel, "/")
	// services/<svc>/tests/newman/<kind>/<файл>
	if len(parts) == 6 && parts[0] == "services" && parts[2] == "tests" && parts[3] == "newman" {
		return parts[1], parts[4], true
	}
	// gateway/tests/newman/<kind>/<файл>
	if len(parts) == 5 && parts[0] == "gateway" && parts[1] == "tests" && parts[2] == "newman" {
		return "gateway", parts[3], true
	}
	return "", "", false
}

// scanNewmanVars — единственное место, где вычисляются находки этого гейта.
//
// Состав наборов берётся у ИНДЕКСА git, а не с диска. Причина не косметическая:
// `local.postman_environment.json` каждого набора объявлен игнорируемым (в него посев
// пишет живые предъявительские токены), а рядом лежит отслеживаемый шаблон, из
// которого прогон этот файл материализует. Обход диска читает оба — и вердикт гейта
// перестаёт быть свойством КОММИТА, становясь свойством рабочего каталога: на машине,
// где хоть раз поднимали стенд, переменная «определена» неотслеживаемым файлом и гейт
// молчит; в свежем клоне (то есть в CI) того же коммита он краснеет. Ровно так этот
// гейт и разошёлся сам с собой — 2026-08-04, две переменные iam.
//
// Обратная сторона того же дефекта тише: неотслеживаемая коллекция (черновик, отчёт
// прогона) приносит СВОИ `{{переменные}}`, и гейт краснеет о наборе, которого коммит
// не содержит. Инъекция в обе стороны — TestNewmanCorpusComesFromTheIndex.
func scanNewmanVars(t *testing.T, root string) newmanScan {
	t.Helper()

	scan := newmanScan{used: map[string]bool{}, unsourced: map[string]bool{}}

	tracked, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав наборов newman: %v", err)
	}

	cols := map[string][]string{}
	envs := map[string][]string{}
	for _, abs := range tracked {
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			t.Fatalf("относительный путь для %s: %v", abs, relErr)
		}
		suite, kind, ok := newmanSuiteSlot(filepath.ToSlash(rel))
		if !ok {
			continue
		}
		switch kind {
		case "collections":
			cols[suite] = append(cols[suite], abs)
		case "environments":
			envs[suite] = append(envs[suite], abs)
		}
	}

	suites := make([]string, 0, len(cols))
	for suite := range cols {
		suites = append(suites, suite)
	}
	sort.Strings(suites) // детерминизм входа — часть контракта проверки

	for _, svc := range suites {
		if len(cols[svc]) == 0 {
			continue // набора нет (напр. geo — только README)
		}
		scan.collections += len(cols[svc])
		scan.suites++

		defined := map[string]bool{}
		for _, e := range envs[svc] {
			for _, k := range envKeys(t, e) {
				defined[k] = true
			}
		}

		used := map[string]bool{}
		for _, c := range cols[svc] {
			b, err := os.ReadFile(c)
			if err != nil {
				t.Fatalf("read %s: %v", c, err)
			}
			s := string(b)
			for _, m := range varRe.FindAllStringSubmatch(s, -1) {
				used[m[1]] = true
			}
			// Переменные, которые коллекция выставляет себе сама по ходу прогона.
			for _, m := range setRe.FindAllStringSubmatch(s, -1) {
				defined[m[1]] = true
			}
		}

		for v := range used {
			scan.used[svc+"/"+v] = true
			if defined[v] || runtimeVars[v] {
				continue
			}
			scan.unsourced[svc+"/"+v] = true
		}
	}
	return scan
}

// TestNewmanVariablesAreDefined — ни одна используемая {{var}} не остаётся без источника.
func TestNewmanVariablesAreDefined(t *testing.T) {
	scan := scanNewmanVars(t, repoRoot(t))

	// Предпосылка: корпус прочитан. Прежняя редакция здесь ПРОПУСКАЛА проверку, и
	// «наборов не найдено» было неотличимо от «наборы в порядке» — притом что ровно
	// это состояние и наступает, если корпус перестал находиться (набор переехал,
	// каталог переименован). Пропуск на пустом корпусе — не осторожность, а зелёный
	// вердикт, за которым ничего не стоит.
	if scan.collections == 0 {
		t.Fatal("прочитано ноль коллекций newman — смотреть было не на что. " +
			"«Ноль находок» здесь означало бы «ноль прочитанного»: проверь, не переехали " +
			"ли наборы из <services/*|gateway>/tests/newman/collections/ и лежат ли они в индексе")
	}

	var problems []string
	for key := range scan.unsourced {
		if _, known := knownGaps[key]; known {
			continue
		}
		svc, v, _ := strings.Cut(key, "/")
		problems = append(problems, svc+": {{"+v+"}} — не в env, не выставляется скриптом, не runtime")
	}
	sort.Strings(problems)

	// Объём осмотренного печатается всегда — и на зелёном тоже.
	t.Logf("осмотрено: коллекций %d, наборов %d, переменных в употреблении %d; без источника %d "+
		"(исключений knownGaps: %d)",
		scan.collections, scan.suites, len(scan.used), len(scan.unsourced), len(knownGaps))

	if len(problems) > 0 {
		t.Errorf("%d newman-переменн(ая|ых) без источника — Postman подставит их ЛИТЕРАЛОМ, и падение "+
			"будет выглядеть багом продукта:\n%s\n\n"+
			"починить — ОДНО из двух:\n"+
			"  1) объявить переменную в ОТСЛЕЖИВАЕМОМ шаблоне окружения набора\n"+
			"     (…/environments/local.postman_environment.template.json), значение допишет\n"+
			"     посев (tests/authz-fixtures/*, patch-env.py). Записи посева в игнорируемый\n"+
			"     local.postman_environment.json НЕДОСТАТОЧНО: этого файла в коммите нет,\n"+
			"     и вердикт по нему был бы свойством рабочего каталога;\n"+
			"  2) выставлять её в самой коллекции через pm.environment.set.",
			len(problems), strings.Join(problems, "\n"))
	}
}

// TestKnownGapsAreTracked — каждая запись knownGaps несёт ссылку на тикет, и карта не
// разрастается молча.
func TestKnownGapsAreTracked(t *testing.T) {
	for k, issue := range knownGaps {
		if !strings.Contains(issue, "#") {
			t.Errorf("knownGaps[%q] = %q — нужна ссылка на тикет (owner/repo#N)", k, issue)
		}
	}
}

// TestKnownGapsStillHaveSubject — исключение обязано умереть вместе со своим предметом.
//
// Ссылка на тикет (её проверяет TestKnownGapsAreTracked) говорит лишь, что запись КОГДА-ТО
// завели осмысленно. Она ничего не говорит о том, жив ли пробел сейчас. Пробел чинят в
// другом месте — в посеве фикстур, — и запись про это не узнаёт: она молча переживает свой
// фикс и продолжает держать переменную вне гейта. Тогда СЛЕДУЮЩИЙ настоящий пробел по этой
// же переменной будет поглощён невидимо, а карта исключений превратится в утверждение о
// продукте, которое давно неверно.
//
// Поэтому запись, которой больше нечего исключать, — находка, а не «просто больше не нужна».
func TestKnownGapsStillHaveSubject(t *testing.T) {
	if len(knownGaps) == 0 {
		return // исключать нечего — гейт взведён для следующей записи
	}

	scan := scanNewmanVars(t, repoRoot(t))
	if scan.collections == 0 {
		// Предпосылка не выполнена: предмет исключений не прочитан вовсе, поэтому
		// «находки нет» здесь означало бы «не смотрели», а не «пробел закрыт».
		t.Skip("newman-наборов не найдено — судить о сроке годности knownGaps не по чему")
	}

	for key, issue := range knownGaps {
		if scan.unsourced[key] {
			continue // пробел жив, исключение по-прежнему имеет предмет
		}
		reason := "переменная теперь откуда-то берётся (env-файл набора, pm.*.set в коллекции или runtime)"
		if !scan.used[key] {
			reason = "переменную больше не использует ни одна коллекция набора"
		}
		t.Errorf("исключение knownGaps[%q] (%s) больше не нужно: %s. Удали запись — иначе эта "+
			"переменная навсегда останется вне гейта, и следующий настоящий пробел по ней "+
			"пройдёт незамеченным. (прочитано коллекций: %d)",
			key, issue, reason, scan.collections)
	}
}

// TestNewmanCorpusComesFromTheIndex — вердикт гейта обязан быть свойством КОММИТА,
// а не рабочего каталога, в котором когда-то гоняли стенд.
//
// Предмет. Env-файл набора (`local.postman_environment.json`) в репозитории не лежит:
// в него посев пишет живые предъявительские токены, и `.gitignore` держит его вне
// индекса намеренно. Рядом лежит ОТСЛЕЖИВАЕМЫЙ шаблон, из которого прогон этот файл
// материализует. Собирая корпус с диска, гейт читает оба — и его ответ начинает
// зависеть от того, гоняли ли на этой машине стенд.
//
// Расхождение ходит в обе стороны, и обе проверяются здесь настоящим входом:
//
//   - ТИШИНА: переменная, которой в отслеживаемом шаблоне нет, «определяется»
//     неотслеживаемым файлом — на машине разработчика гейт молчит, в свежем клоне
//     (то есть в CI) краснеет. Именно этим дефект и был найден;
//   - ШУМ: коллекция, которой в репозитории нет (отчёт прогона, локальный черновик),
//     приносит собственные `{{переменные}}` — гейт краснеет о наборе, которого коммит
//     не содержит.
//
// Положительный контроль в паре с обоими: отслеживаемый шаблон источником быть НЕ
// перестаёт, набор шлюза по-прежнему находится, и перепись прочитанных коллекций
// печатается — «ноль находок» обязано быть отличимо от «ноль прочитанного».
func TestNewmanCorpusComesFromTheIndex(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := gitenv.Command(root, args...)
		cmd.Env = append(cmd.Env,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	collection := func(vars ...string) string {
		refs := make([]string, 0, len(vars))
		for _, v := range vars {
			refs = append(refs, `"{{`+v+`}}"`)
		}
		return `{"item":[` + strings.Join(refs, ",") + `]}`
	}
	env := func(keys ...string) string {
		vals := make([]string, 0, len(keys))
		for _, k := range keys {
			vals = append(vals, `{"key":"`+k+`","value":""}`)
		}
		return `{"values":[` + strings.Join(vals, ",") + `]}`
	}

	git("init", "-q")
	write("go.mod", "module synthetic\n")
	// Ровно то правило игнорирования, что живёт в дереве: материализованный
	// env-файл набора вне индекса, шаблон — в индексе.
	write(".gitignore", "local.postman_environment.json\nstray.postman_collection.json\n")
	write("services/demo/tests/newman/collections/demo.postman_collection.json",
		collection("trackedVar", "strayVar"))
	write("services/demo/tests/newman/environments/local.postman_environment.template.json",
		env("trackedVar"))
	write("gateway/tests/newman/collections/gw.postman_collection.json", collection("gwVar"))
	write("gateway/tests/newman/environments/local.postman_environment.template.json",
		env("gwVar"))
	git("add", "-A")
	git("-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "fixture")

	// СЛЕДЫ ПРОШЛОГО ПРОГОНА — то, что лежит на машине, где поднимали стенд.
	write("services/demo/tests/newman/environments/local.postman_environment.json",
		env("trackedVar", "strayVar"))
	write("services/demo/tests/newman/collections/stray.postman_collection.json",
		collection("ghostVar"))

	scan := scanNewmanVars(t, root)

	// Перепись: сначала убеждаемся, что читали именно коммит.
	if scan.collections != 2 {
		t.Errorf("прочитано коллекций %d, а в индексе их 2 — корпус собран с диска: "+
			"вердикт стал свойством рабочего каталога, а не коммита", scan.collections)
	}

	// (а) ТИШИНА: неотслеживаемый env-файл источником не является.
	if !scan.unsourced["demo/strayVar"] {
		t.Error("{{strayVar}} признана определённой: её выставляет только НЕотслеживаемый " +
			"local.postman_environment.json. На машине со следами прогона гейт молчит, " +
			"в свежем клоне краснеет — один коммит, два разных вердикта")
	}

	// (б) ШУМ: неотслеживаемая коллекция в корпус не входит.
	if scan.used["demo/ghostVar"] {
		t.Error("{{ghostVar}} засчитана использованной: её приносит НЕотслеживаемая " +
			"коллекция. Гейт краснеет о наборе, которого коммит не содержит, — и " +
			"настоящая находка тонет среди привнесённых")
	}

	// (в) ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: отслеживаемое источником быть не перестало.
	if !scan.used["demo/trackedVar"] {
		t.Error("{{trackedVar}} не увидена вовсе — отслеживаемая коллекция потеряна, " +
			"и тогда «ноль находок» означает «ноль прочитанного»")
	}
	if scan.unsourced["demo/trackedVar"] {
		t.Error("{{trackedVar}} объявлена без источника, хотя её определяет ОТСЛЕЖИВАЕМЫЙ " +
			"шаблон окружения: отсев стал грубее своего предмета")
	}
	// (г) ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: набор шлюза находится и по индексу.
	if !scan.used["gateway/gwVar"] {
		t.Error("набор gateway/tests/newman не найден — прежняя редакция находила его " +
			"проверкой существования каталога на диске, и замена обязана сохранить находку")
	}
	if scan.unsourced["gateway/gwVar"] {
		t.Error("{{gwVar}} объявлена без источника, хотя её определяет отслеживаемый шаблон шлюза")
	}
}

func envKeys(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		Values []struct {
			Key string `json:"key"`
		} `json:"values"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := make([]string, 0, len(doc.Values))
	for _, v := range doc.Values {
		out = append(out, v.Key)
	}
	return out
}
