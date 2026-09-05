// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gitcommandenv_test.go — гейт против вызова git, наследующего окружение git.
//
// # Предмет
//
// git определяет репозиторий не только по рабочему каталогу: переменная
// `GIT_DIR` **сильнее** его. Проба, заводящая синтетический репозиторий во
// временном каталоге (`git init` + `git add -A`) и задающая `cmd.Dir`, при
// выставленном `GIT_DIR` работает не со своим репозиторием, а с тем, из которого
// запущен прогон, — пишет его индекс и его конфигурацию.
//
// Замер, из которого гейт выведен (те же пакеты, дерево `release/console`):
// без `GIT_DIR` индекс рабочей копии цел (6994 записи), падений 0; с `GIT_DIR`
// индекс схлопывается до 3 записей, падений 119, а в конфигурацию уезжают
// `user.name`, `user.email` и `core.bare=true`.
//
// # Почему гейт, а не разовая правка
//
// Триггер закрыт на границе — хук `scripts/hooks/pre-push` снимает эти
// переменные перед запуском проверок. Класс этим НЕ закрыт: любой другой запуск
// `go test` с выставленным `GIT_DIR` (из другого хука, из скрипта, из обёртки)
// повторит то же самое, а следующая проба с git-фикстурой будет написана в той
// же неверной форме — она выглядит совершенно обычно.
//
// Следствие тихое: падают не виновные пробы, а гейты, читающие состав дерева
// через `git ls-files` ([treecorpus]) — они видят схлопнутый индекс и честно
// сообщают «прочитано ноль». Виновник не назван, и «ноль находок» становится
// неотличим от «ноль прочитанного».
//
// # Что гейт утверждает
//
//  1. вызов git в Go-коде идёт через `pkg/gitenv` — прямой
//     `exec.Command("git", …)` вне самого помощника есть находка;
//  2. окружение, снятое помощником, не возвращается обратно присваиванием
//     `cmd.Env = append(os.Environ(), …)` — дописывать надо к `cmd.Env`.
//
// Чего гейт НЕ утверждает, названо, чтобы его молчание не читалось шире: он не
// видит вызова git через оболочку (`sh -c "git …"`), через стороннюю библиотеку
// и через имя, вычисляемое в рантайме. Такие формы в дереве не встречаются
// (перепись — в выводе гейта), и появление первой из них — повод расширить
// разбор, а не завести исключение.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: слово `exec.Command`
// стоит в комментариях, объясняющих эту же защиту, и в синтетических исходниках
// парной пробы ниже — текстовый поиск принял бы их за вызовы.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// gitEnvHelperPkg — каталог помощника. Единственное место, где прямой вызов
// git законен: там он и снимает окружение.
//
// Самоистечение: если каталог переедет или исчезнет, перепись ниже сообщит о
// нулевом числе файлов помощника и гейт откажет — исключение не переживёт
// своего предмета.
const gitEnvHelperPkg = "pkg/gitenv"

// gitFinding — одно место, названное координатой и видом.
type gitFinding struct {
	pos  string
	kind string
	what string
}

