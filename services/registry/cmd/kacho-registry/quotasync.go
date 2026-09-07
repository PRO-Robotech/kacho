// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/quota"
	iamclient "github.com/PRO-Robotech/kacho/services/registry/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// Ребро registry→домен величин: ОДНО ребро, ДВЕ полосы.
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
		creds, cerr := grpcclient.TLSClientCreds(cfg.QuotaAuthorityMTLS)
		if cerr != nil {
			return quotaAuthorityEdge{}, noop, fmt.Errorf("registry→quota authority mTLS creds: %w", cerr)
		}
		conn, derr := grpc.NewClient(authority.Endpoint(), creds,
			grpcclient.KeepaliveDialOption(true))
		if derr != nil {
			return quotaAuthorityEdge{}, noop, fmt.Errorf("dial quota authority: %w", derr)
		}
		closeConn = func() { _ = conn.Close() }
		limitClient := iamclient.NewLimitClient(conn)
		src = limitClient
		if store := pg.NewQuotaStore(pool); store != nil {
			guard = quota.NewGuard(store, limitClient, accounts, "registry")
		}
		logger.Info("resource-count quota: limit authority edge configured",
			"endpoint", authority.Endpoint(),
			"mtls", cfg.QuotaAuthorityMTLS.Enable,
			"service", "registry")
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
