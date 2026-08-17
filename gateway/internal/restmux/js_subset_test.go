// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// js_subset_test.go — разбор ПОДМНОЖЕСТВА синтаксиса TS/TSX: ровно столько,
// сколько нужно, чтобы прочитать декларативный реестр ресурсов консоли.
//
// Файл тестовый намеренно: инструмент гейта, не код края.
//
// Что моделируется: объектные и массивные литералы, строки, числа, `true`/
// `false`/`null`, ссылки на константу того же файла. Всё остальное —
// стрелки, вызовы, тернарники, JSX, приведения типов — НЕ моделируется и
// помечается «выражение, которого разбор не понимает». Такое значение можно
// пропустить там, где его содержимое к делу не относится (`render`, `sanitize`,
// `columns`), и НЕЛЬЗЯ пропустить там, где оно относится: вызывающая сторона
// требует конкретного вида и падает, если вид не тот.
//
// Это и есть граница честности разбора. Он не притворяется, что понимает
// произвольный TypeScript; он понимает узкую форму и громко отказывается там,
// где формы нет. Гейт, построенный на «непонятное молча пропустим», перестал бы
// что-либо находить при первом же изменении стиля файла — и не сказал бы об этом.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type jsKind string

const (
	jsObject jsKind = "an object literal"
	jsArray  jsKind = "an array literal"
	jsString jsKind = "a string literal"
	// jsTemplate — шаблонный литерал с подстановкой (`` `${family}_source` ``).
	// Строкой он становится только в известной области видимости, поэтому и
	// хранится отдельно от jsString: «строка, которую ещё некому раскрыть».
	jsTemplate jsKind = "a template literal"
	jsNumber   jsKind = "a number literal"
	jsBool     jsKind = "a boolean literal"
	jsNull     jsKind = "a null literal"
	jsIdent    jsKind = "an identifier"
	// jsSpread — `...expr` элементом массива.
	jsSpread jsKind = "a spread element"
	jsOpaque jsKind = "an expression this scanner does not model"
)

type jsProp struct {
	key  string
	val  jsValue
	line int
}

type jsValue struct {
	kind  jsKind
	str   string // содержимое строки либо имя идентификатора
	boolV bool
	obj   []jsProp
	arr   []jsValue
	raw   string // исходный текст — для непонятого выражения
	line  int
}

func (v jsValue) prop(key string) (jsValue, bool) {
	for _, p := range v.obj {
		if p.key == key {
			return p.val, true
		}
	}
	return jsValue{}, false
}

// jsSource — исходник с индексом строк.
type jsSource struct {
	text   string
	starts []int
}

func newJSSource(text string) *jsSource {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &jsSource{text: text, starts: starts}
}

func (s *jsSource) line(off int) int {
	return sort.SearchInts(s.starts, off+1)
}

// jsTopLevelConsts собирает объявления `const NAME = <value>` / `export const
// NAME = <value>`, стоящие в колонке 0.
//
// Реестр консоли устроен так, что и сам реестр, и переиспользуемые описания
// полей, и константы путей живут именно там. Объявление, чей декларатор — не
// простое имя (деструктуризация), — ошибка, а не пропуск: разбор обязан либо
// понимать файл, либо сказать, что не понимает.
func jsTopLevelConsts(text string) (map[string]jsValue, error) {
	src := newJSSource(text)
	out := make(map[string]jsValue)
	for _, start := range src.starts {
		rest := text[start:]
		decl := rest
		if strings.HasPrefix(decl, "export ") {
			decl = decl[len("export "):]
		}
		if !strings.HasPrefix(decl, "const ") {
			continue
		}
		off := start + (len(rest) - len(decl)) + len("const ")
		name, after := jsReadIdent(text, off)
		if name == "" {
			return nil, fmt.Errorf("line %d: top-level `const` whose declarator is not a plain name", src.line(off))
		}
		eq, err := jsFindAssign(text, after)
		if err != nil {
			return nil, fmt.Errorf("line %d: const %s: %w", src.line(off), name, err)
		}
		v, _, err := jsParseValue(src, eq+1)
		if err != nil {
			return nil, fmt.Errorf("line %d: const %s: %w", src.line(off), name, err)
		}
		out[name] = v
	}
	return out, nil
}