func TestGitCommandsRunWithScrubbedEnvironment(t *testing.T) {
	root := repoRoot(t)

	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	var (
		findings   []gitFinding
		scanned    int
		gitCalls   int // сколько вызовов git найдено ВСЕГО (включая законные)
		helperFile int // сколько файлов помощника прочитано
	)

	fset := token.NewFileSet()
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("относительный путь %s: %v", path, err)
		}
		rel = filepath.ToSlash(rel)

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			// Неразбираемый файл — отказ, а не пропуск: пропущенный файл
			// вычитается из знаменателя молча, и «0 находок» перестаёт
			// означать «прочитано всё».
			t.Fatalf("разбор %s: %v", rel, err)
		}
		scanned++

		inHelper := strings.HasPrefix(rel, gitEnvHelperPkg+"/")
		if inHelper {
			helperFile++
		}

		calls, leaks := scanGitUsage(fset, file, root)
		gitCalls += len(calls)
		if !inHelper {
			for _, c := range calls {
				findings = append(findings, gitFinding{pos: c.pos, kind: "прямой вызов", what: c.what})
			}
		}
		findings = append(findings, leaks...)
	}

	// ПРЕДПОСЫЛКА ГЕЙТА. Она обязана проверяться сама, иначе «ноль находок»
	// однажды будет означать «ничего не прочитано».
	if scanned == 0 {
		t.Fatalf("прочитано НОЛЬ файлов .go — проверять было нечего.\n" +
			"Это отказ, а не успех: пустой обход неотличим от чистого дерева.")
	}
	if helperFile == 0 {
		t.Fatalf("помощника %s в дереве НЕТ (прочитано файлов: %d).\n"+
			"Исключение из гейта пережило свой предмет: либо помощник переехал —\n"+
			"тогда правится константа gitEnvHelperPkg, — либо его сняли, и тогда\n"+
			"снимать надо весь гейт вместе с ним.", gitEnvHelperPkg, scanned)
	}
	if gitCalls == 0 {
		t.Fatalf("вызовов git в дереве НЕ найдено (прочитано файлов: %d).\n"+
			"Гейт, чей предмет — отсутствие, зеленеет и когда предмет исчез, и\n"+
			"когда сломался разбор. Ноль здесь означает второе: git зовут как\n"+
			"минимум внутри %s.", scanned, gitEnvHelperPkg)
	}

	t.Logf("осмотрено файлов .go: %d; вызовов git найдено: %d, из них законных "+
		"(в %s): %d", scanned, gitCalls, gitEnvHelperPkg, gitCalls-countKind(findings, "прямой вызов"))

	if len(findings) == 0 {
		return
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].pos < findings[j].pos })
	var b strings.Builder
	b.WriteString("вызов git в обход помощника pkg/gitenv — находок: ")
	b.WriteString(strconv.Itoa(len(findings)))
	b.WriteString("\n\n")
	for _, f := range findings {
		b.WriteString("  " + f.pos + ": " + f.kind + " — " + f.what + "\n")
	}
	b.WriteString("\nПОЧЕМУ ЭТО ЗАПРЕЩЕНО. `cmd.Dir` не выбирает репозиторий, когда\n")
	b.WriteString("в окружении есть GIT_DIR: переменная сильнее рабочего каталога.\n")
	b.WriteString("Тогда фикстура пишет индекс и конфигурацию ТОЙ рабочей копии, из\n")
	b.WriteString("которой запущен прогон, а падают потом чужие гейты, читающие\n")
	b.WriteString("состав дерева, — с сообщением «прочитано ноль».\n\n")
	b.WriteString("КАК ПРАВИТЬ:\n")
	b.WriteString("  cmd := gitenv.Command(dir, \"init\", \"-q\")            // вместо exec.Command(\"git\", …)\n")
	b.WriteString("  cmd.Env = append(cmd.Env, \"GIT_AUTHOR_NAME=t\")      // вместо append(os.Environ(), …)\n")
	t.Fatal(b.String())
}

func countKind(fs []gitFinding, kind string) int {
	n := 0
	for _, f := range fs {
		if f.kind == kind {
			n++
		}
	}
	return n
}

type gitCall struct {
	pos  string
	what string
}

// relPos — координата от корня дерева. Абсолютный путь в выводе гейта отличается
// от машины к машине и потому не воспроизводится вызывающим дословно.
func relPos(fset *token.FileSet, root string, p token.Pos) string {
	pos := fset.Position(p)
	if rel, err := filepath.Rel(root, pos.Filename); err == nil {
		pos.Filename = filepath.ToSlash(rel)
	}
	return pos.String()
}

// scanGitUsage возвращает вызовы git через os/exec и места, где снятое
// помощником окружение возвращается обратно.
func scanGitUsage(fset *token.FileSet, file *ast.File, root string) ([]gitCall, []gitFinding) {
	var (
		calls []gitCall
		leaks []gitFinding
	)

	// Строковые константы файла — чтобы `exec.Command(gitBin, …)` не проходил
	// мимо разбора только потому, что имя двоичного файла вынесено в константу.
	consts := fileStringConsts(file)

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "exec" {
			return true
		}
		var argv0 int
		switch sel.Sel.Name {
		case "Command":
			argv0 = 0
		case "CommandContext":
			argv0 = 1
		// `exec.LookPath("git")` под запрет НЕ подпадает: он ничего не
		// исполняет и окружения не наследует — искать двоичный файл законно
		// откуда угодно.
		default:
			return true
		}
		if len(call.Args) <= argv0 {
			return true
		}
		name, ok := stringValue(call.Args[argv0], consts)
		if !ok || !isGitBinary(name) {
			return true
		}
		calls = append(calls, gitCall{
			pos:  relPos(fset, root, call.Pos()),
			what: "exec." + sel.Sel.Name + "(" + strconv.Quote(name) + ", …)",
		})
		return true
	})

	// Второй вид: окружение вернули обратно поверх помощника.
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		leaks = append(leaks, scanEnvReassign(fset, fn, root)...)
	}
	return calls, leaks
}

// scanEnvReassign ловит `cmd.Env = <что-то с os.Environ()>` для переменной,
// полученной от gitenv в ТОЙ ЖЕ функции.
//
// Область намеренно узкая: файл вправе собирать и не-git команды со своим
// окружением, и запрет на `os.Environ()` вообще был бы ложным срабатыванием —
// а первый же ложный срабат гейт отключает.
func scanEnvReassign(fset *token.FileSet, fn *ast.FuncDecl, root string) []gitFinding {
	fromHelper := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			if i >= len(as.Lhs) || !isHelperCall(rhs) {
				continue
			}
			if id, ok := as.Lhs[i].(*ast.Ident); ok {
				fromHelper[id.Name] = true
			}
		}
		return true
	})
	if len(fromHelper) == 0 {
		return nil
	}

	var out []gitFinding
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Env" {
				continue
			}
			base, ok := sel.X.(*ast.Ident)
			if !ok || !fromHelper[base.Name] {
				continue
			}
			if i < len(as.Rhs) && containsOsEnviron(as.Rhs[i]) {
				out = append(out, gitFinding{
					pos:  relPos(fset, root, as.Pos()),
					kind: "окружение возвращено обратно",
					what: base.Name + ".Env = … os.Environ() … — дописывай к " + base.Name + ".Env",
				})
			}
		}
		return true
	})
	return out
}

func isHelperCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "gitenv" {
		return false
	}
	return sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext"
}

func containsOsEnviron(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Environ" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" {
			found = true
		}
		return true
	})
	return found
}

// fileStringConsts — строковые константы файла, объявленные литералом.
func fileStringConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if lit, ok := literalString(vs.Values[i]); ok {
					out[name.Name] = lit
				}
			}
		}
	}
	// Локальные константы функций — тем же признаком.
	ast.Inspect(file, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if lit, ok := literalString(vs.Values[i]); ok {
						out[name.Name] = lit
					}
				}
			}
		}
		return true
	})
	return out
}

func literalString(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func stringValue(e ast.Expr, consts map[string]string) (string, bool) {
	if s, ok := literalString(e); ok {
		return s, true
	}
	if id, ok := e.(*ast.Ident); ok {
		if s, ok := consts[id.Name]; ok {
			return s, true
		}
	}
	return "", false
}

// isGitBinary — «это git», а не «в имени есть git».
//
// Признак по роли, а не по подстроке: `git-lfs`, `gitleaks` и `github-cli`
// репозиторием прогона не распоряжаются и под запрет не подпадают.
func isGitBinary(name string) bool {
	base := name
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return base == "git" || base == "git.exe"
}

// TestHookAndHelperScrubTheSameVariables — два места об одном предмете обязаны
// сходиться.
//
// Перечень переменных объявлен дважды: в `pkg/gitenv` (для всего, что зовёт
// git из Go) и в `scripts/hooks/pre-push` (граница, обрывающая наследование до
// запуска проверок). Расхождение между ними тихое: каждая половина по отдельности
// выглядит исправной, а дыра открывается ровно на той переменной, которую
// добавили в одно место и забыли в другом.
//
// Сверка идёт в ОБЕ стороны — иначе половина запрета зеленела бы на пустом
// перечне.
func TestHookAndHelperScrubTheSameVariables(t *testing.T) {
	root := repoRoot(t)
	hookPath := filepath.Join(root, "scripts", "hooks", "pre-push")
	raw, err := os.ReadFile(hookPath) // #nosec G304 -- путь собран из корня дерева
	if err != nil {
		t.Fatalf("чтение %s: %v — граница, обрывающая наследование окружения, "+
			"в дереве не найдена", hookPath, err)
	}

	inHook := map[string]bool{}
	lines := strings.Split(string(raw), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "unset ") {
			continue
		}
		// Продолжение строки обратной косой — форма, в которой перечень и
		// записан; без её разбора половина имён была бы не видна.
		for {
			cont := strings.HasSuffix(line, `\`)
			for _, f := range strings.Fields(strings.TrimSuffix(line, `\`)) {
				if strings.HasPrefix(f, "GIT_") {
					inHook[f] = true
				}
			}
			if !cont || i+1 >= len(lines) {
				break
			}
			i++
			line = strings.TrimSpace(lines[i])
		}
	}

	if len(inHook) == 0 {
		t.Fatalf("в %s НЕТ ни одной снимаемой переменной GIT_*.\n"+
			"Либо граница снята — тогда `go test`, запущенный из хука, снова\n"+
			"унаследует GIT_DIR, — либо изменилась форма записи и эта сверка\n"+
			"перестала читать свой предмет. Оба исхода — находка, а не успех.", hookPath)
	}

	inHelper := map[string]bool{}
	for _, v := range gitenv.Vars() {
		inHelper[v] = true
	}

	var onlyHook, onlyHelper []string
	for v := range inHook {
		if !inHelper[v] {
			onlyHook = append(onlyHook, v)
		}
	}
	for v := range inHelper {
		if !inHook[v] {
			onlyHelper = append(onlyHelper, v)
		}
	}
	sort.Strings(onlyHook)
	sort.Strings(onlyHelper)

	t.Logf("сверено переменных: в хуке %d, у помощника %d", len(inHook), len(inHelper))

	if len(onlyHook) > 0 {
		t.Errorf("хук снимает, а gitenv.Vars() — нет: %s\n"+
			"Вызов git из Go унаследует их при любом запуске мимо хука.",
			strings.Join(onlyHook, ", "))
	}
	if len(onlyHelper) > 0 {
		t.Errorf("gitenv.Vars() снимает, а хук — нет: %s\n"+
			"Их унаследует всё, что хук запускает НЕ через этот пакет: скрипты\n"+
			"оболочки, python-пробы, вызовы make.",
			strings.Join(onlyHelper, ", "))
	}
}
