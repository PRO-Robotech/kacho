// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notifywiring_test.go — NTF1-D08 по дереву kacho: единый рабочий процесс
// `.github/workflows/ci.yaml` исполняет `make notifications-check BASE=HEAD^1`
// и `make notify-tree-gates` на запросе и на `push`; снятый шаг — красный
// (замысел #2915 З31).
//
// ТОНКИЙ ВЫЗЫВАЮЩИЙ. Ведомость обязательных вызовов, разбор YAML рабочих
// процессов, разбор тел `run:` словами оболочки и перечень запретов живут в
// одной функции corelib — `treehygiene.AuditNotifyWiring`. Здесь только корень
// дерева kacho и инъекции прогоном по нему: копия ведомости или правил в этом
// дереве разошлась бы с corelib молча (УК50, CX1-49 (а)).
//
// Гейт НЕ пропускается при -short: шаги `go test` конвейера идут с -short, и
// пропуск снял бы единственного исполнителя провязки молча.
//
// ТЕЛА ЦЕЛЕЙ D08 НЕ СУДИТ: о цели он спрашивает лишь «`make -n` выходит нулём
// и печатает непустое», и тело `@echo ok` ему отвечает. Тела держат пробы
// рецептов ниже: `make -n` печатает ровно один вызов генератора с базой и
// ровно один `go test` с перечнем гейтов, а цель с заведомо неверным входом
// выходит ненулевым кодом.
package repohygiene_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/corelib/treehygiene"
)

// notifyWorkflowsDir — каталог рабочих процессов дерева от его корня.
const notifyWorkflowsDir = ".github/workflows"

// notifyCIWorkflow — единый рабочий процесс дерева, в котором стоят шаги D08.
const notifyCIWorkflow = "ci.yaml"

// notifyTreeGatesFile — файл тонких вызывающих гейтов дерева NTF-1; его пробы
// верхнего уровня и перечень цели `notify-tree-gates` — одно множество.
const notifyTreeGatesFile = "notifytreegates_test.go"

// notifyWiring — исход гейта D08 по каталогу рабочих процессов dir и Makefile
// makefile. Отказ исполнения — «проверка НЕ ИСПОЛНЯЛАСЬ», а не находка.
func notifyWiring(t *testing.T, dir, makefile string) treehygiene.WiringReport {
	t.Helper()
	r, err := treehygiene.AuditNotifyWiring(dir, makefile)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: гейт вызова NTF-1 не исполнился: %v", err)
	}
	t.Log(r.String())
	return r
}

// TestNTF1D08_KachoCIRunsTheNotifyChecks — D08 по дереву kacho: по каждой
// записи ведомости ровно один шаг в едином рабочем процессе, находок ноль.
func TestNTF1D08_KachoCIRunsTheNotifyChecks(t *testing.T) {
	t.Parallel()
	root := repoRootFor(t)
	r := notifyWiring(t, filepath.Join(root, filepath.FromSlash(notifyWorkflowsDir)), filepath.Join(root, "Makefile"))
	logCorelibPin(t, root)
	if r.Workflows == 0 || r.Steps == 0 {
		t.Fatalf("пустой обход — не вердикт: рабочих процессов %d, шагов %d", r.Workflows, r.Steps)
	}
	for _, f := range r.Findings {
		t.Errorf("NTF1-D08 · %s", f)
	}
	for _, rec := range treehygiene.NotifyWiringLedger() {
		if r.Found[rec] != 1 {
			t.Errorf("NTF1-D08 · «%s»: шагов %d, ожидается ровно один — второй шаг той же записи "+
				"исполнял бы проверку дважды и снимался бы порознь", rec, r.Found[rec])
		}
	}
}

// notifyInjection — одна инъекция D08: правка копии рабочих процессов (по
// разбору YAML) либо Makefile, и чем гейт обязан ответить.
type notifyInjection struct {
	name string
	// workflow правит разобранный ci.yaml и возвращает число правок: ноль —
	// инъекция не о том, и проба не исполнялась.
	workflow func(doc *yaml.Node) int
	// makefile правит текст Makefile; ноль правок — проба не исполнялась.
	makefile func(src string) (string, int)
	// why — подстрока находки, общая для КАЖДОЙ находки инъекции: чужая
	// находка значила бы, что инъекция уронила не только свой предмет.
	why []string
	// at — файл, который обязана назвать хотя бы одна находка.
	at string
}