// jsFunc — top-level функция, ТЕЛО которой сводится к «несколько связываний,
// затем возврат массива».
//
// Это единственная форма композиции, которую разбор моделирует, и она выбрана не
// произвольно: реестр выносит в помощник симметричные наборы полей (два
// семейства VIP-источника), различающиеся подставляемым аргументом. Форма
// остаётся ДЕКЛАРАТИВНОЙ — состав набора виден в исходнике целиком, зависит
// только от аргументов вызова и ни от чего больше. Функция с ветвлением,
// циклом или вычислением состава сюда не попадает и, будучи развёрнутой в
// `fields`, ЛОМАЕТ разбор: моделируется узкая форма, всё прочее — громкий отказ.
type jsFunc struct {
	name   string
	params []string
	// locals — связывания `const x = …` до возврата, в порядке объявления.
	locals []jsLocal
	// body — возвращаемое значение: массив полей либо ОДНО поле объектом.
	//
	// Обе формы встречаются в дереве и означают одно и то же — «набор полей,
	// объявленный один раз»: `healthCheckFields()` возвращает массив ветвей
	// проверки живости, `targetsField()` — одно поле-массив целей. Различает их
	// не форма записи, а место употребления: массив разворачивается спредом,
	// объект стоит элементом. Поэтому вид возврата здесь ХРАНИТСЯ, а требование
	// к нему предъявляется в точке употребления, где оно осмысленно.
	body jsValue
	line int
	// file — файл, в котором помощник объявлен. Нужен провенансу: помощник из
	// ОБЩЕГО модуля читается в области видимости СВОЕГО файла, а не того, куда
	// его импортировали, и его объявления полей не считаются в чужом файле.
	file string
}

type jsLocal struct {
	name string
	val  jsValue
}

// jsTopLevelFieldSetFuncs собирает top-level функции описанной выше формы.
//
// Сбор best-effort: файл полон функций-компонентов, и требовать формы от всех
// значило бы сломать разбор на том, что к составу тела отношения не имеет.
// Строгость наступает в точке использования: развёрнут в `fields` помощник,
// которого здесь нет, — отказ с его именем.
//
// `file` проставляется каждому найденному помощнику: он и есть его провенанс.
// Без провенанса помощник из общего модуля читался бы в чужой области видимости
// (там, куда его импортировали), а его собственные соседи — не читались бы вовсе.
func jsTopLevelFieldSetFuncs(file, text string) map[string]jsFunc {
	src := newJSSource(text)
	out := make(map[string]jsFunc)
	for _, start := range src.starts {
		rest := text[start:]
		decl := rest
		if strings.HasPrefix(decl, "export ") {
			decl = decl[len("export "):]
		}
		if !strings.HasPrefix(decl, "function ") {
			continue
		}
		off := start + (len(rest) - len(decl)) + len("function ")
		name, after := jsReadIdent(text, off)
		if name == "" {
			continue
		}
		fn, ok := jsParseFieldSetFunc(src, name, src.line(off), after)
		if !ok {
			continue
		}
		fn.file = file
		out[name] = fn
	}
	return out
}

