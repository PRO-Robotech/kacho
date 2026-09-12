// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retryadvicelane.go — СОВЕТ ПОВТОРИТЬ, АДРЕСОВАННЫЙ НЕКОМУ.
//
// # Предмет
//
// Текст отказа вправе сказать вызывающему, что делать дальше: «повтори». Совет
// исполним ровно тогда, когда повтор в этой полосе что-то меняет. Есть две
// полосы, где он не меняет ничего, и в обеих вызывающий уходит делать то, чего
// сделать нельзя (ban #18, «разработчик»: говорит ли отказ, что делать дальше):
//
//	A1  отказ несёт КОД, который повтор запрещает. Канонический смысл
//	    FAILED_PRECONDITION — «не повторяй, пока состояние не исправлено»;
//	    NOT_FOUND, ALREADY_EXISTS, INVALID_ARGUMENT, PERMISSION_DENIED тем же
//	    повтором не меняются. Совет «повтори» рядом с таким кодом противоречит
//	    коду, который стоит в ТОМ ЖЕ выражении, — и клиент, ключующийся на код
//	    (`api-conventions.md` §reason-token), получает два несовместимых
//	    указания сразу;
//
//	A2  отказ произведён в теле АСИНХРОННОЙ мутации. Повторять там нечего:
//	    запроса уже нет, операция принята, исход её терминален, и внутри
//	    платформы тело не переигрывается (бюджет повтора у исполнителя операции
//	    тратится на терминальную запись, а не на тело). Вызывающий получает
//	    `done:true` с советом повторить запрос, которого не существует.
//
// # Что это НЕ судит — названо, чтобы зелёное не читалось шире
//
//  1. Совет на РАЗРЕШАЮЩЕМ коде (ABORTED, UNAVAILABLE, RESOURCE_EXHAUSTED,
//     DEADLINE_EXCEEDED) законен и остаётся молчанием. Такой совет в дереве
//     ЖИВОЙ и перепись его печатает: ноль здесь означал бы, что ветка
//     разрешающего кода не исполняется вовсе, то есть держится одной инъекцией.
//  2. Код, не принадлежащий ни одному из двух множеств (INTERNAL, UNKNOWN,
//     DATA_LOSS), не судится: повторяемость там не объявлена ни в ту, ни в
//     другую сторону, и суждение было бы нашим вкусом, а не контрактом.
//  3. Сентинел, чьё имя НЕ формы `Err<Код>` (`ErrConflict`, `ErrPoolNotResolved`),
//     кода не называет и под A1 не подпадает. Это осознанная граница: выводить
//     код такого сентинела значило бы завести ВТОРУЮ таблицу маппинга рядом с
//     той, что живёт в сервисе, и разойтись с ней молча.
//  4. Достижимость отказа из асинхронного тела ЧЕРЕЗ вызов (тело зовёт метод
//     хранилища, а текст лежит в нём) — вне наблюдения A2: A2 судит только то,
//     что стоит внутри тела ЛЕКСИЧЕСКИ. Транзитивный обход по именам здесь
//     отвергнут — он переоценивает граф (одноимённые методы разных типов) и
//     давал бы находки на путях, которых нет. Эту половину закрывает A1 всякий
//     раз, когда код отказа повтор запрещает; полоса «разрешающий код, и при
//     этом путь асинхронный» держателя не имеет, и это остаток, а не обещание.
//  5. Строка журнала. Совет оператору «проход повторится» — законный и частый
//     оборот, к вызывающему не адресованный. Литерал, лежащий внутри вызова
//     журналирования, снимается ДО распознавания совета.
//
// # Почему разбор, а не поиск по образцу
//
// Слово «повтор» и `retry` в этом дереве частотны и почти всегда законны: имя
// пакета отступа, поле настройки, метрика усиления, заголовок пробы, проза о
// самом повторе, отрицание повтора. Поиск по подстроке краснел бы на собственном
// объяснении (`testing.md` §«Гейт на класс», п.4). Здесь судится РАЗОБРАННОЕ:
// строковый литерал, стоящий аргументом выражения-отказа, у которого разбор же
// установил код.
//
// # Формы записи, которые распознаватель знает (`testing.md` §«Гейт на класс», п.7)
//
// Корпус двуязычен, поэтому обе половины обязательны. Каждая форма доказана
// своей инъекцией; форма, о которой распознаватель не знает, даёт не красное и
// не зелёное, а МОЛЧАНИЕ.
//
//	F1-en-retry      retry / retries / retrying
//	F2-en-tryagain   try again
//	F3-en-resend     resubmit / resend / reissue
//	F4-ru-povtor     повтори(те) / повторить / повторяйте / повтором
//	F5-ru-poprobuyte попробуйте снова / попробуйте ещё раз
//
// Ось ОТРИЦАНИЯ снимает совет целиком: текст вправе назвать повтор, чтобы
// сказать, что его НЕ будет. Такой текст в дереве есть
// (`"iam register rejected (no retry)"`), но под ЭТОТ гейт он не подпадает —
// его сентинел (`ErrPermanent`) кода не называет, см. границу 3. Перепись
// печатает число снятых отрицанием, и на день заведения оно НОЛЬ: ось держится
// своей инъекцией, а не живым экземпляром, и это сказано прямо, потому что
// «ось есть» и «ось исполняется на дереве» — разные утверждения.
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// retryAdviceSource — один файл Go: путь относительно корня и тело.
//
// Разбор отделён от чтения дерева намеренно: проба способности гейта падать
// подаёт корпус В ПАМЯТИ и потому не заводит ни репозитория, ни временного
// каталога — чужое состояние остаётся нетронутым.
type retryAdviceSource struct {
	Rel  string
	Body string
}

