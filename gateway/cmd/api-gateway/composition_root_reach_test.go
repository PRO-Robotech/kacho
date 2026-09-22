// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// composition_root_reach_test.go — ПРОВЯЗКА СУДИТСЯ ПОЗВАННОСТЬЮ, А НЕ
// ПРИСУТСТВИЕМ ТЕКСТА.
//
// # Зазор, который это закрывает
//
// Провязку композиционного корня утверждали чтением ТЕКСТА `main.go`: проба
// искала в нём строку вызова. Такой предикат ловит грубое СНЯТИЕ вызова и не
// ловит его ПЕРЕНОС: вынеси вызов в функцию, в которую `main()` не заходит, —
// и пакет, и линтер остаются зелёными (измерено на этой полосе: `ok … 2.725s`
// и `0 issues.`). Промежуточная форма — функция, никем не позванная и НЕ
// упомянутая пробой — ловится линтером `unused`; упомянутая пробой не ловится
// ничем.
//
// Предмет ШИРЕ одного стража: тем же приёмом держались хопы за ключами и к
// нашему авторитету отзыва, отказ во внутреннем маршруте, читатель журнала
// смены субъекта, страж проверяющего подпись и место печати строки посадки.
//
// # Чем судится теперь
//
// Разбором `package main` и обходом от `main()`. Возвращается не файл, а
// СИНТЕТИЧЕСКИЙ исходник, в котором лежат РОВНО те объявления, до которых
// `main()` дотягивается по графу ссылок. Пробы ищут в нём то же, что искали в
// файле, — но текст, до которого корень не доходит, в него не попадает, и
// перенос вызова в непозванную функцию краснит.
//
// Граф строится по ССЫЛКЕ, а не только по вызову: функция, переданная значением
// (`health.HTTPReadyz`, `newClientAddressOperator(cfg).ClientIP`), провязана так
// же настоящим образом, как позванная, и считать её недостижимой значило бы
// краснеть на законном коде. Обход идёт ТОЛЬКО по не-тестовым файлам: функция,
// упомянутая лишь пробой, до корня не дотягивается — и это ровно тот остаточный
// зазор, ради которого файл заведён.
//
// # Что читается ЦЕЛИКОМ и почему
//
// Утверждения ОТРИЦАТЕЛЬНЫЕ («снятого в корне больше нет») читают файл целиком
// через `compositionRootVerbatim`. Сузить корпус отрицанию значит ослабить его
// молча: вернувшееся объявление в непозванной функции — тоже возвращение, и
// отрицание обязано его видеть.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// compositionRootFile — координата корня. Объявлена ОДИН раз: вторая копия пути
// разъехалась бы с первой молча.
const compositionRootFile = "main.go"

// compositionRootLabel — имя, под которым достижимый исходник подаётся разбору.
// Оно НЕ есть путь: разбирается синтетический текст, а не файл, и называть его
// именем файла значило бы снова обещать, что судится файл.
const compositionRootLabel = "composition-root.reached.go"

// rootEntryPoint — единственная точка входа, от которой ведётся обход.
const rootEntryPoint = "main"

// funcDecl — объявление вместе с координатой, по которой оно упорядочивается.
type funcDecl struct {
	name string
	file string
	pos  token.Pos
	decl *ast.FuncDecl
}

// parsePackageRoot разбирает НЕ-тестовые файлы пакета из каталога dir.
func parsePackageRoot(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("состав пакета композиционного корня (%s): %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	// Файл, объявляющий точку входа, разбирается ПЕРВЫМ: порядок в
	// синтетическом исходнике задаёт смысл утверждениям о порядке, и тело
	// `main()` обязано стоять раньше любого объявления, чьё имя в нём же
	// встречается.
	sort.SliceStable(names, func(i, j int) bool {
		return names[i] == compositionRootFile && names[j] != compositionRootFile
	})
	for _, n := range names {
		f, perr := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ParseComments)
		if perr != nil {
			t.Fatalf("разбор %s: %v", n, perr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("в %s не разобрано ни одного не-тестового файла — обходить нечего, "+
			"и молчание пробы не является утверждением о корне", dir)
	}
	return fset, files
}

// declaredFuncs собирает объявления пакета. Метод кладётся под своим ИМЕНЕМ:
// обход намеренно огрубляет — лишняя достижимость даёт молчание там, где
// свойство и так держится, а недостающая краснела бы на законном коде.
func declaredFuncs(fset *token.FileSet, files []*ast.File) map[string][]funcDecl {
	out := map[string][]funcDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil {
				continue
			}
			out[fd.Name.Name] = append(out[fd.Name.Name], funcDecl{
				name: fd.Name.Name,
				file: fset.Position(fd.Pos()).Filename,
				pos:  fd.Pos(),
				decl: fd,
			})
		}
	}
	return out
}

