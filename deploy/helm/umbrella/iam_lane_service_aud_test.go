// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// iam_lane_service_aud_test.go — сторона ЛИЧНОСТИ у докерной полосы выдачи
// ВЫВОДИТСЯ из объявленной стороны реестра, а не объявляется вторым умолчанием
// (задача #1184).
//
// Парная к services/registry/deploy/lane_service_aud_test.go: там та же
// четвёрка осей утверждается о стороне РЕЕСТРА. Обе стороны читают ОДНО
// объявление — `global.kacho.registry.serviceAud`, — потому что `global` есть
// единственное, что видно из ОБОИХ контекстов сабчартов (тот же довод, что у
// формирующих значений службы личности и административного перехода).
//
// Оси:
//
//	(а) объявленный единый источник доезжает до настроек iam;
//	(б) без него действует собственная ручка подчарта (одиночная установка);
//	(в) объявлены обе и не сходятся ⇒ рендер ОТКАЗЫВАЕТ, называя ОБЕ величины;
//	(г) законный близнец: объявлены обе и сходятся ⇒ рендер молчит.
package umbrella_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const iamChartDir = "charts/kaname"

// renderIAM рендерит подчарт kaname САМ ПО СЕБЕ (зависимостей у него нет,
// сеть не нужна). Отсутствие helm в CI — жёсткий провал, а не пропуск: гейт,
// молча ставший инертным на джобе, гейтящей мёрж, не является гейтом.
func renderIAM(t *testing.T, sets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	args := []string{"template", "iamlane", iamChartDir}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из теста
	return string(out), err
}

// laneServiceInConfig — значение `service:` в секции `registry-token:` карты настроек.
var registryTokenService = regexp.MustCompile(`(?m)^\s*registry-token:\s*$(?:\n\s+.*)*?\n\s+service:\s*"?([^"\n]*)"?`)

func laneServiceInConfig(t *testing.T, rendered string) string {
	t.Helper()
	m := registryTokenService.FindStringSubmatch(rendered)
	if m == nil {
		t.Fatalf("в отрендеренной карте настроек нет `registry-token.service` (осмотрено байт %d)", len(rendered))
	}
	return strings.TrimSpace(m[1])
}

// TestIAMLaneServiceAudComesFromTheSingleDeclaration — оси (а) и (б).
func TestIAMLaneServiceAudComesFromTheSingleDeclaration(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t, "global.kacho.registry.serviceAud=lane.example")
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	if got := laneServiceInConfig(t, out); got != "lane.example" {
		t.Errorf("сторона личности не выведена из объявленной стороны реестра: registry-token.service = %q, ждали lane.example", got)
	}

	out, err = renderIAM(t, "config.apiServer.registryToken.service=solo.example")
	if err != nil {
		t.Fatalf("рендер отказал: %v\n%s", err, out)
	}
	if got := laneServiceInConfig(t, out); got != "solo.example" {
		t.Errorf("без единого источника собственная ручка обязана действовать: registry-token.service = %q, ждали solo.example", got)
	}
}

// TestIAMLaneServiceAudRefusesUnlinkedSides — оси (в) и (г).
func TestIAMLaneServiceAudRefusesUnlinkedSides(t *testing.T) {
	t.Parallel()

	out, err := renderIAM(t,
		"global.kacho.registry.serviceAud=lane.example",
		"config.apiServer.registryToken.service=other.example")
	if err == nil {
		t.Fatalf("рендер ПРОШЁЛ на разошедшихся объявлениях — стороны полосы разъехались бы молча\n%s", out)
	}
	for _, want := range []string{"global.kacho.registry.serviceAud", "lane.example", "other.example"} {
		if !strings.Contains(out, want) {
			t.Errorf("отказ рендера не называет %q — оператору нечем чинить:\n%s", want, out)
		}
	}

	out, err = renderIAM(t,
		"global.kacho.registry.serviceAud=same.example",
		"config.apiServer.registryToken.service=same.example")
	if err != nil {
		t.Fatalf("сошедшиеся объявления обязаны рендериться, получен отказ: %v\n%s", err, out)
	}
	if got := laneServiceInConfig(t, out); got != "same.example" {
		t.Errorf("registry-token.service = %q, ждали same.example", got)
	}
}
