// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// internalBackendSuffix — как называется ключ ВНУТРЕННЕГО адреса домена в карте
// соединений. Подписка живёт только там: имя её службы начинается с `Internal`,
// и на публичном порту владельца её нет вовсе.
const internalBackendSuffix = "Internal"

// buildSubscriptionStreamHandler собирает единственную проекцию потока.
//
// # Почему адрес НЕ отдельная ручка конфигурации
//
// Внутренний адрес каждого домена край уже объявляет
// (`KACHO_API_GATEWAY_<ДОМЕН>_INTERNAL_GRPC`). Второе объявление того же адреса
// разошлось бы с первым молча — и разошлось бы именно там, где расхождение не
// видно: оба непусты, оба резолвятся, а ведут в разные места. Поэтому владелец
// называется ИМЕНЕМ ДОМЕНА, а адрес берётся из уже объявленного.
//
// # Почему отказ старта, а не отказ запроса
//
// Владелец, названный посадкой, но не имеющий внутреннего адреса, — ошибка
// посадки, а не вызывающего. Обнаруживать её первым запросом в бою значит
// узнать о ней тогда, когда она уже кому-то стоила ответа.
func buildSubscriptionStreamHandler(
	cfg config.Config,
	backends proxy.Backends,
	logger *slog.Logger,
) (*subscriptionstream.Handler, error) {
	names := cfg.SubscriptionOwnerNames()
	owners := make(subscriptionstream.Owners, len(names))
	missing := make([]string, 0, len(names))

	for _, name := range names {
		conn := backends[name+internalBackendSuffix]
		if conn == nil {
			missing = append(missing, name)
			continue
		}
		owners[name] = subscriptionv1.NewInternalSubscriptionServiceClient(conn)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf(
			"KACHO_API_GATEWAY_SUBSCRIPTION_OWNERS называет владельцев без внутреннего адреса: %s — "+
				"подписка живёт только на внутреннем слушателе владельца, и дозвониться до названного домена нечем",
			strings.Join(missing, ", "))
	}

	handler, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		Owners:       owners,
		StreamBudget: cfg.SubscriptionStreamBudget,
		Heartbeat:    cfg.SubscriptionHeartbeat,
		MaxStreams:   cfg.SubscriptionMaxStreams,
		Logger:       logger,
	})
	if err != nil {
		return nil, err
	}

	// Самоотчёт процесса называет ЧИСЛО объявленных владельцев и их имена: ноль
	// — законное состояние этой фазы (первого владельца заводит kacho#1019), но
	// оно обязано быть ВИДНО. Молчаливый ноль неотличим от «всё поднялось».
	logger.Info("subscription stream projection ready",
		"path", subscriptionstream.Path,
		"owners", owners.Names(),
		"owner_count", len(owners),
		"stream_budget", cfg.SubscriptionStreamBudget.String(),
		"heartbeat", cfg.SubscriptionHeartbeat.String(),
		"max_streams", cfg.SubscriptionMaxStreams)

	return handler, nil
}