// referencedNames — все имена, на которые СОССЫЛАЕТСЯ тело объявления.
func referencedNames(fd *ast.FuncDecl) map[string]bool {
	seen := map[string]bool{}
	if fd.Body == nil {
		return seen
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			seen[v.Name] = true
		case *ast.SelectorExpr:
			if v.Sel != nil {
				seen[v.Sel.Name] = true
			}
		}
		return true
	})
	return seen
}

// reachableFromEntry — замыкание достижимости от точки входа.
//
// Возвращает множество достижимых ИМЁН и признак того, что сама точка входа в
// пакете есть. Второе — предпосылка: обход, не нашедший входа, обязан сказать
// это словом, а не вернуть пустое множество, неотличимое от «ничего не
// провязано».
func reachableFromEntry(funcs map[string][]funcDecl, entry string) (map[string]bool, bool) {
	if _, ok := funcs[entry]; !ok {
		return map[string]bool{}, false
	}
	reached := map[string]bool{entry: true}
	queue := []string{entry}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, d := range funcs[cur] {
			for name := range referencedNames(d.decl) {
				if !reached[name] {
					if _, declared := funcs[name]; declared {
						reached[name] = true
						queue = append(queue, name)
					}
				}
			}
		}
	}
	return reached, true
}

// renderReached печатает достижимые объявления одним разбираемым исходником.
//
// Точка входа идёт ПЕРВОЙ: утверждения о ПОРЯДКЕ сравнивают смещения внутри
// `main()`, и объявление, стоящее перед ним, дало бы более раннее совпадение
// своим собственным именем.
func renderReached(fset *token.FileSet, funcs map[string][]funcDecl, reached map[string]bool, entry string) string {
	var decls []funcDecl
	for name, ds := range funcs {
		if !reached[name] {
			continue
		}
		decls = append(decls, ds...)
	}
	sort.Slice(decls, func(i, j int) bool {
		if (decls[i].name == entry) != (decls[j].name == entry) {
			return decls[i].name == entry
		}
		if decls[i].file != decls[j].file {
			return decls[i].file < decls[j].file
		}
		return decls[i].pos < decls[j].pos
	})
	var buf bytes.Buffer
	buf.WriteString("package main\n\n")
	for _, d := range decls {
		if err := printer.Fprint(&buf, fset, d.decl); err != nil {
			// Печать разобранного узла своим же набором позиций не отказывает;
			// если отказала — исходник вернулся бы усечённым, и утверждение о
			// провязке было бы сказано ни о чём.
			panic("печать достижимого объявления композиционного корня: " + err.Error())
		}
		buf.WriteString("\n\n")
	}
	return buf.String()
}

// compositionRoot — исходник композиционного корня, ДОСТИЖИМЫЙ от `main()`.
//
// Имя сохранено от прежнего помощника, читавшего файл целиком: сайтов
// семнадцать, и смысл у них тот же — «корень это делает». Изменилось то, что
// теперь значит «корень»: не текст файла, а код, до которого `main()`
// дотягивается.
func compositionRoot(t *testing.T) string {
	t.Helper()
	fset, files := parsePackageRoot(t, ".")
	funcs := declaredFuncs(fset, files)
	reached, hasEntry := reachableFromEntry(funcs, rootEntryPoint)
	if !hasEntry {
		t.Fatalf("в пакете композиционного корня нет `func %s` — предпосылка обхода "+
			"исчезла, и молчание пробы сказано ни о чём", rootEntryPoint)
	}
	return renderReached(fset, funcs, reached, rootEntryPoint)
}

