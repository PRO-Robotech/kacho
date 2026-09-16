// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foreignproductnameincontract_injection_test.go — доказательство способности
// гейта упасть И СМОЛЧАТЬ.
//
// Инъекция вносит ОДИН факт против законного близнеца: та же строка, тот же
// файл, различие ровно в имени. Без второй половины гейт ловил бы форму —
// «в комментарии есть слово Storage», — и первый же ложный срабат его отключил
// бы.
package repohygiene

import (
	"strings"
	"testing"
)

// injForeignContract — законный комментарий контракта: домен Kachō назван своим
// именем.
const injForeignContractLegal = `syntax = "proto3";

// Cancels the specified operation.
//
// Storage volumes keep their own operations; cancelling one is refused once it
// has reached a terminal state.
rpc Cancel (CancelOperationRequest) returns (Operation);
`

// injForeignContractDefect — ТОТ ЖЕ текст, одно слово заменено на продуктовое
// имя чужой платформы.
const injForeignContractDefect = `syntax = "proto3";

// Cancels the specified operation.
//
// Note that currently Object Storage API does not support cancelling operations.
rpc Cancel (CancelOperationRequest) returns (Operation);
`

// TestForeignProductInjection_ForeignNameIsFoundWithItsCoordinate — дефект
// находится, и находка называет файл, строку и имя.
func TestForeignProductInjection_ForeignNameIsFoundWithItsCoordinate(t *testing.T) {
	t.Parallel()
	sites, lines := ScanForeignProductNames(
		"proto/corelib/operation/operation_service.proto",
		[]byte(injForeignContractDefect),
		ForeignProductNames(),
	)
	if lines == 0 {
		t.Fatal("прочитано ноль строк — разбор не состоялся, и молчание было бы о непрочитанном")
	}
	if len(sites) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — гейт не способен упасть на внесённом дефекте", len(sites))
	}
	got := sites[0]
	if got.Name != "Object Storage" {
		t.Fatalf("находка называет %q, а внесено %q", got.Name, "Object Storage")
	}
	if got.Line != 5 {
		t.Fatalf("находка называет строку %d, дефект внесён в строку 5 — "+
			"координата ведёт читателя не туда", got.Line)
	}
	if got.File != "proto/corelib/operation/operation_service.proto" {
		t.Fatalf("находка называет файл %q", got.File)
	}
	if !strings.Contains(got.Instead, "Storage") {
		t.Fatalf("находка не говорит, чем заменить: %q", got.Instead)
	}
}

// TestForeignProductInjection_TheLegalTwinIsSilent — законный близнец: тот же
// комментарий про тот же предмет, имя домена своё. Гейт молчит.
func TestForeignProductInjection_TheLegalTwinIsSilent(t *testing.T) {
	t.Parallel()
	sites, lines := ScanForeignProductNames(
		"proto/corelib/operation/operation_service.proto",
		[]byte(injForeignContractLegal),
		ForeignProductNames(),
	)
	if lines == 0 {
		t.Fatal("прочитано ноль строк — молчание было бы о непрочитанном, а не о чистом тексте")
	}
	if len(sites) != 0 {
		t.Fatalf("законный близнец объявлен находкой (%d): гейт ловит слово, а не продуктовое имя", len(sites))
	}
}

// TestForeignProductInjection_OurOwnDomainNameIsNotAFinding — собственное
// описание Kachō с уточнением при балансировщике. Консольная ось
// `load-balancer-taxonomy` объявила бы это находкой; словарь этого гейта —
// намеренно уже, и граница проверена, а не подразумевается.
func TestForeignProductInjection_OurOwnDomainNameIsNotAFinding(t *testing.T) {
	t.Parallel()
	src := "// Kachō Network Load Balancer control-plane.\n"
	sites, lines := ScanForeignProductNames("services/nlb/x.proto", []byte(src), ForeignProductNames())
	if lines == 0 {
		t.Fatal("прочитано ноль строк")
	}
	if len(sites) != 0 {
		t.Fatalf("собственное описание Kachō объявлено находкой (%d) — словарь вышел за "+
			"продуктовые имена ЧУЖИХ платформ", len(sites))
	}
}

// TestForeignProductInjection_EveryNameOfTheVocabularyIsFound — распознаватель
// знает КАЖДОЕ объявленное написание, а не первое. Словарь, чья запись не
// проверена, — слепая зона, выданная вперёд.
func TestForeignProductInjection_EveryNameOfTheVocabularyIsFound(t *testing.T) {
	t.Parallel()
	names := ForeignProductNames()
	if len(names) == 0 {
		t.Fatal("словарь пуст")
	}
	for _, n := range names {
		src := "// A comment that names " + n.Name + " and nothing else.\n"
		sites, _ := ScanForeignProductNames("proto/x.proto", []byte(src), names)
		if len(sites) == 0 {
			t.Errorf("запись словаря %q не находится собственной инъекцией — "+
				"она ничего не стережёт", n.Name)
		}
	}
}
