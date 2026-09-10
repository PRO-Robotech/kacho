// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// quotashowposture_injection_test.go — доказательство способности гейта упасть и
// смолчать (#2515). Инъекция настоящей формой из дерева, с законным близнецом.
//
// Каждый отрицательный случай отличается от ПОЛОЖИТЕЛЬНОГО БЛИЗНЕЦА ровно одним
// фактом: иначе неизвестно, какой из двух дал красное, и вердикт недействителен,
// хотя выглядит обычным зелёным.

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// quotaShowOwnerOf — вердикт по ОДНОМУ разобранному корню.
func quotaShowOwnerOf(t *testing.T, src string) quotaShowOwner {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)
	return quotaShowOwner{Service: "svc", Facts: quotaShowFactsIn(file)}
}

// quotaShowLegalRoot — форма, какой она стоит в дереве после #2515: посадка
// выведена из объявления, отсутствие спрошено, литерала нет.
//
// Все отрицательные случаи ниже — ЭТА форма минус один факт.
const quotaShowLegalRoot = `package main

func buildEdge() edge {
	return edge{ReadPosture: corequota.ReadPosture(authority, "vpc")}
}

func handlerOrNil(g *quota.Guard, posture quotapb.Posture) *quotaapp.Handler {
	if g == nil && !posture.AuthorityIsAbsent() {
		return nil
	}
	return quotaapp.NewHandler(g, posture)
}

func register(srv grpc.ServiceRegistrar, svcs *services) {
	if svcs.quotaHandler != nil {
		vpcv1.RegisterQuotaServiceServer(srv, svcs.quotaHandler)
	}
}`

// TestQuotaShowPosture_LegalRootIsSilent — законный близнец.
//
// Без него «находок нет» было бы неотличимо от «гейт ничего не различает».
func TestQuotaShowPosture_LegalRootIsSilent(t *testing.T) {
	t.Parallel()
	o := quotaShowOwnerOf(t, quotaShowLegalRoot)
	require.True(t, o.Facts.Registers, "предмет обязан быть распознан, иначе молчание беспредметно")
	require.Empty(t, o.Violations())
}

// TestQuotaShowPosture_NoDeriveIsAFinding — посадка не выведена из объявления.
func TestQuotaShowPosture_NoDeriveIsAFinding(t *testing.T) {
	t.Parallel()
	// Один факт: перевод не зовётся, посадка решается своим признаком корня.
	src := replaceOnce(t, quotaShowLegalRoot,
		`corequota.ReadPosture(authority, "vpc")`,
		`postureOfMyOwnGuess(authority)`)
	v := quotaShowOwnerOf(t, src).Violations()
	require.Len(t, v, 1)
	require.Contains(t, v[0], quotaShowPostureDeriveVerb,
		"находка обязана называть предмет, а не симптом")
}

// TestQuotaShowPosture_NoAbsenceQuestionIsAFinding — корень не спрашивает про
// объявленное отсутствие, то есть витрина остаётся «возможности нет».
func TestQuotaShowPosture_NoAbsenceQuestionIsAFinding(t *testing.T) {
	t.Parallel()
	// Один факт: вопрос снят, и обработчик снова строится только под полосу.
	src := replaceOnce(t, quotaShowLegalRoot,
		`	if g == nil && !posture.AuthorityIsAbsent() {`,
		`	if g == nil {`)
	v := quotaShowOwnerOf(t, src).Violations()
	require.Len(t, v, 1)
	require.Contains(t, v[0], quotaShowAbsenceAsk)
}

// TestQuotaShowPosture_HardcodedDeclaredIsAFinding — тихий способ вернуть
// дефект: посадка «передана» литералом, компилятор доволен.
func TestQuotaShowPosture_HardcodedDeclaredIsAFinding(t *testing.T) {
	t.Parallel()
	// Один факт: к законной форме добавлен литерал посадки.
	src := replaceOnce(t, quotaShowLegalRoot,
		`	return quotaapp.NewHandler(g, posture)`,
		`	return quotaapp.NewHandler(g, quotapb.AuthorityDeclared())`)
	v := quotaShowOwnerOf(t, src).Violations()
	require.Len(t, v, 1)
	require.Contains(t, v[0], quotaShowDeclaredLiteral)
}

// TestQuotaShowPosture_RootWithoutShowcaseIsSilent — освобождение по ПРЕДМЕТУ,
// а не по ведомости: требовать посадку от корня, витрины не выставляющего,
// значило бы завести освобождение, которому нечего освобождать.
func TestQuotaShowPosture_RootWithoutShowcaseIsSilent(t *testing.T) {
	t.Parallel()
	o := quotaShowOwnerOf(t, `package main

func register(srv grpc.ServiceRegistrar, svcs *services) {
	vpcv1.RegisterNetworkServiceServer(srv, svcs.networkHandler)
}`)
	require.False(t, o.Facts.Registers)
	require.Empty(t, o.Violations(), "у корня без витрины предмета нет")
}

// TestQuotaShowPosture_MentionIsNotACall — гейт судит УЗЕЛ ВЫЗОВА.
//
// Все три имени стоят в прозе — в объяснении самого гейта и в комментариях
// корней, — а `edge.ReadPosture` есть обращение к ПОЛЮ. Предикат по подстроке
// краснел бы на собственном объяснении и зеленел бы на поле.
func TestQuotaShowPosture_MentionIsNotACall(t *testing.T) {
	t.Parallel()
	o := quotaShowOwnerOf(t, `package main

// Здесь названы RegisterQuotaServiceServer, ReadPosture, AuthorityIsAbsent и
// AuthorityDeclared — все четыре имени, и ни одно не является вызовом.
func explain() {
	const s = "RegisterQuotaServiceServer ReadPosture AuthorityIsAbsent AuthorityDeclared"
	_ = s
	_ = edge.ReadPosture
}`)
	require.False(t, o.Facts.Registers, "имя в комментарии и в строке вызовом не является")
	require.False(t, o.Facts.Derives, "обращение к полю вызовом перевода не является")
	require.False(t, o.Facts.AsksAbsent)
	require.False(t, o.Facts.Declares)
}

// replaceOnce — подмена ровно одного вхождения; отсутствие образца есть отказ,
// иначе инъекция молча перестала бы вносить дефект и зеленела бы навсегда.
func replaceOnce(t *testing.T, src, old, updated string) string {
	t.Helper()
	require.Contains(t, src, old, "образец инъекции исчез из формы — инъекция беспредметна")
	return strings.Replace(src, old, updated, 1)
}
