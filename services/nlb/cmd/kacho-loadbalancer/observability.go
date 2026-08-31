// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Observability-проводка composition root: Prometheus diagnostic-listener
// (metrics + /healthz + /readyz), dependency-aware readiness, LRO-worker boot и
// супервизор фоновых goroutine. prometheus импортируется только в adapter-пакете
// internal/observability/metrics (Clean Architecture) — здесь лишь wiring.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/pkg/schemaguard"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/observability/metrics"
)

// Сентинелы readiness-чекеров: причина «down» в логах/ответе /readyz без leak'а
// внутренних деталей наружу (имена зависимостей — operational, cluster-internal).
var (
	errDrainerNotConnected = errors.New("register-drainer not connected to kacho-iam")
	errLROWorkerDown       = errors.New("LRO dispatcher loop not running")
)

// build-info — инжектится через -ldflags "-X main.buildVersion=… -X main.buildCommit=…";
// дефолты для локальной сборки.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

// readinessPinger — узкий DB-порт readiness (Ping). *pgxpool.Pool его
// удовлетворяет; тесты подставляют фейк.
type readinessPinger interface {
	Ping(ctx context.Context) error
}

// startLROWorker подключает Prometheus-Recorder и логгер к package-level
// default-registry LRO-worker'а (ConfigureDefault) и поднимает его dispatcher-loop
// (Start) ДО приёма трафика. Решает два дефекта boot'а:
//   - readiness-deadlock: без явного Start dispatcher стартует лениво на первом
//     Run, но под в NotReady трафика не получает → Run не происходит → вечный
//     NotReady. Явный Start делает Ready=true до трафика;
//   - dead live-worker метрики: default-registry создаётся с NopRecorder, поэтому
//     terminal-write retries/failures и inflight gauge от ЖИВОГО worker-пути не
//     эмитились. WithRecorder подключает их к /metrics.
//
// ConfigureDefault обязан предшествовать Start; вызывается один раз из composition
// root (повторный вызов после старта вернул бы ErrWorkerStarted).
func startLROWorker(rec operations.Recorder, logger *slog.Logger) error {
	if err := operations.ConfigureDefault(operations.WithRecorder(rec), operations.WithLogger(logger)); err != nil {
		return fmt.Errorf("configure LRO default-registry: %w", err)
	}
	operations.Start()
	return nil
}

// buildReadinessCheckers собирает чекеры критичных зависимостей для readiness.
// liveness намеренно НЕ включает их (защита от restart-storm).
//
//   - database — pgxpool.Ping;
//   - register-drainer — bootGate.Ready: в nlb register-drainer держит conn к
//     iam-internal (9091, тот же conn несёт InternalIAMService.Check), поэтому
//     этот чекер — и сигнал «IAM-достижим». При require-iam=false gate всегда
//     Ready (dev back-compat);
//   - lro-worker — operations.Ready: dispatcher-loop запущен и готов забирать
//     in-flight операции.
//
// ВЕРСИЯ СХЕМЫ — ОТДЕЛЬНАЯ ИМЕНОВАННАЯ ЗАВИСИМОСТЬ, а не часть проверки базы.
// Мигратор идёт при каждом раскате, поэтому откат выкатки ставит ПРЕЖНИЙ образ
// на НОВУЮ схему; база при этом отвечает на `Ping`, и без этого чекера под
// объявлялся бы готовым и получал трафик (`pkg/schemaguard`, задача #1734).
// Отдельное имя обязательно: оператор обязан отличить «база недоступна» от
// «образ не той версии, что схема», не читая кода.
func buildReadinessCheckers(db readinessPinger, gate *bootgate.Gate,
	schemaCheck func(context.Context) error) []health.Checker {
	return []health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return db.Ping(ctx) }},
		{Name: schemaguard.CheckerName, Check: schemaCheck},
		{Name: "register-drainer", Check: func(context.Context) error {
			if gate.Ready() {
				return nil
			}
			return errDrainerNotConnected
		}},
		{Name: "lro-worker", Check: func(context.Context) error {
			if operations.Ready() {
				return nil
			}
			return errLROWorkerDown
		}},
	}
}

// superviseBackground оборачивает долгоживущий фоновый loop так, что его
// НЕОЖИДАННЫЙ возврат (loop вышел, пока ctx ещё жив) флипает readiness и
// триггерит graceful-shutdown. Возврат после отмены ctx — штатный путь (nil,
// onUnexpectedExit не вызывается). Убирает fire-and-forget семантику фоновых
// goroutine.
func superviseBackground(ctx context.Context, name string, run func(context.Context) error, onUnexpectedExit func(), logger *slog.Logger) error {
	err := run(ctx)
	if ctx.Err() != nil {
		// ctx отменён (SIGTERM / shutdown-триггер) — это штатное завершение.
		return nil
	}
	logger.Error("background task exited unexpectedly", "task", name, "err", err)
	if onUnexpectedExit != nil {
		onUnexpectedExit()
	}
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s exited unexpectedly", name)
}

// Сроки диагностической поверхности. Названы константами, а не вписаны в
// объявление: они одинаковы у всех диагностических поверхностей платформы, и
// разъехавшиеся числа читались бы как осознанная разница.
const (
	diagReadHeaderBudget = 5 * time.Second
	diagRequestBudget    = 30 * time.Second
	diagIdleBudget       = 60 * time.Second
	diagShutdownBudget   = 5 * time.Second
)

// describeDiagnosticSurface — ОБЪЯВЛЕНИЕ cluster-internal диагностической
// поверхности (/metrics, /healthz, /readyz).
//
// Сервера, привязки порта и гашения здесь нет: их держит профиль не-gRPC
// поверхности (`pkg/servicehost.ServeSurface`). Корень отвечает на четыре
// вопроса — что обслуживается, откуда досягаемо, чем аутентифицировано и на
// сколько рассчитан каждый срок.
//
// Пустой эндпоинт перестал выключать поверхность МОЛЧА: теперь это объявленное
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
			"KACHO_NLB_METRICS_ADDR не задан профилем развёртывания: ни скрейпа, ни проб " +
				"живости и готовности на этой посадке нет — kubelet не узнает о неготовности " +
				"зависимостей ничего")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-nlb",
		Name:    "диагностика (/metrics, /healthz, /readyz)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: mux,

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service, её читают " +
				"скрейп и kubelet. На проводе счётчики процесса и имена зависимостей — ни " +
				"секретов, ни данных арендатора, ни сведений о размещении " +
				"(security.md §«Инфра-чувствительные данные»)"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}
