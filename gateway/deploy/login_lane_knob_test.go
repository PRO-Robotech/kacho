// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// login_lane_knob_test.go — ручка адреса полосы формы объявлена чартом (приёмка
// Ф3 Ф3-45; задание: «профили получают ручку там, где посадка own объявлена;
// если ни один не объявляет own — ручка объявляется в values с пустым значением
// и комментарием»).
//
// Что судится: ключ `authn.iamLoginLaneUrl` объявлен в базовом профиле; шаблон
// эмитит `KACHO_API_GATEWAY_IAM_LOGIN_LANE_URL` РОВНО из этого ключа и только
// когда он непуст — пустое значение под `external` не должно рождать пустую
// переменную, которую страж читал бы как «задано пустым».
func TestLoginLaneKnob_F3_45_IsDeclaredByTheChartAndRenderedFromOneKey(t *testing.T) {
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("чтение объявления чарта: %v", err)
	}
	var values struct {
		Authn map[string]any `yaml:"authn"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("values.yaml не разбирается: %v", err)
	}
	v, declared := values.Authn["iamLoginLaneUrl"]
	if !declared {
		t.Fatal("ключ authn.iamLoginLaneUrl не объявлен в базовом профиле — ручку адреса полосы формы нечем задать ни одному профилю")
	}
	if s, _ := v.(string); s != "" {
		t.Fatalf("базовый профиль объявляет посадку external и обязан оставить адрес полосы формы пустым, получено %q", s)
	}

	tpl, err := os.ReadFile("templates/deployment.yaml")
	if err != nil {
		t.Fatalf("чтение шаблона: %v", err)
	}
	text := string(tpl)
	if strings.Count(text, config.LoginLaneURLKnob) != 1 {
		t.Fatalf("шаблон обязан эмитить %s ровно один раз, найдено %d", config.LoginLaneURLKnob, strings.Count(text, config.LoginLaneURLKnob))
	}
	// Эмиссия — под условием непустоты ключа: `with .Values.authn.iamLoginLaneUrl`.
	guarded := regexp.MustCompile(`(?s)\{\{-?\s*with\s+\.Values\.authn\.iamLoginLaneUrl\s*\}\}.*?` +
		regexp.QuoteMeta(config.LoginLaneURLKnob) + `.*?\{\{-?\s*end\s*\}\}`)
	if !guarded.MatchString(text) {
		t.Fatal("переменная адреса полосы формы обязана эмититься под условием непустоты ключа authn.iamLoginLaneUrl")
	}
}