// TestNTF1D08_InjectionsInKachoAreFound — D08 инъекции (1)–(8) прогоном по
// дереву kacho: копия его рабочих процессов и Makefile, в которой изменён
// ровно один факт. Близнец — та же копия без правки: гейт молчит и находит по
// одному шагу на запись. Копии близнеца и инъекции идут одним путём
// «разбор → Marshal» и различаются только правкой.
func TestNTF1D08_InjectionsInKachoAreFound(t *testing.T) {
	t.Parallel()
	root := repoRootFor(t)
	ledger := treehygiene.NotifyWiringLedger()
	if len(ledger) != 2 || !strings.Contains(ledger[0], "notifications-check") || !strings.Contains(ledger[1], "notify-tree-gates") {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: ведомость corelib %q — не те две записи, о которых инъекции", ledger)
	}
	checkRec, gatesRec := ledger[0], ledger[1]

	t.Run("близнец — копия без правки", func(t *testing.T) {
		dir, mk := notifyInjectedCopy(t, root, notifyInjection{})
		r := notifyWiring(t, dir, mk)
		for _, f := range r.Findings {
			t.Errorf("близнец обязан молчать: %s", f)
		}
		for _, rec := range ledger {
			if r.Found[rec] != 1 {
				t.Errorf("близнец: «%s» шагов %d, ожидается 1", rec, r.Found[rec])
			}
		}
	})

	cases := []notifyInjection{
		{
			name:     "(1) шаг notifications-check снят — задания нет",
			workflow: func(doc *yaml.Node) int { return dropNotifyStep(doc, checkRec) },
			why:      []string{"«" + checkRec + "»: задания нет"},
			at:       notifyCIWorkflow,
		},
		{
			name: "(2) BASE=origin/main — база не первый родитель",
			workflow: func(doc *yaml.Node) int {
				return editNotifyStep(doc, checkRec, func(step *yaml.Node) {
					yamlNodeGet(step, "run").Value = "make notifications-check BASE=origin/main"
				})
			},
			// Шаг с другой базой — не шаг записи: «задания нет» по той же
			// записи — следствие той же правки, а не чужая находка.
			why: []string{"«" + checkRec + "»: база не первый родитель", "«" + checkRec + "»: задания нет"},
			at:  notifyCIWorkflow,
		},
		{
			name: "(3) continue-on-error: true на шаге",
			workflow: func(doc *yaml.Node) int {
				return editNotifyStep(doc, checkRec, func(step *yaml.Node) { yamlNodeSet(step, "continue-on-error", "true") })
			},
			why: []string{"«" + checkRec + "»: у шага continue-on-error: true"},
			at:  notifyCIWorkflow,
		},
		{
			name: "(4) if: на шаге",
			workflow: func(doc *yaml.Node) int {
				return editNotifyStep(doc, checkRec, func(step *yaml.Node) {
					yamlNodeSet(step, "if", "github.event_name == 'pull_request'")
				})
			},
			why: []string{"«" + checkRec + "»: у шага if: github.event_name == 'pull_request'"},
			at:  notifyCIWorkflow,
		},
		{
			name:     "(5) fetch-depth: 1 у checkout задания",
			workflow: func(doc *yaml.Node) int { return shallowNotifyJobCheckout(doc, checkRec) },
			// Обе записи могут стоять в одном задании — тогда находка по
			// каждой; вид находки один.
			why: []string{"fetch-depth ≠ 0 (1)"},
			at:  notifyCIWorkflow,
		},
		{
			name:     "(6) триггер push снят с рабочего процесса",
			workflow: dropNotifyPushTrigger,
			why:      []string{"у рабочего процесса нет триггера push"},
			at:       notifyCIWorkflow,
		},
		{
			name:     "(7) шаг notify-tree-gates снят — задания нет",
			workflow: func(doc *yaml.Node) int { return dropNotifyStep(doc, gatesRec) },
			why:      []string{"«" + gatesRec + "»: задания нет"},
			at:       notifyCIWorkflow,
		},
		{
			name: "(8) цель notifications-check снята из Makefile при шаге на месте — цели нет",
			makefile: func(src string) (string, int) {
				// Переименование заголовка правила — один факт: объявление
				// .PHONY остаётся, правила нет, `make -n` выходит нулём и не
				// исполняет ничего.
				const head = "\nnotifications-check:"
				n := strings.Count(src, head)
				return strings.Replace(src, head, "\nnotifications-check-removed-by-injection:", 1), n
			},
			why: []string{"«" + checkRec + "»: цели нет"},
			at:  "Makefile",
		},
	}
	for _, inj := range cases {
		t.Run(inj.name, func(t *testing.T) {
			dir, mk := notifyInjectedCopy(t, root, inj)
			r := notifyWiring(t, dir, mk)
			if len(r.Findings) == 0 {
				t.Fatalf("инъекция не найдена: находок 0")
			}
			named := false
			for _, f := range r.Findings {
				t.Logf("находка: %s · %s", filepath.Base(strings.SplitN(f.Position, ",", 2)[0]), f.Why)
				if !slices.ContainsFunc(inj.why, func(w string) bool { return strings.Contains(f.Why, w) }) {
					t.Errorf("находка не о предмете инъекции (ожидается одно из %q): %s", inj.why, f)
				}
				named = named || strings.Contains(f.Position, inj.at)
			}
			if !named {
				t.Errorf("ни одна находка не называет файл %s: %v", inj.at, r.Findings)
			}
		})
	}
}

