// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// listpaginationorder_test.go — гейт против пустой страницы, отданной раньше,
// чем проверен формат пагинации.
//
// Предмет. Списочный метод сначала решает, кто спрашивает, и при неопознанном
// или неуправомоченном вызывающем возвращает пустую страницу БЕЗ ошибки. Если
// формат `page_token`/`page_size` к этому моменту не проверен, то мусорный
// курсор и размер страницы вне допустимого диапазона получают `200 []` вместо
// `400 InvalidArgument` — и ответ на один и тот же некорректный ввод зависит от
// того, что вызывающему выдано. Проверка формата не является решением о
// доступе, поэтому её место — до замыкания, а не после.
//
// Почему гейт, а не разовая правка. Свойство не видно в диффе: замыкание и
// чтение страницы обычно пишутся в разных строках и в разное время, а
// «валидирует репозиторий» верно ровно для того пути, который до репозитория
// доходит. Гейт формулирует требование локально — функция, отдающая пустую
// страницу по личности вызывающего, обязана САМА проверить формат первой.
// Тогда следующее такое место краснеет в момент появления, а не в момент, когда
// кто-то снова пройдёт по дереву руками.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: слово
// «ValidatePagination» в комментарии, объясняющем эту же защиту, текстовый поиск
// принял бы за саму защиту — ровно тот класс, который гейт и ловит.
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
)

// identityPredicateVocabulary — формы, которыми в этом дереве пишется «я не
// знаю / не признаю вызывающего».
//
// Словарь — это ОБЪЁМ гейта, а не список исключений: форма, которой здесь нет,
// гейтом не покрыта. Поэтому у каждой записи проверяется предмет
// (TestIdentityPredicateVocabularyHasSubject): запись, которой больше нечего
// распознавать, — находка, потому что она создаёт впечатление покрытия, которого
// нет.
var identityPredicateVocabulary = map[string]string{
	"subject-ident-empty": "" +
		"`<что-то>subject<что-то> == \"\"` — форма vpc: identity не извлечён из " +
		"контекста при включенном фильтре видимости.",
	"is-anonymous-call": "" +
		"вызов `IsAnonymous(...)` — форма iam: принципал отсутствует либо " +
		"опознан как неаутентифицированный.",
}

// paginationValidatorNames — имена функций, вызов которых считается проверкой
// формата пагинации.
//
// Совпадение по ИМЕНИ, а не по пакету: у сервисов свои кодеки курсора (vpc —
// `<unixnano>:<id>`, iam — `<RFC3339Nano>|<id>`, nlb — разделитель \x00), и
// единого пакета для них нет by construction. Проверяется предмет каждого имени
// (TestPaginationValidatorNamesHaveSubject).
var paginationValidatorNames = map[string]string{
	"ValidatePagination":     "общая проверка (page_token + page_size) в use-case-слое vpc/nlb",
	"ValidateListPagination": "то же имя в compute",
	"ValidatePageToken":      "проверка только курсора (iam shared)",
	"DecodePageToken":        "разбор курсора: неудача даёт InvalidArgument — сам по себе является проверкой",
	"PageSize":               "corevalidate.PageSize — отвергает значение вне [0..1000], не зажимает",
}

// listPaginationScanRoots — где ищем.
var listPaginationScanRoots = []string{"services", "gateway", "pkg"}

