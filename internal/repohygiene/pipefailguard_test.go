// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// pipefailguard_test.go — статус конвейера не теряется ни в одном сборочном
// файле и ни в одном конвейере CI.
//
// # Предмет
//
// `a | b` отдаёт оболочке код возврата ТОЛЬКО последней команды. Поэтому
// `<проверка> | tee log`, `<сборка> | grep`, `helm template … | kubectl apply -f -`
// зеленеют при любом провале левой стороны: правая отработала, значит шаг
// успешен. Отказ при этом оседает в тексте, за которым никто не приходит —
// приходить зовёт красный.
//
// Один экземпляр этого класса здесь уже находили и чинили: ночной фаззер писал
// вывод через `tee`, и находка не могла уронить шаг. Гейт, поставленный тогда
// (`TestFuzzWorkflowDoesNotSwallowExitCode`), читает ОДИН файл
// (`continuous-fuzz.yml`) и ОДНУ форму команды (`go test`, ушедший в конвейер).
// Класс же измеряется по дереву, а не по диффу, в котором его заметили:
// на bb26d905 в дереве 11 сборочных файлов и 9 конвейеров CI, и **ни один** не
// объявлял `pipefail` на уровне файла.
//
// # Что здесь считается защитой
//
// Защита — это то, что действует НА САМ КОНВЕЙЕР, а не рядом с ним:
//
//   - сборочный файл: `SHELL := bash` вместе с `.SHELLFLAGS`, содержащим
//     `pipefail`. Обе половины обязательны: `.SHELLFLAGS := -o pipefail -c`
//     без bash уедет в `/bin/sh` (на Debian — dash), который такой ручки не
//     знает вовсе, и рецепт перестанет исполняться;
//   - конвейер CI: `shell: bash` в `defaults.run` (уровня файла, задания) или
//     у самого шага. Это документированный синоним
//     `bash --noprofile --norc -eo pipefail {0}`; умолчание же —
//     `bash -e {0}`, то есть БЕЗ pipefail;
//   - либо сам фрагмент (логическая строка рецепта / тело `run:`) объявляет
//     `pipefail` или читает `PIPESTATUS`.
//
// Файл без единого конвейера находкой НЕ является: требовать объявление там,
// где терять нечего, — ритуал, а не проверка. Гейт вооружается сам в момент,
// когда конвейер появляется.
//
// # Читается исполняемая часть, а не текст
//
// Слово `pipefail` встречается в комментариях, ОБЪЯСНЯЮЩИХ защиту, — в этом
// файле в том числе. Гейт, ищущий слово в сыром тексте, остаётся зелёным при
// СНЯТОЙ защите, читая собственное объяснение. Ровно на этом дефекте уже
// краснела первая редакция соседнего гейта. Поэтому комментарии отбрасываются,
// содержимое кавычек затирается, а `defaults.run.shell` читается из РАЗОБРАННОГО
// документа, где комментария не существует как узла.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько файлов прочитал и сколько конвейеров в них нашёл, и пустой обход —
// провал.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// pipeFinding — один фрагмент, теряющий статус конвейера.
type pipeFinding struct {
	File string
	Line int // строка САМОГО конвейера
	Code string
}

// ─────────────────────────────────────────────────────────────────────────────
// ОБЩЕЕ: чтение исполняемой части строки оболочки.
// ─────────────────────────────────────────────────────────────────────────────

// shellExecutable возвращает две проекции строки: с СОДЕРЖИМЫМ литералов и с
// литералами, свёрнутыми в один знак `Q`. Комментарий отбрасывается в обеих, но
// решётка внутри литерала комментария не начинает.
//
// Вторая проекция нужна затем, чтобы `|` внутри программы jq/awk или внутри
// сообщения не считался конвейером; первая — чтобы объявление защиты,
// записанное в кавычках, всё же засчитывалось.
//
// Литерал сворачивается в ОДИН знак, а не затирается пробелами: длина внутри
// кавычек не должна влиять на разбор. Затирание пробелами уже подводило —
// образец ветви `case` вида `""|"<no value>")` превращался в строку С
// ПРОБЕЛАМИ и переставал узнаваться как образец.
func shellExecutable(line string) (kept, masked string) {
	var k, m strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote != 0 {
			k.WriteByte(ch)
			if ch == quote && (i == 0 || line[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			k.WriteByte(ch)
			m.WriteByte('Q')
			continue
		}
		if ch == '#' {
			// Решётка начинает комментарий только на границе слова: `$#`,
			// `${x#y}` и `a#b` комментарием не являются.
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				break
			}
		}
		k.WriteByte(ch)
		m.WriteByte(ch)
	}
	return k.String(), m.String()
}

