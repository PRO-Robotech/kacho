// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// feed_wiring.go — лента извещений пробы: источник, сервер ленты, сервер
// подписки и их регистрация на внутреннем слушателе.
//
// # Почему сборка здесь, а не в serve.go
//
// Страж композиционного корня судит `serve.go` и запрещает там имя
// `NewServer` — по голому селектору, поэтому под него попадает и сервер ленты,
// и сервер потока, хотя ни тот, ни другой ни слушателя, ни цепочки звеньев не
// собирают. Та же раскладка у соседних владельцев журнала (реестр). Регистрация
// сервера ленты при этом стоит в ПРОД-файле корня пробы (NTF1-C02), а не в
// фундаменте: корень решает, что он служит, фундамент — как.
//
// # Одно условие на сервер ленты, сервер подписки и звено идентичности
//
// Всё три выводятся из флага доставки: при выключенном флаге сервер ленты не
// поднимается (фундамент его и не построит), журнал без ключа ленты пуст и
// не собирается, поэтому сервер подписки не объявляется и не монтируется
// (NTF1-N07 (б)), а звену идентичности служить нечего. Источник ленты
// собирается при любом флаге: он несёт метрику состояния флага (NTF1-N09) и
// отвечает глаголу постановки единым отказом «доставка не настроена».
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	coredb "github.com/PRO-Robotech/corelib/db"
	"github.com/PRO-Robotech/corelib/listnarrow"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/retention"
	"github.com/PRO-Robotech/corelib/subscription"

	probev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/journal"
)

// feedParts — собранная лента пробы. server и subscribe либо оба есть
// (доставка включена), либо обоих нет.
type feedParts struct {
	source    *feed.Source
	server    *feed.Server
	subscribe subscriptionv1.InternalSubscriptionServiceServer
}

// serving — служит ли процесс ленту (и вместе с ней подписку).
func (p feedParts) serving() bool { return p.server != nil }

// buildFeed собирает ленту пробы. Сужатель — тот, которым поток подписки
// спрашивает видимость строки ленты; при выключенной доставке он не нужен.
func buildFeed(cfg config.Config, pool *pgxpool.Pool, narrower *listnarrow.Narrower,
	reg prometheus.Registerer, logger *slog.Logger) (feedParts, error) {
	signal, err := feed.JournalSignal(journal.Journal(), journal.Module, journal.ChangeUpdated)
	if err != nil {
		return feedParts{}, err
	}
	var sealer feed.Sealer
	if cfg.Keyring != nil {
		sealer = cfg.Keyring
	}
	source, err := feed.NewSource(feed.Config{
		Module:  journal.Module,
		Service: journal.Service,
		Enabled: cfg.Notifications,
		Signal:  signal,
		Sealer:  sealer,
		Metrics: reg,
	})
	if err != nil {
		return feedParts{}, fmt.Errorf("источник ленты: %w", err)
	}
	parts := feedParts{source: source}
	if !cfg.Notifications.On() {
		return parts, nil
	}

	server, err := feed.NewServer(feed.ServerConfig{
		Module:  journal.Module,
		Service: journal.Service,
		Enabled: cfg.Notifications,
		DB:      pool,
		Keyring: cfg.Keyring,
		Metrics: reg,
		Log:     logger.With(slog.String("component", "notification_feed")),
		// Сервер ленты модуля исхода не наблюдает: наблюдатель — у владельца
		// класса obligation в notify-sender (feed.NewLocal), а у модуля его нет,
		// и это пишется явным значением, а не пропуском (issue-2924 З28 п.3).
		Observer: feed.NopObserver,
	})
	if err != nil {
		return feedParts{}, fmt.Errorf("сервер ленты: %w", err)
	}
	subscribe, err := buildSubscriptionServer(cfg, narrower, logger)
	if err != nil {
		return feedParts{}, err
	}
	parts.server, parts.subscribe = server, subscribe
	return parts, nil
}

