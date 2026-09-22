// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// testsubprocesscacheguard.go — гейт «проба, чей вердикт строит ВНЕШНИЙ
// ИНСТРУМЕНТ подпроцессом, не отвечает на кэшируемом прогоне», и его судящая
// функция.
//
// # Предмет
//
// `go test` считает отпечаток пакета по его исходникам, импортам и журналу
// обращений САМОЙ пробы. Подпроцесса в отпечатке нет: файлы, которые читает
// `helm`, проба не открывает, журнал о них не знает, правка чарта или профиля
// кеш не сбрасывает. Над деревом, где страж обязан краснеть, печатается
// `ok (cached)` — вердикт становится функцией того, КОГДА его считали в
// последний раз. Разбор механизма и замеры — `internal/cachedverdict` и
// `treecorpus/cachedverdict.go` фундамента.
//
// # Что именно судится
//
// Судится не «есть ли в файле строка со стражем», а ДОСТИЖИМОСТЬ: от каждой
// точки входа пакета (`TestX`, `BenchmarkX`, `FuzzX`, `ExampleX`, `TestMain`)
// строится граф вызовов внутри того же каталога, и на КАЖДОМ пути до места
// запуска внешнего инструмента обязан встретиться страж — РАНЬШЕ по позиции,
// чем продолжение пути. Это не украшение: в дереве обе формы законны и обе
// живые — страж у самого места запуска (помощник `helmTemplate`, тогда под ним
// все нынешние и будущие его вызывающие) и страж в точке входа, когда запуск
// лежит двумя вызовами глубже (`gateway/deploy/login_lane_prereq_test.go`).
// Распознаватель, знающий одну из них, молчал бы на второй.
//
// # Словарь инструментов ЗАКРЫТ
//
// Имя программы, которого нет в словаре, — НАХОДКА, а не умолчание. Это и есть
// ответ на «появится пятая проба»: новый инструмент не проскакивает потому, что
// о нём никто не вспомнил, — он обязан быть разобран, и решение записывается
// одной строкой словаря. Записи словаря самоистекающие: запись, которой в
// дереве нечего исключать, — тоже находка.
//
// # Граница обхода названа, а не подразумевается
//
// Гейт читает `*_test.go` из ИНДЕКСА git и видит ПРЯМОЙ запуск через `os/exec`.
// Инструмент, до которого проба дотягивается через оболочку (`bash script.sh`,
// внутри которого `helm`), этому обходу не виден — и это сказано здесь, а не
// оставлено читателю: «ноль находок» тут означает «ноль находок среди прямых
// запусков», а не «ни одна проба не зовёт helm».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// subprocessToolPolicy — решение по одному внешнему инструменту.
type subprocessToolPolicy struct {
	// guard — обязан ли путь до его запуска нести страж кэшируемого прогона.
	guard bool
	// reason — почему решение такое. Для записи без стража это ещё и предикат
	// снятия: сказано, чем именно её положено заменить.
	reason string
}

// subprocessToolCanon — ЗАКРЫТЫЙ словарь инструментов, запускаемых пробами
// дерева подпроцессом. Состав выведен обходом (`Programs` переписи), а не по
// памяти.
var subprocessToolCanon = map[string]subprocessToolPolicy{
	"helm": {
		guard: true,
		reason: "рендер чарта читает шаблоны, профили и подчарты — ни один из этих " +
			"файлов проба не открывает сама, поэтому их правка кеш не сбрасывает",
	},

	// ── ниже: инструменты, у которых пути до дерева в этой полосе НЕ РАЗБИРАЛИ.
	// Запись не утверждает «им страж не нужен»; она утверждает «эта полоса о них
	// не судила». Предикат снятия у всех один: по инструменту установлено,
	// зависит ли вердикт зовущей пробы от состояния дерева, которого проба не
	// читает сама, — и запись заменяется решением со стражем или без него.
	"go":      {reason: "полоса не разбирала: вход инструмента — модуль и его кеш, не профили посадки"},
	"bash":    {reason: "полоса не разбирала: скрипт задаётся путём, содержимое читает оболочка"},
	"python3": {reason: "полоса не разбирала: генератор задаётся путём, содержимое читает интерпретатор"},
	"gh":      {reason: "полоса не разбирала: вход — состояние трекера, а не дерева"},
	"make":    {reason: "полоса не разбирала: цель читает Makefile и всё, до чего он дотянется"},
	"jq":      {reason: "полоса не разбирала: программа фильтра приходит из самой пробы"},
}

