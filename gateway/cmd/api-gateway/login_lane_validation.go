// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// login_lane_validation.go — страж старта адреса ПОЛОСЫ ФОРМЫ службы доступа
// (приёмка Ф3 Р2, Р16, Ф3-45).
//
// Под посадкой `own` край ретранслирует глаголы формы на слушатель
// службы. Ретранслятор без цели — контроль, отказывающий на каждом запросе всю
// свою жизнь по одной и той же причине, и снаружи это неотличимо от «служба
// лежит»; поэтому пустой адрес под `own` — отказ старта с именем ручки, а не
// умолчание. Под `external` полосы формы нет, и ручка не читается.
//
// Адрес судится теми же правилами, что хопы к нашему авторитету отзыва: по
// ретрансляции едет носитель сессии человека, и незашифрованный адрес нёс бы его
// в открытом виде; слушатель взаимный по TLS (Р16), и клиентская пара с якорем
// обязательны вместе с адресом — половина пары хуже отсутствия обеих, потому что
// выглядит настроенной.
package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/corelib/identityposture"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Имена ручек клиентской пары — те же, что у gRPC-рёбер: одно удостоверение
// края на все хопы.
const (
	mtlsClientCertKnob = "KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE"
	mtlsClientKeyKnob  = "KACHO_API_GATEWAY_MTLS_CLIENT_KEY_FILE"
	mtlsCAKnob         = "KACHO_API_GATEWAY_MTLS_CA_FILE"
)

// LoginLaneConfig — срез настройки, который читает страж.
type LoginLaneConfig struct {
	URL            string
	ClientCertFile string
	ClientKeyFile  string
	CAFile         string
}

// validateLoginLaneConfig отказывает в старте под `own` без годной полосы формы.
//
// Метка окружения здесь НЕ читается намеренно: ретрансляция без цели негодна на
// любом стенде, а dev-посадка на поднятом стенде запрещена (ban #16).
func validateLoginLaneConfig(provider identityposture.Provider, cfg LoginLaneConfig) error {
	if provider != identityposture.Own {
		return nil
	}
	raw := strings.TrimSpace(cfg.URL)
	if raw == "" {
		return fmt.Errorf("%s is empty — on posture %s=own the edge relays sign-in, sign-out, "+
			"password change and the form token to the identity service's login lane, and a relay "+
			"with no target would answer 503 to every request for its whole life, indistinguishable "+
			"from a service outage (refuse to start)",
			config.LoginLaneURLKnob, config.IdentityProviderKnob)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s=%q is not an absolute URL with scheme and host (refuse to start)",
			config.LoginLaneURLKnob, raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s=%q is not https — the relayed request carries the person's session "+
			"cookie, and a plaintext hop would carry it in the clear; the login lane is a mutual-TLS "+
			"listener (refuse to start)", config.LoginLaneURLKnob, raw)
	}
	cert, key, ca := strings.TrimSpace(cfg.ClientCertFile), strings.TrimSpace(cfg.ClientKeyFile), strings.TrimSpace(cfg.CAFile)
	switch {
	case cert == "" && key == "":
		return fmt.Errorf("%s and %s are both empty — the login lane listener asks the caller for a "+
			"client certificate and admits exactly the edge by it; a hop with nothing to present is "+
			"refused on every handshake; declare the pair together with %s (refuse to start)",
			mtlsClientCertKnob, mtlsClientKeyKnob, config.LoginLaneURLKnob)
	case key == "":
		return fmt.Errorf("%s is set without %s — half a pair presents nothing (refuse to start)",
			mtlsClientCertKnob, mtlsClientKeyKnob)
	case cert == "":
		return fmt.Errorf("%s is set without %s — a key with no certificate has nothing to present (refuse to start)",
			mtlsClientKeyKnob, mtlsClientCertKnob)
	}
	if ca == "" {
		return fmt.Errorf("%s is empty — the login lane listener's certificate is issued by the internal CA "+
			"and this process trusts the system roots, so every handshake would fail with an unknown "+
			"authority; pin the bundle together with %s (refuse to start)", mtlsCAKnob, config.LoginLaneURLKnob)
	}
	return nil
}
