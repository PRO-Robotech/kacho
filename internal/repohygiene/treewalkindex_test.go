// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// treewalkindex_test.go — обходчики дерева спрашивают ИНДЕКС, а не диск.
//
// Гейт отделён от помощника, на котором он ездит (`trackedtree_test.go`): пока
// они лежали одним файлом, помощника нельзя было тронуть, не читая девятисот
// строк разбора, а гейт выглядел частью помощника. Предметы у них разные —
// помощник отвечает «что такое дерево», гейт — «все ли берут состав оттуда».

package repohygiene

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ─── ОБХОД ОТ КОРНЯ РЕПОЗИТОРИЯ: РАЗБОР, А НЕ ПОИСК ПОДСТРОКИ ────────────────

// diskRootWalk — одна находка: вызов filepath.Walk/WalkDir/Glob, чей корень
// пришёл от функции, находящей корень репозитория.
//
// # Почему форм ТРИ, а не две
//
// `filepath.Glob` — такое же перечисление содержимого репозитория по диску, как
// обход: `dir/*.sql` отвечает «какие файлы там лежат», и правила игнорирования
// на него не действуют ровно так же. Первая редакция знала только Walk и
// WalkDir, и всё, записанное третьей формой, было ВНЕ НАБЛЮДЕНИЯ — не
// нарушением, которое гейт разрешил, а предметом, которого он не видел.
//
// Слепота измерена, а не предположена: расширение распознавателя на Glob
// подняло перепись с 33 обходов до 46 и обнажило 12 мест, ни одно из которых
// перечнем не покрывалось. Два из них — соседние гейты об ОДНОМ корпусе
// (`services/*/internal/migrations/*.sql`): один спрашивал диск, другой индекс,
// и на неотслеживаемой миграции они давали 342 против 341. Вердикт зависел от
// того, что лежит в рабочем каталоге у прогоняющего (задача продукта #1495).
//
// Отсюда требование к правке этого гейта: добавляя форму, печатай перепись ПО
// ФОРМАМ. Одно суммарное число скрывает ровно тот случай, ради которого форма
// добавлена, — прибавка к осмотренному неотличима от роста дерева.
type diskRootWalk struct {
	File string
	Line int
	Func string // объемлющая функция — по ней читается, ЧЕЙ это обход
	Form string // Walk | WalkDir | Glob — перепись печатается по формам
	Text string
	// Subtree — обходится не сам корень, а каталог ВНУТРИ него
	// (`filepath.Join(root, "services")`). Класс тот же — правила игнорирования
	// действуют на любой глубине, — но исход разный: корень запрещён
	// безусловно, поддерево ведётся сокращающимся перечнем subtreeDiskWalks.
	Subtree bool
}

// inRepoDiskWalks — обходы каталогов ВНУТРИ репозитория, всё ещё берущие состав
// с диска, а не из индекса. ПЕРЕЧЕНЬ ЗАКРЫТ ДЛЯ ПОПОЛНЕНИЯ и может только
// сокращаться: гейт падает и на новом непричисленном обходе, и на записи,
// которой больше нечего покрывать, и на записи без причины.
//
// Почему перечень, а не запрет сразу. Предмет — 33 места в десяти пакетах, и
// перевести их одним изменением вместе с починкой самого стража значило бы
// смешать два предмета, лишив каждый собственного красного. Почему не проза:
// прежняя редакция называла часть этого числом «26 обходов поддеревьев», не дав
// предиката, которым его перемерить, — и для стороннего читателя число было
// невоспроизводимым. Перечень И ЕСТЬ предикат: он проверяется исполнением.
//
// Ключ — «<файл>:<объемлющая функция>», без номера строки: строки съезжают от
// любой правки, а предмет записи — обход, а не его координата. Переезд файла в
// другой пакет ключ, однако, меняет — и гейт это ловит с обеих сторон сразу
// (запись без предмета плюс непричисленный обход), так что «переехало молча»
// здесь невозможно. Значение —
// ПРИЧИНА, по которой обход ещё на диске; пустая причина неотличима от упущения
// и потому запрещена.
const (
	whyPending = "долг перевода на индекс: обход читает содержимое репозитория с диска, " +
		"правила игнорирования на него не действуют"
	whyOwnIgnoreReader = "обход НЕ слеп к игнорируемому: пакет читает правила игнорирования " +
		"сам (ignoredPaths) и пропускает покрытые ими каталоги. Это не дыра, а ВТОРАЯ " +
		"реализация того же предмета — она не знает вложенных правил, отрицаний и " +
		"локальных исключений, которые знает git, и потому обязана сойтись с индексом"
	whyCopiesRealTree = "копирует настоящее дерево во временный каталог, чтобы гейт " +
		"исполнился на нём; состав копии обязан быть составом КОММИТА, иначе проба " +
		"судит чужой рабочий каталог"
)

