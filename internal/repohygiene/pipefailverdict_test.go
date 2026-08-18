// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pipefailverdict_test.go — гейт класса «вердикт получен из трубы под pipefail».
//
// # Предмет
//
// Под `set -o pipefail` статус конвейера — это статус ПОСЛЕДНЕГО НЕНУЛЕВОГО
// звена, а не последнего звена. `grep -q` выходит НА ПЕРВОМ СОВПАДЕНИИ, не
// дочитывая вход. Писателю слева, которому осталось что писать, ядро посылает
// SIGPIPE; он умирает со статусом 141 (либо, если это встроенная команда bash,
// печатает `printf: write error: Broken pipe` и возвращает ненулевой статус).
// `pipefail` поднимает этот статус до статуса всего конвейера — и `if` уходит
// в `else`.
//
// То есть СОВПАДЕНИЕ НАЙДЕНО И ОБЪЯВЛЕНО НЕНАЙДЕННЫМ. Отказ односторонний
// (зелёное не подделывается), но вердикт перестаёт быть свойством дерева.
//
// # Почему это не ловится обзором диффа и не воспроизводится локально
//
// Гонки нет, пока весь вывод помещается в буфер трубы (на Linux — 64 KiB):
// писатель успевает вернуть управление из write(2) до того, как grep выйдет.
// Замер на этой машине (совпадение в начале вывода, 200 прогонов на размер):
// 8 КБ — 0 ложных отказов, 70 КБ — 177, 300 КБ — 200. То есть форма зеленеет на
// маленьком выводе и краснеет на большом — «локально зелено, на ранере красно»
// при одном и том же дереве. Наблюдалось:
// `services/nlb/deploy/tests/render-guard.sh`, прогон 95532676628, задача #658.
//
// Порог зависит от окружения, а не только от размера: он сдвигается вместе с
// объёмом вывода, планировщиком и тем, где в выводе стоит совпадение. Поэтому
// запрет — на ФОРМУ, а не на «слишком большой вывод»: последнее не проверяемо.
//
// # Что ловится
//
// Конвейер, чьё звено `grep` выходит ДО конца входа — флаги `-q`/`--quiet`/
// `--silent` (выход по первому совпадению) и `-m N`/`--max-count=N` (выход по
// N-му), — в файле, который включает `pipefail`, и чей статус читается как
// вердикт.
//
// # Что НЕ ловится и почему это названо, а не умолчано
//
//   - `grep` без ранне-выходящих флагов — он дочитывает вход до EOF, писатель
//     SIGPIPE не получает. Это законная форма, и гейт обязан на ней молчать;
//   - конвейер, чей статус ЯВНО отброшен (`|| true`, `|| :`) — вердикта из него
//     никто не берёт;
//   - файл без `pipefail` — у запрета нет предпосылки: там статус конвейера
//     берётся от последнего звена, а `grep -q` на совпадении даёт 0.
//
// # Чего гейт НЕ РАЗБИРАЕТ (граница покрытия, измеренная, а не предположенная)
//
//   - `head`/`head -n N` — тоже ранне-выходящий потребитель, и под `pipefail`
//     класс ТОТ ЖЕ. Он здесь НЕ закрыт, и это надо назвать прямо, а не выдать
//     охват шире фактического. Замер на день заведения: 48 вхождений в файлах
//     под `pipefail`, и все — ПРОИЗВОДИТЕЛИ ЗНАЧЕНИЯ (`x="$(… | head -1)"`,
//     тело функции-геттера), где вызывающему нужно напечатанное, а не статус.
//     Ни одного, где статус конвейера читался бы как вердикт, не осталось:
//     единственный такой (`scripts/ci-local.sh`) снят этим же изменением —
//     версия теперь берётся подстановкой и раскрытием параметра, без `head`.
//     Но «статус производителя значения всё же может уехать наверх под `set -e`»
//     остаётся верным, и закрывается это ДРУГИМ предикатом — «статус подстановки
//     читается как вердикт», — которого здесь нет. Предикат для перемера:
//     `git ls-files -- '*.sh' | xargs grep -nE '\|[[:space:]]*head[[:space:]]'`;
//   - блоки `run:` в `.github/workflows/*.yml` — это YAML-скаляр, а не shell-файл,
//     и его разбор потребовал бы второго парсера. На день заведения гейта форма
//     там была в ОДНОМ месте (`ui.yml`, проверка формы тега образа) и снята этим
//     же изменением. Предикат для перемера:
//     `git grep -nE '\|[[:space:]]*grep[[:space:]]+-[A-Za-z]*q' -- .github/workflows/`;
//   - рецепты Makefile — форма там не встречается вовсе (замер: 0).
//
// # Как разбирается текст
//
// Shell не имеет AST под рукой, поэтому текст маскируется посимвольным
// автоматом: содержимое комментариев, тел heredoc и строковых литералов
// заменяется пробелами с сохранением разметки строк. Подстановка `$( … )`
// ВНУТРИ двойных кавычек снова считается кодом — иначе форма
// `x="$(cmd | grep -m1 y)"` была бы невидима. Признак ищется только по
// незамаскированному, поэтому упоминание формы в комментарии (а такие в дереве
// есть — этот класс уже описывали дважды) находкой не становится.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Находка и перепись
// ─────────────────────────────────────────────────────────────────────────────

