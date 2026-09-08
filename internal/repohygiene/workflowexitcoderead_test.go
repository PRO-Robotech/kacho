// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// workflowexitcoderead_test.go — ни один шаг конвейера не читает код возврата,
// до которого он не доходит.
//
// # Предмет
//
// Провайдер исполняет тело `run:` через `bash --noprofile --norc -eo pipefail`.
// Под `-e` команда, чей отказ не стоит в условии, обрывает оболочку НЕМЕДЛЕННО.
// Поэтому форма
//
//	cmd; rc=$?
//	case "$rc" in …
//
// не работает НИКОГДА: до присваивания дело не доходит, до развилки тем более.
// Шаг при этом выглядит обычным красным — то есть различение исходов, ради
// которого развилка писалась, отсутствует, а его отсутствие ничем не видно.
//
// Класс дорог именно тишиной. Найденный экземпляр (#2346) объявлял третью
// категорию исхода («прогон недействителен») и печатал её аннотацию ноль раз за
// всю свою жизнь; соседний экземпляр того же класса (#1073) давал задание,
// падавшее за шесть секунд при объявленном пределе в девяносто минут.
//
// # Что здесь считается защитой
//
// Код возврата берётся КАК ДАННЫЕ, а не как условие продолжения. Форм две, обе
// законны, и обе ставят команду в условный контекст, где `-e` не действует:
//
//   - `cmd || rc=$?` — читатель И ЕСТЬ страховка;
//   - `cmd && rc=0 || rc=$?` — то же, плюс явный ноль на успехе.
//
// Третья защита — снятие самого режима: `set +e` до чтения. Она законна, но
// действует до конца блока, поэтому гейт следит за её областью, а не за фактом
// упоминания.
//
// # Границы, названные прямо
//
// Гейт судит РАЗОБРАННЫЙ YAML и внутри тела `run:` отбрасывает комментарии
// оболочки: форма `|| rc=$?` встречается в объяснениях этого самого правила
// (в `pinned-tools-freshness.yml` и в шапке конвейера вынесенной службы), и
// проверка по подстроке краснела бы на собственном обосновании.
//
// Форм объявления шагов знает ДВЕ — работы конвейера и составное действие;
// вторых в дереве сегодня ноль, и перепись печатает это число отдельно, чтобы
// «нарушений в них нет» не читалось как «их не искали».
//
// Шаги на не-bash оболочке (`shell: python`, `pwsh`) и шаги с собственным
// шаблоном без `-e` (`shell: bash {0}`) под наблюдение не подпадают: `-e` там не
// действует, и предмета у запрета нет. Это не послабление, а отсутствие
// предмета, и гейт печатает, сколько блоков он по этой причине не судил.
package repohygiene

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// exitCodeRead — одно чтение `$?` в теле `run:`.
type exitCodeRead struct {
	File    string
	Job     string
	Step    string
	Line    int    // строка внутри тела `run:`, 1-based
	Text    string // сама строка, без комментария
	Guard   string // чем прикрыто: "||", "&&||", "set +e", "" — ничем
	Guarded bool
}

// wfShellHasErrExit — исполняется ли тело `run:` с включённым `-e`.
//
// Второй результат — «оболочка вообще bash-подобная». Шаг на python/pwsh не
// имеет к предмету отношения, и считать его «защищённым» значило бы смешать
// «предмета нет» с «предмет прикрыт».
func wfShellHasErrExit(shell string) (errExit bool, applicable bool) {
	s := strings.TrimSpace(shell)
	switch s {
	case "":
		// Умолчание раннера для Linux/macOS — `bash -e {0}`.
		return true, true
	case "bash", "sh":
		// Документированные синонимы: `bash --noprofile --norc -eo pipefail {0}`
		// и `sh -e {0}`.
		return true, true
	}
	if !strings.Contains(s, "{0}") {
		// Именованная оболочка, не bash: python, pwsh, powershell, cmd.
		return false, false
	}
	// Собственный шаблон. `-e` в нём — единственный авторитет: `bash {0}` его НЕ
	// несёт, и это законный способ снять режим на весь шаг.
	head := s
	if i := strings.Index(s, "{0}"); i >= 0 {
		head = s[:i]
	}
	if !strings.HasPrefix(head, "bash") && !strings.HasPrefix(head, "sh ") {
		return false, false
	}
	for _, f := range strings.Fields(head) {
		if f == "-e" || (strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") &&
			strings.ContainsRune(f, 'e')) {
			return true, true
		}
	}
	return false, true
}

