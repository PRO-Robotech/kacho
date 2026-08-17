// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Слот «пул по умолчанию» для пары (zone_id IS NULL, kind) — КЛАСТЕРНЫЙ
// СИНГЛТОН (партиал-UNIQUE `address_pools_zone_kind_default_uniq`). Из него
// берёт публичный адрес КАЖДЫЙ внешний балансировщик, и на стенде его заводит
// подъём (deploy/scripts/vpc-address-pool-baseline.sql, цель `seed-vpc-pools`).
//
// Посев набора nlb (deploy/scripts/seed-nlb-fixtures.sh §3.6) ту же строку
// ОПОЗНАЁТ ПО ИМЕНИ и, узнав её, уходит в ветку «reusing … placement verified».
// То есть личность пула объявлена в ДВУХ местах, и они обязаны совпадать: если
// имя разъедется, посев набора заведёт ВТОРОЙ пул и попытается отобрать слот —
// его правка признака «по умолчанию» ответит отказом, а сам отказ там мягкий
// (WARNING), поэтому расхождение будет тихим и вылезет отказом выделения адреса
// в чужом наборе.
//
// Совпадение БЛОКОВ существенно отдельно от имени: ограничение исключения по
// пересечению глобально на kind, поэтому одно имя при разных блоках даёт отказ
// вставки, а не тихую вторую строку.

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	standPoolSQLRel   = "deploy/scripts/vpc-address-pool-baseline.sql"
	standPoolSeedRel  = "deploy/scripts/seed-nlb-fixtures.sh"
	standPoolKindName = "EXTERNAL_PUBLIC"
)

// TestStandAnycastPoolBaselineMatchesTheNlbSeeder — два объявления одной
// личности обязаны совпадать.
//
// Гейт НЕ МОЖЕТ пройти вхолостую: нечитаемое или неразобранное объявление —
// отказ с именем файла, а не молчание. «Ноль расхождений» на пустом файле было
// бы неотличимо от согласия, поэтому объём разобранного печатается.
func TestStandAnycastPoolBaselineMatchesTheNlbSeeder(t *testing.T) {
	root := repoRoot(t)

	fromSQL, err := ParseStandPoolIdentityFromSQL(mustReadTreeFile(t, filepath.Join(root, standPoolSQLRel)))
	if err != nil {
		t.Fatalf("%s: %v — гейт перестал читать свой предмет", standPoolSQLRel, err)
	}
	fromSeeder, err := ParseStandPoolIdentityFromSeeder(mustReadTreeFile(t, filepath.Join(root, standPoolSeedRel)))
	if err != nil {
		t.Fatalf("%s: %v — гейт перестал читать свой предмет", standPoolSeedRel, err)
	}

	t.Logf("осмотрено объявлений: 2 (%s, %s); сверено величин: 3 (имя, блок v4, блок v6); вид пула — %s; "+
		"личность подъёма: %s v4=%s v6=%s",
		standPoolSQLRel, standPoolSeedRel, standPoolKindName, fromSQL.Name, fromSQL.V4, fromSQL.V6)

	for _, d := range DiffStandPoolIdentity(fromSQL, fromSeeder) {
		t.Errorf("%s\n"+
			"  Слот «по умолчанию» для (zone_id IS NULL, %s) — кластерный синглтон: посев набора nlb\n"+
			"  не опознает строку подъёма, заведёт ВТОРУЮ и попытается отобрать слот. Его отказ там\n"+
			"  мягкий, поэтому расхождение будет тихим — до отказа выделения адреса в чужом наборе.",
			d, standPoolKindName)
	}
}

func mustReadTreeFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("не прочитан %s: %v — предмета у гейта не осталось, и молчать он не вправе", path, err)
	}
	if len(b) == 0 {
		t.Fatalf("%s пуст — «ноль расхождений» на пустом файле неотличимо от согласия", path)
	}
	return string(b)
}