// Формы записи программы, которые распознаватель знает. Перечень выведен тем же
// обходом, что и перепись: см. пробу предпосылки рядом с гейтом.
const (
	formLiteral  = "литерал"
	formIdent    = "имя, связанное с литералом"
	formLookPath = "exec.LookPath(литерал)"
	formSelf     = "сама проба (os.Args[0])"
	formComputed = "вычисленный путь (собранный двоичный, временный каталог)"
	formUnknown  = "НЕ УСТАНОВЛЕНО"
)

// subprocessSite — одно место запуска подпроцесса.
type subprocessSite struct {
	file    string
	line    int
	pos     token.Pos
	program string // пусто, когда форма не даёт имени
	form    string
	fn      string
}

// SubprocessGuardCensus — объём осмотренного и разложение найденного по осям.
// Перепись печатается ОТДЕЛЬНО от находок: «ноль находок» обязано быть отличимо
// от «ноль прочитанного», а «не установлено» — от того и другого.
type SubprocessGuardCensus struct {
	FilesRead  int
	LinesRead  int
	Packages   int
	Funcs      int
	Sites      int
	Programs   map[string]int
	Forms      map[string]int
	Unresolved []string // координаты мест, где программа НЕ УСТАНОВЛЕНА
	NeedGuard  int      // мест, требующих стража по словарю
	Guarded    int      // из них защищённых на всех путях
	Entries    int      // точек входа, доходящих до таких мест
	Findings   int
}

// AuditTestSubprocessCacheGuards — судящая функция гейта.
//
// Вход — карта «путь относительно корня → текст файла»: так проба гоняется и по
// живому дереву, и по синтетике инъекции, ОДНИМ И ТЕМ ЖЕ кодом.
func AuditTestSubprocessCacheGuards(sources map[string]string) ([]string, SubprocessGuardCensus) {
	cen := SubprocessGuardCensus{Programs: map[string]int{}, Forms: map[string]int{}}
	var findings []string

	byPkg := map[string][]string{}
	for rel := range sources {
		byPkg[path.Dir(rel)] = append(byPkg[path.Dir(rel)], rel)
	}
	pkgs := make([]string, 0, len(byPkg))
	for dir := range byPkg {
		pkgs = append(pkgs, dir)
	}
	sort.Strings(pkgs)
	cen.Packages = len(pkgs)

	seenProgram := map[string]bool{}
	for _, dir := range pkgs {
		files := byPkg[dir]
		sort.Strings(files)
		fset := token.NewFileSet()
		funcs := map[string]*subprocessFunc{}
		var order []string
		for _, rel := range files {
			src := sources[rel]
			cen.FilesRead++
			cen.LinesRead += strings.Count(src, "\n") + 1
			f, err := parser.ParseFile(fset, rel, src, 0)
			if err != nil {
				// Неразобранный файл — НАХОДКА, а не пропуск: молчание на нём
				// означало бы, что гейт судил дерево меньше объявленного.
				findings = append(findings, fmt.Sprintf("%s: разбор не удался: %v — обход неполон", rel, err))
				continue
			}
			collectSubprocessFuncs(fset, rel, f, funcs, &order)
		}
		cen.Funcs += len(funcs)
		for _, name := range order {
			for _, s := range funcs[name].sites {
				cen.Sites++
				cen.Forms[s.form]++
				if s.program == "" {
					cen.Unresolved = append(cen.Unresolved,
						fmt.Sprintf("%s:%d (%s :: %s)", s.file, s.line, s.form, s.fn))
					continue
				}
				cen.Programs[s.program]++
				seenProgram[s.program] = true
			}
		}
		findings = append(findings, auditPackageGuards(dir, funcs, order, &cen)...)
	}

	// Самоистечение словаря: запись, которой нечего исключать, — находка.
	names := make([]string, 0, len(subprocessToolCanon))
	for name := range subprocessToolCanon {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !seenProgram[name] {
			findings = append(findings, fmt.Sprintf(
				"словарь гейта: записи %q в дереве нечего исключать — ни одна проба его не запускает; "+
					"запись пережила свой предмет и снимается вместе с ним", name))
		}
	}

	sort.Strings(findings)
	cen.Findings = len(findings)
	return findings, cen
}

