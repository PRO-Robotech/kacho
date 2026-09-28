// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// principal_verifier_wiring_test.go — проверяющий подпись на пути принципала:
// у мягкого прохода ОДИН производитель, и он назван, а отказ конструктора
// роняет старт в любом классе окружения (kacho#2827).
//
// # Предмет
//
// Здесь стоял страж с двумя ветками по классу окружения: в боевом классе
// отказ конструктора ронял старт, в классе разработки давал мягкий проход с
// предупреждением. Боевой ветке входа не производил никто. Объявление приёма
// (`TokenAcceptance`) отвергает всякую запись, на которой отказал бы
// конструктор, безусловно, а незаявленного адресата в боевом классе раньше
// отвергает страж адресата. Пробы кормили стража подставленной ошибкой —
// текстом, которого конструктор давно не производит.
//
// # Что стоит вместо
//
//   - мягкий проход ключуется своим ЕДИНСТВЕННЫМ производителем: адресат не
//     объявлен. Дойти до этой ветки может только класс разработки, и это
//     показано на настоящей конфигурации, а не на подставленной ошибке;
//   - отказ конструктора — обычная ошибка композиционного корня: старт
//     отвергается в любом классе. Новый отказ конструктора, не повторённый
//     разбором конфигурации, мягким проходом не станет.
//
// main() из пробы не исполнить (он дозванивается до соседей и занимает
// порты), поэтому провязка судится разбором исходника корня — узлами, а не
// текстом, как у соседних гейтов этого пакета. Чтения ошибки конструктора
// находятся по позиции её объявления и лексической области (блок, в котором
// она объявлена), без устаревшего разрешения объектов go/ast: одноимённый
// идентификатор другой области не засчитывается, а переобъявление имени внутри
// области — отказ пробы, потому что без разрешения типов его не различить.
//
// # Выход БЕЗУСЛОВЕН — это часть предмета
//
// «Ветка отказа содержит выход» проходима при выходе, поставленном под класс
// окружения: в классе разработки отказ конструктора снова стал бы мягким
// проходом. Поэтому тело ветки отказа — прямолинейно (только вызовы) и
// кончается выходом, сама ветка — прямой оператор блока, в котором строится
// проверяющий, а блок — ветка «адресат объявлен» и никакая другая.
//
// # Незнакомая форма — находка с её именем, а не паника (kacho#2890)
//
// Судьи проб возвращают находку, а не зовут require: инъекции в
// principal_verifier_wiring_injection_test.go кормят их настоящим корнем,
// изменённым ровно в одном факте. Форма, которой судья не знает, называется в
// находке типом узла и текстом выражения. Паника уронила бы весь пакет, и
// остальные его пробы не исполнились бы вовсе.
//
// Страж адресата судит ОДНУ форму аргумента — вызов
// `<x>.DeclaredTokenAudience()`, тот же, по которому ветвится мягкий проход:
// обе стороны судит один предикат isDeclaredAudienceCall. Переменная,
// выведенная из этого вызова (`aud := cfg.DeclaredTokenAudience()`), признанной
// формой НЕ считается и даёт находку. Довод: без разрешения типов судья не
// докажет, что между объявлением переменной и стражем её не переприсвоили и не
// затенили, — признание формы открыло бы ровно ту дыру, которую страж
// закрывает. Производителя этой формы в дереве нет, а цена отказа — одна
// строка у автора, которому находка называет, что писать.
//
// Построение проверяющего — прямой оператор блока else ветки мягкого прохода
// верхнего уровня. Прежнее требование «ни одна ветка `if`, кроме этой,
// построение не охватывает» было слепо к другим формам ветвления: к замыканию,
// которое зовётся под классом окружения, и к ветке switch. Положительное
// требование закрывает их разом, а находка перечисляет формы, отделяющие
// построение от блока else.
package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// principalVerifierSoftPassMsg — строка журнала, которой корень объявляет
// мягкий проход. По ней проба находит ветку, а оператор — причину.
const principalVerifierSoftPassMsg = "jwks verifier not wired into principal path (HMAC-dev only)"

