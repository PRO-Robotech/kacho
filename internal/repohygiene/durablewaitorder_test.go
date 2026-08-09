// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// durablewaitorder_test.go — гейт против утверждения о состоянии БАЗЫ, которое
// упорядочено ожиданием ДРУГОГО свидетеля.
//
// Предмет. Дренаж (и всякий асинхронный исполнитель) обновляет два свидетеля в
// разные моменты: сперва отдаёт управление применителю — тот увеличивает
// счётчик В ПАМЯТИ пробы, — и только потом отдельным стейтментом помечает
// строку и коммитит транзакцию. Проба, которая ждёт счётчика, а затем ОДНИМ
// чтением спрашивает базу, попадает в зазор между этими двумя моментами. Зазор
// узкий и на тихой машине почти всегда закрыт; под конкуренцией за хост
// (параллельные testcontainers-суиты) он раскрывается, и проба краснеет на
// здоровом продукте — то есть меряет загрузку машины, а не поведение дренажа.
//
// Правило, которое гейт энфорсит, короткое: **на какого свидетеля ждёшь — того
// и спрашивай**. Утверждение о состоянии, которое исполнитель ещё только
// приводит к нужному виду, обязано стоять ВНУТРИ ожидания, а не за ним. Тогда
// проба завершается по ИСХОДУ (строка помечена), а не по сроку, и её вердикт
// перестаёт зависеть от того, сколько миллисекунд заняла чужая фиксация.
//
// Почему гейт, а не разовая правка. Этот класс уже находили и чинили — ровно в
// одной пробе одного сервиса (vpc), с разбором в комментарии. Братья той же
// пробы в compute и nlb правку пережили и остались недетерминированными, потому
// что свойство не видно в диффе: ожидание и чтение пишутся в соседних строках и
// выглядят как одно действие. Гейт формулирует требование локально, поэтому
// следующее такое место краснеет в момент появления.
//
// Ожиданием считается и рукописный цикл со сном: «подождать срок» и «подождать
// исход» различаются не формой вызова, а тем, чем ожидание заканчивается.
//
// Разбор идёт по синтаксическому дереву, а не по тексту: слово «Eventually» в
// комментарии, объясняющем эту же дисциплину, текстовый поиск принял бы за само
// ожидание.
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

// waitOrderScanRoots — где ищем. Только `*_test.go`: предмет запрета — проба.
var waitOrderScanRoots = []string{"services", "pkg", "gateway", "internal"}

// pgxCallNames — вызовы, которыми в этом дереве ходят в Postgres напрямую.
//
// Совпадение по имени МЕТОДА, а не по типу приёмника: гейт разбирает синтаксис
// и типов не выводит. Список — это ОБЪЁМ гейта: метод, которого здесь нет,
// database-обращением не считается, поэтому у каждой записи проверяется предмет
// (TestPgxCallNamesHaveSubject).
var pgxCallNames = map[string]string{
	"Query":    "pgx: чтение набора строк",
	"QueryRow": "pgx: чтение одной строки (в паре со Scan)",
	"Exec":     "pgx: запись/DDL",
	"BeginTx":  "pgx: открытие транзакции",
}

// dbHandleNames — имена, под которыми в пробах живёт ручка к базе.
//
// От них расходится ПРОИЗВОДНОСТЬ: всё, что собрано вызовом с такой ручкой в
// аргументах (`metrics.NewCollector(pool, …)`, `operations.NewRepo(pool, …)`),
// само становится ручкой, и вызов его метода считается обращением к базе. Без
// этого правила ожидание, которое спрашивает базу ЧЕРЕЗ такой объект, гейт
// принял бы за ожидание в памяти и выдал ложную находку.
var dbHandleNames = map[string]string{
	"pool": "pgxpool.Pool — основная форма в интеграционных пробах",
	"db":   "sql/pgx-ручка под коротким именем",
	"conn": "выделенное соединение (LISTEN, advisory-lock)",
	"tx":   "открытая транзакция",
}

type waitOrderHit struct {
	waitLine int
	readLine int
	fn       string
}

