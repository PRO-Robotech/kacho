// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestcheckwiring_injection_test.go — доказательство, что гейт
// провязки СПОСОБЕН упасть и способен смолчать (задача #1851).
//
// # Почему на синтетике, а не на дереве продукта
//
// Гейт обязан быть зелёным на дереве продукта — иначе его нечем читать. Значит
// его способность краснеть на дереве продукта не наблюдается НИКОГДА, и «зелёный»
// там неотличим от «мёртвый». Инъекция подаёт вход, которого в дереве нет by
// construction, и требует обеих способностей по каждой оси отдельно.
//
// # Дерево заводится ИЗОЛИРОВАННО
//
// Каждый вход — свой `t.TempDir()`. Ни одна проба не пишет в рабочую копию, из
// которой запущена: испорченный индекс общего клона заставляет гейты, читающие
// дерево, выдумывать красные вердикты, и отличить это от настоящей находки
// нельзя ничем.
//
// # Законный близнец у КАЖДОЙ оси
//
// Односторонняя проверка зеленела бы на дереве, где всё сломано одинаково.
// Поэтому рядом с каждой находкой стоит вход той же формы, на котором гейт
// обязан МОЛЧАТЬ: сервис, законно не объявляющий цель, и вызов, записанный
// исполняемой строкой, а не прозой.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifestWiringTree — что положить в синтетическое дерево.
type manifestWiringTree struct {
	// makefiles — сервис → содержимое services/<svc>/Makefile.
	makefiles map[string]string
	// workflow — содержимое .github/workflows/ci.yaml.
	workflow string
	// localRunner — содержимое scripts/ci-local.sh.
	localRunner string
}

// declaringMakefile — Makefile, ОБЪЯВЛЯЮЩИЙ цель, вместе с прозой, которая её
// объясняет: в настоящем Makefile имя цели стоит и в шапке, и примером вызова.
const declaringMakefile = `# module-manifest-check — форму манифеста домена судит ОДИН исполнитель.
# Вызов: ` + "`make -C services/iam module-manifest-check`" + `
.PHONY: module-manifest-check
module-manifest-check:
	@./tools/module-manifest-check.sh
`

// silentMakefile — законный близнец: сервис, который цель не объявляет вовсе.
const silentMakefile = `.PHONY: audit-list-filter
audit-list-filter:
	@./tools/audit-list-filter.sh
`

const callingWorkflow = `jobs:
  authz-artifacts:
    steps:
      - name: манифесты модулей — форму судит один загрузчик
        run: |
          rc=0
          make -C services/iam module-manifest-check || rc=$?
`

const callingLocalRunner = `#!/usr/bin/env bash
manifest_form_check() {
    ( cd "$ROOT" && make -C services/iam module-manifest-check ) > "$log" 2>&1 || rc=$?
}
`

// writeManifestWiringTree раскладывает вход на диск и возвращает корень.
func writeManifestWiringTree(t *testing.T, tree manifestWiringTree) string {
	t.Helper()
	root := t.TempDir()

	for svc, body := range tree.makefiles {
		dir := filepath.Join(root, "services", svc)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("не заведён %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
			t.Fatalf("не записан Makefile %s: %v", svc, err)
		}
	}
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("не заведён %s: %v", wfDir, err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "ci.yaml"), []byte(tree.workflow), 0o644); err != nil {
		t.Fatalf("не записан workflow: %v", err)
	}
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("не заведён %s: %v", scriptsDir, err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "ci-local.sh"), []byte(tree.localRunner), 0o644); err != nil {
		t.Fatalf("не записан прогонщик: %v", err)
	}
	return root
}

// wiringFaultsOn — находки гейта на синтетическом дереве.
func wiringFaultsOn(t *testing.T, tree manifestWiringTree) []string {
	t.Helper()
	w, err := readManifestCheckWiring(writeManifestWiringTree(t, tree))
	if err != nil {
		t.Fatalf("синтетическое дерево не прочитано: %v", err)
	}
	if w.MakefilesRead == 0 || w.WorkflowsRead == 0 || w.LocalRunnersRead == 0 {
		t.Fatalf("обход синтетики пуст (Makefile %d · workflow %d · прогонщиков %d) — "+
			"инъекция подала бы вход, которого гейт не видит, и её зелёное ничего не значило бы",
			w.MakefilesRead, w.WorkflowsRead, w.LocalRunnersRead)
	}
	return findManifestCheckWiringFaults(w)
}

// intactTree — полностью провязанное дерево: контроль для всех осей.
func intactTree() manifestWiringTree {
	return manifestWiringTree{
		makefiles:   map[string]string{"iam": declaringMakefile, "vpc": silentMakefile},
		workflow:    callingWorkflow,
		localRunner: callingLocalRunner,
	}
}

