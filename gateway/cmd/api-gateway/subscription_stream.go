// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/subjectchange"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

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
		// Ключ внутреннего адреса даёт config — карта соединений там же объявлена,
		// и второе его написание разошлось бы с ней молча.
		conn := backends[config.InternalBackendKey(name)]
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

	// Самоотчёт процесса называет ЧИСЛО объявленных владельцев и их имена.
	//
	// Ноль остаётся законным ИСХОДОМ (профиль вправе выключить возможность), но
	// перестал быть состоянием поставки: умолчание чарта края называет всех
	// владельцев, служащих глагол (kacho#1388). Поэтому ноль обязан быть ВИДЕН —
	// молчаливый, он неотличим от «всё поднялось», а означает `501` каждому.
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

// buildSubjectChangeWatcher собирает читателя отзыва и связывает его с реестром
// открытых потоков.
//
// # Почему это ФУНКЦИЯ, а не десять строк в точке сборки
//
// Провязку закрывателя нельзя доказать, пока она живёт в `main`: инъекция —
// передать ноль здесь — оставляла ВЕСЬ корпус проб края зелёным и код
// собирающимся, потому что сквозная проба зовёт конструктор читателя напрямую,
// минуя продовую точку сборки. Вынесенная функция даёт пробе тот самый вход,
// который исполняется в бою.
//
// Сам ноль закрывателя отвергает уже [subjectchange.New]; здесь проверяется ВТОРАЯ
// половина того же предмета — что край передаёт туда именно СВОЙ реестр
// открытых потоков, а не что-нибудь непустое.
func buildSubjectChangeWatcher(
	cfg config.Config,
	poller subjectchange.Poller,
	flush func(),
	streams *subscriptionstream.Handler,
	logger *slog.Logger,
) (*subjectchange.Watcher, error) {
	if streams == nil {
		return nil, fmt.Errorf("subject-change watcher: проекция потока не собрана — " +
			"читателю отзыва нечего закрывать, и это ошибка порядка сборки, а не посадки")
	}
	staleAfter := revocationStaleAfter(cfg.SubjectChangePollInterval)
	if staleAfter >= cfg.SubscriptionStreamBudget {
		return nil, fmt.Errorf(
			"subject-change watcher: срок неподтверждённого чтения отзыва %v не меньше срока жизни "+
				"потока %v — fail-closed не наступит ни разу, а закрытие по собственному бюджету "+
				"потока выглядело бы закрытием по отзыву",
			staleAfter, cfg.SubscriptionStreamBudget)
	}
	return subjectchange.New(subjectchange.Config{
		Poller:     poller,
		Flush:      flush,
		Interval:   cfg.SubjectChangePollInterval,
		Closer:     streams,
		StaleAfter: staleAfter,
		Logger:     logger,
	})
}