func jsParseFieldSetFunc(src *jsSource, name string, line, i int) (jsFunc, bool) {
	text := src.text
	i = jsSkipTrivia(text, i)
	if i >= len(text) || text[i] != '(' {
		return jsFunc{}, false
	}
	params, i, ok := jsParseParamNames(text, i)
	if !ok {
		return jsFunc{}, false
	}
	// Между `)` и телом стоит аннотация возвращаемого типа; она к делу не
	// относится, поэтому просто доходим до открывающей скобки тела.
	brace := strings.IndexByte(text[i:], '{')
	if brace < 0 {
		return jsFunc{}, false
	}
	i += brace + 1

	fn := jsFunc{name: name, params: params, line: line}
	for {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			return jsFunc{}, false
		}
		switch {
		case strings.HasPrefix(text[i:], "const "):
			local, next, ok := jsParseLocalConst(src, i+len("const "))
			if !ok {
				return jsFunc{}, false
			}
			fn.locals = append(fn.locals, local)
			i = next
		case strings.HasPrefix(text[i:], "return "):
			v, _, err := jsParseValue(src, i+len("return "))
			if err != nil || (v.kind != jsArray && v.kind != jsObject) {
				return jsFunc{}, false
			}
			fn.body = v
			return fn, true
		default:
			// Любой другой оператор выводит функцию из моделируемой формы.
			return jsFunc{}, false
		}
	}
}

// jsParseParamNames читает имена параметров, пропуская аннотации типов и
// значения по умолчанию. Параметр-деструктуризация формой не поддерживается.
func jsParseParamNames(text string, i int) ([]string, int, bool) {
	depth := 0
	var params []string
	expectName := true
	for i < len(text) {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			return nil, 0, false
		}
		switch c := text[i]; {
		case c == '(' || c == '<' || c == '[' || c == '{':
			if c == '(' && depth == 0 {
				depth++
				i++
				continue
			}
			if c == '{' || c == '[' {
				return nil, 0, false // деструктуризация — вне формы
			}
			depth++
			i++
		case c == ')':
			depth--
			i++
			if depth == 0 {
				return params, i, true
			}
		case c == '>':
			depth--
			i++
		case c == ',':
			expectName = depth == 1
			i++
		case c == '"' || c == '\'':
			end, err := jsSkipQuoted(text, i)
			if err != nil {
				return nil, 0, false
			}
			i = end
		case isIdentByte(c, true):
			name, end := jsReadIdent(text, i)
			if expectName && depth == 1 {
				params = append(params, name)
				expectName = false
			}
			i = end
		default:
			i++
		}
	}
	return nil, 0, false
}

func jsParseLocalConst(src *jsSource, i int) (jsLocal, int, bool) {
	text := src.text
	name, after := jsReadIdent(text, i)
	if name == "" {
		return jsLocal{}, 0, false
	}
	eq, err := jsFindAssign(text, after)
	if err != nil {
		return jsLocal{}, 0, false
	}
	v, end, err := jsParseValue(src, eq+1)
	if err != nil {
		return jsLocal{}, 0, false
	}
	if end < len(text) && text[end] == ';' {
		end++
	}
	return jsLocal{name: name, val: v}, end, true
}

// jsCall — разобранный вызов `helper(arg, …)` с литеральными аргументами.
type jsCall struct {
	callee string
	args   []jsValue
}

// jsParseCall разбирает вызов. Аргумент, не являющийся литералом, — отказ:
// состав набора обязан быть виден в исходнике, а не вычисляться.
func jsParseCall(raw string) (jsCall, error) {
	src := newJSSource(raw)
	name, i := jsReadIdent(raw, 0)
	if name == "" {
		return jsCall{}, fmt.Errorf("%s is not a call to a named helper", jsExcerpt(raw))
	}
	i = jsSkipTrivia(raw, i)
	if i >= len(raw) || raw[i] != '(' {
		return jsCall{}, fmt.Errorf("%s is not a call to a named helper", jsExcerpt(raw))
	}
	call := jsCall{callee: name}
	i++
	for {
		i = jsSkipTrivia(raw, i)
		if i >= len(raw) {
			return jsCall{}, fmt.Errorf("unterminated call %s", jsExcerpt(raw))
		}
		if raw[i] == ')' {
			return call, nil
		}
		if raw[i] == ',' {
			i++
			continue
		}
		v, end, err := jsParseValue(src, i)
		if err != nil {
			return jsCall{}, err
		}
		switch v.kind {
		case jsString, jsNumber, jsBool, jsNull:
		default:
			return jsCall{}, fmt.Errorf("argument %d of %s(…) is %s, expected a literal: a set of fields that depends on a computed argument is not declared anywhere", len(call.args)+1, name, v.kind)
		}
		call.args = append(call.args, v)
		i = end
	}
}