// hasPipeline — есть ли в строке КОНВЕЙЕР.
//
// Отбрасываются: `||` (это не конвейер), ведущий `|` (продолжение), и ветви
// `case`, где `|` разделяет ОБРАЗЦЫ (`""|"<no value>") …`), а не команды.
// Последнее — не гипотетическая осторожность: ровно такая строка есть в
// deploy/Makefile, и без этого различения гейт указывал бы на неё.
//
// Состояние `case` по строкам НЕ отслеживается: образец узнаётся по СВОЕЙ
// форме — набор шаблонов без пробелов, затем закрывающая скобка без парной
// открывающей. Отслеживание состояния сломалось бы на однострочном
// `case … in …) … ;; esac`, который в дереве есть.
func hasPipeline(line string) bool {
	_, masked := shellExecutable(line)
	s := stripCaseArmPrefix(masked)
	s = strings.ReplaceAll(s, "||", "  ")
	if !strings.Contains(s, "|") {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(s), "|")
}

// caseArmPattern — образцы ветви `case`: слова/шаблоны, разделённые `|`, без
// пробелов и без операторов оболочки.
var caseArmPattern = regexp.MustCompile(`^\s*\(?[A-Za-z0-9_Q*?.\[\]{}$@:=/,+-]*(\|[A-Za-z0-9_Q*?.\[\]{}$@:=/,+-]*)*\s*$`)

// stripCaseArmPrefix отрезает у строки часть, являющуюся ОБРАЗЦАМИ ветви `case`.
//
// Признак — первая закрывающая скобка на нулевой глубине, перед которой стоят
// только шаблоны. `f() { … }` и `$(…)` под него не подпадают: там скобка парная.
func stripCaseArmPrefix(masked string) string {
	depth := 0
	for i := 0; i < len(masked); i++ {
		switch masked[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
				continue
			}
			if caseArmPattern.MatchString(masked[:i]) {
				return masked[i+1:]
			}
			return masked
		}
	}
	return masked
}

