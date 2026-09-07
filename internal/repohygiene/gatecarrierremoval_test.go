// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"
)

// gatecarrierremoval_test.go — держатель условия «носитель гейта не исчезает
// молча». Предмет, устройство разбора и границы — в шапке
// gatecarrierremoval.go; здесь только добыча входа, перепись и вердикт.

// TestGateCarrierIsNotRemovedSilently — снятый носитель гейта обязан быть
// объявлен надгробием.
//
// Перепись печатает ТРИ величины, а не одну: носителей прочитано · снято
// относительно базы · записей надгробия. Одного числа мало ровно в том случае,
// ради которого гейт заведён: «снято 0» при непрочитанном корпусе выглядит так
// же, как чистое дерево.
func TestGateCarrierIsNotRemovedSilently(t *testing.T) {
	root := repoRoot(t)

	// Присутствие рабочего дерева git и «ствол не резолвится — ОТКАЗ, а не
	// пропуск» держит соседний помощник; базу он даёт стволовую, а этому гейту
	// нужна точка ответвления линии (см. шапку разбора).
	_ = requireTrunkRef(t, root)

	base, how, err := gateCarrierBase(root)
	if err != nil {
		t.Fatalf("определить базу: %v", err)
	}

	present, err := gateCarrierCensus(root)
	if err != nil {
		t.Fatalf("перепись носителей гейта: %v", err)
	}

	deleted, err := deletedGateCarriers(root, base)
	if err != nil {
		t.Fatalf("перечислить снятые носители: %v", err)
	}

	ledger, err := readGateCarrierLedger(root)
	if err != nil {
		t.Fatalf("прочитать %s: %v", gateCarrierLedgerName, err)
	}
	rows := 0
	if ledger != nil {
		rows = len(ledger.Retired)
	}

	t.Logf("осмотрено: носителей в %s — %d; база: %s; снято относительно неё — %d; записей %s — %d",
		gateCorpusDir, len(present), how, len(deleted), gateCarrierLedgerName, rows)

	everRemoved := func(c string) (bool, error) { return carrierWasEverRemoved(root, c) }
	for _, f := range judgeGateCarrierRemoval(deleted, present, ledger, everRemoved) {
		t.Error(f)
	}
}
