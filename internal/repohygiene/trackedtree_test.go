// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Единый ответ на вопрос «что такое ДЕРЕВО, о котором говорит гейт».
//
// # Зачем это отдельный файл
//
// Гейт, идущий от корня репозитория обходом ДИСКА, читает и то, чего в
// репозитории нет: рабочие копии агентов под `.claude/worktrees/`, распаковки,
// отчёты прогонов, локальные оверлеи с кредами. Все они перечислены в
// `.gitignore` — то есть автор дерева уже сказал, что частью его они не
// являются, — но `filepath.Walk` этого не знает.
//
// Следствие не косметическое: вердикт гейта перестаёт быть свойством КОММИТА и
// становится свойством чужого рабочего каталога. Померено на 8c2eba3e, а не
// предположено: в чистой копии `go test ./internal/repohygiene/...` зелёный, а
// в той же копии плюс ОДИН git-игнорируемый каталог с деревом внутри — красный,
// 14 находок, и КАЖДАЯ координата начинается с `.claude/worktrees/`. Одного
// файла в игнорируемом каталоге хватает, чтобы фаз-гейт объявил «цель есть в
// дереве, но не в матрице» о цели, которой в репозитории не существует.
//
// Обратная сторона того же дефекта тише и хуже: находка, лежащая ВНЕ
// игнорируемого каталога, тонет в сотнях привнесённых, и её никто не читает.
//
// # Что здесь считается авторитетом
//
// `git ls-files` — ровно то множество, которое увидит свежий checkout и CI.
// Тот же выбор и по той же причине уже сделан в `license_test.go` (SPDX) и в
// `run-gate-self-tests.sh` (состав самопроверок); здесь он становится общим.
//
// Недоступность git — ОТКАЗ, а не пропуск. Молчаливый откат на обход диска
// вернул бы ровно тот дефект, ради которого файл написан, и сделал бы это
// незаметно.
type trackedTree struct {
	root  string
	files map[string]bool // пути от корня, слэш-разделённые
	dirs  map[string]bool // каталоги, в которых есть хоть один отслеживаемый файл
}

// newTrackedTree читает индекс git и раскладывает его в два множества: файлы и
// каталоги-предки. Второе нужно, чтобы обход мог отсекать целые поддеревья
// (`filepath.SkipDir`), а не только фильтровать файлы поштучно: игнорируемая
// рабочая копия дерева весит сотни мегабайт, и читать её ради последующего
// отбрасывания незачем.
func newTrackedTree(t *testing.T, root string) *trackedTree {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files в %s: %v — гейт не может назвать дерево, о котором "+
			"он говорит, и обход диска вместо индекса читал бы игнорируемые "+
			"каталоги (рабочие копии агентов, отчёты прогонов). Это отказ, а не пропуск.",
			root, err)
	}
	return parseTrackedTree(root, out)
}

// newSyntheticTree — состав СИНТЕТИЧЕСКОГО дерева, собранного самой проверкой
// во временном каталоге. Такое дерево не является репозиторием, спрашивать у
// него индекс нечего, и обход файловой системы здесь — не откат, а
// единственный возможный авторитет.
//
// Конструктор ОТДЕЛЬНЫЙ намеренно. Молчаливый откат «нет git — иду по диску»
// внутри newTrackedTree вернул бы ровно тот дефект, ради которого написан этот
// файл, и сделал бы это невидимо: на машине без git гейт продолжал бы
// «работать», читая игнорируемые каталоги. Тот же приём и по той же причине уже
// применён в `run-gate-self-tests.sh` (`discover()`): для репозитория авторитет
// — версионный контроль, для синтетики — обход.
func newSyntheticTree(t *testing.T, root string) *trackedTree {
	t.Helper()
	tt := &trackedTree{root: root, files: map[string]bool{}, dirs: map[string]bool{}}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		tt.files[rel] = true
		for d := filepath.ToSlash(filepath.Dir(rel)); d != "." && d != "/"; d = filepath.ToSlash(filepath.Dir(d)) {
			tt.dirs[d] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход синтетического дерева %s: %v", root, err)
	}
	return tt
}

// parseTrackedTree — разбор вывода `git ls-files -z`, вынесен отдельно, чтобы
// инъекция могла подать синтетический ввод.
func parseTrackedTree(root string, nulSeparated []byte) *trackedTree {
	tt := &trackedTree{root: root, files: map[string]bool{}, dirs: map[string]bool{}}
	for _, rel := range strings.Split(string(nulSeparated), "\x00") {
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		tt.files[rel] = true
		for d := filepath.ToSlash(filepath.Dir(rel)); d != "." && d != "/"; d = filepath.ToSlash(filepath.Dir(d)) {
			tt.dirs[d] = true
		}
	}
	return tt
}

// hasFile — файл лежит в индексе.
func (tt *trackedTree) hasFile(rel string) bool { return tt.files[filepath.ToSlash(rel)] }

// hasDir — в каталоге (или ниже) есть хоть один отслеживаемый файл. Каталог, о
// котором индекс не знает, обходить незачем.
func (tt *trackedTree) hasDir(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == "." || rel == "" || tt.dirs[rel]
}

// count — сколько файлов индекса прочитано. Перепись печатается вызывающими,
// чтобы «ноль находок» отличалось от «ноль прочитанного».
func (tt *trackedTree) count() int { return len(tt.files) }

// ─── ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ ──────────────────────────────────────────────────