var inRepoDiskWalks = map[string]string{
	"gateway/internal/restmux/console_body_contract_test.go:consoleExportedStringConsts":                           whyPending,
	"gateway/internal/restmux/console_body_contract_test.go:TestConsoleMutationSurfaceIsAccountedFor":              whyPending,
	"internal/repohygiene/declaredcardinality_test.go:hasProductionReader":                                         whyPending,
	"internal/repohygiene/guardnamedincomment_test.go:TestCommentsNamingAGuardHaveItInScope":                       whyPending,
	"internal/repohygiene/guardnamedincomment_test.go:TestGuardedIdentifiersStillHaveSubject":                      whyPending,
	"internal/repohygiene/hideexistence_test.go:serviceSourceCorpus":                                               whyPending,
	"internal/repohygiene/listpaginationorder_test.go:forEachProductionGoFileForPagination":                        whyPending,
	"internal/repohygiene/listreadrelationparity_test.go:deriveServiceDomain":                                      whyPending,
	"internal/repohygiene/listreadrelationparity_test.go:discoverPageFilters":                                      whyPending,
	"internal/repohygiene/listreadrelationparity_test.go:TestListReadRelationParity_PremiseHolds":                  whyPending,
	"internal/repohygiene/lroreconciler_test.go:callsOperationsFunc":                                               whyPending,
	"internal/repohygiene/lroreconciler_test.go:reconcilerGraceLiterals":                                           whyPending,
	"internal/repohygiene/lroreconciler_test.go:unreachableReconcilerBuilders":                                     whyPending,
	"internal/repohygiene/operationownershipport_test.go:operationServiceImplPackages":                             whyPending,
	"internal/repohygiene/artifactgates/renderguard_test.go:findRenderGuards":                                      whyPending,
	"internal/repohygiene/revocationwindow_test.go:TestNoCallSiteTakesTheWindowUnprovably":                         whyPending,
	"internal/repohygiene/revocationwindow_test.go:TestNoServiceTakesTheWindowImplicitly":                          whyPending,
	"internal/repohygiene/artifactgates/scriptgatewiring_test.go:findToolScriptGates":                              whyPending,
	"internal/repohygiene/unscopedoperationslist_test.go:forEachProductionGoFile":                                  whyPending,
	"internal/repohygiene/usecaselayout_test.go:TestUseCaseLayerHasOneLayout":                                      whyPending,
	"internal/repohygiene/verbvocabulary_test.go:discoverTopLevelVerbLiterals":                                     whyPending,
	"services/compute/internal/check/retired_block_storage_test.go:forEachComputeSource":                           whyPending,
	"services/compute/internal/check/retired_table_identifier_test.go:forEachComputeGoFile":                        whyPending,
	"services/compute/internal/commentlint/commentlint_test.go:TestCommentsAreCleanOfProcessNoiseAndForeignClouds": whyPending,
	"services/compute/internal/handler/request_field_classification_test.go:computeNonTestSources":                 whyPending,
	"services/registry/tools/auditlistfilter/audit.go:checkHandlerIsWired":                                         whyPending,
	"services/registry/tools/auditlistfilter/audit.go:productionRefusalPinned":                                     whyPending,
	"services/registry/tools/auditlistfilter/census_test.go:copyRealTree":                                          whyCopiesRealTree,
	"tools/foreignclouds/foreignclouds.go:Scan":                                                                    whyOwnIgnoreReader,
	"tools/foreignclouds/foreignclouds.go:undeclaredDebtFiles":                                                     whyOwnIgnoreReader,
	"tools/foreignclouds/foreignclouds.go:unusedCoordinates":                                                       whyOwnIgnoreReader,
}

