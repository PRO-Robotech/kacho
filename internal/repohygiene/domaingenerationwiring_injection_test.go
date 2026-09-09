// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// domaingenerationwiring_injection_test.go — доказательство, что гейт провязки
// судьи разреза СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// # Зачем доказательство отдельным прогоном
//
// Гейт обязан быть зелёным на дереве продукта — иначе его нечем читать. Значит
// его способность краснеть там не наблюдается НИКОГДА, и «зелёный» неотличим от
// «мёртвый». Инъекция подаёт вход, которого в дереве нет by construction.
//
// # Форма инъекции: снимается НОВОЕ свойство, старое остаётся на месте
//
// Инъекция вида «завести ещё один элемент» доказательством не является: новый
// элемент нарушает всё, что требуется от элементов вообще, и красное пришло бы
// от соседа. Здесь Makefile не тронут — снимается ровно шаг вызова, — а рядом
// стоит законный близнец: соседняя цель ТОГО ЖЕ каталога, чей вызов на месте и
// который обязан молчать.
//
// # Почему близнецу дан СВОЙ прогон, где он краснеет
//
// Без него молчание близнеца в осях ниже неотличимо от молчания мёртвой
// проверки: механизм про соседнюю цель мог бы не краснеть ни на чём.
package repohygiene

import (
	"strings"
	"testing"
)

// domainGenTwinTarget — законный близнец: соседняя цель того же каталога,
// провязанная тем же способом. Её молчание при инъекции доказывает, что красное
// пришло от предмета, а не от соседа.
const domainGenTwinTarget = "permission-catalog-check"

// domainGenMakefile — синтетический gateway/Makefile: обе цели объявлены, у
// близнеца есть зависимость, как в дереве.
//
// Имя судьи стоит здесь и в КОММЕНТАРИИ тоже — намеренно: разбор обязан читать
// исполняемую часть, а не сырой текст.
const domainGenMakefile = `.PHONY: permission-catalog-check permission-catalog-copies-in-sync
.PHONY: domain-generation-check domain-generation-inject

permission-catalog-copies-in-sync:
	go test ./internal/repohygiene/ -run TestCatalogCopies

permission-catalog-check: permission-catalog-copies-in-sync
	./scripts/gen-permission-catalog.sh --check

# CI GATE разреза: судью зовут целью domain-generation-check.
domain-generation-check:
	./scripts/check-domain-generation.sh

domain-generation-inject:
	./scripts/inject-domain-generation-defects.sh
`

// domainGenCallingWorkflow — процесс, зовущий ОБЕ цели рабочим каталогом.
const domainGenCallingWorkflow = `jobs:
  authz-artifacts:
    steps:
      - name: permission-catalog staleness + copy-drift
        working-directory: gateway
        run: make permission-catalog-check
      - name: разрез — порождение вне дерева монорепо
        working-directory: gateway
        run: make domain-generation-check
`

// domainGenWiredTree — исправное дерево: обе цели объявлены и позваны.
func domainGenWiredTree() judgeWiringTree {
	return judgeWiringTree{
		dirMakefiles: map[string]string{"gateway": domainGenMakefile},
		workflow:     domainGenCallingWorkflow,
		localRunner:  "#!/usr/bin/env bash\ngo build ./...\n",
		makefiles:    map[string]string{"iam": "module-manifest-check:\n\techo ok\n"},
	}
}

func domainGenFaultsOn(t *testing.T, tree judgeWiringTree) []string {
	t.Helper()
	return dirTargetWiringFaultsOn(t, domainGenerationMakefileDir, domainGenerationTarget, tree)
}

func domainGenTwinFaultsOn(t *testing.T, tree judgeWiringTree) []string {
	t.Helper()
	return dirTargetWiringFaultsOn(t, domainGenerationMakefileDir, domainGenTwinTarget, tree)
}