// constructorAssign — присваивание `v, err := middleware.NewJWTVerifier(...)`.
// Ровно одно.
func constructorAssign(t *testing.T, f *ast.File) *ast.AssignStmt {
	t.Helper()
	var found []*ast.AssignStmt
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 2 || as.Tok != token.DEFINE {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewJWTVerifier" {
			if _, ok := as.Lhs[1].(*ast.Ident); ok {
				found = append(found, as)
			}
		}
		return true
	})
	require.Len(t, found, 1,
		"корень обязан строить проверяющего подпись ровно одним присваиванием `v, err := middleware.NewJWTVerifier(...)`")
	return found[0]
}

// innermostBlock — наименьший блок, охватывающий позицию: лексическая область
// переменной, объявленной в этой позиции.
func innermostBlock(f *ast.File, pos token.Pos) *ast.BlockStmt {
	var best *ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		b, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		if pos > b.Lbrace && pos < b.Rbrace && (best == nil || b.Lbrace >= best.Lbrace) {
			best = b
		}
		return true
	})
	return best
}

// scopedVar — переменная, опознанная по позиции объявления и своей области.
type scopedVar struct {
	decl  *ast.Ident
	scope *ast.BlockStmt
}

// uses — идентификаторы переменной в её области после объявления (селекторы
// полей не в счёт) и переобъявления того же имени там же.
func (v scopedVar) uses() (reads []*ast.Ident, redeclared []token.Pos) {
	selectorNames := map[*ast.Ident]bool{}
	ast.Inspect(v.scope, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			selectorNames[x.Sel] = true
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok && id != v.decl && id.Name == v.decl.Name {
						redeclared = append(redeclared, id.Pos())
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				if id.Name == v.decl.Name {
					redeclared = append(redeclared, id.Pos())
				}
			}
		case *ast.Field:
			for _, id := range x.Names {
				if id.Name == v.decl.Name {
					redeclared = append(redeclared, id.Pos())
				}
			}
		}
		return true
	})
	ast.Inspect(v.scope, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id == v.decl || id.Name != v.decl.Name || id.Pos() < v.decl.End() || selectorNames[id] {
			return true
		}
		reads = append(reads, id)
		return true
	})
	return reads, redeclared
}

// isExitCall — вызов, завершающий процесс.
func isExitCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (pkg.Name == "os" && sel.Sel.Name == "Exit") ||
		(pkg.Name == "log" && strings.HasPrefix(sel.Sel.Name, "Fatal"))
}

// unconditionalExit — пусто, если тело прямолинейно (только вызовы) и кончается
// выходом; иначе — чем выход условен.
func unconditionalExit(fset *token.FileSet, body *ast.BlockStmt) string {
	if len(body.List) == 0 {
		return "тело пусто — выхода нет"
	}
	for _, st := range body.List {
		es, ok := st.(*ast.ExprStmt)
		if !ok {
			return "в теле стоит не вызов, а оператор управления у " + fset.Position(st.Pos()).String() +
				" — выход условен"
		}
		if _, ok := es.X.(*ast.CallExpr); !ok {
			return "в теле стоит не вызов у " + fset.Position(st.Pos()).String()
		}
	}
	last := body.List[len(body.List)-1].(*ast.ExprStmt)
	if !isExitCall(last.X) {
		return "последний оператор тела у " + fset.Position(last.Pos()).String() + " процесс не завершает"
	}
	return ""
}

// nilGuardsOf — ветки `if <имя> != nil` в области переменной.
func nilGuardsOf(v scopedVar) []*ast.IfStmt {
	var out []*ast.IfStmt
	ast.Inspect(v.scope, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := ifs.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		id, ok := bin.X.(*ast.Ident)
		if !ok || id.Name != v.decl.Name || id.Pos() < v.decl.End() {
			return true
		}
		if nilID, ok := bin.Y.(*ast.Ident); ok && nilID.Name == "nil" {
			out = append(out, ifs)
		}
		return true
	})
	return out
}

// isDirectStmt — стоит ли оператор прямо в блоке, а не во вложенной ветке.
func isDirectStmt(block *ast.BlockStmt, st ast.Stmt) bool {
	for _, s := range block.List {
		if s == st {
			return true
		}
	}
	return false
}

