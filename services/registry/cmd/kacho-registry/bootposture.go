// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/observability"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
)

// bootPosture — самоотчёт kacho-registry о posture, с которой процесс реально
// стартовал (см. observability.BootPosture).
//
// Откуда значения:
//   - auth_mode     — cfg.AuthMode, уже пропущенный через validateAuthMode.
//   - db_sslmode    — из cfg.DSN(), т.е. из строки, реально уходящей в pgxpool
//     (пустой DBSSLMode деривится в `disable` внутри baseDSN).
//   - public/internal mtls — РОВНО два gRPC-листенера (:9090 / :9091). У registry
//     есть ещё data-plane docker-листенер (OCI-registry HTTP) — он в этот
//     контракт не подмешивается (иначе поле перестало бы значить одно и то же в
//     разных сервисах).
//   - authz_check   — поднято ли ребро registry→iam InternalIAMService.Check.
//   - trusted_forwarders — сужен ли круг отправителей, которым разрешено передавать
//     личность конечного пользователя. Берётся из cfg.TrustedForwarders() — ровно
//     того значения, что уходит в grpcsrv.WithTrustedForwarders на обоих
//     листенерах, — а не из сырого поля: corelib отбрасывает пустые записи,
//     поэтому список из одних пустых записей вырождается там в «доверяем любому»,
//     и рапортовать его как сужение значило бы отчитываться о намерении вместо
//     исхода.
func bootPosture(cfg config.Config) observability.BootPosture {
	return observability.BootPosture{
		Service:           "registry",
		AuthMode:          cfg.AuthMode,
		DBSSLMode:         coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:        cfg.PublicServerMTLS.Enable,
		InternalMTLS:      cfg.InternalServerMTLS.Enable,
		AuthZCheck:        cfg.AuthZIAMGRPCAddr != "",
		TrustedForwarders: cfg.TrustedForwarders().IsNarrowed(),
		// Личность человека этот сервис не проверяет — он принимает уже
		// проверенного вызывающего. Литерал, а не пустая строка: «измерения
		// нет» обязано быть отличимо от «поле не заполнено».
		IdentityProvider: observability.IdentityProviderNotApplicable,
	}
}
