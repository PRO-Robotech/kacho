// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modelcanoncheckwiring_injection_test.go — доказательство, что гейт провязки
// судьи ПОБАЙТОВОЙ СВЕРКИ способен упасть и способен смолчать (задача #1893).
//
// Раскладка синтетического дерева и предпосылки обхода — общие, они живут в
// gatetargetwiring_injection_support_test.go. Здесь — тела носителей и оси.
//
// # Почему тела носителей несут ОБЕ цели
//
// В дереве продукта их объявляет один и тот же Makefile и зовут одни и те же
// носители. Синтетика, знающая одну цель, доказывала бы способность падать на
// входе, которого не бывает, и не могла бы показать главного: что инъекция
// роняет ТОЛЬКО свой предмет. Прогонов поэтому три — контроль, снятие провязки
// у новой цели и снятие у существующей, — и каждый спрашивает ОБА гейта.
//
// # Законный близнец у каждой оси
//
// Односторонняя проверка зеленела бы на дереве, где всё сломано одинаково.
// Рядом с каждой находкой стоит вход той же формы, на котором гейт обязан
// молчать: сервис, законно не объявляющий цель; вызов, записанный исполняемой
// строкой, а не прозой; соседняя цель, чья провязка цела.
package repohygiene

import (
	"strings"
	"testing"
)

// bothTargetsMakefile — Makefile, объявляющий ОБЕ цели, вместе с прозой, которая
// их объясняет: в настоящем Makefile имя каждой цели стоит и в шапке, и примером
// вызова.
const bothTargetsMakefile = `# module-manifest-check — форму манифеста домена судит ОДИН исполнитель.
# Вызов: ` + "`make -C services/iam module-manifest-check`" + `
.PHONY: module-manifest-check
module-manifest-check:
	@./tools/module-manifest-check.sh

# model-canon-check — блоки модели сверяются с манифестами ПОБАЙТОВО.
# Вызов: ` + "`make -C services/iam model-canon-check`" + `
.PHONY: model-canon-check
model-canon-check:
	@./tools/model-canon-check.sh
`

// canonSilentMakefile — законный близнец: сервис, который ни одной цели не
// объявляет. Гейт, требующий цель от каждого сервиса, краснел бы на шести
// сервисах из семи и был бы снят первым же читателем.
const canonSilentMakefile = `.PHONY: audit-list-filter
audit-list-filter:
	@./tools/audit-list-filter.sh
`

// bothCallingWorkflow — конвейер, зовущий обе цели своими шагами.
const bothCallingWorkflow = `jobs:
  authz-artifacts:
    steps:
      - name: манифесты модулей — форму судит один загрузчик
        run: |
          rc=0
          make -C services/iam module-manifest-check || rc=$?
      - name: модель доступов — блоки сверяются с манифестами побайтово
        run: |
          rc=0
          make -C services/iam model-canon-check || rc=$?
`

// bothCallingLocalRunner — прогонщик, зовущий обе цели своими функциями.
const bothCallingLocalRunner = `#!/usr/bin/env bash
manifest_form_check() {
    ( cd "$ROOT" && make -C services/iam module-manifest-check ) > "$log" 2>&1 || rc=$?
}
model_canon_check() {
    ( cd "$ROOT" && make -C services/iam model-canon-check ) > "$log" 2>&1 || rc=$?
}
`

// canonWiringFaultsOn — находки гейта НОВОЙ цели на синтетическом дереве.
func canonWiringFaultsOn(t *testing.T, tree judgeWiringTree) []string {
	t.Helper()
	return judgeWiringFaultsOn(t, modelCanonCheckTarget, tree)
}

// bothWiredTree — дерево, где провязаны ОБЕ цели: контроль для всех осей.
func bothWiredTree() judgeWiringTree {
	return judgeWiringTree{
		makefiles:   map[string]string{"iam": bothTargetsMakefile, "vpc": canonSilentMakefile},
		workflow:    bothCallingWorkflow,
		localRunner: bothCallingLocalRunner,
	}
}

