// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gatetargetwiring_injection_support_test.go — синтетическое дерево, на котором
// инъекции гейтов провязки наблюдают исход.
//
// # Почему на синтетике, а не на дереве продукта
//
// Гейт обязан быть зелёным на дереве продукта — иначе его нечем читать. Значит
// его способность краснеть там не наблюдается НИКОГДА, и «зелёный» неотличим от
// «мёртвый». Инъекция подаёт вход, которого в дереве нет by construction.
//
// # Дерево заводится ИЗОЛИРОВАННО
//
// Каждый вход — свой `t.TempDir()`. Ни одна проба не пишет в рабочую копию, из
// которой запущена: испорченный индекс общего клона заставляет гейты, читающие
// дерево, выдумывать красные вердикты, и отличить это от настоящей находки
// нельзя ничем.
//
// # Раскладка общая, тела — разные
//
// Судей-целей два, и у каждого своя инъекция со своими телами Makefile,
// конвейера и прогонщика. Общей остаётся раскладка на диске: один писатель —
// одна форма дерева, и расходиться ей негде.
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// judgeWiringTree — что положить в синтетическое дерево.
type judgeWiringTree struct {
	// makefiles — сервис → содержимое services/<svc>/Makefile.
	makefiles map[string]string
	// workflow — содержимое .github/workflows/ci.yaml.
	workflow string
	// localRunner — содержимое scripts/ci-local.sh.
	localRunner string
}

// writeJudgeWiringTree раскладывает вход на диск и возвращает корень.
func writeJudgeWiringTree(t *testing.T, tree judgeWiringTree) string {
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

// judgeWiringFaultsOn — находки гейта на синтетическом дереве для цели `target`.
//
// Обход синтетики проверяется на непустоту ПЕРЕД тем, как читать находки: иначе
// инъекция подала бы вход, которого гейт не видит, и её зелёное ничего не
// значило бы.
func judgeWiringFaultsOn(t *testing.T, target string, tree judgeWiringTree) []string {
	t.Helper()
	w, err := readJudgeTargetWiring(writeJudgeWiringTree(t, tree), target)
	if err != nil {
		t.Fatalf("синтетическое дерево не прочитано: %v", err)
	}
	if w.MakefilesRead == 0 || w.WorkflowsRead == 0 || w.WorkflowStepsRead == 0 || w.LocalRunnersRead == 0 {
		t.Fatalf("обход синтетики пуст (Makefile %d · workflow %d · шагов run %d · прогонщиков %d) — "+
			"инъекция подала бы вход, которого гейт не видит, и её зелёное ничего не значило бы",
			w.MakefilesRead, w.WorkflowsRead, w.WorkflowStepsRead, w.LocalRunnersRead)
	}
	return findJudgeTargetWiringFaults(w)
}
