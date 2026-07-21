// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strings"
)

// Validate — secure-by-default boot-guard: в production/production-strict операции
// без mTLS, per-RPC authz Check и с plaintext-DB ЗАПРЕЩЕНЫ (refuse-to-start). Раньше
// AuthMode был dead-code (объявлен, никогда не читался) → storage единственным из
// сервисов boot'ился insecure в «production» с одним WARN. Validate восстанавливает
// инвариант security.md «AuthN+AuthZ ВЕЗДЕ + любой деплой — production fail-closed»,
// зеркаля vpc.Config.Validate / geo.validateSecurityConfig.
//
// dev осознанно терпит insecure-дефолты (WARN эмитит serve.go) — только для локальных
// фикстур и dev-профиля стенда, НИКОГДА на кластере (KACHO_STORAGE_AUTH_MODE=dev на
// проде — security-долг под снос).
//
// Гейтит ровно те три измерения, которые serve.go реально wire'ит по конфигу:
//   - mTLS листенеров — cfg.PublicServerMTLS.Enable / cfg.InternalServerMTLS.Enable;
//   - per-RPC authz Check — подключается ⟺ непустой cfg.AuthZIAMGRPCAddr;
//   - DB-транспорт — cfg.DBSSLMode в DSN.
//
// Поэтому «Validate прошёл в production» ⟺ «serve поднимется secure» by construction.
func (c Config) Validate() error {
	mode, err := parseMode(c.AuthMode)
	if err != nil {
		return fmt.Errorf("KACHO_STORAGE_AUTH_MODE: %w", err)
	}
	if !mode.IsProduction() {
		// dev — insecure-дефолты допустимы (WARN в serve.go, не fatal).
		return nil
	}

	var problems []string

	// ── DB-транспорт: plaintext до БД в проде запрещён ──────────────────────
	switch mode {
	case ModeProduction:
		// Конкретный TLS-режим (require|verify-ca|verify-full) — на усмотрение
		// оператора; строгую проверку сертификата требует production-strict ниже.
		if c.DBSSLMode == "" || c.DBSSLMode == "disable" {
			problems = append(problems, fmt.Sprintf(
				"KACHO_STORAGE_DB_SSLMODE must not be %q (use require|verify-ca|verify-full)", c.DBSSLMode))
		}
	case ModeProductionStrict:
		switch c.DBSSLMode {
		case "require", "verify-ca", "verify-full":
		default:
			problems = append(problems, fmt.Sprintf(
				"KACHO_STORAGE_DB_SSLMODE must be one of require|verify-ca|verify-full (got %q)", c.DBSSLMode))
		}
	}

	// ── mTLS обязателен на ОБОИХ листенерах (internal :9091 НЕ освобождён) ──
	if !c.PublicServerMTLS.Enable || !c.InternalServerMTLS.Enable {
		problems = append(problems,
			"mTLS required on both listeners: set KACHO_STORAGE_PUBLIC_SERVER_MTLS_ENABLE and KACHO_STORAGE_INTERNAL_SERVER_MTLS_ENABLE=true")
	}

	// ── per-RPC authz Check обязателен (иначе serve пропускает интерсептор) ──
	if c.AuthZIAMGRPCAddr == "" {
		problems = append(problems,
			"per-RPC authz Check required on both listeners: set KACHO_STORAGE_AUTHZ_IAM_GRPC_ADDR")
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s mode refuses insecure config: %s", mode, strings.Join(problems, "; "))
	}
	return nil
}
