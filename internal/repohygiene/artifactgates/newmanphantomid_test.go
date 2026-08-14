// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanphantomid_test.go — гейт по дереву: шаг, ПУБЛИКУЮЩИЙ идентификатор ресурса
// из метаданных операции, обязан быть опрошен с НАЗЫВАНИЕМ ИСХОДА этой операции.
//
// # Предмет
//
// Операция Kachō несёт предвыделенный идентификатор ресурса в `metadata` ДАЖЕ когда
// завершилась ошибкой: идентификатор чеканится до того, как отработает асинхронный
// воркер. Шаг, сохраняющий `metadata.<res>Id` в окружение, и опрос, утверждающий
// только `done`, вместе публикуют координату ресурса, которого НЕТ: `done` у
// провалившейся операции такой же `true`, как у успешной.
//
// Дальше по этому идентификатору идут следующие шаги кейса — привязки прав (край
// отвечает успехом, потому что запись каталога резолвится синтаксически), затем
// первый же межсервисный запрос к владельцу отвечает «не найдено». Падает не тот
// шаг, который ошибся, и симптом (отказ авторизации, «не найдено» у соседа) к
// причине (несостоявшееся создание) отношения не имеет. Один такой разбор стоил
// полутора часов и был объявлен проблемой совсем другого механизма.
//
// # Что здесь считается защитой
//
// ИСХОД, названный хоть одним опросом цепочки: исполняемое `pm.expect(...)`,
// аргумент которого читает поле `error` операции (`j.error`, `_do.error`,
// `lastOpError`). Утверждение `done` защитой НЕ является — его удовлетворяет и
// провалившаяся операция; ровно поэтому класс и был невидим.
//
// Вопрос задаётся ОДИН НА ЦЕПОЧКУ, а не каждому опросу: у одной мутации опросов
// бывает несколько, и если исход называет любой из них — назван. Тот же порядок
// уже принят соседним проходом по половине удаления
// (`_assert_delete_operation_outcome` в каждом генераторе), и гейт обязан считать
// цепочки ТЕМ ЖЕ правилом, что генератор, иначе они разойдутся на первом же кейсе
// с двумя опросами.
//
// # Предмет гейта — СГЕНЕРИРОВАННЫЕ коллекции, а не питоновские исходники
//
// Исполняет newman именно коллекции. Генератор, отступивший от формы, виден здесь
// через свой продукт, и обойти гейт правкой мимо генератора нельзя. Тот же выбор и
// по той же причине сделан в `newmanprerequestguard_test.go`.
//
// # Предикат читает ИСПОЛНЯЕМУЮ часть, а не текст
//
// Слово `error` встречается в комментарии, ОБЪЯСНЯЮЩЕМ эту самую защиту, — а такие
// комментарии проход и дописывает. Поиск по сырому тексту зеленел бы на снятой
// защите тем сильнее, чем лучше она описана. Поэтому обе проекции скрипта строит
// один разбор (`jsBlank`): решение о публикации принимается по скелету, где
// содержимое строк погашено (иначе `pm.test('has metadata', …)` сошло бы за
// публикацию), а имена переменных читаются из проекции, где строки целы.
// Комментарии погашены в обеих.
//
// Признание исхода намеренно сделано по проекции СО СТРОКАМИ — той же, что у
// `_strip_js_comments` в генераторах: единственная запись исхода в дереве, которую
// скелет стирает, — `pm.environment.get('lastOpError')`, и по скелету гейт отверг
// бы настоящее утверждение. Строгость выше генераторской здесь не нужна и вредна:
// гейт и проход обязаны считать одно и то же.
//
// # Чего гейт НЕ требует
//
// Он не требует СНЯТИЯ опубликованного имени на ошибке — хотя проход дописывает
// его в обеих ветках (и к опросу, и к синхронной мутации, назвавшей исход у себя:
// newman не прекращает кейс на упавшем утверждении, поэтому без снятия фантом всё
// равно уехал бы дальше). Причина, по которой снятие не ТРЕБУЕТСЯ: чтобы оно было
// безопасным, пришлось бы отличать «ожидается успех» от «ожидается отказ», а это в
// дереве записано семью десятками разных выражений (`to.eql(undefined)`,
// `to.not.exist`, `to.be.an('object')`, `to.be.oneOf([3, 9])`, сравнение кода,
// вхождение текста). Ошибка классификации сняла бы идентификатор у кейса, чей
// ПРЕДМЕТ — отказ, и сломала бы его. Невыдвинутое требование лучше требования,
// которое гейт не умеет проверить: названный исход и так роняет прогон в точке
// причины, а это и есть то, ради чего issue заведён.
//
// Рядом живёт УЖЕ СУЩЕСТВУЮЩИЙ и более узкий механизм — `assert_phantom_drop` в
// генераторе iam: он требует СНЯТИЯ (через реестр `_provisionalIds`) и ничего не
// говорит об утверждении исхода, поэтому в iam оставалось 22 цепочки этого класса.
// Они не соперники: один держит снятие в одном наборе, другой — названный исход во
// всех восьми.
package artifactgates

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