// TestEmptyPageNeverPrecedesPaginationValidation — ни одна функция формы
// `([]T, string, error)` не отдаёт пустую страницу по личности вызывающего
// раньше, чем проверит формат пагинации.
//
// Что делать, если гейт сработал, — три исхода, четвёртого нет:
//
//  1. это списочный путь -> позвать проверку формата ПЕРВЫМ стейтментом
//     функции (форму брать из соседнего сервиса, не изобретать: кодек курсора
//     обязан остаться один на сервис);
//  2. функция не списочная, а совпала формой возврата -> уточнить
//     распознавание ниже (сузить триггер), а не заводить список исключений:
//     списка исключений у этого гейта нет намеренно;
//  3. замыкание не про личность вызывающего (например, порт не провязан) ->
//     то же самое, уточнить распознавание предиката.
//
// Проверено инъекцией в обе стороны на синтетических исходниках
// (TestGateRedOnInjectedDefect / TestGateSilentOnLawfulSameShape): возврат
// дефекта красит гейт и печатает координату, законная конструкция той же формы
// его не задевает.
func TestEmptyPageNeverPrecedesPaginationValidation(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	scannedFiles := 0
	listShapedFuncs := 0
	shortCircuits := 0

	forEachProductionGoFileForPagination(t, root, func(rel string, body []byte) {
		scannedFiles++
		res := analyzeListPaginationOrder(t, rel, body)
		listShapedFuncs += res.listShaped
		shortCircuits += res.shortCircuits
		for _, h := range res.hits {
			hits = append(hits, rel+":"+strconv.Itoa(h.line)+" ("+h.fn+")")
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного» И от «ноль
	// распознанного»: сломанный обход и сгнивший словарь дают одинаково зелёный
	// гейт, если не утверждать объём осмотренного.
	if scannedFiles == 0 {
		t.Fatalf("гейт не прочитал ни одного файла в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", listPaginationScanRoots)
	}
	if listShapedFuncs == 0 {
		t.Fatalf("гейт не нашёл ни одной функции формы ([]T, string, error) в %d прод-файлах — "+
			"распознавание списочной формы сломано, молчание ничего не доказывает", scannedFiles)
	}
	// Нижняя граница по замыканиям: словарь предикатов — это объём гейта, и его
	// сужение (переименовали `subjectID`, сменили имя `IsAnonymous`) уводит места
	// из-под наблюдения молча. Граница ловит именно усадку, а не рост.
	const minShortCircuitsCovered = 13
	if shortCircuits < minShortCircuitsCovered {
		t.Errorf("гейт распознал %d замыканий по личности вызывающего, ожидалось не меньше %d: "+
			"словарь identityPredicateVocabulary перестал покрывать формы, которыми это пишется "+
			"в дереве — места ушли из-под наблюдения молча. Не понижай границу, не проверив, "+
			"куда делись места.", shortCircuits, minShortCircuitsCovered)
	}
	t.Logf("осмотрено прод-файлов: %d; функций формы ([]T, string, error): %d; "+
		"замыканий по личности вызывающего: %d", scannedFiles, listShapedFuncs, shortCircuits)

	if len(hits) > 0 {
		sort.Strings(hits)
		t.Errorf("найдено %d мест, где пустая страница отдаётся раньше проверки формата пагинации:\n  %s\n\n"+
			"Следствие: мусорный page_token и page_size вне диапазона получают 200 [] вместо "+
			"400 InvalidArgument, и ответ на один и тот же некорректный ввод зависит от выданных "+
			"вызывающему прав.\n\nИсход: позвать проверку формата первым стейтментом функции "+
			"(имена — в paginationValidatorNames). Списка исключений у гейта нет намеренно.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestIdentityPredicateVocabularyHasSubject — словарь обязан истекать сам.
//
// Форма, которой в дереве больше нет, создаёт впечатление покрытия, которого
// нет: следующий читатель увидит запись и решит, что этот случай гейт ловит.
func TestIdentityPredicateVocabularyHasSubject(t *testing.T) {
	root := repoRoot(t)

	seen := map[string]int{}
	forEachProductionGoFileForPagination(t, root, func(rel string, body []byte) {
		for kind, n := range countIdentityPredicateForms(t, rel, body) {
			seen[kind] += n
		}
	})

	for kind, why := range identityPredicateVocabulary {
		if strings.TrimSpace(why) == "" {
			t.Errorf("запись словаря %q без обоснования: она обязана называть форму и сервис, "+
				"иначе неотличима от догадки", kind)
		}
		if seen[kind] == 0 {
			t.Errorf("запись словаря %q больше нечего распознавать: в прод-дереве нет ни одного "+
				"вхождения этой формы. Удали запись — иначе она создаёт впечатление покрытия, "+
				"которого нет.", kind)
		}
	}
	t.Logf("вхождений форм словаря: %v", seen)
}

// TestPaginationValidatorNamesHaveSubject — предпосылка запрета: то, чего гейт
// требует, обязано существовать.
//
// Гейт переводит места на вызов проверки формата. Если ни одного такого имени в
// дереве нет, требование невыполнимо, а гейт продолжит требовать своё.
func TestPaginationValidatorNamesHaveSubject(t *testing.T) {
	root := repoRoot(t)

	seen := map[string]int{}
	forEachProductionGoFileForPagination(t, root, func(rel string, body []byte) {
		for name := range paginationValidatorNames {
			seen[name] += countCallsByName(t, rel, body, name)
		}
	})

	for name, why := range paginationValidatorNames {
		if strings.TrimSpace(why) == "" {
			t.Errorf("имя %q без обоснования", name)
		}
		if seen[name] == 0 {
			t.Errorf("имя %q не вызывается нигде в прод-дереве: гейту некуда переводить места "+
				"этой формой — либо имя устарело, либо проверку удалили. Пересмотри запрет, "+
				"а не молчи.", name)
		}
	}
	t.Logf("вызовов проверок формата: %v", seen)
}

// ---- инъекция в обе стороны ----
//
// Гейт без этой пары ловит форму, а не существо: молчание на законной
// конструкции той же формы надо доказать так же, как срабатывание на дефекте.
// Источники синтетические — исход не зависит от состояния дерева, поэтому пара
// остаётся доказательной и после того, как дерево починено.

// TestGateRedOnInjectedDefect — возвращённый дефект краснит гейт И называет
// координату.
func TestGateRedOnInjectedDefect(t *testing.T) {
	const src = `package x

func (u *uc) Execute(ctx ctxT, subjectID string, f filterT, p pageT) ([]row, string, error) {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, "", err
	}
	if subjectID == "" && u.filter != nil {
		return nil, "", nil
	}
	return rd.List(ctx, f, p)
}
`
	got := analyzeListPaginationOrder(t, "injected.go", []byte(src))

	if got.listShaped != 1 {
		t.Fatalf("списочная форма не распознана: listShaped=%d", got.listShaped)
	}
	if got.shortCircuits != 1 {
		t.Fatalf("замыкание по личности не распознано: shortCircuits=%d", got.shortCircuits)
	}
	if len(got.hits) != 1 {
		t.Fatalf("дефект не найден: hits=%v", got.hits)
	}
	if got.hits[0].line != 8 {
		t.Errorf("гейт обязан назвать координату: ожидалась строка 8, получена %d", got.hits[0].line)
	}
	if got.hits[0].fn != "Execute" {
		t.Errorf("гейт обязан назвать функцию: ожидался Execute, получен %q", got.hits[0].fn)
	}
}

// TestGateSilentOnLawfulSameShape — законные конструкции ТОЙ ЖЕ формы гейт не
// задевает.
//
// Три законные формы, все три встречаются в дереве: проверка первым
// стейтментом (vpc/iam после этой правки), проверка внутри самого замыкания
// перед возвратом (pkg/operations.ListOwned), и замыкание по личности в
// НЕсписочной функции (iam user.Get / WhoAmI) — последнее не является предметом
// запрета вовсе.
func TestGateSilentOnLawfulSameShape(t *testing.T) {
	lawful := map[string]string{
		"проверка первым стейтментом": `package x

func (u *uc) Execute(ctx ctxT, subjectID string, f filterT, p pageT) ([]row, string, error) {
	if err := ValidatePagination(p.PageToken, p.PageSize); err != nil {
		return nil, "", err
	}
	if subjectID == "" && u.filter != nil {
		return nil, "", nil
	}
	return u.repo.List(ctx, f, p)
}
`,
		"проверка внутри самого замыкания перед возвратом": `package x

func (r *repo) ListOwned(ctx ctxT, f filterT, owner ownerT) ([]row, string, error) {
	if owner.IsAnonymous() {
		if err := ValidateListPagination(f); err != nil {
			return nil, "", err
		}
		return nil, "", nil
	}
	return r.listWithOwner(ctx, f, &owner)
}
`,
		"замыкание по личности в несписочной функции": `package x

func (u *uc) Get(ctx ctxT, id string) (row, error) {
	if IsAnonymous(ctx) {
		return row{}, errNotFound
	}
	return u.repo.Get(ctx, id)
}
`,
		"пустая страница по непровязанному порту, а не по личности": `package x

func (u *uc) Execute(ctx ctxT, id string, p pageT) ([]row, string, error) {
	if u.subnetReader == nil {
		return nil, "", nil
	}
	return u.subnetReader.List(ctx, id, p)
}
`,
	}

	for name, src := range lawful {
		t.Run(name, func(t *testing.T) {
			got := analyzeListPaginationOrder(t, "lawful.go", []byte(src))
			if len(got.hits) != 0 {
				t.Fatalf("гейт сработал на законной конструкции — он ловит форму, а не существо: %v", got.hits)
			}
		})
	}
}

// TestGateIgnoresValidationHiddenInAnotherBranch — проверка, стоящая ВЫШЕ по
// тексту, но на чужой ветке, зачётом не является.
//
// Без этого «на пути» выродилось бы в «где-то раньше в файле», и гейт зеленел бы
// на коде, где проверка не исполняется.
func TestGateIgnoresValidationHiddenInAnotherBranch(t *testing.T) {
	const src = `package x

func (u *uc) Execute(ctx ctxT, subjectID string, f filterT, p pageT) ([]row, string, error) {
	if f.Verbose {
		if err := ValidatePagination(p.PageToken, p.PageSize); err != nil {
			return nil, "", err
		}
	}
	if subjectID == "" {
		return nil, "", nil
	}
	return u.repo.List(ctx, f, p)
}
`
	got := analyzeListPaginationOrder(t, "hidden.go", []byte(src))
	if len(got.hits) != 1 {
		t.Fatalf("проверка на чужой ветке засчитана как исполненная: hits=%v", got.hits)
	}
}

// ---- анализ ----

type paginationHit struct {
	line int
	fn   string
}

type paginationScan struct {
	listShaped    int
	shortCircuits int
	hits          []paginationHit
}

// analyzeListPaginationOrder разбирает один файл.
//
// Триггер узкий и намеренно локальный:
//
//	функция с результатами ровно ([]T, string, error)   — списочная форма
//	  содержит `if <предикат личности> { … return nil, "", nil … }`
//	  и на пути к этому возврату формат пагинации не проверен
//
// Возврат `nil, "", nil` — это «пустая страница, ошибки нет»: ровно тот
// наблюдаемый исход, который подменяет 400.
//
// «На пути» — БЕЗУСЛОВНО исполняемый вызов, а не просто вызов, стоящий выше по
// тексту: обход спускается по блокам и переносит вниз только те проверки,
// которые исполняются на любой ветке (собственные стейтменты блока и Init
// вложенных `if`). Проверка, спрятанная в чужой ветке, не засчитывается. Зато
// проверка ВНУТРИ самого замыкания, перед его `return`, засчитывается — путь к
// пустой странице через неё проходит (так написан pkg/operations.ListOwned, и
// это законная форма, а не дефект).
//
// Известная граница (floor, не ceiling): вызов проверки, вынесенный в
// собственный метод сервиса, гейт не видит — он ловит форму, которой это
// пишется во всём дереве, и не притворяется доказательством отсутствия.
func analyzeListPaginationOrder(t *testing.T, rel string, body []byte) paginationScan {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}

	var out paginationScan
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !hasListResultShape(fn.Type) {
			continue
		}
		out.listShaped++
		scanStmtsForShortCircuits(fset, fn.Name.Name, fn.Body.List, nil, &out)
	}
	return out
}

// scanStmtsForShortCircuits обходит список стейтментов, неся вниз позиции
// проверок формата, которые к этому месту УЖЕ исполнены безусловно.
func scanStmtsForShortCircuits(fset *token.FileSet, fnName string, stmts []ast.Stmt, inherited []token.Pos, out *paginationScan) {
	running := append([]token.Pos(nil), inherited...)

	for _, stmt := range stmts {
		ifStmt, isIf := stmt.(*ast.IfStmt)
		if !isIf {
			// Вложенные блоки (for / switch / select / block) исполняются условно —
			// спускаемся с уже накопленным, но наружу оттуда ничего не выносим.
			for _, nested := range nestedStmtLists(stmt) {
				scanStmtsForShortCircuits(fset, fnName, nested, running, out)
			}
			running = append(running, validatorCallsIn(stmt)...)
			continue
		}

		// Init выполняется безусловно ещё до вычисления условия.
		initValidators := validatorCallsIn(ifStmt.Init)
		branchInherited := append(append([]token.Pos(nil), running...), initValidators...)

		if ret := directEmptyPageReturn(ifStmt.Body); ret != nil && condHasIdentityPredicate(ifStmt.Cond) {
			out.shortCircuits++
			if !anyValidatorBefore(branchInherited, ret.Pos()) &&
				!anyValidatorBefore(unconditionalValidatorsIn(ifStmt.Body.List), ret.Pos()) {
				out.hits = append(out.hits, paginationHit{
					line: fset.Position(ifStmt.Pos()).Line,
					fn:   fnName,
				})
			}
		}

		if ifStmt.Body != nil {
			scanStmtsForShortCircuits(fset, fnName, ifStmt.Body.List, branchInherited, out)
		}
		switch els := ifStmt.Else.(type) {
		case *ast.BlockStmt:
			scanStmtsForShortCircuits(fset, fnName, els.List, branchInherited, out)
		case *ast.IfStmt:
			scanStmtsForShortCircuits(fset, fnName, []ast.Stmt{els}, branchInherited, out)
		}

		running = append(running, initValidators...)
	}
}

// unconditionalValidatorsIn — проверки формата, исполняемые на любой ветке этого
// списка стейтментов (собственные стейтменты + Init вложенных `if`).
func unconditionalValidatorsIn(stmts []ast.Stmt) []token.Pos {
	var out []token.Pos
	for _, stmt := range stmts {
		if ifStmt, ok := stmt.(*ast.IfStmt); ok {
			out = append(out, validatorCallsIn(ifStmt.Init)...)
			continue
		}
		out = append(out, validatorCallsIn(stmt)...)
	}
	return out
}

func anyValidatorBefore(positions []token.Pos, before token.Pos) bool {
	for _, p := range positions {
		if p < before {
			return true
		}
	}
	return false
}

// nestedStmtLists — списки стейтментов внутри условно исполняемых конструкций.
func nestedStmtLists(stmt ast.Stmt) [][]ast.Stmt {
	switch x := stmt.(type) {
	case *ast.BlockStmt:
		return [][]ast.Stmt{x.List}
	case *ast.ForStmt:
		if x.Body != nil {
			return [][]ast.Stmt{x.Body.List}
		}
	case *ast.RangeStmt:
		if x.Body != nil {
			return [][]ast.Stmt{x.Body.List}
		}
	case *ast.SwitchStmt:
		return caseClauseLists(x.Body)
	case *ast.TypeSwitchStmt:
		return caseClauseLists(x.Body)
	case *ast.SelectStmt:
		if x.Body == nil {
			return nil
		}
		var out [][]ast.Stmt
		for _, c := range x.Body.List {
			if cc, ok := c.(*ast.CommClause); ok {
				out = append(out, cc.Body)
			}
		}
		return out
	}
	return nil
}

func caseClauseLists(body *ast.BlockStmt) [][]ast.Stmt {
	if body == nil {
		return nil
	}
	var out [][]ast.Stmt
	for _, c := range body.List {
		if cc, ok := c.(*ast.CaseClause); ok {
			out = append(out, cc.Body)
		}
	}
	return out
}

// validatorCallsIn — позиции вызовов проверок формата внутри узла.
func validatorCallsIn(n ast.Node) []token.Pos {
	if n == nil {
		return nil
	}
	var out []token.Pos
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, known := paginationValidatorNames[calleeName(call)]; known {
			out = append(out, call.Pos())
		}
		return true
	})
	return out
}

// hasListResultShape — результаты функции суть ровно ([]T, string, error).
//
// Это подпись списочного метода Kachō: страница, курсор следующей страницы,
// ошибка. `Get` (2 результата) под неё не попадает — именно поэтому не-списочные
// замыкания по личности (`user/get.go`, `whoami.go`) гейта не касаются.
func hasListResultShape(ft *ast.FuncType) bool {
	if ft.Results == nil {
		return false
	}
	var types []ast.Expr
	for _, f := range ft.Results.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			types = append(types, f.Type)
		}
	}
	if len(types) != 3 {
		return false
	}
	if _, ok := types[0].(*ast.ArrayType); !ok {
		return false
	}
	return isIdentNamed(types[1], "string") && isIdentNamed(types[2], "error")
}