type waitOrderStats struct {
	waits     int // сколько ожиданий распознано
	loopWaits int // из них рукописных: цикл со сном вместо Eventually
	dbWaits   int // из них таких, чьё условие само спрашивает базу
	hits      []waitOrderHit
}

// TestDurableStateNeverAssertedAfterInProcessWait — ни одна проба не спрашивает
// базу об исходе сразу за ожиданием, которое базу не спрашивало.
//
// Что делать, если гейт сработал, — два исхода, третьего нет:
//
//  1. утверждение об исходе (строка помечена, очередь пуста, ресурс появился) →
//     перенести чтение ВНУТРЬ условия ожидания, рядом с тем, что уже ждётся;
//  2. утверждение об инварианте, который исполнитель не вправе нарушить
//     («строка всё ещё не отправлена, пока пир лежит») → его нельзя оставлять
//     за ожиданием ЧУЖОГО свидетеля: либо ждать своего (условие, читающее эту
//     же строку), либо утверждать инвариант там, где он и держится, — внутри
//     ожидания.
//
// Списка исключений у гейта нет намеренно: запись в нём пережила бы свой
// предмет и вернула бы ровно ту неразличимость, ради которой гейт написан.
func TestDurableStateNeverAssertedAfterInProcessWait(t *testing.T) {
	root := repoRoot(t)

	var (
		hits      []string
		files     int
		waits     int
		loopWaits int
		dbWaits   int
	)

	forEachTestGoFileForWaitOrder(t, root, func(rel string, body []byte) {
		files++
		res := analyzeWaitOrder(t, rel, body)
		waits += res.waits
		loopWaits += res.loopWaits
		dbWaits += res.dbWaits
		for _, h := range res.hits {
			hits = append(hits, rel+":"+strconv.Itoa(h.waitLine)+
				" (ожидание в "+h.fn+"; чтение базы — строка "+strconv.Itoa(h.readLine)+")")
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного» И от «ноль
	// распознанного»: сломанный обход и сгнивший словарь дают одинаково зелёный
	// гейт, если не утверждать объём осмотренного.
	if files == 0 {
		t.Fatalf("гейт не прочитал ни одного *_test.go в %v — предпосылка обхода сломана, "+
			"молчание ничего не доказывает", waitOrderScanRoots)
	}
	// Нижние границы ловят УСАДКУ распознавания (переименовали хелпер, сменили
	// форму ожидания), а не рост дерева. Замер на дереве этой правки — 109 / 54 /
	// 55; границы поставлены с запасом на обычную убыль, чтобы падение означало
	// сломанное распознавание, а не удаление пары проб. Понижать их, не установив,
	// куда делись места, нельзя.
	const minWaits = 90
	const minLoopWaits = 40
	const minDBWaits = 45
	if waits < minWaits {
		t.Errorf("распознано %d ожиданий, ожидалось не меньше %d: форма ожидания перестала "+
			"опознаваться — места ушли из-под наблюдения молча", waits, minWaits)
	}
	if loopWaits < minLoopWaits {
		t.Errorf("распознано %d рукописных ожиданий (цикл со сном), ожидалось не меньше %d: "+
			"вторая форма ожидания перестала опознаваться, и «ноль находок» по ней ничего "+
			"не значит", loopWaits, minLoopWaits)
	}
	if dbWaits < minDBWaits {
		t.Errorf("распознано %d ожиданий, спрашивающих базу, ожидалось не меньше %d: "+
			"производность ручки к базе перестала работать, и законные ожидания вот-вот "+
			"начнут читаться как находки", dbWaits, minDBWaits)
	}
	t.Logf("осмотрено проб-файлов: %d; ожиданий: %d (из них рукописных циклов со сном: %d); "+
		"спрашивают базу: %d; находок: %d", files, waits, loopWaits, dbWaits, len(hits))

	if len(hits) > 0 {
		sort.Strings(hits)
		t.Errorf("найдено %d мест, где состояние базы утверждается сразу за ожиданием "+
			"ДРУГОГО свидетеля:\n  %s\n\nСледствие: между свидетелями нет порядка — исполнитель "+
			"увеличивает счётчик в памяти раньше, чем помечает строку и коммитит, поэтому "+
			"вердикт пробы зависит от того, сколько заняла чужая фиксация. Исход: перенести "+
			"чтение внутрь условия ожидания.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestPgxCallNamesHaveSubject / TestDBHandleNamesHaveSubject — словари обязаны
// истекать сами. Запись, которой больше нечего распознавать, создаёт
// впечатление покрытия, которого нет.
func TestPgxCallNamesHaveSubject(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]int{}
	forEachTestGoFileForWaitOrder(t, root, func(rel string, body []byte) {
		countSelectorCalls(t, rel, body, pgxCallNames, seen)
	})
	for name, why := range pgxCallNames {
		if strings.TrimSpace(why) == "" {
			t.Errorf("запись словаря %q без обоснования", name)
		}
		if seen[name] == 0 {
			t.Errorf("запись словаря %q больше нечего распознавать: ни одна проба дерева её "+
				"не вызывает. Удали запись — иначе она создаёт впечатление покрытия, которого нет.",
				name)
		}
	}
	t.Logf("вызовов pgx-имён в пробах: %v", seen)
}

func TestDBHandleNamesHaveSubject(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]int{}
	forEachTestGoFileForWaitOrder(t, root, func(rel string, body []byte) {
		countIdentUses(t, rel, body, dbHandleNames, seen)
	})
	for name, why := range dbHandleNames {
		if strings.TrimSpace(why) == "" {
			t.Errorf("запись словаря %q без обоснования", name)
		}
		if seen[name] == 0 {
			t.Errorf("имя ручки %q не встречается ни в одной пробе дерева: словарь описывает "+
				"не это дерево", name)
		}
	}
	t.Logf("вхождений имён ручек в пробах: %v", seen)
}

// ---- разбор ----------------------------------------------------------------

func analyzeWaitOrder(t *testing.T, name string, body []byte) waitOrderStats {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: разбор не удался: %v — гейт не вправе засчитать файл осмотренным", name, err)
	}

	var out waitOrderStats
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		handles := dbHandlesOf(fn.Body)
		fnName := fn.Name.Name

		ast.Inspect(fn.Body, func(n ast.Node) bool {
			var list []ast.Stmt
			switch s := n.(type) {
			case *ast.BlockStmt:
				list = s.List
			case *ast.CaseClause:
				list = s.Body
			default:
				return true
			}
			for i, stmt := range list {
				cond, isWait := waitConditionOf(stmt)
				if !isWait {
					continue
				}
				out.waits++
				if _, isLoop := stmt.(*ast.ForStmt); isLoop {
					out.loopWaits++
				}
				condReadsDB := touchesDB(cond, handles)
				if condReadsDB {
					out.dbWaits++
					continue
				}
				if h, found := scanWindow(fset, list[i+1:], handles); found {
					out.hits = append(out.hits, waitOrderHit{
						waitLine: fset.Position(stmt.Pos()).Line,
						readLine: h,
						fn:       fnName,
					})
				}
			}
			return true
		})
	}
	return out
}

// scanWindow идёт по стейтментам за ожиданием и отвечает, попадает ли чтение
// базы в окно «до первого утверждения». Окно закрывается СЛЕДУЮЩИМ ожиданием:
// то, что стоит за ним, упорядочено уже им, а не разбираемым.
func scanWindow(fset *token.FileSet, rest []ast.Stmt, handles map[string]bool) (readLine int, found bool) {
	dbLine := 0
	for _, stmt := range rest {
		if _, isWait := waitConditionOf(stmt); isWait {
			return 0, false
		}
		if dbLine == 0 && touchesDB(stmt, handles) {
			dbLine = fset.Position(stmt.Pos()).Line
		}
		if containsAssertion(stmt) {
			return dbLine, dbLine != 0
		}
	}
	return 0, false
}

// waitConditionOf распознаёт ожидание и отдаёт узел, по которому оно решает,
// пора ли заканчивать: условие `Eventually` либо тело цикла со сном.
func waitConditionOf(stmt ast.Stmt) (ast.Node, bool) {
	if loop, ok := stmt.(*ast.ForStmt); ok && containsSleep(loop.Body) {
		return loop.Body, true
	}
	var (
		cond  ast.Node
		found bool
	)
	ast.Inspect(stmt, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "Eventually") {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || (pkg.Name != "require" && pkg.Name != "assert") {
			return true
		}
		if len(call.Args) >= 2 {
			cond, found = call.Args[1], true
		}
		return false
	})
	return cond, found
}

func containsSleep(n ast.Node) bool {
	got := false
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sleep" {
			got = true
			return false
		}
		return true
	})
	return got
}