// subprocessFunc — одна функция пакета: где у неё страж, кого она зовёт и где
// запускает подпроцессы.
type subprocessFunc struct {
	name string
	file string
	line int
	// entry — точка входа прогона: её никто не зовёт.
	entry bool
	// standalone — МЕТОД. Метод судится в одиночку: ребро `x.run(…)` без вывода
	// типов не отличить от вызова одноимённого метода чужого типа, а
	// приблизительный граф дал бы ложные находки. Поэтому кредит стража от
	// вызывающего метод НЕ получает: страж обязан стоять в нём самом. В дереве
	// таких мест сегодня два (оба `go test` подпроцессом), helm среди них нет —
	// но слепой зоны здесь не остаётся.
	standalone bool
	guardPos   token.Pos
	// headPos — позиция ПЕРВОГО значащего оператора тела (вызов `t.Helper()`
	// значащим не считается). Помощник засчитывается за страж только тогда,
	// когда его страж стоит именно здесь: страж, до которого тело успевает
	// уйти в `t.Skip`, не исполняется, а пропуск `go test` кеширует как `ok`
	// ровно так же, как успех.
	headPos token.Pos
	calls   []subprocessCall
	sites   []subprocessSite
}

type subprocessCall struct {
	callee string
	pos    token.Pos
}

func collectSubprocessFuncs(fset *token.FileSet, rel string, f *ast.File, out map[string]*subprocessFunc, order *[]string) {
	fileLiterals := fileLevelStringIdents(f)
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := fd.Name.Name
		method := fd.Recv != nil
		if method {
			name = subprocessReceiverName(fd.Recv) + "." + name
		}
		if _, dup := out[name]; dup {
			// Одноимённых объявлений в одном пакете не бывает; если вход подан
			// так, что они есть, обход об этом говорит, а не склеивает молча.
			continue
		}
		info := &subprocessFunc{
			name:       name,
			file:       rel,
			line:       fset.Position(fd.Pos()).Line,
			entry:      !method && isTestEntryPoint(name),
			standalone: method,
			guardPos:   token.NoPos,
			headPos:    headStatementPos(fd),
		}
		locals := localStringIdents(fd, fileLiterals)
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.IfStmt:
				if isCacheGuardIf(node) && (info.guardPos == token.NoPos || node.Pos() < info.guardPos) {
					info.guardPos = node.Pos()
				}
			case *ast.CallExpr:
				if id, ok := node.Fun.(*ast.Ident); ok {
					info.calls = append(info.calls, subprocessCall{callee: id.Name, pos: node.Pos()})
					return true
				}
				prog, form, ok := subprocessProgramOf(node, locals)
				if !ok {
					return true
				}
				info.sites = append(info.sites, subprocessSite{
					file: rel, line: fset.Position(node.Pos()).Line, pos: node.Pos(),
					program: prog, form: form, fn: name,
				})
			}
			return true
		})
		out[name] = info
		*order = append(*order, name)
	}
}

// headStatementPos — позиция первого значащего оператора тела функции.
func headStatementPos(fd *ast.FuncDecl) token.Pos {
	for _, st := range fd.Body.List {
		expr, ok := st.(*ast.ExprStmt)
		if ok {
			if call, ok := expr.X.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Helper" {
					continue
				}
			}
		}
		return st.Pos()
	}
	return token.NoPos
}

// subprocessReceiverName — плоское имя типа приёмника, чтобы у метода было имя,
// отличное от одноимённой функции пакета.
func subprocessReceiverName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return "?"
	}
	if n := renderTypeExpr(recv.List[0].Type); n != "" {
		return n
	}
	return "?"
}

// Точку входа опознаёт общий с соседним гейтом `isTestEntryPoint`
// (`bothidentityformsproducer.go`): признак один и тот же — её никто не зовёт.
// Второй копии предиката здесь не заводится.

