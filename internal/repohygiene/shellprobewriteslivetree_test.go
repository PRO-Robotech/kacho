// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// shellprobewriteslivetree_test.go — та же норма, что у Go-половины, но для
// shell-суит: проба не пишет в дерево, из которого запущена.
//
// # Предмет
//
// `TestProbesDoNotWriteIntoTheTreeTheyRunFrom` (#696) держит класс только для
// Go: его корпус — отслеживаемые `*_test.go`. Три из четырёх экземпляров,
// найденных при заведении того гейта, жили в shell, были починены руками, и
// свойство держалось вниманием: следующая написанная суита той же формы не
// краснела ничем.
//
// # Почему НЕ текстовый предикат (отвергнуто числом, а не вкусом)
//
// Поиск подстроки по shell ловит форму `grep … >"$VICTIM"` и не ловит правку
// через встроенный интерпретатор (`python3 - "$file" <<PY … open(path,"w")`):
// из трёх экземпляров он нашёл бы ОДИН и объявил бы остальные два чистыми.
// Гейт, находящий треть предмета и печатающий «ноль находок», — тот самый класс
// «форма без содержания», ради которого гейты и пишутся.
//
// Число ИЗМЕРЕНО, а не заявлено: от поиска по тексту предикат отличают ровно две
// способности — прочитать тело встроенного интерпретатора и провести
// происхождение через позиционные параметры функций. Обе отключаются в одном
// месте, и проба `TestShellProbeWriteGateSeesWhatATextualPredicateCannot`
// прогоняет на НАСТОЯЩИХ прежних редакциях трёх суит четыре посадки: целиком —
// 3 из 3, без каждой способности по отдельности — 2 из 3, без обеих — 1 из 3.
// Средние две строки нужны затем, чтобы ни одну способность нельзя было снять
// незамеченной.
//
// # Предикат
//
// Разбор идёт по ЛЕКСИКЕ shell, а не по строкам: снимаются комментарии (с учётом
// кавычек), собираются продолжения строк, тела heredoc вынимаются из потока
// команд и связываются с командой, которая их открыла. Иначе запрещённая форма,
// написанная в комментарии соседней суиты, читалась бы как код — ровно то, что
// Go-половина закрывает разбором AST.
//
// Происхождение значения прослеживается так же, как в Go-половине:
//
//   - производитель корня ЖИВОГО дерева ВЫВЕДЕН из тела, а не выписан по имени:
//     подстановка, отталкивающаяся от файла САМОГО скрипта (`$0`,
//     `${BASH_SOURCE[0]}`) и восходящая по каталогам (`cd … && pwd`, `dirname`,
//     `realpath`, `readlink`), либо `git rev-parse --show-toplevel`. Имён у этих
//     переменных в дереве много и они разные (`DEPLOY_ROOT`, `REPO_ROOT`,
//     `ROOT`, `UMBRELLA`, `SCRIPT_DIR`), список разошёлся бы с деревом молча;
//   - живое значение течёт через присваивания (`UMBRELLA="$ROOT/helm/umbrella"`),
//     через склейку в слове и через ПОЗИЦИОННЫЕ ПАРАМЕТРЫ функций того же
//     скрипта (`f "$LIVE"` → внутри `f` живы `$1` и `$@`). Считается до
//     неподвижной точки: помощник зовёт помощника — прежняя редакция
//     `podtemplate-annotation-single-owner-test.sh` уходила на ТРИ шага
//     (вызов → `local file="$1"` → `inject_at "$@"` → `python3 - "$@"`);
//   - подстановка, чья головная команда не сохраняет ПУТЬ (`mktemp`, `basename`,
//     `cat`, `bash`), происхождения не передаёт. Поэтому `WORK="$(mktemp -d)"`
//     живым не становится, и отдельного перечня «имён временных каталогов» не
//     нужно — его пришлось бы пополнять на каждый новый способ их завести.
//
// Место записи — не только перенаправление. Виды выведены переписью корпуса
// (`census`-числа печатает сам гейт): перенаправление и `tee`; команды с
// назначением (`cp`, `mv`, `install`, `rsync`, `ln`); команды, чьи аргументы и
// есть цель (`rm`, `touch`, `truncate`, `chmod`, `chown`); правка на месте
// (`sed -i`, `perl -i`, `yq -i`); изменяющая подкоманда `git`; и ВСТРОЕННЫЙ
// ИНТЕРПРЕТАТОР, чьё тело содержит запись, а живое значение приезжает к нему
// аргументом либо подстановкой в незакавыченный heredoc.
//
// Чтение живого дерева законно и обязано молчать: суиты только этим и заняты.
// Поэтому `open(path)` без режима записи, `cat "$LIVE/f"`, `helm template
// "$UMBRELLA"` находкой не являются — различает РЕЖИМ, а не имя интерпретатора.
//
// Цель записи не только помечается живой, но и РАЗРЕШАЕТСЯ в путь относительно
// корня дерева. Это не украшение вердикта: дерево само объявляет часть себя
// артефактами (`.gitignore` — каталоги отчётов прогонов), и запись туда корпуса
// не портит. Отличить одно от другого можно, только назвав место. Послабление
// ИСТЕКАЕТ САМО: снимут строку из `.gitignore` — то же место станет находкой без
// единой правки гейта. Замер по дереву: живых записей 57, из них по объявленному
// артефактом пути 56.
//
// # Чего предикат НЕ ловит — названо, а не умолчано
//
//   - запись по ОТНОСИТЕЛЬНОМУ пути после `cd` в живое дерево прослеживается
//     только когда сам `cd` виден в скрипте: рабочий каталог, унаследованный от
//     вызывающего, скрипту неизвестен, и считать его живым значило бы красить
//     каждую запись каждого скрипта;
//   - там же: если живость выведена ТОЛЬКО по рабочему каталогу, а место назвать
//     не удалось (значение приходит из функции скрипта), находка не заводится —
//     вывод слишком слаб, чтобы утверждать. Молчание названо счётчиком переписи,
//     а не умолчано; на дереве таких мест 1;
//   - создание и снятие КАТАЛОГА (`mkdir`, `rmdir`) записью в корпус не
//     считается: git каталогов не отслеживает, а файл, который туда потом
//     напишут, ловится сам по себе;
//   - подстановка внутри подстановки глубже первого уровня;
//   - путь, собранный в переменную ЧЕРЕЗ внешнюю команду (`p="$(realpath
//     "$LIVE/x")"` живым останется, а `p="$(printf %s "$LIVE/x")"` — нет:
//     головная команда `printf` пути не производит).
//
// Границы куплены точностью сознательно: первое же ложное срабатывание
// отключило бы гейт целиком.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// ─────────────────────────────────────────────────────────────────────────────
// Находка и перепись.
// ─────────────────────────────────────────────────────────────────────────────

// shellWriteFinding — место, где живой корень доезжает до записи.
type shellWriteFinding struct {
	File string // путь суиты относительно корня дерева
	Line int
	What string // чем именно пишет
	Path string // разрешённая цель записи ("" — вывести не удалось)
	Why  string
}

// shellWriteCensus — объём осмотренного. «Ноль находок» обязано быть отличимо
// от «ноль прочитанного», поэтому счётчики печатаются всегда.
type shellWriteCensus struct {
	Files     int // скриптов разобрано
	Commands  int // простых команд осмотрено
	Producers int // мест, где выведен корень живого дерева
	Bodies    int // тел встроенных интерпретаторов осмотрено
	Writes    int // мест записи осмотрено
	Tainted   int // из них по живому корню
	Artifacts int // из них по пути, объявленному деревом артефактом (.gitignore)
	Unnamed   int // из них цель назвать не удалось при выводе живости по каталогу
}

// ─────────────────────────────────────────────────────────────────────────────
// Лексика: комментарии, продолжения строк, heredoc.
// ─────────────────────────────────────────────────────────────────────────────

// shHeredoc — тело, открытое `<<DELIM` на строке команды.
type shHeredoc struct {
	body   string
	expand bool // разделитель БЕЗ кавычек → shell подставляет переменные в тело
}

// shLine — логическая строка скрипта: текст без комментария плюс тела heredoc,
// которые эта строка открыла.
type shLine struct {
	line int
	text string
	docs []shHeredoc
}

