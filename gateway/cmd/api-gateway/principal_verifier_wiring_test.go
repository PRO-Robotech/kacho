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
// текстом, как у соседних гейтов этого пакета. Разбор `parseMain` разрешает
// объекты, поэтому чтение ошибки конструктора отличается от одноимённого
// идентификатора другой области видимости.
package main

import (
	"go/ast"
	"go/token"
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

// constructorErrorIdent — идентификатор ошибки, которую корень получает от
// NewJWTVerifier. Ровно одно такое присваивание.
func constructorErrorIdent(t *testing.T, f *ast.File) *ast.Ident {
	t.Helper()
	var found []*ast.Ident
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 2 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewJWTVerifier" {
			return true
		}
		if id, ok := as.Lhs[1].(*ast.Ident); ok {
			found = append(found, id)
		}
		return true
	})
	require.Len(t, found, 1,
		"корень обязан строить проверяющего подпись ровно одним присваиванием `v, err := middleware.NewJWTVerifier(...)`")
	return found[0]
}

// exitsTheProcess — содержит ли тело вызов, завершающий процесс.
func exitsTheProcess(body *ast.BlockStmt) bool {
	exits := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
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
		if (pkg.Name == "os" && sel.Sel.Name == "Exit") ||
			(pkg.Name == "log" && strings.HasPrefix(sel.Sel.Name, "Fatal")) {
			exits = true
		}
		return true
	})
	return exits
}

// refusalGuards — ветки `if <err> != nil { … завершить процесс … }` по ошибке
// конструктора.
func refusalGuards(f *ast.File, errIdent *ast.Ident) []*ast.IfStmt {
	var out []*ast.IfStmt
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := ifs.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.NEQ {
			return true
		}
		id, ok := bin.X.(*ast.Ident)
		if !ok || id.Obj == nil || id.Obj != errIdent.Obj {
			return true
		}
		if nilID, ok := bin.Y.(*ast.Ident); !ok || nilID.Name != "nil" {
			return true
		}
		if exitsTheProcess(ifs.Body) {
			out = append(out, ifs)
		}
		return true
	})
	return out
}

// Отказ конструктора роняет старт в ЛЮБОМ классе окружения: ошибка
// конструктора читается только условием ветки, завершающей процесс, и её
// телом. Ни в стража по классу окружения, ни в признак «провязан ли
// проверяющий» ниже по корню она не уходит — признак провязки несёт сам
// проверяющий.
func TestPrincipalVerifier_ConstructorRefusalRefusesStartInEveryClass(t *testing.T) {
	fset, f := parseMain(t)
	errIdent := constructorErrorIdent(t, f)
	guards := refusalGuards(f, errIdent)
	require.Len(t, guards, 1,
		"ошибка конструктора обязана читаться ровно одной веткой `if %s != nil { … завершить процесс … }`",
		errIdent.Name)
	guard := guards[0]

	refs, stray := 0, []string{}
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id == errIdent || id.Obj == nil || id.Obj != errIdent.Obj {
			return true
		}
		refs++
		if id.Pos() < guard.Pos() || id.Pos() >= guard.End() {
			stray = append(stray, fset.Position(id.Pos()).String())
		}
		return true
	})
	t.Logf("ОСМОТРЕНО: чтений ошибки конструктора %d · вне ветки отказа старта %d", refs, len(stray))
	require.NotZero(t, refs, "ошибка конструктора не читается вовсе — проба не нашла своего предмета")
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

// namesUndeclaredAudience — условие `cfg.DeclaredTokenAudience() == ""`.
func namesUndeclaredAudience(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return false
	}
	call, ok := bin.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "DeclaredTokenAudience" {
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
	for _, ifs := range enclosingIfBodies(f, site.Pos()) {
		if namesUndeclaredAudience(ifs.Cond) {
			keyed = true
		}
	}
	require.True(t, keyed,
		"мягкий проход %s лежит вне ветки `if cfg.DeclaredTokenAudience() == \"\"` — он ключуется не "+
			"своим производителем", fset.Position(site.Pos()))
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