type pipefailFinding struct {
	File    string
	Line    int    // физическая строка, где стоит ранне-выходящий grep
	Flag    string // что именно делает выход ранним: "-q" | "-m"
	Snippet string
}

func (f pipefailFinding) String() string {
	return fmt.Sprintf("%s:%d — вердикт берётся из трубы в `grep %s` под pipefail: %s",
		f.File, f.Line, f.Flag, f.Snippet)
}

type pipefailCensus struct {
	FilesRead     int // прочитано shell-скриптов
	FilesPipefail int // из них включают pipefail
	GrepPipes     int // конвейеров `… | grep …` рассмотрено в них
	EarlyExit     int // из них с ранне-выходящим grep
	Discarded     int // из ранне-выходящих: статус явно отброшен (|| true / || :)
}

// ─────────────────────────────────────────────────────────────────────────────
// Маскирование: комментарий, heredoc, строковый литерал → пробелы
// ─────────────────────────────────────────────────────────────────────────────

const (
	shNormal = iota
	shSingle
	shDouble
)

type heredocSpec struct {
	delim string
	strip bool // <<- : ведущие табуляции у ограничителя срезаются
}

var heredocStart = regexp.MustCompile(`<<-?\s*(?:'([^']*)'|"([^"]*)"|([A-Za-z_][A-Za-z0-9_]*))`)

