// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_producer_injection_test.go — доказательство того, что
// проверка ПРОВЯЗКИ производителя способна упасть, и что она падает НЕ НА ВСЁМ
// (задача #1901).
//
// Прогонов ТРИ, и третий обязателен: без него молчание уже существующей проверки
// неотличимо от её смерти.
//
//	контроль          — целое дерево: молчат ОБЕ проверки;
//	инъекция новой    — дефект нового предмета: краснеет ТОЛЬКО новая;
//	инъекция прежней  — дефект прежнего предмета: краснеет ТОЛЬКО прежняя.
//
// Поведение САМОГО производителя (имя выведено, перечень выведен, четыре исхода,
// предел объекта) доказывается там, где он живёт: tools/modulemanifests. Здесь —
// только его провязка в подъём стенда, потому что предмет у неё этот каталог.

import (
	"os"
	"strings"
	"testing"
)

// TestManifestProducerWiringAuditFallsAndStaysSilentOnItsTwin — прогонов ТРИ:
// контроль, инъекция нового предмета, инъекция прежнего.
func TestManifestProducerWiringAuditFallsAndStaysSilentOnItsTwin(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v", err)
	}
	makefile := string(raw)
	chart := readManifestDeliveryDecls(t)

	// ── ПРОГОН 1: КОНТРОЛЬ. Молчат обе.
	if findings, census := auditStandBringUpCalls(makefile); len(findings) != 0 {
		t.Fatalf("контроль: на целом Makefile находок %d (осмотрено байт %d): %v",
			len(findings), census.BytesJudged, findings)
	}
	if findings, _ := auditManifestDelivery(chart); len(findings) != 0 {
		t.Fatalf("контроль: на целых объявлениях чарта находок %d: %v", len(findings), findings)
	}

	// ── ПРОГОН 2: ИНЪЕКЦИЯ НОВОГО. Вызов производителя снят из рецепта подъёма.
	// Краснеет ТОЛЬКО новая проверка: объявления чарта не тронуты.
	broken := strings.ReplaceAll(makefile, manifestProducerTarget, "seed-geo")
	findings, census := auditStandBringUpCalls(broken)
	if len(findings) == 0 {
		t.Fatalf("инъекция: вызов производителя снят, а проверка молчит (осмотрено байт %d, "+
			"целей %d) — она не измеряет своего предмета", census.BytesJudged, census.Targets)
	}
	for _, want := range []string{"dev-up", "stack-up", manifestProducerTarget} {
		if !strings.Contains(strings.Join(findings, "\n"), want) {
			t.Errorf("находка не называет %q — чинить придётся перебором: %v", want, findings)
		}
	}
	if chartFindings, _ := auditManifestDelivery(chart); len(chartFindings) != 0 {
		t.Errorf("инъекция нового предмета покраснила ПРЕЖНЮЮ проверку (%v) — красное "+
			"приходит от соседа, и новая могла бы оказаться вакуумной", chartFindings)
	}

	// ── ПРОГОН 3: ИНЪЕКЦИЯ ПРЕЖНЕГО. Снят ключ монтирования у чарта.
	// Краснеет ТОЛЬКО прежняя: рецепт подъёма не тронут — значит её молчание в
	// прогоне 2 было молчанием живой проверки, а не мёртвой.
	brokenChart := chart
	brokenChart.deployment = strings.ReplaceAll(brokenChart.deployment,
		".Values.manifests.mountPath", ".Values.opaSidecar.enabled")
	chartFindings, _ := auditManifestDelivery(brokenChart)
	if len(chartFindings) == 0 {
		t.Fatal("прежняя проверка молчит на снятом ключе монтирования — её молчание " +
			"в прогоне 2 ничего не доказывало")
	}
	if f, _ := auditStandBringUpCalls(makefile); len(f) != 0 {
		t.Errorf("инъекция прежнего предмета покраснила НОВУЮ проверку: %v", f)
	}

	// ── ЗАКОННЫЙ БЛИЗНЕЦ. Цель, названная ТОЛЬКО в комментарии, исполнением не
	// является: иначе проверка зачла бы за вызов собственное объяснение.
	commented := strings.ReplaceAll(makefile,
		"\t$(MAKE) --no-print-directory module-manifests-configmap MODULE_MANIFESTS_STACK=dev; \\",
		"\techo нет; \\\n# $(MAKE) module-manifests-configmap")
	if f, _ := auditStandBringUpCalls(commented); len(f) == 0 {
		t.Error("вызов, оставшийся только в комментарии, зачтён за исполнение — " +
			"проверка судит текст, а не исполняемую часть")
	}
}