// retryAdviceFinding — один совет повторить, адресованный некому.
type retryAdviceFinding struct {
	Rel  string
	Line int
	// Axis — по какой оси совет неисполним: A1 (код запрещает повтор), A2
	// (асинхронное тело) либо обе.
	Axis []string
	Form string
	// Lane — код отказа так, как он записан (`FailedPrecondition`), либо имя
	// сентинела, из которого код выведен.
	Lane string
	Text string
}

// retryAdviceCensus — исход обхода ВМЕСТЕ с объёмом осмотренного.
//
// Величины парные по каждой оси: «файлов прочитано» без «выражений отказа» и
// «выражений отказа» без «текстов осмотрено» скрывают ровно тот случай, ради
// которого гейт заведён, — распознаватель ослеп, а находок ноль.
type retryAdviceCensus struct {
	Files      int
	Parsed     int
	ParseFails []string
	// Refusals — выражений отказа осмотрено (вызов, у которого разбор нашёл код).
	Refusals int
	// Literals — текстов отказа осмотрено внутри них.
	Literals int
	// LogSkipped — литералов, снятых как строка журнала.
	LogSkipped int
	// AsyncBodies — асинхронных тел мутации найдено.
	AsyncBodies int
	// AsyncLiterals — текстов отказа, стоящих внутри них лексически.
	AsyncLiterals int
	// Advices — текстов, в которых распознан совет повторить.
	Advices int
	// Negated — из них снятых отрицанием (законные близнецы).
	Negated int
	// OnAllowedCode — советов на РАЗРЕШАЮЩЕМ коде: законны, молчим. Ось заведена
	// ради допущения, которое иначе осталось бы непроверенным: «совет повторить
	// сам по себе дефект». Он не дефект, и это замер, печатаемый на каждом
	// прогоне.
	OnAllowedCode int
	// OnUnjudgedCode — советов на коде вне обоих множеств.
	OnUnjudgedCode int
	ByForm         map[string]int
	ByCode         map[string]int
	Findings       []retryAdviceFinding
}

// retryAdviceForm — одна форма записи совета.
type retryAdviceForm struct {
	Name string
	Re   *regexp.Regexp
}

var retryAdviceForms = []retryAdviceForm{
	{"F1-en-retry", regexp.MustCompile(`(?i)\bretr(y|ies|ying)\b`)},
	{"F2-en-tryagain", regexp.MustCompile(`(?i)\btry\s+again\b`)},
	{"F3-en-resend", regexp.MustCompile(`(?i)\bre(submit|send|issue)\b`)},
	{"F4-ru-povtor", regexp.MustCompile(`(?i)повтор(и|ите|ить|яйте|ом|ите)?`)},
	{"F5-ru-poprobuyte", regexp.MustCompile(`(?i)попробуйте\s+(снова|ещё|еще)`)},
}

