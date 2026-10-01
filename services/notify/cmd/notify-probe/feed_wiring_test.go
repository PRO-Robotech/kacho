// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// feed_wiring_test.go — что проба СЛУЖИТ, спрошенное у сервера, на котором
// зарегистрировал ПРОД-регистратор корня (registerInternal / registerPublic):
//
//   - доставка включена — внутренний слушатель служит ленту
//     (`corelib.notify.InternalNotificationFeedService`) и подписку
//     (`corelib.subscription.InternalSubscriptionService`);
//   - выключена — ни того, ни другого (NTF1-N07 (б) для пробы: других видов
//     журнала нет, поэтому сервер подписки не объявлен и не смонтирован);
//   - публичный слушатель не служит ничего (обе службы — Internal*, ban #6).
//
// И что носитель поднимает пробу с включённой доставкой без отказа старта:
// объявленное в дескрипторе (звено идентичности, привязка ленты, сужатель,
// бюджет потоков) сходится со служимым набором.

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/servicehost"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

const (
	feedService         = "corelib.notify.InternalNotificationFeedService"
	subscriptionService = "corelib.subscription.InternalSubscriptionService"
)

// assembled — проба, собранная корнем над базой pgtest с окружением флага.
func assembled(t *testing.T, flag string) (config.Config, probe, *slog.Logger, *lockedBuffer) {
	t.Helper()
	ca := newTestCA(t)
	dsn := pgtest.NewDB(t)
	setEnv(t, standEnv(t, dsn, serveModel(t, &model{}), flag, ca))
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("конфигурация стенда отвергнута: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.DSN())
	if err != nil {
		t.Fatal(err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	log := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(log, nil))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := assemble(ctx, cfg, logger, pool, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("корень не собрал пробу: %v", err)
	}
	t.Cleanup(p.close)
	return cfg, p, logger, log
}

func served(register func(grpc.ServiceRegistrar)) []string {
	srv := grpc.NewServer()
	register(srv)
	var out []string
	for name := range srv.GetServiceInfo() {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Регистрация сервера ленты стоит в прод-файле корня и зависит от флага.
func TestRootRegistersTheFeedServerOnlyWhenDeliveryIsOn(t *testing.T) {
	_, on, _, _ := assembled(t, "true")
	got := served(func(r grpc.ServiceRegistrar) { registerInternal(r, on.ports.parts) })
	if strings.Join(got, ",") != feedService+","+subscriptionService {
		t.Fatalf("включённая доставка: внутренний слушатель служит %v, ожидались %s и %s",
			got, feedService, subscriptionService)
	}
	if pub := served(registerPublic); len(pub) != 0 {
		t.Fatalf("публичный слушатель служит %v — Internal*-службы на внешний край не выходят", pub)
	}

	_, off, _, _ := assembled(t, "false")
	if got := served(func(r grpc.ServiceRegistrar) { registerInternal(r, off.ports.parts) }); len(got) != 0 {
		t.Fatalf("выключенная доставка: внутренний слушатель служит %v — ни ленты, ни подписки быть не должно", got)
	}
	if off.ports.parts.source == nil || off.ports.parts.source.Enabled() {
		t.Fatal("источник ленты при выключенной доставке обязан быть собран и выключен (метрика флага)")
	}
}

// Носитель поднимает пробу с включённой доставкой без отказа старта.
func TestCarrierRaisesTheProbeWithoutAStartRefusal(t *testing.T) {
	for _, flag := range []string{"true"} {
		t.Run(flag, func(t *testing.T) {
			cfg, p, logger, log := assembled(t, flag)
			desc, err := describe(cfg, logger, p.ports)
			if err != nil {
				t.Fatalf("дескриптор отвергнут конструктором:\n%v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			serveErr := servicehost.Serve(ctx, desc, registerPublic,
				func(r grpc.ServiceRegistrar) { registerInternal(r, p.ports.parts) })
			if serveErr != nil && strings.Contains(serveErr.Error(), "не поднимается") {
				t.Fatalf("носитель отказал пробе в старте:\n%v", serveErr)
			}
			if serveErr != nil && !strings.Contains(serveErr.Error(), "server has been stopped") {
				t.Fatalf("носитель вернул ошибку подъёма: %v", serveErr)
			}
			if !strings.Contains(log.String(), "start refusals passed") {
				t.Fatalf("перепись отказов старта не напечатана — отказы не исполнялись:\n%s", log.String())
			}
			t.Logf("перепись носителя: %s", strings.TrimSpace(log.String()))
		})
	}
}