// shellLogicalLines — разбор исходника на логические строки.
//
// Три вещи делаются здесь и только здесь, потому что каждая из них ломает любой
// построчный предикат: продолжение строки (`\` в конце) склеивает команду из
// нескольких строк; комментарий начинается `#` ТОЛЬКО вне кавычек и в начале
// слова (иначе `${#a}` и `s/#/x/` съедали бы остаток); тело heredoc — не shell
// вовсе, и токены в нём разбирать нельзя.
func shellLogicalLines(src string) []shLine {
	raw := strings.Split(src, "\n")
	var out []shLine
	// Кавычки в shell ПЕРЕЖИВАЮТ перевод строки: программа awk или jq в
	// одинарных кавычках занимает десяток строк. Разбор, начинающий каждую
	// строку «вне кавычек», читал бы её тело как shell — и находил там
	// перенаправление в сравнении `length(cand) > length(best)`. Поймано на
	// живом скрипте дерева.
	var carry quoteState
	for i := 0; i < len(raw); i++ {
		start := i + 1
		cur := raw[i]
		// Продолжение строки: `\` в конце и он сам не экранирован.
		for trailingBackslash(cur) && i+1 < len(raw) {
			i++
			cur = cur[:len(cur)-1] + raw[i]
		}
		var clean string
		var delims []heredocDecl
		clean, delims, carry = scanShellText(cur, carry)
		ln := shLine{line: start, text: clean}
		for _, d := range delims {
			body, next := readHeredocBody(raw, i+1, d)
			ln.docs = append(ln.docs, shHeredoc{body: body, expand: !d.quoted})
			i = next
		}
		out = append(out, ln)
	}
	return out
}

