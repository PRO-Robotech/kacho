// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
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

// ─── ОБХОД ОТ КОРНЯ РЕПОЗИТОРИЯ: РАЗБОР, А НЕ ПОИСК ПОДСТРОКИ ────────────────

// diskRootWalk — одна находка: вызов filepath.Walk/WalkDir, чей корень пришёл
// от функции, находящей корень репозитория.
type diskRootWalk struct {
	File string
	Line int
	Func string // объемлющая функция — по ней читается, ЧЕЙ это обход
	Text string
}

// rootProducers — функции пакета, говорящие о КОРНЕ МОДУЛЯ.
//
// Признак структурный, а не по списку имён: функция возвращает строку и
// опирается на `go.mod`. Переименование такой функции признак переживает,
// захардкоженный список имён — нет.
//
// Признак намеренно ШИРЕ предмета: под него попадает и `moduleImportPath`,
// которая `go.mod` читает, а не поднимается к нему. Ошибка в эту сторону
// громкая — лишняя находка видна и разбирается; в обратную она тихая, и гейт
// молча перестал бы искать. На дереве a15066ee (23 файла пакета) ложных
// находок от этой широты нет.
//
// Пустой результат означает, что предпосылка гейта исчезла: отличить обход
// корня от обхода поддерева стало нечем. Вызывающий обязан на этом
// остановиться, а не выдать «ноль находок».
func rootProducers(files map[string]*ast.File) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Body == nil ||
				fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
				continue
			}
			returnsString := false
			for _, r := range fd.Type.Results.List {
				if id, isIdent := r.Type.(*ast.Ident); isIdent && id.Name == "string" {
					returnsString = true
				}
			}
			if !returnsString {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				lit, isLit := n.(*ast.BasicLit)
				if isLit && lit.Kind == token.STRING && lit.Value == `"go.mod"` {
					out[fd.Name.Name] = true
				}
				return true
			})
		}
	}
	return out
}

// calleeName — имя вызываемой функции (`f(…)` и `x.f(…)` дают `f`).
func walkCalleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// paramNameAt — имя i-го параметра (учитывая `a, b string` одним полем).
func paramNameAt(fd *ast.FuncDecl, i int) string {
	if fd.Type == nil || fd.Type.Params == nil {
		return ""
	}
	pos := 0
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			if pos == i {
				return ""
			}
			pos++
			continue
		}
		for _, nm := range field.Names {
			if pos == i {
				return nm.Name
			}
			pos++
		}
	}
	return ""
}

// isRootValued — выражение несёт корень репозитория: либо это прямой вызов
// производителя, либо имя, уже окрашенное в этой функции.
func isRootValued(e ast.Expr, producers, local map[string]bool) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return local[v.Name]
	case *ast.CallExpr:
		return producers[walkCalleeName(v.Fun)]
	}
	return false
}

// rootValuedIdents — имена внутри fd, несущие корень репозитория: окрашенные
// параметры плюс присваивания от производителя или от уже окрашенного имени.
func rootValuedIdents(fd *ast.FuncDecl, producers map[string]bool, tainted map[string]map[string]bool) map[string]bool {
	local := map[string]bool{}
	if fd.Name != nil {
		for name := range tainted[fd.Name.Name] {
			local[name] = true
		}
	}
	if fd.Body == nil {
		return local
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, lhs := range as.Lhs {
				id, isIdent := lhs.(*ast.Ident)
				if !isIdent || id.Name == "_" || local[id.Name] {
					continue
				}
				if isRootValued(as.Rhs[i], producers, local) {
					local[id.Name] = true
					changed = true
				}
			}
			return true
		})
	}
	return local
}