func containsAssertion(n ast.Node) bool {
	got := false
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "require" || pkg.Name == "assert") {
			got = true
			return false
		}
		return true
	})
	return got
}

// touchesDB — узел обращается к базе, если содержит вызов pgx-имени либо вызов,
// в котором участвует ручка к базе (приёмником или аргументом).
func touchesDB(n ast.Node, handles map[string]bool) bool {
	if n == nil {
		return false
	}
	got := false
	ast.Inspect(n, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if _, isPgx := pgxCallNames[sel.Sel.Name]; isPgx {
				got = true
				return false
			}
		}
		if mentionsHandle(call, handles) {
			got = true
			return false
		}
		return true
	})
	return got
}

func mentionsHandle(n ast.Node, handles map[string]bool) bool {
	got := false
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && handles[id.Name] {
			got = true
			return false
		}
		return true
	})
	return got
}

// dbHandlesOf собирает имена, которые в этой функции обозначают ручку к базе:
// имена из словаря, приёмник любого `X.Pool`, и всё, что СОБРАНО из уже
// известной ручки конструктором. Три прохода — чтобы производность проходила по
// цепочке `pool → репозиторий → сборщик`; глубже цепочек в дереве нет.
//
// Производность идёт ТОЛЬКО через конструктор (`New…`/`new…`), а не через любой
// вызов: иначе значение, ПРОЧИТАННОЕ из базы (`v := mkVolume(t, repo, …)`), само
// становилось бы ручкой, и всякое утверждение о его полях читалось бы как
// обращение к базе. На дереве это давало ровно одну ложную находку — она и
// заставила сузить правило.
func dbHandlesOf(body *ast.BlockStmt) map[string]bool {
	handles := map[string]bool{}
	for name := range dbHandleNames {
		handles[name] = true
	}
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Pool" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			handles[id.Name] = true
		}
		return true
	})
	for pass := 0; pass < 3; pass++ {
		ast.Inspect(body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range as.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok || !isConstructorCall(call) {
					continue
				}
				derived := false
				for _, arg := range call.Args {
					if mentionsHandle(arg, handles) {
						derived = true
					}
				}
				if !derived {
					continue
				}
				for _, lhs := range as.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
						handles[id.Name] = true
					}
				}
			}
			return true
		})
	}
	return handles
}