func trailingBackslash(s string) bool {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// heredocDecl — объявление heredoc на строке команды.
type heredocDecl struct {
	delim  string
	quoted bool // разделитель в кавычках → подстановки в теле НЕ выполняются
	strip  bool // `<<-` → ведущие табуляции у ограничителя снимаются
}

// scanShellText — снять комментарий и найти объявления heredoc.
//
// Возвращает текст без комментария. Состояния кавычек и подстановок ведутся
// честно: в одинарных кавычках не экранируется ничто, в двойных — только
// `\$`, `\"`, `\\` и экранированный обратный апостроф, а `$(` открывает
// снова могут быть кавычки.
type quoteState struct {
	sq    bool // внутри '…'
	dq    bool // внутри "…"
	depth int  // вложенность $( … )
}

func scanShellText(s string, in quoteState) (string, []heredocDecl, quoteState) {
	var (
		b     strings.Builder
		docs  []heredocDecl
		i     int
		sq    = in.sq
		dq    = in.dq
		depth = in.depth
	)
	// Строка, НАЧАВШАЯСЯ внутри кавычек, — продолжение чужого литерала (тело
	// awk, jq, python -c). Копировать его в поток команд нельзя: сравнение
	// `length(cand) > length(best)` внутри awk-программы читалось бы как
	// перенаправление в файл `length(best))`. Литерал пропускается молча, и
	// разбор возобновляется с закрывающей кавычки — то, что стоит ПОСЛЕ неё
	// (`> "$OUT"`), остаётся видимым.
	for sq || dq {
		if i >= len(s) {
			return "", nil, quoteState{sq: sq, dq: dq, depth: depth}
		}
		switch {
		case dq && s[i] == '\\' && i+1 < len(s):
			i++
		case sq && s[i] == '\'':
			sq = false
		case dq && s[i] == '"':
			dq = false
		}
		i++
	}
	prevSignificant := byte(0) // последний непробельный символ ВНЕ кавычек
	for i < len(s) {
		c := s[i]
		switch {
		case sq:
			if c == '\'' {
				sq = false
			}
			b.WriteByte(c)
			i++
			continue
		case dq:
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(c)
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			if c == '"' {
				dq = false
			}
			b.WriteByte(c)
			i++
			continue
		}
		switch {
		case c == '\\' && i+1 < len(s):
			b.WriteByte(c)
			b.WriteByte(s[i+1])
			i += 2
			prevSignificant = 'x'
			continue
		case c == '\'':
			sq = true
		case c == '"':
			dq = true
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			depth++
			b.WriteString("$(")
			i += 2
			prevSignificant = '('
			continue
		case c == ')' && depth > 0:
			depth--
		case c == '#' && depth == 0 &&
			(prevSignificant == 0 || prevSignificant == ';' || prevSignificant == '&' ||
				prevSignificant == '|' || prevSignificant == '(' || prevSignificant == '{' ||
				isSpaceByte(prevByte(s, i))):
			// Комментарий до конца строки. Условие «в начале слова» обязательно:
			// иначе `${#arr}` и `sed 's/#//'` обрезали бы команду.
			return b.String(), docs, quoteState{sq: sq, dq: dq, depth: depth}
		case c == '<' && i+1 < len(s) && s[i+1] == '<' &&
			!(i+2 < len(s) && s[i+2] == '<') && !(i > 0 && s[i-1] == '<'):
			// `<<` либо `<<-`. `<<<` — here-string, а не heredoc, и отличать
			// его надо С ОБЕИХ сторон: проверки только «следующий не `<`»
			// недостаточно — во второй позиции тройки та же проверка проходит,
			// и `read -ra fl <<<"$files"` читался как heredoc с разделителем
			// `$files`, которого в файле нет. Тогда «тело» съедало ОСТАТОК
			// скрипта: 414 строк превращались в 106, а всё, что ниже, гейт не
			// видел вовсе — и молчал.
			j := i + 2
			strip := false
			if j < len(s) && s[j] == '-' {
				strip = true
				j++
			}
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			d, next := readHeredocDelim(s, j)
			if d.delim != "" {
				d.strip = strip
				docs = append(docs, d)
				b.WriteString(s[i:next])
				i = next
				prevSignificant = 'x'
				continue
			}
		}
		if !isSpaceByte(c) {
			prevSignificant = c
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), docs, quoteState{sq: sq, dq: dq, depth: depth}
}

func prevByte(s string, i int) byte {
	if i == 0 {
		return 0
	}
	return s[i-1]
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == 0 }

// readHeredocDelim — разделитель heredoc: `EOF`, `'PY'`, `"PY"`, `\EOF`.
// Кавычки вокруг разделителя означают, что подстановки в теле НЕ выполняются.
func readHeredocDelim(s string, i int) (heredocDecl, int) {
	var d heredocDecl
	if i >= len(s) {
		return d, i
	}
	switch s[i] {
	case '\'', '"':
		q := s[i]
		j := i + 1
		for j < len(s) && s[j] != q {
			j++
		}
		d.delim = s[i+1 : j]
		d.quoted = true
		if j < len(s) {
			j++
		}
		return d, j
	case '\\':
		i++
		d.quoted = true
	}
	j := i
	for j < len(s) && (isWordByte(s[j])) {
		j++
	}
	d.delim = s[i:j]
	return d, j
}

func isWordByte(c byte) bool {
	return c == '_' || c == '-' || c == '.' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// readHeredocBody — тело от строки `from` до ограничителя. Возвращает тело и
// индекс строки-ограничителя (или последней, если ограничителя нет).
func readHeredocBody(raw []string, from int, d heredocDecl) (string, int) {
	var b strings.Builder
	for i := from; i < len(raw); i++ {
		line := raw[i]
		cmp := line
		if d.strip {
			cmp = strings.TrimLeft(cmp, "\t")
		}
		if strings.TrimRight(cmp, " \t\r") == d.delim {
			return b.String(), i
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), len(raw) - 1
}

// ─────────────────────────────────────────────────────────────────────────────
// Команды и слова.
// ─────────────────────────────────────────────────────────────────────────────

// shRedir — перенаправление вывода.
type shRedir struct {
	op     string
	target string
}

// shCommand — простая команда.
type shCommand struct {
	line   int
	words  []string
	redirs []shRedir
	docs   []shHeredoc
	fn     string // функция, в теле которой стоит команда
	inSub  bool   // команда исполняется в ПОДОБОЛОЧКЕ `$( … )`
}

// splitShellCommands — разбить логическую строку на простые команды по
// управляющим операторам. Разделители ищутся ВНЕ кавычек и подстановок.
func splitShellCommands(text string) []string {
	var (
		out   []string
		b     strings.Builder
		sq    bool
		dq    bool
		depth int
	)
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
		b.Reset()
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case sq:
			if c == '\'' {
				sq = false
			}
			b.WriteByte(c)
			continue
		case dq:
			if c == '\\' && i+1 < len(text) {
				b.WriteByte(c)
				i++
				b.WriteByte(text[i])
				continue
			}
			if c == '"' {
				dq = false
			}
			b.WriteByte(c)
			continue
		}
		switch {
		case c == '\\' && i+1 < len(text):
			b.WriteByte(c)
			i++
			b.WriteByte(text[i])
		case c == '\'':
			sq = true
			b.WriteByte(c)
		case c == '"':
			dq = true
			b.WriteByte(c)
		case c == '$' && i+1 < len(text) && text[i+1] == '(':
			depth++
			b.WriteString("$(")
			i++
		case c == ')' && depth > 0:
			depth--
			b.WriteByte(c)
		case depth > 0:
			b.WriteByte(c)
		case c == ';' || c == '\n':
			flush()
		case c == '&' && i+1 < len(text) && text[i+1] == '&':
			flush()
			i++
		case c == '|' && i+1 < len(text) && text[i+1] == '|':
			flush()
			i++
		case c == '|':
			flush()
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

// shellWords — слова команды с сохранением кавычек в тексте: кавычки значимы
// для разбора ссылок (в одинарных подстановки не выполняются).
func shellWords(text string) []string {
	var (
		out   []string
		b     strings.Builder
		sq    bool
		dq    bool
		depth int
		open  bool
	)
	flush := func() {
		if open {
			out = append(out, b.String())
		}
		b.Reset()
		open = false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case sq:
			if c == '\'' {
				sq = false
			}
			b.WriteByte(c)
			open = true
			continue
		case dq:
			if c == '\\' && i+1 < len(text) {
				b.WriteByte(c)
				i++
				b.WriteByte(text[i])
				open = true
				continue
			}
			if c == '"' {
				dq = false
			}
			b.WriteByte(c)
			open = true
			continue
		}
		switch {
		case c == '\\' && i+1 < len(text):
			b.WriteByte(c)
			i++
			b.WriteByte(text[i])
			open = true
		case c == '\'':
			sq = true
			b.WriteByte(c)
			open = true
		case c == '"':
			dq = true
			b.WriteByte(c)
			open = true
		case c == '$' && i+1 < len(text) && text[i+1] == '(':
			depth++
			b.WriteString("$(")
			i++
			open = true
		case c == ')' && depth > 0:
			depth--
			b.WriteByte(c)
			open = true
		case depth > 0:
			b.WriteByte(c)
			open = true
		case c == ' ' || c == '\t':
			flush()
		default:
			b.WriteByte(c)
			open = true
		}
	}
	flush()
	return out
}

var (
	// redirHead — голова перенаправления: необязательный номер дескриптора,
	// затем `>`/`>>`/`&>`/`>|`.
	redirHead = regexp.MustCompile(`^([0-9]*|&)(>>|>\||&>>|&>|>)$`)
	// redirGlued — то же, но цель приклеена к оператору: `>"$F"`, `2>/dev/null`.
	redirGlued = regexp.MustCompile(`^([0-9]*|&)(>>|>\||&>>|&>|>)(.+)$`)
	// funcDecl — объявление функции: `name()` либо `name ()`.
	funcDecl = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_:.-]*)\(\)$`)
	// assign — присваивание переменной, в том числе `+=`.
	assign = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\+?=)(.*)$`)
)

// parseShellCommand — слова и перенаправления одной простой команды.
//
// Внутри `[[ … ]]` и `(( … ))` символ `>` — сравнение, а не перенаправление:
// разбирать его как запись значило бы красить арифметику.
func parseShellCommand(text string) ([]string, []shRedir) {
	words := shellWords(text)
	if len(words) > 0 && (words[0] == "[[" || words[0] == "((" || words[0] == "[") {
		return words, nil
	}
	var (
		out    []string
		redirs []shRedir
	)
	for i := 0; i < len(words); i++ {
		w := words[i]
		if m := redirHead.FindStringSubmatch(w); m != nil {
			if i+1 < len(words) {
				redirs = append(redirs, shRedir{op: m[2], target: words[i+1]})
				i++
			}
			continue
		}
		if m := redirGlued.FindStringSubmatch(w); m != nil && !strings.HasPrefix(w, "-") {
			redirs = append(redirs, shRedir{op: m[2], target: m[3]})
			continue
		}
		out = append(out, w)
	}
	return out, redirs
}

// shellCommands — все простые команды скрипта с привязкой к функции.
//
// Функция определяется по объявлению `name() {`, её тело — до возврата глубины
// фигурных скобок. Скобки считаются по СЛОВАМ (`{` и `}` целиком), поэтому
// `${VAR}` и программа awk в кавычках глубину не двигают.
func shellCommands(src string) []shCommand {
	var (
		out       []shCommand
		curFn     string
		fnDepth   int
		depth     int
		pendingFn string
	)
	for _, ln := range shellLogicalLines(src) {
		parts := splitShellCommands(ln.text)
		for pi, part := range parts {
			words, redirs := parseShellCommand(part)
			if len(words) == 0 && len(redirs) == 0 {
				continue
			}
			cmd := shCommand{line: ln.line, words: words, redirs: redirs, fn: curFn}
			// Тела heredoc принадлежат ПОСЛЕДНЕЙ команде логической строки:
			// `python3 - "$f" <<PY` стоит в конце, и приклеивать тело к первой
			// команде конвейера было бы неверно.
			if pi == len(parts)-1 {
				cmd.docs = ln.docs
			}
			out = append(out, cmd)
			// Команда, спрятанная в подстановке (`X="$(f "$p")"`), — такая же
			// команда: она и зовёт функции скрипта, и пишет в файловую систему.
			// Без этого разбора трёхшаговый путь «вызов → локальная переменная
			// → встроенный интерпретатор» был невидим целиком — поймано
			// контролем на прежней редакции суиты `podtemplate`.
			out = append(out, subshellCommands(ln.line, curFn, words)...)

			// Объявление распознаётся ДО подсчёта скобок ЭТОЙ ЖЕ строки:
			// `f() {` несёт и имя, и открывающую скобку, и обратный порядок
			// смещал бы каждое тело на одну функцию назад — сдвиг тихий,
			// поймано на суите nlb, где тело одной функции числилось за другой.
			if len(words) > 0 {
				if m := funcDecl.FindStringSubmatch(words[0]); m != nil {
					pendingFn = m[1]
				} else if words[0] == "function" && len(words) > 1 {
					pendingFn = strings.TrimSuffix(words[1], "()")
				}
			}
			for _, w := range words {
				switch {
				case w == "{":
					depth++
					if pendingFn != "" {
						curFn, fnDepth, pendingFn = pendingFn, depth, ""
					}
				case w == "}":
					depth--
					if curFn != "" && depth < fnDepth {
						curFn = ""
					}
				}
			}
		}
	}
	return out
}

// subshellCommands — команды из тел подстановок слова.
//
// Разбор идёт на ОДИН уровень вложенности: этого хватает на все формы дерева, а
// произвольная глубина потребовала бы полноценного разбора shell.
func subshellCommands(line int, fn string, words []string) []shCommand {
	var out []shCommand
	for _, w := range words {
		for _, sub := range commandSubs(w) {
			for _, part := range splitShellCommands(sub) {
				sw, sr := parseShellCommand(part)
				if len(sw) == 0 && len(sr) == 0 {
					continue
				}
				out = append(out, shCommand{
					line: line, words: sw, redirs: sr, fn: fn, inSub: true,
				})
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Происхождение значения.
// ─────────────────────────────────────────────────────────────────────────────

// commandSubs — тела подстановок первого уровня: `$( … )` и обратные апострофы.
func commandSubs(word string) []string {
	var (
		subs  []string
		b     strings.Builder
		depth int
		sq    bool
	)
	for i := 0; i < len(word); i++ {
		c := word[i]
		if depth == 0 {
			if c == '\'' {
				sq = !sq
				continue
			}
			if sq {
				continue
			}
			if c == '$' && i+1 < len(word) && word[i+1] == '(' && !(i+2 < len(word) && word[i+2] == '(') {
				depth = 1
				i++
				continue
			}
			if c == '`' {
				depth = 1
				continue
			}
			continue
		}
		switch c {
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			depth--
			if depth == 0 {
				subs = append(subs, b.String())
				b.Reset()
			} else {
				b.WriteByte(c)
			}
		case '`':
			depth = 0
			subs = append(subs, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		subs = append(subs, b.String())
	}
	return subs
}

// stripCommandSubs — слово без подстановок: остаётся то, что shell подставит из
// переменных. Нужно, чтобы `INJ="$(bash "$0")"` не считалось живым путём: `$0`
// там стоит АРГУМЕНТОМ чужой команды, а не значением слова.
func stripCommandSubs(word string) string {
	var (
		b     strings.Builder
		depth int
	)
	for i := 0; i < len(word); i++ {
		c := word[i]
		if depth == 0 {
			if c == '$' && i+1 < len(word) && word[i+1] == '(' && !(i+2 < len(word) && word[i+2] == '(') {
				depth = 1
				i++
				continue
			}
			if c == '`' {
				depth = 1
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case '`':
			depth = 0
		}
	}
	return b.String()
}

// pathPreservingHeads — головные команды подстановки, чей результат ОСТАЁТСЯ
// путём в том же дереве. Всё остальное происхождения не передаёт: `mktemp`
// заводит свой каталог, `basename` срезает каталог (аналог `filepath.Base` в
// Go-половине), `cat`/`bash`/`printf` возвращают содержимое, а не путь.
var pathPreservingHeads = map[string]bool{
	"cd": true, "dirname": true, "realpath": true, "readlink": true, "pwd": true,
}

// ownFileRef — ссылка на файл САМОГО скрипта. Восхождение от неё даёт корень
// живого дерева — прямой аналог формы `runtime.Caller` + `filepath.Dir`,
// которую распознаёт Go-половина.
var ownFileRef = regexp.MustCompile(`\$0\b|\$\{0\}|BASH_SOURCE`)

// isShellRootProducer — производит ли подстановка корень ЖИВОГО дерева.
//
// Признак ВЫВЕДЕН из тела, а не из имени переменной: имён у этих помощников в
// дереве не меньше десятка (`DEPLOY_ROOT`, `REPO_ROOT`, `ROOT`, `SCRIPT_DIR`),
// и список разошёлся бы с деревом молча.
func isShellRootProducer(sub string) bool {
	words := shellWords(strings.TrimSpace(sub))
	if len(words) == 0 {
		return false
	}
	head := filepath.Base(strings.Trim(words[0], `"'`))
	if head == "git" {
		return strings.Contains(sub, "rev-parse") && strings.Contains(sub, "--show-toplevel")
	}
	if !pathPreservingHeads[head] {
		return false
	}
	// `cd … && pwd`, `dirname "$0"`, `realpath "${BASH_SOURCE[0]}"` — все
	// отталкиваются от файла самого скрипта. Голый `pwd` сюда не попадает: он
	// говорит о рабочем каталоге вызывающего, а тот скрипту неизвестен.
	return ownFileRef.MatchString(sub)
}

// shScope — прослеживание живого значения внутри одного скрипта.
type shScope struct {
	live       map[string]bool         // переменные, чьё значение происходит от живого корня
	val        map[string]string       // и их РАЗРЕШЁННЫЙ путь
	rooted     map[string]bool         // отсчитан ли он от корня дерева
	inexact    map[string]bool         // и содержит ли он заполнитель невыводимого
	liveParams map[string]map[int]bool // функция → индексы живых позиционных параметров
	fn         string                  // текущая функция
	self       string                  // путь самого скрипта — значение `$0`
	producers  int
	out        map[string]map[int]bool // куда живое уехало: функция → индексы аргументов
	cwdLive    bool                    // рабочий каталог процесса переведён в живое дерево
	cwd        string                  // и его путь относительно корня дерева
	cwdKnown   bool
	caps       shellAuditCapabilities
}

var varRef = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*|[0-9]|@|\*)\}?`)

// wordLive — происходит ли значение слова от корня живого дерева.
func (s *shScope) wordLive(w string) bool {
	for _, sub := range commandSubs(w) {
		if isShellRootProducer(sub) {
			return true
		}
	}
	plain := stripSingleQuoted(stripCommandSubs(w))
	if ownFileRef.MatchString(plain) {
		return true
	}
	for _, m := range varRef.FindAllStringSubmatch(plain, -1) {
		name := m[1]
		switch {
		case name == "@" || name == "*":
			for _, ok := range s.liveParams[s.fn] {
				if ok {
					return true
				}
			}
		case name >= "1" && name <= "9":
			if s.liveParams[s.fn][int(name[0]-'0')] {
				return true
			}
		default:
			if s.live[name] {
				return true
			}
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Разрешение пути: где именно в дереве стоит цель записи.
// ─────────────────────────────────────────────────────────────────────────────

// dollarRef — разобранная подстановка `$NAME` / `${NAME<оп><аргумент>}`.
type dollarRef struct {
	name string // имя без индекса: `BASH_SOURCE` для `${BASH_SOURCE[0]}`
	op   string // `:-`, `:=`, `%`, `%%`, `#`, `##`, … либо пусто
	arg  string // аргумент операции — может сам нести подстановки
	next int    // индекс за концом подстановки
}

// parseDollar — подстановка, начинающаяся в позиции i (`t[i] == '$'`).
//
// Разбирается РУКАМИ, а не выражением: аргумент умолчания сам бывает
// подстановкой (`${1:-${REPO_ROOT}/build/x.json}` — настоящая строка дерева), и
// выражение с классом «без фигурных скобок» на ней ломается, отдавая имя `1` и
// литеральный хвост `:-${REPO_ROOT}/build/x.json}` вместо пути.
func parseDollar(t string, i int) (dollarRef, bool) {
	if i >= len(t) || t[i] != '$' {
		return dollarRef{}, false
	}
	if i+1 < len(t) && t[i+1] == '{' {
		depth, j := 1, i+2
		for j < len(t) && depth > 0 {
			switch t[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			j++
		}
		if depth > 0 {
			return dollarRef{}, false
		}
		inner := t[i+2 : j-1]
		if strings.HasPrefix(inner, "#") || strings.HasPrefix(inner, "!") {
			// Длина и косвенность — не путь.
			return dollarRef{next: j}, true
		}
		k := 0
		for k < len(inner) && (isWordByte(inner[k]) || inner[k] == '[' || inner[k] == ']') {
			k++
		}
		r := dollarRef{name: strings.SplitN(inner[:k], "[", 2)[0], next: j}
		for _, op := range []string{":-", ":=", ":?", ":+", "##", "#", "%%", "%", "//", "/", "^^", "^", ",,", ","} {
			if strings.HasPrefix(inner[k:], op) {
				r.op, r.arg = op, inner[k+len(op):]
				break
			}
		}
		return r, true
	}
	j := i + 1
	if j < len(t) && (t[j] == '@' || t[j] == '*' || (t[j] >= '0' && t[j] <= '9')) {
		return dollarRef{name: string(t[j]), next: j + 1}, true
	}
	for j < len(t) && (isWordByte(t[j]) && t[j] != '-' && t[j] != '.') {
		j++
	}
	if j == i+1 {
		return dollarRef{}, false
	}
	return dollarRef{name: t[i+1 : j], next: j}, true
}

// unresolvedSegment — заполнитель невыводимой подстановки. Имя не пустое и без
// разделителя каталогов: пустое склеило бы соседние сегменты в один, а `/`
// увёл бы путь в несуществующий каталог.
const unresolvedSegment = "_"

// shResolved — разрешение слова в путь.
//
//   - path   — то, что удалось вывести (ПРЕФИКС, если разрешение оборвалось);
//   - exact  — дошло ли разрешение до конца слова;
//   - rooted — путь отсчитывается от КОРНЯ дерева, а не от рабочего каталога.
//     Различие несущее: `environments/local.json` — путь относительно текущего
//     каталога и без него бессмыслен, а `$(dirname "$0")/..` уже отсчитан от
//     корня, потому что `$0` — координата самого скрипта в дереве.
type shResolved struct {
	path   string
	exact  bool
	rooted bool
}

// resolvePath — путь цели ОТНОСИТЕЛЬНО корня дерева, насколько он выводим.
//
// Зачем разрешать, а не довольствоваться «живое/не живое»: дерево само объявляет
// часть себя артефактами (`.gitignore`), и запись в такой каталог корпуса не
// портит — он ровно для того и заведён. Отличить одно от другого можно, только
// назвав путь. Побочная польза: вердикт называет координату В ДЕРЕВЕ, а не
// только строку скрипта.
//
// Возвращается ПРЕФИКС: разрешение обрывается на первой невыводимой подстановке
// (`$RUN_ID`, `$1`). Для вопроса «объявлено ли это место артефактом» префикса
// достаточно — он всегда содержит каталог.
func (s *shScope) resolvePath(w string) (path string, named bool) {
	r := s.expand(strings.Trim(strings.TrimSpace(w), `"`))
	if r.path == "" || strings.HasPrefix(r.path, "/") {
		// Пусто либо абсолютный путь: временный каталог или чужое место —
		// дереву он не принадлежит, и судить о нём этому гейту нечем.
		return "", false
	}
	// МЕСТО названо, если разрешение дошло до конца ЛИБО заведомо известен
	// первый каталог: тогда заполнитель стоит внутри известного места, и
	// вопрос об артефакте решается по нему. Если же заполнитель занял первый
	// сегмент, каталог ВЫДУМАН — и говорить о нём нельзя вовсе (иначе гейт
	// уверенно называет место, которого нет; поймано на суите nlb, где путь
	// целиком приходил из функции скрипта).
	named = r.exact
	if !named {
		if i := strings.Index(r.path, "/"); i > 0 &&
			!strings.Contains(r.path[:i], unresolvedSegment) {
			named = true
		}
	}
	if !r.rooted {
		if !s.cwdKnown {
			return "", false
		}
		return filepath.Clean(filepath.Join(s.cwd, r.path)), named
	}
	return filepath.Clean(r.path), named
}

// expand — раскрыть подстановки слова.
func (s *shScope) expand(t string) shResolved {
	var b strings.Builder
	out := shResolved{exact: true}
	for i := 0; i < len(t); {
		switch {
		case t[i] == '$' && i+1 < len(t) && t[i+1] == '(':
			depth, j := 1, i+2
			for j < len(t) && depth > 0 {
				switch t[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
				j++
			}
			if depth > 0 {
				out.path = b.String()
				out.exact = false
				return out
			}
			r := s.expandSub(t[i+2 : j-1])
			if r.path == "" && !r.exact {
				// Невыводимая подстановка НЕ обрывает разрешение: на её место
				// встаёт заполнитель, а литеральный хвост сохраняется. Именно
				// хвост чаще всего и решает вопрос об артефакте — правило
				// `.gitignore` обычно ключуется на расширении (`results/*.log`),
				// а обрыв на `${name}` это расширение уничтожал.
				b.WriteString(unresolvedSegment)
				out.exact = false
				i = j
				continue
			}
			b.WriteString(r.path)
			out.rooted = out.rooted || r.rooted
			i = j
		case t[i] == '$':
			d, ok := parseDollar(t, i)
			if !ok {
				out.path = b.String()
				out.exact = false
				return out
			}
			r := s.expandVar(d)
			if r.path == "" && !r.exact {
				b.WriteString(unresolvedSegment)
				out.exact = false
				i = d.next
				continue
			}
			b.WriteString(r.path)
			out.rooted = out.rooted || r.rooted
			out.exact = out.exact && r.exact
			i = d.next
		case t[i] == '\'' || t[i] == '"':
			i++
		default:
			b.WriteByte(t[i])
			i++
		}
	}
	out.path = b.String()
	return out
}

// expandVar — значение подстановки переменной.
func (s *shScope) expandVar(d dollarRef) shResolved {
	name := d.name
	// `$0` и `${BASH_SOURCE[0]}` — координата самого скрипта. Прочие
	// позиционные параметры и `$@` разрешению не поддаются: их значение
	// приходит от вызывающего.
	if name == "0" || strings.HasPrefix(name, "BASH_SOURCE") {
		return shResolved{path: s.self, exact: true, rooted: true}
	}
	v, known := s.val[name]
	switch d.op {
	case ":-", ":=", ":+":
		if !known || v == "" {
			return s.expand(d.arg)
		}
		return shResolved{path: v, exact: !s.inexact[name], rooted: s.rooted[name]}
	case "%", "%%", "#", "##":
		// Усечение: точный результат неизвестен, но КАТАЛОГ сохраняется — а
		// вопрос «объявлено ли это место артефактом» решается каталогом.
		if !known {
			return shResolved{}
		}
		return shResolved{path: v, exact: false, rooted: s.rooted[name]}
	}
	if !known || v == "" {
		return shResolved{}
	}
	return shResolved{path: v, exact: !s.inexact[name], rooted: s.rooted[name]}
}

// expandSub — путь, который даёт командная подстановка. Разрешаются только те
// головы, что путь СОХРАНЯЮТ; остальные обрывают разрешение — их результат либо
// не путь вовсе (`cat`, `printf`), либо путь в чужом месте (`mktemp`).
func (s *shScope) expandSub(inner string) shResolved {
	words := shellWords(strings.TrimSpace(inner))
	if len(words) == 0 {
		return shResolved{}
	}
	head := filepath.Base(strings.Trim(words[0], `"'`))
	switch head {
	case "git":
		if strings.Contains(inner, "--show-toplevel") {
			return shResolved{path: ".", exact: true, rooted: true}
		}
	case "dirname":
		if len(words) > 1 {
			r := s.expand(strings.Trim(words[1], `"`))
			if r.path != "" {
				r.path = filepath.Dir(r.path)
				return r
			}
		}
	case "realpath", "readlink":
		for _, a := range words[1:] {
			if isFlag(a) {
				continue
			}
			return s.expand(strings.Trim(a, `"`))
		}
	case "cd":
		// `cd X && pwd` — путь X; хвост после `&&` разобран отдельными словами.
		for _, a := range words[1:] {
			if isFlag(a) || a == "&&" || a == "pwd" || a == ";" {
				continue
			}
			return s.expand(strings.Trim(a, `"`))
		}
	case "pwd":
		if s.cwdKnown {
			return shResolved{path: s.cwd, exact: true, rooted: true}
		}
	}
	return shResolved{}
}

// stripSingleQuoted — убрать участки в одинарных кавычках: подстановок там не
// бывает, и `'$ROOT'` живым словом не является.
func stripSingleQuoted(w string) string {
	var b strings.Builder
	sq := false
	for i := 0; i < len(w); i++ {
		if w[i] == '\'' {
			sq = !sq
			continue
		}
		if !sq {
			b.WriteByte(w[i])
		}
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Места записи.
// ─────────────────────────────────────────────────────────────────────────────

// destinationWriters — команды, чья цель ПОСЛЕДНИЙ аргумент.
var destinationWriters = map[string]bool{
	"cp": true, "mv": true, "install": true, "rsync": true, "ln": true,
}

// argumentWriters — команды, чьи аргументы И ЕСТЬ цель.
//
// `mkdir` и `rmdir` сюда НЕ входят, и это решение, а не пропуск: git каталогов
// не отслеживает, поэтому созданный или снятый пустой каталог в корпус не
// попадает и «ноль находок» не портит. Файл, который в этот каталог потом
// напишут, ловится сам по себе — то есть предмет запрета остаётся закрыт, а
// шума от `mkdir -p` рядом с закоммиченными эталонами не будет.
var argumentWriters = map[string]bool{
	"rm": true, "touch": true, "truncate": true,
	"unlink": true, "shred": true, "tee": true, "mkfifo": true,
}

// modeWriters — команды, у которых первый аргумент не путь (режим, владелец).
var modeWriters = map[string]bool{"chmod": true, "chown": true, "chgrp": true}

// inPlaceEditors — правка файла на месте: цель узнаётся по флагу `-i`.
var inPlaceEditors = map[string]bool{"sed": true, "perl": true, "yq": true, "ruby": true}

// interpreters — встроенные интерпретаторы. Запись у них внутри ТЕЛА, а путь
// приезжает аргументом: текстовый предикат по shell этой формы не видит вовсе,
// и именно она составляла две трети найденных экземпляров класса.
var interpreters = map[string]bool{
	"python": true, "python3": true, "perl": true, "ruby": true,
	"node": true, "nodejs": true, "php": true,
}

// interpreterWriteSignal — признак записи в ТЕЛЕ интерпретатора.
//
// Различает РЕЖИМ, а не имя: `open(p)` — чтение и обязано молчать (суиты только
// этим и заняты), `open(p, "w")` — запись. Без различения гейт красил бы каждый
// разбор отрендеренного шаблона, и первый же ложный срабат его бы и отключил.
var interpreterWriteSignal = regexp.MustCompile(
	// Режим разбирается по существу, а не по первой букве: `w`/`a`/`x` в любой
	// компоновке — запись; `r` — запись ТОЛЬКО с `+`. Первая редакция
	// перечисляла буквы одним классом, и `open(path, "rb")` — чтение двоичного
	// файла, форма из живого гейта дерева — объявлялась записью. Ложное
	// срабатывание такого рода снимает гейт целиком.
	`open\s*\([^)]*,\s*['"](?:[wax][a-z+]*|r[a-z]*\+[a-z]*)['"]` +
		`|\.write_text\s*\(|\.write_bytes\s*\(` + // pathlib
		`|os\.(remove|unlink|rename|replace|makedirs|mkdir|rmdir|truncate|symlink|link|chmod)\s*\(` +
		`|shutil\.(copy|copy2|copyfile|copytree|move|rmtree)\s*\(` +
		`|writeFileSync|appendFileSync|renameSync|rmSync|unlinkSync|mkdirSync` + // node
		`|file_put_contents`, // php
)

// gitMutatingSubcommands — подкоманды git, меняющие состояние репозитория.
// Чтения (`ls-files`, `rev-parse`, `show`, `log`, `status`, `diff`) сюда НЕ
// входят намеренно: суиты читают живой репозиторий постоянно.
var gitMutatingSubcommands = map[string]bool{
	"add": true, "rm": true, "mv": true, "commit": true, "commit-tree": true,
	"checkout": true, "switch": true, "restore": true, "reset": true,
	"stash": true, "clean": true, "update-index": true, "update-ref": true,
	"config": true, "init": true, "apply": true, "worktree": true,
	"branch": true, "tag": true, "merge": true, "rebase": true, "am": true,
	"cherry-pick": true, "gc": true, "prune": true, "sparse-checkout": true,
	"notes": true, "symbolic-ref": true, "push": true, "fetch": true,
	"remote": true, "hash-object": true, "write-tree": true, "filter-branch": true,
}

// nonFileTarget — цель перенаправления, файлом НЕ являющаяся: дублирование
// дескриптора (`2>&1`) и служебные потоки (`/dev/null`).
func nonFileTarget(t string) bool {
	t = strings.Trim(t, `"'`)
	if strings.HasPrefix(t, "&") {
		return true
	}
	return strings.HasPrefix(t, "/dev/")
}

// shWrite — осмотренное место записи.
type shWrite struct {
	line  int
	what  string
	why   string
	live  bool
	byCwd bool   // живой признана ТОЛЬКО по рабочему каталогу, а не по значению
	path  string // разрешённый путь относительно корня дерева ("" — не выведен)
	named bool   // и место, которое он называет, действительно известно
}

const (
	whyFile = "запись по пути, производному от корня ЖИВОГО дерева: прерывание прогона " +
		"оставит её в рабочей копии, а резервной копии, из которой её вернуть, у " +
		"прерванного прогона нет"
	whyGit = "изменяющая git-команда против ЖИВОГО репозитория: прерывание прогона до " +
		"уборки оставляет записи в индексе, а состав корпуса гейты берут именно у него"
	whyBody = "встроенный интерпретатор ПИШЕТ, а путь приезжает к нему от корня ЖИВОГО " +
		"дерева: текстовый поиск по shell этой формы не видит, и уборка последней " +
		"строкой прерывание не переживает"
)

// isFlag — аргумент похож на флаг, а не на путь.
func isFlag(w string) bool {
	w = strings.Trim(w, `"'`)
	return strings.HasPrefix(w, "-") && w != "-" && w != "--"
}

// commandHead — имя команды: пропускаются присваивания-префиксы (`FOO=1 cmd`) и
// обёртки, не меняющие смысла (`env`, `command`, `sudo`, `exec`, `time`).
func commandHead(words []string) (string, []string) {
	i := 0
	for i < len(words) {
		w := words[i]
		if assign.MatchString(w) && !strings.HasPrefix(w, "=") {
			i++
			continue
		}
		base := filepath.Base(strings.Trim(w, `"'`))
		switch base {
		case "env", "command", "sudo", "exec", "time", "nice", "nohup", "builtin":
			i++
			continue
		// Ключевые слова оболочки стоят ПЕРЕД командой и её не заменяют. Без
		// этого `if ! f "$live"; then` читалось бы как вызов `if`, и живое
		// значение не уезжало бы в `f` вовсе — а условный вызов помощника это
		// самая частая форма самопроверки.
		case "if", "elif", "while", "until", "then", "do", "else", "!":
			i++
			continue
		}
		return base, words[i+1:]
	}
	return "", nil
}

// inspect — осмотреть команду: обновить происхождение и собрать места записи.
func (s *shScope) inspect(c shCommand) []shWrite {
	s.fn = c.fn
	var writes []shWrite

	// 1. Присваивания. Живое значение делает переменную живой, любое другое —
	//    снимает метку: `WORK="$(mktemp -d)"` перебивает одноимённую живую.
	rest := c.words
	if c.inSub {
		// В подоболочке присваивание наружу не выходит.
		rest = nil
	}
	for len(rest) > 0 {
		w := rest[0]
		if base := filepath.Base(strings.Trim(w, `"'`)); base == "local" || base == "export" ||
			base == "declare" || base == "readonly" || base == "typeset" {
			rest = rest[1:]
			continue
		}
		m := assign.FindStringSubmatch(w)
		if m == nil {
			break
		}
		name, op, val := m[1], m[2], m[3]
		// `X=(...)` и `X+=(...)`: живым делает любой живой элемент.
		live := s.wordLive(val)
		if strings.HasPrefix(strings.TrimSpace(val), "(") {
			live = false
			for _, el := range shellWords(strings.Trim(strings.TrimSpace(val), "()")) {
				if s.wordLive(el) {
					live = true
				}
			}
		}
		if op == "+=" {
			s.live[name] = s.live[name] || live
		} else {
			s.live[name] = live
			if r := s.expand(strings.Trim(strings.TrimSpace(val), `"`)); r.path != "" {
				s.val[name] = r.path
				s.rooted[name] = r.rooted
				// Неточность ПЕРЕЖИВАЕТ присваивание: иначе значение,
				// собранное вокруг заполнителя, читалось бы дальше как
				// выведенное целиком — и гейт уверенно называл бы место,
				// которого нет.
				s.inexact[name] = !r.exact
			} else {
				delete(s.val, name)
				delete(s.rooted, name)
				delete(s.inexact, name)
			}
		}
		if live {
			s.producers++
		}
		rest = rest[1:]
	}

	head, args := commandHead(c.words)

	// 2. Смена рабочего каталога. Нужна, чтобы `cd "$LIVE" && git add …` и
	//    относительная запись после такого `cd` не терялись: путь в них живой,
	//    хотя живого имени в самом слове нет.
	if (head == "cd" || head == "pushd") && !c.inSub {
		for _, a := range args {
			if isFlag(a) {
				continue
			}
			s.cwdLive = s.wordLive(a)
			if pv, ok := s.resolvePath(a); ok {
				s.cwd, s.cwdKnown = pv, true
			} else {
				s.cwdKnown = false
			}
			break
		}
	}

	// 3. Перенаправления.
	for _, r := range c.redirs {
		if nonFileTarget(r.target) {
			continue
		}
		writes = append(writes, s.fileWrite(c.line, "перенаправление "+r.op, r.target))
	}

	// 4. Команды с целью.
	switch {
	case destinationWriters[head]:
		var last string
		for _, a := range args {
			if !isFlag(a) {
				last = a
			}
		}
		if last != "" {
			writes = append(writes, s.fileWrite(c.line, head+" (назначение)", last))
		}
	case argumentWriters[head]:
		for _, a := range args {
			if isFlag(a) {
				continue
			}
			writes = append(writes, s.fileWrite(c.line, head, a))
		}
	case modeWriters[head]:
		seen := false
		for _, a := range args {
			if isFlag(a) {
				continue
			}
			if !seen { // режим/владелец — не путь
				seen = true
				continue
			}
			writes = append(writes, s.fileWrite(c.line, head, a))
		}
	}

	// 5. Правка на месте. Первый нефлаговый аргумент — программа, а не путь.
	if inPlaceEditors[head] && hasInPlaceFlag(args) {
		skipped := false
		for _, a := range args {
			if isFlag(a) {
				continue
			}
			if !skipped && head != "yq" {
				skipped = true
				continue
			}
			writes = append(writes, s.fileWrite(c.line, head+" -i", a))
		}
	}

	// 6. Изменяющая git-команда.
	if head == "git" {
		if w, ok := s.gitWrite(c, args); ok {
			writes = append(writes, w)
		}
	}

	// 7. Встроенный интерпретатор.
	if interpreters[head] && s.caps.interpreterBodies {
		if w, ok := s.interpreterWrite(c, args); ok {
			writes = append(writes, w)
		}
	}

	// 8. Живое значение уехало в функцию того же скрипта — один шаг наружу,
	//    дальше добирает неподвижная точка.
	if head != "" && !isBuiltinCall(head) {
		for i, a := range args {
			if s.wordLive(a) {
				if s.out[head] == nil {
					s.out[head] = map[int]bool{}
				}
				s.out[head][i+1] = true
			}
		}
	}
	return writes
}

// fileWrite — место записи в файловую систему с разрешённой целью.
//
// Цель живая, если её значение происходит от корня живого дерева ЛИБО она
// относительна, а рабочий каталог заведомо переведён в дерево видимым `cd`.
// Рабочий каталог, унаследованный от вызывающего, живым НЕ считается: скрипту
// он неизвестен, и обратное красило бы каждую запись каждого скрипта.
func (s *shScope) fileWrite(line int, what, target string) shWrite {
	w := shWrite{line: line, what: what, why: whyFile}
	t := strings.Trim(target, `"'`)
	switch {
	case s.wordLive(target):
		w.live = true
	case s.cwdLive && t != "" && !strings.HasPrefix(t, "/"):
		w.live, w.byCwd = true, true
	}
	if w.live {
		w.path, w.named = s.resolvePath(target)
	}
	return w
}

func hasInPlaceFlag(args []string) bool {
	for _, a := range args {
		a = strings.Trim(a, `"'`)
		if a == "-i" || strings.HasPrefix(a, "-i.") || a == "--in-place" ||
			(strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "i") &&
				len(a) <= 4 && a != "-e" && a != "-n") {
			return true
		}
	}
	return false
}

// isBuiltinCall — встроенные и заведомо внешние команды, которые функцией
// скрипта быть не могут: передавать в них живое значение «шагом наружу» незачем.
func isBuiltinCall(head string) bool {
	switch head {
	case "echo", "printf", "cd", "pushd", "popd", "return", "exit", "shift", "read",
		"test", "true", "false", "eval", "source", ".", "set", "unset", "trap",
		"cat", "grep", "sed", "awk", "cut", "sort", "head", "tail", "wc", "tr",
		"git", "helm", "kubectl", "docker", "curl", "jq", "yq", "find", "xargs",
		"cp", "mv", "rm", "mkdir", "touch", "ln", "chmod", "chown", "tee", "diff":
		return true
	}
	return false
}

// gitWrite — изменяющая подкоманда git против живого репозитория.
func (s *shScope) gitWrite(c shCommand, args []string) (shWrite, bool) {
	dirLive := s.cwdLive
	sub := ""
	for i := 0; i < len(args); i++ {
		a := strings.Trim(args[i], `"'`)
		if a == "-C" && i+1 < len(args) {
			dirLive = s.wordLive(args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if sub == "" {
			sub = a
		}
	}
	if !gitMutatingSubcommands[sub] {
		return shWrite{}, false
	}
	live := dirLive
	for _, a := range args {
		if s.wordLive(a) {
			live = true
		}
	}
	return shWrite{line: c.line, what: "git " + sub, why: whyGit, live: live}, true
}

// interpreterWrite — встроенный интерпретатор, чьё ТЕЛО пишет.
func (s *shScope) interpreterWrite(c shCommand, args []string) (shWrite, bool) {
	body, expands := interpreterBody(c, args)
	if body == "" || !interpreterWriteSignal.MatchString(body) {
		return shWrite{}, false
	}
	live := false
	path, named := "", false
	for _, a := range args {
		if isFlag(a) {
			continue
		}
		if s.wordLive(a) {
			live = true
			if path == "" {
				path, named = s.resolvePath(a)
			}
		}
	}
	// Незакавыченный разделитель heredoc → shell подставляет переменные прямо в
	// тело, и живое значение попадает туда БЕЗ аргумента.
	if expands && s.wordLive(body) {
		live = true
	}
	head, _ := commandHead(c.words)
	return shWrite{
		line: c.line, what: head + " (запись в теле)", why: whyBody,
		live: live, path: path, named: named,
	}, true
}

// interpreterBody — текст программы: тело heredoc либо аргумент `-c`/`-e`.
func interpreterBody(c shCommand, args []string) (string, bool) {
	var b strings.Builder
	expands := false
	for _, d := range c.docs {
		b.WriteString(d.body)
		b.WriteByte('\n')
		if d.expand {
			expands = true
		}
	}
	for i := 0; i < len(args); i++ {
		a := strings.Trim(args[i], `"'`)
		if (a == "-c" || a == "-e") && i+1 < len(args) {
			b.WriteString(strings.Trim(args[i+1], `"'`))
			b.WriteByte('\n')
			i++
		}
	}
	return b.String(), expands
}

// ─────────────────────────────────────────────────────────────────────────────
// Предикат целиком.
// ─────────────────────────────────────────────────────────────────────────────

// shellAuditCapabilities — две способности предиката, которых у поиска по
// ТЕКСТУ нет и быть не может: прочитать тело встроенного интерпретатора и
// провести происхождение через позиционные параметры функций скрипта.
//
// Отключаются они ровно в одном месте — в контроле, который МЕРЯЕТ, сколько
// предмета виден без них. Число из этого замера и есть довод, по которому
// текстовый предикат отвергнут; без способности его получить довод остался бы
// утверждением о вкусе.
type shellAuditCapabilities struct {
	interpreterBodies bool
	paramFlow         bool
}

// fullShellAudit — предикат целиком. Всё, кроме названного контроля, зовёт его.
var fullShellAudit = shellAuditCapabilities{interpreterBodies: true, paramFlow: true}

// auditShellProbeWritesToLiveTree — весь предикат. Вход — исходники суит
// (путь → текст), чтобы инъекция гоняла ТУ ЖЕ функцию, что и гейт по дереву.
func auditShellProbeWritesToLiveTree(
	sources map[string]string,
	ignored func(repoRelPath string) bool,
) ([]shellWriteFinding, shellWriteCensus) {
	return auditShellProbeWritesToLiveTreeWith(sources, ignored, fullShellAudit)
}

func auditShellProbeWritesToLiveTreeWith(
	sources map[string]string,
	ignored func(repoRelPath string) bool,
	caps shellAuditCapabilities,
) ([]shellWriteFinding, shellWriteCensus) {
	var (
		findings []shellWriteFinding
		census   shellWriteCensus
	)
	rels := make([]string, 0, len(sources))
	for rel := range sources {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		cmds := shellCommands(sources[rel])
		if len(cmds) == 0 {
			continue
		}
		census.Files++
		census.Commands += len(cmds)
		for _, c := range cmds {
			census.Bodies += len(c.docs)
		}

		// Неподвижная точка по позиционным параметрам: помощник зовёт помощника,
		// и прежняя редакция одной из суит уходила на ТРИ шага.
		liveParams := map[string]map[int]bool{}
		rounds := 4
		if !caps.paramFlow {
			rounds = 1
		}
		var last []shWrite
		var lastScope *shScope
		for range rounds {
			sc := &shScope{
				live:       map[string]bool{},
				val:        map[string]string{},
				rooted:     map[string]bool{},
				inexact:    map[string]bool{},
				liveParams: liveParams,
				out:        map[string]map[int]bool{},
				self:       rel,
				caps:       caps,
			}
			last = nil
			for _, c := range cmds {
				last = append(last, sc.inspect(c)...)
			}
			lastScope = sc
			changed := false
			for callee, idx := range sc.out {
				if liveParams[callee] == nil {
					liveParams[callee] = map[int]bool{}
				}
				for i := range idx {
					if !liveParams[callee][i] {
						liveParams[callee][i] = true
						changed = true
					}
				}
			}
			if !changed {
				break
			}
		}
		census.Producers += lastScope.producers

		// Координата у находки одна на строку и вид записи: одна строка может
		// нести и перенаправление, и команду.
		seen := map[string]bool{}
		for _, w := range last {
			census.Writes++
			if !w.live {
				continue
			}
			key := fmt.Sprintf("%d|%s", w.line, w.what)
			if seen[key] {
				continue
			}
			seen[key] = true
			census.Tainted++
			// Дерево само объявляет часть себя артефактами: запись туда корпуса
			// не портит — каталог ровно для того и заведён, а прерывание
			// оставляет там мусор, который никто не читает как содержимое дерева.
			// Послабление ИСТЕКАЕТ САМО: снимут строку из `.gitignore` — то же
			// место станет находкой без единой правки гейта.
			if w.named && w.path != "" && ignored != nil && ignored(w.path) {
				census.Artifacts++
				continue
			}
			// Живость по РАБОЧЕМУ КАТАЛОГУ — вывод слабее, чем живость по
			// ЗНАЧЕНИЮ: цель могла оказаться и абсолютной, и вне дерева. Если
			// вдобавок место назвать не удалось, утверждать нечего — и молчание
			// здесь названо счётчиком, а не умолчано. Живость по значению
			// такого послабления НЕ получает: путь, производный от корня
			// дерева, лежит в дереве, как бы он ни звался.
			if w.byCwd && !w.named {
				census.Unnamed++
				continue
			}
			findings = append(findings, shellWriteFinding{
				File: rel, Line: w.line, What: w.what, Path: w.path, Why: w.why,
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].What < findings[j].What
	})
	return findings, census
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт по дереву.
// ─────────────────────────────────────────────────────────────────────────────

// TestShellProbesDoNotWriteIntoTheTreeTheyRunFrom — гейт по дереву.
func TestShellProbesDoNotWriteIntoTheTreeTheyRunFrom(t *testing.T) {
	root := repoRoot(t)
	sources, outside := shellProbeSources(t, root)

	if len(sources) == 0 {
		t.Fatal("обход не нашёл ни одной shell-суиты — гейт беспредметен: либо состав " +
			"дерева взять не удалось, либо раскладка проб изменилась. В обоих случаях " +
			"зелёный вердикт ниже был бы получен даром.")
	}

	findings, census := auditShellProbeWritesToLiveTree(sources, gitIgnores(t, root))

	// Предпосылки предиката. Без производителя живого корня прослеживать нечего,
	// без единого места записи — не о чем судить: молчание тогда означало бы
	// поломку разбора, а не чистоту дерева.
	if census.Producers == 0 {
		t.Error("в корпусе не найдено ни одного производителя живого корня — источник, " +
			"от которого предикат ведёт происхождение, исчез, и «ноль находок» ниже " +
			"неотличимо от «ноль прочитанного»")
	}
	if census.Writes == 0 {
		t.Error("в корпусе не найдено ни одного места записи — распознавание записи сломано")
	}
	if census.Bodies == 0 {
		t.Error("в корпусе не найдено ни одного тела heredoc — разбор встроенных " +
			"интерпретаторов сломан, а это ровно та форма, которой текстовый предикат " +
			"не видел и ради которой гейт заведён")
	}

	for _, f := range findings {
		target := f.Path
		if target == "" {
			target = "путь вывести не удалось — цель собрана из значения, которого " +
				"в скрипте нет"
		}
		t.Errorf("суита %s:%d пишет через %s в %s:\n  %s\n\n"+
			"Исход один: изолировать — свой временный каталог (`mktemp -d`), своё дерево "+
			"(`cp -r \"$ROOT/.\" \"$WORK/\"` и прогон копии гейта из него) либо свой "+
			"репозиторий (`git init` в нём). Возврат последней строкой или ловушкой "+
			"исходом НЕ является: прерывание до неё не доходит.",
			f.File, f.Line, f.What, target, f.Why)
	}

	// Граница корпуса печатается ВСЕГДА и числом. Корпус собирается по двум
	// конвенциям дерева, а инструменты (генераторы, посев, подъём стенда) в него
	// не входят: они пишут в дерево ПО СВОЕМУ НАЗНАЧЕНИЮ. Молча такую границу
	// держать нельзя — «ноль находок» тогда означало бы и «в корпусе чисто», и
	// «корпус узок», а это разные вещи.
	t.Logf("граница корпуса: отслеживаемых shell-скриптов %d, из них суит %d, "+
		"инструментов вне корпуса %d", len(sources)+outside, len(sources), outside)

	t.Logf("перепись: суит прочитано %d, команд осмотрено %d, выводов живого корня %d, "+
		"тел встроенных интерпретаторов %d, мест записи осмотрено %d, из них по живому "+
		"корню %d, из тех по объявленному артефактом пути %d, с неназываемой целью "+
		"по рабочему каталогу %d, находок %d",
		census.Files, census.Commands, census.Producers, census.Bodies,
		census.Writes, census.Tainted, census.Artifacts, census.Unnamed, len(findings))
}

// shellProbeSources — исходники shell-суит из СОСТАВА дерева (индекс git), а не
// с диска: посторонний каталог рядом с репозиторием иначе влиял бы на вердикт.
//
// Корпус — то же, что у Go-половины: файлы, ОБЪЯВЛЯЮЩИЕ себя пробами. У Go это
// суффикс `_test.go`; у shell такого объявления в языке нет, поэтому берутся ДВА
// признака раскладки — сегмент пути `tests/`/`test/` и `test` в имени файла.
// Ни один не полон сам по себе, и это измерено: по имени 36, по раскладке 63,
// вместе 65 — то есть каждый пропускает то, что видит другой. Гейт с одним
// признаком тихо не читал бы целый вид суит.
//
// Инструменты дерева (генераторы, посев, подъём стенда) в корпус НЕ входят
// намеренно: они пишут в дерево ПО СВОЕМУ НАЗНАЧЕНИЮ, и запрет был бы запретом
// на них — та же граница, по которой Go-половина исключает не-тестовые `.go`.
// Цена границы ИЗМЕРЕНА, а не предположена: предикат прогонялся и по 54
// исключённым скриптам, и живая запись в дерево нашлась там ровно одна —
// генератор, кладущий свой же сгенерированный файл. То есть сегодня граница не
// прячет ни одного экземпляра класса. Число печатается гейтом на каждом прогоне
// («граница корпуса»), чтобы сужение корпуса нельзя было провести молча.
//
// Тот же прогон по исключённым нашёл ТРИ дефекта самого предиката — режим
// `"rb"`, читавшийся как запись; кавычки, не переживавшие перевод строки; и
// умолчание `${1:-…}` с вложенной подстановкой. Каждый закрыт своей парой в
// инъекции. Это и есть довод в пользу того, чтобы мерить шире корпуса, даже
// когда судишь уже корпус.
func shellProbeSources(t *testing.T, root string) (map[string]string, int) {
	t.Helper()
	out := map[string]string{}
	outside := 0
	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("корень %s не открыт: %v — состав суит взять неоткуда, и «ноль находок» "+
			"было бы утверждением ни о чём", root, err)
	}
	defer func() { _ = osRoot.Close() }()
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasSuffix(rel, ".sh") {
			continue
		}
		if !isShellProbePath(rel) {
			outside++
			continue
		}
		body, ok := readTracked(osRoot, rel)
		if !ok {
			continue
		}
		out[rel] = body
	}
	return out, outside
}

// gitIgnores — объявляет ли САМО ДЕРЕВО этот путь артефактом.
//
// Спрашивается git, а не список каталогов в гейте: перечень разошёлся бы с
// `.gitignore` молча, а тогда послабление перестало бы истекать. Ответ на путь,
// которого на диске нет, `check-ignore` даёт по правилам — прогон в свежем
// клоне, где `out/` ещё не создан, судит так же, как прогон после первого
// прогона суит.
func gitIgnores(t *testing.T, root string) func(string) bool {
	t.Helper()
	memo := map[string]bool{}
	return func(rel string) bool {
		if rel == "" || rel == "." {
			return false
		}
		if v, ok := memo[rel]; ok {
			return v
		}
		// Разрешённый путь — ПРЕФИКС: он мог оборваться на середине имени
		// (`out/${stem}.json` → `out/`). Вопрос «объявлено ли это место
		// артефактом» решается каталогом, поэтому спрашивается и сам путь, и
		// заведомый файл под ним.
		v := gitCheckIgnore(root, rel) || gitCheckIgnore(root, filepath.Join(rel, "x"))
		memo[rel] = v
		return v
	}
}

func gitCheckIgnore(root, rel string) bool {
	cmd := gitenv.Command(root, "check-ignore", "-q", "--no-index", "--", rel)
	return cmd.Run() == nil
}

// testPathSegment — сегмент пути, объявляющий каталог пробным.
var testPathSegment = regexp.MustCompile(`(^|/)tests?/`)

// isShellProbePath — файл объявляет себя shell-пробой.
func isShellProbePath(rel string) bool {
	if !strings.HasSuffix(rel, ".sh") {
		return false
	}
	return testPathSegment.MatchString(rel) || strings.Contains(filepath.Base(rel), "test")
}
