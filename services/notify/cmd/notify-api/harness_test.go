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
//	serveAPI(ctx, apiInputs) error — поднимает единственный внутренний слушатель
//	    носителя Х5 (форма «только внутренний слушатель») с цепочкой звеньев
//	    личности (пара CertIdentityExtract → TrustedPrincipalExtract с кругом
//	    пересылающих TrustedForwarderSANs) и прав (authz.Interceptor по
//	    аннотациям каталога), регистрирует InternalNoticeService, NoticeService и
//	    OperationService; возвращается по отмене ctx (nil) либо с ошибкой носителя.
//	apiInputs — зависимости корня: адрес и удостоверение слушателя, домен доверия
//	    и круг пересылающих, пул kacho_notify, часы notify (они же часы сужателя,
//	    З14 п.1 — `authzwiring.NewListNarrower(cli, cfg, now)`), адрес и
//	    удостоверение ребра к службе доступа (Check звена прав и use-case,
//	    пакетная проверка сужателя — больше корню notify-api не дано ничего,
//	    З1; соединение набирают носитель и корень, поэтому подаётся ребро, а не
//	    готовое соединение), посадка (режим, sslmode) и величины звена прав
//	    (окно, срок вопроса, бюджет отказов, граница обработки — ручки NTF-4
//	    Р20), ручки KACHO_NOTIFY_NOTICE_REMINDER_LEAD и
//	    KACHO_NOTIFY_LIST_FILTER_CACHE_TTL, реестр метрик, журнал.
//
// Поля apiInputs — то, что оснастка ПОДАЁТ; их имена — предмет этого файла и
// правятся полосой реализации здесь и только здесь, вместе с её формой корня.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/servicecontract"
)

// apiKnobs — ручки notify-api, которые задаёт «Дано».
type apiKnobs struct {
	reminderLead  time.Duration
	listFilterTTL time.Duration
}

// g0Knobs — ручки G0 (§6): REMINDER_LEAD=24h, LIST_FILTER_CACHE_TTL=5s.
var g0Knobs = apiKnobs{reminderLead: 24 * time.Hour, listFilterTTL: 5 * time.Second}

// raise поднимает notify-api над частями оснастки w и отдаёт клиента края.
func raise(t *testing.T, w *world, k apiKnobs) edge {
	t.Helper()
	addr := "127.0.0.1:" + freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	var serveErr error
	stopped := make(chan struct{})
	in := apiInputs{
		ListenAddr:           addr,
		ServerTLS:            w.ca.serverFiles(t, "notify-api", apiSAN),
		TrustDomain:          trustDomain,
		TrustedForwarderSANs: []string{gatewaySAN},
		Mode:                 servicecontract.ModeDev,
		DBSSLMode:            "disable",
		Pool:                 w.pool,
		Now:                  w.clock.Now,
		KanameAddr:           w.path.addr(),
		KanameCreds:          w.kanameCreds(t),
		AuthzCacheTTL:        5 * time.Second,
		AuthzCheckTimeout:    2 * time.Second,
		AuthzDenyBudget:      100,
		HandlingBudget:       30 * time.Second,
		ReminderLead:         k.reminderLead,
		ListFilterCacheTTL:   k.listFilterTTL,
		Metrics:              prometheus.NewRegistry(),
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() {
		defer close(stopped)
		serveErr = serveAPI(ctx, in)
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
