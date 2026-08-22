// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// idempotency_store.go — построение хранилища однократности по конфигурации.
//
// Вид хранилища выбирает профиль посадки, а не сборка: флот из одной реплики
// обходится памятью процесса, флот больше одной — обязан иметь общее хранилище
// (пару сверяет validateIdempotencyFleetPairing до этого вызова).
package main

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// buildIdempotencyStore возвращает хранилище и, если оно держит внешние ресурсы,
// то, чем их закрыть.
//
// Неизвестный вид сюда не доходит: его отвергает валидация пары. Поэтому здесь
// нет ветки «прочее» с молчаливым откатом на память — откат на память при
// флоте больше одной реплики и есть тот дефект, ради которого всё это заведено.
func buildIdempotencyStore(
	ctx context.Context, cfg config.Config, logger *slog.Logger,
) (middleware.IdempotencyStore, *idempotencypg.Store, io.Closer, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.IdempotencyStoreKind)) {
	case idempotencyStorePostgres:
		store, err := idempotencypg.New(ctx, idempotencypg.Config{
			DSN:      cfg.IdempotencyDSN,
			TTL:      middleware.IdempotencyTTL,
			LeaseTTL: middleware.IdempotencyLeaseTTL,
			Logger:   logger,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		logger.Info("idempotency store: shared, spans the whole fleet",
			"kind", idempotencyStorePostgres, "fleet_size", cfg.FleetSize)
		return store, store, store, nil
	default:
		logger.Info("idempotency store: in this process only, valid for a single replica",
			"kind", idempotencyStoreMemory, "fleet_size", cfg.FleetSize)
		// Второе значение — nil НАМЕРЕННО: внешнего хранилища нет, значит и
		// однократность предъявления доказательства владения остаётся в памяти
		// процесса. Законно это ровно при флоте в одну реплику, и связывает их
		// та же проверка пары, что и для ключа однократности: два предмета, но
		// одно условие — запись видна всем репликам или репликa одна.
		return middleware.NewIdempotencyStore(middleware.IdempotencyTTL), nil, nil, nil
	}
}
