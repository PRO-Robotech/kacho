// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_subchart_follows_kaname_chart_test.go — подчарт службы личности в зонте
// следует отдельному чарту службы там, где различие никем не решено (kacho#3181).
//
// Отдельный чарт службы (репозиторий kaname) — авторитет величин своей службы:
// его INSTALL говорит, что умолчания у срока ключа подписи нет ни у процесса, ни
// в базовых значениях чарта, а боевой профиль ставит потолок ключей доступа 3.
// Подчарт зонта нёс срок 2160h умолчанием и потолок 8 — без записи о решении.
//
// Проба читает объявления базовых значений подчарта, а не рендер: величина,
// подставленная умолчанием, рендерится так же гладко, как объявленная профилем.
// Значение отдельного чарта выписано константой с координатой: репозиторий
// службы в дерево этого продукта не входит, и читать его здесь нечем.
package deploy_test

import (
	"fmt"
	"testing"
)

// kanameChartAccessKeysPerUser — потолок ключей доступа человека в боевом
// профиле отдельного чарта службы (kaname deploy/values.prod.yaml,
// `ownCeilings.accessKeysPerUser`). Потолок сводится к меньшему: величина —
// предел, а поднимать предел без основания нельзя.
const kanameChartAccessKeysPerUser = 3

// judgeKanameSubchartDefaults — находки по базовым значениям подчарта.
func judgeKanameSubchartDefaults(defaults map[string]any) []string {
	var out []string
	if v := asString(lookupAny(defaults, "config", "authn", "tokenSigning", "keyLifetime")); v != "" {
		out = append(out, fmt.Sprintf("базовые значения подчарта подставляют срок ключа подписи (%q): "+
			"умолчания у срока нет ни у процесса, ни в базовых значениях чарта службы — срок "+
			"объявляет профиль, иначе он выбран за оператора", v))
	}
	got := fmt.Sprint(lookupAny(defaults, "config", "ownCeilings", "accessKeysPerUser"))
	if got != fmt.Sprint(kanameChartAccessKeysPerUser) {
		out = append(out, fmt.Sprintf("потолок ключей доступа подчарта %s, а у боевого профиля "+
			"отдельного чарта службы %d — различие никем не решено", got, kanameChartAccessKeysPerUser))
	}
	return out
}

func TestKanameSubchartDefaultsFollowTheKanameChart(t *testing.T) {
	defaults := readSubchartDefaults(t, kanameChartValues)
	findings := judgeKanameSubchartDefaults(defaults)
	t.Logf("перепись: прочитан %s · величин сверено 2 · находок %d", kanameChartValues, len(findings))
	for _, f := range findings {
		t.Error(f)
	}
}

func TestKanameSubchartDefaultsInjection_SilentKeyLifetimeIsFound(t *testing.T) {
	d := map[string]any{"config": map[string]any{
		"authn":       map[string]any{"tokenSigning": map[string]any{"keyLifetime": "2160h"}},
		"ownCeilings": map[string]any{"accessKeysPerUser": 3},
	}}
	if got := judgeKanameSubchartDefaults(d); len(got) != 1 {
		t.Fatalf("подставленный срок не найден: %v", got)
	}
}

func TestKanameSubchartDefaultsInjection_WiderCeilingIsFound(t *testing.T) {
	d := map[string]any{"config": map[string]any{
		"authn":       map[string]any{"tokenSigning": map[string]any{"keyLifetime": ""}},
		"ownCeilings": map[string]any{"accessKeysPerUser": 8},
	}}
	if got := judgeKanameSubchartDefaults(d); len(got) != 1 {
		t.Fatalf("расходящийся потолок не найден: %v", got)
	}
}

func TestKanameSubchartDefaultsInjection_FollowingDefaultsAreSilent(t *testing.T) {
	d := map[string]any{"config": map[string]any{
		"authn":       map[string]any{"tokenSigning": map[string]any{"keyLifetime": ""}},
		"ownCeilings": map[string]any{"accessKeysPerUser": 3},
	}}
	if got := judgeKanameSubchartDefaults(d); len(got) != 0 {
		t.Fatalf("законный близнец назван находкой: %v", got)
	}
}