// maskShellSource возвращает копию src той же длины, где байты комментариев,
// тел heredoc и содержимого строковых литералов заменены пробелами. Переводы
// строк сохраняются, поэтому нумерация строк маски и исходника совпадает.
func maskShellSource(src string) string {
	b := []byte(src)
	out := []byte(src)

	blank := func(i int) {
		if b[i] != '\n' {
			out[i] = ' '
		}
	}

	stack := []int{shNormal}
	cur := func() int { return stack[len(stack)-1] }
	push := func(s int) { stack = append(stack, s) }
	pop := func() {
		if len(stack) > 1 {
			stack = stack[:len(stack)-1]
		}
	}

	var pending []heredocSpec
	var active *heredocSpec

	i := 0
	for i < len(b) {
		c := b[i]

		// ── тело heredoc: гасим до строки-ограничителя ───────────────────────
		if active != nil {
			lineEnd := i
			for lineEnd < len(b) && b[lineEnd] != '\n' {
				lineEnd++
			}
			line := string(b[i:lineEnd])
			cmp := line
			if active.strip {
				cmp = strings.TrimLeft(cmp, "\t")
			}
			if strings.TrimRight(cmp, "\r") == active.delim {
				active = nil // строка-ограничитель — это код, её не гасим
			} else {
				for j := i; j < lineEnd; j++ {
					blank(j)
				}
			}
			i = lineEnd
			if i < len(b) {
				i++ // перешагнуть \n
			}
			continue
		}

		switch cur() {
		case shSingle:
			if c == '\'' {
				pop()
			} else {
				blank(i)
			}
			i++
			continue

		case shDouble:
			if c == '\\' && i+1 < len(b) {
				blank(i)
				blank(i + 1)
				i += 2
				continue
			}
			// `$( … )` внутри кавычек — снова код, иначе форма
			// x="$(cmd | grep -m1 y)" была бы невидима.
			if c == '$' && i+1 < len(b) && b[i+1] == '(' {
				blank(i)
				push(shNormal)
				i += 2
				continue
			}
			if c == '"' {
				pop()
			} else {
				blank(i)
			}
			i++
			continue
		}

		// ── shNormal ─────────────────────────────────────────────────────────
		switch {
		case c == '\\' && i+1 < len(b) && b[i+1] != '\n':
			i += 2
			continue

		case c == '\'':
			push(shSingle)
			i++
			continue

		case c == '"':
			push(shDouble)
			i++
			continue

		case c == ')' && len(stack) > 1:
			// закрытие $( … ), открытого внутри двойных кавычек
			pop()
			blank(i)
			i++
			continue

		case c == '#' && commentStartsHere(b, i):
			for i < len(b) && b[i] != '\n' {
				blank(i)
				i++
			}
			continue

		case c == '<' && i+1 < len(b) && b[i+1] == '<':
			// `<<<` — herestring, не heredoc: трубы нет, писателя нет.
			if i+2 < len(b) && b[i+2] == '<' {
				i += 3
				continue
			}
			if m := heredocStart.FindStringSubmatch(string(b[i:min(i+80, len(b))])); m != nil {
				spec := heredocSpec{strip: i+2 < len(b) && b[i+2] == '-'}
				spec.delim = m[1] + m[2] + m[3]
				if spec.delim != "" {
					pending = append(pending, spec)
				}
			}
			i += 2
			continue

		case c == '\n':
			if len(pending) > 0 {
				active = &pending[0]
				pending = pending[1:]
			}
			// разорванная строка не должна тянуть состояние кавычек вечно
			i++
			continue
		}

		i++
	}

	return string(out)
}

