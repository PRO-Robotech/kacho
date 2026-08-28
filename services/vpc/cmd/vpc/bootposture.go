// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/observability"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
)

// bootPosture — самоотчёт kacho-vpc о posture, с которой процесс реально
// стартовал (см. observability.BootPosture: гейт обязан утверждать на
// наблюдаемом факте, а не на хранимом конфиге).
//
// Откуда значения:
//   - auth_mode     — cfg.AuthN.Mode (уже провалидирован cfg.Validate()).
//   - db_sslmode    — из ТОЙ строки, что уходит в pgxpool (cfg.DSN()): sslmode
//     может прийти как из repository.postgres.ssl-mode, так и из самого raw-URL
//     (composeDSN не перетирает уже заданный в URL), а пустое поле деривится в
//     `disable`. Сырое поле показало бы намерение, а не факт.
//   - public/internal mtls — per-listener server-creds mTLSConfig (тот же
//     mtlsCfg, из которого строятся оба листенера; cfg.ValidateServerMTLS уже
//     отработал).
//   - authz_check   — поднято ли ребро vpc→iam InternalIAMService.Check: пустой
//     endpoint → интерсептор не навешивается.
//   - trusted_forwarders — сужен ли круг отправителей, которым разрешено передавать
//     личность конечного пользователя. Читается из cfg.TrustedForwarders() — из ТОЙ
//     ЖЕ функции, значение которой main.go отдаёт в grpcsrv.WithTrustedForwarders,
//     поэтому отчёт не может разойтись с проводкой. Измерение ОТДЕЛЬНОЕ от mTLS:
//     сертификат доказывает, ЧЕЙ это пир, но не даёт права представляться другим —
//     это решает список. Без строки в отчёте гейт посадки, читающий живой процесс,
//     измерения бы не видел вовсе (и не видел — vpc был из его списка исключён).
func bootPosture(cfg config.Config, mtlsCfg config.MTLSConfig) observability.BootPosture {
	return observability.BootPosture{
		Service:           "vpc",
		AuthMode:          cfg.AuthN.Mode.String(),
		DBSSLMode:         coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:        mtlsCfg.PublicServerMTLS.Enable,
		InternalMTLS: observability.InternalMTLSFrom(mtlsCfg.InternalServerMTLS.Enable),
		AuthZCheck:        cfg.AuthZ.IAMEndpoint != "",
		TrustedForwarders: cfg.TrustedForwarders().IsNarrowed(),
		// Личность человека этот сервис не проверяет — он принимает уже
		// проверенного вызывающего. Литерал, а не пустая строка: «измерения
		// нет» обязано быть отличимо от «поле не заполнено».
		IdentityProvider: observability.IdentityProviderNotApplicable,
	}
}