// retryAdviceNegations — обороты, которые называют повтор, чтобы его ОТРИЦАТЬ.
//
// Перечень нужен целиком: пропущенное отрицание даёт находку на месте, где автор
// честно сказал, что повтора не будет, — то есть гейт красит правду.
var retryAdviceNegations = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bno\s+retr`),
	regexp.MustCompile(`(?i)\bnot\s+retr`),
	regexp.MustCompile(`(?i)\b(do\s+not|don't|never|cannot|can't|won't|will\s+not)\s+(retr|try\s+again|resubmit|resend)`),
	regexp.MustCompile(`(?i)\bretry\s+(is\s+)?(pointless|useless|futile)`),
	regexp.MustCompile(`(?i)\bnon-?retryable\b`),
	regexp.MustCompile(`(?i)не\s+повтор`),
	regexp.MustCompile(`(?i)без\s+повтор`),
	regexp.MustCompile(`(?i)повтор\s+(бессмыслен|бесполезен|не\s)`),
	regexp.MustCompile(`(?i)повторять\s+нечего`),
}

// retryForbiddenCodes — коды, чей канонический смысл повтор ЗАПРЕЩАЕТ: тот же
// запрос при том же состоянии ответит тем же.
var retryForbiddenCodes = map[string]bool{
	"InvalidArgument":    true,
	"NotFound":           true,
	"AlreadyExists":      true,
	"PermissionDenied":   true,
	"Unauthenticated":    true,
	"FailedPrecondition": true,
	"OutOfRange":         true,
	"Unimplemented":      true,
}

// retryAllowedCodes — коды, чей канонический смысл повтор РАЗРЕШАЕТ. Совет на
// них законен и под предикат не подпадает.
var retryAllowedCodes = map[string]bool{
	"Aborted":           true,
	"Unavailable":       true,
	"ResourceExhausted": true,
	"DeadlineExceeded":  true,
	"Canceled":          true,
}

// retryAdviceLogCallees — имена вызовов журналирования. Литерал внутри них
// адресован ОПЕРАТОРУ, а не вызывающему, и под предикат не подпадает.
var retryAdviceLogCallees = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
	"Debugf": true, "Infof": true, "Warnf": true, "Errorf": true,
	"Print": true, "Printf": true, "Println": true,
	"Log": true, "LogAttrs": true,
}

// retryAdviceAsyncCallees — производители АСИНХРОННОГО тела мутации: вызов,
// которому тело передаётся замыканием.
var retryAdviceAsyncCallees = map[string]bool{
	"Run": true, "RunWithWorker": true, "RunSync": true,
}

// retryAdviceSpan — полузамкнутый отрезок позиций.
type retryAdviceSpan struct {
	From, Thru token.Pos
}

func (s retryAdviceSpan) holds(p token.Pos) bool { return p >= s.From && p < s.Thru }

// auditRetryAdvice — единственный анализатор. Второй об одном предмете разошёлся
// бы с первым молча, поэтому перепись здесь одна.
func auditRetryAdvice(sources []retryAdviceSource) retryAdviceCensus {
	c := retryAdviceCensus{ByForm: map[string]int{}, ByCode: map[string]int{}}
	for _, s := range sources {
		c.Files++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, s.Rel, s.Body, parser.SkipObjectResolution)
		if err != nil {
			c.ParseFails = append(c.ParseFails, s.Rel+": "+err.Error())
			continue
		}
		c.Parsed++
		auditRetryAdviceFile(&c, fset, s.Rel, file)
	}
	sort.Slice(c.Findings, func(i, j int) bool {
		if c.Findings[i].Rel != c.Findings[j].Rel {
			return c.Findings[i].Rel < c.Findings[j].Rel
		}
		return c.Findings[i].Line < c.Findings[j].Line
	})
	return c
}

