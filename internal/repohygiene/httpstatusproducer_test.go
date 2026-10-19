// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// httpstatusproducer_test.go — гейт против пробы, ждущей код ответа, которого край
// произвести НЕ МОЖЕТ.
//
// ПРЕДМЕТ. Край своего отображения ошибок не несёт: `runtime.NewServeMux` в
// `gateway/internal/restmux/mux.go` собирается без `WithErrorHandler`, поэтому
// HTTP-статус выбирает `runtime.HTTPStatusFromCode`. Множество производимых кодов
// этим и задано — оно КОНЕЧНО и известно. Утверждение, ждущее кода вне этого
// множества, не может пройти ни при каком состоянии продукта: оно либо всегда
// красное (строгое равенство), либо — хуже — молча расширяет допуск внутри
// `oneOf`, перечисляя исход, у которого нет производителя. Второе не краснеет
// никогда и потому живёт годами.
//
// ЧТО ЭТО НАШЛО, КОГДА ПИСАЛОСЬ (2026-08-04): два кейса ждали строгий 412 на
// `FAILED_PRECONDITION`, а библиотека отдаёт 400 — и говорит об этом собственным
// комментарием в той же ветке («deliberately doesn't translate to the similarly
// named '412 Precondition Failed'»). Ещё четыре места перечисляли 412 (и одно —
// 422) внутри `oneOf`, то есть держали допуск на исход, которого не бывает.
// Тринадцать строк документации iam объявляли то же самое таблицами ошибок.
//
// ПРЕДПОСЫЛКА ПРОВЕРЯЕТСЯ, А НЕ ОБЪЯВЛЯЕТСЯ. Множество производимых кодов здесь
// не выписано литералом — оно ВЫЧИСЛЯЕТСЯ вызовом самой библиотеки по всем кодам
// gRPC. Поэтому обновление grpc-gateway меняет ответ гейта вместе с собой, и
// правило не может пережить свой предмет. Если край когда-нибудь заведёт свой
// `WithErrorHandler`, предпосылка станет ложной — и об этом сказано в
// `api-conventions.md` §«gRPC-код → HTTP-статус», где живёт канон.
//
// ЧИТАЮТСЯ ОБА МЕСТА, ГДЕ ЖИВУТ УТВЕРЖДЕНИЯ. Рукописный кейс и ГЕНЕРАТОР суиты:
// одна строка генератора раскладывается на десятки шагов. Пока читались только
// кейсы, гейт был зелёным при 124 утверждениях о коде ответа, лежавших в восьми
// генераторах, — то есть корзина, в которой он ищет, не совпадала с местом, где
// пишут. Прежний счёт «781 утверждение» стал 905.
//
// ОБЪЁМ ОСМОТРЕННОГО ПЕЧАТАЕТСЯ, И ОТДЕЛЬНО — ОБЪЁМ НЕПРОЧИТАННОГО. «Ноль
// находок» обязано быть отличимо и от «ноль прочитанного» (гейт отказывается
// проходить, не прочитав ни одного файла), и от «прочитано не то»: строки,
// называющие код ответа формой, которую предикат не разбирает, считаются
// отдельно, иначе рост слепой зоны неотличим от чистоты.
package repohygiene

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc/codes"
)

// muxOwnStatuses — коды, которые край отдаёт САМ, не транслируя ответ бэкенда.
// Они не выводятся из `HTTPStatusFromCode`, поэтому названы поимённо и с
// причиной: список без причин через полгода неотличим от списка удобства, а
// именно в такой список и складывают то, что лень чинить.
//
// Производитель у каждой записи ЖИВОЙ и проверяется отдельным утверждением
// (см. edgeStatusProducers ниже): запись, чей производитель ушёл из дерева, —
// находка, а не тихое послабление.
var muxOwnStatuses = map[int]string{
	405: "маршрут есть, метод не тот — решает сам мультиплексор до вызова бэкенда",
	415: "тело в типе, который marshaler не берёт — тоже до бэкенда",
	413: "объявленная длина тела больше потолка — отдаёт middleware края ДО мультиплексора " +
		"(и ingress своим пределом тоже), поэтому проба, допускающая 413 на большом теле, " +
		"называет исход, у которого есть производитель",
}