// TestTrackedTreeExcludesIgnoredAndKeepsTracked — предикат обязан краснеть на
// внесённом дефекте и МОЛЧАТЬ на законной конструкции той же формы.
//
// Дефект вносится настоящим: во временном репозитории заводится игнорируемый
// каталог с .go-файлом внутри — та самая форма, что живёт в рабочих деревьях
// агентов. Законная конструкция — отслеживаемый .go-файл в каталоге с ТЕМ ЖЕ
// необычным именем-префиксом, чтобы отсев не мог оказаться грубым запретом по
// имени: удали фильтрацию по индексу — и первая половина покраснеет; замени её
// на «отбрасывать всё, что начинается с .claude» — покраснеет вторая.
func TestTrackedTreeExcludesIgnoredAndKeepsTracked(t *testing.T) {
	root := t.TempDir()
	mustRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	mustRun("init", "-q")
	write(".gitignore", ".claude/worktrees/\n")
	write("services/x/real.go", "package x\n")
	// Законная конструкция ТОЙ ЖЕ ФОРМЫ: каталог `.claude`, но НЕ `worktrees`,
	// и файл в индексе. Он обязан остаться виден.
	write(".claude/agents/kept.go", "package agents\n")
	mustRun("add", ".gitignore", "services/x/real.go", ".claude/agents/kept.go")
	mustRun("-c", "user.name=t", "-c", "user.email=t@example.invalid",
		"commit", "-q", "-m", "fixture")
	// ДЕФЕКТ: копия дерева в игнорируемом каталоге.
	write(".claude/worktrees/copy/services/x/real.go", "package x\n")
	write(".claude/worktrees/copy/ghost.go", "package ghost\n")

	tt := newTrackedTree(t, root)

	if got := tt.count(); got != 3 {
		var names []string
		for f := range tt.files {
			names = append(names, f)
		}
		sort.Strings(names)
		t.Fatalf("перепись: прочитано %d файлов индекса, ожидалось 3 (%v)", got, names)
	}
	// (а) КРАСНОЕ НАПРАВЛЕНИЕ: привнесённое из игнорируемого каталога не видно.
	for _, rel := range []string{
		".claude/worktrees/copy/ghost.go",
		".claude/worktrees/copy/services/x/real.go",
	} {
		if tt.hasFile(rel) {
			t.Errorf("%s принят за часть дерева — обход читает диск, а не индекс", rel)
		}
	}
	if tt.hasDir(".claude/worktrees") || tt.hasDir(".claude/worktrees/copy") {
		t.Error(".claude/worktrees/ не отсечён как каталог — поддерево будет прочитано целиком")
	}
	// (б) МОЛЧАЛИВОЕ НАПРАВЛЕНИЕ: законное той же формы остаётся видимым.
	if !tt.hasFile(".claude/agents/kept.go") {
		t.Error(".claude/agents/kept.go потерян — отсев грубее своего предмета: " +
			"он запрещает по имени каталога вместо того, чтобы спрашивать индекс")
	}
	if !tt.hasDir(".claude/agents") || !tt.hasDir("services/x") {
		t.Error("каталог с отслеживаемым содержимым объявлен ненужным")
	}
	if !tt.hasFile("services/x/real.go") {
		t.Error("обычный отслеживаемый файл потерян")
	}
}

// TestTreeWalkersAskTheIndex — предпосылка запрета, записанная так, чтобы её
// можно было опровергнуть.
//
// Запрет обоснован фактом о дереве: под корнем репозитория ЛЕЖАТ каталоги,
// которые git игнорирует. Если факт перестанет быть верным, запрет станет
// пустым — и об этом должно быть видно, а не догадываться. Поэтому проверка
// утверждает не «в дереве нет мусора», а «обходчики спрашивают индекс»: она
// перечисляет обходы от КОРНЯ репозитория и требует, чтобы каждый шёл через
// trackedTree.
//
// Перечисление идёт по исходникам пакета, а не по памяти автора: новый обход от
// корня, добавленный мимо помощника, обязан быть виден.
func TestTreeWalkersAskTheIndex(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// Файлы самого пакета — из индекса, не с диска (иначе проверка про обход
	// индекса сама читала бы диск).
	var pkgFiles []string
	for f := range tt.files {
		if strings.HasPrefix(f, "internal/repohygiene/") && strings.HasSuffix(f, ".go") {
			pkgFiles = append(pkgFiles, f)
		}
	}
	sort.Strings(pkgFiles)
	if len(pkgFiles) == 0 {
		t.Fatal("прочитано ноль файлов пакета — перепись пуста, вердикт ничего не значит")
	}

	// Обход от КОРНЯ узнаётся по аргументу: `filepath.Walk(root,` /
	// `filepath.WalkDir(root,`. Обход поддерева (`filepath.Join(root, "services")`)
	// сюда не подпадает by construction: `.claude/` лежит только в корне.
	var offenders []string
	scanned := 0
	for _, rel := range pkgFiles {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		scanned++
		for i, line := range strings.Split(string(body), "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "//") { // объяснение формы — не форма
				continue
			}
			if !strings.Contains(code, "filepath.Walk(root,") &&
				!strings.Contains(code, "filepath.WalkDir(root,") {
				continue
			}
			offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+" — "+code)
		}
	}

	t.Logf("осмотрено файлов пакета: %d (индекс: %d файлов)", scanned, tt.count())
	if len(offenders) > 0 {
		t.Errorf("обход от корня репозитория идёт по ДИСКУ, а не по индексу — %d шт.:\n  %s\n\n"+
			"Под корнем лежат каталоги, которых в репозитории нет (`.claude/worktrees/` —"+
			" рабочие копии агентов, отчёты прогонов, локальные оверлеи). Прочитав их,"+
			" гейт делает свой вердикт свойством ЧУЖОГО рабочего каталога, а не коммита."+
			" Возьми список файлов у newTrackedTree(t, root).",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}
