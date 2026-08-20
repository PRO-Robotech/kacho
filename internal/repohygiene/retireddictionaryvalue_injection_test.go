// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retiredValueTinyContract материализует синтетический контракт: один .proto,
// содержимое которого задаёт вызывающий.
func retiredValueTinyContract(t *testing.T, body string) (root string, files []string) {
	t.Helper()
	root = t.TempDir()
	p := filepath.Join(root, "proto", "kacho", "cloud", "demo", "v1", "demo.proto")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root, []string{p}
}

var retiredValueTinyLedger = []RetiredDictionaryValue{{
	Value: "jit_revoke", Dictionary: "demo.q.op", By: "000_test.sql", Reason: "снято в тесте",
}}

// TestRetiredDictionaryValueInjection_RedOnTheRealShape — ИНЪЕКЦИЯ НАСТОЯЩИМ
// ВХОДОМ: та самая форма, в которой дефект жил в дереве (#796) — перечисление
// «Canonical values», где снятое значение стоит между живыми.
func TestRetiredDictionaryValueInjection_RedOnTheRealShape(t *testing.T) {
	root, files := retiredValueTinyContract(t, `syntax = "proto3";
package kacho.cloud.demo.v1;

message InvalidateSubjectRequest {
  // EventType — diagnostic only.
  // Canonical values: binding_revoke / binding_grant / jit_revoke /
  // group_member_change.
  string event_type = 4;
}
`)
	findings, census, err := AuditRetiredDictionaryValues(root, files, retiredValueTinyLedger)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("воссозданный дефект дал %d находок, ожидалась одна: %v", len(findings), findings)
	}
	got := findings[0].String()
	for _, want := range []string{"demo.proto", "jit_revoke", "снято в тесте", ":6"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: %s", want, got)
		}
	}
	if census.Files != 1 || census.Lines == 0 {
		t.Errorf("перепись не сошлась: файлов %d, строк %d", census.Files, census.Lines)
	}
}

// TestRetiredDictionaryValueInjection_SilentOnTheLegitimateTwin — вторая сторона
// контроля: ТА ЖЕ форма, тот же файл, то же перечисление — но все значения
// живые. Гейт, ключующийся на форме («Canonical values»), здесь покраснеет.
func TestRetiredDictionaryValueInjection_SilentOnTheLegitimateTwin(t *testing.T) {
	root, files := retiredValueTinyContract(t, `syntax = "proto3";
package kacho.cloud.demo.v1;

message InvalidateSubjectRequest {
  // EventType — diagnostic only.
  // Canonical values: binding_revoke / binding_grant / group_member_change.
  string event_type = 4;
}
`)
	findings, census, err := AuditRetiredDictionaryValues(root, files, retiredValueTinyLedger)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("законная конструкция той же формы дала %d находок — анализатор ловит форму, "+
			"а не существо: %s", len(findings), findings[0].String())
	}
	// Премиса самого контроля: молчание получено на ПРОЧИТАННОМ входе.
	if census.Files == 0 || census.Lines == 0 {
		t.Fatalf("контроль молчал на пустом входе (файлов %d, строк %d) — он ничего не доказывает",
			census.Files, census.Lines)
	}
}

// TestRetiredDictionaryValueInjection_EmptyInputsAreRefusals — обе предпосылки
// анализатора обязаны быть отказом, а не «нулём находок».
func TestRetiredDictionaryValueInjection_EmptyInputsAreRefusals(t *testing.T) {
	root, files := retiredValueTinyContract(t, "syntax = \"proto3\";\n")

	if _, _, err := AuditRetiredDictionaryValues(root, files, nil); err == nil {
		t.Error("пустое надгробие прошло как «ноль находок» — анализатор инертен и молчит об этом")
	}
	if _, _, err := AuditRetiredDictionaryValues(root, nil, retiredValueTinyLedger); err == nil {
		t.Error("пустой корпус прошёл как «ноль находок» — «не нашли» неотличимо от «не читали»")
	}
	incomplete := []RetiredDictionaryValue{{Value: "x", Dictionary: "d", By: "m"}}
	if _, _, err := AuditRetiredDictionaryValues(root, files, incomplete); err == nil {
		t.Error("надгробие без причины принято — надпись обязательна, иначе оно ничего не сообщает")
	}
}

// TestRetiredDictionaryValueInjection_LiveDictionaryContradictionIsSeen —
// самоистечение в обратную сторону: значение, вернувшееся в живой словарь.
func TestRetiredDictionaryValueInjection_LiveDictionaryContradictionIsSeen(t *testing.T) {
	live := map[string]map[string][]string{
		"demo.q": {"op": {"binding_grant", "jit_revoke"}},
	}
	if back := LiveIn(retiredValueTinyLedger, live); len(back) != 1 {
		t.Fatalf("вернувшееся в словарь значение не замечено: %v", back)
	}
	clean := map[string]map[string][]string{"demo.q": {"op": {"binding_grant"}}}
	if back := LiveIn(retiredValueTinyLedger, clean); len(back) != 0 {
		t.Errorf("на чистом словаре объявлено противоречие: %v", back)
	}
}
