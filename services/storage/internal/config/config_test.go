// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

// secureProd — базовый production-Config, проходящий Validate(): используется как
// отправная точка, из которой каждый negative-кейс ослабляет ровно одно измерение.
func secureProd() config.Config {
	return config.Config{
		AuthMode:           "production",
		DBSSLMode:          "require",
		AuthZIAMGRPCAddr:   "kacho-iam-internal:9091",
		GeoClientMTLS:      grpcclient.TLSClient{Enable: true},
		IAMClientMTLS:      grpcclient.TLSClient{Enable: true},
		PublicServerMTLS:   grpcsrv.TLSServer{Enable: true},
		InternalServerMTLS: grpcsrv.TLSServer{Enable: true},
	}
}

// TestLoad_defaultAuthModeProduction — secure-by-default: без явного
// KACHO_STORAGE_AUTH_MODE бинарь резолвится в production (fail-closed), не dev.
// dev — явный opt-in (dev-профиль deploy-стенда выставляет его через env). Зеркалит
// iam/vpc/geo/nlb posture (security.md «любой деплой — production-mode»).
func TestLoad_defaultAuthModeProduction(t *testing.T) {
	var c config.Config
	if err := config.LoadInto(&c, map[string]string{
		"KACHO_STORAGE_DB_PASSWORD": "secret",
	}); err != nil {
		t.Fatalf("LoadInto err = %v", err)
	}
	if c.AuthMode != "production" {
		t.Fatalf("default auth mode = %q, want production (fail-closed by default)", c.AuthMode)
	}
}

// TestValidate_devTolerant — dev-режим осознанно терпит insecure-дефолты (plaintext
// DB, mTLS off, authz off): локальные фикстуры и dev-стенд стартуют без kacho-iam.
// Validate НЕ отказывает старту в dev (WARN эмитит serve.go, не fatal).
func TestValidate_devTolerant(t *testing.T) {
	c := config.Config{
		AuthMode:  "dev",
		DBSSLMode: "disable",
		// mTLS off, authz addr empty — всё insecure, но dev это допускает.
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("dev mode must tolerate insecure config, got err = %v", err)
	}
}

// TestValidate_productionSecureOK — полностью secure production-Config проходит.
func TestValidate_productionSecureOK(t *testing.T) {
	if err := secureProd().Validate(); err != nil {
		t.Fatalf("secure production config must validate, got err = %v", err)
	}
}

// TestValidate_productionRefusesInsecure — КЛЮЧЕВОЙ behaviour-lock (#56): production
// с plaintext-DB + mTLS off + authz off ОБЯЗАН отказать старту (refuse-to-start).
// Ранее AuthMode был dead-code → storage boot'ился insecure с одним WARN (единственный
// сервис не fail-closed). Восстанавливает security.md «AuthN+AuthZ ВЕЗДЕ + production
// fail-closed».
func TestValidate_productionRefusesInsecure(t *testing.T) {
	c := config.Config{
		AuthMode:  "production",
		DBSSLMode: "disable",
		// mTLS off, authz addr empty.
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("production mode with plaintext DB + no mTLS + no authz must refuse to start, got nil")
	}
	// Наблюдаемое поведение: отказ упоминает все три insecure-измерения.
	msg := err.Error()
	for _, want := range []string{"SSLMODE", "mTLS", "AUTHZ_IAM_GRPC_ADDR"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message must mention %q; got: %s", want, msg)
		}
	}
}

// TestValidate_productionRequiresDBSSL — production с secure mTLS+authz, но
// plaintext DB (sslmode=disable) → refuse. Plaintext до БД в проде запрещён.
func TestValidate_productionRequiresDBSSL(t *testing.T) {
	c := secureProd()
	c.DBSSLMode = "disable"
	if err := c.Validate(); err == nil {
		t.Fatal("production mode with sslmode=disable must refuse to start")
	}
	c.DBSSLMode = ""
	if err := c.Validate(); err == nil {
		t.Fatal("production mode with empty sslmode (libpq→disable) must refuse to start")
	}
}

// TestValidate_productionRequiresMTLS — production с secure DB+authz, но mTLS off на
// любом из листенеров → refuse. mTLS обязателен на ОБОИХ (public :9090 + internal
// :9091 — internal НЕ освобождён, security.md).
func TestValidate_productionRequiresMTLS(t *testing.T) {
	c := secureProd()
	c.PublicServerMTLS.Enable = false
	if err := c.Validate(); err == nil {
		t.Fatal("production mode with public mTLS off must refuse to start")
	}
	c = secureProd()
	c.InternalServerMTLS.Enable = false
	if err := c.Validate(); err == nil {
		t.Fatal("production mode with internal mTLS off must refuse to start")
	}
}

// TestValidate_productionRequiresAuthz — production с secure DB+mTLS, но пустой
// authz-адрес → refuse. Без AuthZIAMGRPCAddr per-RPC Check не подключается (serve.go
// пропускает authz-интерсептор) → неавторизованные запросы. Fail-closed.
func TestValidate_productionRequiresAuthz(t *testing.T) {
	c := secureProd()
	c.AuthZIAMGRPCAddr = ""
	if err := c.Validate(); err == nil {
		t.Fatal("production mode with empty authz iam addr must refuse to start")
	}
}

// TestValidate_productionStrictRequiresStrictSSL — production-strict требует
// sslmode ∈ {require,verify-ca,verify-full}; disable/произвольное → refuse.
func TestValidate_productionStrictRequiresStrictSSL(t *testing.T) {
	c := secureProd()
	c.AuthMode = "production-strict"
	if err := c.Validate(); err != nil {
		t.Fatalf("production-strict with sslmode=require must validate, got err = %v", err)
	}
	c.DBSSLMode = "disable"
	if err := c.Validate(); err == nil {
		t.Fatal("production-strict with sslmode=disable must refuse to start")
	}
	c.DBSSLMode = "allow"
	if err := c.Validate(); err == nil {
		t.Fatal("production-strict with sslmode=allow must refuse to start")
	}
}

// TestValidate_unknownMode — незнакомый AuthMode → refuse (whitelist).
func TestValidate_unknownMode(t *testing.T) {
	c := secureProd()
	c.AuthMode = "prod" // typo
	if err := c.Validate(); err == nil {
		t.Fatal("unknown auth mode must refuse to start")
	}
}