// auditRetryAdviceFile — три прохода по одному дереву разбора: журнальные
// отрезки, асинхронные тела, выражения отказа. Порядок несущий: снятие
// журнальной строки обязано произойти ДО распознавания совета, иначе оператор
// читался бы как вызывающий.
func auditRetryAdviceFile(c *retryAdviceCensus, fset *token.FileSet, rel string, file *ast.File) {
	var logSpans, asyncSpans []retryAdviceSpan
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		switch {
		case retryAdviceLogCallees[name] && !retryAdviceIsFmtOrStatus(sel):
			logSpans = append(logSpans, retryAdviceSpan{call.Pos(), call.End()})
		case retryAdviceAsyncCallees[name] && retryAdviceIsOperations(sel):
			for _, arg := range call.Args {
				lit, isLit := arg.(*ast.FuncLit)
				if !isLit {
					continue
				}
				c.AsyncBodies++
				asyncSpans = append(asyncSpans, retryAdviceSpan{lit.Pos(), lit.End()})
			}
		}
		return true
	})

	seen := map[token.Pos]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		code, lane, found := retryAdviceCodeOf(call)
		if !found {
			return true
		}
		c.Refusals++
		for _, lit := range retryAdviceLiteralsOf(call) {
			if seen[lit.Pos()] {
				continue
			}
			seen[lit.Pos()] = true
			if retryAdviceInAny(logSpans, lit.Pos()) {
				c.LogSkipped++
				continue
			}
			c.Literals++
			async := retryAdviceInAny(asyncSpans, lit.Pos())
			if async {
				c.AsyncLiterals++
			}
			text, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				text = lit.Value
			}
			retryAdviceJudge(c, fset, rel, lit, text, code, lane, async)
		}
		return true
	})
}

// retryAdviceJudge — суждение об одном тексте отказа.
func retryAdviceJudge(c *retryAdviceCensus, fset *token.FileSet, rel string,
	lit *ast.BasicLit, text, code, lane string, async bool) {
	form, ok := retryAdviceFormOf(text)
	if !ok {
		return
	}
	c.Advices++
	c.ByForm[form]++
	c.ByCode[code]++
	if retryAdviceNegated(text) {
		c.Negated++
		return
	}
	var axes []string
	if retryForbiddenCodes[code] {
		axes = append(axes, "A1-код-запрещает-повтор")
	} else if retryAllowedCodes[code] {
		c.OnAllowedCode++
	} else {
		c.OnUnjudgedCode++
	}
	if async {
		axes = append(axes, "A2-асинхронное-тело")
	}
	if len(axes) == 0 {
		return
	}
	c.Findings = append(c.Findings, retryAdviceFinding{
		Rel:  rel,
		Line: fset.Position(lit.Pos()).Line,
		Axis: axes,
		Form: form,
		Lane: lane,
		Text: text,
	})
}

// retryAdviceIsFmtOrStatus — вызов принадлежит `fmt`/`status`/`errors`, а не
// журналу. Нужен потому, что `Errorf` носят и тот и другой, и без различения
// `fmt.Errorf` снимался бы как журнальная строка — то есть предмет гейта уходил
// бы из-под наблюдения целиком.
func retryAdviceIsFmtOrStatus(sel *ast.SelectorExpr) bool {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Name {
	case "fmt", "status", "errors":
		return true
	}
	return false
}

// retryAdviceIsOperations — производитель асинхронного тела назван пакетом
// операций, а не любым `Run` дерева.
func retryAdviceIsOperations(sel *ast.SelectorExpr) bool {
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "operations"
}