// buildSubscriptionServer собирает ОБЩИЙ сервер потока над журналом пробы.
// Сужатель обязателен: за глаголом подписки пообъектной проверки нет (он
// scope_filtered), и несужающий сервер отдал бы журнал молча.
func buildSubscriptionServer(cfg config.Config, narrower *listnarrow.Narrower,
	logger *slog.Logger) (subscriptionv1.InternalSubscriptionServiceServer, error) {
	if narrower == nil {
		return nil, fmt.Errorf("поток подписки: сужателя нет — глагол подписки не выставляется " +
			"несужающим, а без потока notify к ленте не просыпается")
	}
	dsn := cfg.SingleConnDSN()
	if key := coredb.PoolParamFromDSN(dsn); key != "" {
		return nil, fmt.Errorf("поток подписки: строка подключения несёт параметр пула %q: "+
			"вне пула это неизвестный PG-параметр и FATAL при подключении", key)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal:      journal.Journal(),
		DSN:          dsn,
		Narrower:     narrower,
		MaxStreams:   cfg.SubscriptionMaxStreams,
		StreamBudget: cfg.SubscriptionStreamBudget,
		IdlePoll:     cfg.SubscriptionIdlePoll,
		Logger:       logger.With(slog.String("component", "subscription")),
	})
	if err != nil {
		return nil, fmt.Errorf("поток подписки: %w", err)
	}
	return srv, nil
}

// sweepStopBudget — сколько гашение ждёт петли уборки после отмены: проход
// уборщика — пачки по одному оператору со своим сроком, и гашение не вправе
// закрыть пул под идущим оператором.
const sweepStopBudget = 15 * time.Second

// startFeedSweeps поднимает уборку ленты и уборку журнала подписки — при
// любом флаге: строки, поставленные до выключения, закрываются и убираются
// (NTF1-B31 (в)), а журнал без уборки рос бы монотонно. Возвращает гашение:
// отмена петель и ожидание их конца — ДО закрытия пула.
func startFeedSweeps(ctx context.Context, pool *pgxpool.Pool, reg prometheus.Registerer,
	logger *slog.Logger) (stop func(), err error) {
	sweepCtx, cancel := context.WithCancel(ctx)
	feedSweeper, err := feed.StartSweeper(sweepCtx, feed.SweeperConfig{
		Module:  journal.Module,
		Service: journal.Service,
		DB:      pool,
		Metrics: reg,
		Log:     logger.With(slog.String("component", "notification_feed_sweeper")),
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("уборщик ленты: %w", err)
	}
	journalSweeper, err := subscription.StartJournalRetentionSweep(sweepCtx, pool, journal.Journal(),
		retention.DefaultConfig(),
		logger.With(slog.String("component", "journal_retention_sweep")))
	if err != nil {
		cancel()
		feedSweeper.Wait(sweepStopBudget)
		return nil, fmt.Errorf("уборка журнала подписки: %w", err)
	}
	return func() {
		cancel()
		if !feedSweeper.Wait(sweepStopBudget) {
			logger.Error("уборщик ленты не остановился за срок гашения", "budget", sweepStopBudget)
		}
		if !journalSweeper.Wait(sweepStopBudget) {
			logger.Error("уборка журнала подписки не остановилась за срок гашения", "budget", sweepStopBudget)
		}
	}, nil
}

// registerPublic — публичный слушатель: служб у пробы на нём нет. Все её
// службы — Internal* (ban #6). Слушатель поднимается носителем парой с
// внутренним, и его пустой служимый набор — объявление, а не пропуск.
func registerPublic(grpc.ServiceRegistrar) {}

// registerInternal — внутренний слушатель: глагол пробы
// InternalNotifyProbeService при любом флаге (при выключенной доставке он
// отвечает единым отказом «доставка не настроена», NTF1-N06), а сервер ленты и
// сервер подписки — когда доставка включена. Регистрация ленты стоит ЗДЕСЬ, в
// прод-файле корня (NTF1-C02).
func registerInternal(reg grpc.ServiceRegistrar, ports servePorts) {
	probev1.RegisterInternalNotifyProbeServiceServer(reg, ports.send)
	parts := ports.parts
	if !parts.serving() {
		return
	}
	notifyv1.RegisterInternalNotificationFeedServiceServer(reg, parts.server)
	subscriptionv1.RegisterInternalSubscriptionServiceServer(reg, parts.subscribe)
}