// diskEnumerationForms — формы перечисления содержимого по диску, которые
// распознаватель знает.
//
// Три, а не две: `filepath.Glob` отвечает на тот же вопрос «какие файлы лежат в
// этом каталоге» и правила игнорирования видит так же — то есть это обход,
// записанный иначе. Форма, которой распознаватель не знает, даёт не зелёное и не
// красное, а МОЛЧАНИЕ: всё записанное в ней невидимо целиком.
//
// Перечень ОДИН на детектор и перепись намеренно. Разойдись они, перепись
// печатала бы «Glob 0» о форме, которой детектор не ищет, — и «ноль находок»
// стало бы неотличимо от «ноль искомого». По этой же причине перепись печатает
// ВСЕ формы отсюда, включая нулевые.
var diskEnumerationForms = map[string]bool{"Walk": true, "WalkDir": true, "Glob": true}

// rootProducers — функции пакета, говорящие о КОРНЕ ДЕРЕВА, которое
// принадлежит репозиторию.
//
// Признак структурный, а не по списку имён: функция возвращает строку и либо
// (а) опирается на `go.mod`, либо (б) ПОДНИМАЕТСЯ вверх по каталогам
// (`filepath.Dir` в связке с `os.Stat`), пока не найдёт каталог-маркер.
// Переименование такой функции признак переживает, захардкоженный список имён —
// нет.
//
// Вторая форма добавлена не для симметрии. Она была слепым пятном: помощник,
// поднимающийся до каталога-маркера, возвращает каталог РЕПОЗИТОРИЯ так же
// верно, как поиск `go.mod`, — просто не тот же самый. Гейт, знавший только
// первую форму, обходов от таких корней не видел вовсе, и один из них
// действительно читал игнорируемый git'ом сгенерированный каталог (фикс —
// в этом же коммите).
//
// Признак намеренно ШИРЕ предмета: под него попадает и `moduleImportPath`,
// которая `go.mod` читает, а не поднимается к нему. Ошибка в эту сторону
// громкая — лишняя находка видна и разбирается; в обратную она тихая, и гейт
// молча перестал бы искать. На дереве этого коммита (3110 файлов, 333 пакета)
// ложных находок от этой широты нет.
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
			climbs, stats, caller := false, false, false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if lit, isLit := n.(*ast.BasicLit); isLit &&
					lit.Kind == token.STRING && lit.Value == `"go.mod"` {
					out[fd.Name.Name] = true
				}
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				sel, isSel := call.Fun.(*ast.SelectorExpr)
				if !isSel {
					return true
				}
				pkg, isIdent := sel.X.(*ast.Ident)
				if !isIdent {
					return true
				}
				switch pkg.Name + "." + sel.Sel.Name {
				case "filepath.Dir":
					climbs = true
				case "os.Stat":
					stats = true
				case "runtime.Caller":
					// Третья форма поиска корня, которой признак не знал:
					// оттолкнуться от файла ЭТОГО исходника и подняться на
					// известное число каталогов. Маркера она не ищет и не
					// статит — поэтому пара «поднимается И проверяет» её не
					// узнавала, и обход от такого корня был невидим.
					caller = true
				}
				return true
			})
			if (climbs && stats) || (climbs && caller) {
				out[fd.Name.Name] = true
			}
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
	_, ok := rootValueOf(e, producers, local)
	return ok
}

