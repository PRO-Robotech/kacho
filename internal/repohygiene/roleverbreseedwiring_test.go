// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// roleverbreseedwiring_test.go — КАК досев дотягивается до единственного писателя
// проекции роли, и откуда его самого зовут.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТОГО НЕ ХВАТАЛО
//
// Приёмка `role-verb-projection-sole-writer.md` (§6) объявляет требование «досев
// зовёт писателя через порт, а не своим SQL» СЛЕДСТВИЕМ двух других: писатель
// один и лежит в `repo/`. Следствие неполно, и дыра наблюдалась вживую: писатель
// можно свести в ОДИН, положить его в `repo/` — и всё равно дотянуться до него из
// use-case ПРЯМЫМ ИМПОРТОМ пакета-адаптера. Гейт единственности при этом зелен:
// писатель действительно один, слой действительно `repo/`. Зелены и обе пробы
// сервиса: они зовут путь, а не разглядывают ребро импортов.
//
// Здесь закрываются ровно две оси, которых у того гейта нет:
//
//  1. слой use-case (`services/*/internal/apps/`) НЕ импортирует пакет, в котором
//     стоит единственный писатель. Писателя зовут через порт (`kachorepo.Writer`,
//     `shared.DoWithWriteTxVoid`) — это и есть раскладка `architecture.md`
//     §«Clean Architecture»: use-case объявляет порт, adapter его реализует;
//  2. ссылка на пересчёт проекции в ДЕРЕВЕ ровно одна, и стоит она в
//     композиционном корне. Спрятанный где угодно ещё вызов означает сразу
//     четыре вещи: его отказ приезжает обёрнутым в чужую ошибку и печатается
//     уровнем чужой полосы (свою держит `roleverbreseedbootlane_test.go`);
//     наблюдателя у него нет, поэтому счётчик недосчитывает; на старте пересчёт
//     идёт больше одного раза; и перепись второго прогона затирает первую.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА НАЗВАНА
//
// Ось 1 судит РЕБРО ИМПОРТОВ, а не вызов: use-case, добравшийся до писателя
// через третий пакет, который сам импортирует адаптер, гейтом не ловится.
// Такого пакета в дереве нет, и заводить его ради обхода пришлось бы намеренно.
//
// Ось 2 опознаёт ПРЕДМЕТ по экспортированному имени, несущему `RoleVerb`, среди
// функций пакета досева, а его СЛЕДЫ ищет по всему непроверочному дереву и в
// любой форме записи ссылки. Прежняя редакция искала неквалифицированный вызов и
// только в каталоге досева — то есть стерегла ровно тот каталог, в котором
// прятать вызов и не нужно: из соседнего пакета форма записи квалифицированная
// by construction. Ноль точек входа — ОТКАЗ, а не «нарушений нет»: без предмета
// у гейта нет и вердикта.
//
// Граница оси 2, названная честно: она судит ИМЯ, а не смысл. Пересчёт,
// переехавший под имя без `RoleVerb`, из предмета выпадет — и гейт замолчит,
// оставаясь зелёным (`testing.md` §«Гейт на класс», п. 9). Исход тогда один из
// двух: снять гейт вместе с предметом либо перевести на новый признак и
// доказать заново.
//
// Обе оси печатают объём осмотренного: «ноль находок» обязано быть отличимо от
// «ноль прочитанного».

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

	"github.com/PRO-Robotech/kacho/internal/gitenv"
)

// useCaseLayerMarker — признак слоя use-case в пути файла.
const useCaseLayerMarker = "/internal/apps/"

// roleVerbSeedPackageDir — каталог досева, чьи вызовы считает ось 2.
const roleVerbSeedPackageDir = "services/iam/internal/apps/kacho/seed"

// treeGoFiles — непроверочные файлы Go по ИНДЕКСУ git: единица счёта —
// отслеживаемый файл, поэтому неотслеживаемый мусор рабочей копии в вердикт не
// попадает, а пропавший из индекса файл не остаётся осмотренным.
func treeGoFiles(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}
	var files []string
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		files = append(files, rel)
	}
	return files
}

