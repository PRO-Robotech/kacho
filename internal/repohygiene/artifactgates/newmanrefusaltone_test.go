// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanrefusaltone_test.go — гейт против утверждения о тексте отказа, которое
// приведением регистра ДОКАЗУЕМО не различает тон, объявленный контрактом.
//
// # Чем он отличается от соседа по полосе операций
//
// `newmanoperationtext_test.go` судит ОДНУ полосу, у которой текст вычисляется
// целиком двумя производителями, и требует там равенства. Вне этой полосы текст
// производит ДОМЕН, вычислить его целиком нельзя, и требование равенства
// краснело бы на верном продукте. Поэтому здесь предмет уже: не «утверждай текст
// целиком», а «не выбрасывай из утверждения то, что уже известно про регистр».
//
// # Почему приведение регистра — предмет, а не педантизм
//
// Тон сообщений — часть контракта (`api-conventions.md` §Error-format: «Тексты —
// часть контракта; меняются только осознанно»). `toLowerCase()` НЕ РАЗЛИЧАЕТ
// регистр by construction, поэтому расхождение тона по регистру не может
// покраснеть ни в одном прогоне. Так и вышло на полосе операций: расхождение
// одной буквы прожило всю жизнь трёх кейсов, заведённых ровно ради того, чтобы
// текст был частью контракта (#1370, #1401).
//
// В ОТРИЦАНИИ приведение регистра ЗАКОННО — там оно РАСШИРЯЕТ проверку, — и
// гейт отрицаний не судит.
//
// # Находка — ДОКАЗАТЕЛЬСТВО, а не догадка. Видов четыре.
//
//  1. РАСХОЖДЕНИЕ С ПРОИЗВОДИТЕЛЕМ. Утверждаемый литерал находится у
//     производителя ТОЛЬКО под приведением регистра — значит производитель пишет
//     его иначе, и приведение прячет именно это расхождение. Гейт печатает
//     дословное написание производителя, поэтому починка не требует догадок.
//
//  2. ЗАГЛАВНЫЕ, ОБЪЯВЛЕННЫЕ САМИМ ЗАГОЛОВКОМ. Заголовок кейса называет текст в
//     кавычках, в нём есть постоянная часть с заглавными, и эта часть дословно
//     существует у производителя. Тогда утверждение, приводящее регистр, не
//     различает того, что заголовок объявил, — независимо от того, какой
//     литерал оно проверяет. Мерка здесь — объявление САМОГО АВТОРА, а не
//     суждение гейта о естественном языке.
//
//  3. УТВЕРЖДЕНИЕ УЖЕ ОБЪЯВЛЕННОГО. Заголовок называет текст, его постоянная
//     часть дословно существует у производителя, а утверждается лишь СТРОГАЯ
//     её часть. Шаг проверяет меньше, чем сам объявил, и сообщение о другом
//     предмете с той же общей частью проходит: объявлено «type is immutable»,
//     утверждается «immutable» — краснеть на неизменяемости ЧУЖОГО поля нечем.
//     Заглавных этот вид не требует, поэтому ловит ровно то, что виду 2
//     недоступно.
//
//  4. УТВЕРЖДАЕТСЯ ОБЩАЯ ЧАСТЬ ТОНА, А НЕ ТЕКСТ ВЛАДЕЛЬЦА. У владельца этой
//     коллекции есть НЕ МЕНЕЕ ДВУХ разных отказов, несущих утверждаемую часть и
//     вдобавок называющих, чей он (перед ней стоит литеральное слово, а не одна
//     подстановка). Тогда установлено: утверждение проходит на отказе, которого
//     кейс не называл, и подмены одного отказа другим не различает ни при каком
//     ответе. ОДНОГО производителя не достаточно намеренно — при единственном
//     тексте утверждение однозначно в пределах владельца, и находка была бы
//     суждением о прозе.
//
//     Мерка этого вида — ДЕРЕВО, а не объявление автора, поэтому объявления в
//     заголовке он НЕ ТРЕБУЕТ. Требование объявления стояло здесь до #1520 —
//     унаследованное от видов 2 и 3, где объявление и есть мерка, — и выводило
//     из-под наблюдения 20 утверждений в 19 шагах, из которых у девяти
//     заголовок при этом обещал «verbatim text».
//
// # Производителя надо УМЕТЬ ПРОЧИТАТЬ, иначе все четыре вида молчат
//
// Текст, которого распознаватель не прочитал, для гейта не существует: у
// утверждения о нём «производителя нет», и ни один вид не срабатывает — это не
// находка и не её отсутствие, это невидимость (`testing.md` §«Гейт на класс»,
// п. 7). Поэтому читаются ВСЕ ВОСЕМЬ форм записи отказа этого дерева:
//
//	status.Error(f)      — форматная строка отказа;
//	fmt.Errorf           — обёртка;
//	fmt.Sprintf          — текст, собранный форматированием и уезжающий клиенту
//	                       через `status.Errorf(codes.X, "%s", msg)`; форма
//	                       ГОСПОДСТВУЮЩАЯ у таблиц SQLSTATE→текст;
//	errors.New           — сигнальная ошибка с постоянным текстом;
//	Wrapf/Wrap           — текст ВТОРЫМ аргументом, за сигнальной ошибкой;
//	конструктор с именем поля — `serviceerr.InvalidArg(f, "…")`, имя и литералом,
//	                       и переменной;
//	проверка формата чужого id — текст собирается ИЗ АРГУМЕНТА;
//	табличная запись     — `"type": "…"`, по вызову не читается вовсе.
//
// Вызовы читаются по склеенному тексту файла, а не построчно: перенос второго
// аргумента на следующую строку здесь обычен. Каждая форма доказана инъекцией,
// и рядом с ними стоит контроль — проза комментария производителем не становится.
//
// ТРИ ИЗ ВОСЬМИ ФОРМ ДОБАВЛЕНЫ #1748, и цена слепоты измерена, а не предположена:
// `fmt.Sprintf`, `errors.New` и семейство `Wrapf` не читались вовсе. С ними
// производителей у iam стало 1291 вместо 1028, у общего фундамента — 1804 вместо
// 1171, а гейт нашёл ДЕВЯТЬ настоящих утверждений вида 4 («already exists»
// покрывает пять разных отказов iam, «cannot be deleted» — одиннадцать).
// Перепись при этом подтвердила, что прибавка была СЛЕПОЙ ЗОНОЙ, а не
// регрессией дерева: полоса осматриваемых утверждений не изменилась (302 до и
// после расширения), изменилось только число доказанных среди них.
//
// РАСПОЗНАВАТЕЛЬ ОДИН НА ОБА ГЕЙТА. Сосед по полосе
// (`newmanrefusalproducer_test.go`) читает производителей ЭТОЙ же функцией:
// до #1748 у него была своя надстройка, и два распознавателя говорили об одном
// предмете, расходясь молча.
//
// # Что НЕ судится, и это названо, а не умолчано
//
// Приведение регистра там, где производитель пишет текст строчными целиком И у
// владельца нет второго отказа, несущего ту же часть богаче, находкой не
// считается: доказательства расхождения нет ни по написанию (вид 1), ни по
// объявлению автора (виды 2-3), ни по дереву (вид 4), а «утверждение слабее, чем
// хотелось бы» само по себе — суждение о прозе, у которого машинного предиката
// нет (`security.md` §«Механического детектора сборки НЕТ»). Граница названа
// прямо, чтобы молчание гейта об этих местах не читалось как их проверка;
// перепись печатает, сколько таких утверждений осмотрено.
//
// ЧИНИТСЯ находка любого вида одинаково: утверждай текст владельца целиком, без
// приведения регистра, — общий слой генератора несёт для этого две формы,
// равенство и вхождение (`assert_refusal_message`,
// `assert_refusal_message_contains`). Вхождение — не послабление, а ответ на
// сообщение с хвостом, который статически не вычисляется (накопитель нарушений,
// перечень помех); утверждается в обеих формах ВЕСЬ текст владельца.
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