// rootValueOf — «это значение лежит внутри репозитория?» и, если да, «это САМ
// корень или каталог внутри него?».
//
// Различение несёт исход: обход корня запрещён безусловно, обход поддерева
// ведётся сокращающимся перечнем. Оба — один класс (правила игнорирования
// действуют на любой глубине), но разной срочности.
//
// local: ключ — имя, несущее путь внутри репозитория; значение — «это сам
// корень». Поэтому принадлежность и точность читаются РАЗНЫМИ операциями, и
// подмена одной другою — как раз то, из-за чего каталог, собранный
// `filepath.Join`, засчитывался корнем.
func rootValueOf(e ast.Expr, producers, local map[string]bool) (exact bool, inRepo bool) {
	switch v := e.(type) {
	case *ast.Ident:
		ex, ok := local[v.Name]
		return ex, ok
	case *ast.CallExpr:
		if producers[walkCalleeName(v.Fun)] {
			return true, true
		}
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok2 := sel.X.(*ast.Ident); ok2 && pkg.Name == "filepath" && sel.Sel.Name == "Join" {
				for i, a := range v.Args {
					if _, in := rootValueOf(a, producers, local); in {
						// Join(root) без добавленных сегментов — это по-прежнему
						// корень; с сегментами — уже поддерево.
						return len(v.Args) == 1 && i == 0, true
					}
				}
			}
		}
	}
	return false, false
}

// rootValuedIdents — имена внутри fd, несущие корень репозитория: окрашенные
// параметры плюс присваивания от производителя или от уже окрашенного имени.
func rootValuedIdents(fd *ast.FuncDecl, producers map[string]bool, tainted map[string]map[string]bool) map[string]bool {
	local := map[string]bool{}
	if fd.Name != nil {
		for name, exact := range tainted[fd.Name.Name] {
			local[name] = exact
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
				if !isIdent || id.Name == "_" {
					continue
				}
				exact, inRepo := rootValueOf(as.Rhs[i], producers, local)
				if !inRepo {
					continue
				}
				was, known := local[id.Name]
				next := was || exact
				if known && next == was {
					continue
				}
				local[id.Name] = next
				changed = true
			}
			return true
		})
	}
	return local
}

