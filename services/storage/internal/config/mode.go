// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"strings"
)

// Mode — общий режим работы сервиса kacho-storage (управляет fail-closed posture).
// Зеркалит vpc/geo/nlb: dev терпит insecure-дефолты (только для локальных фикстур и
// dev-профиля стенда), production/production-strict — refuse-to-start при небезопасной
// конфигурации (см. Config.Validate, security.md «любой деплой — production-mode»).
//
//	ModeDev              — СТРОГО локальный fixture-режим (unit/integration-тесты и
//	                       локальная разработка), НИКОГДА не для развёрнутого стенда:
//	                       insecure-defaults (mTLS off, sslmode=disable, authz off)
//	                       только логируются WARN, не роняют старт.
//	ModeProduction       — fail-closed: обязательны mTLS на обоих листенерах, per-RPC
//	                       authz Check (непустой AuthZIAMGRPCAddr) и не-plaintext DB
//	                       (sslmode ≠ disable). Иначе — отказ старта.
//	ModeProductionStrict — production + строгая проверка DB-SSL (sslmode обязан быть
//	                       одним из require|verify-ca|verify-full).
type Mode int

// Значения ENUM. iota-порядок стабилен; не менять без миграции deploy-values.
const (
	ModeDev Mode = iota
	ModeProduction
	ModeProductionStrict
)

// String — каноническое имя для логирования / config-ошибок.
func (m Mode) String() string {
	switch m {
	case ModeDev:
		return "dev"
	case ModeProduction:
		return "production"
	case ModeProductionStrict:
		return "production-strict"
	default:
		return fmt.Sprintf("mode(%d)", int(m))
	}
}

// IsProduction возвращает true для любого production-варианта (fail-closed posture).
func (m Mode) IsProduction() bool {
	return m == ModeProduction || m == ModeProductionStrict
}

// parseMode — точечная инверсия String(); whitelist допустимых значений AuthMode.
func parseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dev":
		return ModeDev, nil
	case "production":
		return ModeProduction, nil
	case "production-strict":
		return ModeProductionStrict, nil
	default:
		return ModeDev, fmt.Errorf("unknown mode %q (allowed: dev, production, production-strict)", s)
	}
}