// TestModuleManifestCheckWiringGate_Injection — обе способности по каждой оси.
func TestModuleManifestCheckWiringGate_Injection(t *testing.T) {
	// ── КОНТРОЛЬ: всё цело — гейт обязан молчать ────────────────────────────
	//
	// Стоит первым и не является формальностью: без него всякая находка ниже
	// объяснялась бы гейтом, который краснеет на любом входе.
	if got := wiringFaultsOn(t, intactTree()); len(got) != 0 {
		t.Fatalf("КОНТРОЛЬ: на провязанном дереве гейт нашёл %d — он краснеет на исправном "+
			"входе, и ни одна находка ниже ничего не доказывает:\n  %s",
			len(got), strings.Join(got, "\n  "))
	}
	// Законный близнец контроля: сервис `vpc` цель не объявляет и её никто не
	// зовёт — это НЕ находка. Гейт, требующий цель от каждого сервиса, краснел бы
	// на шести сервисах из семи и был бы снят первым же читателем.
	t.Log("контроль: провязанное дерево — 0 находок; сервис без цели молчит")

	// ── ИНЪЕКЦИЯ НОВОГО, ось 1: конвейер цель не зовёт ─────────────────────
	{
		tree := intactTree()
		tree.workflow = "jobs:\n  authz-artifacts:\n    steps:\n      - name: что-то другое\n        run: go build ./...\n"
		got := wiringFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("ось «конвейер не зовёт»: ожидалась ровно 1 находка, получено %d:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		if !strings.Contains(got[0], "services/iam") || !strings.Contains(got[0], "ни один шаг конвейера") {
			t.Errorf("находка не называет предмет и координату: %s", got[0])
		}
		t.Logf("инъекция «конвейер не зовёт»: 1 находка — %s", got[0])
	}

	// ── ИНЪЕКЦИЯ НОВОГО, ось 2: локальный прогонщик цель не зовёт ──────────
	//
	// Отдельная ось, а не половина предыдущей: внутри накопительной линии
	// вердикта конвейера не будет вовсе, поэтому провязка, оставшаяся в одном
	// носителе из двух, — это ровно тот случай, ради которого перепись гейта
	// печатает носители порознь.
	{
		tree := intactTree()
		tree.localRunner = "#!/usr/bin/env bash\nrun \"go build\" go build ./...\n"
		got := wiringFaultsOn(t, tree)
		if len(got) != 1 {
			t.Fatalf("ось «прогонщик не зовёт»: ожидалась ровно 1 находка, получено %d:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		if !strings.Contains(got[0], localRunnerRel) {
			t.Errorf("находка не называет носителя: %s", got[0])
		}
		t.Logf("инъекция «прогонщик не зовёт»: 1 находка — %s", got[0])
	}

	// ── ИНЪЕКЦИЯ НОВОГО, ось 3: ПРОЗА вместо провязки ─────────────────────
	//
	// Несущая ось. Имя цели встречается в комментариях, которые её же объясняют,
	// и гейт, ищущий имя подстрокой, зеленел бы на собственном объяснении —
	// оставаясь зелёным при снятой провязке. Здесь оба носителя называют цель
	// ПОЛНОСТЬЮ и только в комментарии.
	{
		tree := intactTree()
		tree.workflow = "jobs:\n  authz-artifacts:\n    steps:\n" +
			"      # здесь когда-то звали: make -C services/iam module-manifest-check\n" +
			"      - name: что-то другое\n        run: go build ./...\n"
		tree.localRunner = "#!/usr/bin/env bash\n" +
			"# провязка снята: make -C services/iam module-manifest-check\n" +
			"run \"go build\" go build ./...\n"
		got := wiringFaultsOn(t, tree)
		if len(got) != 2 {
			t.Fatalf("ось «проза вместо провязки»: ожидалось 2 находки (оба носителя), получено %d — "+
				"гейт зачёл комментарий за вызов и остался бы зелёным при снятой провязке:\n  %s",
				len(got), strings.Join(got, "\n  "))
		}
		t.Logf("инъекция «проза вместо провязки»: 2 находки — комментарий за вызов НЕ зачтён")
	}

	// ── ИНЪЕКЦИЯ НОВОГО, ось 4: обратное направление ──────────────────────
	//
	// Носитель зовёт цель у сервиса, который её не объявляет: шаг молча
	// позеленел бы на несуществующей цели. Без этой оси гейт был бы
	// односторонним и пропускал бы провязку в пустоту.
	{
		tree := intactTree()
		tree.workflow = strings.ReplaceAll(callingWorkflow, "services/iam", "services/storage")
		tree.localRunner = strings.ReplaceAll(callingLocalRunner, "services/iam", "services/storage")
		got := wiringFaultsOn(t, tree)
		// iam объявляет и никем не зван (2 находки) + storage зван и не объявляет
		// у обоих носителей (2 находки).
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
		t.Logf("инъекция «зовут несуществующую цель»: 4 находки, из них обратных %d", reverse)
	}

	// ── ПУСТОЙ ОБХОД — не «находок нет», а «прочитано ноль» ────────────────
	//
	// Дерево без services/ обязано давать ОШИБКУ ЧТЕНИЯ, а не тихий зелёный:
	// иначе гейт, смотрящий не туда, отчитывался бы чистым.
	{
		root := t.TempDir()
		if _, err := readManifestCheckWiring(root); err == nil {
			t.Error("пустое дерево прочиталось без ошибки — «ноль находок» стало бы неотличимо " +
				"от «ноль прочитанного»")
		} else {
			t.Logf("пустой обход: ошибка чтения получена — %v", err)
		}
	}
}