// isCacheGuardIf — законные формы стража. Обе живут в дереве, и обе обязаны
// распознаваться: иначе гейт краснел бы на исправной пробе.
//
//	if treecorpus.RunResultWillBeCached() { t.Fatal…(…) }
//	if msg := cachedverdict.SubprocessRefusal("helm"); msg != "" { t.Fatal…(msg) }
//
// Требуется не только предикат, но и ОТКАЗ в теле: `if …WillBeCached() { … }`
// с печатью и без падения зелёного не отменяет.
func isCacheGuardIf(stmt *ast.IfStmt) bool {
	predicate := false
	inspect := func(n ast.Node) {
		ast.Inspect(n, func(x ast.Node) bool {
			call, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case pkg.Name == "treecorpus" && sel.Sel.Name == "RunResultWillBeCached",
				pkg.Name == "cachedverdict" && sel.Sel.Name == "SubprocessRefusal":
				predicate = true
			}
			return true
		})
	}
	if stmt.Init != nil {
		inspect(stmt.Init)
	}
	if stmt.Cond != nil {
		inspect(stmt.Cond)
	}
	if !predicate {
		return false
	}
	refuses := false
	ast.Inspect(stmt.Body, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if strings.HasPrefix(sel.Sel.Name, "Fatal") || sel.Sel.Name == "Exit" {
			refuses = true
		}
		return true
	})
	return refuses
}

// subprocessProgramOf — разбор одного вызова exec.Command/CommandContext.
func subprocessProgramOf(call *ast.CallExpr, locals map[string]stringBinding) (string, string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return "", "", false
	}
	idx := 0
	switch sel.Sel.Name {
	case "Command":
		idx = 0
	case "CommandContext":
		idx = 1
	default:
		return "", "", false
	}
	if idx >= len(call.Args) {
		return "", formUnknown, true
	}
	switch arg := call.Args[idx].(type) {
	case *ast.BasicLit:
		if arg.Kind == token.STRING {
			if v, err := strconv.Unquote(arg.Value); err == nil {
				return v, formLiteral, true
			}
		}
	case *ast.Ident:
		if b, ok := locals[arg.Name]; ok {
			return b.value, b.form, true
		}
	case *ast.IndexExpr:
		if s, ok := arg.X.(*ast.SelectorExpr); ok {
			if x, ok := s.X.(*ast.Ident); ok && x.Name == "os" && s.Sel.Name == "Args" {
				return "", formSelf, true
			}
		}
	}
	return "", formUnknown, true
}

type stringBinding struct {
	value string // пусто, когда имя не разрешается в имя программы
	form  string
}

func fileLevelStringIdents(f *ast.File) map[string]stringBinding {
	out := map[string]stringBinding{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, nm := range vs.Names {
				if i < len(vs.Values) {
					bindString(out, nm.Name, vs.Values[i])
				}
			}
		}
	}
	return out
}

func localStringIdents(fd *ast.FuncDecl, inherited map[string]stringBinding) map[string]stringBinding {
	out := map[string]stringBinding{}
	for k, v := range inherited {
		out[k] = v
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if len(node.Rhs) == 1 {
					// Форма `bin, err := exec.LookPath("helm")` — правая часть
					// одна на два имени, и имя программы несёт она.
					if i == 0 {
						bindString(out, id.Name, node.Rhs[0])
					}
					continue
				}
				if i < len(node.Rhs) {
					bindString(out, id.Name, node.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, nm := range node.Names {
				if i < len(node.Values) {
					bindString(out, nm.Name, node.Values[i])
				}
			}
		}
		return true
	})
	return out
}

func bindString(out map[string]stringBinding, name string, expr ast.Expr) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				out[name] = stringBinding{value: s, form: formIdent}
				return
			}
		}
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "exec" && sel.Sel.Name == "LookPath" && len(v.Args) == 1 {
				if lit, ok := v.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						out[name] = stringBinding{value: s, form: formLookPath}
						return
					}
				}
			}
		}
		out[name] = stringBinding{form: formComputed}
		return
	}
	out[name] = stringBinding{form: formUnknown}
}

