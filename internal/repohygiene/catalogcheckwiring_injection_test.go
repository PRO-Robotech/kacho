// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogcheckwiring_injection_test.go — доказательство, что гейт провязки
// сверки копий каталога прав способен упасть и способен смолчать (задача #2084).
//
// Раскладка синтетического дерева — общая, она живёт в
// gatetargetwiring_injection_support_test.go. Здесь — тела носителей и оси.
//
// # Почему инъекция на синтетике, а не в .github/workflows
//
// Гейт обязан быть зелёным на дереве продукта — иначе его нечем читать. Значит
// его способность краснеть там не наблюдается НИКОГДА, и «зелёный» неотличим от
// «мёртвый». Править ради инъекции настоящий файл конвейера нельзя ни при каких
// условиях: он общий, его правит соседняя полоса, и восстановление «побайтово»
// доказывается только тем, что кто-то не забыл проверить. Синтетика подаёт вход,
// которого в дереве нет by construction, и ничего чужого не трогает.
//
// # Почему тела несут ТРИ цели, а не одну
//
// Инъекция обязана ронять ТОЛЬКО свой предмет. Дерево, знающее одну цель,
// доказывало бы способность падать на входе, которого не бывает, и не могло бы
// показать главного: что красное пришло от нового гейта, а не от соседа.
// Поэтому здесь три судьи разом — сверка копий (gateway), её сосед по каталогу
// (`rest-route-table-check`, тот же gateway) и судья сервиса (`module-manifest-check`,
// services/iam, чужой механизм провязки).
//
// # Законный близнец у каждой оси
//
// Односторонняя проверка зеленела бы на дереве, где всё сломано одинаково.
// Рядом с каждой находкой стоит вход той же формы, на котором гейт обязан
// молчать: соседняя цель того же каталога, провязанная законно; вызов,
// записанный исполняемой строкой, а не прозой; судья сервиса, чья провязка цела.
package repohygiene

import (
	"strings"
	"testing"
)

// catalogTwinTarget — соседний судья ТОГО ЖЕ каталога: законный близнец.
//
// Он делит с предметом и каталог, и механизм, и форму вызова — то есть отличается
// ровно тем, что проверяется. Близнец из другого каталога был бы слабее: его
// молчание объяснялось бы чем угодно.
const catalogTwinTarget = "rest-route-table-check"

// catalogGatewayMakefile — Makefile каталога, объявляющий цепочку сверки, её
// соседа и ПРОЗУ, которая их же объясняет: в настоящем Makefile имя каждой цели
// стоит и в шапке, и примером вызова.
const catalogGatewayMakefile = `# permission-catalog-copies-in-sync — сверка двух вшитых копий каталога прав.
# Вызов: ` + "`make -C gateway permission-catalog-copies-in-sync`" + `
.PHONY: permission-catalog-copies-in-sync
permission-catalog-copies-in-sync:
	@test -f "$(IAM_CATALOG)" || { echo "нет копии iam"; exit 1; }
	@diff -u "$(IAM_CATALOG)" "$(PERMISSION_CATALOG_TARGET)"

# permission-catalog-check — то, что зовёт конвейер; сверка стоит его зависимостью.
.PHONY: permission-catalog-check
permission-catalog-check: permission-catalog-copies-in-sync
	./scripts/gen-permission-catalog.sh

# rest-route-table-check — соседний судья того же каталога; в дереве продукта его
# зовёт тот же job конвейера, что и цепочку каталога прав.
.PHONY: rest-route-table-check
rest-route-table-check:
	@./scripts/gen-rest-route-table.sh
`

// catalogServiceMakefile — судья сервиса: чужой механизм провязки, стоящий рядом
// ради проверки разделимости предметов.
const catalogServiceMakefile = `# module-manifest-check — форму манифеста домена судит ОДИН исполнитель.
.PHONY: module-manifest-check
module-manifest-check:
	@./tools/module-manifest-check.sh
`