// isConstructorCall — вызов вида `New…`/`new…`: он СОБИРАЕТ объект, а не
// возвращает прочитанное. Только через такой вызов ручка к базе передаётся
// дальше по цепочке.
func isConstructorCall(call *ast.CallExpr) bool {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	case *ast.Ident:
		name = fun.Name
	default:
		return false
	}
	return strings.HasPrefix(strings.ToLower(name), "new")
}

// ---- перепись --------------------------------------------------------------

func forEachTestGoFileForWaitOrder(t *testing.T, root string, visit func(rel string, body []byte)) {
	t.Helper()
	for _, sub := range waitOrderScanRoots {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("корень обхода %s не существует: %v — перепись описывает не это дерево", dir, err)
		}
		err := rootedWalk(dir,
			func(rel string) bool { return strings.HasSuffix(rel, "_test.go") },
			func(abs string, body []byte) error {
				rel, err := filepath.Rel(root, abs)
				if err != nil {
					return err
				}
				visit(filepath.ToSlash(rel), body)
				return nil
			})
		if err != nil {
			t.Fatalf("обход %s: %v", dir, err)
		}
	}
}

func countSelectorCalls(t *testing.T, name string, body []byte, want map[string]string, into map[string]int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: разбор не удался: %v", name, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if _, wanted := want[sel.Sel.Name]; wanted {
				into[sel.Sel.Name]++
			}
		}
		return true
	})
}

