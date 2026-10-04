// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trusted_proxy_circle_test.go — КРУГ ДОВЕРЕННЫХ ЗВЕНЬЕВ АДРЕСА КЛИЕНТА
// ОБЪЯВЛЯЕТСЯ ЯВНО И РАЗБИРАЕТСЯ ДО СТАРТА (kacho#3028).
//
// Ручка KACHO_API_GATEWAY_AUTHZ_TRUSTED_PROXY_CIDRS — сети, из которых край
// принимает заголовки пересылки. Утверждения парами:
//
//	разбор     — список сетей (через запятую, пробелы допустимы) → префиксы;
//	отказ      — неразборная запись называет ручку и запись, а не молча
//	             выпадает из круга;
//	противоречие — доверие заголовкам включено, а круг пуст: «доверяю
//	             пересылке» без единого звена, которому доверено. Это отказ
//	             старта, а не тихое «никому»: иначе стенд за раздачей снова
//	             видит всех клиентов одним адресом, и никто об этом не узнаёт;
//	близнец    — доверие выключено (флагом или нулём прыжков): пустой круг
//	             законен.
package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

func TestTrustedProxyCircle_ParsesTheDeclaredNetworks(t *testing.T) {
	c := config.Config{AuthZTrustedXForwardedFor: true, AuthZTrustedProxyCount: 1,
		AuthZTrustedProxyCIDRs: " 10.244.0.0/16, fd00::/8 "}
	got, err := c.TrustedProxyCircle()
	if err != nil {
		t.Fatalf("законный круг отвергнут: %v", err)
	}
	if len(got) != 2 || got[0].String() != "10.244.0.0/16" || got[1].String() != "fd00::/8" {
		t.Fatalf("круг %v, ожидались 10.244.0.0/16 и fd00::/8", got)
	}
}

func TestTrustedProxyCircle_RefusesAnUnparsableEntry(t *testing.T) {
	c := config.Config{AuthZTrustedXForwardedFor: true, AuthZTrustedProxyCount: 1,
		AuthZTrustedProxyCIDRs: "10.244.0.0/16,10.0.0.300/8"}
	_, err := c.TrustedProxyCircle()
	if err == nil {
		t.Fatal("неразборная запись принята — она выпала бы из круга молча")
	}
	for _, want := range []string{config.TrustedProxyCIDRsKnob, "10.0.0.300/8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}
}

func TestTrustedProxyCircle_RefusesTrustWithAnEmptyCircle(t *testing.T) {
	c := config.Config{AuthZTrustedXForwardedFor: true, AuthZTrustedProxyCount: 1}
	if _, err := c.TrustedProxyCircle(); err == nil || !strings.Contains(err.Error(), config.TrustedProxyCIDRsKnob) {
		t.Fatalf("доверие заголовкам при пустом круге не отказано (или отказ без имени ручки): %v", err)
	}
}

func TestTrustedProxyCircle_EmptyCircleIsLawfulWhenTrustIsOff(t *testing.T) {
	for _, c := range []config.Config{
		{AuthZTrustedXForwardedFor: false, AuthZTrustedProxyCount: 1},
		{AuthZTrustedXForwardedFor: true, AuthZTrustedProxyCount: 0},
	} {
		got, err := c.TrustedProxyCircle()
		if err != nil || len(got) != 0 {
			t.Errorf("доверие выключено (%+v): круг %v, ошибка %v — ожидался пустой круг без отказа",
				c.AuthZTrustedXForwardedFor, got, err)
		}
	}
}