// TestModelCanonCheckWiringGate_Injection — обе способности по каждой оси, и
// три прогона на разделимость предметов.
func TestModelCanonCheckWiringGate_Injection(t *testing.T) {
	// ── ПРОГОН 1, КОНТРОЛЬ: провязаны обе цели — молчат ОБА гейта ──────────
	//
	// Стоит первым и не является формальностью: без него всякая находка ниже
	// объяснялась бы гейтом, который краснеет на любом входе.
	{
		intact := bothWiredTree()
		if got := canonWiringFaultsOn(t, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: на провязанном дереве гейт %s нашёл %d — он краснеет на исправном "+
				"входе, и ни одна находка ниже ничего не доказывает:\n  %s",
				modelCanonCheckTarget, len(got), strings.Join(got, "\n  "))
		}
		if got := judgeWiringFaultsOn(t, moduleManifestCheckTarget, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: соседний гейт %s краснеет на том же входе (%d) — общий механизм "+
				"негоден, и разделимость предметов ниже недоказуема:\n  %s",
				moduleManifestCheckTarget, len(got), strings.Join(got, "\n  "))
		}
		t.Log("контроль: обе цели провязаны — 0 находок у обоих гейтов; сервис без целей молчит")
	}

	// ── ПРОГОН 2, ИНЪЕКЦИЯ НОВОГО: снята провязка ТОЛЬКО у model-canon-check ─
	//
	// Форма инъекции выбрана намеренно: снимается НОВОЕ свойство у элемента,
	// чьё старое на месте. Инъекция вида «завести ещё один элемент» нарушила бы
	// заодно и существующий контроль, и красное пришло бы от соседа — тогда
	// новый гейт мог бы оказаться вакуумным, не показав этого ничем.
	{
		tree := bothWiredTree()
		tree.workflow = strings.Replace(bothCallingWorkflow,
			"      - name: модель доступов — блоки сверяются с манифестами побайтово\n"+
				"        run: |\n          rc=0\n          make -C services/iam model-canon-check || rc=$?\n",
			"      - name: что-то другое\n        run: go build ./...\n", 1)
		tree.localRunner = strings.Replace(bothCallingLocalRunner,
			"    ( cd \"$ROOT\" && make -C services/iam model-canon-check ) > \"$log\" 2>&1 || rc=$?\n",
			"    run \"go build\" go build ./...\n", 1)

		got := canonWiringFaultsOn(t, tree)
		if len(got) != 2 {
			t.Fatalf("инъекция нового: ожидалось 2 находки (оба носителя), получено %d:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		var byWorkflow, byRunner bool
		for _, f := range got {
			if !strings.Contains(f, modelCanonCheckTarget) {
				t.Errorf("находка не называет цель: %s", f)
			}
			if strings.Contains(f, "ни один шаг конвейера") {
				byWorkflow = true
			}
			if strings.Contains(f, localRunnerRel) {
				byRunner = true
			}
		}
		if !byWorkflow || !byRunner {
			t.Errorf("находки не назвали обоих носителей: конвейер=%v прогонщик=%v", byWorkflow, byRunner)
		}
		// Существующий контроль обязан МОЛЧАТЬ: инъекция роняет только своё.
		if other := judgeWiringFaultsOn(t, moduleManifestCheckTarget, tree); len(other) != 0 {
			t.Fatalf("инъекция нового задела соседа: гейт %s дал %d находок — красное пришло бы "+
				"от него, и вердикт нового гейта ничего не значил бы:\n  %s",
				moduleManifestCheckTarget, len(other), strings.Join(other, "\n  "))
		}
		t.Logf("инъекция нового: 2 находки у %s, 0 у %s — предметы разделимы",
			modelCanonCheckTarget, moduleManifestCheckTarget)
	}

	// ── ПРОГОН 3, ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО: снята провязка соседа ────────────
	//
	// Без него молчание существующего контроля в прогоне 2 неотличимо от
	// молчания мёртвого: гейт соседа мог бы не краснеть ни на чём.
	{
		tree := bothWiredTree()
		tree.workflow = strings.Replace(bothCallingWorkflow,
			"      - name: манифесты модулей — форму судит один загрузчик\n"+
				"        run: |\n          rc=0\n          make -C services/iam module-manifest-check || rc=$?\n",
			"      - name: что-то другое\n        run: go build ./...\n", 1)
		tree.localRunner = strings.Replace(bothCallingLocalRunner,
			"    ( cd \"$ROOT\" && make -C services/iam module-manifest-check ) > \"$log\" 2>&1 || rc=$?\n",
			"    run \"go build\" go build ./...\n", 1)

		other := judgeWiringFaultsOn(t, moduleManifestCheckTarget, tree)
		if len(other) != 2 {
			t.Fatalf("инъекция существующего: гейт %s обязан дать 2 находки, дал %d — он не способен "+
				"падать, и его молчание в прогоне 2 ничего не доказывало:\n  %s",
				moduleManifestCheckTarget, len(other), strings.Join(other, "\n  "))
		}
		if got := canonWiringFaultsOn(t, tree); len(got) != 0 {
			t.Fatalf("инъекция существующего задела новый гейт: %d находок:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		t.Logf("инъекция существующего: 2 находки у %s, 0 у %s — оба гейта живы и независимы",
			moduleManifestCheckTarget, modelCanonCheckTarget)
	}

	// ── ОСЬ: ПРОЗА вместо провязки ────────────────────────────────────────
	//
	// Имя цели встречается в комментариях, которые её же объясняют, и гейт,
	// ищущий имя подстрокой, зеленел бы на собственном объяснении — оставаясь
	// зелёным при снятой провязке. Здесь оба носителя называют цель ПОЛНОСТЬЮ и
	// только в комментарии.
	{
		tree := bothWiredTree()
		tree.workflow = "jobs:\n  authz-artifacts:\n    steps:\n" +
			"      - name: манифесты модулей\n        run: |\n" +
			"          make -C services/iam module-manifest-check || rc=$?\n" +
			"      - name: что-то другое\n        run: |\n" +
			"          # здесь когда-то звали: make -C services/iam model-canon-check\n" +
			"          go build ./...\n"
		tree.localRunner = bothCallingLocalRunner[:strings.Index(bothCallingLocalRunner, "model_canon_check() {")] +
			"# провязка снята: make -C services/iam model-canon-check\n"
		got := canonWiringFaultsOn(t, tree)
		if len(got) != 2 {
			t.Fatalf("ось «проза вместо провязки»: ожидалось 2 находки, получено %d — гейт зачёл "+
				"комментарий за вызов и остался бы зелёным при снятой провязке:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		t.Log("ось «проза вместо провязки»: 2 находки — комментарий за вызов НЕ зачтён")
	}

	// ── ОСЬ: имя цели в НЕИСПОЛНЯЕМОМ поле шага ───────────────────────────
	//
	// Несущая для #1893. Такая строка комментарием НЕ является — её не отбросил
	// бы текстовый обход, — а вызовом не становится: заголовок шага и условие его
	// исполнения не запускают ничего. Отличить это от провязки умеет только
	// разбор YAML, ради которого механизм и переведён на него.
	{
		tree := bothWiredTree()
		tree.workflow = "jobs:\n  authz-artifacts:\n    steps:\n" +
			"      - name: манифесты модулей\n        run: |\n" +
			"          make -C services/iam module-manifest-check || rc=$?\n" +
			"      - name: 'снято: make -C services/iam model-canon-check'\n" +
			"        if: \"contains('make -C services/iam model-canon-check', 'x')\"\n" +
			"        run: go build ./...\n"
		got := canonWiringFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("ось «имя цели в неисполняемом поле»: ожидалась 1 находка (конвейер), получено %d — "+
				"гейт зачёл заголовок шага за вызов:\n  %s", len(got), strings.Join(got, "\n  "))
		}
		if !strings.Contains(got[0], "ни один шаг конвейера") {
			t.Errorf("находка не назвала носителя: %s", got[0])
		}
		t.Log("ось «имя цели в заголовке шага»: 1 находка — заголовок за вызов НЕ зачтён")
	}

	// ── ОСЬ: обратное направление ─────────────────────────────────────────
	//
	// Носитель зовёт цель у сервиса, который её не объявляет: шаг молча
	// позеленел бы на несуществующей цели. Без этой оси гейт был бы
	// односторонним и пропускал бы провязку в пустоту.
	{
		tree := bothWiredTree()
		tree.workflow = strings.ReplaceAll(bothCallingWorkflow,
			"make -C services/iam model-canon-check", "make -C services/storage model-canon-check")
		tree.localRunner = strings.ReplaceAll(bothCallingLocalRunner,
			"make -C services/iam model-canon-check", "make -C services/storage model-canon-check")
		got := canonWiringFaultsOn(t, tree)
		// iam объявляет и никем не зван (2) + storage зван и не объявляет у обоих
		// носителей (2).
		if len(got) != 4 {
			t.Fatalf("ось «зовут несуществующую цель»: ожидалось 4 находки, получено %d:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		var reverse int
		for _, f := range got {
			if strings.Contains(f, "позеленеет ни на чём") {
				reverse++
			}
		}
		if reverse != 2 {
			t.Errorf("обратное направление не сработало: находок «позеленеет ни на чём» %d из 2", reverse)
		}
		t.Logf("ось «зовут несуществующую цель»: 4 находки, из них обратных %d", reverse)
	}

	// ── ПУСТОЙ ОБХОД — не «находок нет», а «прочитано ноль» ────────────────
	{
		root := t.TempDir()
		if _, err := readJudgeTargetWiring(root, modelCanonCheckTarget); err == nil {
			t.Error("пустое дерево прочиталось без ошибки — «ноль находок» стало бы неотличимо " +
				"от «ноль прочитанного»")
		} else {
			t.Logf("пустой обход: ошибка чтения получена — %v", err)
		}
	}

	// ── НЕРАЗБИРАЕМЫЙ КОНВЕЙЕР — тоже «прочитано ноль», а не «вызова нет» ──
	//
	// Ось заведена вместе с разбором YAML: файл, который не разобрался, обязан
	// давать ОШИБКУ, иначе гейт объявил бы провязку отсутствующей на основании
	// того, что он ничего не прочитал.
	{
		tree := bothWiredTree()
		tree.workflow = "jobs:\n  authz-artifacts:\n   steps:\n  - name: рваный отступ\n\t\trun: x\n"
		if _, err := readJudgeTargetWiring(writeJudgeWiringTree(t, tree), modelCanonCheckTarget); err == nil {
			t.Error("неразбираемый файл конвейера прочитался без ошибки — «вызова нет» стало бы " +
				"неотличимо от «не прочитано ничего»")
		} else {
			t.Logf("неразбираемый конвейер: ошибка получена — %v", err)
		}
	}
}