// catalogCallingWorkflow — конвейер, зовущий все три цели: две — рабочим
// каталогом шага, третью — через ` + "`-C`" + `.
const catalogCallingWorkflow = `jobs:
  authz-artifacts:
    steps:
      - name: permission-catalog staleness + copy-drift
        working-directory: gateway
        run: make permission-catalog-check
      - name: rest-route-table staleness
        working-directory: gateway
        run: make rest-route-table-check
  gates:
    steps:
      - name: манифесты модулей — форму судит один загрузчик
        run: |
          rc=0
          make -C services/iam module-manifest-check || rc=$?
`

// catalogCallingLocalRunner — прогонщик зовёт судью сервиса. Каталожную цепочку
// он не зовёт и не обязан: её предмет требует полного чекаута монорепо.
const catalogCallingLocalRunner = `#!/usr/bin/env bash
manifest_form_check() {
    ( cd "$ROOT" && make -C services/iam module-manifest-check ) > "$log" 2>&1 || rc=$?
}
`

// catalogWiredTree — дерево, где провязаны все три цели: контроль для всех осей.
func catalogWiredTree() judgeWiringTree {
	return judgeWiringTree{
		makefiles:    map[string]string{"iam": catalogServiceMakefile},
		dirMakefiles: map[string]string{catalogMakefileDir: catalogGatewayMakefile},
		workflow:     catalogCallingWorkflow,
		localRunner:  catalogCallingLocalRunner,
	}
}

// catalogParityFaultsOn — находки гейта СВЕРКИ на синтетическом дереве.
func catalogParityFaultsOn(t *testing.T, tree judgeWiringTree) []string {
	t.Helper()
	return dirTargetWiringFaultsOn(t, catalogMakefileDir, catalogParityTarget, tree)
}

// catalogTwinFaultsOn — находки того же гейта про соседнюю цель того же каталога.
func catalogTwinFaultsOn(t *testing.T, tree judgeWiringTree) []string {
	t.Helper()
	return dirTargetWiringFaultsOn(t, catalogMakefileDir, catalogTwinTarget, tree)
}