// jsTemplateSubstitution — `${…}` внутри шаблонного литерала.
var jsTemplateSubstitution = regexp.MustCompile(`\$\{([^{}]*)\}`)

// jsResolveTemplate раскрывает шаблонный литерал в области видимости.
//
// Подставляется ТОЛЬКО голое имя, разрешающееся в строку: любое выражение внутри
// `${…}` означает, что имя поля вычисляется, а вычисленного имени поля в
// контракте нет — такое честнее отвергнуть, чем угадать.
func jsResolveTemplate(raw string, scope func(string) (string, bool)) (string, error) {
	var bad string
	out := jsTemplateSubstitution.ReplaceAllStringFunc(raw, func(m string) string {
		expr := strings.TrimSpace(jsTemplateSubstitution.FindStringSubmatch(m)[1])
		v, ok := scope(expr)
		if !ok {
			if bad == "" {
				bad = expr
			}
			return m
		}
		return v
	})
	if bad != "" {
		return "", fmt.Errorf("template substitution ${%s} does not resolve to a string in scope", bad)
	}
	return out, nil
}

// jsFindAssign находит `=` инициализатора, пропуская аннотацию типа. Внутри
// аннотации скобки/угловые могут быть вложены, но `=` там не встречается:
// дженерик-дефолты в объявлении переменной невозможны.
func jsFindAssign(text string, i int) (int, error) {
	for i < len(text) {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			break
		}
		switch text[i] {
		case '=':
			if i+1 < len(text) && (text[i+1] == '=' || text[i+1] == '>') {
				return 0, fmt.Errorf("unexpected %q before the initializer", text[i:i+2])
			}
			return i, nil
		case ';', '\n':
			return 0, fmt.Errorf("declaration without an initializer")
		default:
			i++
		}
	}
	return 0, fmt.Errorf("declaration without an initializer")
}

func jsReadIdent(text string, i int) (string, int) {
	start := i
	for i < len(text) && isIdentByte(text[i], i == start) {
		i++
	}
	return text[start:i], i
}

// isIdentByte — алфавит ИМЁН разбираемого подмножества.
//
// Не-ASCII байт (>= 0x80) считается частью имени намеренно. Причина не в
// снисходительности: имена с буквами вне латиницы — законный JS/TS и здешний
// стиль (в дереве консоли таких имён 73 — `const родитель`, `function
// отметить`). Байтовый алфавит отвергал их молча и не там, где написано: имя
// читалось пустым, помощник не попадал в область видимости, и разбор падал
// сообщением «это не помощник ЭТОГО файла» — то есть указывал на форму, тогда
// как форма была верной, а не по зубам оказалась БУКВА.
//
// Гейт от этого не слабеет: его предмет — ФОРМА набора полей (тело помощника
// `const…; return [ … ]`, литеральные аргументы, видимость состава в исходнике),
// а не то, каким алфавитом записаны имена. Разбор по-прежнему ломается на всём,
// что за пределами формы, — просто перестаёт ломаться на том, что внутри неё.
// Тот же довод уже записан у `expandConsoleFieldSpread`: гейт не навязывает
// стиль зоне, которой не владеет.
func isIdentByte(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '$':
		return true
	case c >= 0x80:
		return true
	case c >= '0' && c <= '9':
		return !first
	}
	return false
}

