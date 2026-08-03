// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanprerequestguard_test.go — гейт формы стража окружения в newman-наборах:
// утверждение в PRE-request скрипте обязано быть спарено с ПРОПУСКОМ запроса.
//
// # Что здесь запрещено и почему
//
// Утверждение, стоящее в pre-request скрипте, по построению говорит одно: «того,
// что этому шагу нужно, харнесс не дал». Дальше есть ровно два исхода:
//
//	САНКЦИОНИРОВАННАЯ ФОРМА — утвердить (назвав переменную), затем
//	`pm.execution.skipRequest()`. Примитив пропускает РОВНО ОДИН запрос и не
//	исполняет его test-script, поэтому ни одно другое утверждение шага не
//	засчитывается; утверждение стража уже отработало, поэтому пропуск остаётся
//	ЗАПИСАННЫМ падением с именем переменной, а не немым. Эталон —
//	`services/iam/tests/newman/scripts/gen.py::require_env_url`.
//
//	ОТСТУПЛЕНИЕ — утвердить и ВСЁ РАВНО ОТПРАВИТЬ. Тогда шаг уезжает без того,
//	чего у него нет: без предъявителя — АНОНИМНО, то есть к ДРУГОМУ принципалу;
//	мимо внутреннего листенера — на публичный, где маршрута нет. Одно утверждение
//	падает, а все ОСТАЛЬНЫЕ утверждения шага исполняются и засчитываются — против
//	субъекта и ответа, которых кейс не называл. Типичное ожидание негатива
//	(401/403/404) при этом сходится само собой, и кейс проходит ПО НЕВЕРНОЙ
//	ПРИЧИНЕ: предмет, ради которого он написан, не проверяется вовсе.
//
// Форма не изобретена этим гейтом. Она уже объявлена правильной в самом дереве —
// `services/iam/tests/newman/scripts/exec-coverage.py` (раздел «STATIC BANS»):
// «an environment guard must FAIL, not merely skip. The sanctioned shape is
// gen.py::require_env_url — assert (naming the variable), then skip». Здесь она
// становится проверяемой: exec-coverage читает ОТЧЁТ прогона и ловит обратную
// половину (страж, который пропускает МОЛЧА), а страж, который утверждает и
// отправляет, отчёту неотличим от нормального шага — он исполнился и ответил.
//
// # Радиус, померенный по дереву, а не по диффу
//
// Механизм зовётся `_auth_pre_script` и живёт по одной копии в КАЖДОМ из восьми
// генераторов наборов (`gateway/` + семь `services/*/`); девятая копия — своя, в
// `services/storage/tests/newman/cases/sec-d.py::_internal_check_url`. Дифф,
// в котором отступление замечают, показывает одну из девяти.
//
// # Предикат читает КОД, а не текст
//
// Скобки встречаются внутри строковых литералов (`'предусловие: {{' + n + '}}'`),
// внутри регулярных выражений (`/\{\{[A-Za-z0-9_]+\}\}/g`) и в комментариях.
// Счёт сырых скобок дал бы неверную вложенность, а поиск слова `skipRequest`
// подошёл бы комментарию, который эту защиту ОБЪЯСНЯЕТ, — гейт зеленел бы на
// снятой защите. Поэтому текст сначала приводится к скелету кода
// (`jsCodeSkeleton`), и все решения принимаются по нему.
//
// Предмет гейта — СГЕНЕРИРОВАННЫЕ коллекции, а не питоновские исходники: именно
// коллекции исполняет newman. Генератор, отступивший от формы, виден здесь через
// свой продукт, и обойти гейт правкой мимо генератора нельзя.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─── разбор ──────────────────────────────────────────────────────────────────

// preAssertion — одно утверждение pre-request скрипта с координатой.
type preAssertion struct {
	collection string // путь от корня репозитория
	item       string // имя шага в коллекции
	line       int    // строка внутри pre-request скрипта, 1-based
	name       string // имя утверждения, если оно записано литералом
	depth      int    // вложенность в блоках кода на момент утверждения
	hasSkip    bool   // в ОБЪЕМЛЮЩЕМ блоке есть pm.execution.skipRequest()
}

func (a preAssertion) String() string {
	nm := a.name
	if nm == "" {
		nm = "<имя не литерал>"
	}
	return fmt.Sprintf("%s :: %s : строка %d pre-request : %s", a.collection, a.item, a.line, nm)
}