// retryAdviceCodeOf — код, который несёт выражение отказа, и то, откуда он
// выведен.
//
// Две формы, и обе живые: аргумент `codes.X` (так пишут `status.Error` и СВОИ
// конструкторы отказа) и сентинел формы `Err<Код>` под `%w`. Конструктор
// сервиса под первую форму подпадает by construction — код он получает
// аргументом, — и потому перечня своих конструкторов здесь нет: перечень
// разошёлся бы с деревом при первом новом.
func retryAdviceCodeOf(call *ast.CallExpr) (code, lane string, ok bool) {
	for _, arg := range call.Args {
		sel, isSel := arg.(*ast.SelectorExpr)
		if !isSel {
			continue
		}
		id, isID := sel.X.(*ast.Ident)
		if !isID {
			continue
		}
		if id.Name == "codes" {
			return sel.Sel.Name, "codes." + sel.Sel.Name, true
		}
		if c, found := retryAdviceCodeOfSentinel(sel.Sel.Name); found {
			return c, id.Name + "." + sel.Sel.Name, true
		}
	}
	for _, arg := range call.Args {
		id, isID := arg.(*ast.Ident)
		if !isID {
			continue
		}
		if c, found := retryAdviceCodeOfSentinel(id.Name); found {
			return c, id.Name, true
		}
	}
	return "", "", false
}

// retryAdviceCodeOfSentinel — код из имени сентинела формы `Err<Код>`.
//
// Имя, остаток которого кодом не является (`ErrConflict`, `ErrInternal`),
// кода НЕ называет и наверх не идёт: см. границу 3 в шапке.
func retryAdviceCodeOfSentinel(name string) (string, bool) {
	rest, cut := strings.CutPrefix(name, "Err")
	if !cut || rest == "" {
		return "", false
	}
	if retryForbiddenCodes[rest] || retryAllowedCodes[rest] {
		return rest, true
	}
	return "", false
}

// retryAdviceLiteralsOf — строковые литералы аргументов выражения отказа,
// включая вложенные `fmt.Sprintf`/`fmt.Errorf`: текст отказа в этом дереве
// собирают и так.
func retryAdviceLiteralsOf(call *ast.CallExpr) []*ast.BasicLit {
	var out []*ast.BasicLit
	for _, arg := range call.Args {
		retryAdviceCollectLiterals(arg, &out, 0)
	}
	return out
}

func retryAdviceCollectLiterals(n ast.Node, out *[]*ast.BasicLit, depth int) {
	if depth > 4 || n == nil {
		return
	}
	switch v := n.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			*out = append(*out, v)
		}
	case *ast.BinaryExpr:
		retryAdviceCollectLiterals(v.X, out, depth+1)
		retryAdviceCollectLiterals(v.Y, out, depth+1)
	case *ast.CallExpr:
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || !retryAdviceIsFmtOrStatus(sel) {
			return
		}
		for _, a := range v.Args {
			retryAdviceCollectLiterals(a, out, depth+1)
		}
	}
}

func retryAdviceInAny(spans []retryAdviceSpan, p token.Pos) bool {
	for _, s := range spans {
		if s.holds(p) {
			return true
		}
	}
	return false
}

// retryAdviceFormOf — распознанная форма совета, если он есть.
func retryAdviceFormOf(text string) (string, bool) {
	for _, f := range retryAdviceForms {
		if f.Re.MatchString(text) {
			return f.Name, true
		}
	}
	return "", false
}

func retryAdviceNegated(text string) bool {
	for _, re := range retryAdviceNegations {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// Describe — находка словами, вместе с тем, что делать.
//
// Диагнозы разные, а не один: «код запрещает повтор» и «повторять нечего, потому
// что запроса уже нет» требуют РАЗНЫХ правок, и находка, называющая симптом
// вместо причины, посылает читателя не туда (`testing.md` §«Гейт на класс», п.8).
func (f retryAdviceFinding) Describe() string {
	return fmt.Sprintf("%s:%d [%s/%s] отказ на полосе %s советует повторить: %q",
		f.Rel, f.Line, strings.Join(f.Axis, "+"), f.Form, f.Lane, f.Text)
}

// loadRetryAdviceSources читает корпус прод-кода Go из дерева.
//
// Корпус — ВСЕ отслеживаемые `*.go` дерева, кроме проб: предмет гейта — текст,
// который уедет к вызывающему, а в пробе такой текст есть ожидание, а не обещание.
func loadRetryAdviceSources(root string) ([]retryAdviceSource, error) {
	paths, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		return nil, fmt.Errorf("состав прод-кода: %w", err)
	}
	var out []retryAdviceSource
	for _, abs := range paths {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			rel = abs
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		body, rerr := readFileString(abs)
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, retryAdviceSource{Rel: rel, Body: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}