// edgeStatusProducers — чем ДОКАЗЫВАЕТСЯ каждая запись muxOwnStatuses, которую
// производит не библиотека, а наш собственный код. Ключ — код, значение — файл
// и литерал, который его выдаёт.
//
// Без этого запись «этот код кто-то производит» была бы утверждением о продукте,
// которое ничем не держится: снимут middleware — запись переживёт свой предмет и
// начнёт освобождать пробу, ждущую невозможного.
var edgeStatusProducers = map[int]struct{ file, literal string }{
	413: {"gateway/internal/middleware/http_body_limit.go", "http.StatusRequestEntityTooLarge"},
}

// statusAssertion — литерал кода рядом с `pm.response.code`.
//
// Форм РАВЕНСТВА в этом дереве две, и обе настоящие: chai даёт `.to.eql` и
// `.to.equal`, кейсы пишут и так и так (замер на d24476c1: 169 строк первой формы
// и 66 второй). Предикат, знающий одну, молча освобождал бы вторую — и «ноль
// находок» покрывал бы 66 утверждений, которых он не читал.
var (
	reEql   = regexp.MustCompile(`pm\.response\.code[^;]{0,120}?\.to\.(?:eql|equal)\(\s*(\d{3})\s*\)`)
	reOneOf = regexp.MustCompile(`pm\.response\.code[^;]{0,160}?\.to\.be\.oneOf\(\s*\[([0-9,\s]+)\]`)
)

// reUnreadShape — строка, которая ГОВОРИТ о коде ответа, но ни одной формой выше
// не читается: отрицательное сравнение (`.to.not.equal(403)`), ветвление
// (`if (pm.response.code === 404)`), список с подстановкой. Такие не находки — но
// и не «прочитано». Их число печатается отдельно, иначе рост слепой зоны
// неотличим от чистоты, а смена формы записи тихо выключает гейт.
var reUnreadShape = regexp.MustCompile(`pm\.response\.code`)

func producibleStatuses() map[int]codes.Code {
	out := map[int]codes.Code{}
	// Коды gRPC — непрерывный диапазон 0..16; берём с запасом и полагаемся на то,
	// что библиотека сама решает, что делать с неизвестным.
	for c := codes.Code(0); c <= codes.Code(16); c++ {
		out[runtime.HTTPStatusFromCode(c)] = c
	}
	return out
}

// stripJSLineComment убирает хвостовой `//`-комментарий. Гейт обязан отличать код
// от комментария: иначе он находит слово в тексте, ОБЪЯСНЯЮЩЕМ проверку, и
// краснеет на разборе, который сам же и просил написать.
func stripJSLineComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