// guardCensus — ОБЪЁМ ОСМОТРЕННОГО. Без него «ноль находок» и «ноль прочитанного»
// читаются одинаково, а обход, переставший доходить до коллекций, выходит зелёным.
//
// Скрипты считаются ПО ЯРУСАМ, и это не украшение переписи. Пока ярус не назван
// отдельным числом, слепая зона по целому ярусу читается как «чисто»: первая
// редакция этого гейта обходила только шаги, а pre-request КОРНЯ коллекции —
// скрипт, который newman исполняет перед КАЖДЫМ запросом — не читала вовсе.
// Гейт печатал «утверждений 4139, из них в санкционированной форме 4139», то
// есть ноль находок, тогда как в корнях лежало 13 утверждений ровно того класса,
// который он запрещает. Число по ярусу делает такую зону видимой: ярус, у
// которого скриптов ноль, теперь виден как ноль.
type guardCensus struct {
	collections  int
	items        int
	scripts      int // ВСЕГО узлов с непустым pre-request скриптом
	rootScripts  int // из них — корень коллекции (исполняется перед КАЖДЫМ запросом)
	folderScript int // из них — папка (исполняется перед каждым запросом папки)
	itemScripts  int // из них — сам шаг
	assertions   int
	sanctioned   int
	unparsed     []string
}

func (c guardCensus) String() string {
	return fmt.Sprintf("осмотрено коллекций %d, шагов %d, pre-request скриптов %d "+
		"(корень коллекции %d, папка %d, шаг %d); утверждений в них %d, "+
		"из них в санкционированной форме %d",
		c.collections, c.items, c.scripts, c.rootScripts, c.folderScript, c.itemScripts,
		c.assertions, c.sanctioned)
}

var (
	pmTestRe = regexp.MustCompile(`pm\.test\s*\(`)
	pmSkipRe = regexp.MustCompile(`pm\.execution\.skipRequest\s*\(`)
	// имя утверждения — первый строковый литерал сразу за `pm.test(`.
	pmTestNameRe = regexp.MustCompile(`^pm\.test\s*\(\s*(['"` + "`" + `])((?:[^'"` + "`" + `\\]|\\.)*)['"` + "`" + `]`)
)

// analyzeNewmanPrerequestGuards — ЕДИНСТВЕННОЕ место, где вычисляются находки
// этого гейта. Возвращает ВСЕ утверждения pre-request скриптов (и санкционированные,
// и отступающие), чтобы перепись и находки считались ОДНИМ предикатом.
func analyzeNewmanPrerequestGuards(t *testing.T, tt *trackedTree) ([]preAssertion, guardCensus) {
	t.Helper()

	var rels []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	var all []preAssertion
	census := guardCensus{}
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(rel)))
		if err != nil {
			census.unparsed = append(census.unparsed, rel+": "+err.Error())
			continue
		}
		var col struct {
			Item  json.RawMessage `json:"item"`
			Event newmanEvents    `json:"event"`
		}
		if err := json.Unmarshal(b, &col); err != nil {
			census.unparsed = append(census.unparsed, rel+": "+err.Error())
			continue
		}
		census.collections++

		// scan — один и тот же предикат на любом ярусе. Ярус влияет ТОЛЬКО на
		// координату и на счётчик переписи, но не на то, что считается
		// отступлением: newman исполняет pre-request корня и папки перед каждым
		// запросом в их области, поэтому утверждение без пропуска отправляет шаг
		// ровно так же — только сразу во многих шагах, а не в одном.
		scan := func(src, site string) {
			if strings.TrimSpace(src) == "" {
				return
			}
			census.scripts++
			for _, a := range scanPreRequestAssertions(src) {
				a.collection = rel
				a.item = site
				census.assertions++
				if a.hasSkip && a.depth > 0 {
					census.sanctioned++
				}
				all = append(all, a)
			}
		}

		// ЯРУС 1 — корень коллекции. Читается ПЕРВЫМ намеренно: именно этот ярус
		// был слепой зоной, и именно он самый широкий по радиусу.
		if src := col.Event.preRequest(); strings.TrimSpace(src) != "" {
			census.rootScripts++
			scan(src, "<корень коллекции — исполняется перед КАЖДЫМ запросом>")
		}

		nodes, err := flattenNewmanItems(col.Item)
		if err != nil {
			census.unparsed = append(census.unparsed, rel+": "+err.Error())
			continue
		}
		for _, n := range nodes {
			src := n.Event.preRequest()
			if n.isFolder {
				// ЯРУС 2 — папка.
				if strings.TrimSpace(src) != "" {
					census.folderScript++
					scan(src, "<папка "+n.Name+" — перед каждым запросом папки>")
				}
				continue
			}
			// ЯРУС 3 — шаг.
			census.items++
			if strings.TrimSpace(src) != "" {
				census.itemScripts++
			}
			scan(src, n.Name)
		}
	}
	return all, census
}

