// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// principal_verifier_wiring_injection_test.go — инъекции в обе стороны для
// судей principal_verifier_wiring_test.go (kacho#2890).
//
// Вход каждой инъекции — НАСТОЯЩИЙ корень из дерева, изменённый по позициям
// его разбора ровно в одном факте; законный близнец — тот же корень без
// правки. Поэтому каждая проба сперва утверждает молчание судьи на корне как
// он есть, затем — названную находку на правке. Синтетика здесь не годится:
// судья, слепой к форме, ищет её в живом корне рядом со всем, что там лежит.
//
// Незнакомая судье форма обязана давать находку с именем формы, а не панику:
// паника роняет весь пакет, и остальные его пробы не исполняются вовсе.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// rootEdit — замена байтов [from, to) исходника корня текстом.
type rootEdit struct {
	from, to int
	text     string
}

// rootSource — исходник корня и его разбор: вход инъекций.
func rootSource(t *testing.T) ([]byte, *token.FileSet, *ast.File) {
	t.Helper()
	src, err := os.ReadFile("main.go")
	require.NoError(t, err, "корень не читается — инъекциям не на чем стоять")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.SkipObjectResolution)
	require.NoError(t, err, "корень не разбирается")
	return src, fset, f
}

// mutateRoot — применяет правки по позициям разбора и разбирает результат.
func mutateRoot(t *testing.T, src []byte, edits ...rootEdit) (*token.FileSet, *ast.File) {
	t.Helper()
	sort.Slice(edits, func(i, j int) bool { return edits[i].from > edits[j].from })
	out := append([]byte(nil), src...)
	for _, e := range edits {
		out = append(out[:e.from], append([]byte(e.text), out[e.to:]...)...)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", out, parser.SkipObjectResolution)
	require.NoError(t, err, "правка инъекции сломала разбор корня — инъекция не того предмета")
	return fset, f
}

// offset — байтовое смещение позиции в исходнике.
func offset(fset *token.FileSet, pos token.Pos) int { return fset.Position(pos).Offset }

// audienceGuardSite — вызов стража и его ветка в корне как он есть; судья на
// нём обязан молчать (законный близнец каждой инъекции стража).
func audienceGuardSite(t *testing.T) ([]byte, *token.FileSet, *ast.CallExpr, *ast.IfStmt) {
	t.Helper()
	src, fset, f := rootSource(t)
	finding, census := audienceGuardFinding(fset, f)
	require.Empty(t, finding, "законный близнец — корень как он есть — обязан молчать")
	t.Log("близнец: " + census)
	fn := mainFunc(f)
	require.NotNil(t, fn, "предпосылка инъекции: в корне есть main")
	calls := audienceGuardCalls(f)
	require.Len(t, calls, 1, "предпосылка инъекции: страж адресата позван ровно раз")
	guard := audienceGuardIf(fn.Body, calls[0])
	require.NotNil(t, guard, "предпосылка инъекции: страж стоит условием ветки верхнего уровня")
	require.Len(t, calls[0].Args, 2, "предпосылка инъекции: у стража два аргумента")
	return src, fset, calls[0], guard
}

// requireNamedFinding — судья дал находку, и она называет каждое из слов.
func requireNamedFinding(t *testing.T, finding string, names ...string) {
	t.Helper()
	require.NotEmpty(t, finding, "инъекция не дала находки — судья слеп к форме")
	for _, n := range names {
		require.Contains(t, finding, n, "находка не называет %q", n)
	}
	t.Log("находка: " + finding)
}

// A3: страж судит сырое поле вместо объявленного адресата. Сырое поле не
// обрезано, и боевой класс с адресатом из одних пробелов прошёл бы стража,
// тогда как мягкий проход ветвится по обрезанному.
func TestPrincipalVerifierInjection_GuardJudgingTheRawFieldIsNamed(t *testing.T) {
	src, fset, call, _ := audienceGuardSite(t)
	arg := call.Args[1]
	mfset, mf := mutateRoot(t, src, rootEdit{offset(fset, arg.Pos()), offset(fset, arg.End()), "cfg.TokenAudience"})
	finding, _ := audienceGuardFinding(mfset, mf)
	requireNamedFinding(t, finding, "*ast.SelectorExpr", "cfg.TokenAudience")
}

// Переменная, выведенная из объявленного адресата: признанной формой она не
// считается (довод — в шапке principal_verifier_wiring_test.go), и судья
// называет её, а не падает.
func TestPrincipalVerifierInjection_GuardJudgingAVariableIsNamed(t *testing.T) {
	src, fset, call, guard := audienceGuardSite(t)
	arg := call.Args[1]
	at := offset(fset, guard.Pos())
	mfset, mf := mutateRoot(t, src,
		rootEdit{at, at, "aud := cfg.DeclaredTokenAudience()\n\t"},
		rootEdit{offset(fset, arg.Pos()), offset(fset, arg.End()), "aud"})
	finding, _ := audienceGuardFinding(mfset, mf)
	requireNamedFinding(t, finding, "*ast.Ident", "aud")
}

// Ошибка стража присвоена полю, а не объявлена именем: форма левой части
// незнакома судье, и он называет её, а не падает.
func TestPrincipalVerifierInjection_GuardErrorIntoAFieldIsNamed(t *testing.T) {
	src, fset, _, guard := audienceGuardSite(t)
	init := guard.Init.(*ast.AssignStmt)
	lhs := init.Lhs[0]
	bin, ok := guard.Cond.(*ast.BinaryExpr)
	require.True(t, ok, "предпосылка инъекции: условие ветки стража — сравнение")
	mfset, mf := mutateRoot(t, src,
		rootEdit{offset(fset, lhs.Pos()), offset(fset, lhs.End()), "st.audErr"},
		rootEdit{offset(fset, init.TokPos), offset(fset, init.TokPos) + len(init.Tok.String()), "="},
		rootEdit{offset(fset, bin.X.Pos()), offset(fset, bin.X.End()), "st.audErr"})
	finding, _ := audienceGuardFinding(mfset, mf)
	requireNamedFinding(t, finding, "*ast.SelectorExpr", "st.audErr")
}

// verifierPlacementSite — ветка else мягкого прохода в корне как он есть;
// судья построения на нём обязан молчать (законный близнец инъекций B).
func verifierPlacementSite(t *testing.T) ([]byte, *token.FileSet, *ast.BlockStmt) {
	t.Helper()
	src, fset, f := rootSource(t)
	require.Empty(t, verifierPlacementFinding(fset, f, constructorAssign(t, f)),
		"законный близнец — построение прямым оператором else — обязан молчать")
	fn := mainFunc(f)
	require.NotNil(t, fn, "предпосылка инъекции: в корне есть main")
	softs := undeclaredAudienceIfs(fn.Body)
	require.Len(t, softs, 1, "предпосылка инъекции: одна ветка мягкого прохода верхнего уровня")
	els, ok := softs[0].Else.(*ast.BlockStmt)
	require.True(t, ok, "предпосылка инъекции: у ветки мягкого прохода есть блок else")
	return src, fset, els
}

// wrapElseBody — обернуть содержимое блока else текстами до и после.
func wrapElseBody(t *testing.T, before, after string) (*token.FileSet, *ast.File) {
	t.Helper()
	src, fset, els := verifierPlacementSite(t)
	open, closing := offset(fset, els.Lbrace)+1, offset(fset, els.Rbrace)
	return mutateRoot(t, src, rootEdit{open, open, before}, rootEdit{closing, closing, after})
}

// requirePlacementFinding — находка называет координату построения и форму.
func requirePlacementFinding(t *testing.T, fset *token.FileSet, f *ast.File, form string) {
	t.Helper()
	assign := constructorAssign(t, f)
	finding := verifierPlacementFinding(fset, f, assign)
	requireNamedFinding(t, finding, fset.Position(assign.Pos()).String(), form)
	require.False(t, strings.Contains(finding, "\n"), "находка — одна строка")
}

// B1: построение в замыкании, которое зовётся только вне класса разработки.
func TestPrincipalVerifierInjection_ConstructionInAClosureUnderTheClassIsNamed(t *testing.T) {
	fset, f := wrapElseBody(t,
		"\n\t\twireVerifier := func() {",
		"}\n\t\tif cfg.AppEnv != \"dev\" {\n\t\t\twireVerifier()\n\t\t}\n\t")
	requirePlacementFinding(t, fset, f, "*ast.FuncLit")
}

// B2: построение в блоке ветки switch по классу окружения.
func TestPrincipalVerifierInjection_ConstructionInASwitchCaseBlockIsNamed(t *testing.T) {
	fset, f := wrapElseBody(t,
		"\n\t\tswitch cfg.AppEnv {\n\t\tcase \"dev\":\n\t\tdefault:\n\t\t\t{",
		"}\n\t\t}\n\t")
	requirePlacementFinding(t, fset, f, "*ast.CaseClause")
}

// softPassBranch — ветка мягкого прохода верхнего уровня в корне как он есть.
func softPassBranch(t *testing.T, f *ast.File) *ast.IfStmt {
	t.Helper()
	fn := mainFunc(f)
	require.NotNil(t, fn, "предпосылка инъекции: в корне есть main")
	softs := undeclaredAudienceIfs(fn.Body)
	require.Len(t, softs, 1, "предпосылка инъекции: одна ветка мягкого прохода верхнего уровня")
	return softs[0]
}

// audienceReceiverOf — получатель вызова `<x>.DeclaredTokenAudience()`.
func audienceReceiverOf(t *testing.T, e ast.Expr) ast.Expr {
	t.Helper()
	call, ok := e.(*ast.CallExpr)
	require.True(t, ok, "предпосылка инъекции: адресат взят вызовом")
	sel, ok := call.Fun.(*ast.SelectorExpr)
	require.True(t, ok, "предпосылка инъекции: вызов адресата — селектор")
	return sel.X
}

// ПОЛУЧАТЕЛЬ: страж и мягкий проход обязаны спрашивать адресата у ОДНОГО
// получателя. Каждая строка меняет ровно один факт о получателе; законный
// близнец — корень как он есть (audienceGuardSite утверждает его молчание).
func TestPrincipalVerifierInjection_GuardAndSoftPassAskOneReceiver(t *testing.T) {
	src, fset, call, _ := audienceGuardSite(t)
	_, _, f := rootSource(t)
	soft := softPassBranch(t, f)
	guardRecv := audienceReceiverOf(t, call.Args[1])
	softRecv := audienceReceiverOf(t, soft.Cond.(*ast.BinaryExpr).X)
	softAt := fset.Position(soft.Pos())
	initAt := softAt
	initAt.Column += len("if ")

	for _, tc := range []struct {
		name  string
		edits []rootEdit
		names []string
	}{
		{"страж спрашивает другого получателя",
			[]rootEdit{{offset(fset, guardRecv.Pos()), offset(fset, guardRecv.End()), "devCfg"}},
			[]string{"devCfg", "cfg", "получател"}},
		{"мягкий проход спрашивает другого получателя",
			[]rootEdit{{offset(fset, softRecv.Pos()), offset(fset, softRecv.End()), "devCfg"}},
			[]string{"devCfg", "cfg", "получател"}},
		{"получатель переписан между стражем и мягким проходом",
			[]rootEdit{{offset(fset, soft.Pos()), offset(fset, soft.Pos()), "cfg.TokenAudience = \"\"\n\t"}},
			[]string{softAt.String(), "cfg"}},
		{"получатель переписан в инициализации ветки мягкого прохода",
			[]rootEdit{{offset(fset, soft.Pos()) + len("if "), offset(fset, soft.Pos()) + len("if "), "cfg = devCfg; "}},
			[]string{initAt.String(), "cfg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mfset, mf := mutateRoot(t, src, tc.edits...)
			finding, _ := audienceGuardFinding(mfset, mf)
			requireNamedFinding(t, finding, tc.names...)
		})
	}
}

// Мягкий проход, завёрнутый в замыкание внутри своей ветки THEN, лексически
// лежит в ней, а исполняется там, где замыкание позовут: ключ не доказан.
// Законный близнец — корень как он есть; второй близнец — ветка switch внутри
// THEN: мягкий проход по-прежнему ключуется незаявленным адресатом.
func TestPrincipalVerifierInjection_SoftPassOutsideItsKeyIsNamed(t *testing.T) {
	src, fset, f := rootSource(t)
	finding, census := softPassFinding(fset, f)
	require.Empty(t, finding, "законный близнец — корень как он есть — обязан молчать")
	t.Log("близнец: " + census)
	soft := softPassBranch(t, f)
	require.NotEmpty(t, soft.Body.List, "предпосылка инъекции: у ветки мягкого прохода есть тело")
	st := soft.Body.List[0]
	from, to := offset(fset, st.Pos()), offset(fset, st.End())

	t.Run("замыкание внутри THEN", func(t *testing.T) {
		mfset, mf := mutateRoot(t, src,
			rootEdit{from, from, "warnSoft := func() {\n\t\t"},
			rootEdit{to, to, "\n\t\t}\n\t\t_ = warnSoft"})
		finding, _ := softPassFinding(mfset, mf)
		requireNamedFinding(t, finding, "*ast.FuncLit", mfset.Position(softPassSites(mf)[0].Pos()).String())
	})
	t.Run("ветка switch вместо if мягкого прохода", func(t *testing.T) {
		els, ok := soft.Else.(*ast.BlockStmt)
		require.True(t, ok, "предпосылка инъекции: у ветки мягкого прохода есть блок else")
		mfset, mf := mutateRoot(t, src,
			rootEdit{offset(fset, soft.Pos()), offset(fset, soft.Body.Lbrace) + 1,
				"switch {\n\tcase cfg.DeclaredTokenAudience() == \"\":"},
			rootEdit{offset(fset, soft.Body.Rbrace), offset(fset, els.Lbrace) + 1, "default:"})
		finding, _ := softPassFinding(mfset, mf)
		requireNamedFinding(t, finding, "*ast.CaseClause", mfset.Position(softPassSites(mf)[0].Pos()).String())
	})
	t.Run("близнец: switch внутри THEN", func(t *testing.T) {
		mfset, mf := mutateRoot(t, src,
			rootEdit{from, from, "switch cfg.AppEnv {\n\t\tdefault:\n\t\t"},
			rootEdit{to, to, "\n\t\t}"})
		finding, census := softPassFinding(mfset, mf)
		require.Empty(t, finding, "мягкий проход под switch внутри своей ветки THEN ключуется тем же адресатом")
		t.Log("близнец: " + census)
	})
}