// ─── производители текстов отказа ────────────────────────────────────────────

var (
	// Форматная строка отказа, читаемая ровно в той форме, в какой её читает
	// компилятор.
	rtStatusText = regexp.MustCompile(`status\.Errorf?\(\s*codes\.[A-Za-z]+\s*,\s*"((?:[^"\\]|\\.)*)"`)
	rtWrapText   = regexp.MustCompile(`fmt\.Errorf\(\s*"((?:[^"\\]|\\.)*)"`)
	// Проверка формата чужого идентификатора собирает текст ИЗ АРГУМЕНТА, а не из
	// шаблона: `corevalidate.ResourceID("Image", …)` даёт клиенту `invalid Image
	// id '<X>'`. Распознаватель, не знающий этой формы, не видел бы РОВНО того
	// расхождения, ради которого гейт заведён (`testing.md` §«Гейт на класс», п.7).
	rtResourceIDArg = regexp.MustCompile(`ResourceID\(\s*"([A-Za-z][\w ]*)"`)
	// Отказ, собранный конструктором с ИМЕНЕМ ПОЛЯ первым аргументом
	// (`serviceerr.InvalidArg("boot_source", "…")`, `shared.InvalidArg`,
	// `errInvalidArg`). Форма в этом дереве господствующая, и распознаватель,
	// её не знающий, объявлял бы «производителя нет» у ЦЕЛОГО вида отказов —
	// то есть молчал бы там, где предмет и живёт (testing.md §«Гейт на класс», п.7).
	// Имя поля бывает и ЛИТЕРАЛОМ, и переменной (`InvalidArg(f, "…")` в цикле по
	// маске) — вторая форма в этом дереве несёт целый набор текстов о
	// неизменяемости, и распознаватель, знающий только первую, их не видел.
	rtInvalidArg = regexp.MustCompile(`\w*InvalidArg\w*\(\s*(?:"[^"]*"|[\w.]+)\s*,\s*"((?:[^"\\]|\\.)*)"`)
	// Отказ, вынесенный в ТАБЛИЦУ: `"type": "type is immutable after …",`.
	// Текст доезжает до клиента через `status.Errorf(codes.X, "%s", msg)`, где
	// шаблон — сам «%s», поэтому по вызову он не читается вовсе.
	rtMapEntry = regexp.MustCompile(`^\s*"[^"]+"\s*:\s*"((?:[^"\\]|\\.)*)"\s*,\s*$`)
	// Текст, собранный ФОРМАТИРОВАНИЕМ, а не конструктором отказа:
	// `fmt.Sprintf("…", …)` уезжает клиенту через `status.Errorf(codes.X, "%s", msg)`
	// либо через поле сигнальной ошибки. Форма ГОСПОДСТВУЮЩАЯ у таблиц
	// SQLSTATE→текст (`pgmaperr.go`, `errmap.go`) — то есть ровно у тех текстов,
	// которые арендатор и видит (#1748).
	rtSprintfText = regexp.MustCompile(`fmt\.Sprintf\(\s*"((?:[^"\\]|\\.)*)"`)
	// Сигнальная ошибка с постоянным текстом: `errors.New("network is not empty")`.
	rtErrorsNew = regexp.MustCompile(`errors\.New\(\s*"((?:[^"\\]|\\.)*)"`)
	// `iamerr.Wrapf(iamerr.ErrNotFound, "%s %s is not an active cluster admin", …)`
	// — текст стоит ВТОРЫМ аргументом, за сигнальной ошибкой.
	rtWrapArg = regexp.MustCompile(`\bWrapf?\(\s*[\w.]+\s*,\s*"((?:[^"\\]|\\.)*)"`)
	rtVerb    = regexp.MustCompile(`%[#+\-0-9.]*[a-zA-Z]`)
	// Слово, а не подстановка: три и более буквы подряд. Ими производитель
	// называет, ЧЕЙ это отказ («Machine type», «Network interface»).
	rtWordish = regexp.MustCompile(`[A-Za-z]{3,}`)
)