// importedPaths отдаёт пути импорта файла Go.
func importedPaths(filename, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		p, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// useCaseFileImports — файл слоя use-case и его пути импорта.
type useCaseFileImports struct {
	Rel     string
	Imports []string
}

// roleVerbWriterPackageOf — путь импорта пакета, если файл ПИШЕТ проекцию.
// Читатель таблицы пакетом-писателем не становится: это и есть законный близнец,
// на котором ось обязана молчать.
func roleVerbWriterPackageOf(rel, src string) (string, bool, error) {
	if !strings.Contains(src, roleVerbTable) {
		return "", false, nil
	}
	writes, _, err := roleVerbWritesIn(rel, src)
	if err != nil {
		return "", false, err
	}
	if len(writes) == 0 {
		return "", false, nil
	}
	return modulePathPrefix + filepath.ToSlash(filepath.Dir(rel)), true, nil
}

// isUseCaseLayer — лежит ли файл в слое use-case.
func isUseCaseLayer(rel string) bool {
	return strings.Contains("/"+rel, useCaseLayerMarker)
}

// useCaseWriterImportFindings — файлы use-case, импортирующие пакет писателя.
func useCaseWriterImportFindings(apps []useCaseFileImports, writerPkgs map[string]string) []string {
	var findings []string
	for _, a := range apps {
		for _, imp := range a.Imports {
			if src, isWriter := writerPkgs[imp]; isWriter {
				findings = append(findings, a.Rel+" импортирует "+imp+" (писатель — "+src+")")
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// TestRoleVerbWriterIsReachedFromUseCaseThroughThePort — слой use-case не
// импортирует пакет единственного писателя проекции роли.
func TestRoleVerbWriterIsReachedFromUseCaseThroughThePort(t *testing.T) {
	root := repoRoot(t)
	files := treeGoFiles(t, root)

	var (
		filesRead    int
		writerPkgs   = map[string]string{} // путь импорта → файл, где найден писатель
		useCaseFiles int
		apps         []useCaseFileImports
	)

	for _, rel := range files {
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		filesRead++
		body := string(b)
		pkg, isWriter, perr := roleVerbWriterPackageOf(rel, body)
		if perr != nil {
			t.Fatalf("разбор %s: %v — файл индекса не разобран, и его молчание "+
				"ничего не значит", rel, perr)
		}
		if isWriter {
			writerPkgs[pkg] = rel
		}
		if isUseCaseLayer(rel) {
			imps, ierr := importedPaths(rel, body)
			if ierr != nil {
				t.Fatalf("разбор импортов %s: %v", rel, ierr)
			}
			useCaseFiles++
			apps = append(apps, useCaseFileImports{Rel: rel, Imports: imps})
		}
	}

	pkgList := make([]string, 0, len(writerPkgs))
	for p := range writerPkgs {
		pkgList = append(pkgList, p)
	}
	sort.Strings(pkgList)

	t.Logf("осмотрено непроверочных файлов Go: %d; из них в слое use-case (%s): %d; "+
		"пакетов-писателей проекции роли: %d %v",
		filesRead, useCaseLayerMarker, useCaseFiles, len(pkgList), pkgList)

	if filesRead == 0 {
		t.Fatal("осмотрено ноль файлов — гейт не читал дерева, и его молчание ничего не значит")
	}
	if useCaseFiles == 0 {
		t.Fatalf("в дереве нет ни одного непроверочного файла слоя %s — предпосылка "+
			"гейта неверна: либо слой переименован, либо обход его не видит",
			useCaseLayerMarker)
	}
	if len(pkgList) == 0 {
		t.Fatalf("ни один непроверочный файл не пишет %s — предмета у гейта нет: "+
			"либо таблица переименована, либо писателя не осталось вовсе", roleVerbTable)
	}

	for _, f := range useCaseWriterImportFindings(apps, writerPkgs) {
		t.Errorf("%s\n"+
			"Слой use-case дотягивается до писателя проекции роли ПРЯМЫМ ИМПОРТОМ "+
			"пакета-адаптера. Писателя зовут через порт: `kachorepo.Writer` + "+
			"`shared.DoWithWriteTxVoid`, метод `RolesW().ReplaceRoleVerbs`. Иначе "+
			"use-case решает не только КОГДА и ДЛЯ КАКИХ ролей пересчитывать, но и "+
			"КАК писать, — а форму строки, отображение отказов и транзакционность "+
			"держит слой repo/. Гейт единственности этого не видит: писатель при "+
			"таком импорте по-прежнему один и по-прежнему в repo/.", f)
	}
}

// exportedRoleVerbSeedEntryPoints — экспортированные функции пакета досева, чьё
// имя несёт `RoleVerb`. Неэкспортированные помощники сюда не входят намеренно:
// предмет оси — точка входа, которую зовут ИЗВНЕ, а не внутренняя механика.
func exportedRoleVerbSeedEntryPoints(t *testing.T, root string, files []string) (map[string]bool, int) {
	t.Helper()
	entries := map[string]bool{}
	read := 0
	for _, rel := range files {
		if filepath.ToSlash(filepath.Dir(rel)) != roleVerbSeedPackageDir {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		read++
		names, perr := exportedRoleVerbEntryPointsIn(rel, string(b))
		if perr != nil {
			t.Fatalf("разбор %s: %v", rel, perr)
		}
		for _, n := range names {
			entries[n] = true
		}
	}
	return entries, read
}

// exportedRoleVerbEntryPointsIn — экспортированные функции файла, чьё имя несёт
// `RoleVerb`. Метод (функция с получателем) точкой входа пакета не является:
// пересчёт зовут как функцию пакета, а не через значение.
func exportedRoleVerbEntryPointsIn(filename, src string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		if fn.Name.IsExported() && strings.Contains(fn.Name.Name, roleVerbReseedMarker) {
			out = append(out, fn.Name.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// isBootCompositionRoot — ЕДИНСТВЕННОЕ место дерева, где ссылка на пересчёт
// законна. Определение «корня» берётся у соседнего гейта (`bootCompositionRoot`),
// а не выписывается второй раз: два места об одном предмете разошлись бы молча.
func isBootCompositionRoot(rel string) bool {
	return filepath.ToSlash(rel) == bootCompositionRoot
}

// roleVerbReseedRefsIn отдаёт ССЫЛКИ на точки входа пересчёта — в ЛЮБОЙ законной
// форме записи: неквалифицированный вызов внутри пакета досева,
// квалифицированный `<пакет>.<Имя>` из любого другого пакета, взятие функции
// значением. Имя объемлющей функции идёт в находку, чтобы она несла координату.
//
// Считаются ССЫЛКИ, а не вызовы, и это не педантизм: вызов прячется одной
// строкой (`run := ReseedSystemRoleVerbs`, затем `run(...)`) — в позиции вызова
// оказывается имя переменной, и распознаватель по вызовам молчит. Ссылка так не
// прячется: чтобы позвать функцию, её надо назвать.
//
// Объявление самой точки входа ссылкой НЕ является: иначе гейт краснел бы на
// всяком дереве, где пересчёт вообще существует, — то есть всегда.
func roleVerbReseedRefsIn(filename, src string, entries map[string]bool) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	type span struct {
		from, to token.Pos
		name     string
	}
	var spans []span
	declNames := make(map[*ast.Ident]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		declNames[fn.Name] = true
		if fn.Body != nil {
			spans = append(spans, span{from: fn.Body.Pos(), to: fn.Body.End(), name: fn.Name.Name})
		}
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok || !entries[ident.Name] || declNames[ident] {
			return true
		}
		owner := "<пакетный уровень>"
		for _, s := range spans {
			if ident.Pos() >= s.from && ident.End() <= s.to {
				owner = s.name
				break
			}
		}
		out = append(out, filename+"::"+owner+" → "+ident.Name)
		return true
	})
	return out, nil
}

// TestRoleVerbReseedHasOneReferenceInTheTreeAndItIsTheBootRoot — пересчёт
// проекции роли за старт происходит РОВНО ОДИН раз, и зовут его ИЗ КОМПОЗИЦИОННОГО
// КОРНЯ, где у его отказа есть собственная полоса.
//
// Прежняя редакция этой оси стерегла ОДИН КАТАЛОГ — пакет досева — и опознавала
// лишь неквалифицированный вызов. Обе границы обходятся, не желая того: позови
// пересчёт из соседнего пакета, и форма записи станет квалифицированной by
// construction, а файл — вне обхода. Спрятанный так вызов не давал ни красного,
// ни зелёного: гейт его не видел. Предмет расширен до ДЕРЕВА, а признак — до
// ссылки в любой форме.
//
// Почему «ровно один», а не «хотя бы один в корне»: два прогона за старт — это
// вдвое больше транзакций на полусотне системных ролей и перепись, затирающая
// первую. Оба прогона по отдельности выглядят исправно.
func TestRoleVerbReseedHasOneReferenceInTheTreeAndItIsTheBootRoot(t *testing.T) {
	root := repoRoot(t)
	files := treeGoFiles(t, root)

	entries, seedFilesRead := exportedRoleVerbSeedEntryPoints(t, root, files)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)

	var atRoot, elsewhere []string
	filesRead, filesParsed := 0, 0
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		filesRead++
		src := string(b)
		// Предфильтр ТОЧЕН, а не эвристичен: точка входа отбирается по наличию
		// `roleVerbReseedMarker` в имени, поэтому текст всякой ссылки на неё этот
		// маркер содержит. Пропущенный здесь файл ссылки не несёт by construction.
		if !strings.Contains(src, roleVerbReseedMarker) {
			continue
		}
		filesParsed++
		refs, rerr := roleVerbReseedRefsIn(rel, src, entries)
		if rerr != nil {
			t.Fatalf("разбор %s: %v", rel, rerr)
		}
		if isBootCompositionRoot(rel) {
			atRoot = append(atRoot, refs...)
		} else {
			elsewhere = append(elsewhere, refs...)
		}
	}
	sort.Strings(atRoot)
	sort.Strings(elsewhere)

	t.Logf("осмотрено непроверочных файлов Go дерева: %d; из них разобрано (несут `%s`): %d; "+
		"файлов пакета досева (%s) прочитано: %d; точек входа пересчёта: %d %v; "+
		"ссылок в композиционном корне (%s): %d; ссылок вне корня: %d",
		filesRead, roleVerbReseedMarker, filesParsed, roleVerbSeedPackageDir, seedFilesRead,
		len(names), names, bootCompositionRoot, len(atRoot), len(elsewhere))

	if filesRead == 0 {
		t.Fatalf("обход дерева не прочитал ни одного непроверочного файла Go — " +
			"предпосылка гейта неверна, и «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if seedFilesRead == 0 {
		t.Fatalf("в каталоге %s не прочитано ни одного непроверочного файла — "+
			"предпосылка гейта неверна: каталог переехал либо обход его не видит",
			roleVerbSeedPackageDir)
	}
	if len(names) == 0 {
		t.Fatalf("в пакете досева нет ни одной экспортированной функции с `%s` в имени — "+
			"предмета у гейта нет: пересчёт переименован либо снят", roleVerbReseedMarker)
	}
	if filesParsed == 0 {
		t.Fatalf("ни один файл дерева не несёт `%s` — предмет исчез из дерева, "+
			"и молчание гейта о нём ничего не говорит", roleVerbReseedMarker)
	}

	for _, f := range elsewhere {
		t.Errorf("%s\n"+
			"Пересчёт проекции роли зовётся ВНЕ композиционного корня. Своя полоса "+
			"отказа у него есть только там (%s) — значит здесь его отказ приезжает "+
			"вызывающему обёрнутым в ЧУЖУЮ ошибку и печатается уровнем чужой полосы, "+
			"а на старте пересчёт идёт БОЛЬШЕ ОДНОГО РАЗА: вдвое больше транзакций и "+
			"перепись, затирающая первую.", f, bootCompositionRoot)
	}
	switch len(atRoot) {
	case 1:
	case 0:
		t.Errorf("в композиционном корне (%s) ссылок на пересчёт проекции роли НЕТ.\n"+
			"Тогда на старте проекция не пересеивается вовсе: роль с одними селекторами "+
			"адресует объект и не разрешает на нём ничего, а вердикт по её выдаче "+
			"отказывает МОЛЧА.", bootCompositionRoot)
	default:
		t.Errorf("в композиционном корне (%s) ссылок на пересчёт проекции роли %d, "+
			"а обязана быть одна: %v\n"+
			"Пересчёт за старт идёт больше одного раза; перепись второго прогона "+
			"затирает первую, и оба прогона по отдельности выглядят исправно.",
			bootCompositionRoot, len(atRoot), atRoot)
	}
}
