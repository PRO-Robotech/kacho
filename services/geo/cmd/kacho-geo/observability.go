// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Observability-проводка composition root: ОБЪЯВЛЕНИЕ диагностической
// поверхности (/metrics, /healthz, /readyz) и разведённые живость с готовностью.
// Поднимает и гасит поверхность профиль не-gRPC поверхности
// (`pkg/servicehost.ServeSurface`) — здесь только данные. prometheus
// импортируется в adapter-пакете internal/observability/metrics (Clean
// Architecture). geo — leaf-сервис без register-outbox и без асинхронных
// операций, поэтому набор метрик ограничен восстановлением осиротевших LRO.

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/pkg/schemaguard"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
)

// buildReadinessCheckers — ИМЕНОВАННЫЕ зависимости, без которых geo обслуживать
// не может. Живость их НЕ включает намеренно: блип зависимости обязан снять под
// из ротации, а не перезапустить процесс.
//
// Зависимость здесь ровно одна, и это не недоделка. geo — лист графа: своих
// соседей на пути запроса он не зовёт, асинхронного исполнителя операций не
// поднимает (мутации каталога завершаются синхронно, `operations.Start` в этом
// корне не вызывается), очереди регистраций у него нет. Единственное, без чего
// он не отвечает, — собственная база. Перечислить здесь больше значило бы
// объявить зависимости, которых нет, и получить готовность, отражающую не
// сервис, а наше представление о нём.
//
// Ребро решения о доступе в перечень не входит по другой причине: соединение с
// владельцем прав держит носитель контура, корню оно не выдаётся. Пока
// носитель не отдаёт его наружу, чекер на нём был бы написан по догадке.
// ВЕРСИЯ СХЕМЫ — ОТДЕЛЬНАЯ ИМЕНОВАННАЯ ЗАВИСИМОСТЬ, а не часть проверки базы.
// Мигратор идёт при каждом раскате, поэтому откат выкатки ставит ПРЕЖНИЙ образ
// на НОВУЮ схему; база при этом отвечает на `Ping`, и без этого чекера под
// объявлялся бы готовым и получал трафик (`pkg/schemaguard`, задача #1734).
// Отдельное имя обязательно: оператор обязан отличить «база недоступна» от
// «образ не той версии, что схема», не читая кода.
func buildReadinessCheckers(pool *pgxpool.Pool, schemaCheck func(context.Context) error) []health.Checker {
	return []health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
		{Name: schemaguard.CheckerName, Check: schemaCheck},
	}
}

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
func describeDiagnosticSurface(endpoint string, m *metrics.Metrics, agg *health.Aggregator,
	mode servicecontract.Mode, logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())
	mux.Handle("GET /healthz", agg.LiveHandler())
	mux.Handle("GET /readyz", agg.ReadyHandler())

	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_GEO_METRICS_ADDR не задан профилем развёртывания: ни скрейпа, ни проб " +
				"живости и готовности на этой посадке нет — kubelet не узнает о неготовности " +
				"базы ничего")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-geo",
		Name:    "диагностика (/metrics, /healthz, /readyz)",
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
