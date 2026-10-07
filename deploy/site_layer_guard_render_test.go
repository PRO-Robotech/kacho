// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// СТРАЖ СЛОЯ ПЛОЩАДКИ: профиль с заглушками не применяется к кластеру (kacho#3040).
//
// Координаты площадки (узел почтовой полосы, адрес отправителя, внешний origin
// консоли) в публичном профиле стоят ЗАГЛУШКАМИ, а настоящие приходят слоем
// площадки вне git. Рендер для проверок в CI слоя площадки не имеет by
// construction и обязан проходить на заглушках. Применение к кластеру — нет:
// стенд с заглушками поднимается, поды готовы, а письма уходят на
// неразрешимый узел и ссылки в них ведут в никуда, молча.
//
// Различитель — ручка `global.kacho.siteLayer.enforced`: её взводят рецепты
// применения (`stack-up`, cutover-fe3455.sh) и только они. Взведённая ручка при
// любой оставшейся заглушке — отказ рендера, называющий каждую величину.
package deploy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const siteLayerEnforced = siteLayerEnforcedSet

func renderWithSiteLayer(t *testing.T, chain []string, site string, sets ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-гейт обязан исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-гейт пропущен")
	}
	args := []string{"template", "kacho-umbrella", umbrellaDir, "-n", "kacho"}
	for _, p := range chain {
		args = append(args, "-f", filepath.Join(umbrellaDir, p))
	}
	if site != "" {
		f := filepath.Join(t.TempDir(), "values.probe-secrets.yaml")
		if err := os.WriteFile(f, []byte(site), 0o600); err != nil {
			t.Fatalf("слой площадки пробы не записан: %v", err)
		}
		args = append(args, "-f", f)
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	return string(out), err
}

// Слой площадки пробы — НЕ заглушки (иначе близнец ничего не доказал бы) и не
// чьи-то координаты: домен для проб (RFC 6761), и происхождение консоли — по
// https: профиль стенда завершает TLS на крае и http-происхождения не примет.
const probeSiteLayer = `global:
  kacho:
    identity:
      smtp:
        connectionURI: "smtps://sender%40mail.kacho.test@relay.kacho.test:465/"
        fromAddress: "sender@mail.kacho.test"
      appBaseURL: "https://console.kacho.test"
`

const probeSiteLayerMailOnly = `global:
  kacho:
    identity:
      smtp:
        connectionURI: "smtps://sender%40mail.kacho.test@relay.kacho.test:465/"
`

func TestSiteLayerGuardRefusesAnAppliedStandStillCarryingPlaceholders(t *testing.T) {
	stacks := deployStacks(t)
	const refusal = "слой площадки"
	cases := []struct {
		name, stack, site string
		sets              []string
		want, notWant     []string // want пуст — рендер обязан пройти
	}{
		{name: "законный близнец: рендер проверок без слоя площадки", stack: "a8f60d"},
		{name: "управляемый стенд применяется без слоя площадки", stack: "a8f60d",
			sets: []string{siteLayerEnforced},
			want: []string{refusal, "smtp.connectionURI", "smtp.fromAddress", "appBaseURL"}},
		{name: "законный близнец: тот же стенд со слоем площадки", stack: "a8f60d",
			site: probeSiteLayer, sets: []string{siteLayerEnforced}},
		{name: "слой площадки заменил только узел", stack: "a8f60d",
			site: probeSiteLayerMailOnly, sets: []string{siteLayerEnforced},
			want: []string{refusal, "smtp.fromAddress", "appBaseURL"}, notWant: []string{"smtp.connectionURI"}},
		{name: "боевой профиль применяется без слоя площадки", stack: "prod",
			sets: []string{siteLayerEnforced},
			want: []string{refusal, "smtp.connectionURI", "smtp.fromAddress"}},
		{name: "законный близнец: стенд разработки без заглушек", stack: "dev",
			sets: []string{siteLayerEnforced}},
	}
	for _, c := range cases {
		chain, ok := stacks[c.stack]
		if !ok {
			t.Fatalf("%s: стека %q в таблице нет", c.name, c.stack)
		}
		out, err := renderWithSiteLayer(t, chain, c.site, c.sets...)
		switch {
		case len(c.want) == 0 && err != nil:
			t.Errorf("%s: законный близнец отвергнут рендером: %v\n%s", c.name, err, lastLines(out, 5))
		case len(c.want) != 0 && err == nil:
			t.Errorf("%s: рендер прошёл, а страж обязан был отказать (ждали %q)", c.name, c.want)
		case len(c.want) != 0:
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("%s: рендер отказал, но текст не называет %q:\n%s", c.name, w, lastLines(out, 5))
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(out, w) {
					t.Errorf("%s: отказ называет %q, которую слой площадки заменил:\n%s", c.name, w, lastLines(out, 5))
				}
			}
		}
		if !t.Failed() {
			t.Logf("%s: исход как ждали", c.name)
		}
	}
}