// rtOwner — чьи производители обслуживают этот путь. Сервисный каталог отвечает
// за себя, всё прочее (`pkg/`, `gateway/`) — общее для всех.
//
// Область НЕ шире сервиса намеренно: одноимённый текст соседнего домена, набранный
// строчными, маскировал бы настоящее расхождение своего. Замерено: при переписи по
// всему дереву расхождение `invalid image id` против `invalid Image id` пропадало
// из находок — его закрывал одноимённый текст ЧУЖОГО сервиса.
const rtSharedOwner = "*"

func rtOwner(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) > 1 && parts[0] == "services" {
		return parts[1]
	}
	return rtSharedOwner
}

// rtProducers — тексты отказа дерева, разложенные по владельцам.
func rtProducers(root string, goFiles []string) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	add := func(owner, tmpl string) {
		if out[owner] == nil {
			out[owner] = map[string]bool{}
		}
		out[owner][tmpl] = true
	}
	for _, rel := range goFiles {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь получен из индекса git этого модуля
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		owner := rtOwner(rel)
		// Вызов конструктора отказа переносится на вторую строку сплошь и рядом
		// (`serviceerr.InvalidArg("boot_source",` + текст ниже). Построчный
		// распознаватель такой формы не видит, а «производителя нет» и «я его не
		// прочитал» неотличимы — поэтому вызовы читаются по СКЛЕЕННОМУ тексту, и
		// только табличная запись остаётся построчной: у неё смысл задан строкой.
		var joined strings.Builder
		for _, raw := range strings.Split(string(b), "\n") {
			joined.WriteString(slpStripGoComment(raw))
			joined.WriteString("\n")
		}
		whole := joined.String()
		for _, re := range []*regexp.Regexp{rtStatusText, rtWrapText, rtInvalidArg,
			rtSprintfText, rtErrorsNew, rtWrapArg} {
			for _, m := range re.FindAllStringSubmatch(whole, -1) {
				add(owner, strings.ReplaceAll(m[1], "%q", `"%s"`))
			}
		}
		for _, m := range rtResourceIDArg.FindAllStringSubmatch(whole, -1) {
			add(owner, "invalid "+m[1]+" id '%s'")
		}
		for _, raw := range strings.Split(string(b), "\n") {
			line := slpStripGoComment(raw)
			for _, m := range append(rtStatusText.FindAllStringSubmatch(line, -1),
				rtWrapText.FindAllStringSubmatch(line, -1)...) {
				add(owner, strings.ReplaceAll(m[1], "%q", `"%s"`))
			}
			for _, m := range rtResourceIDArg.FindAllStringSubmatch(line, -1) {
				add(owner, "invalid "+m[1]+" id '%s'")
			}
			for _, m := range rtInvalidArg.FindAllStringSubmatch(line, -1) {
				add(owner, strings.ReplaceAll(m[1], "%q", `"%s"`))
			}
			if m := rtMapEntry.FindStringSubmatch(line); m != nil {
				add(owner, strings.ReplaceAll(m[1], "%q", `"%s"`))
			}
		}
	}
	return out, nil
}

