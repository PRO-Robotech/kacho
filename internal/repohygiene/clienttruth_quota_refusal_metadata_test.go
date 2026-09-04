// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestQuotaRefusalAmountsReachTheClient — величины, посчитанные единственным
// производителем отказа учёта, обязаны доезжать до клиента машиночитаемо.
//
// Предмет, обе половины моста и границы разбора — в шапке
// clienttruth_quota_refusal_metadata.go. Способность гейта упасть и смолчать
// доказана инъекцией в обе стороны:
// clienttruth_quota_refusal_metadata_injection_test.go.
func TestQuotaRefusalAmountsReachTheClient(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}

	// АВТОРИТЕТ перечня владельцев — тот же, из которого рендерится сам
	// производитель. Выписать его здесь значило бы завести второе место об
	// одном предмете: седьмой владелец появился бы у производителя и не
	// появился бы у проверки, причём молча.
	owners := make([]string, 0, len(quota.RefusalOwners()))
	for _, o := range quota.RefusalOwners() {
		owners = append(owners, o.Service)
	}

	c, err := collectQuotaRefusalMetadata(tree, owners)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	// ── ПЕРЕПИСЬ. Печатается ВСЕГДА: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного». Обе величины по каждой половине названы отдельно —
	// одно число скрыло бы ровно тот случай, ради которого гейт заведён.
	t.Logf("перепись: владельцев учёта %d · файлов Go разобрано %d · "+
		"мостов отказа %d (приклеивают величины %d) · "+
		"сборок ответа учёта %d (несут метаданные %d)",
		len(c.Owners), c.Parsed,
		c.BridgesFound, c.BridgesAttaching,
		c.OutwardFound, c.OutwardWithMetadata)

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт:
	// «нарушений нет» и «нечего было читать» печатаются одинаково.
	if len(c.Owners) == 0 {
		t.Fatal("владельцев учёта не объявлено ни одного — сверять не с чем")
	}
	if c.Parsed == 0 {
		t.Fatal("прод-файлов Go у владельцев учёта не разобрано ни одного — " +
			"вердикт беспредметен")
	}
	if c.BridgesFound == 0 {
		t.Fatal("мостов SQLSTATE→sentinel не найдено ни одного — распознаватель " +
			"перестал видеть предмет (см. testing.md §«Гейт на класс», п.7)")
	}
	if c.OutwardFound == 0 {
		t.Fatal("сборок ответа полосы учёта не найдено ни одной — распознаватель " +
			"перестал видеть предмет (см. testing.md §«Гейт на класс», п.7)")
	}
	// Предпосылка, без которой отрицание ниже ЗАМОЛКАЕТ (testing.md §«Гейт на
	// класс», п.9): у каждого владельца обязаны быть обе половины, иначе
	// «величины не теряются» станет правдой оттого, что терять стало нечего.
	// Наличие половин держит соседний гейт
	// TestEveryQuotaChargingOwnerMapsTheRefusal; здесь оно проверяется как СВОЯ
	// предпосылка, а не как его предмет.
	if c.BridgesFound != len(c.Owners) {
		t.Fatalf("мост отказа найден у %d владельцев из %d — предпосылка не выполнена; "+
			"предмет наличия моста держит TestEveryQuotaChargingOwnerMapsTheRefusal",
			c.BridgesFound, len(c.Owners))
	}
	if c.OutwardFound != len(c.Owners) {
		t.Fatalf("сборка ответа полосы учёта найдена у %d владельцев из %d — "+
			"предпосылка не выполнена", c.OutwardFound, len(c.Owners))
	}

	for _, f := range quotaRefusalMetadataFindings(c) {
		t.Errorf("величины отказа учёта не доезжают до клиента: %s", f)
	}
}
