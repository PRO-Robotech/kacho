// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_posture_gate_is_not_narrowed_injection_test.go — доказательство того,
// что держатель полноты гейта посадки СПОСОБЕН УПАСТЬ и падает ровно на своём
// предмете.
//
// # Почему инъекция настоящим текстом дерева
//
// Предмет держателя — провязка рецептов, и она есть текст. Синтетический
// Makefile доказывал бы о своей копии; здесь берётся ТЕКСТ ДЕРЕВА и в нём
// возвращается ровно один факт — то состояние, из которого задача #2390 и
// выведена. Суждение зовётся то же самое (`judgePostureGateWiring`).
//
// # Осей четыре, у каждой законный близнец
//
//  1. СУЖЕНИЕ      подъём зовёт узкий гейт → красное, называющее сужение;
//  2. ПРОБРОС      исключения не пробрасываются → красное;
//  3. ПРОФИЛЬ      цель не пинит боевой профиль → красное;
//  4. ПУСТОТА      рецепт не распознан → ОТКАЗ, а не «нарушений 0».
//
// Законный близнец у первых трёх один и тот же и обязателен: НЕТРОНУТЫЙ текст
// дерева обязан давать молчание. Без него красное по любой оси было бы
// неотличимо от гейта, который краснеет всегда.
package deploy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// treeWiring — тексты дерева, которые судит держатель.
func treeWiring(t *testing.T) (string, string) {
	t.Helper()
	return readTreeFile(t, shardWorkflow), readTreeFile(t, standMakefile)
}

// injectOnce — ровно ОДНА замена в тексте, с требованием, чтобы предмет замены
// нашёлся: инъекция, ничего не изменившая, доказывает не способность держателя
// упасть, а собственную безвредность.
func injectOnce(t *testing.T, body, old, new string) string {
	t.Helper()
	require.Containsf(t, body, old,
		"инъекция не нашла, что портить (%q): она бы прошла, ничего не изменив", old)
	return strings.Replace(body, old, new, 1)
}

// TestPostureGateHolderIsSilentOnTheTree — ЗАКОННЫЙ БЛИЗНЕЦ всех осей ниже.
func TestPostureGateHolderIsSilentOnTheTree(t *testing.T) {
	wf, mk := treeWiring(t)
	w := judgePostureGateWiring(t, wf, mk)
	require.Emptyf(t, w.findings,
		"держатель краснеет на нетронутом дереве — тогда его красное ничего не значит:\n%s",
		strings.Join(w.findings, "\n"))
	require.NotZero(t, w.RecipeLen, "рецепт подъёма пуст — вердикт был бы о пустоте")
}

func TestPostureGateHolderFallsOnANarrowedGate(t *testing.T) {
	wf, mk := treeWiring(t)
	// ОДИН факт: подъём зовёт УЗКИЙ гейт. Ровно так и было до #2390.
	mk = injectOnce(t, mk,
		"$(MAKE) --no-print-directory assert-production-posture POSTURE_SKIP=",
		"$(MAKE) --no-print-directory assert-forwarder-narrowing POSTURE_SKIP=")
	mk += "\nassert-forwarder-narrowing: guard-kind-context\n" +
		"\t@POSTURE_PROFILE=dev POSTURE_SKIP=\"$(POSTURE_SKIP)\" bash scripts/assert-production-posture.sh\n"

	w := judgePostureGateWiring(t, wf, mk)
	joined := strings.Join(w.findings, "\n")
	require.NotEmpty(t, w.findings, "держатель промолчал на возвращённом сужении — он не держатель")
	require.Contains(t, joined, "assert-forwarder-narrowing",
		"находка не называет цель, которой судят посадку: читатель пойдёт искать не там")
	require.Contains(t, joined, "СУЖЕНИЕ",
		"находка называет не тот предмет: узкая цель — это сужение гейта, а не его отсутствие")
}

func TestPostureGateHolderFallsWhenSkipIsNotForwarded(t *testing.T) {
	wf, mk := treeWiring(t)
	// ОДИН факт: проброс исключений снят — и в вызове, и в рецепте цели.
	mk = injectOnce(t, mk,
		"assert-production-posture POSTURE_SKIP=\"$(POSTURE_SKIP)\"",
		"assert-production-posture")
	mk = injectOnce(t, mk,
		"@POSTURE_PROFILE=production POSTURE_SKIP=\"$(POSTURE_SKIP)\" bash",
		"@POSTURE_PROFILE=production bash")

	w := judgePostureGateWiring(t, wf, mk)
	joined := strings.Join(w.findings, "\n")
	require.NotEmpty(t, w.findings, "держатель промолчал на снятом пробросе исключений")
	require.Contains(t, joined, "POSTURE_SKIP", "находка не называет предмет")
	require.NotContains(t, joined, "СУЖЕНИЕ",
		"держатель смешал две находки: цель осталась полной, снят только проброс")
}

func TestPostureGateHolderFallsWhenProfileIsNotPinned(t *testing.T) {
	wf, mk := treeWiring(t)
	// ОДИН факт: цель гейта не пинит профиль и наследует его из окружения.
	mk = injectOnce(t, mk,
		"@POSTURE_PROFILE=production POSTURE_SKIP=\"$(POSTURE_SKIP)\" bash",
		"@POSTURE_SKIP=\"$(POSTURE_SKIP)\" bash")

	w := judgePostureGateWiring(t, wf, mk)
	joined := strings.Join(w.findings, "\n")
	require.NotEmpty(t, w.findings, "держатель промолчал на непиньемом профиле")
	require.Contains(t, joined, "POSTURE_PROFILE=production", "находка не называет предмет")
}

func TestPostureGateHolderRefusesAnUnreadableRecipe(t *testing.T) {
	wf, mk := treeWiring(t)
	// ОДИН факт: заголовок цели подъёма переименован — рецепта под этим именем
	// в тексте больше нет. Это «читать нечего», и держатель обязан ОТКАЗАТЬ,
	// а не отчитаться «нарушений 0».
	mk = injectOnce(t, mk, "\ndev-up:", "\ndev-up-renamed:")

	sub := &testing.T{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		judgePostureGateWiring(sub, wf, mk)
	}()
	<-done
	require.Truef(t, sub.Failed(),
		"держатель отчитался о нераспознанном рецепте вместо отказа: «ноль находок» стало бы "+
			"неотличимо от «ноль прочитанного»")
}