// thenBodiesEnclosing — ветки `if`, чьё тело THEN охватывает позицию. Ветка
// else не засчитывается: условие называет то, что верно в then.
func thenBodiesEnclosing(f *ast.File, pos token.Pos) []*ast.IfStmt {
	var out []*ast.IfStmt
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if ok && pos > ifs.Body.Lbrace && pos < ifs.Body.Rbrace {
			out = append(out, ifs)
		}
		return true
	})
	return out
}

// Отказ конструктора роняет старт в ЛЮБОМ классе окружения: ошибка
// конструктора читается только условием ветки, завершающей процесс, и её
// телом; выход в этой ветке безусловен; сама ветка — прямой оператор блока,
// где строится проверяющий, а блок — ветка «адресат объявлен». Ни в стража по
// классу окружения, ни в признак «провязан ли проверяющий» ниже по корню она
// не уходит — признак провязки несёт сам проверяющий.
func TestPrincipalVerifier_ConstructorRefusalRefusesStartInEveryClass(t *testing.T) {
	fset, f := parseMain(t)
	assign := constructorAssign(t, f)
	block := innermostBlock(f, assign.Pos())
	require.NotNil(t, block, "присваивание конструктора вне блока — проба не нашла своей области")
	errVar := scopedVar{decl: assign.Lhs[1].(*ast.Ident), scope: block}

	// Проверяющий строится в ветке «адресат объявлен» и ни в какой другой: в
	// ветке по классу окружения он не строился бы вовсе в соседнем классе.
	if why := verifierPlacementFinding(fset, f, assign); why != "" {
		t.Fatal(why)
	}

	reads, redeclared := errVar.uses()
	require.Empty(t, redeclared,
		"имя %s переобъявлено в области ошибки конструктора (%v) — по позиции его не различить",
		errVar.decl.Name, redeclared)

	guards := nilGuardsOf(errVar)
	require.Len(t, guards, 1,
		"ошибка конструктора обязана читаться ровно одной веткой `if %s != nil { … завершить процесс … }`",
		errVar.decl.Name)
	guard := guards[0]
	require.True(t, isDirectStmt(block, guard),
		"ветка отказа конструктора у %s стоит внутри другой ветки — в соседней ветке отказ старт не роняет",
		fset.Position(guard.Pos()))
	if why := unconditionalExit(fset, guard.Body); why != "" {
		t.Fatalf("ветка отказа конструктора у %s не завершает процесс безусловно: %s — в каком-то "+
			"классе окружения отказ конструктора стал бы мягким проходом", fset.Position(guard.Pos()), why)
	}

	var stray []string
	for _, id := range reads {
		if id.Pos() < guard.Pos() || id.Pos() >= guard.End() {
			stray = append(stray, fset.Position(id.Pos()).String())
		}
	}
	t.Logf("ОСМОТРЕНО: чтений ошибки конструктора %d · вне ветки отказа старта %d · операторов в ветке отказа %d",
		len(reads), len(stray), len(guard.Body.List))
	require.NotEmpty(t, reads, "ошибка конструктора не читается вовсе — проба не нашла своего предмета")
	require.Empty(t, stray,
		"ошибка конструктора читается вне ветки отказа старта: %s — там она решает класс окружения "+
			"или провязку, а должна только ронять старт", strings.Join(stray, ", "))
}

// softPassSites — вызовы журнала с объявлением мягкого прохода.
func softPassSites(f *ast.File) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil && v == principalVerifierSoftPassMsg {
			out = append(out, call)
		}
		return true
	})
	return out
}

// isDeclaredAudienceCall — выражение есть вызов `<x>.DeclaredTokenAudience()`:
// единственная форма объявленного адресата, которую судят и мягкий проход, и
// страж адресата.
func isDeclaredAudienceCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "DeclaredTokenAudience"
}

// namesUndeclaredAudience — условие `cfg.DeclaredTokenAudience() == ""`.
func namesUndeclaredAudience(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL || !isDeclaredAudienceCall(bin.X) {
		return false
	}
	lit, ok := bin.Y.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `""`
}