// jsParseValue разбирает значение, начинающееся с позиции i, и возвращает
// индекс сразу за ним.
//
// Литерал признаётся литералом ТОЛЬКО если он занимает выражение целиком: за
// объектом `{...}`, который на деле лишь ветка тернарника или операнд `??`,
// следует не разделитель, и такое значение честнее считать непонятым, чем
// выдать его часть за целое.
func jsParseValue(src *jsSource, i int) (jsValue, int, error) {
	text := src.text
	i = jsSkipTrivia(text, i)
	if i >= len(text) {
		return jsValue{}, 0, fmt.Errorf("unexpected end of file where a value was expected")
	}
	line := src.line(i)
	start := i

	structural := func(v jsValue, end int) (jsValue, int, error) {
		j := jsSkipTrivia(text, end)
		if j < len(text) && !jsIsValueTerminator(text[j]) {
			// Литерал оказался частью большего выражения.
			opaqueEnd, err := jsScanExpr(text, start)
			if err != nil {
				return jsValue{}, 0, err
			}
			return jsValue{kind: jsOpaque, raw: text[start:opaqueEnd], line: line}, opaqueEnd, nil
		}
		return v, end, nil
	}

	switch c := text[i]; {
	case c == '{':
		props, end, err := jsParseObject(src, i)
		if err != nil {
			return jsValue{}, 0, err
		}
		return structural(jsValue{kind: jsObject, obj: props, line: line}, end)
	case c == '[':
		elems, end, err := jsParseArray(src, i)
		if err != nil {
			return jsValue{}, 0, err
		}
		return structural(jsValue{kind: jsArray, arr: elems, line: line}, end)
	case c == '"' || c == '\'':
		s, end, err := jsParseString(text, i)
		if err != nil {
			return jsValue{}, 0, err
		}
		return structural(jsValue{kind: jsString, str: s, line: line}, end)
	case c == '`':
		end, err := jsSkipTemplate(text, i)
		if err != nil {
			return jsValue{}, 0, err
		}
		body := text[i+1 : end-1]
		if strings.Contains(body, "${") {
			// Строкой станет только в области видимости, где известны
			// подставляемые имена; до тех пор — шаблон, не строка.
			return structural(jsValue{kind: jsTemplate, raw: body, line: line}, end)
		}
		return structural(jsValue{kind: jsString, str: body, line: line}, end)
	case c >= '0' && c <= '9', c == '-' || c == '+':
		end, err := jsScanExpr(text, i)
		if err != nil {
			return jsValue{}, 0, err
		}
		return jsValue{kind: jsNumber, raw: strings.TrimSpace(text[i:end]), line: line}, end, nil
	case isIdentByte(c, true):
		name, end := jsReadIdent(text, i)
		j := jsSkipTrivia(text, end)
		if j < len(text) && !jsIsValueTerminator(text[j]) {
			opaqueEnd, err := jsScanExpr(text, start)
			if err != nil {
				return jsValue{}, 0, err
			}
			return jsValue{kind: jsOpaque, raw: text[start:opaqueEnd], line: line}, opaqueEnd, nil
		}
		switch name {
		case "true", "false":
			return jsValue{kind: jsBool, boolV: name == "true", line: line}, end, nil
		case "null", "undefined":
			return jsValue{kind: jsNull, line: line}, end, nil
		}
		return jsValue{kind: jsIdent, str: name, line: line}, end, nil
	default:
		end, err := jsScanExpr(text, i)
		if err != nil {
			return jsValue{}, 0, err
		}
		return jsValue{kind: jsOpaque, raw: text[i:end], line: line}, end, nil
	}
}

func jsIsValueTerminator(c byte) bool {
	switch c {
	case ',', '}', ']', ')', ';':
		return true
	}
	return false
}

func jsParseObject(src *jsSource, i int) ([]jsProp, int, error) {
	text := src.text
	i++ // '{'
	var props []jsProp
	for {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			return nil, 0, fmt.Errorf("line %d: unterminated object literal", src.line(i-1))
		}
		if text[i] == '}' {
			return props, i + 1, nil
		}
		if text[i] == ',' {
			i++
			continue
		}
		keyLine := src.line(i)
		var key string
		switch c := text[i]; {
		case c == '"' || c == '\'':
			s, end, err := jsParseString(text, i)
			if err != nil {
				return nil, 0, err
			}
			key, i = s, end
		case c == '.':
			return nil, 0, fmt.Errorf("line %d: spread inside an object literal is not modelled", keyLine)
		case isIdentByte(c, true):
			key, i = jsReadIdent(text, i)
		default:
			return nil, 0, fmt.Errorf("line %d: object key starting with %q is not modelled", keyLine, string(c))
		}
		i = jsSkipTrivia(text, i)
		if i >= len(text) || text[i] != ':' {
			return nil, 0, fmt.Errorf("line %d: shorthand or computed property %q is not modelled", keyLine, key)
		}
		v, end, err := jsParseValue(src, i+1)
		if err != nil {
			return nil, 0, err
		}
		props = append(props, jsProp{key: key, val: v, line: keyLine})
		i = end
	}
}

