// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// rolescopechainstaysempty_injection_test.go — ДОКАЗАТЕЛЬСТВО того, что гейт
// §2.8 способен упасть и способен смолчать.
//
// Инъекция подаётся НАСТОЯЩИМ входом — текстом ветви той же формы, какую пишет
// производитель, — а не выдуманной строкой: гейт, доказанный на форме, которой в
// дереве не бывает, не доказан ни на чём.
//
// Осей три, и каждая прогоняется отдельно:
//
//	контроль          законные ветви (5a) и (5b) — гейт МОЛЧИТ;
//	инъекция нового   третья ветвь без ярусного источника — гейт КРАСНЕЕТ и
//	                  называет координату;
//	инъекция прямого  посев звена значениями — краснеет тоже, у него предиката
//	                  нет вовсе.
//
// Третий прогон обязателен и не является повтором второго: у прямого посева
// ДРУГОЙ распознаватель, и его молчание при живом первом было бы неотличимо от
// исправной работы.

import (
	"strings"
	"testing"
)

// roleScopeChainLegalBranches — ДВЕ законные ветви, дословно по форме
// производителя. Положительный контроль: без него всякое отрицание ниже зеленело
// бы на распознавателе, не находящем НИЧЕГО.
const roleScopeChainLegalBranches = `
INSERT INTO kacho_iam.resource_parent_edge (object_type, object_id, parent_type, parent_id, depth)
  SELECT 'iam_role'::text, o.id, 'account'::text, o.account_id, 1
    FROM kacho_iam.roles o
   WHERE COALESCE(o.account_id, '') <> ''
UNION ALL
  SELECT 'iam_role'::text, o.id, 'project'::text, o.project_id, 1
    FROM kacho_iam.roles o
   WHERE COALESCE(o.project_id, '') <> '';
`

// TestRoleScopeChainGateStaysSilentOnTheLegalTwin — КОНТРОЛЬ.
func TestRoleScopeChainGateStaysSilentOnTheLegalTwin(t *testing.T) {
	found, census := ScanRoleScopeChain("services/iam/internal/migrations/legal.sql",
		roleScopeChainLegalBranches)

	t.Logf("перепись контроля: ветвей %d, из них ярусных %d, находок %d",
		census.Branches, census.TierSourced, len(found))

	if census.Branches != 2 {
		t.Fatalf("законных ветвей распознано %d вместо двух: распознаватель не видит "+
			"формы, которую пишет производитель, и его молчание ниже ничего не значит",
			census.Branches)
	}
	if census.TierSourced != 2 {
		t.Fatalf("ярусными признаны %d ветви из двух: гейт краснел бы на законном "+
			"производителе и был бы снят первым же читателем", census.TierSourced)
	}
	if len(found) != 0 {
		t.Fatalf("гейт нашёл %d находок на ЗАКОННЫХ ветвях: %v", len(found), found)
	}
}

// TestRoleScopeChainGateRedsOnAThirdProducer — ИНЪЕКЦИЯ НОВОГО.
//
// Внесена ровно ОДНА перемена против контроля: добавлена третья ветвь, берущая
// звено у `cluster_id`. Обе законные остаются на месте и обязаны по-прежнему
// молчать — иначе красное пришло бы от соседа, и гейт мог бы оказаться
// вакуумным, не показав этого ничем.
func TestRoleScopeChainGateRedsOnAThirdProducer(t *testing.T) {
	injected := roleScopeChainLegalBranches + `
UNION ALL
  SELECT 'iam_role'::text, o.id, 'cluster'::text, o.cluster_id, 1
    FROM kacho_iam.roles o
   WHERE COALESCE(o.cluster_id, '') <> '';
`
	found, census := ScanRoleScopeChain("services/iam/internal/migrations/injected.sql", injected)

	t.Logf("перепись инъекции: ветвей %d, из них ярусных %d, находок %d",
		census.Branches, census.TierSourced, len(found))

	if len(found) != 1 {
		t.Fatalf("третья ветвь без ярусного источника дала %d находок вместо одной: "+
			"гейт не способен упасть на предмете, ради которого заведён", len(found))
	}
	if census.TierSourced != 2 {
		t.Errorf("законных ветвей признано ярусными %d вместо двух: инъекция уронила "+
			"НЕ ТОЛЬКО проверяемое, и красное могло прийти от соседа", census.TierSourced)
	}
	if !strings.Contains(found[0].What, "cluster") {
		t.Errorf("находка не называет виновную ветвь: %q — читатель пойдёт искать её "+
			"перебором", found[0].What)
	}
	if found[0].Line == 0 {
		t.Error("находка без номера строки: координата обязана быть точной")
	}
}

// TestRoleScopeChainGateRedsOnADirectSeed — ИНЪЕКЦИЯ ПРЯМОГО ПОСЕВА.
//
// Ось СВОЯ: у посева значениями предиката нет вовсе, поэтому «ярусный источник»
// у него отсутствует by construction, а не по недосмотру. Отдельный прогон
// обязателен — молчание этого распознавателя при живом первом было бы неотличимо
// от исправной работы.
func TestRoleScopeChainGateRedsOnADirectSeed(t *testing.T) {
	injected := `
INSERT INTO kacho_iam.resource_parent_edge (object_type, object_id, parent_type, parent_id, depth)
VALUES ('iam_role', 'rol_probe', 'account', 'acc_probe', 1);
`
	found, census := ScanRoleScopeChain("services/iam/internal/migrations/seed.sql", injected)

	t.Logf("перепись посева: ветвей %d, находок %d", census.Branches, len(found))
	if len(found) != 1 {
		t.Fatalf("прямой посев звена дал %d находок вместо одной: у него нет предиката "+
			"вовсе, и признать его законным нечем", len(found))
	}
}

// TestRoleScopeChainGateIgnoresANonProducer — ВТОРОЙ законный близнец.
//
// Имя типа встречается в словаре типов и в перечислениях, и производителем звена
// от этого не становится. Без этой пробы гейт краснел бы на словаре — то есть на
// строке, которая не производит ничего.
func TestRoleScopeChainGateIgnoresANonProducer(t *testing.T) {
	dictionary := `
INSERT INTO kacho_iam.scope_chain_type_dictionary (dotted, model_type) VALUES
  ('iam.role', 'iam_role');
`
	found, census := ScanRoleScopeChain("services/iam/internal/migrations/dict.sql", dictionary)
	t.Logf("перепись словаря: упоминаний таблицы звеньев %d, ветвей %d, находок %d",
		census.Statements, census.Branches, len(found))
	if len(found) != 0 {
		t.Fatalf("гейт нашёл %d находок в СЛОВАРЕ типов: он судит по имени типа, а не по "+
			"производству звена, и будет снят первым же читателем", len(found))
	}
}