// Мягкий проход ключуется своим производителем — незаявленным адресатом, — а
// не отказом конструктора: иначе любой будущий отказ конструктора, не
// повторённый разбором конфигурации, проходил бы мягко.
func TestPrincipalVerifier_SoftPassIsKeyedOnTheUndeclaredAudience(t *testing.T) {
	fset, f := parseMain(t)
	sites := softPassSites(f)
	require.Len(t, sites, 1, "корень обязан объявлять мягкий проход ровно одной строкой журнала %q",
		principalVerifierSoftPassMsg)
	site := sites[0]

	keyed := false
	for _, ifs := range thenBodiesEnclosing(f, site.Pos()) {
		if namesUndeclaredAudience(ifs.Cond) {
			keyed = true
		}
	}
	require.True(t, keyed,
		"мягкий проход %s лежит вне ветки THEN условия `if cfg.DeclaredTokenAudience() == \"\"` — он "+
			"ключуется не своим производителем", fset.Position(site.Pos()))
}

// verifierPlacementFinding — пусто, если присваивание конструктора — прямой
// оператор блока else единственной ветки мягкого прохода верхнего уровня
// (ветка «адресат объявлен»); иначе — находка с координатой построения.
func verifierPlacementFinding(fset *token.FileSet, f *ast.File, assign *ast.AssignStmt) string {
	at := fset.Position(assign.Pos())
	fn := mainFunc(f)
	if fn == nil {
		return fmt.Sprintf("в корне нет функции main — построение проверяющего у %s не с чем сверить", at)
	}
	softs := undeclaredAudienceIfs(fn.Body)
	if len(softs) != 1 {
		return fmt.Sprintf("веток мягкого прохода верхнего уровня %d, а не одна — построение проверяющего у %s "+
			"не с чем сверить", len(softs), at)
	}
	els, ok := softs[0].Else.(*ast.BlockStmt)
	if !ok {
		return fmt.Sprintf("у ветки мягкого прохода у %s нет блока else (форма %T) — построение проверяющего у %s "+
			"стоит не в ветке «адресат объявлен»", fset.Position(softs[0].Pos()), softs[0].Else, at)
	}
	if isDirectStmt(els, assign) {
		return ""
	}
	return fmt.Sprintf("построение проверяющего у %s — не прямой оператор блока else ветки мягкого прохода у %s; "+
		"охватывают его: %s — в соседней стороне этих форм проверяющий не строится",
		at, fset.Position(els.Pos()), nestingForms(fset, f, assign, fn, softs[0]))
}

// nestingForms — формы, охватывающие узел, кроме пропущенных: функция,
// замыкание, ветвление, ветка case, цикл, вложенный блок. Тело формы
// отдельной формой не считается.
func nestingForms(fset *token.FileSet, f *ast.File, target ast.Node, skip ...ast.Node) string {
	var stack, path []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if n.Pos() > target.Pos() || n.End() < target.End() {
			return false
		}
		if n == target {
			path = slices.Clone(stack)
			return false
		}
		stack = append(stack, n)
		return true
	})
	var forms []string
	for i, n := range path {
		if slices.Contains(skip, n) {
			continue
		}
		switch n.(type) {
		case *ast.FuncDecl, *ast.FuncLit, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt,
			*ast.CaseClause, *ast.CommClause, *ast.ForStmt, *ast.RangeStmt:
		case *ast.BlockStmt:
			if i == 0 {
				continue
			}
			switch path[i-1].(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause, *ast.LabeledStmt:
			default:
				continue
			}
		default:
			continue
		}
		forms = append(forms, fmt.Sprintf("%T у %s", n, fset.Position(n.Pos())))
	}
	if len(forms) == 0 {
		return "ни одна форма ветвления, кроме самой ветки мягкого прохода, — построение стоит вне её блока else"
	}
	return strings.Join(forms, " → ")
}

// undeclaredAudienceIfs — ветки мягкого прохода на верхнем уровне корня.
func undeclaredAudienceIfs(body *ast.BlockStmt) []*ast.IfStmt {
	var out []*ast.IfStmt
	for _, st := range body.List {
		if ifs, ok := st.(*ast.IfStmt); ok && namesUndeclaredAudience(ifs.Cond) {
			out = append(out, ifs)
		}
	}
	return out
}

