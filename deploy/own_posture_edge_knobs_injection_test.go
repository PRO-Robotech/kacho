// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_edge_knobs_injection_test.go — ДОКАЗАТЕЛЬСТВО, ЧТО СУДЬЯ СОСЕДНЕГО
// ФАЙЛА СПОСОБЕН УПАСТЬ.
//
// Гейт, зелёный на дереве, ничего о себе не сообщает: тем же зелёным отвечает
// разбор, не дошедший до предмета. Здесь тот же судья (`judgeOwnPostureChain`)
// кормится СИНТЕТИКОЙ, в которой воспроизведён ровно тот дефект, ради которого
// гейт заведён, и рядом — его законный близнец, отличающийся ОДНИМ фактом.
//
// Синтетика, а не живое дерево: проба, привязанная к сегодняшней накладке,
// истекла бы вместе с ней — накладка однажды сойдётся с предикатом, и
// самопроверка покраснела бы на достижении своей цели.
package deploy_test

import (
	"strings"
	"testing"
)

// injectedProviderNames — состав чужого, каким его выводит гейт из зонта.
var injectedProviderNames = []string{"hydra", "kratos"}

// ownPostureChainWithForeignKnobsLive — накладка, объявившая посадку и НЕ
// снявшая ничего. Воспроизведён признак цепочки `own` на `bec320cf47d`.
func ownPostureChainWithForeignKnobsLive() map[string]any {
	return map[string]any{
		"api-gateway": map[string]any{
			"authn": map[string]any{"identityProvider": "own"},
			"tokenAcceptance": map[string]any{
				"issuers": "https://kaname.kacho.local,https://hydra.api.kacho.cloud",
			},
			"hydra": map[string]any{
				"introspectionUrl": "https://kacho-umbrella-hydra-admin-tls.kacho.svc:4445/admin/oauth2/introspect",
				"adminUrl":         "https://kacho-umbrella-hydra-admin-tls.kacho.svc:4445",
				"adminCa":          map[string]any{"secretName": "kacho-umbrella-hydra-admin-tls"},
			},
			"kratosPublicUrl": "http://kacho-umbrella-kratos-public.kacho.svc:80",
		},
		"registry": map[string]any{
			"tokenAcceptance": map[string]any{
				"issuers": "https://kaname.kacho.local,https://hydra.api.kacho.cloud",
			},
		},
	}
}

// ownPostureChainCleared — ЗАКОННЫЙ БЛИЗНЕЦ: та же посадка, ручки сняты.
func ownPostureChainCleared() map[string]any {
	return map[string]any{
		"api-gateway": map[string]any{
			"authn":           map[string]any{"identityProvider": "own"},
			"tokenAcceptance": map[string]any{"issuers": "https://kaname.kacho.local"},
			"hydra": map[string]any{
				"introspectionUrl": "",
				"adminUrl":         "",
				"adminCa":          map[string]any{"secretName": ""},
			},
			"kratosPublicUrl": "disabled",
		},
		"registry": map[string]any{
			"tokenAcceptance": map[string]any{"issuers": "https://kaname.kacho.local"},
		},
	}
}

// TestInjection_OwnPostureWithLiveForeignKnobsIsFound — отрицательный кейс.
//
// Ожидается ПЯТЬ находок: две половины приёма токенов плюс три ручки без
// читателя (адрес интроспекции, административный адрес, якорь его хопа) и
// адрес чужой службы личности — итого шесть. Число названо, а не «больше нуля»:
// «хоть одна находка» зеленело бы на судье, нашедшем один пункт из шести.
func TestInjection_OwnPostureWithLiveForeignKnobsIsFound(t *testing.T) {
	t.Parallel()
	got := judgeOwnPostureChain("own", "own", ownPostureChainWithForeignKnobsLive(), injectedProviderNames)
	if len(got) != 6 {
		t.Fatalf("находок %d, ожидалось 6:\n%s", len(got), strings.Join(got, "\n"))
	}
	joined := strings.Join(got, "\n")
	for _, must := range []string{
		"api-gateway.tokenAcceptance.issuers",
		"registry.tokenAcceptance.issuers",
		"api-gateway.hydra.introspectionUrl",
		"api-gateway.hydra.adminUrl",
		"api-gateway.hydra.adminCa.secretName",
		"api-gateway.kratosPublicUrl",
	} {
		if !strings.Contains(joined, must) {
			t.Errorf("находка не называет координату %s — оператору нечего править:\n%s", must, joined)
		}
	}
}

// TestInjection_OwnPostureClearedIsSilent — положительный близнец.
//
// Без него проба выше зеленеет на судье, который краснеет на ЛЮБОЙ накладке
// посадки `own`, и «ручки живы» становится неотличимо от «посадка `own`
// невозможна никогда».
func TestInjection_OwnPostureClearedIsSilent(t *testing.T) {
	t.Parallel()
	if got := judgeOwnPostureChain("own", "own", ownPostureChainCleared(), injectedProviderNames); len(got) != 0 {
		t.Fatalf("снятые ручки признаны находкой (%d):\n%s", len(got), strings.Join(got, "\n"))
	}
}

// TestInjection_ExternalPostureWithLiveForeignKnobsIsSilent — ВТОРАЯ сторона.
//
// Судья, краснеющий на живых ручках безотносительно посадки, снял бы чужого
// поставщика у шести цепочек, которые стоят на нём ПО РЕШЕНИЮ, и им стало бы
// нечем проверять человека. Меняется РОВНО ОДИН факт против отрицательного
// кейса — объявленная посадка.
func TestInjection_ExternalPostureWithLiveForeignKnobsIsSilent(t *testing.T) {
	t.Parallel()
	chain := ownPostureChainWithForeignKnobsLive()
	chain["api-gateway"].(map[string]any)["authn"] = map[string]any{"identityProvider": "external"}
	if got := judgeOwnPostureChain("prod", "external", chain, injectedProviderNames); len(got) != 0 {
		t.Fatalf("посадка `external` с живыми ручками поставщика признана находкой (%d):\n%s",
			len(got), strings.Join(got, "\n"))
	}
}

// TestInjection_ForeignIssuerRecognizerDiscriminates — САМОПРОВЕРКА
// распознавателя чужого издателя.
//
// Поиск подстроки по перечню целиком отвечал бы «да» и на перечне, где чужого
// издателя нет. Здесь предъявлены обе стороны: адрес, называющий компонент
// поставщика отдельным сегментом, и адрес, у которого имя компонента —
// часть другого слова.
func TestInjection_ForeignIssuerRecognizerDiscriminates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		list string
		want int
	}{
		{"https://kaname.kacho.local", 0},
		{"https://kaname.kacho.local,https://hydra.api.kacho.cloud", 1},
		{"https://kaname.kacho.local,https://localhost:28080/.ory/hydra/public", 1},
		{"http://localhost:28080/.ory/hydra/public/", 1},
		{"https://hydrargyrum.kacho.local", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := namesAForeignIssuer(c.list, injectedProviderNames); len(got) != c.want {
			t.Errorf("перечень %q: найдено чужих записей %d (%v), ожидалось %d",
				c.list, len(got), got, c.want)
		}
	}
}
