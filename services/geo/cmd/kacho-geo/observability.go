// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Observability-проводка composition root: ОБЪЯВЛЕНИЕ диагностической
// поверхности (/metrics). Поднимает и гасит её профиль не-gRPC поверхности
// (`pkg/servicehost.ServeSurface`) — здесь только данные. prometheus
// импортируется в adapter-пакете internal/observability/metrics (Clean
// Architecture). geo — leaf-сервис без register-outbox и без асинхронных
// операций, поэтому набор метрик ограничен восстановлением осиротевших LRO.

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
)

// build-info — инжектится через -ldflags "-X main.buildVersion=… -X main.buildCommit=…";
// дефолты для локальной сборки.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

// describeDiagnosticSurface — ОБЪЯВЛЕНИЕ диагностической поверхности.
//
// Сервера здесь нет, привязки порта нет, гашения нет: всё это принадлежит
// профилю. Корень отвечает ровно на четыре вопроса — что обслуживается, откуда
// досягаемо, чем аутентифицировано и на сколько времени рассчитан каждый срок.
//
// Пустой эндпоинт больше не «выключено молча»: он превращается в объявленное
// выключение с причиной, и причина едет в журнал. Различие не косметическое —
// профиль развёртывания, забывший задать адрес, и посадка без скрейпа выглядели
// одинаково.
func describeDiagnosticSurface(endpoint string, m *metrics.Metrics, mode servicecontract.Mode,
	logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())

	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_GEO_METRICS_ADDR не задан профилем развёртывания: скрейпа на этой посадке нет")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-geo",
		Name:    "диагностика (/metrics)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: mux,

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service и несёт " +
				"счётчики процесса — ни секретов, ни данных арендатора, ни сведений о размещении " +
				"на проводе нет (security.md §«Инфра-чувствительные данные»)"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}

// Сроки диагностической поверхности. Величины названы здесь, а не вписаны в
// объявление: они одинаковы у всех диагностических поверхностей платформы, и
// разъехавшиеся числа читались бы как осознанная разница.
const (
	// diagReadHeaderBudget — потолок чтения заголовка: медленный отправитель не
	// должен держать соединение, ничего не прислав.
	diagReadHeaderBudget = 5 * time.Second
	// diagRequestBudget — потолок чтения и записи запроса целиком. Сбор метрик
	// синхронный и короткий; потоковых ответов на этой поверхности нет.
	diagRequestBudget = 30 * time.Second
	// diagIdleBudget — потолок простоя keep-alive соединения скрейпа.
	diagIdleBudget = 60 * time.Second
	// diagShutdownBudget — срок гашения. Прежде гашение шло контекстом БЕЗ срока,
	// то есть процесс на остановке ждал последнего скрейпа неограниченно.
	diagShutdownBudget = 5 * time.Second
)
