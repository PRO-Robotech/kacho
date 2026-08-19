// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellsource_test.go — разбор shell-исходника: слово, кавычки, перенаправление,
// документ-вставка, определение функции.
//
// Отдельный файл, потому что предмет отдельный: здесь только «что здесь
// написано», без единого суждения о том, что из этого следует. Суждение живёт в
// `shellprobewriteslivetree_test.go` (гейт изоляции shell-проб, #724), а
// разрешение путей — в `shellpathresolve_test.go`. Пока они лежали одним файлом,
// тронуть разбор было нельзя, не прочитав двух тысяч строк.
//
// # Почему разбор, а не поиск по тексту
//
// Гейт обязан читать ИСПОЛНЯЕМОЕ. Комментарий, поясняющий запрещённую форму,
// нарушением не является, а текстовый поиск не отличает его от вызова — и
// одинаково находит форму в шапке скрипта, в строке помощи и в собственном
// объяснении запрета. Здесь комментарии оболочки снимаются на уровне лексера,
// одинарные кавычки не раскрываются, а тело документа-вставки не разбирается как
// shell: оно принадлежит чужому языку и рассматривается своим порядком.
//
// # Чего разбор НЕ делает
//
// Он не строит поток управления: `if`, `case`, `for` остаются словами. Гейту
// нужны присваивания, вызовы, цели перенаправлений и границы функций — порядка
// исходника для этого достаточно, а полноценный интерпретатор оболочки был бы
// кодом, который нечем проверить на дереве.
package repohygiene