// compositionRootVerbatim — файл корня ЦЕЛИКОМ.
//
// Только для ОТРИЦАТЕЛЬНЫХ утверждений: «снятого больше нет». Сужение корпуса
// отрицанию — ослабление молчанием, и возвращённое объявление в непозванной
// функции обязано быть найдено.
func compositionRootVerbatim(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(compositionRootFile)
	if err != nil {
		t.Fatalf("чтение композиционного корня: %v", err)
	}
	return string(b)
}

// ─── ПЕРЕПИСЬ И ПРЕДПОСЫЛКА ────────────────────────────────────────────────

// TestCompositionRootReach_Census печатает объём осмотренного.
//
// «Находок ноль» обязано быть отличимо от «прочитано ноль»: обход, не нашедший
// ни одного объявления или дотянувшийся до всех до единого, о провязке не
// говорит ничего.
func TestCompositionRootReach_Census(t *testing.T) {
	t.Parallel()
	fset, files := parsePackageRoot(t, ".")
	funcs := declaredFuncs(fset, files)
	reached, hasEntry := reachableFromEntry(funcs, rootEntryPoint)
	if !hasEntry {
		t.Fatalf("точки входа `func %s` в пакете нет — предпосылка обхода сломана", rootEntryPoint)
	}
	total := 0
	for _, ds := range funcs {
		total += len(ds)
	}
	reachedCount := 0
	var unreached []string
	for name, ds := range funcs {
		if reached[name] {
			reachedCount += len(ds)
			continue
		}
		unreached = append(unreached, name)
	}
	sort.Strings(unreached)
	t.Logf("перепись: файлов пакета %d · объявлений %d · достижимо от %s() %d · недостижимо %d %v",
		len(files), total, rootEntryPoint, reachedCount, len(unreached), unreached)
	if total == 0 {
		t.Fatal("объявлений в пакете ноль — обход прочитал не то")
	}
	if reachedCount == 0 {
		t.Fatal("от точки входа не достижимо НИ ОДНО объявление — граф ссылок построен не по тому")
	}
}

// ─── ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ, НА СИНТЕТИКЕ ──────────────────────────────────
//
// Синтетика, а не живое дерево: доказательство, опирающееся на живой корень,
// исчезло бы вместе с починкой — то есть ровно тогда, когда предикат достиг
// своей цели.

// writeSyntheticRoot кладёт исходник пакета во временный каталог.
func writeSyntheticRoot(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, compositionRootFile), []byte(src), 0o600); err != nil {
		t.Fatalf("синтетический корень не записан: %v", err)
	}
	return dir
}

// reachedSourceOf — то же тело, что исполняется на дереве, над синтетикой.
func reachedSourceOf(t *testing.T, dir string) (string, bool) {
	t.Helper()
	fset, files := parsePackageRoot(t, dir)
	funcs := declaredFuncs(fset, files)
	reached, hasEntry := reachableFromEntry(funcs, rootEntryPoint)
	if !hasEntry {
		return "", false
	}
	return renderReached(fset, funcs, reached, rootEntryPoint), true
}

// TestCompositionRootReachInjection_MovedOutOfReachIsNotSeen — ДЕФЕКТ КРАСНИТ.
//
// Вызов остаётся в файле корня дословно, но живёт в функции, в которую `main()`
// не заходит, и упомянут только пробой — та самая форма, что оставляла зелёными
// и пакет, и линтер.
func TestCompositionRootReachInjection_MovedOutOfReachIsNotSeen(t *testing.T) {
	t.Parallel()
	const moved = `package main

func main() {
	wireTheRest()
}

func wireTheRest() {
}

func deadPostureGuard() {
	if err := validateIdentityPosture(identityLane); err != nil {
		log.Fatalf("identity posture startup-validation: %v", err)
	}
}
`
	src, ok := reachedSourceOf(t, writeSyntheticRoot(t, moved))
	if !ok {
		t.Fatal("точка входа в синтетике не найдена — инъекция построена не о том")
	}
	if strings.Contains(src, "validateIdentityPosture(identityLane)") {
		t.Fatalf("перенос вызова в непозванную функцию остался невидимым: достижимый "+
			"исходник всё ещё содержит вызов.\n%s", src)
	}
	if !strings.Contains(src, "wireTheRest()") {
		t.Fatalf("обход потерял ПОЗВАННУЮ функцию — инъекция уронила не только свой "+
			"предмет, и красное пришло бы от соседа.\n%s", src)
	}
}