func jsParseArray(src *jsSource, i int) ([]jsValue, int, error) {
	text := src.text
	i++ // '['
	var elems []jsValue
	for {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			return nil, 0, fmt.Errorf("line %d: unterminated array literal", src.line(i-1))
		}
		if text[i] == ']' {
			return elems, i + 1, nil
		}
		if text[i] == ',' {
			i++
			continue
		}
		if strings.HasPrefix(text[i:], "...") {
			line := src.line(i)
			end, err := jsScanExpr(text, i+3)
			if err != nil {
				return nil, 0, err
			}
			elems = append(elems, jsValue{kind: jsSpread, raw: strings.TrimSpace(text[i+3 : end]), line: line})
			i = end
			continue
		}
		v, end, err := jsParseValue(src, i)
		if err != nil {
			return nil, 0, err
		}
		elems = append(elems, v)
		i = end
	}
}

// arrowReturnedObject достаёт объектный литерал, который возвращает стрелка
// вида `(…) => ({…})`. Форма другая — ошибка: `template` решает состав тела
// создания, и «не разобрал, ну и ладно» здесь означало бы не проверить ресурс.
func arrowReturnedObject(v jsValue) (jsValue, error) {
	if v.kind == jsObject {
		return v, nil
	}
	if v.kind != jsOpaque {
		return jsValue{}, fmt.Errorf("is %s, expected an arrow returning an object literal", v.kind)
	}
	raw := v.raw
	arrow := jsFindArrow(raw)
	if arrow < 0 {
		return jsValue{}, fmt.Errorf("is not an arrow function: %s", jsExcerpt(raw))
	}
	src := newJSSource(raw)
	i := jsSkipTrivia(raw, arrow+2)
	if i >= len(raw) || raw[i] != '(' {
		return jsValue{}, fmt.Errorf("arrow does not return a parenthesised object literal: %s", jsExcerpt(raw))
	}
	body, _, err := jsParseValue(src, i+1)
	if err != nil {
		return jsValue{}, err
	}
	if body.kind != jsObject {
		return jsValue{}, fmt.Errorf("arrow returns %s, expected an object literal: %s", body.kind, jsExcerpt(raw))
	}
	return body, nil
}

func jsExcerpt(raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	if len(raw) > 80 {
		return raw[:80] + "…"
	}
	return raw
}

// jsFindArrow ищет `=>` вне строк, комментариев и вложенных скобок.
func jsFindArrow(text string) int {
	depth := 0
	prev := byte(0)
	for i := 0; i < len(text); {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			return -1
		}
		switch c := text[i]; {
		case c == '(' || c == '[' || c == '{':
			depth++
			prev = c
			i++
		case c == ')' || c == ']' || c == '}':
			depth--
			prev = c
			i++
		case c == '"' || c == '\'':
			j, err := jsSkipQuoted(text, i)
			if err != nil {
				return -1
			}
			i, prev = j, '"'
		case c == '`':
			j, err := jsSkipTemplate(text, i)
			if err != nil {
				return -1
			}
			i, prev = j, '`'
		case c == '/' && jsRegexAllowedAfter(prev):
			j, err := jsSkipRegex(text, i)
			if err != nil {
				return -1
			}
			i, prev = j, '/'
		case c == '=' && i+1 < len(text) && text[i+1] == '>' && depth == 0:
			return i
		default:
			prev = c
			i++
		}
	}
	return -1
}