func countIdentUses(t *testing.T, name string, body []byte, want map[string]string, into map[string]int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: разбор не удался: %v", name, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if _, wanted := want[id.Name]; wanted {
				into[id.Name]++
			}
		}
		return true
	})
}

// ---- инъекция в обе стороны ------------------------------------------------
//
// Гейт без этой пары ловит форму, а не существо: молчание на законной
// конструкции той же формы надо доказать так же, как срабатывание на дефекте.
// Источники синтетические — исход не зависит от состояния дерева, поэтому пара
// остаётся доказательной и после того, как дерево починено.

// TestWaitOrderGateRedOnInjectedDefect — возвращённый дефект краснит гейт И
// называет координату.
func TestWaitOrderGateRedOnInjectedDefect(t *testing.T) {
	const src = `package x

func TestProbe(t *testing.T) {
	pool := setup(t)
	require.Eventually(t, func() bool {
		return iam.applied.Load() == 1
	}, 10*time.Second, 100*time.Millisecond, "delivered exactly once")

	sent, _ := countSent(ctx, t, pool)
	assert.Equal(t, 1, sent, "intent ultimately delivered")
}
`
	got := analyzeWaitOrder(t, "injected.go", []byte(src))
	if got.waits != 1 {
		t.Fatalf("ожидание не распознано: waits=%d", got.waits)
	}
	if got.dbWaits != 0 {
		t.Fatalf("ожидание в памяти засчитано спрашивающим базу: dbWaits=%d", got.dbWaits)
	}
	if len(got.hits) != 1 {
		t.Fatalf("дефект не найден: hits=%+v", got.hits)
	}
	if got.hits[0].waitLine != 5 {
		t.Errorf("гейт обязан назвать координату ожидания: ожидалась строка 5, получена %d", got.hits[0].waitLine)
	}
	if got.hits[0].readLine != 9 {
		t.Errorf("гейт обязан назвать координату чтения: ожидалась строка 9, получена %d", got.hits[0].readLine)
	}
	if got.hits[0].fn != "TestProbe" {
		t.Errorf("гейт обязан назвать пробу: получено %q", got.hits[0].fn)
	}
}

// TestWaitOrderGateRedOnSleepLoopWait — тот же дефект, записанный рукописным
// циклом со сном. «Подождать срок» и «подождать исход» различаются не формой
// вызова, поэтому цикл обязан ловиться так же.
func TestWaitOrderGateRedOnSleepLoopWait(t *testing.T) {
	const src = `package x

func TestProbe(t *testing.T) {
	pool := setup(t)
	for i := 0; i < 20; i++ {
		if iam.applied.Load() == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var sent bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT sent_at IS NOT NULL FROM o").Scan(&sent))
	assert.True(t, sent, "intent marked sent")
}
`
	got := analyzeWaitOrder(t, "injected_loop.go", []byte(src))
	if got.waits != 1 {
		t.Fatalf("цикл со сном не распознан ожиданием: waits=%d", got.waits)
	}
	if len(got.hits) != 1 {
		t.Fatalf("дефект не найден: hits=%+v", got.hits)
	}
	if got.hits[0].waitLine != 5 {
		t.Errorf("ожидалась координата ожидания 5, получена %d", got.hits[0].waitLine)
	}
}

// TestWaitOrderGateSilentWhenWaitAsksTheDatabase — ЗАКОННЫЙ БЛИЗНЕЦ той же
// формы: то же чтение за тем же ожиданием, но ожидание САМО спрашивает базу.
// Без этой половины гейт ловил бы форму, а не существо.
func TestWaitOrderGateSilentWhenWaitAsksTheDatabase(t *testing.T) {
	const src = `package x

func TestProbe(t *testing.T) {
	pool := setup(t)
	require.Eventually(t, func() bool {
		if iam.applied.Load() != 1 {
			return false
		}
		var sent bool
		if err := pool.QueryRow(ctx, "SELECT sent_at IS NOT NULL FROM o").Scan(&sent); err != nil {
			return false
		}
		return sent
	}, 10*time.Second, 100*time.Millisecond, "delivered and marked sent")

	sent, _ := countSent(ctx, t, pool)
	assert.Equal(t, 1, sent, "intent ultimately delivered")
}
`
	got := analyzeWaitOrder(t, "lawful_db_wait.go", []byte(src))
	if got.waits != 1 || got.dbWaits != 1 {
		t.Fatalf("законное ожидание обязано остаться ПОСЧИТАННЫМ, иначе молчание неотличимо "+
			"от слепоты: waits=%d dbWaits=%d", got.waits, got.dbWaits)
	}
	if len(got.hits) != 0 {
		t.Fatalf("законная конструкция той же формы не должна быть находкой: %+v", got.hits)
	}
}

