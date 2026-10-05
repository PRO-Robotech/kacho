// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nlbjournalliteral_s1a7_test.go — полоса RED S1-A7 (issue-2918, NTF-3, Н3-Ф2):
// литеральных вставок в журнал nlb в не-тестовом Go — 0. Оба прежних места
// (`repo/kacho/pg/outbox_emitter.go` — эмиттер модуля, через который идут все
// глаголы, и `apps/kacho/jobs/free_ip_runner.go` `emitReconcileFinalize`)
// переводятся на функцию фундамента с дескриптором nlb (замысел З5, З6;
// приёмка NTF-3 §1.13: «у nlb 2 → 0»).
//
// Распознаватель — тот же, что у переписи форм записи журнала
// (`journalwriteforms.go`); здесь не заводится второго. Предмет у пробы ровно
// один — владелец nlb и три формы оператора SQL; общегейтовое «у всех модулей»
// держит NTF3-70 (полоса S1-A6), и после её посадки эта проба снимается вместе
// с предметом как поглощённая.
package repohygiene

import (
	"testing"
)

func TestNlbJournalHasNoLiteralInsert_S1A7(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tree := newTrackedTree(t, root)
	census, err := CensusJournalWriteForms(root, tree.files)
	if err != nil {
		t.Fatalf("перепись форм записи журнала не собрана: %v", err)
	}
	// --- ПРЕДПОСЫЛКА: перепись не беспредметна, владелец nlb виден,
	// распознаватели трёх форм оператора живы (нашли экземпляры где-либо).
	for _, why := range JournalCensusPremiseFailures(census) {
		t.Error(why)
	}
	nlbSeen := false
	for _, o := range census.Owners {
		if o.Service == "nlb" {
			nlbSeen = true
		}
	}
	if !nlbSeen {
		t.Error("владелец журнала nlb переписью не выведен — молчание о nlb беспредметно")
	}
	forms := []JournalWriteForm{JournalFormLiteralStatement, JournalFormSchemaFormatted, JournalFormNameFormatted}
	alive := 0
	for _, f := range forms {
		alive += census.Recognizer[f]
	}
	if alive == 0 {
		t.Error("распознаватели операторных форм не нашли в дереве ни одного экземпляра — они мертвы")
	}
	if t.Failed() {
		t.FailNow()
	}
	// --- ПРЕДМЕТ.
	total := 0
	for _, f := range forms {
		pts := census.PointsOf("nlb", f)
		total += len(pts)
		t.Logf("nlb, форма %q: точек %d (экземпляров формы в дереве %d)", f, len(pts), census.Recognizer[f])
		for _, p := range pts {
			t.Errorf("S1-A7 (З5, З6): литеральная вставка в журнал nlb мимо функции фундамента: %s%s", p.Pos, describeJournalPoint(p))
		}
	}
	t.Logf("S1-A7: литеральных вставок в журнал nlb — %d (ожидается 0)", total)
}