import (
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Разбор shell: слово, команда, документ-вставка.
// ─────────────────────────────────────────────────────────────────────────────

// shWord — слово команды. Хранится и «сырой» вид (для сообщения человеку), и
// «раскрываемая» часть — та, что вне одинарных кавычек: подстановки внутри `'…'`
// оболочка не выполняет, и считать их ссылкой на переменную значило бы читать
// текст вместо исполняемого.
type shWord struct {
	raw string
	exp string
	lit string // буквальное значение, если слово целиком литерально (флаги, имена команд)
}

type shCmd struct {
	line    int
	fn      string // функция, в чьём теле стоит команда
	words   []shWord
	redirs  []shWord // цели перенаправлений ВЫВОДА
	heredoc string   // тело документа-вставки, привязанного к команде
}

type shParser struct {
	src  string
	i    int
	line int

	cmds []shCmd
	cur  shCmd

	raw, exp, lit strings.Builder
	inWord        bool
	litOK         bool

	depth     int      // глубина фигурных скобок
	fnStack   []string // имена функций
	fnDepth   []int    // глубина, на которой каждая открылась
	pendingFn string   // «name()» встречено, ждём «{»

	pendingHD []shHeredoc
	funcs     int

	// unterminated — цитата или подстановка ушла в конец файла. Признак
	// РАССИНХРОНА лексера: дальше он читает код как строку и наоборот, а
	// «команд не найдено» становится неотличимо от «команд нет».
	unterminated bool
}

type shHeredoc struct {
	delim  string
	strip  bool // <<- : у терминатора срезаются табуляции
	cmdIdx int  // индекс команды, к которой привязано тело (в s.cmds)
}

func shellParse(src string) ([]shCmd, int) {
	cmds, funcs, _ := shellParseChecked(src)
	return cmds, funcs
}

// shellParseChecked — то же, плюс признак рассинхрона.
func shellParseChecked(src string) ([]shCmd, int, bool) {
	p := &shParser{src: src, line: 1, litOK: true}
	p.run()
	return p.cmds, p.funcs, p.unterminated
}

func (p *shParser) run() {
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == '\\' && p.i+1 < len(p.src):
			// Экранирование: следующий символ — буквальный. Перенос строки
			// склеивает строки и словом не заканчивается.
			if p.src[p.i+1] == '\n' {
				p.line++
				p.i += 2
				continue
			}
			p.startWord()
			p.raw.WriteByte(c)
			p.raw.WriteByte(p.src[p.i+1])
			p.lit.WriteByte(p.src[p.i+1])
			p.exp.WriteByte(p.src[p.i+1])
			p.i += 2
		case c == '\'':
			p.startWord()
			p.i++
			p.raw.WriteByte('\'')
			for p.i < len(p.src) && p.src[p.i] != '\'' {
				if p.src[p.i] == '\n' {
					p.line++
				}
				p.raw.WriteByte(p.src[p.i])
				p.lit.WriteByte(p.src[p.i])
				p.i++
			}
			if p.i < len(p.src) {
				p.raw.WriteByte('\'')
				p.i++
			} else {
				p.unterminated = true
			}
		case c == '"':
			p.startWord()
			p.i++
			p.raw.WriteByte('"')
			for p.i < len(p.src) && p.src[p.i] != '"' {
				if p.src[p.i] == '\\' && p.i+1 < len(p.src) {
					p.raw.WriteString(p.src[p.i : p.i+2])
					p.exp.WriteByte(p.src[p.i+1])
					p.lit.WriteByte(p.src[p.i+1])
					p.i += 2
					continue
				}
				if p.src[p.i] == '\n' {
					p.line++
				}
				if p.atSubstitution() {
					// ЗДЕСЬ лексер и терял синхронизацию. Подстановка внутри
					// строки возвращает полный разбор оболочки, поэтому кавычка
					// внутри неё строкой быть не обязана: в
					// `"$(tr -d '"')"` закрывающая кавычка строки стоит ПОСЛЕ
					// подстановки, а побайтовый поиск ближайшей `"` брал ту, что
					// внутри `'…'`, — и дальше весь файл читался наизнанку.
					p.litOK = false
					st := p.i
					p.skipSubstituted()
					seg := p.src[st:p.i]
					p.raw.WriteString(seg)
					p.exp.WriteString(seg)
					p.lit.WriteString(seg)
					continue
				}
				p.raw.WriteByte(p.src[p.i])
				p.exp.WriteByte(p.src[p.i])
				p.lit.WriteByte(p.src[p.i])
				p.i++
			}
			if p.i < len(p.src) {
				p.raw.WriteByte('"')
				p.i++
			} else {
				p.unterminated = true
			}
		case c == '#' && !p.inWord:
			// Комментарий. Именно здесь гейт перестаёт читать текст: упоминание
			// формы в шапке скрипта вызовом не является.
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
		case c == '\n':
			p.endWord()
			p.endCmd()
			p.i++
			p.line++
			p.consumeHeredocs()
		case c == ' ' || c == '\t' || c == '\r':
			p.endWord()
			p.i++
		case c == ';' || c == '&' || c == '|':
			if c == '&' && p.i+1 < len(p.src) && p.src[p.i+1] == '>' {
				// bash-форма `&>файл`.
				p.endWord()
				p.i += 2
				if p.i < len(p.src) && p.src[p.i] == '>' {
					p.i++
				}
				p.readRedirTarget()
				continue
			}
			p.endWord()
			p.endCmd()
			p.i++
			for p.i < len(p.src) && (p.src[p.i] == c) {
				p.i++
			}
		case c == '(':
			// «name()» — определение функции; иначе подоболочка. Слово с именем
			// к этому моменту ещё не закрыто (скобка идёт вплотную), поэтому
			// закрываем его сами и лишь потом решаем.
			if p.i+1 < len(p.src) && p.src[p.i+1] == ')' {
				p.endWord()
				if len(p.cur.words) == 1 && p.cur.words[0].lit != "" {
					p.pendingFn = p.cur.words[0].lit
					p.cur.words = nil
					p.i += 2
					continue
				}
				if len(p.cur.words) == 2 && p.cur.words[0].lit == "function" &&
					p.cur.words[1].lit != "" {
					p.pendingFn = p.cur.words[1].lit
					p.cur.words = nil
					p.i += 2
					continue
				}
			}
			p.endWord()
			p.endCmd()
			p.i++
		case c == ')':
			p.endWord()
			p.endCmd()
			p.i++
		case c == '{':
			if !p.inWord {
				p.endWord()
				// bash-форма `function name {` — без круглых скобок.
				if p.pendingFn == "" && len(p.cur.words) == 2 &&
					p.cur.words[0].lit == "function" && p.cur.words[1].lit != "" {
					p.pendingFn = p.cur.words[1].lit
					p.cur.words = nil
				}
				p.endCmd()
				p.depth++
				if p.pendingFn != "" {
					p.fnStack = append(p.fnStack, p.pendingFn)
					p.fnDepth = append(p.fnDepth, p.depth)
					p.pendingFn = ""
					p.funcs++
				}
				p.i++
				continue
			}
			p.startWord()
			p.raw.WriteByte(c)
			p.exp.WriteByte(c)
			p.lit.WriteByte(c)
			p.i++
		case c == '}':
			if !p.inWord {
				p.endWord()
				p.endCmd()
				if n := len(p.fnDepth); n > 0 && p.fnDepth[n-1] == p.depth {
					p.fnStack = p.fnStack[:n-1]
					p.fnDepth = p.fnDepth[:n-1]
				}
				if p.depth > 0 {
					p.depth--
				}
				p.i++
				continue
			}
			p.startWord()
			p.raw.WriteByte(c)
			p.exp.WriteByte(c)
			p.lit.WriteByte(c)
			p.i++
		case c == '<':
			p.endWord()
			if strings.HasPrefix(p.src[p.i:], "<<<") {
				p.i += 3 // строка-вставка: чтение, не запись
				continue
			}
			if strings.HasPrefix(p.src[p.i:], "<<") {
				p.i += 2
				p.readHeredocDelim()
				continue
			}
			p.i++
			p.skipWordRaw() // перенаправление ВВОДА — чтение
		case c == '>':
			p.endWord()
			p.i++
			if p.i < len(p.src) && p.src[p.i] == '>' {
				p.i++
			}
			if p.i < len(p.src) && p.src[p.i] == '|' {
				p.i++
			}
			if p.i < len(p.src) && p.src[p.i] == '&' {
				// Дублирование дескриптора (`2>&1`) — файла здесь нет.
				p.i++
				p.skipWordRaw()
				continue
			}
			p.readRedirTarget()
		case c == '$':
			p.startWord()
			p.litOK = false
			p.consumeDollar()
		case c == '`':
			// Внутри обратных кавычек — код оболочки, поэтому и здесь границу
			// ищет общий пропускатель: свой побайтовый цикл ломался на `'"'`
			// ровно так же, как ломался он внутри двойных кавычек.
			p.startWord()
			p.litOK = false
			st := p.i
			p.skipBackquoted()
			seg := p.src[st:p.i]
			p.raw.WriteString(seg)
			p.exp.WriteString(seg)
		default:
			p.startWord()
			p.raw.WriteByte(c)
			p.exp.WriteByte(c)
			p.lit.WriteByte(c)
			p.i++
		}
	}
	p.endWord()
	p.endCmd()
}