// TestWaitOrderGateSilentWhenDerivedHandleAsksTheDatabase — второй законный
// близнец: ожидание спрашивает базу не сама, а через собранный из ручки объект
// (сборщик метрик). Без производности ручки это читалось бы как ожидание в
// памяти и давало ложную находку на каждой такой пробе.
func TestWaitOrderGateSilentWhenDerivedHandleAsksTheDatabase(t *testing.T) {
	const src = `package x

func TestProbe(t *testing.T) {
	pool := setup(t)
	col := metrics.NewCollector(pool, rec, metrics.CollectorConfig{})
	require.Eventually(t, func() bool {
		_ = col.Scan(ctx)
		return rec.BacklogDepth(tbl) >= 1
	}, 10*time.Second, 100*time.Millisecond, "backlog surfaced")

	var pending bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT sent_at IS NULL FROM o").Scan(&pending))
	assert.True(t, pending, "still pending")
}
`
	got := analyzeWaitOrder(t, "lawful_derived.go", []byte(src))
	if got.dbWaits != 1 {
		t.Fatalf("ожидание через собранный из ручки объект не признано обращением к базе: dbWaits=%d", got.dbWaits)
	}
	if len(got.hits) != 0 {
		t.Fatalf("ложная находка на законной форме: %+v", got.hits)
	}
}

// TestWaitOrderGateSilentWhenNextStatementIsItselfAWait — окно закрывается
// следующим ожиданием: чтение, стоящее ВНУТРИ него, упорядочено уже им.
func TestWaitOrderGateSilentWhenNextStatementIsItselfAWait(t *testing.T) {
	const src = `package x

func TestProbe(t *testing.T) {
	pool := setup(t)
	require.Eventually(t, func() bool {
		return iam.applied.Load() == 1
	}, 5*time.Second, 50*time.Millisecond, "applied")

	require.Eventually(t, func() bool {
		var sent bool
		_ = pool.QueryRow(ctx, "SELECT sent_at IS NOT NULL FROM o").Scan(&sent)
		return sent
	}, 5*time.Second, 50*time.Millisecond, "marked sent")
}
`
	got := analyzeWaitOrder(t, "lawful_next_wait.go", []byte(src))
	if got.waits != 2 {
		t.Fatalf("ожидания не распознаны: waits=%d", got.waits)
	}
	if len(got.hits) != 0 {
		t.Fatalf("чтение внутри следующего ожидания не является находкой: %+v", got.hits)
	}
}

// TestWaitOrderGateSilentOnInProcessAssertionAfterInProcessWait — отрицательный
// контроль распознавания: утверждение о том же свидетеле, на который ждали,
// находкой не является. Иначе гейт краснел бы на каждой пробе с фейком.
func TestWaitOrderGateSilentOnInProcessAssertionAfterInProcessWait(t *testing.T) {
	const src = `package x

func TestProbe(t *testing.T) {
	require.Eventually(t, func() bool {
		return fake.registeredCount() == 1
	}, 3*time.Second, 50*time.Millisecond)

	req := fake.registered[0]
	assert.Equal(t, "compute_instance:epd-mirror", req.GetObject())
}
`
	got := analyzeWaitOrder(t, "lawful_inproc.go", []byte(src))
	if got.waits != 1 {
		t.Fatalf("ожидание не распознано: waits=%d", got.waits)
	}
	if len(got.hits) != 0 {
		t.Fatalf("утверждение о том же свидетеле не является находкой: %+v", got.hits)
	}
}
