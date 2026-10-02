// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_base_default_test.go — посадка личности, объявленная
// БАЗОВЫМ профилем чарта края, разбирается тем же читателем, что и процесс
// (kacho#2862).
//
// ПРЕДМЕТ. У процесса умолчания посадки нет, поэтому оно живёт в базовом
// профиле чарта: рендер без профиля стенда (подчарт, поставленный сам по себе,
// либо зонт без накладки) отдаёт процессу ровно это значение через
// KACHO_API_GATEWAY_IDENTITY_PROVIDER. Если словарь фундамента значения не
// производит, main.go падает на ResolvedIdentityProvider ещё до стража, и под
// уходит в перезапуски с текстом разбора, а не с именем ручки профиля.
//
// Прежде этого не держал никто: пробы стендов судят цепочки deploy/stacks.txt,
// а каждая цепочка объявляет посадку сама и умолчания чарта не читает. Проба
// согласия половин (deploy/helm/umbrella/identity_posture_profiles_test.go)
// сравнивает базовые значения ДРУГ С ДРУГОМ и о словаре не спрашивает: два
// одинаково снятых значения она пропускает.
//
// КАК СУДИТСЯ. Значение идёт тем же путём, что в поде: переменная окружения →
// config.Load → ResolvedIdentityProvider → Validate с именем ручки. Второго
// разбора здесь нет — зовётся код края.
package deploy

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// judgeBaseIdentityPosture — исход пути значения от переменной до стража.
// nil — процесс принимает значение; иначе текст отказа процесса.
func judgeBaseIdentityPosture(t *testing.T, raw string) error {
	t.Helper()
	t.Setenv(config.IdentityProviderKnob, raw)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	p, err := cfg.ResolvedIdentityProvider()
	if err != nil {
		return err
	}
	return p.Validate(config.IdentityProviderKnob)
}

func TestBaseProfileIdentityPostureIsAcceptedByTheEdge(t *testing.T) {
	raw, err := os.ReadFile("values.yaml")
	if err != nil {
		t.Fatalf("чтение базового профиля: %v", err)
	}
	var values struct {
		Authn map[string]any `yaml:"authn"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("values.yaml не разбирается: %v", err)
	}
	v, declared := values.Authn["identityProvider"]
	s, _ := v.(string)
	t.Logf("перепись: базовый профиль края · ключ authn.identityProvider объявлен=%v · значение %q", declared, s)
	if !declared || strings.TrimSpace(s) == "" {
		t.Fatal("базовый профиль края посадку не объявляет — умолчания нет ни у него, ни у процесса")
	}
	if err := judgeBaseIdentityPosture(t, s); err != nil {
		t.Fatalf("рендер без профиля стенда кладёт краю посадку %q, и процесс её не принимает: %v", s, err)
	}
}

// Инъекция в обе стороны: снятое словарём значение находится текстом процесса,
// законный близнец молчит. Без неё зелёный на дереве неотличим от пробы,
// которая не умеет падать.
func TestBaseIdentityPostureJudge_WithdrawnValueIsRefusedByTheEdge(t *testing.T) {
	err := judgeBaseIdentityPosture(t, "external")
	if err == nil {
		t.Fatal("снятая посадка external принята — проба не способна покраснеть на своём предмете")
	}
	if !strings.Contains(err.Error(), config.IdentityProviderKnob) || !strings.Contains(err.Error(), `"external"`) {
		t.Fatalf("отказ обязан назвать ручку и значение, получено: %v", err)
	}
}

func TestBaseIdentityPostureJudge_LawfulTwinIsAccepted(t *testing.T) {
	if err := judgeBaseIdentityPosture(t, "own"); err != nil {
		t.Fatalf("законная посадка own отвергнута: %v", err)
	}
}
