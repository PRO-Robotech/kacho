// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trusted_proxy_peers_test.go — ЗВЕНЬЯ ФРОНТА ПОИМЁННО ОБЪЯВЛЯЮТСЯ ВМЕСТЕ С
// КРУГОМ (kacho#3028, круг 3).
//
// Ручка KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_PEERS — имена безголовых служб,
// чьи поды край признаёт звеньями фронта. Сеть круга без имён — доверие всей
// сети подов, а имена без сети — доверие, которому нечем исполниться; обе
// половины объявляются вместе либо не объявляются вовсе. Утверждения парами:
// отказ называет ручку и запись, близнец отличен в один факт.
package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

func trusting(cidrs, peers string) config.Config {
	return config.Config{AuthZTrustedXForwardedFor: true, AuthZTrustedProxyCount: 1,
		AuthZTrustedProxyCIDRs: cidrs, AuthZTrustedProxyPeers: peers, AuthZTrustedProxyPeersRefresh: 5 * time.Second}
}

func TestTrustedProxyPeers_ReadsTheDeclaredNamesWithTheirCircle(t *testing.T) {
	got, err := trusting("10.244.0.0/16", " api-gateway-front-console ,api-gateway-front-ingress").TrustedProxyPeers()
	if err != nil {
		t.Fatalf("законная пара «сеть + звенья» отвергнута: %v", err)
	}
	if strings.Join(got, ",") != "api-gateway-front-console,api-gateway-front-ingress" {
		t.Fatalf("звенья %v", got)
	}
}

func TestTrustedProxyPeers_CircleWithoutNamedLinksIsRefused(t *testing.T) {
	_, err := trusting("10.244.0.0/16", "").TrustedProxyPeers()
	if err == nil {
		t.Fatal("сеть круга без звеньев поимённо принята — заголовку доверяла бы вся сеть подов")
	}
	if !strings.Contains(err.Error(), config.TrustedProxyPeersKnob) {
		t.Fatalf("отказ не называет ручку %s: %v", config.TrustedProxyPeersKnob, err)
	}
}

func TestTrustedProxyPeers_NamedLinksWithoutACircleAreRefused(t *testing.T) {
	_, err := trusting("", "api-gateway-front-console").TrustedProxyPeers()
	if err == nil {
		t.Fatal("звенья поимённо без сети круга приняты — объявленное доверие не исполнилось бы ни разу")
	}
	if !strings.Contains(err.Error(), config.TrustedProxyCIDRsKnob) {
		t.Fatalf("отказ не называет ручку %s: %v", config.TrustedProxyCIDRsKnob, err)
	}
}

func TestTrustedProxyPeers_AnAddressInsteadOfANameIsRefused(t *testing.T) {
	_, err := trusting("10.244.0.0/16", "api-gateway-front-console,10.244.1.17").TrustedProxyPeers()
	if err == nil {
		t.Fatal("адрес вместо имени принят — звеном стал бы любой под по этому адресу")
	}
	for _, want := range []string{config.TrustedProxyPeersKnob, "10.244.1.17"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}
}

func TestTrustedProxyPeers_ZeroRefreshIsRefused(t *testing.T) {
	c := trusting("10.244.0.0/16", "api-gateway-front-console")
	c.AuthZTrustedProxyPeersRefresh = 0
	if _, err := c.TrustedProxyPeers(); err == nil || !strings.Contains(err.Error(), "REFRESH") {
		t.Fatalf("нулевой период обновления звеньев принят (%v) — перечень не обновлялся бы никогда", err)
	}
}

// Близнецы «никому»: ни сети, ни имён — законно; доверие выключено — пара не
// судится, но запись разбирается.
func TestTrustedProxyPeers_NeitherHalfIsLawful(t *testing.T) {
	if got, err := trusting("", "").TrustedProxyPeers(); err != nil || len(got) != 0 {
		t.Fatalf("ни сети, ни звеньев: %v, %v; ожидалось «никому» без отказа", got, err)
	}
	off := trusting("10.244.0.0/16", "")
	off.AuthZTrustedXForwardedFor = false
	if _, err := off.TrustedProxyPeers(); err != nil {
		t.Fatalf("доверие выключено, пара не судится: %v", err)
	}
	off.AuthZTrustedProxyPeers = "10.244.1.17"
	if _, err := off.TrustedProxyPeers(); err == nil {
		t.Fatal("доверие выключено, а негодная запись принята — ошибка настройки при любом флаге")
	}
}