func TestNoCaseAssertsAStatusTheEdgeCannotProduce(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	producible := producibleStatuses()
	if len(producible) < 8 {
		t.Fatalf("предпосылка не выполнена: библиотека вернула лишь %d различных статусов — "+
			"множество производимых кодов не вычислено, и утверждать «этого не бывает» не на чем",
			len(producible))
	}

	// Утверждения о коде ответа живут в ДВУХ местах, и оба обязаны читаться:
	// рукописный кейс и ГЕНЕРАТОР, который раскладывает одну строку на десятки
	// шагов. Пока читались только кейсы, четыре строки генераторов давали 42 шага
	// в 6 коллекциях, и ни одна из них не была даже рассмотрена. Соседний гейт
	// того же корпуса (tools/mixedoutcomeaudit) читает оба места — то есть где
	// лежат утверждения, корпус уже знал.
	patterns := []string{
		filepath.Join(root, "services", "*", "tests", "newman", "cases", "*.py"),
		filepath.Join(root, "gateway", "tests", "newman", "cases", "*.py"),
		filepath.Join(root, "services", "*", "tests", "newman", "scripts", "gen.py"),
		filepath.Join(root, "gateway", "tests", "newman", "scripts", "gen.py"),
	}
	var files []string
	for _, p := range patterns {
		m, err := filepath.Glob(p)
		if err != nil {
			t.Fatalf("обход %s: %v", p, err)
		}
		files = append(files, m...)
	}
	sort.Strings(files)

	filesRead := 0
	var total statusCensus
	var findings []string
	bodies := make(map[string]string, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f) // #nosec G304 -- путь из обхода дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		rel, _ := filepath.Rel(root, f)
		bodies[rel] = string(b)
	}
	dials := dialledTransports(t, bodies)
	for _, f := range files {
		filesRead++
		rel, _ := filepath.Rel(root, f)
		got, c := judgeStatusAssertions(rel, bodies[rel], producible, dials[rel])
		findings = append(findings, got...)
		total.add(c)
	}
	assertionsSeen := total.seen
	t.Logf("осмотрено: файлов кейсов %d, строк %d; строк, называющих код ответа, %d "+
		"(из них ПРОЧИТАНО предикатом %d, НЕ прочитано %d — отрицательные сравнения, "+
		"ветвления, списки с подстановкой); утверждений о коде ответа %d; "+
		"производимых статусов %d (вычислено вызовом библиотеки, не выписано); "+
		"файлов, набирающих транспорт мимо края В КОДЕ, %d — утверждений, отнесённых к его "+
		"слушателю, %d (в таких файлах край судится по-прежнему: освобождён только код слушателя); "+
		"файлов, называющих транспорт лишь текстом (комментарий, описание, константа без набора), "+
		"%d — освобождения у них нет; вызовов набора с неразрешённым адресатом %d",
		filesRead, total.lines, total.mentioning, total.mentioning-total.unread, total.unread,
		assertionsSeen, len(producible), total.transportFiles, total.viaTransport,
		total.mentionOnly, total.unresolvedDials)

	if filesRead == 0 {
		t.Fatal("прочитано НОЛЬ файлов кейсов — гейт не проверен ни против чего. " +
			"Это отказ, а не «находок нет».")
	}
	if assertionsSeen == 0 {
		t.Fatal("не найдено НИ ОДНОГО утверждения о коде ответа — предикат перестал " +
			"опознавать форму, которой пишут пробы. Это отказ, а не чистота.")
	}
	if len(findings) > 0 {
		t.Fatalf("утверждений о непроизводимом коде: %d\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// transportOwnStatuses — коды, которые производит НЕ край, а слушатель транспорта,
// который кейс набирает мимо края (`deploy/e2e-shards.json`, `optional_transports`:
// адрес передаёт прогонщик через `--env-var`).
//
// ПРЕДПОСЫЛКА ГЕЙТА «каждое утверждение о коде — об ответе края» в наборе registry
// ложна: плоскость данных реестра отвечает протоколом OCI distribution, и её
// успешные коды (`201` — блоб или манифест принят, `202` — сеанс загрузки открыт)
// край не производит никогда. Пока такие шаги утверждали только отказы (401, 404),
// ложь предпосылки была невидима: эти коды край тоже производит.
//
// «Набирает» решается по КОДУ, а не по тексту (dialledTransports): вызов
// `require_env_url`, чей адресат — имя транспорта литералом либо модульной
// константой. Имя в комментарии, описании или константе, которую никто не
// набирает, освобождения не даёт.
//
// Освобождение — ПО ФАЙЛУ, набирающему транспорт, и ТОЛЬКО по кодам его слушателя:
// шаг края в том же файле, ждущий 201, проходит молча — это объявленная слепая зона,
// и её размер печатается числом («утверждений, отнесённых к слушателю»). Шаг,
// ждущий кода, которого не производит НИ край, НИ слушатель, — находка и здесь.
//
// Производитель у записи — координата в нашем коде, и он проверяется отдельно
// (TestTransportOwnStatusesStillHaveASubject): плоскость данных ретранслирует коды
// хранилища через `httputil.ReverseProxy`; уйдёт ретрансляция — уйдёт и освобождение.
var transportOwnStatuses = map[string]transportStatuses{
	"registryDataPlaneBaseUrl": {
		statuses: map[int]string{
			201: "OCI distribution: блоб или манифест принят — код хранилища, ретранслируемый плоскостью данных",
			202: "OCI distribution: сеанс загрузки блоба открыт — так же",
		},
		file:    "services/registry/internal/dataplane/proxy.go",
		literal: "httputil.ReverseProxy",
	},
}

type transportStatuses struct {
	statuses      map[int]string
	file, literal string
}

// statusCensus — объём осмотренного судом файла.
type statusCensus struct {
	lines, mentioning, unread, seen int
	transportFiles, viaTransport    int
	mentionOnly, unresolvedDials    int
}

func (c *statusCensus) add(o statusCensus) {
	c.lines += o.lines
	c.mentioning += o.mentioning
	c.unread += o.unread
	c.seen += o.seen
	c.transportFiles += o.transportFiles
	c.viaTransport += o.viaTransport
	c.mentionOnly += o.mentionOnly
	c.unresolvedDials += o.unresolvedDials
}

// fileDials — ответ распознавателя набора об одном файле.
type fileDials struct {
	Dials      []string `json:"dials"`
	Unresolved int      `json:"unresolved"`
	Error      string   `json:"error"`
}

// transportDialDriver — распознаватель набора транспорта, исполняется python3 над
// РАЗОБРАННЫМ модулем (`ast`), а не над текстом: комментарий в дерево разбора не
// попадает, описание и строка — узлы-константы, а не вызов.
//
// Набор — вызов `require_env_url` (единственная функция генераторов, которая
// направляет шаг на адрес транспорта, `scripts/gen.py`), именем либо атрибутом;
// адресат — первый позиционный довод либо `var=`. Адресат разрешается, если он
// строковый литерал или имя, связанное на уровне модуля ровно одной строковой
// константой; иначе вызов считается неразрешённым и освобождения не даёт.
// Формы, найденные обходом дерева (2026-10-19, 82 файла): вызов именем с
// литералом (5) и с именем (3). Иная форма набора освобождения не получит и
// покраснеет находкой — в сторону отказа, а не молчания.
//
// Вход — JSON «путь → текст» на stdin, выход — JSON «путь → ответ». Отказ
// разбора — поле error, и гейт на нём падает: неразобранный файл не «не
// набирает», а не прочитан.
const transportDialDriver = `
import ast, json, sys

FUNC = "require_env_url"

def module_constants(tree):
    seen, bad = {}, set()
    for node in tree.body:
        if isinstance(node, ast.Assign):
            targets, value = node.targets, node.value
        elif isinstance(node, ast.AnnAssign) and node.value is not None:
            targets, value = [node.target], node.value
        else:
            continue
        for tg in targets:
            if not isinstance(tg, ast.Name):
                continue
            if (isinstance(value, ast.Constant) and isinstance(value.value, str)
                    and seen.get(tg.id, value.value) == value.value):
                seen[tg.id] = value.value
            else:
                bad.add(tg.id)
    return {k: v for k, v in seen.items() if k not in bad}

def dials(src):
    tree = ast.parse(src)
    consts = module_constants(tree)
    found, unresolved = set(), 0
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        f = node.func
        name = f.id if isinstance(f, ast.Name) else f.attr if isinstance(f, ast.Attribute) else None
        if name != FUNC:
            continue
        arg = node.args[0] if node.args else next(
            (k.value for k in node.keywords if k.arg == "var"), None)
        if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
            found.add(arg.value)
        elif isinstance(arg, ast.Name) and arg.id in consts:
            found.add(consts[arg.id])
        else:
            unresolved += 1
    return sorted(found), unresolved

out = {}
for rel, src in json.load(sys.stdin).items():
    try:
        d, u = dials(src)
        out[rel] = {"dials": d, "unresolved": u, "error": ""}
    except SyntaxError as e:
        out[rel] = {"dials": [], "unresolved": 0, "error": "%s:%s %s" % (rel, e.lineno, e.msg)}
sys.stdout.write(json.dumps(out))
`

// dialledTransports — для каждого файла: транспорты, которые он набирает В КОДЕ.
// Один вызов интерпретатора на весь обход; отказ разбора хоть одного файла —
// провал гейта с координатой.
func dialledTransports(t *testing.T, bodies map[string]string) map[string]fileDials {
	t.Helper()
	in, err := json.Marshal(bodies)
	if err != nil {
		t.Fatalf("вход распознавателя набора: %v", err)
	}
	cmd := exec.Command(pythonInterpreter(t), "-c", transportDialDriver) // #nosec G204 -- фиксированный интерпретатор и текст распознавателя
	cmd.Stdin = bytes.NewReader(in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("распознаватель набора транспорта не исполнился: %v\n%s", err, stderr.String())
	}
	out := map[string]fileDials{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("ответ распознавателя набора не разобран: %v\n%s", err, raw)
	}
	if len(out) != len(bodies) {
		t.Fatalf("распознаватель ответил о %d файлах из %d — набор не судим", len(out), len(bodies))
	}
	for _, d := range out {
		if d.Error != "" {
			t.Fatalf("файл кейсов не разобран, набор транспорта не установлен: %s", d.Error)
		}
	}
	return out
}

// judgeStatusAssertions — суд одного файла кейсов: утверждения о коде, которого
// не производит ни край, ни слушатель транспорта, набираемого этим файлом В КОДЕ
// (d — ответ dialledTransports о файле).
func judgeStatusAssertions(rel, body string, producible map[int]codes.Code, d fileDials) ([]string, statusCensus) {
	c := statusCensus{unresolvedDials: d.Unresolved}
	var findings []string
	dialled := map[int]bool{}
	for _, name := range d.Dials {
		tr, ok := transportOwnStatuses[name]
		if !ok {
			continue
		}
		c.transportFiles = 1
		for code := range tr.statuses {
			dialled[code] = true
		}
	}
	if c.transportFiles == 0 {
		for name := range transportOwnStatuses {
			if strings.Contains(body, name) {
				c.mentionOnly = 1
			}
		}
	}
	for i, raw := range strings.Split(body, "\n") {
		c.lines++
		line := stripJSLineComment(raw)
		if !reUnreadShape.MatchString(line) {
			continue
		}
		c.mentioning++
		var got []int
		if m := reEql.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			got = append(got, n)
		}
		if m := reOneOf.FindStringSubmatch(line); m != nil {
			for _, part := range strings.Split(m[1], ",") {
				if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
					got = append(got, n)
				}
			}
		}
		if len(got) == 0 {
			c.unread++
		}
		for _, code := range got {
			c.seen++
			if _, ok := producible[code]; ok {
				continue
			}
			if _, ok := muxOwnStatuses[code]; ok {
				continue
			}
			if dialled[code] {
				c.viaTransport++
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"%s:%d — утверждение ждёт HTTP %d, а край его не производит: "+
					"`runtime.HTTPStatusFromCode` не отдаёт этот код ни для одного кода gRPC, "+
					"и мультиплексор сам его не выдаёт. Строгое равенство здесь красное всегда; "+
					"внутри `oneOf` — допуск на исход, которого не бывает, и он не покраснеет "+
					"никогда. Канон отображения — api-conventions.md §«gRPC-код → HTTP-статус».",
				rel, i+1, code))
		}
	}
	return findings, c
}