// rtCorpus — тексты, которыми вправе отвечать шаг этой коллекции: свой сервис
// плюс общий фундамент и край.
type rtCorpus struct {
	blob string // все шаблоны через разделитель — для поиска дословного вхождения
	low  string // он же строчными — для поиска вхождения под приведением регистра
	n    int
}

func rtCorpusFor(byOwner map[string]map[string]bool, owner string) rtCorpus {
	set := map[string]bool{}
	for t := range byOwner[rtSharedOwner] {
		set[t] = true
	}
	for t := range byOwner[owner] {
		set[t] = true
	}
	names := make([]string, 0, len(set))
	for t := range set {
		names = append(names, t)
	}
	sort.Strings(names)
	// Разделитель, которого нет ни в одном тексте: без него хвост одного шаблона
	// и голова следующего склеились бы в «производителя», которого нет.
	blob := strings.Join(names, "\x00")
	return rtCorpus{blob: blob, low: strings.ToLower(blob), n: len(names)}
}

// ─── разбор шага ─────────────────────────────────────────────────────────────

var (
	rtAssertOn  = regexp.MustCompile(`\.to\.(?:include|contain|eql|equal)\(`)
	rtNegation  = regexp.MustCompile(`\.to\.not\.`)
	rtLower     = regexp.MustCompile(`toLowerCase\(\)`)
	rtMessage   = regexp.MustCompile(`\bmessage\b`)
	rtJSLiteral = regexp.MustCompile(`'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"`)
	// Текст, объявленный заголовком: то, что автор поставил в кавычки.
	rtTitleQuoted = regexp.MustCompile(`'([^']{9,})'|"([^"]{9,})"`)
	// Подстановка внутри объявленного текста: значение, а не постоянная часть.
	rtTitleSubst = regexp.MustCompile(`\.\.\.|<[^>]{1,24}>|%[a-z]|\{\{[^}]+\}\}`)
	rtStartsWord = regexp.MustCompile(`^[A-Za-z]`)
)

