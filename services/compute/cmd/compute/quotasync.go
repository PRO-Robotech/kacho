// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/grpcclient"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
	"github.com/PRO-Robotech/kacho/services/compute/internal/config"
)

// Ребро compute→домен величин: ОДНО ребро, ДВЕ полосы.
//
// Полосы означают разное при недоступности соседа — разрешение величины на пути
// запроса fail-closed, фоновая дельта даёт ограниченное отставание, — но
// включаются они ОДНИМ объявлением. Два объявления об одном ребре разошлись бы
// молча, и разошлись бы именно там, где расхождение значит «спрашиваем у одного,
// догоняем у другого».

// quotaAuthorityEdge — собранное ребро величин: полоса пути запроса плюс останов.
type quotaAuthorityEdge struct {
	// Limits — nil, когда домен объявлен отсутствующим: отсутствие представимо
	// ОТДЕЛЬНО от адреса, а не пустой строкой.
	Limits quota.LimitResolver
}

// buildQuotaAuthorityEdge разрешает объявление, при надобности дозванивается и
// заводит фоновую полосу.
func buildQuotaAuthorityEdge(
	ctx context.Context,
	cfg config.Config,
	pool *pgxpool.Pool,
	schema string,
	logger *slog.Logger,
) (quotaAuthorityEdge, func(), error) {
	noop := func() {}

	authority, err := cfg.QuotaAuthorityDeclaration()
	if err != nil {
		return quotaAuthorityEdge{}, noop, err
	}

	var (
		limits    quota.LimitResolver
		src       corequota.Source
		closeConn = noop
	)
	if authority.Deployed() {
		creds, cerr := grpcclient.TLSClientTransportCreds(cfg.QuotaAuthorityMTLS)
		if cerr != nil {
			return quotaAuthorityEdge{}, noop, fmt.Errorf("compute→quota authority mTLS creds: %w", cerr)
		}
		conn, derr := dialPeerCreds(authority.Endpoint(), creds, true)
		if derr != nil {
			return quotaAuthorityEdge{}, noop, fmt.Errorf("dial quota authority: %w", derr)
		}
		closeConn = func() { _ = conn.Close() }
		limitClient := clients.NewLimitClient(conn)
		limits, src = limitClient, limitClient
		logger.Info("resource-count quota: limit authority edge configured",
			"endpoint", authority.Endpoint(),
			"mtls", cfg.QuotaAuthorityMTLS.Enable,
			"service", "compute")
	}

	// Заведение стоит БЕЗУСЛОВНО — решение принимает StartLimitSync, читая
	// объявление. Пока оно принималось здесь, признаком служило наличие
	// соединения соседа по авторизации, и после снятия авторитета величин подъём
	// отказал бы ПРИ СБОРКЕ, а этот отказ фатален.
	stopSync, serr := corequota.StartLimitSync(
		ctx, pool, authority, src, schema, corequota.Config{}, logger)
	if serr != nil {
		closeConn()
		return quotaAuthorityEdge{}, noop, fmt.Errorf("start quota limit sync: %w", serr)
	}

	return quotaAuthorityEdge{Limits: limits}, func() { stopSync(); closeConn() }, nil
}