// newmanEvents — события узла коллекции. ОДИН тип на все три яруса (корень,
// папка, шаг): в схеме Postman они описаны одинаково, и разбирать их разными
// путями значило бы завести ярус, умеющий отстать от остальных.
type newmanEvents []struct {
	Listen string `json:"listen"`
	Script struct {
		// newman допускает и массив строк, и одну строку.
		Exec json.RawMessage `json:"exec"`
	} `json:"script"`
}

// preRequest — СКЛЕЙКА всех pre-request скриптов узла, а не первый из них.
//
// Прежняя редакция возвращала первый и молча роняла остальные. newman исполняет
// ВСЕ события `prerequest` узла, поэтому отступление во втором было бы невидимо.
// Узлов с двумя такими событиями в дереве сегодня нет — но «сегодня нет» есть
// свойство дерева, а не свойство разбора, и разбор, опирающийся на то, чего пока
// не написали, воспроизводит форму дерева вместо формы запрета.
func (ev newmanEvents) preRequest() string {
	var parts []string
	for _, e := range ev {
		if e.Listen != "prerequest" || len(e.Script.Exec) == 0 {
			continue
		}
		var lines []string
		if err := json.Unmarshal(e.Script.Exec, &lines); err == nil {
			parts = append(parts, strings.Join(lines, "\n"))
			continue
		}
		var one string
		if err := json.Unmarshal(e.Script.Exec, &one); err == nil {
			parts = append(parts, one)
		}
	}
	return strings.Join(parts, "\n")
}

// newmanItem — узел коллекции: шаг ЛИБО папка, со своими скриптами.
type newmanItem struct {
	Name     string `json:"name"`
	Item     json.RawMessage
	Event    newmanEvents `json:"event"`
	isFolder bool
}

