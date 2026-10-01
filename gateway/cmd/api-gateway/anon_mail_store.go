// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// anon_mail_store.go — хранилище звена-ограничителя анонимной почты края
// (замысел issue-2917, З8).
//
// Вид хранилища — то же объявление, что у однократности
// (`KACHO_IDEMPOTENCY_STORE`): второго объявления «где живёт состояние края»
// нет, и пару «вид ↔ флот» сверяет тот же отказ старта.
package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware/anonmail"
)

// buildAnonMailStore — хранилище ограничителя. При виде postgres аргументом
// служит УЖЕ построенное хранилище однократности (CX2-44 (1)): цепочку миграций
// края, в том числе таблицы ограничителя и строку ведра, накатывает его
// построение под блокировкой схемы, и пул ограничителя строится только после
// этого — по тому же адресу. Порядок выражен входом: без него сборка отказывает.
func buildAnonMailStore(ctx context.Context, idem *idempotencypg.Store, cfg config.Config,
	limits config.AnonMailLimits, logger *slog.Logger) (anonmail.Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.IdempotencyStoreKind)) {
	case idempotencyStorePostgres:
		if idem == nil {
			return nil, errors.New("anonymous mail limiter: the postgres store is built after the idempotency store, " +
				"and the idempotency store is not built")
		}
		s, err := anonmail.NewPostgresStore(ctx, idem.ConnString(), limits, logger)
		if err != nil {
			return nil, err
		}
		logger.Info("anonymous mail limiter store: shared, own pool", "kind", idempotencyStorePostgres)
		return s, nil
	default:
		logger.Info("anonymous mail limiter store: in this process only, valid for a single replica",
			"kind", idempotencyStoreMemory, "fleet_size", cfg.FleetSize)
		return anonmail.NewMemoryStore(limits, time.Now, logger)
	}
}
