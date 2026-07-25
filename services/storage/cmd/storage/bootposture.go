// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/observability"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
)

// bootPosture — самоотчёт kacho-storage о posture, с которой процесс реально
// стартовал (см. observability.BootPosture). Эталон контракта: остальные семь
// сервисов приведены к этой же форме, storage добрал недостающее поле `service`.
//
// Откуда значения:
//   - auth_mode     — cfg.AuthMode, уже принятый cfg.Validate() (fail-closed
//     boot-guard #56).
//   - db_sslmode    — из cfg.DSN(), т.е. из строки, РЕАЛЬНО уходящей в pgxpool,
//     а не из поля конфига. Разница не косметическая: весь смысл этой строки —
//     отчитываться об ИСХОДЕ, а не о намерении, поэтому читать поле, которое
//     лишь ПРЕДПОЛАГАЕТСЯ подставить в DSN, значило бы воспроизвести ровно ту
//     ошибку, ради которой самоотчёт и заведён (см. observability.BootPosture).
//     Пустое поле деривится в `disable` внутри baseDSN — и так и будет доложено.
//   - public/internal mtls — server-creds двух gRPC-листенеров (:9090/:9091).
//   - authz_check   — поднято ли ребро storage→iam InternalIAMService.Check.
func bootPosture(cfg config.Config) observability.BootPosture {
	return observability.BootPosture{
		Service:      "storage",
		AuthMode:     cfg.AuthMode,
		DBSSLMode:    coredb.SSLModeFromDSN(cfg.DSN()),
		PublicMTLS:   cfg.PublicServerMTLS.Enable,
		InternalMTLS: cfg.InternalServerMTLS.Enable,
		AuthZCheck:   cfg.AuthZIAMGRPCAddr != "",
	}
}