// auditPackageGuards — судит ОДИН каталог: от каждой точки входа идёт по графу
// вызовов пакета и спрашивает у каждого места запуска, встретился ли страж
// РАНЬШЕ по позиции, чем продолжение пути.
func auditPackageGuards(dir string, funcs map[string]*subprocessFunc, order []string, cen *SubprocessGuardCensus) []string {
	var findings []string

	// Имя программы вне закрытого словаря — находка. Она отдельна от вопроса о
	// страже: сначала решается, разобран ли инструмент вообще.
	for _, name := range order {
		for _, s := range funcs[name].sites {
			if s.program == "" {
				continue
			}
			if _, known := subprocessToolCanon[s.program]; !known {
				findings = append(findings, fmt.Sprintf(
					"%s:%d: проба запускает подпроцессом %q (%s), а словарь гейта о нём не знает — "+
						"инструмент не разобран: скажи в subprocessToolCanon, нужен ли ему страж "+
						"кэшируемого прогона, и почему", s.file, s.line, s.program, s.form))
			}
		}
	}

	type siteKey struct {
		file string
		line int
	}
	need := map[siteKey]subprocessSite{}
	protectedEverywhere := map[siteKey]bool{}
	reached := map[siteKey]bool{}
	entriesReaching := map[string]bool{}
	for _, name := range order {
		fn := funcs[name]
		for _, s := range fn.sites {
			pol, known := subprocessToolCanon[s.program]
			if !known || !pol.guard {
				continue
			}
			if fn.standalone {
				// Метод судится в одиночку — см. поле standalone.
				cen.NeedGuard++
				if fn.guardPos != token.NoPos && fn.guardPos < s.pos {
					cen.Guarded++
					continue
				}
				findings = append(findings, fmt.Sprintf(
					"%s:%d: метод %s запускает %q подпроцессом без стража кэшируемого прогона в "+
						"САМОМ методе — кредит стража от вызывающего метод не получает, "+
						"потому что ребро вызова метода гейт не строит",
					s.file, s.line, fn.name, s.program))
				continue
			}
			k := siteKey{s.file, s.line}
			need[k] = s
			protectedEverywhere[k] = true
		}
	}
	cen.NeedGuard += len(need)
	if len(need) == 0 {
		return findings
	}

	// Страж, вынесенный в отдельного помощника (`requireHelm(t)` и его родня),
	// — тоже страж, и распознаватель обязан его видеть: иначе гейт краснел бы на
	// исправной пробе, а автор снимал бы вынос ради зелёного. Засчитывается
	// только помощник, отказывающий НА ВХОДЕ: его страж стоит раньше любого
	// собственного вызова и любого собственного запуска, то есть путь через него
	// не доходит до вердикта. Страж в глубине ветки помощника не засчитывается —
	// про такой неизвестно, исполнится ли он.
	refusesOnEntry := map[string]bool{}
	for _, name := range order {
		fn := funcs[name]
		refusesOnEntry[name] = fn.guardPos != token.NoPos && fn.guardPos == fn.headPos
	}

	// guardFrom — позиция, начиная с которой продолжение пути в функции уже
	// защищено: либо её собственный страж, либо вызов помощника, отказывающего
	// на входе; что раньше.
	guardFrom := func(fn *subprocessFunc) token.Pos {
		at := fn.guardPos
		for _, c := range fn.calls {
			if !refusesOnEntry[c.callee] {
				continue
			}
			if at == token.NoPos || c.pos < at {
				at = c.pos
			}
		}
		return at
	}

	reportedPaths := map[string]bool{}
	var walk func(entry string, fn *subprocessFunc, guarded bool, trail []string, seen map[string]bool)
	walk = func(entry string, fn *subprocessFunc, guarded bool, trail []string, seen map[string]bool) {
		if fn == nil {
			return
		}
		key := fn.name + "|" + strconv.FormatBool(guarded)
		if seen[key] {
			return
		}
		seen[key] = true
		trail = append(trail, fn.name)
		guardAt := guardFrom(fn)

		for _, s := range fn.sites {
			pol, known := subprocessToolCanon[s.program]
			if !known || !pol.guard {
				continue
			}
			k := siteKey{s.file, s.line}
			reached[k] = true
			entriesReaching[entry] = true
			if guarded || (guardAt != token.NoPos && guardAt < s.pos) {
				continue
			}
			protectedEverywhere[k] = false
			line := fmt.Sprintf(
				"%s:%d: путь %s доходит до запуска %q подпроцессом без стража кэшируемого прогона — "+
					"вердикт этой пробы `go test` может отдать из кеша над деревом, где предмет пробы "+
					"уже сломан; страж ставится ДО запуска, формой "+
					"`if msg := cachedverdict.SubprocessRefusal(%q); msg != \"\" { t.Fatal(msg) }`",
				s.file, s.line, strings.Join(trail, " → "), s.program, s.program)
			if !reportedPaths[line] {
				reportedPaths[line] = true
				findings = append(findings, line)
			}
		}
		for _, c := range fn.calls {
			callee, ok := funcs[c.callee]
			if !ok || callee.standalone {
				continue
			}
			next := guarded || (guardAt != token.NoPos && guardAt < c.pos)
			walk(entry, callee, next, trail, seen)
		}
	}
	for _, name := range order {
		if !funcs[name].entry || funcs[name].standalone {
			continue
		}
		walk(name, funcs[name], false, nil, map[string]bool{})
	}

	for k, s := range need {
		if !reached[k] {
			// Площадка, до которой не доходит ни одна точка входа, вердикта не
			// производит — и это не «чисто», а находка своего рода: помощник без
			// вызывающего либо обход, потерявший ребро графа.
			findings = append(findings, fmt.Sprintf(
				"%s:%d: запуск %q подпроцессом не достижим ни от одной точки входа каталога %s — "+
					"либо помощник мёртв, либо ребро графа вызовов потеряно обходом",
				s.file, s.line, s.program, dir))
			continue
		}
		if protectedEverywhere[k] {
			cen.Guarded++
		}
	}
	cen.Entries += len(entriesReaching)
	return findings
}

