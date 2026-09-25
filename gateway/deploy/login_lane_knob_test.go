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

// Ручка адреса СЛУШАТЕЛЯ ВЫДАЧИ службы — второй цели ретрансляции края, на
// которую уходят обе координаты церемонии авторизации (замысел LINE-A-1 §5.1б
// п. 2, полоса L13). Тот же порядок, что у ручки полосы формы: ключ
// `authn.iamIssuanceUrl` объявлен базовым профилем пустым (базовый профиль —
// посадка external, где ретрансляции нет), шаблон эмитит переменную РОВНО из
// этого ключа и только при непустом значении. Под `own` незаданная ручка —
// отказ старта края с её именем; значение задаёт профиль посадки.
func TestIssuanceKnob_L13_IsDeclaredByTheChartAndRenderedFromOneKey(t *testing.T) {
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
	v, declared := values.Authn["iamIssuanceUrl"]
	if !declared {
		t.Fatal("ключ authn.iamIssuanceUrl не объявлен в базовом профиле — ручку адреса слушателя выдачи нечем задать ни одному профилю")
	}
	if s, _ := v.(string); s != "" {
		t.Fatalf("базовый профиль объявляет посадку external и обязан оставить адрес слушателя выдачи пустым, получено %q", s)
	}
	tpl, err := os.ReadFile("templates/deployment.yaml")
	if err != nil {
		t.Fatalf("чтение шаблона: %v", err)
	}
	text := string(tpl)
	if n := strings.Count(text, config.IssuanceURLKnob); n != 1 {
		t.Fatalf("шаблон обязан эмитить %s ровно один раз, найдено %d", config.IssuanceURLKnob, n)
	}
	guarded := regexp.MustCompile(`(?s)\{\{-?\s*with\s+\.Values\.authn\.iamIssuanceUrl\s*\}\}\s*- name: ` +
		regexp.QuoteMeta(config.IssuanceURLKnob) + `\s+value: \{\{ \. \| quote \}\}\s*\{\{-?\s*end\s*\}\}`)
	if !guarded.MatchString(text) {
		t.Fatal("переменная адреса слушателя выдачи обязана эмититься под условием непустоты ключа authn.iamIssuanceUrl и из его значения")
	}
}
