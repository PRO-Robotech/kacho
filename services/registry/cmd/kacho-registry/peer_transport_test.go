// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// peer_transport_test.go — поведенческий замок на транспорт ИСХОДЯЩИХ рёбер
// registry.
//
// Класс, который эти пробы закрывают: ручка транспорта у ребра есть, проводка её
// читает, а отказа в старте при невзведённой ручке нет. Клиентские creds на
// Enable=false вырождаются в insecure БЕЗ ошибки
// (pkg/grpcclient.TLSClientTransportCreds), поэтому процесс поднимается, печатает
// «registry→iam edges wired» с честным `authz_mtls=false` — и каждый Check уходит
// по открытому каналу. Контроль присутствует и не отказывает ни разу за свою
// жизнь.
//
// Отрицания идут ПАРАМИ с положительным контролем: «отвергнуто» без «законное
// проходит» неотличимо от стража, который отвергает всё.

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
)

// armedProdCfg — боевая посадка, у которой все три исходящих ребра и подняты
// (непустой адрес), и защищены. Каждый negative-кейс ослабляет ровно одно
// измерение.
func armedProdCfg(mode string) config.Config {
	return config.Config{
		AuthMode:                  mode,
		DBSSLMode:                 "require",
		AuthZIAMGRPCAddr:          "kaname-internal.kacho.svc:9091",
		IAMProjectGRPCAddr:        "kaname.kacho.svc:9090",
		GeoGRPCAddr:               "kacho-geo.kacho.svc:9090",
		PublicServerMTLS:          grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS:        grpcsrv.TLSServer{Enable: true},
		IAMAuthzMTLS:              grpcclient.TLSClient{Enable: true},
		IAMProjectMTLS:            grpcclient.TLSClient{Enable: true},
		GeoMTLS:                   grpcclient.TLSClient{Enable: true},
		AuthZTrustedForwarderSANs: []string{"spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"},
		// Домен доверия — величина установки, и конструктор дескриптора требует её
		// названной: процесс, не назвавший домена, своим не признаёт никого.
		AuthZTrustDomain: "kacho.cloud",
	}
}

// Положительный контроль: полностью взведённая боевая посадка стартует. Без него
// любое отрицание ниже зеленело бы и на страже, отвергающем всё.
func TestValidateSecurityConfig_ProductionAcceptsArmedPeerEdges(t *testing.T) {
	for _, mode := range []string{"production", "production-strict"} {
		if err := validateSecurityConfig(armedProdCfg(mode)); err != nil {
			t.Fatalf("%s with every peer edge armed must boot, got refusal: %v", mode, err)
		}
	}
}

// Ребро, по которому принимается решение о доступе (per-RPC Check на обоих
// листенерах + регистрация владельца через fga-proxy), обязано нести проверяемый
// транспорт. Отказ обязан называть ручку — его читает оператор, поднимающий стенд.
//
// К3 п.2: отказ и в PLAIN production, не только в строгом режиме — боевой стенд
// по умолчанию идёт именно в plain.
func TestValidateSecurityConfig_RefusesUnverifiedAuthzEdge(t *testing.T) {
	for _, mode := range []string{"production", "production-strict"} {
		t.Run(mode, func(t *testing.T) {
			cfg := armedProdCfg(mode)
			cfg.IAMAuthzMTLS.Enable = false
			err := validateSecurityConfig(cfg)
			if err == nil {
				t.Fatalf("%s with the registry→iam authz edge dialled over cleartext must refuse to start, got nil", mode)
			}
			if want := "KACHO_REGISTRY_IAM_AUTHZ_MTLS_ENABLE"; !strings.Contains(err.Error(), want) {
				t.Errorf("refusal must name the knob %q; got: %v", want, err)
			}
		})
	}
}

// Ребро registry→iam public несёт резолв области (существование проекта и поиск
// аккаунта на Create). Открытый канал здесь означает подменённое существование
// чужого проекта — вход в решение о доступе, а не косметика.
func TestValidateSecurityConfig_RefusesUnverifiedProjectEdge(t *testing.T) {
	cfg := armedProdCfg("production")
	cfg.IAMProjectMTLS.Enable = false
	err := validateSecurityConfig(cfg)
	if err == nil {
		t.Fatal("production with the registry→iam project edge dialled over cleartext must refuse to start, got nil")
	}
	if want := "KACHO_REGISTRY_IAM_PROJECT_MTLS_ENABLE"; !strings.Contains(err.Error(), want) {
		t.Errorf("refusal must name the knob %q; got: %v", want, err)
	}
}

// Ребро registry→geo несёт валидацию размещения реестра (регион). Тот же класс.
func TestValidateSecurityConfig_RefusesUnverifiedGeoEdge(t *testing.T) {
	cfg := armedProdCfg("production")
	cfg.GeoMTLS.Enable = false
	err := validateSecurityConfig(cfg)
	if err == nil {
		t.Fatal("production with the registry→geo edge dialled over cleartext must refuse to start, got nil")
	}
	if want := "KACHO_REGISTRY_GEO_MTLS_ENABLE"; !strings.Contains(err.Error(), want) {
		t.Errorf("refusal must name the knob %q; got: %v", want, err)
	}
}

// «Страж видит ребро ⟺ ребро дилится»: незаданный адрес означает, что
// композиционный корень соединение не поднимает вовсе (`if cfg.<Addr> != ""`), —
// требовать на нём транспорт не о чем. Без этой пробы страж превратился бы в
// требование задать ручку ребра, которого нет.
func TestValidateSecurityConfig_IgnoresPeerEdgeThatIsNotDialled(t *testing.T) {
	cfg := armedProdCfg("production")
	cfg.GeoGRPCAddr = ""
	cfg.GeoMTLS.Enable = false
	if err := validateSecurityConfig(cfg); err != nil {
		t.Fatalf("an edge with no address is never dialled; the guard must not demand its transport, got: %v", err)
	}
}

// dev осознанно терпит невзведённый транспорт — только для локальных фикстур. На
// РАЗВЁРНУТОМ стенде dev-посадка запрещена отдельным правилом (security.md
// §«Production-mode обязателен ВЕЗДЕ»), поэтому послабление не расширяет
// поверхность стенда.
func TestValidateSecurityConfig_DevToleratesUnverifiedPeerEdges(t *testing.T) {
	cfg := armedProdCfg("dev")
	cfg.IAMAuthzMTLS.Enable = false
	cfg.IAMProjectMTLS.Enable = false
	cfg.GeoMTLS.Enable = false
	if err := validateSecurityConfig(cfg); err != nil {
		t.Fatalf("dev mode must tolerate unverified peer edges (local fixtures), got: %v", err)
	}
}
