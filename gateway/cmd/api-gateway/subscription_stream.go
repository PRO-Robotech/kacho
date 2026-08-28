// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

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
		Owners:               owners,
		StreamBudget:         cfg.SubscriptionStreamBudget,
		Heartbeat:            cfg.SubscriptionHeartbeat,
		MaxStreams:           cfg.SubscriptionMaxStreams,
		MaxStreamsPerSubject: cfg.SubscriptionMaxStreamsPerSubject,
		Logger:               logger,
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
		"max_streams", cfg.SubscriptionMaxStreams,
		"max_streams_per_subject", cfg.SubscriptionMaxStreamsPerSubject)

	return handler, nil
}

// revocationStaleFactor / revocationStaleFloor — во сколько перепросов подряд
// обходится срок, после которого край считает читателя отзыва потерянным.
//
// # Почему величина ВЫВОДИТСЯ, а не объявляется ручкой
//
// Она измеряет отказ ОДНОГО названного механизма — перепроса изменений
// субъекта, — и всякий раз, когда его период меняют, должна меняться вместе с
// ним. Ручка, объявленная отдельно, разошлась бы с периодом молча и разошлась бы
// именно там, где расхождение не видно: обе непусты, обе выглядят разумно, а
// fail-closed наступает либо на всякой заминке, либо никогда.
//
// # Почему пять, а не «побольше на всякий случай»
//
// Один пропущенный перепрос — заминка сети, а не потеря читателя; объявить её
// аварией значит закрывать потоки всего флота на каждом чихе соседа. Пять подряд
// заминкой уже не бывают. Пол в десять секунд держит нижнюю сторону: при частом
// перепросе пять периодов складываются в срок, за который сосед не успевает
// даже перезапуститься.
//
// Верхнюю сторону держит не эта константа, а страж старта: срок обязан быть
// заметно меньше срока жизни потока, иначе fail-closed не наступает ни разу.
const (
	revocationStaleFactor = 5
	revocationStaleFloor  = 10 * time.Second
)

// revocationStaleAfter — срок, после которого неподтверждённое чтение отзыва
// само становится решением закрыть потоки.
func revocationStaleAfter(pollInterval time.Duration) time.Duration {
	stale := pollInterval * revocationStaleFactor
	if stale < revocationStaleFloor {
		return revocationStaleFloor
	}
	return stale
}