// notifyInjectedCopy — копия рабочих процессов и Makefile дерева root с
// правкой inj. Каждый файл рабочего процесса идёт путём «разбор → Marshal»
// и у близнеца, и у инъекции. Правка, не нашедшая предмета, — отказ пробы.
func notifyInjectedCopy(t *testing.T, root string, inj notifyInjection) (string, string) {
	t.Helper()
	src := filepath.Join(root, filepath.FromSlash(notifyWorkflowsDir))
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %s не читается: %v", src, err)
	}
	dst := t.TempDir()
	wfDir := filepath.Join(dst, "workflows")
	if err := os.MkdirAll(wfDir, 0o750); err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	edited := 0
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, e.Name())) // #nosec G304 -- файл рабочего процесса дерева
		if err != nil {
			t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %v", err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %s не разбирается YAML: %v", e.Name(), err)
		}
		if inj.workflow != nil && e.Name() == notifyCIWorkflow {
			edited += inj.workflow(&doc)
		}
		if raw, err = yaml.Marshal(&doc); err != nil {
			t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %s не записывается YAML: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(wfDir, e.Name()), raw, 0o600); err != nil {
			t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %v", err)
		}
	}
	mkRaw, err := os.ReadFile(filepath.Join(root, "Makefile")) // #nosec G304 -- Makefile дерева
	if err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	mk := string(mkRaw)
	if inj.makefile != nil {
		var n int
		mk, n = inj.makefile(mk)
		edited += n
	}
	mkPath := filepath.Join(dst, "Makefile")
	if err := os.WriteFile(mkPath, []byte(mk), 0o600); err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	if (inj.workflow != nil || inj.makefile != nil) && edited != 1 {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: правок %d, ожидается 1 — инъекция не о том", edited)
	}
	return wfDir, mkPath
}

// notifyJobs — задания разобранного рабочего процесса.
func notifyJobs(doc *yaml.Node) []*yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	jobs := yamlNodeGet(doc.Content[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil
	}
	var out []*yaml.Node
	for i := 1; i < len(jobs.Content); i += 2 {
		out = append(out, jobs.Content[i])
	}
	return out
}

// isNotifyStep — тело шага равно записи ведомости (после снятия краевых
// пробелов).
func isNotifyStep(step *yaml.Node, rec string) bool {
	run := yamlNodeGet(step, "run")
	return run != nil && strings.TrimSpace(run.Value) == rec
}

