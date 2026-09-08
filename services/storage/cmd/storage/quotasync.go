// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// Ребро storage→домен величин: ОДНО ребро, ДВЕ полосы.
//
// Полосы означают разное при недоступности соседа — разрешение величины на пути
// запроса fail-closed, фоновая дельта даёт ограниченное отставание, — но
// включаются они ОДНИМ объявлением. Два объявления об одном ребре разошлись бы
// молча, и разошлись бы именно там, где расхождение значит «спрашиваем у одного,
// догоняем у другого».

// quotaAuthorityEdge — собранное ребро величин: полоса пути запроса.
type quotaAuthorityEdge struct {
	// Guard — nil, когда домен объявлен отсутствующим: отсутствие представимо
	// ОТДЕЛЬНО от адреса, а не пустой строкой.
	Guard *quota.Guard
}

// buildQuotaAuthorityEdge разрешает объявление, при надобности дозванивается и
// заводит фоновую полосу.
func buildQuotaAuthorityEdge(
	ctx context.Context,
	cfg config.Config,
	pool *pgxpool.Pool,
	accounts quota.AccountLocator,
	logger *slog.Logger,
) (quotaAuthorityEdge, func(), error) {
	noop := func() {}

	authority, err := cfg.QuotaAuthorityDeclaration()
	if err != nil {
		return quotaAuthorityEdge{}, noop, err
	}

	var (
		guard     *quota.Guard
		src       corequota.Source
		closeConn = noop
	)
	if authority.Deployed() {
		conn, derr := dialPeer(authority.Endpoint(), cfg.QuotaAuthorityMTLS, logger, "quota-authority")
		if derr != nil {
			return quotaAuthorityEdge{}, noop, fmt.Errorf("dial quota authority: %w", derr)
		}
		if conn == nil {
			// dialPeer отдаёт nil на пустом адресе. Сюда мы попасть не можем:
			// объявление уже разрешено и несёт непустой адрес. Ветка стоит,
			// чтобы «невозможно» было названо, а не выведено читателем.
			return quotaAuthorityEdge{}, noop, fmt.Errorf(
				"объявление домена величин разрешено адресом %q, а набиратель отдал "+
					"пустое соединение", authority.Endpoint())
		}
		closeConn = func() { _ = conn.Close() }
		limitClient := clients.NewLimitClient(conn)
		guard = quota.NewGuard(pg.NewQuotaRepo(pool), limitClient, accounts)
		src = limitClient
		logger.Info("resource-count quota: limit authority edge configured",
			"endpoint", authority.Endpoint(),
			"mtls", cfg.QuotaAuthorityMTLS.Enable,
			"service", "storage")
	}

	// Заведение стоит БЕЗУСЛОВНО — решение принимает StartLimitSync, читая
	// объявление.
	stopSync, serr := corequota.StartLimitSync(
		ctx, pool, authority, src, pg.QuotaSchema, corequota.Config{}, logger)
	if serr != nil {
		closeConn()
		return quotaAuthorityEdge{}, noop, fmt.Errorf("start quota limit sync: %w", serr)
	}

	return quotaAuthorityEdge{Guard: guard}, func() { stopSync(); closeConn() }, nil
}
