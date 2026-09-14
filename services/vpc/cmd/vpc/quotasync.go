// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/corelib/quota"
	"github.com/PRO-Robotech/corelib/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/quota"
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
	// ReadPosture — как ЭТА установка объявила домен величин, для ВИТРИНЫ.
	//
	// Отдельно от полосы, а не выведено из её отсутствия (#2515). Полоса
	// собирается только под развёрнутый домен, поэтому её отсутствие означает
	// РАЗОМ два состояния: «провязать забыли» и «оператор объявил, что домена
	// величин нет». Следствия у них для арендатора противоположные, и пока
	// различия не было, витрина отвечала на законную посадку так же, как на
	// дефект сборки.
	ReadPosture quotaread.Posture
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
	pool *pgxpool.Pool,
	schema string,
	logger *slog.Logger,
) (quotaAuthorityEdge, func(), error) {
	noop := func() {}

	authority, err := cfg.QuotaAuthority()
	if err != nil {
		return quotaAuthorityEdge{}, noop, err
	}

	// Полосы пути запроса НЕТ и быть не может: производителя у контракта
	// авторитета величин не осталось ни в одном дереве, и объявленный адрес
	// отвергается стражем старта (`pkg/quota/quotaedge`.ValidateAuthorityHasAProducer).
	// Порт остаётся сокетом: он переживает смерть своей реализации by construction,
	// и это ровно то, ради чего он порт. Кто его наполнит — решает развилка
	// PRO-Robotech/kacho#2190.

	// Снимок величины обязан ДОГОНЯТЬ авторитет: без тянущего строка,
	// заведённая один раз, живёт со своей величиной вечно, и смена предела
	// администратором не доезжает до проекта никогда. Заведение стоит здесь
	// БЕЗУСЛОВНО — решение принимает StartLimitSync, читая объявление.
	stopSync, serr := corequota.StartLimitSync(
		ctx, pool, authority, nil, schema, corequota.Config{}, logger)
	if serr != nil {
		return quotaAuthorityEdge{}, noop, fmt.Errorf("start quota limit sync: %w", serr)
	}

	return quotaAuthorityEdge{
			Limits:      nil,
			ReadPosture: corequota.ReadPosture(authority, "vpc"),
		},
		stopSync, nil
}
