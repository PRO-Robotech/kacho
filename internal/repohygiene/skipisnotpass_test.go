// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// skipisnotpass_test.go — вердикт о ДЕРЕВЕ: у каждой работы, чьи шаги гасит
// отметка, есть перепись исходов, роняющая работу при погашенном вердикте.
//
// Разбор предмета — в шапке skipisnotpass.go. Здесь только обход дерева и
// перепись объёма осмотренного.
package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkippedVerdictDoesNotPassAsGreen(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files := listWorkflows(t, root)
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто",
			workflowsDir)
	}

	var total skipVerdictCensus
	var all []string
	hitAll := map[[2]string]bool{}
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("не прочитан %s: %v", rel, err)
		}
		findings, census, hit := checkSkipIsNotPass(rel, string(raw))
		total.add(census)
		all = append(all, findings...)
		for k := range hit {
			hitAll[k] = true
		}
	}

	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: файлов конвейера %d, работ с гашением %d, "+
		"гасимых шагов %d, работ с переписью исходов %d, послаблений сработало %d из %d",
		total.Files, total.Jobs, total.Gated, total.WithOwner, total.Exempt,
		len(skipVerdictExempt))

	// ПРЕДПОСЫЛКА ГЕЙТА: предмет существует. Ноль гасимых шагов означает, что
	// отметка из дерева ушла, — тогда снимать надо и гейт вместе с предметом, а
	// не молча зеленеть, обещая защиту.
	if total.Gated == 0 {
		t.Fatalf("ни одного шага, гасимого отметкой %s, в %s — у гейта не осталось "+
			"предмета. Либо отметка снята из дерева (тогда снимайте и этот гейт), "+
			"либо сломан обход", skipVerdictFlag, workflowsDir)
	}

	// Послабление обязано истекать САМО: запись, не нашедшая своей работы, есть
	// утверждение о дереве, которого в дереве больше нет.
	for key, reason := range skipVerdictExempt {
		if !hitAll[key] {
			t.Errorf("послабление %s / работа '%s' не нашло своей работы — "+
				"снимите запись, она переживает свой предмет (причина записи: %s)",
				key[0], key[1], reason)
		}
	}

	for _, f := range all {
		t.Error(f)
	}
}