// findDiskRootWalks — обходы ДИСКА от корня репозитория, найденные РАЗБОРОМ.
//
// # Почему не поиск подстроки
//
// Первая редакция искала `filepath.Walk(root,` подстрокой и снимала строки,
// начинающиеся с `//`. Она находила сама себя — её собственные литералы-образцы
// суть код, а не комментарий, поэтому фильтр их не снимал; и она находила
// `newSyntheticTree`, где `root` — временный каталог, а не корень репозитория,
// потому что дискриминатором было ИМЯ переменной. Разбор снимает оба: литерал
// вызовом не является by construction, а корень обхода прослеживается до
// источника, а не до написания.
//
// # Что считается корнем репозитория
//
// Значение, пришедшее от `rootProducers` — прямо, через присваивание или через
// параметр вызванной функции (неподвижная точка по графу вызовов пакета).
// Поэтому `filepath.Join(root, "services")` (поддерево) и `t.TempDir()`
// (синтетика) не подпадают — не по имени, а потому что источник другой.
//
// # Граница, о которой гейт заявляет вслух
//
// Граф вызовов строится по ИМЕНИ функции внутри пакета: значение, уехавшее в
// переменную-функцию или в чужой пакет, не прослеживается. Это осознанный
// предел, а не упущение — обходы этого пакета живут в нём же.
func findDiskRootWalks(fset *token.FileSet, files map[string]*ast.File, producers map[string]bool) []diskRootWalk {
	funcs := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name != nil && fd.Body != nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}

	// Окраска параметров по графу вызовов до неподвижной точки. Порядок обхода
	// map на результат не влияет: повторяем, пока что-то меняется.
	tainted := map[string]map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, fd := range funcs {
			local := rootValuedIdents(fd, producers, tainted)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee, known := funcs[walkCalleeName(call.Fun)]
				if !known {
					return true
				}
				for i, arg := range call.Args {
					if !isRootValued(arg, producers, local) {
						continue
					}
					p := paramNameAt(callee, i)
					if p == "" || p == "_" {
						continue
					}
					if tainted[callee.Name.Name] == nil {
						tainted[callee.Name.Name] = map[string]bool{}
					}
					if !tainted[callee.Name.Name][p] {
						tainted[callee.Name.Name][p] = true
						changed = true
					}
				}
				return true
			})
		}
	}

	var out []diskRootWalk
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				return true
			}
			local := rootValuedIdents(fd, producers, tainted)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				sel, isSel := call.Fun.(*ast.SelectorExpr)
				if !isSel {
					return true
				}
				pkg, isIdent := sel.X.(*ast.Ident)
				if !isIdent || pkg.Name != "filepath" {
					return true
				}
				if sel.Sel.Name != "Walk" && sel.Sel.Name != "WalkDir" {
					return true
				}
				if len(call.Args) == 0 || !isRootValued(call.Args[0], producers, local) {
					return true
				}
				enclosing := ""
				if fd.Name != nil {
					enclosing = fd.Name.Name
				}
				out = append(out, diskRootWalk{
					File: name,
					Line: fset.Position(call.Pos()).Line,
					Func: enclosing,
					Text: "filepath." + sel.Sel.Name + "(" + renderExpr(fset, call.Args[0]) + ", …)",
				})
				return true
			})
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// renderExpr — выражение обратно в текст, чтобы находка называла координату
// вместе с тем, что именно там написано.
func renderExpr(fset *token.FileSet, e ast.Expr) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "<неразобрано>"
	}
	return b.String()
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
// корня, добавленный мимо помощника, обязан быть виден. Дискриминатор проверен
// в обе стороны отдельно — см. TestDiskRootWalkDiscriminatorCutsBothWays.
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

	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, rel := range pkgFiles {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		parsed, perr := parser.ParseFile(fset, rel, body, parser.ParseComments)
		if perr != nil {
			t.Fatalf("%s: разбор: %v", rel, perr)
		}
		files[rel] = parsed
	}

	producers := rootProducers(files)
	if len(producers) == 0 {
		t.Fatal("в пакете не нашлось ни одной функции, находящей корень репозитория " +
			"(возвращает строку и упоминает `go.mod`). Предпосылка гейта исчезла: " +
			"без источника корня отличить обход корня от обхода поддерева нечем, " +
			"и «ноль находок» тут значило бы «нечем было искать».")
	}
	names := make([]string, 0, len(producers))
	for p := range producers {
		names = append(names, p)
	}
	sort.Strings(names)

	t.Logf("осмотрено файлов пакета: %d (индекс: %d файлов); источники корня: %s",
		len(files), tt.count(), strings.Join(names, ", "))

	offenders := findDiskRootWalks(fset, files, producers)
	if len(offenders) > 0 {
		lines := make([]string, 0, len(offenders))
		for _, o := range offenders {
			lines = append(lines, o.File+":"+strconv.Itoa(o.Line)+" ("+o.Func+") — "+o.Text)
		}
		t.Errorf("обход от корня репозитория идёт по ДИСКУ, а не по индексу — %d шт.:\n  %s\n\n"+
			"Под корнем лежат каталоги, которых в репозитории нет (`.claude/worktrees/` —"+
			" рабочие копии агентов, отчёты прогонов, локальные оверлеи). Прочитав их,"+
			" гейт делает свой вердикт свойством ЧУЖОГО рабочего каталога, а не коммита."+
			" Возьми список файлов у newTrackedTree(t, root).",
			len(offenders), strings.Join(lines, "\n  "))
	}
}