// ─── минимальная модель коллекции Postman ────────────────────────────────────

type nmScript struct {
	Exec []string `json:"exec"`
}

type nmEvent struct {
	Listen string   `json:"listen"`
	Script nmScript `json:"script"`
}

type nmRequest struct {
	Method string          `json:"method"`
	URL    json.RawMessage `json:"url"`
}

type nmItem struct {
	Name    string     `json:"name"`
	Item    []nmItem   `json:"item"`
	Request *nmRequest `json:"request"`
	Event   []nmEvent  `json:"event"`
}

type nmCollection struct {
	Item []nmItem `json:"item"`
}

func (it nmItem) isFolder() bool { return len(it.Item) > 0 || it.Request == nil }

func (it nmItem) method() string {
	if it.Request == nil {
		return ""
	}
	return strings.ToUpper(it.Request.Method)
}

// rawURL — адрес шага. В коллекциях он записан и строкой, и объектом с полем
// `raw`; читать надо обе формы, иначе половина дерева станет гейту невидима.
func (it nmItem) rawURL() string {
	if it.Request == nil || len(it.Request.URL) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(it.Request.URL, &s); err == nil {
		return s
	}
	var o struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(it.Request.URL, &o); err == nil {
		return o.Raw
	}
	return ""
}

func (it nmItem) testScript() string {
	for _, ev := range it.Event {
		if ev.Listen == "test" {
			return strings.Join(ev.Script.Exec, "\n")
		}
	}
	return ""
}

// ─── распознавание полос ─────────────────────────────────────────────────────

var (
	nmOpPollPath = regexp.MustCompile(`/operations/\{\{(\w+)\}\}`)
	nmBindRe     = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=`)
	nmEnvSetRe   = regexp.MustCompile(`pm\.environment\.set\(\s*'`)
	nmExpectRe   = regexp.MustCompile(`pm\.expect\(`)
)

var nmMutationMethods = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// nmIsPoll — шаг читает операцию: GET на `/operations/{{<имя>}}`. Отмена операции
// (`:cancel`) — мутация, а не опрос, и сюда не попадает: у неё другой метод.
func nmIsPoll(it nmItem) bool {
	return it.method() == "GET" && nmOpPollPath.MatchString(it.rawURL())
}

// nmBraceDepth — глубина фигурных скобок на каждом смещении скелета. По ней
// решается видимость объявления: объявление видно там, где его блок ещё не закрыт.
func nmBraceDepth(code string) []int {
	d := make([]int, len(code)+1)
	cur := 0
	for i := 0; i < len(code); i++ {
		d[i] = cur
		switch code[i] {
		case '{':
			cur++
		case '}':
			cur--
		}
	}
	d[len(code)] = cur
	return d
}

// nmStringLiteralAt — значение строкового литерала, начинающегося на `pos`
// (символ кавычки) в проекции, где содержимое строк цело.
func nmStringLiteralAt(lit string, pos int) (string, int, bool) {
	if pos >= len(lit) {
		return "", pos, false
	}
	q := lit[pos]
	if q != '\'' && q != '"' && q != '`' {
		return "", pos, false
	}
	var b strings.Builder
	for i := pos + 1; i < len(lit); i++ {
		if lit[i] == '\\' && i+1 < len(lit) {
			b.WriteByte(lit[i+1])
			i++
			continue
		}
		if lit[i] == q {
			return b.String(), i + 1, true
		}
		b.WriteByte(lit[i])
	}
	return "", pos, false
}

// nmArgTail — текст от `from` до скобки, закрывающей вызов, начатый до `from`.
func nmArgTail(code string, from int) string {
	depth := 1
	for i := from; i < len(code); i++ {
		switch code[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return code[from:i]
			}
		}
	}
	return code[from:]
}