// mainFunc — функция main корня; nil, если её нет.
func mainFunc(f *ast.File) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "main" && fd.Body != nil {
			return fd
		}
	}
	return nil
}

// audienceGuardCalls — вызовы стража адресата в корне.
func audienceGuardCalls(f *ast.File) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "validateProductionTokenAudience" {
				calls = append(calls, call)
			}
		}
		return true
	})
	return calls
}

// audienceGuardIf — ветка верхнего уровня корня, в условии которой позван
// страж; nil, если такой нет.
func audienceGuardIf(body *ast.BlockStmt, call *ast.CallExpr) *ast.IfStmt {
	var guard *ast.IfStmt
	for _, st := range body.List {
		ifs, ok := st.(*ast.IfStmt)
		if !ok {
			continue
		}
		init, ok := ifs.Init.(*ast.AssignStmt)
		if ok && len(init.Rhs) == 1 && init.Rhs[0] == call {
			guard = ifs
		}
	}
	return guard
}

// audienceGuardFinding — пусто, если страж адресата стоит условием ветки
// отказа верхнего уровня, выход в ней безусловен, судит он величину мягкого
// прохода и стоит до его ветки; тогда census называет осмотренное.
func audienceGuardFinding(fset *token.FileSet, f *ast.File) (finding, census string) {
	fn := mainFunc(f)
	if fn == nil {
		return "в корне нет функции main — проба не нашла своего предмета", ""
	}
	body := fn.Body
	calls := audienceGuardCalls(f)
	if len(calls) != 1 {
		return fmt.Sprintf("страж адресата обязан зваться корнем ровно один раз, а зовётся %d", len(calls)), ""
	}
	call := calls[0]
	guard := audienceGuardIf(body, call)
	if guard == nil {
		return fmt.Sprintf("страж адресата у %s позван не в условии ветки отказа верхнего уровня корня — его "+
			"может охватить ветка, в соседней стороне которой он не зовётся", fset.Position(call.Pos())), ""
	}
	lhs := guard.Init.(*ast.AssignStmt).Lhs[0]
	errID, ok := lhs.(*ast.Ident)
	if !ok {
		return fmt.Sprintf("ошибка стража адресата у %s присвоена %s (форма %T), а не объявлена именем — без "+
			"разрешения типов судья не проследит её до условия ветки", fset.Position(lhs.Pos()),
			types.ExprString(lhs), lhs), ""
	}
	errName := errID.Name
	bin, ok := guard.Cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return fmt.Sprintf("условие ветки стража адресата — не `%s != nil`", errName), ""
	}
	if x, ok := bin.X.(*ast.Ident); !ok || x.Name != errName {
		return "условие ветки стража адресата читает не его ошибку", ""
	}
	if why := unconditionalExit(fset, guard.Body); why != "" {
		return fmt.Sprintf("ветка отказа стража адресата у %s не завершает процесс безусловно: %s",
			fset.Position(guard.Pos()), why), ""
	}

	if len(call.Args) != 2 {
		return fmt.Sprintf("страж адресата принимает класс окружения и адресата, а позван с %d аргументами",
			len(call.Args)), ""
	}
	if judged := call.Args[1]; !isDeclaredAudienceCall(judged) {
		return fmt.Sprintf("страж адресата у %s судит %s (форма %T), а не вызов DeclaredTokenAudience() — величину, "+
			"по которой ветвится мягкий проход; признана одна форма, довод — в шапке пробы",
			fset.Position(judged.Pos()), types.ExprString(judged), judged), ""
	}

	softs := undeclaredAudienceIfs(body)
	if len(softs) != 1 {
		return fmt.Sprintf("корень обязан ветвиться по незаявленному адресату ровно одной веткой верхнего "+
			"уровня, а ветвится %d", len(softs)), ""
	}
	soft := softs[0]
	if guard.End() >= soft.Pos() {
		return fmt.Sprintf("страж адресата у %s стоит после ветки мягкого прохода у %s — боевой класс дошёл бы "+
			"до мягкого прохода", fset.Position(guard.Pos()), fset.Position(soft.Pos())), ""
	}
	return "", fmt.Sprintf("ОСМОТРЕНО: вызовов стража адресата %d · ветка отказа у %s · ветка мягкого прохода у %s",
		len(calls), fset.Position(guard.Pos()), fset.Position(soft.Pos()))
}

