// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// tokencheckcomposition_injection_test.go — доказательство того, что
// TestMandatoryTokenChecksAreDeclaredOnceAndConsumed СПОСОБЕН упасть, и падает он
// на существе.
//
// Инъекция гоняет ТЕ ЖЕ функции разбора, что и гейт: проба, повторяющая логику
// гейта своей копией, доказывала бы свойство копии.
//
// У гейта четыре независимых утверждения, и каждое доказывается парой:
//
//	                                      (а) дефект → находка   (б) законный близнец → молчание
//	перечень объявлен один раз            второе объявление      MissingChecks рядом не считается
//	построение опознано                   вызов через псевдоним  одноимённая функция чужого пакета
//	состав объявлен и полон               состав без срока       полный состав
//	проверка сверх перечня объяснена      без причины            с причиной рядом
package repohygiene

import (
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

// ── (1) перечень объявлен ровно один раз ────────────────────────────────────

// tokenCheckInjectedSecondList — ВТОРОЕ объявление перечня, рядом с законными
// соседями того же пакета.
//
// Законные близнецы здесь: MissingChecks (функция ПРО перечень, но не перечень)
// и mandatoryChecksCensusFloor (имя начинается так же, объявлением перечня не
// является).
const tokenCheckInjectedSecondList = `package verifier

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

const mandatoryChecksCensusFloor = 10

func MandatoryChecks() []tokenpolicy.Check {
	return []tokenpolicy.Check{tokenpolicy.CheckExpiry}
}

func MissingChecks(declared []tokenpolicy.Check) []tokenpolicy.Check { return nil }
`

// TestCheckListScannerFindsASecondDeclaration — сторона (а) первого утверждения.
func TestCheckListScannerFindsASecondDeclaration(t *testing.T) {
	found, census, err := ScanCheckListDeclarations(
		"synthetic/verifier.go", []byte(tokenCheckInjectedSecondList), tokenCheckListName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Decls == 0 {
		t.Fatalf("осмотрено ноль объявлений — разбирается не то дерево")
	}
	if len(found) != 1 {
		t.Fatalf("объявлений найдено %d, ожидалось 1 (второй дом перечня): %+v", len(found), found)
	}
	if found[0].Line == 0 || found[0].File == "" {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", found[0])
	}
}

// TestCheckListScannerIsSilentOnNeighboursOfTheList — сторона (б).
//
// Ни функция ПРО перечень, ни константа с похожим именем объявлением перечня не
// являются. Без этой половины гейт ловил бы слово, а не предмет.
func TestCheckListScannerIsSilentOnNeighboursOfTheList(t *testing.T) {
	const src = `package verifier

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

const mandatoryChecksCensusFloor = 10

var mandatoryChecksSeen = map[tokenpolicy.Check]bool{}

func MissingChecks(declared []tokenpolicy.Check) []tokenpolicy.Check { return nil }

func mandatoryChecksOf(v any) []tokenpolicy.Check { return nil }
`
	found, census, err := ScanCheckListDeclarations("synthetic/verifier.go", []byte(src), tokenCheckListName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Decls == 0 {
		t.Fatalf("осмотрено ноль объявлений — разбирается не то дерево")
	}
	if len(found) != 0 {
		t.Fatalf("разбор объявил перечнем то, что перечнем не является (%+v) — он ловит "+
			"слово, а не предмет, и будет отключён первым же ложным срабатыванием", found)
	}
}

// ── (2) построение опознано по пути импорта, а не по имени пакета ───────────

// TestVerifierConstructionScannerResolvesTheAlias — сторона (а).
//
// Псевдоним пакета задаёт вызывающий. Разбор, ключующийся на имя в исходнике,
// потерял бы построение от одного переименования импорта — и молчал бы об этом.
func TestVerifierConstructionScannerResolvesTheAlias(t *testing.T) {
	const src = `package main

import (
	verify "github.com/PRO-Robotech/kacho/services/registry/internal/clients/jwks"
	"github.com/PRO-Robotech/kacho/services/registry/internal/clients/other"
)

func build() {
	_ = verify.New(sources, aud)
	_ = other.New(nothing)
}
`
	producers := map[string]bool{
		tokenCheckModulePath + "/services/registry/internal/clients/jwks.New": true,
	}
	found, census, err := ScanVerifierConstructions("synthetic/main.go", []byte(src), producers)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Calls < 2 {
		t.Fatalf("осмотрено вызовов %d — разбирается не то дерево", census.Calls)
	}
	if len(found) != 1 {
		t.Fatalf("построений найдено %d, ожидалось ровно 1: %+v", len(found), found)
	}
	if !strings.HasSuffix(found[0].Producer, "/clients/jwks.New") {
		t.Errorf("производитель опознан как %q — псевдоним не разрешён", found[0].Producer)
	}
	if found[0].Line == 0 {
		t.Errorf("находка без строки: %+v", found[0])
	}
}

// TestVerifierConstructionScannerIsSilentOnASameNamedStranger — сторона (б).
//
// Одноимённая функция ЧУЖОГО пакета построением проверяющего не является. Разбор
// по имени `jwks.New` спутал бы их — и словарь производителей пришлось бы
// держать в двух написаниях.
func TestVerifierConstructionScannerIsSilentOnASameNamedStranger(t *testing.T) {
	const src = `package main

import jwks "github.com/some/other/jwks"

func build() { _ = jwks.New(anything) }
`
	producers := map[string]bool{
		tokenCheckModulePath + "/services/registry/internal/clients/jwks.New": true,
	}
	found, _, err := ScanVerifierConstructions("synthetic/main.go", []byte(src), producers)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("разбор принял одноимённую функцию чужого пакета за наше построение (%+v)", found)
	}
}

// ── (3) состав объявлен и полон ─────────────────────────────────────────────

// tokenCheckInjectedIncomplete — состав БЕЗ срока.
//
// Из всех недостающих проверок срок выбран не случайно: разбор токена, встретив
// срок, его проверит, а НЕ встретив — не возразит. Проба, подающая токен со
// сроком, этого свойства не измеряет вовсе.
const tokenCheckInjectedIncomplete = `package jwks

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

var implementedChecks = []tokenpolicy.Check{
	tokenpolicy.CheckAlgorithmAllowed,
	tokenpolicy.CheckKeyID,
	tokenpolicy.CheckSignature,
	tokenpolicy.CheckKeyBoundAlgorithm,
	tokenpolicy.CheckIssuer,
	tokenpolicy.CheckAudience,
	tokenpolicy.CheckTokenType,
	tokenpolicy.CheckNotBefore,
	tokenpolicy.CheckCriticalHeaders,
	tokenpolicy.CheckRevocation,
}

func (v *Verifier) DeclaredChecks() []tokenpolicy.Check { return implementedChecks }
`

// tokenCheckInjectedComplete — тот же файл с полным составом.
const tokenCheckInjectedComplete = `package jwks

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

var implementedChecks = []tokenpolicy.Check{
	tokenpolicy.CheckAlgorithmAllowed,
	tokenpolicy.CheckKeyID,
	tokenpolicy.CheckSignature,
	tokenpolicy.CheckKeyBoundAlgorithm,
	tokenpolicy.CheckIssuer,
	tokenpolicy.CheckAudience,
	tokenpolicy.CheckTokenType,
	tokenpolicy.CheckExpiry,
	tokenpolicy.CheckNotBefore,
	tokenpolicy.CheckCriticalHeaders,
	tokenpolicy.CheckRevocation,
}

func (v *Verifier) DeclaredChecks() []tokenpolicy.Check { return implementedChecks }
`

// tokenCheckInjectedConstants — сопоставление «имя константы → значение», как его
// читает гейт из объявления политики.
//
// Здесь оно СОБРАНО тем же разбором, что в гейте, а не выписано: выписанная
// копия разошлась бы с политикой молча — то есть ровно тем классом, который гейт
// и запрещает.
func tokenCheckInjectedConstants(t *testing.T) map[string]string {
	t.Helper()
	const policySrc = `package tokenpolicy

type Check string

const (
	CheckSignature Check = "signature"
	CheckAlgorithmAllowed Check = "algorithm-allowed"
	CheckKeyBoundAlgorithm Check = "key-bound-algorithm"
	CheckIssuer Check = "issuer"
	CheckAudience Check = "audience"
	CheckTokenType Check = "token-type"
	CheckExpiry Check = "expiry"
	CheckNotBefore Check = "not-before"
	CheckKeyID Check = "key-id"
	CheckCriticalHeaders Check = "critical-headers"
	CheckRevocation Check = "revocation"
	CheckSenderBinding Check = "sender-binding"
)
`
	consts, err := ScanCheckConstants("synthetic/policy.go", []byte(policySrc), tokenCheckTypeName)
	if err != nil {
		t.Fatalf("разбор констант синтетики: %v", err)
	}
	if len(consts) < 10 {
		t.Fatalf("прочитано констант %d — сверять состав нечем", len(consts))
	}
	return consts
}

// declaredOf — состав, названный файлом, в значениях перечня.
func declaredOf(t *testing.T, src string, consts map[string]string) ([]TokenCheckSite, []CheckNaming, []tokenpolicy.Check) {
	t.Helper()
	decls, namings, _, err := ScanCheckComposition(
		"synthetic/checks.go", []byte(src), tokenCheckPolicyImport, tokenCheckDeclName)
	if err != nil {
		t.Fatalf("разбор состава синтетики: %v", err)
	}
	uniq := map[tokenpolicy.Check]bool{}
	for _, n := range namings {
		if v, ok := consts[n.Ident]; ok {
			uniq[tokenpolicy.Check(v)] = true
		}
	}
	var out []tokenpolicy.Check
	for c := range uniq {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return decls, namings, out
}

// TestCheckCompositionScannerFindsAMissingMandatoryCheck — сторона (а).
func TestCheckCompositionScannerFindsAMissingMandatoryCheck(t *testing.T) {
	consts := tokenCheckInjectedConstants(t)
	decls, _, declared := declaredOf(t, tokenCheckInjectedIncomplete, consts)
	if len(decls) != 1 {
		t.Fatalf("объявлений состава найдено %d, ожидалось 1: %+v", len(decls), decls)
	}
	missing := tokenpolicy.MissingChecks(declared)
	if len(missing) != 1 || missing[0] != tokenpolicy.CheckExpiry {
		t.Fatalf("нехватка обязательной проверки не опознана: не хватает %v (объявлено %v)",
			missing, declared)
	}
}

// TestCheckCompositionScannerIsSilentOnACompleteComposition — сторона (б).
func TestCheckCompositionScannerIsSilentOnACompleteComposition(t *testing.T) {
	consts := tokenCheckInjectedConstants(t)
	decls, _, declared := declaredOf(t, tokenCheckInjectedComplete, consts)
	if len(decls) != 1 {
		t.Fatalf("объявлений состава найдено %d, ожидалось 1 — гейт потерял предмет: %+v",
			len(decls), decls)
	}
	if missing := tokenpolicy.MissingChecks(declared); len(missing) != 0 {
		t.Fatalf("разбор объявил нехватку на ПОЛНОМ составе (%v) — он отвергает исправный "+
			"случай и будет отключён первым же ложным срабатыванием", missing)
	}
}

// TestCheckCompositionScannerIgnoresAFileThatDoesNotImportThePolicy — третья
// сторона того же утверждения: файл, не импортирующий политику, о составе
// ничего не объявляет, и приписывать ему состав нельзя.
func TestCheckCompositionScannerIgnoresAFileThatDoesNotImportThePolicy(t *testing.T) {
	const src = `package jwks

type tokenpolicy struct{}

func (v *Verifier) DeclaredChecks() []tokenpolicy.Check { return nil }
`
	decls, namings, _, err := ScanCheckComposition(
		"synthetic/checks.go", []byte(src), tokenCheckPolicyImport, tokenCheckDeclName)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if len(decls) != 0 || len(namings) != 0 {
		t.Fatalf("разбор приписал состав файлу, который политику не импортирует "+
			"(объявлений %d, упоминаний %d) — совпало ИМЯ, а не предмет", len(decls), len(namings))
	}
}

// ── (4) проверка сверх перечня объявляет причину ────────────────────────────

// TestCheckCompositionScannerFlagsAnUnreasonedExtra — сторона (а).
func TestCheckCompositionScannerFlagsAnUnreasonedExtra(t *testing.T) {
	const src = `package jwks

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

var implementedChecks = []tokenpolicy.Check{
	tokenpolicy.CheckExpiry,
	tokenpolicy.CheckSenderBinding,
}

func (v *Verifier) DeclaredChecks() []tokenpolicy.Check { return implementedChecks }
`
	consts := tokenCheckInjectedConstants(t)
	_, namings, _ := declaredOf(t, src, consts)
	var extraReasoned, extraSeen int
	for _, n := range namings {
		if n.Ident != "CheckSenderBinding" {
			continue
		}
		extraSeen++
		if n.Reasoned {
			extraReasoned++
		}
	}
	if extraSeen != 1 {
		t.Fatalf("упоминаний проверки сверх перечня найдено %d, ожидалось 1: %+v", extraSeen, namings)
	}
	if extraReasoned != 0 {
		t.Fatalf("разбор счёл причину объявленной там, где комментария нет вовсе — " +
			"молчаливое расхождение прошло бы как объяснённое")
	}
}

// TestCheckCompositionScannerAcceptsAReasonedExtra — сторона (б).
//
// Расхождение поверхностей законно, пока оно ОБЪЯВЛЕНО. Без этой половины гейт
// запрещал бы всякое отличие, и первое же законное отличие его отключило бы.
func TestCheckCompositionScannerAcceptsAReasonedExtra(t *testing.T) {
	const src = `package jwks

import "github.com/PRO-Robotech/kacho/pkg/tokenpolicy"

var implementedChecks = []tokenpolicy.Check{
	tokenpolicy.CheckExpiry,
	// Причина сверх перечня: эта поверхность принимает токен машинного
	// принципала, и привязка к предъявленному материалу здесь обязательна.
	tokenpolicy.CheckSenderBinding,
}

func (v *Verifier) DeclaredChecks() []tokenpolicy.Check { return implementedChecks }
`
	consts := tokenCheckInjectedConstants(t)
	_, namings, _ := declaredOf(t, src, consts)
	for _, n := range namings {
		if n.Ident == "CheckSenderBinding" && !n.Reasoned {
			t.Fatalf("причина объявлена комментарием прямо над проверкой, а разбор её "+
				"не увидел (%+v) — гейт краснел бы на законном отличии", n)
		}
	}
}