// stripShellComment убирает комментарий оболочки, не трогая `#` внутри кавычек
// и внутри слова (`${x#y}`, `a#b` комментарием не являются).
func stripShellComment(line string) string {
	var (
		inSingle, inDouble bool
		prevBlank          = true // начало строки считается границей слова
	)
	for i, r := range line {
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			}
		case inDouble:
			if r == '"' {
				inDouble = false
			}
		case r == '\'':
			inSingle = true
		case r == '"':
			inDouble = true
		case r == '#' && prevBlank:
			return line[:i]
		}
		prevBlank = r == ' ' || r == '\t'
	}
	return line
}

// exitCodeReadIndex — позиция чтения `$?`, НЕ находящегося в одинарных кавычках.
//
// `'… $? …'` — литерал: оболочка его не подставляет, и это законная форма
// написать про `$?` в тексте (сообщение, подсказка, объяснение). `"$?"` —
// подстановка, то есть настоящее чтение, и оно судится наравне с голым.
func exitCodeReadIndex(line string) int {
	var inSingle, inDouble bool
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		switch {
		case c == '\\':
			i++ // экранированный знак кодом не является
		case c == '\'' && !inDouble:
			// Внутри двойных кавычек апостроф — обычный знак, а не начало
			// литерала: без этой оговорки `"don't"` открывал бы литерал до конца
			// строки и глушил бы всё, что за ним.
			inSingle = true
		case c == '"':
			inDouble = !inDouble
		case c == '$' && i+1 < len(line) && line[i+1] == '?':
			// Двойные кавычки чтение НЕ отменяют: `echo "код $?"` подставляет
			// код возврата ровно так же, как голое `$?`.
			return i
		}
	}
	return -1
}

// heredocTerminator — слово-ограничитель, если строка открывает heredoc.
//
// Тело heredoc — ТЕКСТ, а не код: `rc=$?` внутри него оболочка не исполняет.
// Не зная этой формы, гейт краснел бы на подсказке, напечатанной шагом, — то
// есть на объяснении самого себя.
func heredocTerminator(line string) string {
	i := strings.Index(line, "<<")
	if i < 0 || strings.HasPrefix(line[i:], "<<<") {
		return ""
	}
	rest := strings.TrimLeft(line[i+2:], "-")
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return ""
	}
	end := len(rest)
	for j, r := range rest {
		if r == ' ' || r == '\t' || r == ';' || r == '|' || r == '&' || r == '>' {
			end = j
			break
		}
	}
	word := strings.Trim(rest[:end], "'\"")
	if word == "" {
		return ""
	}
	for _, r := range word {
		if !(r == '_' || r == '-' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9')) {
			return ""
		}
	}
	return word
}