// jsScanExpr возвращает индекс первого символа ПОСЛЕ выражения, начинающегося
// в i: скобки считаются, строки/шаблоны/регулярки/комментарии пропускаются.
func jsScanExpr(text string, i int) (int, error) {
	depth := 0
	prev := byte(0)
	for i < len(text) {
		i = jsSkipTrivia(text, i)
		if i >= len(text) {
			break
		}
		c := text[i]
		switch {
		case c == '(' || c == '[' || c == '{':
			depth++
			prev = c
			i++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return i, nil
			}
			depth--
			prev = c
			i++
		case (c == ',' || c == ';') && depth == 0:
			return i, nil
		case c == '"' || c == '\'':
			j, err := jsSkipQuoted(text, i)
			if err != nil {
				return 0, err
			}
			i, prev = j, '"'
		case c == '`':
			j, err := jsSkipTemplate(text, i)
			if err != nil {
				return 0, err
			}
			i, prev = j, '`'
		case c == '/' && jsRegexAllowedAfter(prev):
			j, err := jsSkipRegex(text, i)
			if err != nil {
				return 0, err
			}
			i, prev = j, '/'
		default:
			prev = c
			i++
		}
	}
	return 0, fmt.Errorf("unterminated expression")
}

// jsRegexAllowedAfter — позиция, в которой `/` начинает регулярное выражение, а
// не деление.
//
// `}`, `<` и `>` в список НЕ входят намеренно: в TSX за ними стоит закрытие
// JSX-тега (`… value={x} />`, `</span>`), и трактовка такого `/` как регулярки
// съела бы половину файла — молча и до конца строки. Регулярка сразу после
// блока или сравнения — конструкция, которой в декларативном реестре не бывает.
func jsRegexAllowedAfter(prev byte) bool {
	switch prev {
	case 0, '(', ',', '=', ':', '[', '!', '&', '|', '?', '{', ';':
		return true
	}
	return false
}

// jsSkipTrivia пропускает пробелы и комментарии обоих видов.
func jsSkipTrivia(text string, i int) int {
	for i < len(text) {
		c := text[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return len(text)
			}
			i += 2 + end + 2
		default:
			return i
		}
	}
	return i
}

// jsParseString читает строковый литерал и возвращает его СОДЕРЖИМОЕ.
func jsParseString(text string, i int) (string, int, error) {
	quote := text[i]
	var b strings.Builder
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			if j+1 >= len(text) {
				return "", 0, fmt.Errorf("unterminated string literal")
			}
			b.WriteByte(text[j+1])
			j++
		case quote:
			return b.String(), j + 1, nil
		default:
			b.WriteByte(text[j])
		}
	}
	return "", 0, fmt.Errorf("unterminated string literal")
}

func jsSkipQuoted(text string, i int) (int, error) {
	_, end, err := jsParseString(text, i)
	return end, err
}

// jsSkipTemplate пропускает шаблонный литерал вместе с вложенными `${…}`.
func jsSkipTemplate(text string, i int) (int, error) {
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '`':
			return j + 1, nil
		case '$':
			if j+1 < len(text) && text[j+1] == '{' {
				depth := 1
				k := j + 2
				for k < len(text) && depth > 0 {
					switch text[k] {
					case '{':
						depth++
					case '}':
						depth--
					case '`':
						end, err := jsSkipTemplate(text, k)
						if err != nil {
							return 0, err
						}
						k = end - 1
					}
					k++
				}
				j = k - 1
			}
		}
	}
	return 0, fmt.Errorf("unterminated template literal")
}

func jsSkipRegex(text string, i int) (int, error) {
	inClass := false
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '\n':
			return 0, fmt.Errorf("unterminated regular expression literal")
		case '/':
			if inClass {
				continue
			}
			j++
			for j < len(text) && isIdentByte(text[j], false) {
				j++
			}
			return j, nil
		}
	}
	return 0, fmt.Errorf("unterminated regular expression literal")
}