// TestDiskRootWalkDiscriminatorCutsBothWays — дискриминатор обязан ловить то,
// что должен, и МОЛЧАТЬ на законной конструкции той же формы.
//
// Оба направления — не абстрактные: это ровно те два случая, на которых
// текстовая редакция гейта краснела в чистой копии a15066ee.
//
//   - красное: корень репозитория уходит в обход прямо, через переменную и
//     через параметр помощника (последнее — способ обойти запрет «мимо
//     помощника», ради которого гейт и написан);
//   - молчаливое: параметр с ТЕМ ЖЕ именем `root`, которому передают временный
//     каталог; обход поддерева `filepath.Join(root, …)`; и упоминание формы в
//     строковом литерале и в комментарии — то есть в тексте, но не в коде.
//
// Убери прослеживание источника — покраснеет вторая половина; убери разбор в
// пользу поиска подстроки — покраснеют литерал и комментарий.
func TestDiskRootWalkDiscriminatorCutsBothWays(t *testing.T) {
	const src = `package p

import (
	"path/filepath"
	"os"
	"testing"
)

func repoRoot(t *testing.T) string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
}

// ЛОВИТСЯ: корень уходит в обход через переменную.
func viaVar(t *testing.T) {
	root := repoRoot(t)
	_ = filepath.Walk(root, nil)
}

// ЛОВИТСЯ: корень уходит в обход прямо, без промежуточного имени.
func viaInline(t *testing.T) {
	_ = filepath.WalkDir(repoRoot(t), nil)
}

// ЛОВИТСЯ: обход спрятан в помощнике, корень приезжает параметром.
func helperWalks(dir string) { _ = filepath.Walk(dir, nil) }

func viaHelper(t *testing.T) { helperWalks(repoRoot(t)) }

// МОЛЧИТ: тот же параметр с тем же именем, но приезжает временный каталог.
func syntheticWalks(root string) { _ = filepath.Walk(root, nil) }

func viaSynthetic(t *testing.T) { syntheticWalks(t.TempDir()) }

// МОЛЧИТ: обход поддерева.
func subtree(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "services")
	_ = filepath.Walk(base, nil)
}

// МОЛЧИТ: форма упомянута в строковом литерале — это текст, а не вызов.
// И здесь, в комментарии, тоже: filepath.Walk(root, …).
func mentionsOnly(t *testing.T) {
	_ = "filepath.Walk(root, ...)"
	_ = "filepath.WalkDir(root, ...)"
}
`
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "synthetic.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	files := map[string]*ast.File{"synthetic.go": parsed}

	producers := rootProducers(files)
	if !producers["repoRoot"] {
		t.Fatalf("источник корня не опознан структурно (%v) — дискриминатор слеп на входе", producers)
	}

	got := map[string]bool{}
	for _, o := range findDiskRootWalks(fset, files, producers) {
		got[o.Func] = true
	}

	// (а) КРАСНОЕ НАПРАВЛЕНИЕ — каждое обязано быть названо.
	mustCatch := map[string]string{
		"viaVar":      "корень уходит в обход через переменную",
		"viaInline":   "корень уходит в обход прямо, без промежуточного имени",
		"helperWalks": "обход спрятан в помощнике, корень приезжает параметром",
	}
	for fn, why := range mustCatch {
		if !got[fn] {
			t.Errorf("не пойман обход корня в %s (%s); найдено: %v", fn, why, sortedNames(got))
		}
	}

	// (б) МОЛЧАЛИВОЕ НАПРАВЛЕНИЕ — законная конструкция ТОЙ ЖЕ ФОРМЫ.
	//
	// helperWalks и syntheticWalks пишут БУКВАЛЬНО одно и то же —
	// `filepath.Walk(<параметр>, nil)`; различает их только источник значения.
	// subtree обходит поддерево, mentionsOnly упоминает форму текстом.
	mustStaySilent := map[string]string{
		"syntheticWalks": "тот же параметр, но приезжает временный каталог — " +
			"дискриминатором стало ИМЯ переменной, а не источник значения",
		"viaSynthetic": "вызов синтетического обхода сам обходом не является",
		"subtree": "обход поддерева объявлен обходом корня — " +
			"дискриминатор грубее своего предмета",
		"mentionsOnly": "форма найдена в строковом литерале или комментарии — " +
			"проверка читает ТЕКСТ, а не исполняемую часть",
		"repoRoot": "поиск go.mod принят за обход",
	}
	for fn, why := range mustStaySilent {
		if got[fn] {
			t.Errorf("ложная находка в %s: %s", fn, why)
		}
	}

	if n := len(got); n != len(mustCatch) {
		t.Errorf("находок в %d функциях, ожидалось %d: %v", n, len(mustCatch), sortedNames(got))
	}
}

// keysOf — отсортированные ключи, чтобы сообщение падения было читаемым.
func sortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