// declaresPipefail — фрагмент сам объявляет защиту. Читается ИСПОЛНЯЕМАЯ часть.
func declaresPipefail(fragment string) bool {
	for _, raw := range strings.Split(fragment, "\n") {
		kept, _ := shellExecutable(raw)
		if strings.Contains(kept, "pipefail") || strings.Contains(kept, "PIPESTATUS") {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// СБОРОЧНЫЕ ФАЙЛЫ.
// ─────────────────────────────────────────────────────────────────────────────

var (
	reShellAssign     = regexp.MustCompile(`(?m)^SHELL\s*[:?+]?=\s*(\S+)`)
	reShellFlagsPipe  = regexp.MustCompile(`(?m)^\.SHELLFLAGS\s*[:?+]?=.*pipefail`)
	reMakeRecipeStart = regexp.MustCompile(`^\t`)
)

// makefileProtected — файл объявил защиту на своём уровне.
//
// Требуются ОБЕ половины. `.SHELLFLAGS` без bash — не защита, а поломка:
// умолчание make — `/bin/sh`, и `-o pipefail` он не понимает.
func makefileProtected(body string) (ok bool, why string) {
	flags := reShellFlagsPipe.MatchString(stripMakeComments(body))
	m := reShellAssign.FindStringSubmatch(stripMakeComments(body))
	shellIsBash := m != nil && strings.HasSuffix(filepath.Base(strings.TrimSpace(m[1])), "bash")
	switch {
	case flags && shellIsBash:
		return true, ""
	case flags && !shellIsBash:
		return false, "`.SHELLFLAGS` несёт pipefail, но `SHELL` не bash — умолчание make (`/bin/sh`) такой ручки не знает"
	default:
		return false, "нет `.SHELLFLAGS` с pipefail на уровне файла"
	}
}

// stripMakeComments убирает строки-комментарии make (решётка в первой позиции
// после отступа), чтобы объявление, ЗАКОММЕНТИРОВАННОЕ, не считалось за
// действующее.
func stripMakeComments(body string) string {
	var out []string
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimLeft(l, " \t")
		if strings.HasPrefix(t, "#") {
			out = append(out, "")
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// makefilePipelines — конвейеры в рецептах, СГРУППИРОВАННЫЕ в логические строки.
//
// Возвращает ДВА множества: все найденные конвейеры (перепись) и те, чей рецепт
// сам защиты не объявляет (кандидаты в находки, если её нет и у файла).
// Разделение не косметическое: перепись обязана считать ПРОЧИТАННОЕ, иначе
// строка отчёта называет числом не то, что измерила.
//
// Рецепт склеивается по обратному слэшу: `set -o pipefail; \` в начале рецепта
// защищает весь рецепт, а не только первую физическую строку. Без склейки гейт
// указывал бы на собственную защищённую цель корневого Makefile.
func makefilePipelines(body string) (all, unprotected []pipeFinding) {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) {
		if !reMakeRecipeStart.MatchString(lines[i]) {
			i++
			continue
		}
		start := i
		var logical []string
		for i < len(lines) && reMakeRecipeStart.MatchString(lines[i]) {
			logical = append(logical, lines[i][1:])
			if !strings.HasSuffix(strings.TrimRight(lines[i], " \t"), "\\") {
				i++
				break
			}
			i++
		}
		self := declaresPipefail(strings.Join(logical, "\n"))
		for k, l := range logical {
			if !hasPipeline(l) {
				continue
			}
			f := pipeFinding{Line: start + k + 1, Code: strings.TrimSpace(l)}
			all = append(all, f)
			if !self {
				unprotected = append(unprotected, f)
			}
		}
	}
	return all, unprotected
}

// ─────────────────────────────────────────────────────────────────────────────
// КОНВЕЙЕРЫ CI.
// ─────────────────────────────────────────────────────────────────────────────

// shellGivesPipefail — значение ключа `shell:`, дающее pipefail.
//
// `bash` — документированный синоним `bash --noprofile --norc -eo pipefail {0}`.
// Произвольная строка засчитывается, только если сама называет pipefail.
func shellGivesPipefail(v string) bool {
	v = strings.TrimSpace(v)
	return v == "bash" || strings.Contains(v, "pipefail")
}

// workflowPipelines — шаги, где конвейер теряет статус.
//
// Документ РАЗБИРАЕТСЯ, а не читается построчно: `defaults.run.shell` —
// структурный факт, и в разобранном документе комментария не существует как
// узла, поэтому «объяснение защиты» защитой стать не может by construction.
func workflowPipelines(body string) (all, unprotected []pipeFinding, steps int, err error) {
	var doc yaml.Node
	if uerr := yaml.Unmarshal([]byte(body), &doc); uerr != nil {
		return nil, nil, 0, uerr
	}
	if len(doc.Content) == 0 {
		return nil, nil, 0, nil
	}
	root := doc.Content[0]

	fileShell := defaultsShell(mapValue(root, "defaults"))

	jobs := mapValue(root, "jobs")
	if jobs == nil {
		return nil, nil, 0, nil
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		job := jobs.Content[i+1]
		jobShell := defaultsShell(mapValue(job, "defaults"))
		stepsNode := mapValue(job, "steps")
		if stepsNode == nil {
			continue
		}
		for _, st := range stepsNode.Content {
			run := mapValue(st, "run")
			if run == nil || run.Kind != yaml.ScalarNode {
				continue
			}
			steps++
			effective := fileShell
			if jobShell != "" {
				effective = jobShell
			}
			if s := mapValue(st, "shell"); s != nil && s.Kind == yaml.ScalarNode {
				effective = s.Value
			}
			protected := shellGivesPipefail(effective) || declaresPipefail(run.Value)
			// Координата обязана указывать на САМ конвейер, иначе отчёт не
			// actionable. У блочного скаляра (`run: |`) узел сообщает строку
			// самого ключа, поэтому содержимое начинается со следующей;
			// у однострочного `run: cmd` — ту же строку. Свёрнутый скаляр
			// (`run: >`) склеивает строки при разборе, поэтому построчной
			// координаты у него нет и находка адресуется шагом.
			base := run.Line
			perLine := true
			switch run.Style {
			case yaml.LiteralStyle:
				base++
			case yaml.FoldedStyle:
				perLine = false
			}
			for k, l := range strings.Split(run.Value, "\n") {
				if !hasPipeline(l) {
					continue
				}
				line := base
				if perLine {
					line = base + k
				}
				f := pipeFinding{Line: line, Code: strings.TrimSpace(l)}
				all = append(all, f)
				if !protected {
					unprotected = append(unprotected, f)
				}
			}
		}
	}
	return all, unprotected, steps, nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func defaultsShell(defaults *yaml.Node) string {
	run := mapValue(defaults, "run")
	sh := mapValue(run, "shell")
	if sh == nil || sh.Kind != yaml.ScalarNode {
		return ""
	}
	return sh.Value
}

// ─────────────────────────────────────────────────────────────────────────────
// ОСНОВНОЙ ПРОХОД.
// ─────────────────────────────────────────────────────────────────────────────

// TestNoPipelineLosesItsStatus — по всему дереву, а не по одному файлу.
func TestNoPipelineLosesItsStatus(t *testing.T) {
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	var makefiles, workflows []string
	for rel := range tree.files {
		base := filepath.Base(rel)
		switch {
		case base == "Makefile" || strings.HasSuffix(base, ".mk"):
			makefiles = append(makefiles, rel)
		case strings.HasPrefix(rel, ".github/workflows/") &&
			(strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")):
			workflows = append(workflows, rel)
		}
	}
	sort.Strings(makefiles)
	sort.Strings(workflows)

	if len(makefiles) == 0 || len(workflows) == 0 {
		t.Fatalf("прочитано сборочных файлов %d, конвейеров CI %d — «ноль находок» здесь "+
			"означало бы «ноль прочитанного». Почини обход.", len(makefiles), len(workflows))
	}

	seen, lost, lostFiles := 0, 0, 0

	for _, rel := range makefiles {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		all, unprotected := makefilePipelines(string(body))
		seen += len(all)
		if len(unprotected) == 0 {
			continue
		}
		ok, why := makefileProtected(string(body))
		if ok {
			continue
		}
		lost += len(unprotected)
		lostFiles++
		t.Errorf("%s — %s. В рецептах %d конвейер(ов) без защиты, и статус каждого берётся "+
			"от ПОСЛЕДНЕЙ команды: провал левой стороны рецепт не роняет.\n%s\n"+
			"Починка — объявить один раз файлом:\n"+
			"    SHELL := bash\n    .SHELLFLAGS := -o pipefail -c",
			rel, why, len(unprotected), renderPipeFindings(rel, unprotected))
	}

	for _, rel := range workflows {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		all, unprotected, steps, perr := workflowPipelines(string(body))
		if perr != nil {
			t.Errorf("%s: документ не разбирается (%v) — гейт не может утверждать о нём ничего", rel, perr)
			continue
		}
		if steps == 0 {
			continue
		}
		seen += len(all)
		if len(unprotected) == 0 {
			continue
		}
		lost += len(unprotected)
		lostFiles++
		t.Errorf("%s — ни `defaults.run.shell`, ни сам шаг не дают pipefail. %d конвейер(ов) "+
			"теряют статус левой стороны (умолчание GitHub — `bash -e {0}`, БЕЗ pipefail).\n%s\n"+
			"Починка — объявить один раз файлом:\n"+
			"    defaults:\n      run:\n        shell: bash",
			rel, len(unprotected), renderPipeFindings(rel, unprotected))
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Запрет обоснован тем, что конвейеры в
	// этих файлах ЕСТЬ. Если разбор перестал их видеть (сменился формат, съехал
	// предикат), «ноль находок» будет означать «ноль прочитанного» — и это
	// находка, а не чистота.
	if seen == 0 {
		t.Fatalf("во всём дереве не найдено НИ ОДНОГО конвейера (%d сборочных файлов, %d конвейеров CI). "+
			"Либо дерево перестало ими пользоваться, либо разбор перестал их видеть; "+
			"и то и другое требует разбирательства, а не зелёного.", len(makefiles), len(workflows))
	}

	t.Logf("перепись: прочитано сборочных файлов %d, конвейеров CI %d; конвейеров в них найдено %d, "+
		"из них теряют статус %d (в %d файле(ах))",
		len(makefiles), len(workflows), seen, lost, lostFiles)
}

func renderPipeFindings(rel string, hits []pipeFinding) string {
	var b strings.Builder
	for _, h := range hits {
		b.WriteString("    " + rel + ":" + strconv.Itoa(h.Line) + ": " + h.Code + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// КОНТРОЛЬ В ОБЕ СТОРОНЫ.
//
// Гейт обязан покраснеть на внесённом дефекте И промолчать на ЗАКОННОЙ
// конструкции той же формы. Без второй половины он ловит форму, а не существо,
// и первый же ложный срабат его снимает.
// ─────────────────────────────────────────────────────────────────────────────

func TestMakefilePredicateCutsBothWays(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int // ожидаемое число находок
	}{
		{
			name: "ДЕФЕКТ: конвейер в рецепте, объявления нет",
			body: "SHELL := bash\n\nt:\n\thelm template x | kubectl apply -f -\n",
			want: 1,
		},
		{
			name: "ЗАКОННО: объявление файлом",
			body: "SHELL := bash\n.SHELLFLAGS := -o pipefail -c\n\nt:\n\thelm template x | kubectl apply -f -\n",
			want: 0,
		},
		{
			name: "ЗАКОННО: рецепт объявляет защиту сам",
			body: "SHELL := bash\n\nt:\n\t@set -o pipefail; \\\n\thelm template x | kubectl apply -f -\n",
			want: 0,
		},
		{
			name: "ДЕФЕКТ: объявление ЗАКОММЕНТИРОВАНО — читается код, а не проза",
			body: "SHELL := bash\n# .SHELLFLAGS := -o pipefail -c\n\nt:\n\thelm template x | kubectl apply -f -\n",
			want: 1,
		},
		{
			name: "ДЕФЕКТ: .SHELLFLAGS есть, но оболочка не bash — ручки такой нет",
			body: ".SHELLFLAGS := -o pipefail -c\n\nt:\n\thelm template x | kubectl apply -f -\n",
			want: 1,
		},
		{
			name: "ЗАКОННО: конвейера нет — терять нечего",
			body: "SHELL := bash\n\nt:\n\tgo build ./...\n",
			want: 0,
		},
		{
			name: "ЗАКОННО: `||` конвейером не является",
			body: "SHELL := bash\n\nt:\n\tkind get clusters || ./create.sh\n",
			want: 0,
		},
		{
			// Форма взята с натуры: deploy/Makefile, разбор метки образа.
			name: "ЗАКОННО: `|` разделяет ОБРАЗЦЫ ветви case, а не команды",
			body: "SHELL := bash\n\nt:\n\t@case \"$$rev\" in \\\n\t  \"\"|\"<no value>\") rev=\"(нет метки)\" ;; \\\n\tesac\n",
			want: 0,
		},
		{
			name: "ДЕФЕКТ: в ветви case стоит НАСТОЯЩИЙ конвейер — образец его не прикрывает",
			body: "SHELL := bash\n\nt:\n\t@case \"$$x\" in \\\n\t  a|b) helm template x | kubectl apply -f - ;; \\\n\tesac\n",
			want: 1,
		},
		{
			name: "ЗАКОННО: `|` внутри литерала (программа jq) конвейером не является",
			body: "SHELL := bash\n\nt:\n\tjq -r '.a | .b' f.json\n",
			want: 0,
		},
		{
			name: "ДЕФЕКТ: объявление есть у СОСЕДНЕГО рецепта, не у этого",
			body: "SHELL := bash\n\na:\n\t@set -o pipefail; echo x | cat\n\nb:\n\techo y | cat\n",
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, hits := makefilePipelines(tc.body)
			if ok, _ := makefileProtected(tc.body); ok {
				hits = nil
			}
			if len(hits) != tc.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(hits), tc.want, hits)
			}
		})
	}
}

func TestWorkflowPredicateCutsBothWays(t *testing.T) {
	const step = "jobs:\n  j:\n    steps:\n      - run: |\n          go test ./... | tee out.log\n"
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "ДЕФЕКТ: конвейер, оболочка по умолчанию (bash -e, без pipefail)",
			body: step,
			want: 1,
		},
		{
			name: "ЗАКОННО: defaults.run.shell уровня файла",
			body: "defaults:\n  run:\n    shell: bash\n" + step,
			want: 0,
		},
		{
			name: "ЗАКОННО: defaults.run.shell уровня задания",
			body: "jobs:\n  j:\n    defaults:\n      run:\n        shell: bash\n    steps:\n      - run: |\n          go test ./... | tee out.log\n",
			want: 0,
		},
		{
			name: "ЗАКОННО: shell у самого шага",
			body: "jobs:\n  j:\n    steps:\n      - run: |\n          go test ./... | tee out.log\n        shell: bash\n",
			want: 0,
		},
		{
			name: "ЗАКОННО: шаг объявляет защиту сам",
			body: "jobs:\n  j:\n    steps:\n      - run: |\n          set -o pipefail\n          go test ./... | tee out.log\n",
			want: 0,
		},
		{
			name: "ДЕФЕКТ: объявление в КОММЕНТАРИИ тела шага",
			body: "jobs:\n  j:\n    steps:\n      - run: |\n          # без pipefail статус берётся от tee\n          go test ./... | tee out.log\n",
			want: 1,
		},
		{
			name: "ДЕФЕКТ: `shell: bash` объявлен у ДРУГОГО шага",
			body: "jobs:\n  j:\n    steps:\n      - run: echo a\n        shell: bash\n      - run: |\n          go test ./... | tee out.log\n",
			want: 1,
		},
		{
			name: "ДЕФЕКТ: `shell: bash` записан КОММЕНТАРИЕМ рядом с defaults",
			body: "defaults:\n  run:\n    # shell: bash\n    working-directory: x\n" + step,
			want: 1,
		},
		{
			name: "ЗАКОННО: `shell: sh` — но он pipefail не даёт, значит находка",
			body: "defaults:\n  run:\n    shell: sh\n" + step,
			want: 1,
		},
		{
			name: "ЗАКОННО: конвейера в шаге нет",
			body: "jobs:\n  j:\n    steps:\n      - run: |\n          go build ./...\n",
			want: 0,
		},
		{
			name: "ЗАКОННО: `||` конвейером не является",
			body: "jobs:\n  j:\n    steps:\n      - run: |\n          kind delete cluster || true\n",
			want: 0,
		},
		{
			name: "ЗАКОННО: шаг без run (uses) не рассматривается",
			body: "jobs:\n  j:\n    steps:\n      - uses: actions/checkout@v4\n",
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, hits, _, err := workflowPipelines(tc.body)
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if len(hits) != tc.want {
				t.Fatalf("находок %d, ожидалось %d: %v", len(hits), tc.want, hits)
			}
		})
	}
}

// TestWorkflowLineNumbersPointAtThePipeline — координата находки обязана
// указывать на сам конвейер, иначе отчёт не actionable.
//
// Проверяются обе формы записи команды: блочный скаляр (`run: |`, у которого
// узел сообщает строку КЛЮЧА) и однострочная (`run: cmd`, у которой та же
// строка и есть команда). Одна формула для обеих даёт промах на единицу —
// первая редакция указывала на строку выше.
func TestWorkflowLineNumbersPointAtThePipeline(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "блочный скаляр",
			body: "jobs:\n  j:\n    steps:\n      - name: x\n        run: |\n          echo a\n          go test ./... | tee out.log\n",
		},
		{
			name: "блочный скаляр с обрезкой хвоста",
			body: "jobs:\n  j:\n    steps:\n      - name: x\n        run: |-\n          echo a\n          go test ./... | tee out.log\n",
		},
		{
			name: "однострочная команда",
			body: "jobs:\n  j:\n    steps:\n      - name: x\n        run: go test ./... | tee out.log\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, hits, _, err := workflowPipelines(tc.body)
			if err != nil {
				t.Fatalf("разбор: %v", err)
			}
			if len(hits) != 1 {
				t.Fatalf("находок %d, ожидалась 1: %v", len(hits), hits)
			}
			lines := strings.Split(tc.body, "\n")
			if hits[0].Line < 1 || hits[0].Line > len(lines) {
				t.Fatalf("координата %d вне файла (%d строк)", hits[0].Line, len(lines))
			}
			if got := lines[hits[0].Line-1]; !strings.Contains(got, "go test") {
				t.Fatalf("координата %d указывает на %q, а конвейер на другой строке", hits[0].Line, got)
			}
		})
	}
}