// TestStatusJudgeInjection_TransportFileMayAssertOnlyItsListenerStatus — суд
// способен упасть и смолчать на синтетике, по одному факту на пару. Набор
// решает тот же распознаватель, что судит дерево (dialledTransports), — не
// подставленный ответ:
//
//	(а) файл без транспорта ждёт 201 — находка (край 201 не производит);
//	(б) тот же файл, набирающий транспорт модульной константой, — молчание, и
//	    утверждение отнесено к слушателю;
//	(в) файл с набором ждёт 207 — находка: освобождён код слушателя, а не всё;
//	(г) набор литералом и (д) доводом `var=` — молчание;
//	(е) имя транспорта в КОММЕНТАРИИ — находка, файл не засчитан набирающим;
//	(ж) имя в описании модуля — находка; (з) константа, которую никто не
//	    набирает, — находка (прежняя синтетика ставила набор именно так);
//	(и) набор с адресатом-параметром функции — находка, вызов неразрешён.
func TestStatusJudgeInjection_TransportFileMayAssertOnlyItsListenerStatus(t *testing.T) {
	t.Parallel()
	producible := producibleStatuses()
	const step = "S = \"pm.test('s', () => pm.expect(pm.response.code).to.eql(%d));\"\n"
	const dial = "DP = \"registryDataPlaneBaseUrl\"\nPRE = require_env_url(DP, \"/v2/\")\n"
	cases := []struct {
		name, body                      string
		findings, transport, via, other int
	}{
		{"(а) без транспорта, 201", fmt.Sprintf(step, 201), 1, 0, 0, 0},
		{"(б) набор константой, 201", dial + fmt.Sprintf(step, 201), 0, 1, 1, 0},
		{"(в) набор константой, 207", dial + fmt.Sprintf(step, 207), 1, 1, 0, 0},
		{"(г) набор литералом, 201",
			"PRE = require_env_url(\"registryDataPlaneBaseUrl\", \"/v2/\", \"why\")\n" + fmt.Sprintf(step, 201), 0, 1, 1, 0},
		{"(д) набор доводом var=, 202",
			"PRE = require_env_url(var=\"registryDataPlaneBaseUrl\", path=\"/v2/\")\n" + fmt.Sprintf(step, 202), 0, 1, 1, 0},
		{"(е) имя в комментарии, 201", "# registryDataPlaneBaseUrl\n" + fmt.Sprintf(step, 201), 1, 0, 0, 1},
		{"(ж) имя в описании, 201",
			"\"\"\"Шаги ходят на {{registryDataPlaneBaseUrl}}.\"\"\"\n" + fmt.Sprintf(step, 201), 1, 0, 0, 1},
		{"(з) константа без набора, 201", "DP = \"registryDataPlaneBaseUrl\"\n" + fmt.Sprintf(step, 201), 1, 0, 0, 1},
		{"(и) адресат-параметр, 201",
			"def _dp(v):\n    return require_env_url(v, \"/v2/\")\n" + fmt.Sprintf(step, 201), 1, 0, 0, 0},
	}
	bodies := map[string]string{}
	for i, tc := range cases {
		bodies[fmt.Sprintf("c%d.py", i)] = tc.body
	}
	dials := dialledTransports(t, bodies)
	for i, tc := range cases {
		rel := fmt.Sprintf("c%d.py", i)
		got, c := judgeStatusAssertions(rel, tc.body, producible, dials[rel])
		if len(got) != tc.findings || c.transportFiles != tc.transport || c.viaTransport != tc.via ||
			c.mentionOnly != tc.other {
			t.Errorf("%s: находок %d, набирающих %d, отнесено к слушателю %d, названо лишь текстом %d — "+
				"ждали %d, %d, %d, %d", tc.name, len(got), c.transportFiles, c.viaTransport, c.mentionOnly,
				tc.findings, tc.transport, tc.via, tc.other)
		}
	}
	if d := dials["c8.py"]; d.Unresolved != 1 {
		t.Errorf("(и): неразрешённых вызовов набора %d, ждали 1 — перепись не видит набора мимо распознавателя", d.Unresolved)
	}
}

