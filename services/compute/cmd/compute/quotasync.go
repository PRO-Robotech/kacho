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
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/quota"
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
	// ReadPosture — как ЭТА установка объявила домен величин, для ВИТРИНЫ.
	//
	// Отдельно от полосы, а не выведено из её отсутствия. Полоса
	// собирается только под развёрнутый домен, поэтому её отсутствие означает
	// РАЗОМ два состояния: «провязать забыли» и «оператор объявил, что домена
	// величин нет». Следствия у них для арендатора противоположные, и пока
	// различия не было, витрина отвечала на законную посадку так же, как на
	// дефект сборки.
	ReadPosture quotaread.Posture
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

	// Полосы пути запроса НЕТ и быть не может: производителя у контракта
	// авторитета величин не осталось ни в одном дереве, и объявленный адрес
	// отвергается стражем старта (`pkg/quota/quotaedge`.ValidateAuthorityHasAProducer).
	// Порт остаётся сокетом: он переживает смерть своей реализации by construction,
	// и это ровно то, ради чего он порт. Кто его наполнит — решает развилка,
	// которую ведёт задача продукта 2190.

	// Заведение стоит БЕЗУСЛОВНО — решение принимает StartLimitSync, читая
	// объявление. Пока оно принималось здесь, признаком служило наличие
	// соединения соседа по авторизации, и после снятия авторитета величин подъём
	// отказал бы ПРИ СБОРКЕ, а этот отказ фатален.
	stopSync, serr := corequota.StartLimitSync(
		ctx, pool, authority, nil, schema, corequota.Config{}, logger)
	if serr != nil {
		return quotaAuthorityEdge{}, noop, fmt.Errorf("start quota limit sync: %w", serr)
	}

	return quotaAuthorityEdge{
			Limits:      nil,
			ReadPosture: corequota.ReadPosture(authority, "compute"),
		},
		stopSync, nil
}