// TestCompositionRootReachInjection_RenamedButStillCalledIsSeen — ЗАКОННЫЙ
// БЛИЗНЕЦ МОЛЧИТ.
//
// Против дефекта меняется РОВНО ОДИН факт: `main()` заходит в функцию, несущую
// вызов. Переименование при сохранённой позванности — законный рефакторинг, и
// краснеть на нём значило бы судить форму, а не провязку.
func TestCompositionRootReachInjection_RenamedButStillCalledIsSeen(t *testing.T) {
	t.Parallel()
	const renamed = `package main

func main() {
	wireTheRest()
	postureGuardRenamed()
}

func wireTheRest() {
}

func postureGuardRenamed() {
	if err := validateIdentityPosture(identityLane); err != nil {
		log.Fatalf("identity posture startup-validation: %v", err)
	}
}
`
	src, ok := reachedSourceOf(t, writeSyntheticRoot(t, renamed))
	if !ok {
		t.Fatal("точка входа в синтетике не найдена — близнец построен не о том")
	}
	if !strings.Contains(src, "validateIdentityPosture(identityLane)") {
		t.Fatalf("законный рефакторинг покраснел: вызов позван корнем через "+
			"переименованную функцию, а достижимый исходник его не содержит.\n%s", src)
	}
}

// TestCompositionRootReachInjection_ReferenceByValueIsReached — функция,
// переданная ЗНАЧЕНИЕМ, достижима.
//
// Без этой половины обход краснел бы на законной провязке обработчика,
// подаваемого мультиплексору по имени.
func TestCompositionRootReachInjection_ReferenceByValueIsReached(t *testing.T) {
	t.Parallel()
	const byValue = `package main

func main() {
	mux.HandleFunc("/healthz", healthz)
}

func healthz() {
	wiredDeeper()
}

func wiredDeeper() {
}
`
	src, ok := reachedSourceOf(t, writeSyntheticRoot(t, byValue))
	if !ok {
		t.Fatal("точка входа в синтетике не найдена")
	}
	for _, want := range []string{"func healthz()", "func wiredDeeper()"} {
		if !strings.Contains(src, want) {
			t.Fatalf("ссылка значением потеряна обходом: нет %q.\n%s", want, src)
		}
	}
}

// TestCompositionRootReachInjection_PremiseFailsWithoutAnEntryPoint —
// ПРЕДПОСЫЛКА ОБХОДА.
//
// Пакет без точки входа обязан быть распознан как «обходить не от чего», а не
// отдать пустой исходник: пустой неотличим от «ничего не провязано», и на нём
// зазеленело бы любое отрицание.
func TestCompositionRootReachInjection_PremiseFailsWithoutAnEntryPoint(t *testing.T) {
	t.Parallel()
	const noEntry = `package main

func wireTheRest() {
}
`
	fset, files := parsePackageRoot(t, writeSyntheticRoot(t, noEntry))
	_, hasEntry := reachableFromEntry(declaredFuncs(fset, files), rootEntryPoint)
	if hasEntry {
		t.Fatal("обход объявил точку входа найденной там, где её нет — предпосылка " +
			"не проверяется, и «ничего не достижимо» прошло бы за вердикт")
	}
}

// ─── КЛАСС ОСТАЁТСЯ ЗАКРЫТЫМ ───────────────────────────────────────────────