// scanRunBodyForExitCodeReads — чтения `$?` в теле одного блока `run:`.
func scanRunBodyForExitCodeReads(file, job, step, body string) []exitCodeRead {
	var out []exitCodeRead
	errExitOn := true
	heredoc := ""

	raw := strings.Split(body, "\n")
	// Логическая строка — с учётом продолжений; помним её первый номер.
	for i := 0; i < len(raw); i++ {
		if heredoc != "" {
			if strings.TrimSpace(raw[i]) == heredoc {
				heredoc = ""
			}
			continue
		}

		start := i + 1
		text := stripShellComment(raw[i])
		for {
			t := strings.TrimRight(text, " \t")
			cont := strings.HasSuffix(t, "\\") || strings.HasSuffix(t, "|") ||
				strings.HasSuffix(t, "&&") || strings.HasSuffix(t, "||")
			if !cont || i+1 >= len(raw) {
				text = t
				break
			}
			i++
			text = strings.TrimSuffix(t, "\\") + " " + strings.TrimSpace(stripShellComment(raw[i]))
		}

		if term := heredocTerminator(text); term != "" {
			heredoc = term
		}

		// Область `set +e` / `set -e`. Читается разобранная строка, а не текст:
		// упоминание режима в комментарии уже отброшено выше.
		for _, stmt := range strings.Split(text, ";") {
			f := strings.Fields(stmt)
			if len(f) >= 2 && f[0] == "set" {
				for _, a := range f[1:] {
					if strings.HasPrefix(a, "+") && strings.ContainsRune(a, 'e') {
						errExitOn = false
					}
					if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") &&
						strings.ContainsRune(a, 'e') {
						errExitOn = true
					}
				}
			}
		}

		idx := exitCodeReadIndex(text)
		if idx < 0 {
			continue
		}
		prefix := text[:idx]

		r := exitCodeRead{File: file, Job: job, Step: step, Line: start,
			Text: strings.TrimSpace(text)}
		switch {
		case !errExitOn:
			r.Guard, r.Guarded = "set +e", true
		case strings.Contains(prefix, "&&") && strings.Contains(prefix, "||"):
			r.Guard, r.Guarded = "&& … ||", true
		case strings.Contains(prefix, "||"):
			r.Guard, r.Guarded = "||", true
		default:
			// Читатель стоит после `;` либо на своей строке — команда,
			// чей код он берёт, до него не доживает.
			r.Guard, r.Guarded = "", false
		}
		out = append(out, r)
	}
	return out
}

// wfRunBlock — один блок `run:` с уже вычисленной оболочкой.
type wfRunBlock struct {
	File, Job, Step, Body, Shell string
}

// collectRunBlocks разбирает объявление и отдаёт его блоки `run:`.
//
// Форм объявления шагов в этом дереве ДВЕ, и знать надо обе: работы конвейера
// (`jobs.<id>.steps`) и составное действие (`runs.steps`). Составных действий
// сегодня ноль — и это ровно та причина, по которой форму надо покрыть сейчас:
// появись она позже, всё записанное в ней оказалось бы ВНЕ НАБЛЮДЕНИЯ, а
// перепись не отличила бы это от «нарушений нет».
//
// У составного действия `shell:` обязателен у каждого `run:`, поэтому умолчания
// файла и работы к нему не применяются — и не применяются здесь.
func collectRunBlocks(file string, content []byte) ([]wfRunBlock, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	fileShell := ""
	if n := yamlMapValue(yamlMapValue(yamlMapValue(root, "defaults"), "run"), "shell"); n != nil {
		fileShell = n.Value
	}

	var out []wfRunBlock

	// Составное действие: шаги лежат под `runs.steps`, работ у него нет.
	if runs := yamlMapValue(root, "runs"); runs != nil {
		if steps := yamlMapValue(runs, "steps"); steps != nil {
			out = append(out, runBlocksOfSteps(file, "(составное действие)", "", steps)...)
		}
	}

	jobs := yamlMapValue(root, "jobs")
	if jobs == nil {
		return out, nil
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		jobName := jobs.Content[i].Value
		job := jobs.Content[i+1]

		jobShell := fileShell
		if n := yamlMapValue(yamlMapValue(yamlMapValue(job, "defaults"), "run"), "shell"); n != nil {
			jobShell = n.Value
		}
		steps := yamlMapValue(job, "steps")
		if steps == nil {
			continue
		}
		out = append(out, runBlocksOfSteps(file, jobName, jobShell, steps)...)
	}
	return out, nil
}

// runBlocksOfSteps — блоки `run:` одной последовательности шагов.
func runBlocksOfSteps(file, owner, inheritedShell string, steps *yaml.Node) []wfRunBlock {
	var out []wfRunBlock
	for _, st := range steps.Content {
		runNode := yamlMapValue(st, "run")
		if runNode == nil {
			continue
		}
		name := "(без имени)"
		if n := yamlMapValue(st, "name"); n != nil {
			name = n.Value
		}
		shell := inheritedShell
		if n := yamlMapValue(st, "shell"); n != nil {
			shell = n.Value
		}
		out = append(out, wfRunBlock{
			File: file, Job: owner, Step: name,
			Body: runNode.Value, Shell: shell,
		})
	}
	return out
}

