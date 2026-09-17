// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bakedledgersubject_injection_test.go — доказательство способности оси упасть
// И СМОЛЧАТЬ, на синтетическом составе дерева.
//
// Инъекция вносит ОДИН факт против законного близнеца: та же запись ведомости,
// тот же состав, различие ровно в том, лежит ли названный файл в дереве. Без
// второй половины ось ловила бы форму — «в ведомости есть запись», — и пустая
// ведомость перестала бы быть целью.
package repohygiene

import (
	"strings"
	"testing"
)

// injBakedTree — синтетический состав: один порождённый стаб.
func injBakedTree() (func(string) bool, int) {
	files := map[string]bool{"pkg/api/kacho/cloud/vpc/v1/network.pb.go": true}
	return func(rel string) bool { return files[rel] }, len(files)
}

// TestBakedLedgerInjection_EntryWithoutSubjectIsFound — ДЕФЕКТ: запись называет
// файл, которого в составе нет. Обязана находиться, с координатой.
func TestBakedLedgerInjection_EntryWithoutSubjectIsFound(t *testing.T) {
	t.Parallel()
	inTree, n := injBakedTree()
	ledger := []knownBakedDescriptor{
		{"pkg/api/corelib/api/v1/operation.pb.go", 1, "регенерация после публикации фундамента"},
	}
	stale, census := JudgeBakedLedgerSubjects(ledger, inTree, n)
	if census.Entries != 1 || census.TreeFiles != 1 {
		t.Fatalf("перепись не сошлась: %s", census)
	}
	if len(stale) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — ось не способна упасть на записи без предмета", len(stale))
	}
	if !strings.Contains(stale[0], "pkg/api/corelib/api/v1/operation.pb.go") {
		t.Fatalf("находка не называет координату: %q", stale[0])
	}
	if census.WithSubject != 0 {
		t.Fatalf("перепись засчитала предмет, которого нет: %s", census)
	}
}

// TestBakedLedgerInjection_EntryWithASubjectIsSilent — ЗАКОННЫЙ БЛИЗНЕЦ: та же
// запись, названный файл в составе есть. Ось молчит.
func TestBakedLedgerInjection_EntryWithASubjectIsSilent(t *testing.T) {
	t.Parallel()
	inTree, n := injBakedTree()
	ledger := []knownBakedDescriptor{
		{"pkg/api/kacho/cloud/vpc/v1/network.pb.go", 1, "регенерация после публикации фундамента"},
	}
	stale, census := JudgeBakedLedgerSubjects(ledger, inTree, n)
	if len(stale) != 0 {
		t.Fatalf("законная запись объявлена находкой (%d): %s", len(stale), FormatBakedLedgerStale(stale))
	}
	if census.WithSubject != 1 {
		t.Fatalf("перепись не засчитала живой предмет: %s", census)
	}
}

// TestBakedLedgerInjection_AnEmptyLedgerIsTheGoalNotAFailure — пустая ведомость
// проходит. Отказ на ней подталкивал бы держать запись ради зелёного, то есть
// возвращал бы послабление без предмета — ровно то, что ось стережёт.
func TestBakedLedgerInjection_AnEmptyLedgerIsTheGoalNotAFailure(t *testing.T) {
	t.Parallel()
	inTree, n := injBakedTree()
	stale, census := JudgeBakedLedgerSubjects(nil, inTree, n)
	if len(stale) != 0 {
		t.Fatalf("пустая ведомость объявлена находкой: %s", FormatBakedLedgerStale(stale))
	}
	if census.Entries != 0 || census.TreeFiles != 1 {
		t.Fatalf("перепись пустой ведомости молчит о прочитанном: %s", census)
	}
}

// TestBakedLedgerInjection_TheSameFileTwiceIsFound — две записи об одном файле
// истекут порознь: одна снимется вместе с предметом, вторая переживёт его.
func TestBakedLedgerInjection_TheSameFileTwiceIsFound(t *testing.T) {
	t.Parallel()
	inTree, n := injBakedTree()
	ledger := []knownBakedDescriptor{
		{"pkg/api/kacho/cloud/vpc/v1/network.pb.go", 1, "регенерация после публикации фундамента"},
		{"pkg/api/kacho/cloud/vpc/v1/network.pb.go", 1, "она же второй записью"},
	}
	stale, _ := JudgeBakedLedgerSubjects(ledger, inTree, n)
	if len(stale) != 1 {
		t.Fatalf("находок %d, ожидалась 1 — удвоенная запись не опознана", len(stale))
	}
	if !strings.Contains(stale[0], "дважды") {
		t.Fatalf("находка не называет предмет: %q", stale[0])
	}
}
