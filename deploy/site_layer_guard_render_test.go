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
//
// Издатель платформы (kacho#3113) — то же происхождение, что консоль, во всех
// семи местах: чеканка службы доступа и приём края и реестра. Звено
// балансировки (kacho#3115): профиль площадки включает приём заголовка PROXY,
// и слой площадки обязан его назвать. Адрес — из диапазона для замеров
// (RFC 2544), не документации и не чей-то.
const probeSiteLayer = probeSiteLayerNoBalancer + probeBalancerLayer

// Слой площадки без звена балансировки: приём заголовка PROXY включён
// профилем, а круг доверия остаётся заглушкой.
const probeSiteLayerNoBalancer = probeSiteLayerNoIssuer + probeIssuerLayer

const probeBalancerLayer = `uif:
  publicFront:
    clientAddress:
      proxyProtocol:
        trustedFrom: ["198.18.0.10/32"]
`

// Слой площадки, написанный до kacho#3113: координаты почты и консоли есть,
// издателя нет — он остаётся заглушкой профиля.
const probeSiteLayerNoIssuer = `global:
  kacho:
    identity:
      smtp:
        connectionURI: "smtps://sender%40mail.kacho.test@relay.kacho.test:465/"
        fromAddress: "sender@mail.kacho.test"
      appBaseURL: "https://console.kacho.test"
`

const probeIssuer = "https://console.kacho.test"

const probeKeySet = probeIssuer + "=https://kaname-internal.kacho.svc:9097/.well-known/kaname/jwks.json"

const probeIssuerLayer = `kaname:
  config:
    authn:
      tokenSigning:
        issuer: "` + probeIssuer + `"
api-gateway:
  tokenAcceptance:
    issuers: "` + probeIssuer + `"
    issuerKeySets: "` + probeKeySet + `"
    platformIssuer: "` + probeIssuer + `"
registry:
  tokenAcceptance:
    issuers: "` + probeIssuer + `"
    issuerKeySets: "` + probeKeySet + `"
    platformIssuer: "` + probeIssuer + `"
`

// Слой площадки, заменивший издателя везде, кроме издателя приёма реестра: там —
// другое (не заглушка) происхождение. Копии разошлись. Слой целиком выписан
// заново: второй ключ `registry:` в одном файле заменил бы первый ЦЕЛИКОМ, и
// проба судила бы не то расхождение.
const probeSiteLayerRegistryOnAnotherIssuer = probeSiteLayerNoIssuer + `kaname:
  config:
    authn:
      tokenSigning:
        issuer: "` + probeIssuer + `"
api-gateway:
  tokenAcceptance:
    issuers: "` + probeIssuer + `"
    issuerKeySets: "` + probeKeySet + `"
    platformIssuer: "` + probeIssuer + `"
registry:
  tokenAcceptance:
    issuers: "` + probeIssuer + `"
    issuerKeySets: "` + probeKeySet + `"
    platformIssuer: "https://other.kacho.test"
`

// proxyProtocolOn — включённый приём заголовка PROXY у входа консоли.
const proxyProtocolOn = "uif.publicFront.clientAddress.proxyProtocol.enabled=true"

const (
	proxyProtocolOffKnob   = "uif.publicFront.clientAddress.proxyProtocol.enabled=false"
	proxyProtocolOffReason = "uif.publicFront.clientAddress.proxyProtocol.disabledBecause=проба выключенной формы"
)

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
			want: []string{refusal, "smtp.connectionURI", "smtp.fromAddress", "appBaseURL", "tokenSigning.issuer",
				"proxyProtocol.trustedFrom"}},
		{name: "законный близнец: тот же стенд со слоем площадки", stack: "a8f60d",
			site: probeSiteLayer, sets: []string{siteLayerEnforced}},
		{name: "слой площадки не заменил издателя платформы (kacho#3113)", stack: "a8f60d",
			site: probeSiteLayerNoIssuer, sets: []string{siteLayerEnforced},
			want: []string{refusal, "kaname.config.authn.tokenSigning.issuer", "api-gateway.tokenAcceptance.platformIssuer",
				"api-gateway.tokenAcceptance.issuerKeySets", "registry.tokenAcceptance.issuers"},
			notWant: []string{"appBaseURL", "smtp.fromAddress"}},
		{name: "слой площадки развёл копии издателя: приём реестра на другом издателе", stack: "a8f60d",
			site: probeSiteLayerRegistryOnAnotherIssuer, sets: []string{siteLayerEnforced},
			want:    []string{refusal, "registry.tokenAcceptance.platformIssuer", "не называет издателя чеканки"},
			notWant: []string{"api-gateway.tokenAcceptance", "registry.tokenAcceptance.issuers"}},
		{name: "слой площадки заменил только узел", stack: "a8f60d",
			site: probeSiteLayerMailOnly, sets: []string{siteLayerEnforced},
			want: []string{refusal, "smtp.fromAddress", "appBaseURL", "tokenSigning.issuer"}, notWant: []string{"smtp.connectionURI"}},
		{name: "боевой профиль применяется без слоя площадки", stack: "prod",
			sets: []string{siteLayerEnforced},
			want: []string{refusal, "smtp.connectionURI", "smtp.fromAddress"}},
		{name: "законный близнец: стенд разработки без заглушек", stack: "dev",
			sets: []string{siteLayerEnforced}},
		{name: "приём заголовка PROXY включён, слой площадки не заменил звено балансировки", stack: "a8f60d",
			site: probeSiteLayerNoBalancer, sets: []string{siteLayerEnforced},
			want: []string{refusal, "proxyProtocol.trustedFrom"}, notWant: []string{"appBaseURL"}},
		{name: "законный близнец: приём выключен с причиной, звено балансировки не названо", stack: "a8f60d",
			site: probeSiteLayerNoBalancer, sets: []string{siteLayerEnforced, proxyProtocolOffKnob, proxyProtocolOffReason}},
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
