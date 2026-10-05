// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trusted_proxy_link_sans_test.go — ИМЕНА ЗВЕНЬЕВ В СЕРТИФИКАТЕ ОБЪЯВЛЯЮТСЯ
// ВМЕСТЕ С КРУГОМ И С МЕХАНИЗМОМ ИХ ПРОВЕРКИ (kacho#3028, C4).
//
// Адрес пода — не личность звена: под с теми же метками попадает в службу
// фронта, а адрес ушедшего пода выдаётся другому. Звено узнаётся по имени в
// клиентском сертификате, проверенном якорем установки на внешнем
// TLS-слушателе. Имена без слушателя, проверяющего сертификат, — доверие,
// которому нечем исполниться; круг без имён — доверие адресу. Утверждения
// парами: отказ называет ручку, близнец отличен в один факт.
package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

const linkSAN = "spiffe://kacho.test/ns/kacho/sa/console-front"

// linked — край за звеном: круг, звенья поимённо, имена и слушатель, который
// проверяет клиентский сертификат якорем.
func linked(sans string) config.Config {
	c := trusting("10.244.0.0/16", "api-gateway-front-console")
	c.AuthZTrustedProxySANs = sans
	c.TLSListenAddr, c.TLSCertFile, c.TLSKeyFile = ":8443", "/tls/tls.crt", "/tls/tls.key"
	c.HybridMTLSExternal, c.MTLSCAFile = true, "/mtls/ca.crt"
	return c
}

func TestTrustedProxyLinkSANs_ReadsTheDeclaredNames(t *testing.T) {
	got, err := linked(linkSAN).TrustedProxyLinkSANs()
	if err != nil || strings.Join(got, ",") != linkSAN {
		t.Fatalf("законное объявление: %v, %v", got, err)
	}
}

func TestTrustedProxyLinkSANs_CircleWithoutNamesIsRefused(t *testing.T) {
	_, err := linked("").TrustedProxyLinkSANs()
	if err == nil || !strings.Contains(err.Error(), config.TrustedProxySANsKnob) {
		t.Fatalf("круг без имён звеньев принят (доверие адресу) либо отказ не называет ручку: %v", err)
	}
}

func TestTrustedProxyLinkSANs_NamesWithoutACircleAreRefused(t *testing.T) {
	c := linked(linkSAN)
	c.AuthZTrustedProxyCIDRs, c.AuthZTrustedProxyPeers = "", ""
	if _, err := c.TrustedProxyLinkSANs(); err == nil || !strings.Contains(err.Error(), config.TrustedProxyCIDRsKnob) {
		t.Fatalf("имена без круга приняты либо отказ не называет ручку круга: %v", err)
	}
}

// Механизм проверки: внешний TLS-слушатель, необязательный клиентский
// сертификат, якорь. Без любой из трёх имя звена не предъявить.
func TestTrustedProxyLinkSANs_NamesWithoutAVerifyingListenerAreRefused(t *testing.T) {
	for name, mutate := range map[string]func(*config.Config){
		"нет TLS-слушателя":              func(c *config.Config) { c.TLSListenAddr = "" },
		"слушатель не просит сертификат": func(c *config.Config) { c.HybridMTLSExternal = false },
		"нет якоря":                      func(c *config.Config) { c.MTLSCAFile = "" },
	} {
		t.Run(name, func(t *testing.T) {
			c := linked(linkSAN)
			mutate(&c)
			if _, err := c.TrustedProxyLinkSANs(); err == nil || !strings.Contains(err.Error(), config.TrustedProxySANsKnob) {
				t.Fatalf("имена звеньев без проверяющего слушателя приняты: %v", err)
			}
		})
	}
}

func TestTrustedProxyLinkSANs_MalformedNameIsRefusedAtAnyFlag(t *testing.T) {
	c := linked("*")
	c.AuthZTrustedXForwardedFor = false
	if _, err := c.TrustedProxyLinkSANs(); err == nil || !strings.Contains(err.Error(), config.TrustedProxySANsKnob) {
		t.Fatalf("подстановочный знак принят при выключенном доверии: %v", err)
	}
}

// Близнец «никому»: ни круга, ни звеньев, ни имён — законно.
func TestTrustedProxyLinkSANs_NothingDeclaredIsLawful(t *testing.T) {
	if got, err := trusting("", "").TrustedProxyLinkSANs(); err != nil || len(got) != 0 {
		t.Fatalf("ничего не объявлено: %v, %v", got, err)
	}
}

// Боевой профиль: метки разработки терпят послабление, прочие — нет.
func TestProductionPosture(t *testing.T) {
	for env, want := range map[string]bool{"": true, "production": true, "staging": true, "dev": false, "local": false, " TEST ": false} {
		if got := (config.Config{AppEnv: env}).ProductionPosture(); got != want {
			t.Errorf("AppEnv=%q: ProductionPosture()=%v, ожидалось %v", env, got, want)
		}
	}
}