func isIdentNamed(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// condHasIdentityPredicate — содержит ли условие (рекурсивно по && / || / скобкам
// / отрицанию) хоть одну форму из словаря.
func condHasIdentityPredicate(cond ast.Expr) bool {
	return identityPredicateKind(cond) != ""
}

// identityPredicateKind возвращает ключ словаря первой найденной формы.
func identityPredicateKind(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return identityPredicateKind(x.X)
	case *ast.UnaryExpr:
		return identityPredicateKind(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			if k := identityPredicateKind(x.X); k != "" {
				return k
			}
			return identityPredicateKind(x.Y)
		}
		if x.Op == token.EQL && isSubjectIdent(x.X) && isEmptyStringLit(x.Y) {
			return "subject-ident-empty"
		}
		if x.Op == token.EQL && isSubjectIdent(x.Y) && isEmptyStringLit(x.X) {
			return "subject-ident-empty"
		}
		return ""
	case *ast.CallExpr:
		if calleeName(x) == "IsAnonymous" {
			return "is-anonymous-call"
		}
		return ""
	}
	return ""
}

func isSubjectIdent(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return strings.Contains(strings.ToLower(x.Name), "subject")
	case *ast.SelectorExpr:
		return strings.Contains(strings.ToLower(x.Sel.Name), "subject")
	}
	return false
}

func isEmptyStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``")
}

// directEmptyPageReturn — `return nil, "", nil` ПРЯМО в этом блоке.
//
// Прямо, а не рекурсивно: замыкание — это возврат самого `if`, а не возврат
// вложенной в него функции-литерала.
func directEmptyPageReturn(b *ast.BlockStmt) *ast.ReturnStmt {
	if b == nil {
		return nil
	}
	for _, stmt := range b.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 3 {
			continue
		}
		if isNilIdent(ret.Results[0]) && isEmptyStringLit(ret.Results[1]) && isNilIdent(ret.Results[2]) {
			return ret
		}
	}
	return nil
}

func isNilIdent(e ast.Expr) bool { return isIdentNamed(e, "nil") }

// calleeName — имя вызываемой функции без квалификатора пакета/приёмника.
func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func countIdentityPredicateForms(t *testing.T, rel string, body []byte) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	out := map[string]int{}
	ast.Inspect(file, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if k := identityPredicateKind(ifStmt.Cond); k != "" {
			out[k]++
		}
		return true
	})
	return out
}

func countCallsByName(t *testing.T, rel string, body []byte, name string) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	n := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && calleeName(call) == name {
			n++
		}
		return true
	})
	return n
}

// forEachProductionGoFileForPagination обходит прод-дерево.
//
// Тесты, моки и сгенерированные stub'ы исключены: их предмет — сам репозиторий,
// и форму дефекта они обязаны уметь написать, чтобы гейт можно было проверить
// инъекцией.
func forEachProductionGoFileForPagination(t *testing.T, root string, fn func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range listPaginationScanRoots {
		base := filepath.Join(root, sub)
		if _, err := os.Stat(base); err != nil {
			t.Fatalf("каталог %s не найден (%v) — область обхода гейта сломана", sub, err)
		}
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				switch info.Name() {
				case ".git", "node_modules", "vendor", "docs-site", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if strings.HasPrefix(rel, "pkg/api/") || strings.Contains(rel, "mock") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			fn(rel, body)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", sub, err)
		}
	}
}