// dropNotifyStep снимает шаги записи rec и возвращает их число.
func dropNotifyStep(doc *yaml.Node, rec string) int {
	removed := 0
	for _, job := range notifyJobs(doc) {
		steps := yamlNodeGet(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		kept := steps.Content[:0]
		for _, s := range steps.Content {
			if isNotifyStep(s, rec) {
				removed++
				continue
			}
			kept = append(kept, s)
		}
		steps.Content = kept
	}
	return removed
}

// editNotifyStep правит шаги записи rec и возвращает их число.
func editNotifyStep(doc *yaml.Node, rec string, edit func(step *yaml.Node)) int {
	n := 0
	for _, job := range notifyJobs(doc) {
		steps := yamlNodeGet(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, s := range steps.Content {
			if isNotifyStep(s, rec) {
				edit(s)
				n++
			}
		}
	}
	return n
}

// shallowNotifyJobCheckout ставит fetch-depth: 1 checkout'у задания, где
// стоит шаг записи rec, и возвращает число правленых заданий.
func shallowNotifyJobCheckout(doc *yaml.Node, rec string) int {
	n := 0
	for _, job := range notifyJobs(doc) {
		steps := yamlNodeGet(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode || !slices.ContainsFunc(steps.Content, func(s *yaml.Node) bool { return isNotifyStep(s, rec) }) {
			continue
		}
		for _, s := range steps.Content {
			uses := yamlNodeGet(s, "uses")
			if uses == nil || !strings.HasPrefix(uses.Value, "actions/checkout@") {
				continue
			}
			with := yamlNodeGet(s, "with")
			if with == nil {
				with = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				s.Content = append(s.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "with"}, with)
			}
			yamlNodeSet(with, "fetch-depth", "1")
			n++
		}
	}
	return n
}

// dropNotifyPushTrigger снимает триггер push с рабочего процесса.
func dropNotifyPushTrigger(doc *yaml.Node) int {
	if len(doc.Content) == 0 {
		return 0
	}
	on := yamlNodeGet(doc.Content[0], "on")
	if on == nil || on.Kind != yaml.MappingNode {
		return 0
	}
	for i := 0; i+1 < len(on.Content); i += 2 {
		if on.Content[i].Value == "push" {
			on.Content = append(on.Content[:i], on.Content[i+2:]...)
			return 1
		}
	}
	return 0
}

func yamlNodeGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// yamlNodeSet ставит скалярное значение ключа key, заводя ключ при нужде.
func yamlNodeSet(m *yaml.Node, key, value string) {
	if v := yamlNodeGet(m, key); v != nil {
		v.Kind, v.Tag, v.Value, v.Content = yaml.ScalarNode, "", value, nil
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

// makeOut — вывод и код `make` в корне дерева. Отказ запуска make — отказ
// пробы, а не код цели.
func makeOut(t *testing.T, root string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("make", append([]string{"--no-print-directory"}, args...)...) // #nosec G204 -- argv[0] фиксирован, аргументы — литералы пробы
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err == nil {
		return out.String(), 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: make %s не запустился: %v", strings.Join(args, " "), err)
	}
	return out.String(), ee.ExitCode()
}

// linesWith — строки s, содержащие каждое из слов words как отдельное слово.
func linesWith(s string, words ...string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		f := strings.Fields(ln)
		if !slices.ContainsFunc(words, func(w string) bool { return !slices.Contains(f, w) }) {
			out = append(out, ln)
		}
	}
	return out
}

// TestNTF1D08_NotificationsCheckRecipeCallsTheGenerator — тело цели
// `notifications-check`: `make -n` печатает ровно один вызов генератора пина
// с `-check -base <база>`, без гасителя кода; без BASE цель отказывает
// «УСЛОВИЕ НЕ СОЗДАНО»; база, не разрешающаяся в коммит, — красный генератора
// «база не найдена» с ненулевым кодом (D07 (д)), а не пропуск.
func TestNTF1D08_NotificationsCheckRecipeCallsTheGenerator(t *testing.T) {
	t.Parallel()
	root := repoRootFor(t)
	const base = "d08probebase"
	out, code := makeOut(t, root, "-n", "notifications-check", "BASE="+base)
	if code != 0 {
		t.Fatalf("make -n notifications-check BASE=%s: код %d\n%s", base, code, out)
	}
	calls := linesWith(out, "-check", "-base")
	if len(calls) != 1 {
		t.Fatalf("ожидается ровно один вызов генератора с -check -base, найдено %d:\n%s", len(calls), out)
	}
	call := calls[0]
	for _, want := range []string{"github.com/PRO-Robotech/corelib/cmd/notifygen", `"` + base + `"`} {
		if !strings.Contains(call, want) {
			t.Errorf("вызов генератора не несёт %s: %s", want, call)
		}
	}
	for _, quench := range []string{"|| true", "|| :", "|| exit 0", "; true", "set +e"} {
		if strings.Contains(call, quench) {
			t.Errorf("вызов генератора гасит код (%q): %s", quench, call)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(call), "-") {
		t.Errorf("вызов генератора с префиксом «-» — make игнорирует его код: %s", call)
	}

	t.Run("без BASE — условие не создано, код ненулевой", func(t *testing.T) {
		out, code := makeOut(t, root, "notifications-check")
		if code == 0 || !strings.Contains(out, "УСЛОВИЕ НЕ СОЗДАНО") {
			t.Fatalf("ожидается отказ «УСЛОВИЕ НЕ СОЗДАНО» с ненулевым кодом, код %d:\n%s", code, out)
		}
	})

	t.Run("база не разрешается в коммит — база не найдена, код ненулевой", func(t *testing.T) {
		out, code := makeOut(t, root, "notifications-check", "BASE=refs/heads/ntf1-d08-no-such-base")
		if code == 0 || !strings.Contains(out, "не найдена") {
			t.Fatalf("ожидается красный генератора «база не найдена» с ненулевым кодом, код %d:\n%s", code, out)
		}
	})
}

// notifyTreeGateTests — имена проб верхнего уровня файла тонких вызывающих
// гейтов дерева NTF-1 (разбор файла, а не поиск образца).
func notifyTreeGateTests(t *testing.T, root string) []string {
	t.Helper()
	path := filepath.Join(root, "internal", "repohygiene", notifyTreeGatesFile)
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: %s не разбирается: %v", path, err)
	}
	var names []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Test") {
			names = append(names, fd.Name.Name)
		}
	}
	if len(names) == 0 {
		t.Fatalf("проба НЕ ИСПОЛНЯЛАСЬ: в %s ноль проб верхнего уровня", path)
	}
	sort.Strings(names)
	return names
}

// TestNTF1D08_NotifyTreeGatesRecipeRunsEveryGate — тело цели
// `notify-tree-gates`: `make -n` печатает ровно один `go test` пакета
// гейтов с `-count=1` и `-run`, чьё множество имён равно множеству проб
// верхнего уровня файла тонких вызывающих — проба, заведённая без строки
// перечня, и строка без пробы равно находки.
func TestNTF1D08_NotifyTreeGatesRecipeRunsEveryGate(t *testing.T) {
	t.Parallel()
	root := repoRootFor(t)
	out, code := makeOut(t, root, "-n", "notify-tree-gates")
	if code != 0 {
		t.Fatalf("make -n notify-tree-gates: код %d\n%s", code, out)
	}
	runs := linesWith(out, "go", "test", "./internal/repohygiene/")
	if len(runs) != 1 {
		t.Fatalf("ожидается ровно один go test ./internal/repohygiene/, найдено %d:\n%s", len(runs), out)
	}
	if !slices.Contains(strings.Fields(runs[0]), "-count=1") {
		t.Errorf("go test без -count=1 — вердикт дерева из кеша недействителен: %s", runs[0])
	}
	m := regexp.MustCompile(`-run '\^\(([A-Za-z0-9_|]+)\)\$'`).FindStringSubmatch(runs[0])
	if m == nil {
		t.Fatalf("образец -run не разобран (ожидается '^(A|B|…)$'): %s", runs[0])
	}
	got := strings.Split(m[1], "|")
	sort.Strings(got)
	want := notifyTreeGateTests(t, root)
	t.Logf("перечень цели %d · проб в %s %d", len(got), notifyTreeGatesFile, len(want))
	if !slices.Equal(got, want) {
		t.Fatalf("перечень цели notify-tree-gates разошёлся с пробами %s:\n  цель:  %q\n  пробы: %q",
			notifyTreeGatesFile, got, want)
	}

	t.Run("перечень с именем без пробы — цель красная, а не «no tests to run»", func(t *testing.T) {
		out, code := makeOut(t, root, "notify-tree-gates", "NOTIFY_TREE_GATES=TestNTF1D08NoSuchGate")
		if code == 0 || !strings.Contains(out, "исполнено зелёными 0") {
			t.Fatalf("ожидается ненулевой код и «исполнено зелёными 0», код %d:\n%s", code, out)
		}
	})
}
