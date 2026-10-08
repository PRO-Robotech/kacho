// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// harness_test.go — провязка оснастки к испытуемому. ЕДИНСТВЕННЫЙ файл
// оснастки, который называет испытуемого; fixture_test.go и
// fixture_selfcheck_test.go собираются и исполняются без него (положительный
// контроль оснастки идёт ПЕРВЫМ, `go test -overlay`, где этот файл снят).
//
// # Контракт испытуемого, который утверждают пробы
//
// Корень развёртывания `notify-api` (замысел issue-2924 З1, З14, З16, З17;
// приёмка NTF-5 Р2, Р16, Р18) — пакет main каталога services/notify/cmd/notify-api:
//
//	serveAPI(ctx, config.Config, apiRuntime) error — поднимает единственный
//	    внутренний слушатель носителя Х5 (форма «только внутренний слушатель») с
//	    цепочкой звеньев личности (пара CertIdentityExtract → TrustedPrincipalExtract
//	    с кругом пересылающих) и прав (authz.Interceptor по аннотациям каталога),
//	    регистрирует InternalNoticeService, NoticeService и OperationService;
//	    возвращается по отмене ctx (nil) либо с ошибкой носителя.
//	config.Config — значения ручек notify-api в том виде, в каком их отдаёт загрузчик:
//	    порт и удостоверение слушателя, домен доверия и круг пересылающих, адрес и
//	    клиентское удостоверение ребра к службе доступа (Check звена прав и
//	    use-case, пакетная проверка сужателя — больше корню notify-api не дано
//	    ничего, З1), посадка, величины звена прав (ручки NTF-4 Р20),
//	    KACHO_NOTIFY_NOTICE_REMINDER_LEAD и KACHO_NOTIFY_LIST_FILTER_CACHE_TTL.
//	apiRuntime — не ручки: пул kacho_notify, часы notify (они же часы сужателя,
//	    З14 п.1 — `authzwiring.NewListNarrower(cli, cfg, now)`), реестр метрик,
//	    журнал.
//
// Поля, которые оснастка ПОДАЁТ, — предмет этого файла и правятся полосой
// реализации здесь и только здесь, вместе с её формой корня.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/config"
)

// apiKnobs — ручки notify-api, которые задаёт «Дано».
type apiKnobs struct {
	reminderLead  time.Duration
	listFilterTTL time.Duration
}

// g0Knobs — ручки G0 (§6): REMINDER_LEAD=24h, LIST_FILTER_CACHE_TTL=5s.
var g0Knobs = apiKnobs{reminderLead: 24 * time.Hour, listFilterTTL: 5 * time.Second}

// raise поднимает notify-api над частями оснастки w и отдаёт клиента края.
// Ручки — значения типа конфигурации notify-api, как их отдал бы загрузчик;
// посадка — dev (боевую судит страж main, а не носитель), база — pgtest без TLS.
func raise(t *testing.T, w *world, k apiKnobs) edge {
	t.Helper()
	port := freePort(t)
	addr := "127.0.0.1:" + port
	ctx, cancel := context.WithCancel(context.Background())
	var serveErr error
	stopped := make(chan struct{})
	srv := w.ca.serverFiles(t, "notify-api", apiSAN)
	peerCert, peerKey := w.ca.issue(t, "notify-api-client", false, apiSAN)
	cfg := config.Config{
		AuthMode:                    "dev",
		PeerTLSCertFile:             peerCert,
		PeerTLSKeyFile:              peerKey,
		PeerTLSCAFile:               w.ca.caFile(),
		DBSSLMode:                   "disable",
		AuthzIAMGRPCAddr:            w.path.addr(),
		InternalPort:                port,
		InternalServerCertFile:      srv.CertFile,
		InternalServerKeyFile:       srv.KeyFile,
		InternalServerClientCAFiles: srv.ClientCAFiles,
		AuthzTrustDomain:            trustDomain,
		AuthzTrustedForwarderSANs:   []string{gatewaySAN},
		AuthzCacheTTL:               5 * time.Second,
		AuthzCheckTimeout:           2 * time.Second,
		AuthzDenyBudgetPerSec:       100,
		HandlingBudget:              30 * time.Second,
		NoticeReminderLead:          k.reminderLead,
		ListFilterCacheTTL:          k.listFilterTTL,
	}
	rt := apiRuntime{
		Pool:    w.pool,
		Now:     w.clock.Now,
		Metrics: prometheus.NewRegistry(),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() {
		defer close(stopped)
		serveErr = serveAPI(ctx, cfg, rt)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
		if serveErr != nil {
			t.Errorf("носитель notify-api вернул ошибку: %v", serveErr)
		}
	})
	waitListening(t, addr, stopped, func() error { return serveErr })
	return dialEdge(t, w.ca, addr)
}