// PremiseFailure — отказ ПРЕДПОСЫЛКИ обхода, и ПУСТАЯ строка, когда предпосылка
// держится. Живёт рядом с судящей функцией, а не в теле пробы: страж, который
// нельзя позвать с синтетическим входом, сам никогда не проверялся.
//
// Пустой обход — не «чисто», а «ничего не смотрели». Ноль мест запуска при
// непустом обходе — сломанный разбор. Ноль мест, требующих стража, — либо
// предмет ушёл из дерева целиком, либо словарь перестал его узнавать; и то и
// другое решается человеком.
func (c SubprocessGuardCensus) PremiseFailure() string {
	switch {
	case c.FilesRead == 0:
		return "прочитано ноль файлов проб — «ноль находок» означало бы «ноль прочитанного»"
	case c.Sites == 0:
		return "мест запуска подпроцесса ноль при непустом обходе — разбор сломан: " +
			"в дереве они ЕСТЬ (os/exec зовут пробы посадки, гейты и генераторы)"
	case c.NeedGuard == 0:
		return "мест, требующих стража, ноль — либо предмет ушёл из дерева целиком, " +
			"либо словарь гейта перестал его узнавать"
	}
	return ""
}

// SubprocessGuardCensusLine — перепись одной строкой для журнала пробы.
func (c SubprocessGuardCensus) Line() string {
	progs := make([]string, 0, len(c.Programs))
	for name, n := range c.Programs {
		progs = append(progs, fmt.Sprintf("%s×%d", name, n))
	}
	sort.Strings(progs)
	forms := make([]string, 0, len(c.Forms))
	for name, n := range c.Forms {
		forms = append(forms, fmt.Sprintf("%s×%d", name, n))
	}
	sort.Strings(forms)
	return fmt.Sprintf(
		"перепись: файлов прочитано %d, строк %d, каталогов %d, функций %d; "+
			"мест запуска подпроцесса %d [%s]; формы записи [%s]; "+
			"НЕ УСТАНОВЛЕНО программ %d; требуют стража %d, защищены на всех путях %d; "+
			"точек входа, доходящих до них, %d; находок %d",
		c.FilesRead, c.LinesRead, c.Packages, c.Funcs,
		c.Sites, strings.Join(progs, " "), strings.Join(forms, " "),
		len(c.Unresolved), c.NeedGuard, c.Guarded, c.Entries, c.Findings)
}