// flattenNewmanItems — рекурсивный обход коллекции. Возвращает и листья (шаги),
// и ПАПКИ: у папки бывает собственный pre-request скрипт, исполняемый перед
// каждым запросом внутри неё, и пропуск папки завёл бы слепой ярус.
func flattenNewmanItems(raw json.RawMessage) ([]newmanItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var nodes []newmanItem
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	var out []newmanItem
	for _, n := range nodes {
		if len(n.Item) > 0 {
			n.isFolder = true
			out = append(out, n)
			kids, err := flattenNewmanItems(n.Item)
			if err != nil {
				return nil, err
			}
			out = append(out, kids...)
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// scanPreRequestAssertions — утверждения одного pre-request скрипта.
//
// Объемлющий блок утверждения — от самого утверждения до места, где вложенность
// падает НИЖЕ той, на которой утверждение стоит. Пропуск засчитывается только
// внутри этого окна: `skipRequest()`, стоящий в ДРУГОЙ ветке ниже по тексту,
// к этому утверждению отношения не имеет.
func scanPreRequestAssertions(src string) []preAssertion {
	code := jsCodeSkeleton(src)

	// вложенность на КАЖДОМ байте (по скелету, т.е. только по коду)
	depth := make([]int, len(code)+1)
	d := 0
	for i := 0; i < len(code); i++ {
		depth[i] = d
		switch code[i] {
		case '{':
			d++
		case '}':
			d--
		}
	}
	depth[len(code)] = d

	lineOf := func(off int) int { return 1 + strings.Count(src[:off], "\n") }

	var out []preAssertion
	for _, m := range pmTestRe.FindAllStringIndex(code, -1) {
		start := m[0]
		d0 := depth[start]
		end := len(code)
		for i := start; i < len(code); i++ {
			if depth[i] < d0 {
				end = i
				break
			}
		}
		a := preAssertion{
			line:    lineOf(start),
			depth:   d0,
			hasSkip: pmSkipRe.MatchString(code[start:end]),
		}
		if nm := pmTestNameRe.FindStringSubmatch(src[start:]); nm != nil {
			a.name = nm[2]
		}
		out = append(out, a)
	}
	return out
}

// jsCodeSkeleton — тот же текст той же длины, где СОДЕРЖИМОЕ строковых литералов,
// шаблонных строк, регулярных выражений и комментариев заменено пробелами.
//
// Зачем именно так, а не «вырезать»: длина и переводы строк сохраняются, поэтому
// смещения скелета совпадают со смещениями исходника и координата находки остаётся
// настоящей. Кавычки и косые черты сами по себе оставлены — скобками они не
// являются и вложенность не меняют.
func jsCodeSkeleton(src string) string {
	out := []byte(src)
	n := len(src)
	blank := func(from, to int) {
		if to > n {
			to = n
		}
		for k := from; k < to; k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	prev := byte(0) // последний значащий символ кода — им решается, регулярное ли это выражение
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j

		case c == '/' && i+1 < n && src[i+1] == '*':
			j := i + 2
			for j+1 < n && !(src[j] == '*' && src[j+1] == '/') {
				j++
			}
			blank(i, j+2)
			i = j + 2

		case c == '\'' || c == '"' || c == '`':
			q := c
			j := i + 1
			for j < n {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == q || (q != '`' && src[j] == '\n') {
					break
				}
				j++
			}
			blank(i+1, j)
			prev = q
			i = j + 1

		case c == '/' && regexLiteralCanStart(prev):
			j := i + 1
			inClass := false
			closed := false
			for j < n {
				if src[j] == '\\' {
					j += 2
					continue
				}
				switch src[j] {
				case '[':
					inClass = true
				case ']':
					inClass = false
				case '/':
					if !inClass {
						closed = true
					}
				case '\n':
					j = n
				}
				if closed {
					break
				}
				j++
			}
			if !closed { // не регулярное выражение, а просто деление — ничего не гасим
				prev = '/'
				i++
				continue
			}
			blank(i+1, j)
			prev = '/'
			i = j + 1

		default:
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				prev = c
			}
			i++
		}
	}
	return string(out)
}

// regexLiteralCanStart — после чего `/` начинает регулярное выражение, а не деление.
func regexLiteralCanStart(prev byte) bool {
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', '}', ';', '+', '-', '*', '%', '~', '^', '<', '>':
		return true
	}
	return false
}

// ─── ГЕЙТ ────────────────────────────────────────────────────────────────────