// TestCompositionRootIsReadFromDiskInExactlyOnePlace — координата корня одна.
//
// Пока файл корня можно взять с диска из любой пробы, класс возвращается одной
// законно выглядящей строкой: новая проба читает или разбирает ФАЙЛ и снова
// судит присутствие вместо позванности.
//
// Гейт судит ИСПОЛНЯЕМУЮ форму, а не строку: находкой является вызов
// `os.ReadFile("main.go")` либо `parser.ParseFile(_, "main.go", nil, _)` —
// разбор с ПУСТЫМ исходником, то есть чтение с диска. Тот же литерал в роли
// ЯРЛЫКА синтетики (третий аргумент непуст) находкой не является: это
// самопроверка предиката, и краснеть на ней значило бы запрещать инъекцию.
func TestCompositionRootIsReadFromDiskInExactlyOnePlace(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("состав пакета: %v", err)
	}
	fset := token.NewFileSet()
	scanned, labelUses := 0, 0
	var offenders []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, "_test.go") || n == "composition_root_reach_test.go" {
			continue
		}
		f, perr := parser.ParseFile(fset, n, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("разбор пробы %s: %v", n, perr)
		}
		scanned++
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, isIdent := sel.X.(*ast.Ident)
			if !isIdent {
				return true
			}
			switch {
			case pkg.Name == "os" && sel.Sel.Name == "ReadFile" && len(call.Args) == 1 &&
				isRootPathLiteral(call.Args[0]):
				offenders = append(offenders, n+":"+strconv.Itoa(fset.Position(call.Pos()).Line)+
					" (чтение файла корня с диска)")
			case pkg.Name == "parser" && sel.Sel.Name == "ParseFile" && len(call.Args) == 4 &&
				isRootPathLiteral(call.Args[1]):
				if isNilIdent(call.Args[2]) {
					offenders = append(offenders, n+":"+strconv.Itoa(fset.Position(call.Pos()).Line)+
						" (разбор файла корня с диска)")
					return true
				}
				labelUses++
			}
			return true
		})
	}
	t.Logf("перепись: прочитано проб пакета %d · берут корень с диска мимо помощника %d · "+
		"используют имя корня ЯРЛЫКОМ синтетики %d",
		scanned, len(offenders), labelUses)
	if scanned == 0 {
		t.Fatal("гейт не прочитал ни одной пробы пакета — предпосылка обхода сломана")
	}
	for _, o := range offenders {
		t.Errorf("%s: провязку снова судит присутствие текста, а не позванность. "+
			"Возьми `compositionRoot(t)` (достижимое от main) либо "+
			"`compositionRootVerbatim(t)` (только для отрицаний)", o)
	}
}

// isRootPathLiteral — аргумент есть строковый литерал с координатой корня.
func isRootPathLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `"`+compositionRootFile+`"`
}

// isNilIdent — аргумент есть `nil`, то есть «исходник взять с диска».
func isNilIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

// TestCompositionRootDiskGateInjection_LabelIsNotAFinding — ЗАКОННЫЙ БЛИЗНЕЦ
// МОЛЧИТ, а ЧТЕНИЕ С ДИСКА — находка.
//
// Против находки синтетика меняет РОВНО ОДИН факт: третий аргумент разбора
// непуст. Без этой пары гейт был бы неотличим от запрета самого имени.
func TestCompositionRootDiskGateInjection_LabelIsNotAFinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		expr    string
		finding bool
	}{
		{"разбор с диска", `parser.ParseFile(fset, "main.go", nil, 0)`, true},
		{"чтение с диска", `os.ReadFile("main.go")`, true},
		{"ярлык синтетики", `parser.ParseFile(fset, "main.go", src, 0)`, false},
		{"достижимое", `parser.ParseFile(fset, compositionRootLabel, compositionRoot(t), 0)`, false},
	}
	for _, c := range cases {
		src := "package main\n\nfunc probe() {\n\t_, _ = " + c.expr + "\n}\n"
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, "synthetic_test.go", src, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("%s: синтетика не разбирается: %v", c.name, perr)
		}
		found := false
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, isIdent := sel.X.(*ast.Ident)
			if !isIdent {
				return true
			}
			if pkg.Name == "os" && sel.Sel.Name == "ReadFile" && len(call.Args) == 1 &&
				isRootPathLiteral(call.Args[0]) {
				found = true
			}
			if pkg.Name == "parser" && sel.Sel.Name == "ParseFile" && len(call.Args) == 4 &&
				isRootPathLiteral(call.Args[1]) && isNilIdent(call.Args[2]) {
				found = true
			}
			return true
		})
		if found != c.finding {
			t.Errorf("%s: распознано как находка=%v, ожидалось %v (%s)", c.name, found, c.finding, c.expr)
		}
	}
}