// rootIsExact — выражение есть САМ корень репозитория, а не каталог внутри него.
func rootIsExact(e ast.Expr, producers, local map[string]bool) bool {
	exact, inRepo := rootValueOf(e, producers, local)
	return inRepo && exact
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
					exact, inRepo := rootValueOf(arg, producers, local)
					if !inRepo {
						continue
					}
					p := paramNameAt(callee, i)
					if p == "" || p == "_" {
						continue
					}
					if tainted[callee.Name.Name] == nil {
						tainted[callee.Name.Name] = map[string]bool{}
					}
					was, known := tainted[callee.Name.Name][p]
					next := was || exact
					if known && next == was {
						continue
					}
					tainted[callee.Name.Name][p] = next
					changed = true
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
				if !diskEnumerationForms[sel.Sel.Name] {
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
					File:    name,
					Line:    fset.Position(call.Pos()).Line,
					Func:    enclosing,
					Form:    sel.Sel.Name,
					Text:    "filepath." + sel.Sel.Name + "(" + renderExpr(fset, call.Args[0]) + ", …)",
					Subtree: !rootIsExact(call.Args[0], producers, local),
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
// Перечисление идёт по исходникам ВСЕГО дерева, а не по памяти автора и не по
// одному каталогу: новый обход от корня, добавленный мимо помощника, обязан быть
// виден, где бы его ни завели. Дискриминатор проверен в обе стороны отдельно —
// см. TestDiskRootWalkDiscriminatorCutsBothWays.
//
// # Почему дерево, а не этот пакет
//
// Прежняя редакция читала только `internal/repohygiene/`, тогда как обоснование
// запрета — факт про ВЕСЬ репозиторий. Гейт судил не тот объём, и это ровно тот
// класс, который он же и запрещает.
//
// Замер — разбором, не грепом, и разница воспроизводима одной командой:
// `git grep -oE 'filepath\.(Walk|WalkDir)\('` по отслеживаемым `.go` даёт 69,
// разбор — 57, потому что греп считает и комментарии, и строковые литералы.
// Прежняя редакция называла здесь 67; число было верным для другого предиката,
// и перемерить его по тексту правила было нельзя — предикат не назывался.
// Теперь называется, вместе с обеими величинами.
//
// # Поддерево — тот же класс, и оно тоже под гейтом
//
// Обход ПОДДЕРЕВА (`filepath.Join(root, "services")`) — тот же класс, что и
// обход корня: `.gitignore` действует на любой глубине, а под `deploy/`,
// `gateway/`, `ui-future/` и `services/` игнорируемое лежит на всякой машине,
// где поднимали стенд или собирали фронтенд (распаковки чартов, сборочные
// каталоги, node_modules, отчёты прогонов). Два таких обхода уже оказались
// дефектными по этой самой причине.
//
// Прежняя редакция объявляла их «открытым долгом с числом 26» — и это было
// худшее из двух: не запрет и не измерение. Числа было не перемерить (предикат
// не назван), а пока долг живёт прозой, к нему невозбранно дописывается
// двадцать седьмой. Теперь предикат назван (значение внутри репозитория —
// корень либо собранный поверх него путь), а долг ведётся ПЕРЕЧНЕМ
// inRepoDiskWalks, который может только сокращаться: новый непричисленный обход
// — находка, запись без предмета — находка, запись без причины — находка.
//
// Обход САМОГО корня в перечень не дописывается: три существующие записи
// объяснены тем, что читают правила игнорирования собственными силами, и это
// вторая реализация предмета, а не дыра. Четвёртой такой записи быть не должно.
func TestTreeWalkersAskTheIndex(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// Файлы — из индекса, не с диска (иначе проверка про обход индекса сама
	// читала бы диск). Раскладываются по каталогам: производители корня и граф
	// вызовов — понятия пакета, а не дерева.
	byPkg := map[string][]string{}
	for f := range tt.files {
		if strings.HasSuffix(f, ".go") {
			byPkg[path.Dir(f)] = append(byPkg[path.Dir(f)], f)
		}
	}
	if len(byPkg) == 0 {
		t.Fatal("прочитано ноль .go-файлов — перепись пуста, вердикт ничего не значит")
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	var (
		offenders     []diskRootWalk
		parsedFiles   int
		pkgsWithRoot  int
		producerNames = map[string]bool{}
	)
	for _, pkg := range pkgs {
		rels := byPkg[pkg]
		sort.Strings(rels)
		fset := token.NewFileSet()
		files := map[string]*ast.File{}
		for _, rel := range rels {
			body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			parsed, perr := parser.ParseFile(fset, rel, body, parser.ParseComments)
			if perr != nil {
				// Синтаксически битый файл поймает сборка; молчать о нём нельзя —
				// непрочитанное не есть «чисто».
				t.Fatalf("%s: разбор: %v — непрочитанный файл делает «ноль находок» "+
					"неотличимым от «ноль осмотренного»", rel, perr)
			}
			files[rel] = parsed
			parsedFiles++
		}
		producers := rootProducers(files)
		if len(producers) == 0 {
			continue
		}
		pkgsWithRoot++
		for p := range producers {
			producerNames[p] = true
		}
		offenders = append(offenders, findDiskRootWalks(fset, files, producers)...)
	}

	// Предпосылка: в дереве обязан быть хоть один источник корня. Ноль означает,
	// что отличить обход корня от обхода поддерева стало нечем, — и «ноль
	// находок» тут значило бы «нечем было искать».
	if pkgsWithRoot == 0 {
		t.Fatal("во всём дереве не нашлось ни одной функции, находящей корень " +
			"репозитория (возвращает строку и опирается на `go.mod` либо " +
			"поднимается вверх по каталогам до маркера). Предпосылка гейта исчезла: " +
			"без источника корня отличить обход корня от обхода поддерева нечем.")
	}
	names := make([]string, 0, len(producerNames))
	for p := range producerNames {
		names = append(names, p)
	}
	sort.Strings(names)

	t.Logf("осмотрено: индекс %d файлов -> %d .go в %d пакетах; пакетов с источником "+
		"корня: %d; сами источники: %s",
		tt.count(), parsedFiles, len(byPkg), pkgsWithRoot, strings.Join(names, ", "))

	sort.Slice(offenders, func(i, j int) bool {
		if offenders[i].File != offenders[j].File {
			return offenders[i].File < offenders[j].File
		}
		return offenders[i].Line < offenders[j].Line
	})

	// Обходы делятся на два исхода, и ни один из них не «нормально».
	//
	// Обход САМОГО корня запрещён безусловно. Обход ПОДДЕРЕВА внутри репозитория
	// — тот же класс (правила игнорирования действуют на любой глубине, и два
	// уже разобранных дефекта были именно такими), но его перевод затрагивает
	// два с половиной десятка мест, поэтому он ведётся ПЕРЕЧНЕМ, который может
	// только сокращаться: новое такое место — находка, а запись, которой больше
	// нечего покрывать, — тоже находка.
	//
	// Прежняя редакция называла это «открытым долгом с числом 26», и число было
	// названо без предиката, которым его можно перемерить. Теперь предикат и
	// есть перечень: он воспроизводится исполнением, а не памятью.
	var rootWalks, unpinned []diskRootWalk
	covered := map[string]bool{}
	var reasonless []string
	for _, o := range offenders {
		key := o.File + ":" + o.Func
		why, pinned := inRepoDiskWalks[key]
		switch {
		case !pinned && !o.Subtree:
			rootWalks = append(rootWalks, o)
		case !pinned:
			unpinned = append(unpinned, o)
		default:
			covered[key] = true
			if strings.TrimSpace(why) == "" {
				reasonless = append(reasonless, key)
			}
		}
	}

	var stale []string
	for key := range inRepoDiskWalks {
		if !covered[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	sort.Strings(reasonless)

	subtrees := 0
	byForm := map[string]int{}
	for _, o := range offenders {
		if o.Subtree {
			subtrees++
		}
		byForm[o.Form]++
	}
	// Перепись ПО ФОРМАМ, а не одним числом: расширив распознаватель на новую
	// форму, сравнивать надо осмотренное с находками отдельно по каждой. Одно
	// суммарное число не отличает «прибавка была слепой зоной» от «дерево
	// выросло» — то есть скрывает именно то, ради чего форму добавляли.
	forms := make([]string, 0, len(diskEnumerationForms))
	for f := range diskEnumerationForms {
		forms = append(forms, f+" "+strconv.Itoa(byForm[f]))
	}
	sort.Strings(forms)
	t.Logf("перепись обходов: внутри репозитория %d (корневых %d, поддеревьев %d); "+
		"по формам: %s; перечень: записей %d, с предметом %d, просроченных %d",
		len(offenders), len(offenders)-subtrees, subtrees, strings.Join(forms, ", "),
		len(inRepoDiskWalks), len(covered), len(stale))

	for _, key := range stale {
		t.Errorf("запись перечня без предмета: «%s» числится обходом по диску, но такого обхода "+
			"в дереве больше нет.\n"+
			"  Перечень обязан только сокращаться: запись, которой нечего покрывать, унаследует "+
			"следующее слепое пятно — ровно так послабление переживает свой предмет.\n"+
			"  ЧТО ДЕЛАТЬ: сними запись.", key)
	}
	for _, key := range reasonless {
		t.Errorf("запись перечня без причины: «%s». Запись без обоснования неотличима от "+
			"упущения, и снять её потом будет некому — назови, почему обход ещё на диске.", key)
	}

	if len(unpinned) > 0 {
		lines := make([]string, 0, len(unpinned))
		for _, o := range unpinned {
			lines = append(lines, o.File+":"+strconv.Itoa(o.Line)+" ("+o.Func+") — "+o.Text)
		}
		t.Errorf("обход ПОДДЕРЕВА репозитория идёт по диску и в перечне не значится — %d шт.:\n  %s\n\n"+
			"Правила игнорирования действуют на любой глубине: под deploy/, gateway/, ui-future/ и "+
			"services/ игнорируемое лежит на всякой машине, где поднимали стенд или собирали "+
			"фронтенд (распаковки чартов, сборочные каталоги, node_modules, отчёты прогонов). Два "+
			"обхода поддерева уже оказались дефектными по этой самой причине.\n"+
			"  ЧТО ДЕЛАТЬ: возьми состав у pkg/treecorpus. Перечень inRepoDiskWalks закрыт "+
			"для пополнения: он существует, чтобы уже накопленное сокращалось, а не чтобы в него "+
			"дописывали новое.",
			len(unpinned), strings.Join(lines, "\n  "))
	}

	if len(rootWalks) > 0 {
		lines := make([]string, 0, len(rootWalks))
		for _, o := range rootWalks {
			lines = append(lines, o.File+":"+strconv.Itoa(o.Line)+" ("+o.Func+") — "+o.Text)
		}
		t.Errorf("обход от корня репозитория идёт по ДИСКУ, а не по индексу — %d шт.:\n  %s\n\n"+
			"Под корнем лежат каталоги, которых в репозитории нет (`.claude/worktrees/` —"+
			" рабочие копии агентов, отчёты прогонов, локальные оверлеи, сборочные и"+
			" сгенерированные каталоги). Прочитав их, гейт делает свой вердикт свойством"+
			" ЧУЖОГО рабочего каталога, а не коммита — и в обе стороны: красный на файле,"+
			" которого в репозитории нет, и молчание в свежем checkout там, где обязан"+
			" говорить. Возьми список файлов у newTrackedTree(t, root) — в этом пакете,"+
			" или у pkg/treecorpus — в любом другом.",
			len(rootWalks), strings.Join(lines, "\n  "))
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

// Вторая форма производителя: подъём по каталогам до КАТАЛОГА-МАРКЕРА, без
// единого упоминания go.mod. Именно она была слепым пятном.
func treeRootByMarker(t *testing.T) string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "gateway")); err == nil {
			return filepath.Join(dir, "gateway")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ЛОВИТСЯ: корень найден подъёмом до маркера и уходит в обход диска.
func viaMarkerRoot(t *testing.T) {
	root := treeRootByMarker(t)
	_ = filepath.Walk(root, nil)
}

// МОЛЧИТ: строку возвращает функция, которая по каталогам НЕ поднимается —
// она их только соединяет. Признак не должен срабатывать на одном filepath.
func joinsOnly(base string) string { return filepath.Join(base, "sub") }

func viaJoinsOnly(t *testing.T) { _ = filepath.Walk(joinsOnly(t.TempDir()), nil) }

// ── ТРЕТЬЯ ФОРМА: перечисление образцом. Была слепой зоной целиком ─────────

// ЛОВИТСЯ: образец собран поверх корня — то же перечисление содержимого
// репозитория по диску, только записанное не обходом.
func globsSubtree(t *testing.T) {
	root := repoRoot(t)
	_, _ = filepath.Glob(filepath.Join(root, "services", "*.sql"))
}

// ЛОВИТСЯ: образец с метасимволом в СЕРЕДИНЕ — форма, которой переводятся
// перечисления вида ui-future/<модуль>/Dockerfile.
func globsMetaInTheMiddle(t *testing.T) {
	_, _ = filepath.Glob(filepath.Join(repoRoot(t), "ui-future", "*", "Dockerfile"))
}

// МОЛЧИТ: тот же вызов той же формы, но каталог временный — законный близнец.
// Без него гейт ловил бы ФОРМУ, а не существо, и первый же ложный срабат на
// синтетике его отключил бы.
func globsSynthetic(t *testing.T) {
	_, _ = filepath.Glob(filepath.Join(t.TempDir(), "*.sql"))
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
	if !producers["treeRootByMarker"] {
		t.Fatalf("подъём по каталогам до маркера не опознан как источник корня (%v). "+
			"Это была слепая зона: помощник, поднимающийся до каталога-маркера, "+
			"возвращает каталог репозитория так же верно, как поиск go.mod, — и "+
			"обходы от него гейт не видел вовсе", producers)
	}
	// Предпосылка новой формы, записанная так, чтобы её нельзя было потерять
	// молча: снимут Glob из перечня — покраснеет здесь, а не «станет чисто».
	if !diskEnumerationForms["Glob"] {
		t.Fatal("распознаватель не знает формы Glob. Это не «нет находок», а «нет " +
			"искомого»: перечисление образцом невидимо целиком, и всё, записанное " +
			"им, уходит из-под наблюдения — не нарушением, а слепой зоной")
	}
	if producers["joinsOnly"] {
		t.Errorf("соединение путей принято за подъём к корню (%v) — признак стал "+
			"грубее своего предмета и будет ловить любую функцию с filepath", producers)
	}

	got := map[string]bool{}
	subtree := map[string]bool{}
	for _, o := range findDiskRootWalks(fset, files, producers) {
		got[o.Func] = true
		subtree[o.Func] = o.Subtree
	}

	// (а) КРАСНОЕ НАПРАВЛЕНИЕ — каждое обязано быть названо, и названо ВЕРНО.
	//
	// Значение — «это обход поддерева». Классификация здесь не украшение: от неё
	// зависит исход (корень запрещён безусловно, поддерево ведётся перечнем), и
	// именно она однажды разъехалась с предметом — обход, собранный
	// `filepath.Join` поверх корня, засчитывался обходом самого корня, потому
	// что принадлежность и точность читались одной операцией.
	mustCatch := map[string]bool{
		"viaVar":        false, // корень уходит в обход через переменную
		"viaInline":     false, // корень уходит в обход прямо, без промежуточного имени
		"helperWalks":   false, // обход спрятан в помощнике, корень приезжает параметром
		"viaMarkerRoot": false, // корень найден подъёмом до маркера — форма, которой признак раньше не знал
		"subtree":       true,  // каталог ВНУТРИ репозитория: тот же класс, другой исход
		// Третья форма перечисления. До её добавления обе эти функции были
		// невидимы целиком — не разрешены, а не осмотрены.
		"globsSubtree":         true,
		"globsMetaInTheMiddle": true,
	}
	for fn, wantSubtree := range mustCatch {
		if !got[fn] {
			t.Errorf("не пойман обход внутри репозитория в %s; найдено: %v", fn, sortedNames(got))
			continue
		}
		if subtree[fn] != wantSubtree {
			kind := map[bool]string{true: "поддеревом", false: "самим корнем"}
			t.Errorf("%s классифицирован %s, а обходит %s. Классификация решает исход: "+
				"обход корня запрещён безусловно, обход поддерева ведётся сокращающимся "+
				"перечнем — перепутав их, гейт либо требует невозможного, либо разрешает "+
				"лишнее", fn, kind[subtree[fn]], kind[wantSubtree])
		}
	}

	// (б) МОЛЧАЛИВОЕ НАПРАВЛЕНИЕ — законная конструкция ТОЙ ЖЕ ФОРМЫ.
	//
	// helperWalks и syntheticWalks пишут БУКВАЛЬНО одно и то же —
	// `filepath.Walk(<параметр>, nil)`; различает их только источник значения.
	// mentionsOnly упоминает форму текстом.
	mustStaySilent := map[string]string{
		"syntheticWalks": "тот же параметр, но приезжает временный каталог — " +
			"дискриминатором стало ИМЯ переменной, а не источник значения",
		"viaSynthetic": "вызов синтетического обхода сам обходом не является",
		"viaJoinsOnly": "поддерево ВРЕМЕННОГО каталога — вне репозитория, " +
			"признак стал ловить любое соединение путей",
		"mentionsOnly": "форма найдена в строковом литерале или комментарии — " +
			"проверка читает ТЕКСТ, а не исполняемую часть",
		"repoRoot": "поиск go.mod принят за обход",
		"globsSynthetic": "образец ТОЙ ЖЕ формы над временным каталогом — " +
			"дискриминатором стала форма вызова, а не источник значения",
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