// TestPreRequestAssertionIsPairedWithSkip — утверждение pre-request скрипта обязано
// быть спарено с пропуском запроса.
//
// Что делать, если гейт сработал, — ровно два исхода, третьего нет:
//
//  1. это СТРАЖ (шагу не дали того, что ему нужно) → добавить
//     `pm.execution.skipRequest()` в ту же ветку, сразу за утверждением. Тогда шаг
//     не отправляется, ни одно его утверждение не засчитывается, а пропуск остаётся
//     записанным падением с именем переменной;
//  2. это НЕ страж, а обычная проверка → её место в test-script, где она
//     оценивается вместе с ответом. В pre-request она либо тождественно верна, либо
//     падает на каждом прогоне.
//
// «Пока оставим как есть» исходом не является: пока утверждение стоит в pre-request
// без пропуска, шаг продолжает уезжать без того, чего у него нет.
func TestPreRequestAssertionIsPairedWithSkip(t *testing.T) {
	root := repoRoot(t)
	all, census := analyzeNewmanPrerequestGuards(t, newTrackedTree(t, root))

	// ПЕРЕПИСЬ печатается ВСЕГДА, включая зелёный прогон.
	t.Log(census.String())

	if len(census.unparsed) > 0 {
		t.Errorf("не разобрано %d коллекц(ия|ий) — по ним у гейта НЕТ ответа, и молчание "+
			"про них неотличимо от «форма соблюдена»:\n  %s",
			len(census.unparsed), strings.Join(census.unparsed, "\n  "))
	}
	if census.collections == 0 {
		t.Fatal("не прочитано НИ ОДНОЙ коллекции newman — это слепой обход, а не «чисто». " +
			"Гейт на пустом множестве зелен всегда: почини отбор файлов " +
			"(*/tests/newman/collections/*.json в индексе git).")
	}
	if census.assertions == 0 {
		t.Fatal("в pre-request скриптах не найдено НИ ОДНОГО утверждения — предпосылка гейта " +
			"не выполнена: он ищет форму, которой в дереве нет вовсе, и его зелёный ничего " +
			"не значит. Проверь разбор коллекций (event[listen=prerequest].script.exec).")
	}
	if census.sanctioned == 0 {
		t.Fatal("НИ ОДНО утверждение pre-request не в санкционированной форме — либо дерево " +
			"целиком отступило, либо предикат перестал узнавать пропуск " +
			"(pm.execution.skipRequest). Молчание такого гейта было бы бессмысленным.")
	}

	var problems []string
	for _, a := range all {
		switch {
		case a.depth == 0:
			problems = append(problems, a.String()+
				"  ← утверждение вне какой-либо ветки: оно исполняется на КАЖДОМ прогоне, "+
				"а запрос уходит в любом случае")
		case !a.hasSkip:
			problems = append(problems, a.String()+
				"  ← утверждение есть, pm.execution.skipRequest() в той же ветке НЕТ: шаг всё "+
				"равно отправляется")
		}
	}
	sort.Strings(problems)

	if len(problems) > 0 {
		t.Errorf("%d утвержден(ие|ий) pre-request отступают от санкционированной формы "+
			"«утвердить (назвав переменную), затем pm.execution.skipRequest()». Шаг уезжает "+
			"без того, чего у него нет — без предъявителя АНОНИМНО, то есть к ДРУГОМУ "+
			"принципалу, — падает одно утверждение стража, а все ОСТАЛЬНЫЕ утверждения шага "+
			"засчитываются против субъекта, которого кейс не называл:\n  %s\n\n"+
			"Форма объявлена правильной самим деревом: exec-coverage.py, раздел STATIC BANS, "+
			"эталон — gen.py::require_env_url. Правится в ГЕНЕРАТОРЕ набора "+
			"(tests/newman/scripts/gen.py) с последующей регенерацией коллекций, а не в "+
			"коллекции руками.",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// ─── ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ ──────────────────────────────────────────────────
//
// Гейт без этой пары ловит форму, а не существо: молчание на законной конструкции
// той же формы надо доказать так же, как срабатывание на дефекте. Источники
// синтетические — исход не зависит от состояния дерева, поэтому пара остаётся
// доказательной и после того, как дерево починено.

// Отступление ровно в той форме, в какой оно жило в дереве до 2026-08-04:
// утвердить, снять заголовок и ОТПРАВИТЬ.
const injectedDeviantGuard = `// per-step auth: bearer from env 'jwtStranger'
const __t = pm.environment.get('jwtStranger') || pm.variables.get('jwtStranger') || '';
if (__t) {
  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});
} else {
  pm.test('harness config: jwtStranger is set (subject under test)', () => {
    pm.expect.fail('jwtStranger is not set');
  });
  pm.request.headers.remove('Authorization');
}`

// TestPreRequestGuardGateRedOnInjectedDefect — возвращённый дефект краснит гейт И
// называет координату.
func TestPreRequestGuardGateRedOnInjectedDefect(t *testing.T) {
	got := scanPreRequestAssertions(injectedDeviantGuard)
	if len(got) != 1 {
		t.Fatalf("утверждение не распознано: %+v", got)
	}
	if got[0].hasSkip {
		t.Fatal("гейт принял отступление за санкционированную форму: пропуска в ветке нет, " +
			"а предикат утверждает обратное")
	}
	if got[0].line != 6 {
		t.Errorf("гейт обязан назвать координату: ожидалась строка 6, получена %d", got[0].line)
	}
	if got[0].name != "harness config: jwtStranger is set (subject under test)" {
		t.Errorf("гейт обязан назвать утверждение (а через него — переменную): %q", got[0].name)
	}
	if got[0].depth != 1 {
		t.Errorf("вложенность прочитана неверно: %d", got[0].depth)
	}
}

// TestPreRequestGuardGateSilentOnLawfulSameShape — законные конструкции ТОЙ ЖЕ формы
// гейт не задевает. Все четыре встречаются в дереве.
func TestPreRequestGuardGateSilentOnLawfulSameShape(t *testing.T) {
	lawful := map[string]string{
		"страж предъявителя в санкционированной форме": `const __t = pm.environment.get('jwtStranger') || '';
if (__t) {
  pm.request.headers.upsert({key: 'Authorization', value: 'Bearer ' + __t});
} else {
  pm.test('harness config: jwtStranger is set (subject under test)', () => {
    pm.expect.fail('jwtStranger is not set');
  });
  pm.execution.skipRequest();
}`,
		"страж адреса (require_env_url)": `const __cfgUrl = pm.environment.get('internalBaseUrl') || '';
if (__cfgUrl) {
  pm.request.url = __cfgUrl + pm.variables.replaceIn('/iam/v1/internal/cluster');
} else {
  pm.test('harness config: internalBaseUrl is set — Internal* RPC', () => {
    pm.expect.fail('internalBaseUrl is not set');
  });
  pm.execution.skipRequest();
}`,
		"страж операции": `if (!pm.environment.get('opId')) {
  pm.test('operation id opId was captured (the mutation returned an Operation)', () => {
    pm.expect.fail('opId is empty');
  });
  pm.execution.skipRequest();
}`,
		"страж предусловия в IIFE — со скобками ВНУТРИ регулярного выражения": `(function () {
  var _u = '';
  try { _u = pm.request.url.toString(); } catch (e) { return; }
  var _all = _u.match(/\{\{[A-Za-z0-9_]+\}\}/g);
  if (!_all) { return; }
  var _n = null;
  for (var _i = 0; _i < _all.length; _i++) {
    var _c = _all[_i].slice(2, -2);
    if (!pm.variables.has(_c)) { _n = _c; break; }
  }
  if (_n === null) { return; }
  pm.test('предусловие: {{' + _n + '}} не было захвачено — запрос не отправлен', function () {
    pm.expect.fail(_n + ' не определена ни в одной области');
  });
  pm.execution.skipRequest();
})();`,
	}

	for name, src := range lawful {
		t.Run(name, func(t *testing.T) {
			got := scanPreRequestAssertions(src)
			if len(got) != 1 {
				t.Fatalf("утверждение не распознано (или распознано лишнее): %+v", got)
			}
			if got[0].depth == 0 {
				t.Fatalf("законный страж прочитан как безусловный — предикат неверно считает "+
					"вложенность: %+v", got[0])
			}
			if !got[0].hasSkip {
				t.Fatalf("гейт сработал на законной конструкции той же формы — он ловит форму, "+
					"а не существо: %+v", got[0])
			}
		})
	}
}

// TestPreRequestGuardGateReadsCodeNotText — предикат читает КОД, а не текст.
//
// Три ловушки, каждая уже ломала гейты этого дерева: защита, названная в
// КОММЕНТАРИИ, который её объясняет; та же строка внутри СТРОКОВОГО ЛИТЕРАЛА; и
// скобки внутри литерала, сбивающие счёт вложенности.
func TestPreRequestGuardGateReadsCodeNotText(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantHit bool // true — гейт обязан считать это отступлением
	}{
		{
			name: "пропуск назван в комментарии, который его объясняет",
			src: `if (!pm.environment.get('jwtStranger')) {
  pm.test('harness config: jwtStranger is set', () => { pm.expect.fail('нет'); });
  // здесь должен стоять pm.execution.skipRequest(), иначе шаг уедет анонимно
  pm.request.headers.remove('Authorization');
}`,
			wantHit: true,
		},
		{
			name: "пропуск назван внутри строкового литерала",
			src: `if (!pm.environment.get('jwtStranger')) {
  pm.test('harness config: jwtStranger is set', () => {
    pm.expect.fail('починка: добавить pm.execution.skipRequest() в эту ветку');
  });
  pm.request.headers.remove('Authorization');
}`,
			wantHit: true,
		},
		{
			name: "пропуск назван в блочном комментарии",
			src: `if (!pm.environment.get('jwtStranger')) {
  pm.test('harness config: jwtStranger is set', () => { pm.expect.fail('нет'); });
  /* pm.execution.skipRequest(); — было снято, чтобы посмотреть ответ */
  pm.request.headers.remove('Authorization');
}`,
			wantHit: true,
		},
		{
			name: "скобки внутри литерала не сбивают вложенность законного стража",
			src: `if (!pm.environment.get('jwtStranger')) {
  pm.test('нет {{jwtStranger}} — } { ', () => { pm.expect.fail('} { }'); });
  pm.execution.skipRequest();
}`,
			wantHit: false,
		},
		{
			name: "пропуск в ДРУГОЙ ветке ниже по тексту не засчитывается",
			src: `if (!pm.environment.get('jwtStranger')) {
  pm.test('harness config: jwtStranger is set', () => { pm.expect.fail('нет'); });
  pm.request.headers.remove('Authorization');
}
if (!pm.environment.get('opId')) {
  pm.execution.skipRequest();
}`,
			wantHit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scanPreRequestAssertions(tc.src)
			if len(got) != 1 {
				t.Fatalf("утверждение не распознано (или распознано лишнее): %+v", got)
			}
			hit := got[0].depth == 0 || !got[0].hasSkip
			if hit != tc.wantHit {
				t.Fatalf("гейт %s: депth=%d hasSkip=%v", map[bool]string{
					true:  "промолчал там, где обязан краснеть",
					false: "сработал на законной конструкции",
				}[tc.wantHit], got[0].depth, got[0].hasSkip)
			}
		})
	}
}

// TestPreRequestGuardGateFlagsUnconditionalAssertion — утверждение вне какой-либо
// ветки. Оно исполняется на каждом прогоне и запрос не останавливает.
//
// Ровно в этой форме жил девятый экземпляр класса
// (`services/storage/tests/newman/cases/sec-d.py::_internal_check_url`): утвердить
// безусловно, а переадресовать — только если значение есть. При пустом значении шаг
// уезжал на ПУБЛИЧНЫЙ листенер, где внутреннего маршрута нет и быть не должно.
func TestPreRequestGuardGateFlagsUnconditionalAssertion(t *testing.T) {
	const src = `const intBase = pm.environment.get('internalBaseUrl') || '';
pm.test('harness config: internalBaseUrl is set (internal Check probe target)', () => pm.expect(intBase, 'пусто').to.not.equal(''));
if (intBase) { pm.request.url = intBase + '/iam/v1/internal/iam:check'; }`

	got := scanPreRequestAssertions(src)
	if len(got) != 1 {
		t.Fatalf("утверждение не распознано: %+v", got)
	}
	if got[0].depth != 0 {
		t.Fatalf("безусловное утверждение прочитано как условное (вложенность %d) — тогда "+
			"гейт зеленел бы на нём, если ниже по скрипту найдётся чужой пропуск", got[0].depth)
	}
	if got[0].line != 2 {
		t.Errorf("гейт обязан назвать координату: ожидалась строка 2, получена %d", got[0].line)
	}
}

// TestPreRequestGuardAnalyzerSeesEveryCollection — предпосылка гейта выше.
//
// Обход, переставший доходить до коллекций (каталог переименовали, поле скрипта
// назвали иначе), выходит ЗЕЛЁНЫМ на пустом множестве — ровно тот класс, который
// здесь искореняют. Поэтому разобранное множество сверяется с индексом git:
// каждая коллекция, лежащая в дереве, обязана быть прочитана.
func TestPreRequestGuardAnalyzerSeesEveryCollection(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	want := 0
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			want++
		}
	}
	_, census := analyzeNewmanPrerequestGuards(t, tt)
	t.Logf("%s; коллекций в индексе git %d", census.String(), want)

	if want == 0 {
		t.Fatal("в индексе git нет НИ ОДНОЙ коллекции newman — сверять не с чем, " +
			"и зелёный гейта выше ничего не значит")
	}
	if census.collections != want {
		t.Errorf("прочитано %d коллекций из %d, лежащих в индексе — разница есть слепая зона "+
			"гейта, а не «чисто»", census.collections, want)
	}

	// СЧЁТА ФАЙЛОВ НЕДОСТАТОЧНО, и это не теория.
	//
	// Первая редакция этой самопроверки сверяла ровно одно число — сколько файлов
	// открыто, — и была зелёной, пока обход не читал ЦЕЛЫЙ ЯРУС: pre-request корня
	// коллекции (скрипт, исполняемый newman перед КАЖДЫМ запросом) не разбирался
	// вовсе. Все 82 файла «прочитаны», перепись объявляла ноль находок, а в корнях
	// лежало 13 утверждений ровно того класса, который гейт запрещает. Проверка
	// формы без содержания: открыть файл и осмотреть его — разные утверждения.
	//
	// Поэтому ярус подтверждается СВОИМ числом. Ярус, переставший разбираться,
	// обнуляет его, и это видно, а не читается как «чисто».
	if census.rootScripts == 0 {
		t.Error("НИ ОДИН pre-request корня коллекции не прочитан. Либо разбор перестал " +
			"доходить до поля event самой коллекции, либо корневые скрипты исчезли из дерева. " +
			"В первом случае гейт слеп к ярусу, который исполняется перед КАЖДЫМ запросом, " +
			"и его зелёный ничего не значит; во втором — предпосылка изменилась и эту проверку " +
			"надо пересмотреть осознанно, а не снять.")
	}
	if census.itemScripts == 0 {
		t.Error("НИ ОДИН pre-request шага не прочитан — обход перестал доходить до листьев " +
			"коллекции, и «ноль находок» здесь означает «ноль прочитанного»")
	}
	if census.rootScripts+census.folderScript+census.itemScripts != census.scripts {
		t.Errorf("ярусы не сходятся с общим счётом (%d+%d+%d != %d) — значит есть скрипты, "+
			"попавшие в общий счёт мимо ярусов, и перепись перестала быть переписью",
			census.rootScripts, census.folderScript, census.itemScripts, census.scripts)
	}
}