func (p *shParser) startWord() {
	if !p.inWord {
		p.inWord = true
		p.litOK = true
		if p.cur.line == 0 {
			p.cur.line = p.line
		}
	}
}

func (p *shParser) endWord() {
	if !p.inWord {
		return
	}
	w := shWord{raw: p.raw.String(), exp: p.exp.String()}
	if p.litOK {
		w.lit = p.lit.String()
	}
	p.cur.words = append(p.cur.words, w)
	p.raw.Reset()
	p.exp.Reset()
	p.lit.Reset()
	p.inWord = false
	p.litOK = true
}

func (p *shParser) endCmd() {
	if len(p.cur.words) == 0 && len(p.cur.redirs) == 0 {
		p.cur = shCmd{}
		return
	}
	if p.cur.line == 0 {
		p.cur.line = p.line
	}
	if n := len(p.fnStack); n > 0 {
		p.cur.fn = p.fnStack[n-1]
	}
	p.cmds = append(p.cmds, p.cur)
	p.cur = shCmd{}
}

// readRedirTarget — слово после `>`/`>>` есть ЦЕЛЬ записи.
func (p *shParser) readRedirTarget() {
	for p.i < len(p.src) && (p.src[p.i] == ' ' || p.src[p.i] == '\t') {
		p.i++
	}
	save := p.cur.words
	p.cur.words = nil
	p.readOneWord()
	if len(p.cur.words) == 1 {
		p.cur.redirs = append(p.cur.redirs, p.cur.words[0])
	}
	p.cur.words = save
}