// workflowFilesFromIndex — конвейеры, взятые из ИНДЕКСА git, а не с диска.
//
// Диск отдал бы и рабочие копии агентов, и распаковки — вердикт гейта перестал
// бы быть свойством коммита.
func workflowFilesFromIndex(t *testing.T) []string {
	t.Helper()
	tt := newTrackedTree(t, repoRoot(t))
	var out []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".yml") && !strings.HasSuffix(rel, ".yaml") {
			continue
		}
		base := rel[strings.LastIndex(rel, "/")+1:]
		// Обе формы объявления шагов: конвейер и составное действие. Вторых в
		// дереве сегодня ноль, и перепись печатает это числом — иначе «ноль
		// нарушений в составных действиях» было бы неотличимо от «их не искали».
		if strings.Contains(rel, ".github/workflows/") || base == "action.yml" || base == "action.yaml" {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// TestNoWorkflowStepReadsAnExitCodeItCannotReach — ни один шаг под `-e` не берёт
// код возврата после голого вызова.
func TestNoWorkflowStepReadsAnExitCodeItCannotReach(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	files := workflowFilesFromIndex(t)
	if len(files) == 0 {
		t.Fatal("в индексе не найдено ни одного конвейера — обход пуст, и «ноль " +
			"находок» здесь означало бы «ноль прочитанного»")
	}

	var (
		blocks, underErrExit, notApplicable, reads int
		byGuard                                    = map[string]int{}
		findings                                   []exitCodeRead
	)
	for _, rel := range files {
		// readTreeFile сам роняет прогон на непрочитанном и на пустом файле:
		// молча обойти конвейер нельзя, это вернуло бы неразличимость «ноль
		// находок» и «ноль прочитанного».
		content := readTreeFile(t, root, rel)
		rbs, err := collectRunBlocks(rel, []byte(content))
		if err != nil {
			t.Fatalf("конвейер не разобран: %v", err)
		}
		for _, rb := range rbs {
			blocks++
			errExit, applicable := wfShellHasErrExit(rb.Shell)
			if !applicable || !errExit {
				notApplicable++
				continue
			}
			underErrExit++
			for _, r := range scanRunBodyForExitCodeReads(rb.File, rb.Job, rb.Step, rb.Body) {
				reads++
				if r.Guarded {
					byGuard[r.Guard]++
					continue
				}
				findings = append(findings, r)
			}
		}
	}

	if blocks == 0 {
		t.Fatal("конвейеры прочитаны, но блоков `run:` в них ноль — разбор не " +
			"состоялся, и вердикт беспредметен")
	}

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от «ноль
	// прочитанного», и от «прочитано, но не судимо» тоже.
	composites := 0
	for _, rel := range files {
		base := rel[strings.LastIndex(rel, "/")+1:]
		if base == "action.yml" || base == "action.yaml" {
			composites++
		}
	}
	t.Logf("перепись: объявлений %d (конвейеров %d · составных действий %d) · "+
		"блоков `run:` %d · из них под `-e` %d · вне предмета (не bash либо "+
		"шаблон без -e) %d · чтений `$?` %d",
		len(files), len(files)-composites, composites, blocks, underErrExit,
		notApplicable, reads)
	guards := make([]string, 0, len(byGuard))
	for g := range byGuard {
		guards = append(guards, g)
	}
	sort.Strings(guards)
	for _, g := range guards {
		t.Logf("  прикрыто «%s»: %d", g, byGuard[g])
	}

	for _, f := range findings {
		t.Errorf("%s :: работа %s :: шаг «%s», строка тела %d:\n    %s\n"+
			"    код возврата взят после ГОЛОГО вызова. Под `bash -e` оболочка "+
			"обрывается на ненулевом коде НЕМЕДЛЕННО — до присваивания, до развилки, "+
			"до аннотации. Возьми код как данные: `cmd || rc=$?` либо "+
			"`cmd && rc=0 || rc=$?`; развилку вынеси в скрипт, который можно "+
			"прогнать пробой (#2346).",
			f.File, f.Job, f.Step, f.Line, f.Text)
	}
}