// TestDomainGenerationWiringGate_Injection — пять прогонов, каждый своей осью.
func TestDomainGenerationWiringGate_Injection(t *testing.T) {
	t.Parallel()

	// ── ПРОГОН 1, КОНТРОЛЬ: провязаны обе цели — молчат обе ────────────────
	//
	// Стоит первым и не является формальностью: без него всякая находка ниже
	// объяснялась бы гейтом, который краснеет на любом входе.
	{
		intact := domainGenWiredTree()
		if got := domainGenFaultsOn(t, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: на провязанном дереве гейт цели %s нашёл %d — он краснеет на исправном "+
				"входе, и ни одна находка ниже ничего не доказывает:\n  %s",
				domainGenerationTarget, len(got), strings.Join(got, "\n  "))
		}
		if got := domainGenTwinFaultsOn(t, intact); len(got) != 0 {
			t.Fatalf("КОНТРОЛЬ: близнец %s краснеет на том же входе (%d):\n  %s",
				domainGenTwinTarget, len(got), strings.Join(got, "\n  "))
		}
		t.Log("контроль: судья разреза и его законный близнец провязаны — 0 находок у обоих")
	}

	// ── ПРОГОН 2: ШАГ ВЫЗОВА СНЯТ ИЗ ПРОЦЕССА ──────────────────────────────
	//
	// Ровно тот дефект, ради которого гейт заведён, и ровно то состояние, в
	// котором судья разреза прожил свою жизнь до этой задачи: цель исполнима и
	// не исполняется. Makefile не тронут, связь внутри него цела, близнец
	// по-прежнему позван.
	{
		tree := domainGenWiredTree()
		tree.workflow = strings.Replace(domainGenCallingWorkflow,
			"      - name: разрез — порождение вне дерева монорепо\n"+
				"        working-directory: gateway\n"+
				"        run: make domain-generation-check\n",
			"      - name: что-то другое\n        run: go build ./...\n", 1)

		got := domainGenFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("инъекция «шаг вызова снят»: ожидалась 1 находка, получено %d:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		// Диагностика обязана называть ПРЕДМЕТ — цель, процесс и каталог, — а не
		// «связь не найдена»: находка, называющая симптом, посылает читателя
		// искать не там, и на неё тратят прогон, прежде чем снять гейт как
		// непонятный.
		if !strings.Contains(got[0], domainGenerationTarget) {
			t.Errorf("находка не называет ЦЕЛЬ: %s", got[0])
		}
		if !strings.Contains(got[0], "ci.yaml") {
			t.Errorf("находка не называет ПРОЦЕСС: %s", got[0])
		}
		if !strings.Contains(got[0], domainGenerationMakefileDir+"/Makefile") {
			t.Errorf("находка не называет каталог объявления: %s", got[0])
		}
		if twin := domainGenTwinFaultsOn(t, tree); len(twin) != 0 {
			t.Fatalf("инъекция задела близнеца %s (%d находок) — красное пришло бы от него:\n  %s",
				domainGenTwinTarget, len(twin), strings.Join(twin, "\n  "))
		}
		t.Logf("инъекция «шаг вызова снят»: 1 находка, называет цель, процесс и каталог; близнец молчит\n  %s", got[0])
	}

	// ── ПРОГОН 3: ИМЯ ЦЕЛИ СТОИТ ТОЛЬКО В КОММЕНТАРИИ ПРОЦЕССА ─────────────
	//
	// Имя судьи встречается в прозе, которая его же объясняет, — в шапке
	// Makefile, в комментарии шага, на странице документации. Гейт, ищущий имя
	// подстрокой, зеленел бы на собственном объяснении, оставаясь зелёным при
	// снятой провязке.
	{
		tree := domainGenWiredTree()
		tree.workflow = `jobs:
  authz-artifacts:
    steps:
      - name: permission-catalog staleness + copy-drift
        working-directory: gateway
        run: make permission-catalog-check
      - name: что-то другое
        working-directory: gateway
        run: |
          # здесь когда-то звали: make domain-generation-check
          # и напрямую: make -C gateway domain-generation-check
          go build ./...
`
		got := domainGenFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("ось «имя цели только в комментарии»: ожидалась 1 находка, получено %d — гейт "+
				"зачёл комментарий за вызов и остался бы зелёным при снятой провязке:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		if twin := domainGenTwinFaultsOn(t, tree); len(twin) != 0 {
			t.Fatalf("ось «имя цели только в комментарии» задела близнеца (%d):\n  %s",
				len(twin), strings.Join(twin, "\n  "))
		}
		t.Logf("ось «имя цели только в комментарии»: 1 находка — комментарий за вызов НЕ зачтён\n  %s", got[0])
	}

	// ── ПРОГОН 4: РАБОЧИЙ КАТАЛОГ ШАГА СНЯТ ────────────────────────────────
	//
	// Провязку ломают, не тронув ни одной строки со словом make: снимают
	// `working-directory`, и голый `make domain-generation-check` адресуется
	// корню дерева, где такой цели нет. Шаг тогда падает ни на чём, а гейт,
	// judging по одному лишь присутствию слова, назвал бы провязку целой.
	{
		tree := domainGenWiredTree()
		tree.workflow = strings.Replace(domainGenCallingWorkflow,
			"      - name: разрез — порождение вне дерева монорепо\n"+
				"        working-directory: gateway\n"+
				"        run: make domain-generation-check\n",
			"      - name: разрез — порождение вне дерева монорепо\n"+
				"        run: make domain-generation-check\n", 1)

		got := domainGenFaultsOn(t, tree)
		if len(got) == 0 {
			t.Fatal("ось «рабочий каталог снят»: находок нет — гейт зачёл вызов в чужом каталоге " +
				"за провязку, то есть остаётся зелёным на шаге, который судью не исполнит")
		}
		joined := strings.Join(got, "\n  ")
		if !strings.Contains(joined, "корень дерева") {
			t.Errorf("находка не называет КАТАЛОГ, которому адресован вызов: %s", joined)
		}
		if twin := domainGenTwinFaultsOn(t, tree); len(twin) != 0 {
			t.Fatalf("ось «рабочий каталог снят» задела близнеца (%d):\n  %s",
				len(twin), strings.Join(twin, "\n  "))
		}
		t.Logf("ось «рабочий каталог снят»: %d находок, каталог назван; близнец молчит\n  %s", len(got), joined)
	}

	// ── ПРОГОН 5: ЗАКОННЫЙ БЛИЗНЕЦ СПОСОБЕН КРАСНЕТЬ ──────────────────────
	//
	// Без него молчание близнеца в прогонах 2-4 неотличимо от молчания мёртвой
	// проверки. Снимается вызов БЛИЗНЕЦА, и тогда краснеть обязан он, а судья
	// разреза — молчать: предметы разделимы, общий механизм не путает их.
	{
		tree := domainGenWiredTree()
		tree.workflow = strings.Replace(domainGenCallingWorkflow,
			"      - name: permission-catalog staleness + copy-drift\n"+
				"        working-directory: gateway\n"+
				"        run: make permission-catalog-check\n",
			"      - name: что-то другое\n        run: go build ./...\n", 1)

		twin := domainGenTwinFaultsOn(t, tree)
		if len(twin) == 0 {
			t.Fatal("КОНТРОЛЬ БЛИЗНЕЦА: вызов близнеца снят, а он молчит — значит его молчание в " +
				"прогонах 2-4 ничего не доказывало")
		}
		if got := domainGenFaultsOn(t, tree); len(got) != 0 {
			t.Fatalf("снятие вызова БЛИЗНЕЦА покраснило судью разреза (%d) — предметы не разделены:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		t.Logf("контроль близнеца: снят его вызов — краснеет он (%d), судья разреза молчит", len(twin))
	}
}