// readOneWord — прочитать ровно одно слово (для целей перенаправления).
func (p *shParser) readOneWord() {
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == ' ' || c == '\t' || c == '\n' || c == ';' || c == '&' || c == '|' ||
			c == '(' || c == ')' || c == '<' || c == '>' {
			break
		}
		switch c {
		case '\'':
			p.startWord()
			p.i++
			p.raw.WriteByte('\'')
			for p.i < len(p.src) && p.src[p.i] != '\'' {
				if p.src[p.i] == '\n' {
					p.line++
				}
				p.raw.WriteByte(p.src[p.i])
				p.lit.WriteByte(p.src[p.i])
				p.i++
			}
			if p.i < len(p.src) {
				p.raw.WriteByte('\'')
				p.i++
			} else {
				p.unterminated = true
			}
		case '"':
			p.startWord()
			p.i++
			p.raw.WriteByte('"')
			for p.i < len(p.src) && p.src[p.i] != '"' {
				if p.atSubstitution() {
					p.litOK = false
					st := p.i
					p.skipSubstituted()
					seg := p.src[st:p.i]
					p.raw.WriteString(seg)
					p.exp.WriteString(seg)
					p.lit.WriteString(seg)
					continue
				}
				if p.src[p.i] == '\n' {
					p.line++
				}
				p.raw.WriteByte(p.src[p.i])
				p.exp.WriteByte(p.src[p.i])
				p.lit.WriteByte(p.src[p.i])
				p.i++
			}
			if p.i < len(p.src) {
				p.raw.WriteByte('"')
				p.i++
			} else {
				p.unterminated = true
			}
		case '$':
			p.startWord()
			p.litOK = false
			p.consumeDollar()
		default:
			p.startWord()
			p.raw.WriteByte(c)
			p.exp.WriteByte(c)
			p.lit.WriteByte(c)
			p.i++
		}
	}
	p.endWord()
}

// skipWordRaw — пропустить слово, ничего не запоминая.
func (p *shParser) skipWordRaw() {
	for p.i < len(p.src) && (p.src[p.i] == ' ' || p.src[p.i] == '\t') {
		p.i++
	}
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == ' ' || c == '\t' || c == '\n' || c == ';' || c == '&' || c == '|' ||
			c == '(' || c == ')' || c == '<' || c == '>' {
			return
		}
		p.i++
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Пропуск вложенных конструкций — ОДНО место, где записаны правила вложенности
// оболочки.
//
// Функции ниже только двигают позицию (и счётчик строк), а вызывающий сам
// решает, в какие буферы положить пройденный кусок. Единственный автор правил
// нужен не ради красоты: три побайтовых цикла, искавших закрывающую кавычку
// каждый по-своему, разошлись между собой молча — и разошлись там, где
// расхождение не видно, потому что все три возвращают «прочитано» на любом
// входе.
//
// Признак ошибки здесь — не «нашёл не то», а РАССИНХРОН: кавычка, ушедшая до
// конца файла, переворачивает чтение наизнанку, и дальше код читается как
// строка, а строка как код. Поэтому каждая функция, дойдя до конца исходника с
// незакрытой конструкцией, поднимает `unterminated`: гейт обязан отличать «в
// этом скрипте записи нет» от «этот скрипт не прочитан».
// ─────────────────────────────────────────────────────────────────────────────

// atSubstitution — стоим ли на начале конструкции, внутри которой снова
// действуют правила оболочки: `$(…)`, `${…}`, “ `…` “.
func (p *shParser) atSubstitution() bool {
	if p.i >= len(p.src) {
		return false
	}
	if p.src[p.i] == '`' {
		return true
	}
	return p.src[p.i] == '$' && p.i+1 < len(p.src) &&
		(p.src[p.i+1] == '(' || p.src[p.i+1] == '{')
}

// skipSubstituted — пройти конструкцию, на начале которой мы стоим.
func (p *shParser) skipSubstituted() {
	if p.src[p.i] == '`' {
		p.skipBackquoted()
		return
	}
	p.i++ // '$'
	p.skipSubstitution()
}

// skipSingleQuoted — стоим на открывающей `'`. Внутри неё оболочка не делает
// ничего: ни подстановок, ни экранирования, — поэтому и разбирать нечего.
func (p *shParser) skipSingleQuoted() {
	p.i++
	for p.i < len(p.src) {
		if p.src[p.i] == '\'' {
			p.i++
			return
		}
		if p.src[p.i] == '\n' {
			p.line++
		}
		p.i++
	}
	p.unterminated = true
}

// skipDoubleQuoted — стоим на открывающей `"`. Одиночная кавычка внутри неё —
// обычный символ, а вот подстановка возвращает полный разбор.
func (p *shParser) skipDoubleQuoted() {
	p.i++
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '\\' && p.i+1 < len(p.src):
			if p.src[p.i+1] == '\n' {
				p.line++
			}
			p.i += 2
		case c == '"':
			p.i++
			return
		case p.atSubstitution():
			p.skipSubstituted()
		case c == '\n':
			p.line++
			p.i++
		default:
			p.i++
		}
	}
	p.unterminated = true
}