// commentStartsHere — `#` начинает комментарий, только когда стоит в начале
// слова: в `foo#bar` и `${x#y}` это обычный символ, а не комментарий.
func commentStartsHere(b []byte, i int) bool {
	if i == 0 {
		return true
	}
	switch b[i-1] {
	case ' ', '\t', '\n', ';', '|', '&', '(', '{':
		return true
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Логические строки: конвейер и продолжения переносятся на следующую строку
// ─────────────────────────────────────────────────────────────────────────────

type logicalLine struct {
	text  string // маскированный текст, склеенный из физических строк
	first int    // номер первой физической строки (с 1)
	// starts[k] — смещение в text, с которого начинается физическая строка first+k
	starts []int
	lines  []int
}

func splitLogicalLines(masked string) []logicalLine {
	phys := strings.Split(masked, "\n")
	var out []logicalLine

	continues := func(cur, next string) bool {
		c := strings.TrimRight(strings.TrimSpace(cur), " \t")
		n := strings.TrimSpace(next)
		if strings.HasSuffix(c, `\`) {
			return true
		}
		if strings.HasSuffix(c, "|") || strings.HasSuffix(c, "&&") || strings.HasSuffix(c, "||") {
			return true
		}
		if strings.HasPrefix(n, "&&") || strings.HasPrefix(n, "||") {
			return true
		}
		if strings.HasPrefix(n, "|") && !strings.HasPrefix(n, "||") {
			return true
		}
		return false
	}

	for i := 0; i < len(phys); i++ {
		ll := logicalLine{first: i + 1}
		var sb strings.Builder
		for {
			ll.starts = append(ll.starts, sb.Len())
			ll.lines = append(ll.lines, i+1)
			sb.WriteString(phys[i])
			if i+1 < len(phys) && continues(phys[i], phys[i+1]) {
				sb.WriteString(" ")
				i++
				continue
			}
			break
		}
		ll.text = sb.String()
		out = append(out, ll)
	}
	return out
}

// physLineAt — какой физической строке принадлежит смещение off.
func (l logicalLine) physLineAt(off int) int {
	line := l.first
	for k, s := range l.starts {
		if off >= s {
			line = l.lines[k]
		}
	}
	return line
}

// ─────────────────────────────────────────────────────────────────────────────
// Признак
// ─────────────────────────────────────────────────────────────────────────────

// pipeIntoGrep — труба в grep/egrep/fgrep. Захватывает хвост строки, чтобы из
// него прочитать флаги.
var pipeIntoGrep = regexp.MustCompile(`\|[ \t]*(?:[A-Za-z0-9_/.-]*/)?(?:grep|egrep|fgrep)\b`)

// statusDiscarded — статус конвейера явно выброшен.
var statusDiscarded = regexp.MustCompile(`\|\|[ \t]*(?:true|:)(?:[ \t]|;|$)`)

var (
	longQuiet    = regexp.MustCompile(`--(?:quiet|silent)\b`)
	longMaxCount = regexp.MustCompile(`--max-count\b`)
)

// grepEarlyExitFlag читает флаги grep, начинающиеся сразу за его именем, и
// возвращает "-q"/"-m", если среди них есть ранне-выходящий, иначе "".
//
// Читаются ТОЛЬКО ведущие токены-флаги: после первого не-флага идёт образец, и
// `-q` внутри образца флагом не является.
func grepEarlyExitFlag(tail string) string {
	fields := strings.Fields(tail)
	for _, f := range fields {
		if f == "--" {
			return ""
		}
		if !strings.HasPrefix(f, "-") || f == "-" {
			return ""
		}
		if strings.HasPrefix(f, "--") {
			switch {
			case longQuiet.MatchString(f):
				return "-q"
			case longMaxCount.MatchString(f):
				return "-m"
			}
			continue
		}
		// короткая связка: -qF, -Eq, -m1, -qxF …
		body := f[1:]
		if strings.ContainsAny(body, "qm") {
			// `-m` может нести число слитно (-m1); буква всё равно та же.
			if strings.Contains(body, "q") {
				return "-q"
			}
			return "-m"
		}
		// флаг с отдельным значением (-e PATTERN, -A 1) — его значение не флаг.
		if strings.ContainsAny(body, "efAB") {
			return ""
		}
	}
	return ""
}

// auditPipefailVerdicts — единственная реализация признака. Её гоняют и обход
// дерева, и инъекция: проба, повторяющая логику своей копией, доказывала бы
// свойство копии.
func auditPipefailVerdicts(sources map[string]string) (pipefailCensus, []pipefailFinding) {
	var c pipefailCensus
	var findings []pipefailFinding

	for _, file := range pipefailSortedFiles(sources) {
		src := sources[file]
		c.FilesRead++

		masked := maskShellSource(src)
		if !strings.Contains(masked, "pipefail") {
			continue // предпосылки запрета нет: статус берётся от последнего звена
		}
		c.FilesPipefail++

		for _, ll := range splitLogicalLines(masked) {
			for _, loc := range pipeIntoGrep.FindAllStringIndex(ll.text, -1) {
				c.GrepPipes++
				flag := grepEarlyExitFlag(ll.text[loc[1]:])
				if flag == "" {
					continue // grep дочитывает вход — писатель SIGPIPE не получит
				}
				c.EarlyExit++
				if statusDiscarded.MatchString(ll.text[loc[1]:]) {
					c.Discarded++
					continue // вердикта из этого конвейера никто не берёт
				}
				line := ll.physLineAt(loc[0])
				findings = append(findings, pipefailFinding{
					File:    file,
					Line:    line,
					Flag:    flag,
					Snippet: snippetOf(src, line),
				})
			}
		}
	}
	return c, findings
}

func snippetOf(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line-1 < 0 || line-1 >= len(lines) {
		return ""
	}
	s := strings.TrimSpace(lines[line-1])
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

func pipefailSortedFiles(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Корпус: shell-скрипты дерева
// ─────────────────────────────────────────────────────────────────────────────

// pipefailShellSources — отслеживаемые shell-скрипты: по расширению `.sh` ЛИБО
// по shebang. Второй признак не косметика: `scripts/hooks/pre-push` расширения
// не несёт, включает `pipefail` и в перепись по одному расширению не попал бы.
func pipefailShellSources(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range trackedPaths(t, root) {
		abs := filepath.Join(root, rel)
		b, err := os.ReadFile(abs)
		if err != nil {
			continue // нет в рабочем дереве (submodule, sparse) — не находка
		}
		if strings.HasSuffix(rel, ".sh") {
			out[rel] = string(b)
			continue
		}
		head := b
		if len(head) > 64 {
			head = head[:64]
		}
		first := strings.SplitN(string(head), "\n", 2)[0]
		if strings.HasPrefix(first, "#!") && (strings.Contains(first, "bash") ||
			strings.Contains(first, "/sh") || strings.HasSuffix(first, "sh")) {
			out[rel] = string(b)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Гейт
// ─────────────────────────────────────────────────────────────────────────────

// TestPipefailVerdictNeverComesFromAPipe — гейт задачи #658.
func TestPipefailVerdictNeverComesFromAPipe(t *testing.T) {
	root := repoRoot(t)
	sources := pipefailShellSources(t, root)

	// Предпосылка 1: корпус непуст. «Ноль находок» на нуле прочитанного —
	// не вердикт, а беспредметная перепись.
	if len(sources) == 0 {
		t.Fatalf("в дереве не найдено ни одного shell-скрипта — гейт беспредметен: " +
			"перепись прочитала ноль файлов, и «ноль находок» ничего не означает")
	}

	census, findings := auditPipefailVerdicts(sources)

	// Предпосылка 2: запрет обоснован тем, что `pipefail` превращает SIGPIPE
	// писателя в статус конвейера. Если pipefail не включает НИ ОДИН файл —
	// признак перестал что-либо измерять, и молчание гейта ложно.
	if census.FilesPipefail == 0 {
		t.Fatalf("прочитано %d shell-скриптов, и ни один не включает pipefail — "+
			"предпосылка запрета исчезла, значит признак больше ничего не измеряет "+
			"(либо сломан разбор). Гейт обязан покраснеть, а не промолчать",
			census.FilesRead)
	}

	for _, f := range findings {
		t.Errorf("%s\n\n"+
			"  `grep %s` выходит до конца входа; писатель слева получает SIGPIPE, "+
			"а `pipefail` поднимает это до статуса конвейера — найденное объявляется "+
			"ненайденным.\n"+
			"  Чинить сравнением без внешнего процесса:\n"+
			"    литерал      → [[ \"$out\" == *\"$needle\"* ]]\n"+
			"    выражение    → [[ \"$out\" =~ $re ]]   (правая часть БЕЗ кавычек)\n"+
			"    строка целиком → [[ $'\\n'\"$out\"$'\\n' == *$'\\n'\"$needle\"$'\\n'* ]]\n"+
			"  Задача: #658",
			f, f.Flag)
	}

	t.Logf("перепись: shell-скриптов прочитано %d, из них под pipefail %d, "+
		"конвейеров `| grep` рассмотрено %d, из них ранне-выходящих %d "+
		"(статус явно отброшен у %d), находок %d",
		census.FilesRead, census.FilesPipefail, census.GrepPipes,
		census.EarlyExit, census.Discarded, len(findings))
}
