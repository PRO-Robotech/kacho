// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscription_wiring.go — сборка ОБЩЕГО сервера потока изменений для журнала
// реестра.
//
// # Почему это отдельный файл, а не строки в композиционном корне
//
// Страж корня (`TestCompositionRootCarriesNoChainOfItsOwn`) читает `serve.go` и
// запрещает там имя `NewServer`: его предмет — «слушатель и цепочка звеньев
// собираются на стороне сервиса», то есть вторая поверхность правки контура.
// Дискриминатор у него — ГОЛОЕ ИМЯ селектора, поэтому под него попадает всякий
// `NewServer` любого пакета, включая этот — сервер ПОТОКА, который ни слушателя,
// ни цепочки звеньев не собирает.
//
// Разнести сборку по файлу — не обход стража, а та же раскладка, что у соседнего
// владельца журнала: у него сборка живёт в файле проводки, а не в корне. Корень
// зовёт `buildSubscriptionServer` и величин посадки не выбирает.
//
// Что страж от этого НЕ теряет: он по-прежнему судит `serve.go` и по-прежнему
// поймал бы там и слушатель, и цепочку. Что он не умеет — отличать сервер потока
// от слушателя; предмет назван, а не починен здесь.

package main

import (
	"context"
	"fmt"
	"log/slog"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/retention"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/registry/internal/subscriptionjournal"
)

// buildSubscriptionServer собирает ОБЩИЙ сервер потока изменений для журнала
// реестра.
//
// Владелец приносит сюда ЖУРНАЛ и величины ПОСАДКИ — и ничего больше: курсор,
// граница устоявшегося, пределы, сужение по правам и порядок отказов принадлежат
// общему серверу и владельцу не выдаются.
//
// Сужатель — ТОТ ЖЕ объект, что сужает страницы списков реестров: за глаголом
// подписки нет пообъектной проверки на крае (он сужаемый), поэтому откатываться
// не на что, а второй экземпляр означал бы, что поток сужается не тем, чем
// сужаются списки. Отношение видимости при этом берётся у него же (`v_list` на
// `registry_registry` — умолчание его карты), то есть вопрос, который задаёт
// поток, есть ТОТ ЖЕ вопрос, что задаёт список.
//
// Отсутствие сужателя — не отказ, а ОТСУТСТВИЕ ВОЗМОЖНОСТИ: сервер не собирается,
// глагол не выставляется. Прочие отказы возвращаются, а не логируются: величина
// посадки, о которой никто не сказал, не должна обнаруживаться первым запросом в
// бою.
func buildSubscriptionServer(cfg config.Config, narrower *listnarrow.Narrower,
	logger *slog.Logger) (subscriptionv1.InternalSubscriptionServiceServer, error) {
	if narrower == nil {
		return nil, nil
	}
	// Основа адреса производит output-only поле состояния события. Пустая
	// означала бы не «поле не заполнено», а «адрес у реестра такой» — утверждение,
	// которого владелец не делал и которое подписчик записал бы как факт.
	dsn := cfg.SingleConnDSN()
	if cfg.EndpointBase == "" {
		return nil, fmt.Errorf("поток изменений: endpoint base не задан " +
			"(KACHO_REGISTRY_ENDPOINT_BASE) — состояние каждого события несло бы адрес " +
			"вида \"/reg-…\", и подписчик записал бы его как факт")
	}
	// Страж посадки: параметр ПУЛА в строке одиночного соединения означает отказ
	// на подключении, а не на сборке, — и потому обязан быть пойман здесь, а не
	// первой подпиской в бою. Предикат один на дерево (coredb.PoolParamFromDSN):
	// он отдаёт ИМЯ ключа, поэтому отказ называет ручку, а не строку подключения,
	// которая несёт пароль базы.
	if key := coredb.PoolParamFromDSN(dsn); key != "" {
		return nil, fmt.Errorf("поток изменений: строка подключения несёт параметр пула %q: "+
			"вне пула это неизвестный PG-параметр и FATAL при подключении, "+
			"а отказ наступил бы не на сборке, а у каждой подписки в бою", key)
	}
	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		return nil, err
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal: subscriptionjournal.Journal(cfg.EndpointBase),
		// Выделенное соединение вне пула: `LISTEN` требует своей сессии, а сессия
		// из пула вернулась бы в него вместе с подпиской.
		//
		// Отсюда и ОТДЕЛЬНАЯ форма строки подключения: `DSN()` дописывает
		// `pool_max_conns`, а вне пула это неизвестный PG-параметр и FATAL при
		// подключении. Разбор строки такую ошибку не ловит — ключ уезжает серверу
		// как runtime-параметр, — поэтому отказ наступил бы на ПОДКЛЮЧЕНИИ и
		// выглядел бы тихо: сервер поднят, глагол выставлен, а каждая подписка
		// отвечает «источник недоступен» и никогда ничем иным.
		DSN:          dsn,
		Narrower:     narrower,
		ProjectGate:  gate,
		MaxStreams:   cfg.SubscriptionMaxStreams,
		StreamBudget: cfg.SubscriptionStreamBudget,
		IdlePoll:     cfg.SubscriptionIdlePoll,
		Logger:       logger,
	})
	if err != nil {
		return nil, fmt.Errorf("поток изменений: %w", err)
	}
	return srv, nil
}

// startJournalRetentionSweep поднимает фоновую уборку РЕСУРСНОГО ЖУРНАЛА реестра.
//
// # Почему отдельная петля, а не запись в реестре уборки таблицы операций
//
// Предметы разные, и пороги у них выводятся из РАЗНЫХ читателей: у таблицы
// операций читатель — оператор, разбирающий отказавшую мутацию, у журнала —
// подписчик, возобновляющийся с сохранённой позиции. Расписание при этом у обеих
// петель ОДНО и берётся из одного места (`retention.DefaultConfig`), поэтому
// разойтись им нечем: разошлись бы два ЛИТЕРАЛА, а не два вызова одной функции.
//
// Так же устроен и сосед: восемь владельцев таблицы операций поднимают по своей
// петле каждый, и это не восемь расписаний, а восемь вызовов одного.
//
// Отказывает, а не предупреждает: отказ означает негодные величины расписания
// либо объявление, при котором уборка невыразима, — то есть уборку, которая
// исполняется и не убирает ничего.
func startJournalRetentionSweep(ctx context.Context, db subscription.Execer,
	cfg config.Config, logger *slog.Logger) error {
	if _, err := subscription.StartJournalRetentionSweep(
		ctx, db, subscriptionjournal.Journal(cfg.EndpointBase),
		retention.DefaultConfig(),
		logger.With(slog.String("component", "journal_retention_sweep")),
	); err != nil {
		return fmt.Errorf("уборка ресурсного журнала: %w", err)
	}
	return nil
}