// skipBackquoted — стоим на открывающей “ ` “. Внутри — код оболочки.
func (p *shParser) skipBackquoted() {
	p.i++
	for p.i < len(p.src) {
		switch c := p.src[p.i]; {
		case c == '\\' && p.i+1 < len(p.src):
			if p.src[p.i+1] == '\n' {
				p.line++
			}
			p.i += 2
		case c == '`':
			p.i++
			return
		case c == '\'':
			p.skipSingleQuoted()
		case c == '"':
			p.skipDoubleQuoted()
		case c == '$' && p.i+1 < len(p.src) && (p.src[p.i+1] == '(' || p.src[p.i+1] == '{'):
			p.skipSubstituted()
		case c == '\n':
			p.line++
			p.i++
		default:
			p.i++
		}
	}
	p.unterminated = true
}

// skipSubstitution — стоим на открывающей скобке подстановки: `(` у `$(…)` и
// `$((…))`, `{` у `${…}`. Знак `$` вызывающий уже прошёл.
//
// Кавычки внутри читаются кавычками ТОЛЬКО у круглой формы: `$(…)` — это код, и
// `$(tr -d '"')` обязан читаться как код. У `${…}` подстановка кончается парной
// `}` (скобки считаются ради `${x:-${y}}`), а кавычки внутри трогать нельзя:
// иначе `${x//'/_}` — где одиночная кавычка есть ДАННЫЕ — увела бы разбор до
// конца файла, то есть лечение вернуло бы ту самую болезнь.
func (p *shParser) skipSubstitution() {
	if p.i >= len(p.src) {
		p.unterminated = true
		return
	}
	open := p.src[p.i]
	closing := byte(')')
	if open == '{' {
		closing = '}'
	}
	depth := 0
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == '\\' && p.i+1 < len(p.src):
			if p.src[p.i+1] == '\n' {
				p.line++
			}
			p.i += 2
		case open == '(' && c == '\'':
			p.skipSingleQuoted()
		case open == '(' && c == '"':
			p.skipDoubleQuoted()
		case open == '(' && c == '`':
			p.skipBackquoted()
		case open == '(' && c == '<':
			// `$(cat <<'PYSRC' … PYSRC)` — тело документа-вставки принадлежит
			// ЧУЖОМУ языку, и закрывающая скобка подстановки стоит ПОСЛЕ него.
			// Не сняв тело, разбор искал бы `)` внутри чужого исходника: первая
			// же скобка или апостроф python уводили лексер до конца файла.
			// Терминатор читается тем же readHeredocDelim, что и в основном
			// проходе, — второй копии правил не заводим.
			//
			// Строка-вставка `<<<` СЪЕДАЕТСЯ ЦЕЛИКОМ, все три знака сразу. Отсев
			// её условием при одиночном шаге — ловушка: сдвинувшись на знак,
			// разбор видит оставшиеся `<<` и заводит документ-вставку с
			// терминатором из аргумента, после чего дочитывает файл до конца в
			// поисках терминатора, которого нет. Здесь порядок ветвей и есть
			// вся разница между «прочитано» и «не прочитано».
			if strings.HasPrefix(p.src[p.i:], "<<<") {
				p.i += 3
				break
			}
			if strings.HasPrefix(p.src[p.i:], "<<") {
				p.i += 2
				p.readHeredocDelim()
				break
			}
			p.i++
		case c == '$' && p.i+1 < len(p.src) && (p.src[p.i+1] == '(' || p.src[p.i+1] == '{'):
			p.skipSubstituted()
		case c == open:
			depth++
			p.i++
		case c == closing:
			depth--
			p.i++
			if depth == 0 {
				return
			}
		case c == '\n':
			p.line++
			p.i++
			p.consumeHeredocs()
		default:
			p.i++
		}
	}
	p.unterminated = true
}

