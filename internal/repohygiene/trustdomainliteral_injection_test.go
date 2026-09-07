// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainliteral_injection_test.go — доказательство того, что
// TestTrustDomainIsDeclaredNotCompiled СПОСОБЕН упасть, и падает он на существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanTrustDomainLiterals), что и гейт:
// доказательство, проверяющее вторую реализацию, доказывает вторую реализацию.
//
// Прогонов ТРИ, а не два, и третий обязателен: без него молчание распознавателя
// на законном близнеце неотличимо от молчания мёртвого разбора.
//
//	control    — чистый файл: находок ноль, а перепись НЕ ноль;
//	injection  — внесён домен, каждой из двух форм: находка с координатой;
//	legitimate — форма личности, проза и потребитель величины: молчание.
package repohygiene

import (
	"strings"
	"testing"
)

// trustDomainInjectedDefect — домен доверия, объявленный кодом, ОБЕИМИ формами.
//
// Псевдоним пакета-владельца здесь не украшение: разбор по ИМЕНИ пакета вторую
// находку не увидел бы, и обойти гейт можно было бы одной буквой в объявлении
// импорта.
const trustDomainInjectedDefect = `package authzguard

import (
	gs "github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

const sanTrustPrefix = "spiffe://kacho.cloud/ns/"

func domain() gs.TrustDomain {
	return gs.NewTrustDomain("kacho.cloud")
}
`

// trustDomainInjectedLegitimate — то же место БЕЗ объявления домена, вместе с
// законными соседями. Каждый сосед — свой способ обмануть разбор:
//
//   - комментарий, называющий ФОРМУ личности вместе с доменом: проза, которая
//     обязана остаться, иначе форму SAN негде прочитать;
//   - потребитель величины — `NewTrustDomain(cfg.TrustDomain)`: домен приезжает
//     из установки, литерала нет;
//   - строка, начинающаяся теми же буквами, но схемы не несущая.
const trustDomainInjectedLegitimate = `package authzguard

import (
	gs "github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// SAN модуля имеет вид spiffe://kacho.cloud/ns/<ns>/sa/kacho-<svc> — это ПРОЗА:
// форму личности читает оператор, а не процесс.
const knobName = "authz.trust-domain"

func domain(cfg Config) gs.TrustDomain {
	return gs.NewTrustDomain(cfg.TrustDomain)
}
`

// trustDomainInjectedOwnerShape — файл ВЛАДЕЛЬЦА: форма личности без домена.
// Власть-заполнитель и её отсутствие домена не несут и установку ни к чему не
// обязывают.
const trustDomainInjectedOwnerShape = `package grpcsrv

const spiffeScheme = "spiffe://"

// SANShape — форма личности, которую пишет оператор.
const SANShape = "spiffe://<домен-доверия>/ns/<пространство>/sa/<учётка>"
`

// trustDomainInjectedOwnerConcrete — тот же владелец, но домен СКОМПИЛИРОВАН.
// Отличие от близнеца выше — ОДИН факт: власть названа доменом, а не
// заполнителем.
const trustDomainInjectedOwnerConcrete = `package grpcsrv

const kachoSpiffePrefix = "spiffe://kacho.cloud/"
`

// TestTrustDomainScannerFindsADeclaredDomain — сторона (а): внесённый дефект
// становится находкой, и находка несёт координату.
func TestTrustDomainScannerFindsADeclaredDomain(t *testing.T) {
	sites, census, err := ScanTrustDomainLiterals(
		"services/iam/internal/authzguard/fgaproxy.go", []byte(trustDomainInjectedDefect))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Literals == 0 || census.Calls == 0 {
		t.Fatalf("осмотрено литералов %d, вызовов %d — разбирается не то дерево",
			census.Literals, census.Calls)
	}
	if len(sites) != 2 {
		t.Fatalf("находок %d, ожидалось 2 (схема в литерале и домен в конструкторе): %+v",
			len(sites), sites)
	}
	byForm := map[string]TrustDomainSite{}
	for _, s := range sites {
		byForm[s.Form] = s
		if s.File == "" || s.Line == 0 {
			t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", s)
		}
		if s.Authority != "kacho.cloud" {
			t.Errorf("власть прочитана как %q, ожидалось %q: %+v", s.Authority, "kacho.cloud", s)
		}
		if !s.AuthorityIsConcrete {
			t.Errorf("домен опознан заполнителем — тогда гейт молчал бы на самом дефекте: %+v", s)
		}
	}
	if _, ok := byForm["literal"]; !ok {
		t.Errorf("форма `literal` не опознана: %+v", sites)
	}
	if _, ok := byForm["constructor"]; !ok {
		t.Errorf("форма `constructor` не опознана — псевдоним пакета-владельца обошёл разбор: %+v", sites)
	}
	if byForm["literal"].Line == byForm["constructor"].Line {
		t.Errorf("обе находки на одной строке — разбор считает не узлы: %+v", sites)
	}
}

// TestTrustDomainScannerIsSilentOnALegitimateTwin — сторона (б): законный
// близнец молчание не теряет.
func TestTrustDomainScannerIsSilentOnALegitimateTwin(t *testing.T) {
	sites, census, err := ScanTrustDomainLiterals(
		"services/iam/internal/authzguard/fgaproxy.go", []byte(trustDomainInjectedLegitimate))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Literals == 0 || census.Calls == 0 {
		t.Fatalf("осмотрено литералов %d, вызовов %d — молчание сказано ни о чём",
			census.Literals, census.Calls)
	}
	if len(sites) != 0 {
		t.Fatalf("законный близнец объявлен находкой (%+v) — гейт запрещал бы прозу о форме "+
			"личности и потребителя величины, то есть ровно то, ради чего он заведён", sites)
	}
}

// TestTrustDomainOwnerMayDeclareTheShapeButNotTheDomain — третий прогон: у
// владельца форма законна, а домен — нет. Отличие между двумя фикстурами —
// ОДИН факт: власть-заполнитель против власти-домена.
func TestTrustDomainOwnerMayDeclareTheShapeButNotTheDomain(t *testing.T) {
	shape, _, err := ScanTrustDomainLiterals("pkg/grpcsrv/trust_domain.go", []byte(trustDomainInjectedOwnerShape))
	if err != nil {
		t.Fatalf("разбор синтетики владельца: %v", err)
	}
	if len(shape) != 2 {
		t.Fatalf("у владельца найдено %d мест, ожидалось 2 (схема и форма): %+v", len(shape), shape)
	}
	for _, s := range shape {
		if s.AuthorityIsConcrete {
			t.Errorf("форма личности опознана доменом (%q) — тогда владельцу негде было бы "+
				"объявить форму, и гейт запрещал бы разбор личности вовсе: %+v", s.Authority, s)
		}
	}

	concrete, _, err := ScanTrustDomainLiterals("pkg/grpcsrv/cert_identity.go", []byte(trustDomainInjectedOwnerConcrete))
	if err != nil {
		t.Fatalf("разбор синтетики владельца: %v", err)
	}
	if len(concrete) != 1 {
		t.Fatalf("у владельца найдено %d мест, ожидалось 1: %+v", len(concrete), concrete)
	}
	if !concrete[0].AuthorityIsConcrete {
		t.Fatalf("скомпилированный у владельца домен опознан заполнителем — гейт молчал бы "+
			"на месте, которое связывает КАЖДУЮ установку: %+v", concrete[0])
	}
	if !strings.Contains(concrete[0].Value, "kacho.cloud") {
		t.Fatalf("значение прочитано не то: %+v", concrete[0])
	}
}