// TestTransportOwnStatusesStillHaveASubject — освобождение по транспорту живёт,
// пока живы транспорт, его производитель и хоть один файл, его набирающий В
// КОДЕ: упоминание в описании или комментарии освобождение не держит.
func TestTransportOwnStatusesStillHaveASubject(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	shards, err := os.ReadFile(filepath.Join(root, "deploy", "e2e-shards.json"))
	if err != nil {
		t.Fatalf("объявление транспортов не читается: %v", err)
	}
	var declared struct {
		OptionalTransports map[string]json.RawMessage `json:"optional_transports"`
	}
	if err := json.Unmarshal(shards, &declared); err != nil {
		t.Fatalf("deploy/e2e-shards.json не разобран: %v", err)
	}
	// Состав — из индекса git, а не с диска: игнорируемое (распаковки, отчёты)
	// лежит на любой машине, где поднимали стенд.
	tracked, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".py")
	if err != nil {
		t.Fatalf("состав кейсов из индекса: %v", err)
	}
	bodies := map[string]string{}
	for _, f := range tracked {
		if !strings.Contains(filepath.ToSlash(f), "/tests/newman/cases/") {
			continue
		}
		b, err := os.ReadFile(f) // #nosec G304 -- путь из индекса git
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		bodies[f] = string(b)
	}
	if len(bodies) == 0 {
		t.Fatal("обход кейсов пуст — освобождение не с чем сверить")
	}
	dials := dialledTransports(t, bodies)
	for name, tr := range transportOwnStatuses {
		if _, ok := declared.OptionalTransports[name]; !ok {
			t.Errorf("%s: транспорта нет в optional_transports deploy/e2e-shards.json — "+
				"освобождение пережило свой предмет", name)
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tr.file)))
		found := false
		if err == nil {
			for _, raw := range strings.Split(string(body), "\n") {
				if strings.HasPrefix(strings.TrimSpace(raw), "//") {
					continue
				}
				if strings.Contains(raw, tr.literal) {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("%s: в %s нет исполняемого %s — слушатель больше не ретранслирует коды, "+
				"а освобождение продолжает их прощать", name, tr.file, tr.literal)
		}
		dialling, mentioning := 0, 0
		for f, d := range dials {
			if slices.Contains(d.Dials, name) {
				dialling++
			} else if strings.Contains(bodies[f], name) {
				mentioning++
			}
		}
		t.Logf("перепись: транспорт %s · кодов освобождено %d · файлов кейсов осмотрено %d, "+
			"набирают его в коде %d, называют лишь текстом %d",
			name, len(tr.statuses), len(bodies), dialling, mentioning)
		if dialling == 0 {
			t.Errorf("%s: ни один файл кейсов транспорт в коде не набирает (называют лишь текстом %d) — "+
				"освобождению нечего освобождать", name, mentioning)
		}
	}
}