// consumeDollar — подстановка: `$VAR`, `${…}`, `$(…)`, `$((…))`.
func (p *shParser) consumeDollar() {
	start := p.i
	p.i++ // '$'
	if p.i >= len(p.src) {
		p.raw.WriteString(p.src[start:])
		p.exp.WriteString(p.src[start:])
		return
	}
	switch p.src[p.i] {
	case '\'', '"':
		// `$'…'` — ANSI-C цитата, `$"…"` — локализованная. Обе ЛИТЕРАЛЬНЫ, и
		// подстановок внутри не делают. Без отдельной ветки открывающая кавычка
		// съедалась как имя переменной, а закрывающая открывала НОВУЮ строку — и
		// лексер уезжал до конца файла. Наблюдалось на живой пробе: 467 строк
		// разобрались в две команды, а гейт напечатал «разобрано» и промолчал.
		q := p.src[p.i]
		p.i++
		for p.i < len(p.src) && p.src[p.i] != q {
			if p.src[p.i] == '\\' && p.i+1 < len(p.src) {
				p.i += 2
				continue
			}
			if p.src[p.i] == '\n' {
				p.line++
			}
			p.i++
		}
		if p.i < len(p.src) {
			p.i++
		} else {
			p.unterminated = true
		}
		p.raw.WriteString(p.src[start:p.i])
		p.lit.WriteString(p.src[start:p.i])
		return
	case '(', '{':
		// Границу подстановки ищет skipSubstitution — единственное место,
		// где записаны правила вложенности. Второй разбор разошёлся бы с первым
		// молча — оба возвращают «прочитано» на любом входе.
		p.skipSubstitution()
	default:
		for p.i < len(p.src) && (isShNameByte(p.src[p.i]) || p.src[p.i] == '@' || p.src[p.i] == '*') {
			p.i++
			if p.src[p.i-1] == '@' || p.src[p.i-1] == '*' {
				break
			}
		}
		if p.i == start+1 && p.i < len(p.src) {
			p.i++ // `$0`, `$?`, `$$`
		}
	}
	seg := p.src[start:p.i]
	p.raw.WriteString(seg)
	p.exp.WriteString(seg)
}

func isShNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// readHeredocDelim — терминатор документа-вставки после `<<`.
func (p *shParser) readHeredocDelim() {
	strip := false
	if p.i < len(p.src) && p.src[p.i] == '-' {
		strip = true
		p.i++
	}
	for p.i < len(p.src) && (p.src[p.i] == ' ' || p.src[p.i] == '\t') {
		p.i++
	}
	var d strings.Builder
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == '\'' || c == '"' {
			q := c
			p.i++
			for p.i < len(p.src) && p.src[p.i] != q {
				d.WriteByte(p.src[p.i])
				p.i++
			}
			if p.i < len(p.src) {
				p.i++
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' || c == ';' || c == '|' || c == '&' ||
			c == ')' || c == '>' || c == '<' {
			break
		}
		if c == '\\' {
			p.i++
			continue
		}
		d.WriteByte(c)
		p.i++
	}
	if d.Len() > 0 {
		p.pendingHD = append(p.pendingHD, shHeredoc{delim: d.String(), strip: strip, cmdIdx: len(p.cmds)})
	}
}

// consumeHeredocs — тела документов-вставок, объявленных на только что
// закончившейся строке. Тело — исполняемая нагрузка чужого языка, поэтому оно
// не разбирается как shell, но и не выбрасывается: по нему решается, ПИШЕТ ли
// вызов вообще.
func (p *shParser) consumeHeredocs() {
	for len(p.pendingHD) > 0 {
		hd := p.pendingHD[0]
		p.pendingHD = p.pendingHD[1:]
		var body strings.Builder
		for p.i < len(p.src) {
			nl := strings.IndexByte(p.src[p.i:], '\n')
			var ln string
			if nl < 0 {
				ln = p.src[p.i:]
				p.i = len(p.src)
			} else {
				ln = p.src[p.i : p.i+nl]
				p.i += nl + 1
				p.line++
			}
			probe := ln
			if hd.strip {
				probe = strings.TrimLeft(probe, "\t")
			}
			if strings.TrimRight(probe, "\r") == hd.delim {
				break
			}
			body.WriteString(ln)
			body.WriteByte('\n')
		}
		if hd.cmdIdx < len(p.cmds) {
			p.cmds[hd.cmdIdx].heredoc += body.String()
		} else if p.inWord || len(p.cur.words) > 0 {
			// Команда ещё ОТКРЫТА, и документ объявлен внутри неё самой —
			// например `x=$(cat <<'PY' … PY)`. Нагрузка принадлежит ЭТОЙ
			// команде: отдав её предыдущей, гейт назвал бы чужую координату, а
			// прочитанное осталось бы прочитанным — то есть ошибка была бы
			// тихой.
			p.cur.heredoc += body.String()
		} else if hd.cmdIdx == len(p.cmds) {
			// Команда закрыта, а документ объявлен на её строке, которая
			// продолжилась, — привязываем к последней закрытой.
			if n := len(p.cmds); n > 0 {
				p.cmds[n-1].heredoc += body.String()
			}
		}
	}
}
