// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_producer_injection_test.go — доказательство того, что
// проверка ПРОВЯЗКИ производителя способна упасть, что она падает НЕ НА ВСЁМ и
// что она молчит на законных близнецах (задачи #1901, #1909).
//
// Прогонов ТРИ, и третий обязателен: без него молчание уже существующей проверки
// неотличимо от её смерти.
//
//	контроль          — целое дерево: молчат ОБЕ проверки;
//	инъекция новой    — дефект нового предмета: краснеет ТОЛЬКО новая;
//	инъекция прежней  — дефект прежнего предмета: краснеет ТОЛЬКО прежняя.
//
// Сверх трёх прогонов — по одному близнецу на КАЖДУЮ законную форму, которую
// распознаватель обязан знать: вызов, оставшийся в комментарии; упоминание
// `helm upgrade` внутри строкового литерала; голая точка у носителя ВНЕ каталога
// чарта; и ось порядка — вызов на месте, но позже helm.
//
// Поведение САМОГО производителя (имя выведено, перечень выведен, четыре исхода,
// предел объекта) доказывается там, где он живёт: pkg/modulemanifest/producer. Здесь —
// только его провязка в выкатку, потому что предмет у неё этот каталог.

import (
	"strings"
	"testing"
)

// syntheticCarrier — носитель, собранный проверкой. Дерева не трогает.
func syntheticCarrier(path, kind, text string) deployCarrier {
	return deployCarrier{Path: path, Kind: kind, Text: text}
}

