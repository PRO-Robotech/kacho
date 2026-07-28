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
	"sort"
	"strings"
)

type jsKind string

const (
	jsObject jsKind = "an object literal"
	jsArray  jsKind = "an array literal"
	jsString jsKind = "a string literal"
	jsNumber jsKind = "a number literal"
	jsBool   jsKind = "a boolean literal"
	jsNull   jsKind = "a null literal"
	jsIdent  jsKind = "an identifier"
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

func isIdentByte(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '$':
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
			// Подстановка — уже выражение, а не литерал.
			return jsValue{kind: jsOpaque, raw: text[i:end], line: line}, end, nil
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
