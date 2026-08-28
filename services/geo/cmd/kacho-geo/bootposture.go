// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/observability"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/config"
)

// bootPosture — самоотчёт kacho-geo о posture, с которой процесс реально
// стартовал (см. observability.BootPosture).
//
// Откуда значения:
//   - auth_mode     — cfg.AuthMode, уже пропущенный через разбор режима в
//     дескрипторе (закрытый перечень; неизвестное значение — отказ старта).
//   - db_sslmode    — из cfg.DSN(), т.е. из строки, реально уходящей в pgxpool
//     (пустой DBSSLMode деривится в `disable` внутри baseDSN).
//   - public/internal mtls — server-creds двух gRPC-листенеров (:9090/:9091).
//   - authz_check   — поднято ли ребро geo→iam InternalIAMService.Check.
//   - trusted_forwarders — сужен ли круг отправителей, которым разрешено передавать
//     личность конечного пользователя. Читается ТЕМ ЖЕ предикатом, что и отказ
//     старта, и что транспорт: общая библиотека отбрасывает пустые записи, поэтому
//     список из одних пустых строк вырождается там в «доверяем любому», и
//     рапортовать его как сужение значило бы отчитываться о намерении вместо
//     исхода. Величина одна на всех троих — расходиться нечему.
func bootPosture(cfg config.Config) observability.BootPosture {
	return observability.BootPosture{
		Service:           "geo",
		AuthMode:          cfg.AuthMode,
		DBSSLMode:         coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:        cfg.PublicServerMTLS.Enable,
		InternalMTLS: observability.InternalMTLSFrom(cfg.InternalServerMTLS.Enable),
		AuthZCheck:        cfg.AuthZIAMGRPCAddr != "",
		TrustedForwarders: cfg.TrustedForwarders().IsNarrowed(),
		// Личность человека этот сервис не проверяет — он принимает уже
		// проверенного вызывающего. Литерал, а не пустая строка: «измерения
		// нет» обязано быть отличимо от «поле не заполнено».
		IdentityProvider: observability.IdentityProviderNotApplicable,
	}
}