// TestCatalogCopyParityWiringGate_Injection — обе способности по каждой оси.
func TestCatalogCopyParityWiringGate_Injection(t *testing.T) {
	// ── ПРОГОН 1, КОНТРОЛЬ: провязаны все три цели — молчат все ─────────────
	//
	// Стоит первым и не является формальностью: без него всякая находка ниже
	// объяснялась бы гейтом, который краснеет на любом входе.
	{
		intact := catalogWiredTree()
		if got := catalogParityFaultsOn(t, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: на провязанном дереве гейт цели %s нашёл %d — он краснеет на исправном "+
				"входе, и ни одна находка ниже ничего не доказывает:\n  %s",
				catalogParityTarget, len(got), strings.Join(got, "\n  "))
		}
		if got := catalogTwinFaultsOn(t, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: соседняя цель %s краснеет на том же входе (%d):\n  %s",
				catalogTwinTarget, len(got), strings.Join(got, "\n  "))
		}
		if got := judgeWiringFaultsOn(t, moduleManifestCheckTarget, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: судья сервиса %s краснеет на том же входе (%d) — общий механизм "+
				"негоден, и разделимость предметов ниже недоказуема:\n  %s",
				moduleManifestCheckTarget, len(got), strings.Join(got, "\n  "))
		}
		t.Log("контроль: провязаны сверка, её сосед и судья сервиса — 0 находок у всех трёх")
	}

	// ── ПРОГОН 2: ШАГ ВЫЗОВА СНЯТ ИЗ ПРОЦЕССА ──────────────────────────────
	//
	// Ровно тот дефект, ради которого гейт заведён: цель осталась исполнимой и
	// перестала исполняться. Форма инъекции выбрана намеренно — снимается НОВОЕ
	// свойство у элемента, чьё старое на месте: Makefile не тронут, связь внутри
	// него цела, соседняя цель того же каталога по-прежнему позвана.
	{
		tree := catalogWiredTree()
		tree.workflow = strings.Replace(catalogCallingWorkflow,
			"      - name: permission-catalog staleness + copy-drift\n"+
				"        working-directory: gateway\n"+
				"        run: make permission-catalog-check\n",
			"      - name: что-то другое\n        run: go build ./...\n", 1)

		got := catalogParityFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("инъекция «шаг вызова снят»: ожидалась 1 находка, получено %d:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		// Диагностика обязана называть ПРЕДМЕТ — цель и процессы, — а не «связь
		// не найдена»: находка, называющая симптом, посылает читателя искать не
		// там, и на неё тратят прогон, прежде чем снять гейт как непонятный.
		if !strings.Contains(got[0], catalogParityTarget) {
			t.Errorf("находка не называет ЦЕЛЬ: %s", got[0])
		}
		if !strings.Contains(got[0], "ci.yaml") {
			t.Errorf("находка не называет ПРОЦЕСС: %s", got[0])
		}
		if !strings.Contains(got[0], catalogMakefileDir+"/Makefile") {
			t.Errorf("находка не называет каталог объявления: %s", got[0])
		}
		// Законный близнец: соседняя цель того же каталога позвана и молчит.
		if twin := catalogTwinFaultsOn(t, tree); len(twin) != 0 {
			t.Fatalf("инъекция задела соседнюю цель %s (%d находок) — красное пришло бы от неё:\n  %s",
				catalogTwinTarget, len(twin), strings.Join(twin, "\n  "))
		}
		// Судья сервиса — чужой механизм провязки — обязан молчать тоже.
		if other := judgeWiringFaultsOn(t, moduleManifestCheckTarget, tree); len(other) != 0 {
			t.Fatalf("инъекция задела судью сервиса %s (%d находок):\n  %s",
				moduleManifestCheckTarget, len(other), strings.Join(other, "\n  "))
		}
		t.Logf("инъекция «шаг вызова снят»: 1 находка, называет цель и процесс; сосед и судья сервиса молчат\n  %s", got[0])
	}

	// ── ПРОГОН 3: ИМЯ ЦЕЛИ СТОИТ ТОЛЬКО В КОММЕНТАРИИ ПРОЦЕССА ─────────────
	//
	// Имя цели встречается в прозе, которая её же объясняет. Гейт, ищущий имя
	// подстрокой, зеленел бы на собственном объяснении — оставаясь зелёным при
	// снятой провязке. Здесь процесс называет ОБЕ цепочки полностью и только
	// комментарием.
	{
		tree := catalogWiredTree()
		tree.workflow = `jobs:
  authz-artifacts:
    steps:
      - name: что-то другое
        working-directory: gateway
        run: |
          # здесь когда-то звали: make permission-catalog-check
          # и напрямую: make -C gateway permission-catalog-copies-in-sync
          go build ./...
      - name: rest-route-table staleness
        working-directory: gateway
        run: make rest-route-table-check
  gates:
    steps:
      - name: манифесты модулей
        run: |
          make -C services/iam module-manifest-check || rc=$?
`
		got := catalogParityFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("ось «имя цели только в комментарии»: ожидалась 1 находка, получено %d — гейт "+
				"зачёл комментарий за вызов и остался бы зелёным при снятой провязке:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		if twin := catalogTwinFaultsOn(t, tree); len(twin) != 0 {
			t.Fatalf("ось «имя цели только в комментарии» задела соседа (%d):\n  %s",
				len(twin), strings.Join(twin, "\n  "))
		}
		t.Logf("ось «имя цели только в комментарии»: 1 находка — комментарий за вызов НЕ зачтён\n  %s", got[0])
	}

	// ── ПРОГОН 4: ЗАКОННЫЙ БЛИЗНЕЦ СПОСОБЕН КРАСНЕТЬ ──────────────────────
	//
	// Без него молчание соседа в прогонах 2 и 3 неотличимо от молчания мёртвого:
	// гейт про соседнюю цель мог бы не краснеть ни на чём.
	{
		tree := catalogWiredTree()
		tree.workflow = strings.Replace(catalogCallingWorkflow,
			"      - name: rest-route-table staleness\n"+
				"        working-directory: gateway\n"+
				"        run: make rest-route-table-check\n",
			"      - name: что-то другое\n        run: go vet ./...\n", 1)

		twin := catalogTwinFaultsOn(t, tree)
		if len(twin) != 1 {
			t.Fatalf("инъекция близнеца: гейт цели %s обязан дать 1 находку, дал %d — он не способен "+
				"падать, и его молчание в прогонах 2 и 3 ничего не доказывало:\n  %s",
				catalogTwinTarget, len(twin), strings.Join(twin, "\n  "))
		}
		if got := catalogParityFaultsOn(t, tree); len(got) != 0 {
			t.Fatalf("инъекция близнеца задела предмет гейта (%d находок):\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		t.Logf("инъекция близнеца: 1 находка у %s, 0 у %s — предметы разделимы и оба гейта живы",
			catalogTwinTarget, catalogParityTarget)
	}

	// ── ОСЬ: ВЫЗОВ ЕСТЬ, А РАБОЧИЙ КАТАЛОГ СНЯТ ───────────────────────────
	//
	// Так провязку ломают, не тронув ни одной строки со словом `make`. Гейт,
	// не читающий `working-directory`, зачёл бы такой шаг за вызов и остался бы
	// зелёным на конвейере, который падает на «No rule to make target».
	{
		tree := catalogWiredTree()
		tree.workflow = strings.Replace(catalogCallingWorkflow,
			"        working-directory: gateway\n        run: make permission-catalog-check\n",
			"        run: make permission-catalog-check\n", 1)

		got := catalogParityFaultsOn(t, tree)
		if len(got) != 2 {
			t.Fatalf("ось «рабочий каталог снят»: ожидалось 2 находки (никто не зовёт + зовут не там), "+
				"получено %d — гейт зачёл вызов из чужого каталога за провязку:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		var wrongDir bool
		for _, f := range got {
			if strings.Contains(f, "не в том каталоге") && strings.Contains(f, "корень дерева") {
				wrongDir = true
			}
		}
		if !wrongDir {
			t.Errorf("находки не назвали ЧУЖОЙ каталог вызова:\n  %s", strings.Join(got, "\n  "))
		}
		t.Logf("ось «рабочий каталог снят»: 2 находки, чужой каталог назван\n  %s", strings.Join(got, "\n  "))
	}

	// ── ПУСТОЙ ОБХОД — не «находок нет», а «прочитано ноль» ────────────────
	{
		root := t.TempDir()
		if _, err := readMakeTargetWiring(root, catalogMakefileDir, catalogParityTarget); err == nil {
			t.Error("пустое дерево прочиталось без ошибки — «ноль находок» стало бы неотличимо " +
				"от «ноль прочитанного»")
		} else {
			t.Logf("пустой обход: ошибка чтения получена — %v", err)
		}
	}

	// ── НЕРАЗБИРАЕМЫЙ КОНВЕЙЕР — тоже «прочитано ноль», а не «вызова нет» ──
	{
		tree := catalogWiredTree()
		tree.workflow = "jobs:\n  authz-artifacts:\n   steps:\n  - name: рваный отступ\n\t\trun: x\n"
		if _, err := readMakeTargetWiring(writeJudgeWiringTree(t, tree), catalogMakefileDir, catalogParityTarget); err == nil {
			t.Error("неразбираемый файл конвейера прочитался без ошибки — «вызова нет» стало бы " +
				"неотличимо от «не прочитано ничего»")
		} else {
			t.Logf("неразбираемый конвейер: ошибка получена — %v", err)
		}
	}
}