type rtFinding struct {
	collection, title, step, why string
}

func (f rtFinding) String() string {
	return fmt.Sprintf("%s :: %s / %s — %s", f.collection, f.title, f.step, f.why)
}

type rtCensus struct {
	collections, steps int
	// stepsWithDeclaredText — шаги, чей заголовок называет текст отказа в кавычках.
	stepsWithDeclaredText int
	// foldedAssertions — позитивные утверждения о сообщении, приводящие регистр.
	// Это и есть осматриваемая совокупность: ноль здесь означает ослепший
	// распознаватель, а не чистое дерево.
	foldedAssertions int
	// foldedWithoutProof — из них те, у которых доказательства расхождения нет
	// (производитель пишет текст строчными и заголовок заглавных не объявлял).
	// Печатается отдельно, чтобы «ноль находок» не читалось шире, чем есть.
	foldedWithoutProof int
}

// rtDeclaredTexts — тексты, объявленные заголовком кейса.
//
// Фильтр узок намеренно: заголовки этого корпуса двуязычны и несут стрелки,
// поэтому кавычки в них обрамляют и прозу. Текст отказа в дереве английский,
// начинается с буквы и стрелки не содержит; всё прочее — не объявление текста,
// и судить по нему значило бы краснеть на собственном объяснении.
func rtDeclaredTexts(title string) []string {
	var out []string
	for _, m := range rtTitleQuoted.FindAllStringSubmatch(title, -1) {
		q := strings.TrimSpace(m[1])
		if q == "" {
			q = strings.TrimSpace(m[2])
		}
		if q == "" || !rtIsASCII(q) || !strings.Contains(q, " ") {
			continue
		}
		if strings.Contains(q, "→") || strings.Contains(q, "->") {
			continue
		}
		// Объявление вправе НАЧИНАТЬСЯ с подстановки: `'... output-only …'`,
		// `'<Resource> <id> not found'`. Требование «первая буква» отвергало такие
		// объявления целиком, и вид, записанный этой формой, оставался вне
		// наблюдения — не находкой, а невидимостью.
		if !rtStartsWord.MatchString(strings.TrimLeft(rtTitleSubst.ReplaceAllString(q, " "), " ")) {
			continue
		}
		out = append(out, q)
	}
	return out
}

func rtIsASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// rtConstParts — постоянные части объявленного текста: то, что клиент увидит
// дословно при любой подстановке.
func rtConstParts(declared string) []string {
	var out []string
	for _, p := range rtTitleSubst.Split(declared, -1) {
		p = strings.Trim(p, " .,:;'\"")
		if len(p) >= 4 {
			out = append(out, p)
		}
	}
	return out
}