// Боевой класс не доходит до мягкого прохода только потому, что РАНЬШЕ его
// незаявленного адресата отвергает страж адресата. Поэтому держится и сам
// вызов стража: он стоит условием ветки отказа на верхнем уровне корня (ни
// одна ветка его не охватывает), выход в ней безусловен, судит он ту же
// величину, по которой ветвится мягкий проход, и стоит ДО этой ветки. Снятый
// или переставленный ниже страж открывал бы мягкий проход боевому классу.
func TestPrincipalVerifier_AudienceGuardRefusesBeforeTheSoftPass(t *testing.T) {
	fset, f := parseMain(t)
	finding, census := audienceGuardFinding(fset, f)
	require.Empty(t, finding)
	t.Log(census)
}

// acceptanceRecords — записи приёма так, как их собирает корень.
func acceptanceRecords(t *testing.T, cfg config.Config) []middleware.IssuerKeySet {
	t.Helper()
	acceptance, err := cfg.TokenAcceptance()
	require.NoError(t, err, "объявление приёма обязано разобраться — иначе проба судит не адресата")
	records := make([]middleware.IssuerKeySet, 0, len(acceptance))
	for _, b := range acceptance {
		records = append(records, middleware.IssuerKeySet{
			Issuer: b.Issuer, KeySetURL: b.KeySetURL, TokenTypes: b.TokenTypes,
			TolerateAbsentTokenType: b.TolerateAbsentTokenType, ReadRevocation: b.ReadRevocation,
		})
	}
	return records
}

// ПРОИЗВОДИТЕЛЬ МЯГКОГО ПРОХОДА И ЕГО БЛИЗНЕЦЫ — на настоящей конфигурации
// края, настоящем разборе объявления приёма и настоящем конструкторе.
//
//   - класс разработки, адресат не объявлен — вход проходит объявление приёма и
//     стража адресата и доходит до мягкого прохода: производитель есть;
//   - тот же вход в боевом классе — страж адресата отвергает старт и называет
//     ручку: до мягкого прохода боевой класс не доходит;
//   - законный близнец — адресат объявлен: конструктор строит проверяющего на
//     записях, которые пропустил разбор, в обоих классах.
func TestPrincipalVerifier_SoftPassHasItsProducerInTheDevClassOnly(t *testing.T) {
	dev := config.Config{
		AppEnv:             "dev",
		TokenIssuers:       "https://issuer.kacho.test",
		TokenIssuerKeySets: "https://issuer.kacho.test=https://kaname-internal.kacho.svc:9097/.well-known/jwks.json",
	}
	acceptanceRecords(t, dev)
	require.NoError(t, validateProductionTokenAudience(dev.AppEnv, dev.DeclaredTokenAudience()),
		"в классе разработки незаявленный адресат страж адресата пропускает")
	require.Empty(t, dev.DeclaredTokenAudience(), "производитель мягкого прохода — незаявленный адресат")

	prod := dev
	prod.AppEnv = "production"
	acceptanceRecords(t, prod)
	audErr := validateProductionTokenAudience(prod.AppEnv, prod.DeclaredTokenAudience())
	require.Error(t, audErr, "в боевом классе незаявленный адресат обязан ронять старт раньше мягкого прохода")
	require.Contains(t, audErr.Error(), config.AudienceKnob, "отказ обязан назвать ручку адресата")

	for _, twin := range []config.Config{dev, prod} {
		twin.TokenAudience = "https://api.kacho.test"
		require.NoError(t, validateProductionTokenAudience(twin.AppEnv, twin.DeclaredTokenAudience()))
		_, err := middleware.NewJWTVerifier(middleware.JWTVerifierConfig{
			Issuers: acceptanceRecords(t, twin), ExpectedAudience: twin.DeclaredTokenAudience(),
		})
		require.NoError(t, err, "с объявленным адресатом конструктор обязан строить проверяющего (env=%q)", twin.AppEnv)
	}
	t.Logf("ОСМОТРЕНО: классов окружения 2 · производитель мягкого прохода — незаявленный адресат, только в классе разработки")
}