// TestManifestProducerWiringAuditFallsAndStaysSilentOnItsTwin — прогонов ТРИ:
// контроль, инъекция нового предмета, инъекция прежнего; плюс близнецы по
// каждой законной форме.
func TestManifestProducerWiringAuditFallsAndStaysSilentOnItsTwin(t *testing.T) {
	carriers := bringUpCarriers(t)
	chart := readManifestDeliveryDecls(t)

	// ── ПРОГОН 1: КОНТРОЛЬ. Молчат обе.
	findings, census := auditBringUpPaths(carriers)
	t.Logf("контроль: %s", census.Summary())
	if len(findings) != 0 {
		t.Fatalf("контроль: на целом дереве находок %d: %v", len(findings), findings)
	}
	if f, _ := auditManifestDelivery(chart); len(f) != 0 {
		t.Fatalf("контроль: на целых объявлениях чарта находок %d: %v", len(f), f)
	}

	// ── ПРОГОН 2: ИНЪЕКЦИЯ НОВОГО. Вызов производителя снят из КАЖДОГО пути
	// выкатки. Краснеет ТОЛЬКО новая проверка: объявления чарта не тронуты.
	broken := make([]deployCarrier, 0, len(carriers))
	for _, c := range carriers {
		c.Text = strings.ReplaceAll(c.Text, manifestProducerToken, "seed-geo")
		broken = append(broken, c)
	}
	brokenFindings, brokenCensus := auditBringUpPaths(broken)
	if len(brokenFindings) == 0 {
		t.Fatalf("инъекция: вызов производителя снят, а проверка молчит (%s) — "+
			"она не измеряет своего предмета", brokenCensus.Summary())
	}
	if brokenCensus.Units != census.Units {
		t.Errorf("инъекция изменила число путей выкатки (%d против %d) — она уронила "+
			"не то, что проверяется", brokenCensus.Units, census.Units)
	}
	for _, want := range []string{"dev-up", "stack-up", "cutover-fe3455.sh", manifestProducerToken} {
		if !strings.Contains(strings.Join(brokenFindings, "\n"), want) {
			t.Errorf("находка не называет %q — чинить придётся перебором: %v", want, brokenFindings)
		}
	}
	if chartFindings, _ := auditManifestDelivery(chart); len(chartFindings) != 0 {
		t.Errorf("инъекция нового предмета покраснила ПРЕЖНЮЮ проверку (%v) — красное "+
			"приходит от соседа, и новая могла бы оказаться вакуумной", chartFindings)
	}

	// ── ПРОГОН 3: ИНЪЕКЦИЯ ПРЕЖНЕГО. Снят ключ монтирования у чарта.
	// Краснеет ТОЛЬКО прежняя: носители не тронуты — значит её молчание в
	// прогоне 2 было молчанием живой проверки, а не мёртвой.
	brokenChart := chart
	brokenChart.deployment = strings.ReplaceAll(brokenChart.deployment,
		".Values.manifests.mountPath", ".Values.opaSidecar.enabled")
	chartFindings, _ := auditManifestDelivery(brokenChart)
	if len(chartFindings) == 0 {
		t.Fatal("прежняя проверка молчит на снятом ключе монтирования — её молчание " +
			"в прогоне 2 ничего не доказывало")
	}
	if f, _ := auditBringUpPaths(carriers); len(f) != 0 {
		t.Errorf("инъекция прежнего предмета покраснила НОВУЮ проверку: %v", f)
	}

	// ── БЛИЗНЕЦ 1: вызов, оставшийся ТОЛЬКО в комментарии, исполнением не
	// является — иначе проверка зачла бы за вызов собственное объяснение.
	commented := []deployCarrier{syntheticCarrier("deploy/Makefile", "Makefile",
		"up:\n\t# зовём "+manifestProducerToken+" перед helm\n\thelm upgrade x ./helm/umbrella\n")}
	if f, _ := auditBringUpPaths(commented); len(f) == 0 {
		t.Error("вызов, оставшийся только в комментарии, зачтён за исполнение — " +
			"проверка судит текст, а не исполняемую часть")
	}

	// ── БЛИЗНЕЦ 2: `helm upgrade` внутри строкового литерала вызовом не
	// является. Носитель зовёт производителя и катит умбреллу правильно, а
	// прозу о helm печатает ДО этого — находки быть не должно.
	quoted := []deployCarrier{syntheticCarrier(umbrellaChartDir+"/roll.sh", "скрипт",
		"#!/usr/bin/env bash\n"+
			"log \"helm upgrade будет прогнан ниже\"\n"+
			"make -C ../.. "+manifestProducerToken+"\n"+
			"helm upgrade rel . -n kacho\n")}
	if f, c := auditBringUpPaths(quoted); len(f) != 0 {
		t.Errorf("законный близнец покраснел (%v) — упоминание helm в строковом литерале "+
			"зачтено за вызов; перепись: %s", f, c.Summary())
	}

	// ── БЛИЗНЕЦ 3: голая точка у носителя ВНЕ каталога чарта называет ЧУЖОЙ
	// чарт. Такой носитель путём выкатки умбреллы не является, и требовать от
	// него производителя нельзя.
	foreign := []deployCarrier{syntheticCarrier("deploy/scripts/other.sh", "скрипт",
		"#!/usr/bin/env bash\nhelm upgrade cert-manager . -n kacho\n")}
	if f, c := auditBringUpPaths(foreign); len(f) != 0 || c.Units != 0 {
		t.Errorf("чужой чарт зачтён за умбреллу (находок %d, путей %d) — голая точка "+
			"засчитана носителю вне каталога чарта: %v", len(f), c.Units, f)
	}
	// ...и та же точка У НОСИТЕЛЯ ИЗ КАТАЛОГА ЧАРТА обязана засчитаться —
	// иначе близнец выше зеленел бы оттого, что форма не распознаётся вовсе.
	own := []deployCarrier{syntheticCarrier(umbrellaChartDir+"/roll.sh", "скрипт",
		"#!/usr/bin/env bash\nhelm upgrade rel . -n kacho\n")}
	if f, c := auditBringUpPaths(own); len(f) == 0 || c.Units != 1 {
		t.Errorf("голая точка у носителя ИЗ каталога чарта не засчитана (находок %d, "+
			"путей %d) — близнец выше ничего не доказывал", len(f), c.Units)
	}

	// ── БЛИЗНЕЦ 4 (ось порядка): производитель зовётся, но ПОСЛЕ helm. Объект
	// появится позже, чем под его смонтирует, — это находка, и она обязана
	// называть порядок, а не отсутствие вызова.
	late := []deployCarrier{syntheticCarrier("deploy/Makefile", "Makefile",
		"up:\n\thelm upgrade x ./helm/umbrella\n\t$(MAKE) "+manifestProducerToken+"\n")}
	lateFindings, _ := auditBringUpPaths(late)
	if len(lateFindings) == 0 {
		t.Error("производитель, зовущийся ПОСЛЕ helm, находкой не признан — " +
			"доставка есть предусловие, а не следствие")
	} else if !strings.Contains(strings.Join(lateFindings, "\n"), "ПОСЛЕ первого прогона helm") {
		t.Errorf("находка называет симптом вместо причины: %v", lateFindings)
	}
}