type nmBinding struct {
	off, depth int
	name       string
	derived    bool
}

// nmPublishedResourceVars — имена переменных окружения, которые шаг выставляет
// ЗНАЧЕНИЕМ, происходящим из `metadata` операции.
//
// Одного вхождения слова `metadata` в скрипт мало: генераторы кладут в один и тот
// же шаг и захват идентификатора ОПЕРАЦИИ (`j.id`), и захват идентификатора
// РЕСУРСА (`j.metadata.<res>Id`), причём оба — через локальную `const v` в СВОЁМ
// блоке. Без учёта области видимости идентификатор операции сошёл бы за ресурсный,
// и гейт потребовал бы защиты там, где публиковать нечего.
//
// `opVar` — имя, которым цепочка адресует саму операцию (берётся из адреса опроса);
// оно исключается: это ручка операции, а не координата ресурса.
func nmPublishedResourceVars(src, opVar string) []string {
	return nmDerivedEnvSets(src, "metadata", func(name string) bool { return name == opVar })
}

// nmDerivedEnvSets — имена переменных окружения, которым шаг присваивает значение,
// ПРОИСХОДЯЩЕЕ от `seed`, с учётом области видимости промежуточных объявлений.
//
// Разбор один на обе надобности намеренно — так же, как в самих генераторах
// (`_js_code_and_literals`): два разборщика расходятся молча и расходятся ровно
// там, где расхождение не видно. `seed` — единственное, чем отличаются два
// вызывающих: `metadata` (координата ресурса, опубликованная из ответа операции —
// гейт фантомного идентификатора) и `pm.response` (любой захват из собственного
// ответа шага — гейт захвата без утверждения). `skip` исключает имена, которые
// вызывающий координатой не считает.
func nmDerivedEnvSets(src, seed string, skip func(name string) bool) []string {
	code := jsCodeSkeleton(src)
	lit := jsCodeKeepingStrings(src)
	depth := nmBraceDepth(code)

	visible := func(bs []nmBinding, at int, expr string) bool {
		for _, b := range bs {
			if b.off >= at || !b.derived {
				continue
			}
			closed := false
			for k := b.off; k < at; k++ {
				if depth[k] < b.depth {
					closed = true
					break
				}
			}
			if closed {
				continue
			}
			if regexp.MustCompile(`\b` + regexp.QuoteMeta(b.name) + `\b`).MatchString(expr) {
				return true
			}
		}
		return false
	}

	var bindings []nmBinding
	for _, m := range nmBindRe.FindAllStringSubmatchIndex(code, -1) {
		semi := strings.IndexByte(code[m[1]:], ';')
		if semi < 0 {
			semi = len(code) - m[1]
		}
		expr := code[m[1] : m[1]+semi]
		b := nmBinding{off: m[0], depth: depth[m[0]], name: code[m[2]:m[3]]}
		b.derived = strings.Contains(expr, seed) || visible(bindings, m[0], expr)
		bindings = append(bindings, b)
	}

	var out []string
	seen := map[string]bool{}
	for _, m := range nmEnvSetRe.FindAllStringIndex(code, -1) {
		name, after, ok := nmStringLiteralAt(lit, m[1]-1)
		if !ok || name == "" || (skip != nil && skip(name)) {
			continue
		}
		comma := strings.IndexByte(code[after:], ',')
		if comma < 0 {
			continue
		}
		expr := nmArgTail(code, after+comma+1)
		if !strings.Contains(expr, seed) && !visible(bindings, m[0], expr) {
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// nmStatesOperationOutcome — исход операции НАЗВАН: аргумент исполняемого
// `pm.expect(...)` читает поле ошибки. Признаётся ПОЛЕ, а не носитель и не форма:
// имя переменной у каждого поллера своё (`j`, `_do`, `op`, `o`), а записей исхода
// в дереве несколько десятков.
func nmStatesOperationOutcome(src string) bool {
	code := jsCodeKeepingStrings(src)
	for _, m := range nmExpectRe.FindAllStringIndex(code, -1) {
		arg := nmArgTail(code, m[1])
		if i := strings.IndexByte(arg, ';'); i >= 0 {
			arg = arg[:i]
		}
		if strings.Contains(arg, ".error") || strings.Contains(arg, "lastOpError") {
			return true
		}
	}
	return false
}

// ─── гейт ────────────────────────────────────────────────────────────────────

type nmFinding struct {
	collection string
	casePath   string
	step       string
	vars       []string
	reason     string
}

func (f nmFinding) String() string {
	return fmt.Sprintf("%s :: %s :: %s — публикует %s, %s",
		f.collection, f.casePath, f.step, strings.Join(f.vars, ","), f.reason)
}

func TestPublishedResourceIdIsGuardedByOperationOutcome(t *testing.T) {
	root := repoRoot(t)

	// Состав берётся из ИНДЕКСА git, а не обходом диска: под корнем лежат рабочие
	// копии агентов и распаковки отчётов, и вердикт по ним был бы свойством чужого
	// рабочего каталога, а не коммита (см. trackedtree_test.go).
	tt := newTrackedTree(t, root)
	var cols []string
	for rel := range tt.files {
		if strings.Contains(rel, "/tests/newman/collections/") && strings.HasSuffix(rel, ".json") {
			cols = append(cols, rel)
		}
	}
	sort.Strings(cols)

	findings, cen, err := auditPublishedIdOutcome(root, cols)
	if err != nil {
		t.Fatal(err)
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. «Ноль находок» обязано быть отличимо от
	// «ноль прочитанного»: гейт, у которого распознаватель полосы перестал что-либо
	// узнавать (переименование поля, смена формы адреса, перенос коллекций), молча
	// стал бы вечнозелёным.
	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 || cen.chains == 0 {
		t.Fatalf("прочитано коллекций %d, шагов %d, цепочек мутация→опрос %d — распознаватель полосы ничего не узнал",
			cen.collections, cen.steps, cen.chains)
	}
	if cen.publishing == 0 {
		t.Fatalf("в %d цепочках не найдено НИ ОДНОЙ публикации идентификатора из metadata — предикат публикации ослеп",
			cen.chains)
	}

	t.Logf("осмотрено: коллекций %d, шагов %d, цепочек мутация→опрос %d, из них публикующих идентификатор ресурса %d",
		cen.collections, cen.steps, cen.chains, cen.publishing)

	if len(findings) > 0 {

		var b strings.Builder
		fmt.Fprintf(&b, "идентификатор ресурса опубликован без названного исхода операции: %d\n", len(findings))
		fmt.Fprintf(&b, "\nОперация несёт предвыделенный идентификатор в metadata и когда завершилась ошибкой.\n")
		fmt.Fprintf(&b, "Опрос обязан НАЗВАТЬ исход (pm.expect над полем error), а не только done.\n")
		fmt.Fprintf(&b, "Форму дописывает проход `_assert_published_id_outcome` в scripts/gen.py набора.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}

// nmCensus — объём осмотренного. Печатается вместе с вердиктом, чтобы «ноль
// находок» было отличимо от «ноль прочитанного».
type nmCensus struct{ collections, steps, chains, publishing int }

// auditPublishedIdOutcome — весь разбор одним входом: чтение коллекций, обход
// папок, построение цепочек, находки и перепись.
//
// Вынесено из тела гейта намеренно: проба, доказывающая способность гейта упасть,
// обязана гонять ТУ ЖЕ функцию, а не свою копию логики — иначе она доказывает
// свойство копии (тот же порядок, что у `panicrecoverywiring_injection_test.go`).
func auditPublishedIdOutcome(root string, cols []string) ([]nmFinding, nmCensus, error) {
	var findings []nmFinding
	var cen nmCensus
	for _, rel := range cols {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, cen, fmt.Errorf("чтение %s: %w", rel, err)
		}
		var col nmCollection
		if err := json.Unmarshal(b, &col); err != nil {
			return nil, cen, fmt.Errorf("разбор %s: %w", rel, err)
		}
		cen.collections++

		var walk func(items []nmItem, path []string)
		walk = func(items []nmItem, path []string) {
			var run []nmItem
			flush := func() {
				if len(run) == 0 {
					return
				}
				s, c, p, f := nmScanRun(rel, path, run)
				cen.steps += s
				cen.chains += c
				cen.publishing += p
				findings = append(findings, f...)
				run = nil
			}
			for _, it := range items {
				if it.isFolder() {
					flush()
					walk(it.Item, append(path, it.Name))
					continue
				}
				run = append(run, it)
			}
			flush()
		}
		walk(col.Item, nil)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings, cen, nil
}

// nmAssignsEnvVar — шаг присваивает это имя окружения (любым значением).
func nmAssignsEnvVar(src, name string) bool {
	code := jsCodeSkeleton(src)
	lit := jsCodeKeepingStrings(src)
	for _, m := range nmEnvSetRe.FindAllStringIndex(code, -1) {
		if v, _, ok := nmStringLiteralAt(lit, m[1]-1); ok && v == name {
			return true
		}
	}
	return false
}

// nmScanRun — разбор одного непрерывного отрезка шагов (шаги одного родителя).
//
// Цепочки строятся ТЕМ ЖЕ правилом, что проход `_assert_published_id_outcome` в
// генераторах: опрос отходит ближайшей предшествующей мутации, которая
// ПРИСВАИВАЕТ имя из адреса опроса, а если такой нет — ближайшей предшествующей
// мутации вообще. Правило одно на обе стороны намеренно: между созданием и его
// опросом законно стоит другая мутация (отмена той же операции), и наивное
// «последняя мутация» отдало бы опрос ей — гейт требовал бы того, чего проход не
// дописывает, и наоборот.
func nmScanRun(rel string, path []string, run []nmItem) (steps, chains, publishing int, findings []nmFinding) {
	steps = len(run)
	var muts []int
	for i, it := range run {
		if nmMutationMethods[it.method()] && !nmOpPollPath.MatchString(it.rawURL()) {
			muts = append(muts, i)
		}
	}
	chains = len(muts)
	polls := map[int][]int{}

	for i, it := range run {
		if !nmIsPoll(it) {
			continue
		}
		m := nmOpPollPath.FindStringSubmatch(it.rawURL())
		owner := -1
		for _, k := range muts {
			if k >= i {
				break
			}
			if nmAssignsEnvVar(run[k].testScript(), m[1]) {
				owner = k
			}
		}
		if owner < 0 {
			for _, k := range muts {
				if k < i {
					owner = k
				}
			}
		}
		if owner >= 0 {
			polls[owner] = append(polls[owner], i)
		}
	}

	for _, si := range muts {
		opVar := "opId"
		for _, k := range polls[si] {
			if m := nmOpPollPath.FindStringSubmatch(run[k].rawURL()); m != nil {
				opVar = m[1]
			}
		}
		own := run[si].testScript()
		vars := nmPublishedResourceVars(own, opVar)
		if len(vars) == 0 {
			continue
		}
		publishing++

		// Исход, названный САМИМ шагом мутации, — законная форма, а не пробел:
		// синхронная операция (geo) завершается в ответе на мутацию, опрашивать
		// нечего, и её шаг утверждает `done` и отсутствие `error` прямо у себя.
		// Гейт, не знающий этой формы, потребовал бы опроса там, где его быть не
		// может, — и был бы снят как непонятный.
		if nmStatesOperationOutcome(own) {
			continue
		}

		casePath := strings.Join(path, " / ")
		if len(polls[si]) == 0 {
			findings = append(findings, nmFinding{rel, casePath, run[si].Name, vars,
				"а исход операции не назван ни им самим, ни каким-либо опросом"})
			continue
		}
		var code strings.Builder
		for _, k := range polls[si] {
			code.WriteString(run[k].testScript())
			code.WriteByte('\n')
		}
		if !nmStatesOperationOutcome(code.String()) {
			findings = append(findings, nmFinding{rel, casePath, run[si].Name, vars,
				"а опрос утверждает только done — провалившаяся операция тоже done"})
		}
	}
	return steps, chains, publishing, findings
}