// rtRicherProducers — тексты производителя, которые несут постоянную часть `lp`
// и вдобавок НАЗЫВАЮТ, чей это отказ: перед `lp` у них стоит литеральное слово, а
// не одна лишь подстановка.
func rtRicherProducers(corpus rtCorpus, lp string) []string {
	var out []string
	for _, t := range strings.Split(corpus.blob, "\x00") {
		i := strings.Index(strings.ToLower(t), lp)
		if i <= 0 {
			continue
		}
		head := rtVerb.ReplaceAllString(t[:i], " ")
		if !rtWordish.MatchString(head) {
			continue
		}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func rtHasUpper(s string) bool {
	return strings.ToLower(s) != s
}

// rtComparedPart — хвост строки, начиная с оператора сравнения. Всё, что до
// него, — имя пробы и пояснение отказа, то есть речь к человеку: у неё
// производителя нет и быть не должно.
func rtComparedPart(line string) string {
	loc := rtAssertOn.FindStringIndex(line)
	if loc == nil {
		return ""
	}
	return line[loc[0]:]
}

// auditRefusalTone — весь разбор одним входом, чтобы инъекция гоняла ТУ ЖЕ
// функцию, а не свою копию логики.
func auditRefusalTone(root string, cols []string, corpusOf func(collection string) rtCorpus) ([]rtFinding, rtCensus, error) {
	var findings []rtFinding
	var cen rtCensus

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
		corpus := corpusOf(rel)

		var walk func(items []nmItem, title string)
		walk = func(items []nmItem, title string) {
			for _, it := range items {
				if it.isFolder() {
					next := title
					if next == "" {
						next = it.Name
					}
					walk(it.Item, next)
					continue
				}
				cen.steps++
				declared := rtDeclaredTexts(title)
				if len(declared) > 0 {
					cen.stepsWithDeclaredText++
				}
				findings = append(findings,
					rtAuditStep(rel, title, it, declared, corpus, &cen)...)
			}
		}
		walk(col.Item, "")
	}
	return findings, cen, nil
}

func rtAuditStep(rel, title string, it nmItem, declared []string, corpus rtCorpus, cen *rtCensus) []rtFinding {
	var lines []string
	for _, ev := range it.Event {
		if ev.Listen == "test" {
			lines = append(lines, ev.Script.Exec...)
		}
	}

	var why []string
	var foldedLits []string
	folded := false
	for _, raw := range lines {
		line := slpStripJSComment(raw)
		if !rtAssertOn.MatchString(line) || rtNegation.MatchString(line) {
			continue
		}
		if !rtMessage.MatchString(line) || !rtLower.MatchString(line) {
			continue
		}
		folded = true
		cen.foldedAssertions++

		// Вид 1: литерал находится у производителя ТОЛЬКО под приведением регистра.
		for _, lit := range rtJSLiteral.FindAllStringSubmatch(rtComparedPart(line), -1) {
			s := lit[1]
			if s == "" {
				s = lit[2]
			}
			s = strings.TrimSpace(s)
			if len(s) < 6 || !rtIsASCII(s) {
				continue
			}
			foldedLits = append(foldedLits, s)
			if strings.Contains(corpus.blob, s) {
				continue // производитель пишет ровно так — приведение ничего не прячет
			}
			i := strings.Index(corpus.low, strings.ToLower(s))
			if i < 0 {
				continue // производителя нет вовсе — это предмет соседнего гейта, не этого
			}
			why = append(why, fmt.Sprintf(
				"утверждается %q с приведением регистра, а производитель пишет %q — приведение "+
					"прячет РОВНО это расхождение и покраснеть не может НИКОГДА. Утверждай "+
					"написание производителя без toLowerCase()",
				s, corpus.blob[i:i+len(s)]))
		}
	}
	if !folded {
		return nil
	}

	// Вид 2: заголовок сам объявил заглавные, а утверждение их не различает.
	sawProof := len(why) > 0
	for _, d := range declared {
		var upper []string
		for _, p := range rtConstParts(d) {
			if rtHasUpper(p) && strings.Contains(corpus.blob, p) {
				upper = append(upper, p)
			}
		}
		if len(upper) == 0 {
			continue
		}
		sawProof = true
		why = append(why, fmt.Sprintf(
			"заголовок объявил текст %q, у производителя он существует дословно с заглавными "+
				"(%q), а утверждение приводит регистр — объявленного тона оно не различает "+
				"ни при каком ответе",
			d, strings.Join(upper, "`, `")))
	}

	// Вид 3: утверждение УЖЕ объявленного заголовком текста. Заголовок называет
	// текст в кавычках, его постоянная часть дословно существует у производителя,
	// а утверждаемый литерал — её СТРОГАЯ часть. Тогда шаг проверяет меньше, чем
	// сам объявил, и объявленное им отличие не различает ни при каком ответе:
	// сообщение о другом предмете, несущее ту же общую часть, проходит.
	//
	// Мерка здесь — снова объявление САМОГО АВТОРА, а не суждение гейта о прозе;
	// заглавных не требуется, поэтому вид ловит ровно то, что виду 2 недоступно:
	// «immutable» вместо объявленного «type is immutable».
	for _, d := range declared {
		for _, p := range rtConstParts(d) {
			if !strings.Contains(corpus.blob, p) {
				continue
			}
			lp := strings.ToLower(p)
			for _, lit := range foldedLits {
				ll := strings.ToLower(lit)
				if ll == lp || !strings.Contains(lp, ll) {
					continue
				}
				sawProof = true
				why = append(why, fmt.Sprintf(
					"заголовок объявил текст %q, у производителя его постоянная часть %q "+
						"существует дословно, а утверждается лишь %q — строгая её часть. Шаг "+
						"проверяет МЕНЬШЕ, чем объявил: сообщение о другом предмете с той же "+
						"общей частью проходит",
					d, p, lit))
			}
		}
	}

	// Вид 4: ОБЪЯВЛЕНИЕ ОБОБЩЕНО ПОДСТАНОВКОЙ ТАМ, ГДЕ ПРОИЗВОДИТЕЛЬ ПИШЕТ ЛИТЕРАЛ.
	//
	// Виды 2 и 3 меряют утверждение объявлением автора и потому молчат, когда
	// объявление СЛАБЕЕ производителя: заголовок `'<Resource> <id> not found'`
	// объявляет постоянной частью «not found» — заглавных нет (вид 2 нем), а
	// утверждение совпадает с объявлением, не уступая ему (вид 3 нем). Слабость
	// лежит в самом объявлении, обобщённом подстановкой.
	//
	// ДОКАЗАТЕЛЬСТВО, А НЕ СУЖДЕНИЕ, И ОНО ТРЕБУЕТ ДВУХ ПРОИЗВОДИТЕЛЕЙ. Находка
	// объявляется, только когда у ВЛАДЕЛЬЦА этой коллекции есть не менее ДВУХ
	// разных текстов, которые несут ту же постоянную часть и вдобавок называют,
	// ЧЕЙ это отказ (перед ней стоит литеральное слово, а не одна подстановка).
	// Тогда установлено: утверждение проходит на отказе, которого кейс не
	// называл, — и краснеть на подмене одного отказа другим ему нечем.
	//
	// ОДНОГО производителя НЕ ДОСТАТОЧНО намеренно: при единственном тексте
	// утверждение слабее объявления, но однозначно в пределах владельца, и
	// находка была бы суждением о прозе.
	//
	// ОБЪЯВЛЕНИЯ В ЗАГОЛОВКЕ ЭТОТ ВИД НЕ ТРЕБУЕТ — и это ПОПРАВКА, а не
	// послабление (#1520). Виды 2 и 3 меряют утверждение объявлением автора,
	// потому что судят «шаг проверяет МЕНЬШЕ, чем сам объявил»; там объявление —
	// это мерка. Здесь мерка ДРУГАЯ: не объявление, а ДЕРЕВО — два разных отказа
	// одного владельца, несущие ту же часть и называющие, чей он. Такое
	// доказательство не становится ни сильнее, ни слабее от того, назвал ли
	// заголовок текст в кавычках, и требование объявления было унаследовано от
	// соседних видов, а не выведено.
	//
	// Цена унаследованного требования измерена: с ним вид молчал на 20
	// утверждениях в 19 шагах, из которых у ДЕВЯТИ заголовок при этом обещал
	// «verbatim text» либо «дословный тон». То есть требование объявления
	// выводило из-под наблюдения ровно те шаги, чей автор считал, что текст он
	// проверяет.
	for _, lit := range foldedLits {
		ll := strings.ToLower(lit)
		rich := rtRicherProducers(corpus, ll)
		if len(rich) < 2 {
			continue
		}
		sawProof = true
		why = append(why, fmt.Sprintf(
			"утверждается лишь %q — ОБЩАЯ часть тона, а не текст владельца. У этого владельца "+
				"%d разных отказа несут ту же часть и вдобавок называют, чей он (например %q и %q). "+
				"Утверждение проходит на отказе, которого кейс не называл, и подмены одного "+
				"отказа другим не различает НИ ПРИ КАКОМ ответе. Утверждай текст владельца "+
				"целиком, без toLowerCase() (`assert_refusal_message` общего слоя)",
			lit, len(rich), rich[0], rich[1]))
	}

	if !sawProof {
		cen.foldedWithoutProof++
		return nil
	}

	out := make([]rtFinding, 0, len(why))
	for _, w := range why {
		out = append(out, rtFinding{collection: rel, title: title, step: it.Name, why: w})
	}
	return out
}

// ─── гейт по дереву ──────────────────────────────────────────────────────────

func TestNewmanRefusalToneIsNotFoldedAway(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева — из ИНДЕКСА git: под корнем лежат рабочие копии агентов и
	// распаковки отчётов прогонов, и вердикт по ним был бы свойством чужого
	// рабочего каталога, а не коммита.
	tt := newTrackedTree(t, root)

	byOwner, err := rtProducers(root, optGoFiles(tt))
	if err != nil {
		t.Fatal(err)
	}
	// Предпосылка первая: производители найдены. Пустой набор означает ослепший
	// распознаватель, и тогда «расхождения нет» у КАЖДОГО утверждения дерева.
	if len(byOwner[rtSharedOwner]) == 0 {
		t.Fatal("в общем фундаменте (pkg/, gateway/) не найдено НИ ОДНОГО текста отказа — " +
			"распознаватель производителей ослеп; чинить надо гейт, а не выходить успехом")
	}

	cols := optCollections(tt)
	findings, cen, err := auditRefusalTone(root, cols, func(rel string) rtCorpus {
		return rtCorpusFor(byOwner, rtOwner(rel))
	})
	if err != nil {
		t.Fatal(err)
	}

	if cen.collections == 0 {
		t.Fatal("ни одной коллекции newman в индексе git — гейту нечего читать")
	}
	if cen.steps == 0 {
		t.Fatalf("прочитано коллекций %d, шагов 0 — обход не узнал ни одного шага", cen.collections)
	}
	// Предпосылка вторая: оба распознавателя живы. Ноль здесь означает «ноль
	// прочитанного», а не «ноль находок».
	if cen.foldedAssertions == 0 {
		t.Fatalf("в %d шагах не найдено НИ ОДНОГО утверждения о сообщении с приведением регистра — "+
			"распознаватель утверждения ослеп", cen.steps)
	}
	if cen.stepsWithDeclaredText == 0 {
		t.Fatalf("в %d шагах ни один заголовок не объявляет текста отказа в кавычках — "+
			"распознаватель объявления ослеп, и вид 2 не проверяется ни на чём", cen.steps)
	}

	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, fmt.Sprintf("%s:%d", o, len(byOwner[o])))
	}
	sort.Strings(owners)
	t.Logf("осмотрено: коллекций %d, шагов %d; заголовок объявляет текст отказа у %d шагов; "+
		"позитивных утверждений о сообщении с приведением регистра %d, из них БЕЗ доказательства "+
		"расхождения %d (производитель пишет строчными и заголовок заглавных не объявлял — "+
		"граница гейта, а не их проверка). Производителей по владельцам: %v",
		cen.collections, cen.steps, cen.stepsWithDeclaredText,
		cen.foldedAssertions, cen.foldedWithoutProof, owners)

	if len(findings) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "утверждений о тексте отказа, доказуемо не различающих объявленный тон: %d\n\n", len(findings))
		b.WriteString("Тон сообщений — часть контракта. `toLowerCase()` не различает регистр\n")
		b.WriteString("by construction, поэтому расхождение тона по регистру под ним не может\n")
		b.WriteString("покраснеть НИ В ОДНОМ прогоне. Чинится в cases/*.py набора: утверждай\n")
		b.WriteString("написание производителя без приведения регистра; коллекции затем\n")
		b.WriteString("перегенерируются scripts/gen.py своего набора.\n\n")
		for _, f := range findings {
			fmt.Fprintf(&b, "  %s\n", f)
		}
		t.Error(b.String())
	}
}