// TestPreRequestGuardCoversEveryTier — предикат ОДИН на все три яруса.
//
// Ярус корня и ярус папки исполняются перед КАЖДЫМ запросом своей области,
// поэтому отступление там шире по радиусу, чем в отдельном шаге, — а разбор их
// когда-то не читал вовсе. Здесь синтетическая коллекция несёт один и тот же
// дефект на каждом ярусе, и гейт обязан найти ВСЕ три: ярус, выпавший из обхода,
// уменьшит это число, и тест назовёт, какой именно.
func TestPreRequestGuardCoversEveryTier(t *testing.T) {
	const deviant = `if (!pm.environment.get('jwtStranger')) {
  pm.test('harness config: jwtStranger is set', () => pm.expect.fail('нет'));
}`
	const lawful = `if (!pm.environment.get('opId')) {
  pm.test('operation id opId was captured', () => pm.expect.fail('opId is empty'));
  pm.execution.skipRequest();
}`
	ev := func(src string) json.RawMessage {
		b, err := json.Marshal([]map[string]any{{
			"listen": "prerequest",
			"script": map[string]any{"exec": strings.Split(src, "\n")},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	for _, tier := range []string{"корень коллекции", "папка", "шаг"} {
		t.Run(tier, func(t *testing.T) {
			var col struct {
				Item  json.RawMessage `json:"item"`
				Event newmanEvents    `json:"event"`
			}
			leafSrc, folderSrc, rootSrc := lawful, lawful, lawful
			switch tier {
			case "корень коллекции":
				rootSrc = deviant
			case "папка":
				folderSrc = deviant
			case "шаг":
				leafSrc = deviant
			}
			leaf, _ := json.Marshal([]map[string]any{{"name": "шаг", "event": json.RawMessage(ev(leafSrc))}})
			folder, _ := json.Marshal([]map[string]any{{
				"name": "папка", "item": json.RawMessage(leaf), "event": json.RawMessage(ev(folderSrc)),
			}})
			if err := json.Unmarshal(ev(rootSrc), &col.Event); err != nil {
				t.Fatal(err)
			}
			col.Item = folder

			// тот же разбор, что и у гейта, но на синтетическом дереве
			var found int
			var where string
			check := func(src, site string) {
				for _, a := range scanPreRequestAssertions(src) {
					if a.depth == 0 || !a.hasSkip {
						found++
						where = site
					}
				}
			}
			check(col.Event.preRequest(), "корень коллекции")
			nodes, err := flattenNewmanItems(col.Item)
			if err != nil {
				t.Fatal(err)
			}
			var sawFolder bool
			for _, n := range nodes {
				if n.isFolder {
					sawFolder = true
					check(n.Event.preRequest(), "папка")
					continue
				}
				check(n.Event.preRequest(), "шаг")
			}
			if !sawFolder {
				t.Fatal("обход не вернул ни одной ПАПКИ — ярус папки выпал из разбора, " +
					"и дефект в скрипте папки был бы невидим")
			}
			if found != 1 {
				t.Fatalf("на ярусе %q дефект не найден (находок %d) — этот ярус гейт не читает",
					tier, found)
			}
			if where != tier {
				t.Fatalf("дефект внесён на ярус %q, а найден на %q — координата неверна", tier, where)
			}
		})
	}
}
