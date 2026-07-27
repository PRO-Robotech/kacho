// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/observability"

	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// bootPosture — самоотчёт kacho-compute о posture, с которой процесс реально
// стартовал (см. observability.BootPosture).
//
// Откуда значения:
//   - auth_mode     — cfg.AuthMode, уже пропущенный через validateAuthMode
//     (whitelist + fail-closed гейты), т.е. режим, который процесс ПРИНЯЛ.
//   - db_sslmode    — из cfg.DSN(), т.е. из строки, реально уходящей в pgxpool
//     (пустой DBSSLMode деривится в `disable` внутри baseDSN).
//   - public/internal mtls — server-creds двух gRPC-листенеров (:9090/:9091).
//   - authz_check   — поднято ли ребро compute→iam InternalIAMService.Check.
//   - trusted_forwarders — сужен ли круг отправителей, которым разрешено передавать
//     личность конечного пользователя. Считается по НЕПУСТЫМ записям, а не по длине
//     среза: corelib отбрасывает пустые строки, поэтому список из одних пустых
//     записей вырождается там в «доверяем любому» — рапортовать его как сужение
//     значило бы отчитываться о намерении вместо исхода.
func bootPosture(cfg config.Config) observability.BootPosture {
	return observability.BootPosture{
		Service:           "compute",
		AuthMode:          cfg.AuthMode,
		DBSSLMode:         coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:        cfg.PublicServerMTLS.Enable,
		InternalMTLS:      cfg.InternalServerMTLS.Enable,
		AuthZCheck:        cfg.AuthZIAMGRPCAddr != "",
		TrustedForwarders: hasNonEmpty(cfg.AuthZTrustedForwarderSANs),
	}
}

// hasNonEmpty — есть ли в срезе хотя бы одна непустая запись. Зеркалит фильтр
// corelib grpcsrv.WithTrustedForwarders (принимает только s != ""), поэтому отчёт
// о посадке совпадает с тем, что реально попало в allow-list.
func hasNonEmpty(ss []string) bool {
	for _, s := range ss {
		if s != "" {
			return true
		}
	}
	return false
}