// TestEdgeOwnStatusesStillHaveAProducer — освобождение живёт, пока живёт то, что
// оно освобождает.
//
// `muxOwnStatuses` перечисляет коды, которых `HTTPStatusFromCode` не отдаёт, и
// тем ОСВОБОЖДАЕТ пробы, которые их ждут. Для кодов, производимых библиотекой
// grpc-gateway (405, 415), предмет держится самой библиотекой. Для кода, который
// производит НАШ код, предмета в дереве может не стать — и тогда запись начнёт
// освобождать пробу, ждущую исхода, которого больше нет: ровно то освобождение,
// которому нечего исключать.
//
// Поэтому у каждой такой записи назван производитель координатой, и здесь
// проверяется, что он на месте. Утверждается ИСПОЛНЯЕМАЯ часть: литерал ищется
// в файле, а совпадение в комментарии предметом не считается, иначе шапка,
// объясняющая запрет, вечно доказывала бы его предпосылку.
func TestEdgeOwnStatusesStillHaveAProducer(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	checked := 0
	for code, src := range edgeStatusProducers {
		if _, declared := muxOwnStatuses[code]; !declared {
			t.Errorf("%d: производитель назван, но код НЕ объявлен в muxOwnStatuses — "+
				"запись доказывает то, чего никто не освобождает. Удали её", code)
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(src.file)))
		if err != nil {
			t.Errorf("%d: производитель %s не читается (%v) — запись muxOwnStatuses[%d] "+
				"пережила свой предмет. Либо верни координату, либо сними освобождение",
				code, src.file, err, code)
			continue
		}
		found := false
		for _, raw := range strings.Split(string(body), "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "//") {
				continue // комментарий — не производитель
			}
			if strings.Contains(stripJSLineComment(raw), src.literal) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%d: в %s нет исполняемого %s — код больше никто не производит, "+
				"а muxOwnStatuses[%d] продолжает освобождать пробы, которые его ждут. "+
				"Сними освобождение вместе с производителем",
				code, src.file, src.literal, code)
			continue
		}
		checked++
	}

	t.Logf("перепись: кодов в muxOwnStatuses %d, из них производимых нашим кодом %d "+
		"(производитель проверен у %d)", len(muxOwnStatuses), len(edgeStatusProducers), checked)

	if len(edgeStatusProducers) > 0 && checked == 0 {
		t.Fatal("ни у одной записи производитель не подтверждён — проверка предпосылки " +
			"сама осталась без предмета")
	}
}
