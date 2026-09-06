// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/clients"
)

// Ребро vpc→домен величин: ОДНО ребро, ДВЕ полосы.
//
// Полосы означают разное при недоступности соседа — разрешение величины на пути
// запроса fail-closed, фоновая дельта даёт ограниченное отставание, — но
// включаются они ОДНИМ объявлением (`quota.authority`). Два объявления об одном
// ребре разошлись бы молча, и разошлись бы именно там, где расхождение значит
// «спрашиваем у одного, догоняем у другого».
//
// Приёмка `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
// стадия S1, решение Д1.

// quotaAuthorityEdge — собранное ребро величин: обе полосы плюс останов.
type quotaAuthorityEdge struct {
	// Limits — полоса пути запроса. nil, когда домен объявлен отсутствующим:
	// отсутствие представимо ОТДЕЛЬНО от адреса, а не пустой строкой.
	//
	// Останова здесь нет намеренно: он один на всё ребро и возвращается вторым
	// значением сборки. Второе поле-останов было бы вторым способом закрыть одно
	// и то же — и первый же вызывающий, закрывший не тем, оставил бы соединение
	// открытым молча.
	Limits quota.LimitResolver
}

// buildQuotaAuthorityEdge разрешает объявление, при надобности дозванивается и
// заводит фоновую полосу.
//
// Дозвон стоит под ОБЪЯВЛЕНИЕМ, а не под наличием соседнего соединения. Прежде
// величины брались по соединению авторизации: своей ручки у резолва не было
// намеренно — второй адрес того же слушателя разошёлся бы с первым молча.
// Довод верен ровно до тех пор, пока авторитет величин и авторитет авторизации —
// одна служба; уход модуля квотирования это условие снимает.
func buildQuotaAuthorityEdge(
	ctx context.Context,
	cfg config.Config,
	mtlsCfg config.MTLSConfig,
	pool *pgxpool.Pool,
	schema string,
	logger *slog.Logger,
) (quotaAuthorityEdge, func(), error) {
	noop := func() {}

	authority, err := cfg.QuotaAuthority(mtlsCfg)
	if err != nil {
		return quotaAuthorityEdge{}, noop, err
	}

	var (
		limits    quota.LimitResolver
		src       corequota.Source
		closeConn = noop
	)
	if authority.Deployed() {
		conn, derr := dialPeer(ctx, "vpc→quota authority",
			mtlsCfg.QuotaAuthorityMTLS.Enable, mtlsCfg.QuotaAuthorityClientCreds, false,
			clients.BuildOptions{
				Endpoint: authority.Endpoint(),
				// Односторонний TLS у этого ребра не объявляется: удостоверение
				// здесь — только клиентский сертификат (довод — godoc
				// Config.QuotaAuthority). Поле читается набирателем лишь на
				// выключенном client-cert, то есть на посадке, где открытым
				// текстом ходят и все прочие рёбра.
				TLS: false,
			})
		if derr != nil {
			return quotaAuthorityEdge{}, noop, fmt.Errorf("dial quota authority: %w", derr)
		}
		closeConn = func() { _ = conn.Close() }
		limitClient := clients.NewLimitClient(conn)
		limits, src = limitClient, limitClient
		logger.Info("resource-count quota: limit authority edge configured",
			"endpoint", authority.Endpoint(),
			"mtls", mtlsCfg.QuotaAuthorityMTLS.Enable,
			"service", "vpc")
	}

	// Снимок величины обязан ДОГОНЯТЬ авторитет: без тянущего строка,
	// заведённая один раз, живёт со своей величиной вечно, и смена предела
	// администратором не доезжает до проекта никогда. Заведение стоит здесь
	// БЕЗУСЛОВНО — решение принимает StartLimitSync, читая объявление.
	stopSync, serr := corequota.StartLimitSync(
		ctx, pool, authority, src, schema, corequota.Config{}, logger)
	if serr != nil {
		closeConn()
		return quotaAuthorityEdge{}, noop, fmt.Errorf("start quota limit sync: %w", serr)
	}

	return quotaAuthorityEdge{Limits: limits},
		func() { stopSync(); closeConn() }, nil
}
