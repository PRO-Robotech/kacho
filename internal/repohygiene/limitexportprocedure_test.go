// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// limitexportprocedure_test.go — держатель условия 1 задачи #2134:
// процедура выгрузки назначенных величин ЗАПИСАНА в инструкции обновления, и
// записана дословно той же, какую называет отказ наката.
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `limitexportprocedure_injection_test.go`.
//
// # Почему координата здесь ЖЁСТКАЯ, а не выведена обходом
//
// Обход, судящий «те инструкции, где процедура уже есть», зеленел бы ровно тогда,
// когда её вычеркнули: ноль предметов — ноль находок. Требуемая пара
// «таблица → инструкция» названа явно, поэтому исчезновение процедуры — находка,
// а не тишина. Пропажу самой инструкции гейт тоже называет.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installGuideSuffix — по чему инструкции опознаются в составе дерева.
const installGuideSuffix = "INSTALL.md"

// requiredExportProcedures — таблицы, чья выгрузка обязана быть записана, и где.
//
// Сегодня запись одна, и это не заготовка на будущее: `kaname.limits` — таблица,
// которую снесёт снятие домена величин из службы доступа (#2117 S4), и её счёт
// строк ненулевой у любой установки, потому что цепочка сама сеет умолчания.
// Снесёт другая версия другую таблицу с данными арендатора — запись добавится
// вместе с той миграцией.
var requiredExportProcedures = map[string]string{
	"kaname.limits": "services/iam/INSTALL.md",
}

// TestUpgradeGuideCarriesTheExportProcedureVerbatim — сам гейт.
func TestUpgradeGuideCarriesTheExportProcedureVerbatim(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	guides := map[string]string{}
	for rel := range tt.files {
		if !strings.HasSuffix(rel, installGuideSuffix) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("чтение %s: %v — гейт не вправе судить документ, которого он не прочитал", rel, err)
		}
		guides[rel] = string(body)
	}

	census, findings := JudgeExportProcedure(guides, requiredExportProcedures)
	t.Log(census.String())

	if census.Guides == 0 || census.Lines == 0 {
		t.Fatalf("обход пуст (%s): инструкций в дереве не прочитано ни одной, и «находок ноль» "+
			"здесь означало бы «прочитано ноль», а не «годно»", census.String())
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}
