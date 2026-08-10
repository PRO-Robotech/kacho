// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/observability"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
)

// bootPosture — самоотчёт kacho-nlb о posture, с которой процесс реально
// стартовал (см. observability.BootPosture).
//
// Откуда значения:
//   - auth_mode     — cfg.Mode() (уже провалидирован config.Load → Validate).
//   - db_sslmode    — из repository.postgres.url: это ТА строка, что уходит в
//     pgxpool (nlb не деривит DSN и не имеет отдельного ssl-mode поля).
//     Отсутствующий в URL sslmode честно рапортуется как libpq-дефолт `prefer`
//     (plaintext-fallback), а не как «защищено».
//   - public/internal mtls — у nlb ОДИН server-cert (cfg.MTLS.Server), который
//     применяется к ОБОИМ gRPC-листенерам (:9090 и :9091), поэтому оба поля
//     отражают одно и то же реальное состояние — асимметрии тут не существует.
//   - authz_check   — не адрес, а факт проводки: клиент
//     InternalIAMService.Check поднимается только при успешном dial'е
//     iam-internal (peers.Check != nil).
//   - trusted_forwarders — сужен ли круг отправителей, которым разрешено передавать
//     личность конечного пользователя. Считается по НЕПУСТЫМ записям, а не по длине
//     среза: corelib отбрасывает пустые строки, поэтому список из одних пустых
//     записей вырождается там в «доверяем любому» — рапортовать его как сужение
//     значило бы отчитываться о намерении вместо исхода.
func bootPosture(cfg *config.Config, authzCheckWired bool) observability.BootPosture {
	return observability.BootPosture{
		Service:           "nlb",
		AuthMode:          cfg.Mode().String(),
		DBSSLMode:         coredb.SSLModeFromDSN(cfg.Repository.Postgres.URL),
		PublicMTLS:        cfg.MTLS.Server.Enable,
		InternalMTLS:      cfg.MTLS.Server.Enable,
		AuthZCheck:        authzCheckWired,
		TrustedForwarders: cfg.TrustedForwarders().IsNarrowed(),
	}
}
