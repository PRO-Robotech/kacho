// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// diagnostics.go — cluster-internal diagnostic-листенер kacho-registry
// (/healthz, /readyz, /metrics), разведённые живость с готовностью и сканер
// состояния очереди регистраций.
//
// # Почему отдельный листенер
//
// Ни публичная gRPC-поверхность, ни data-plane для этого не годятся: первая
// tenant-facing, вторая принимает docker-клиентов снаружи, а внутренняя
// кардинальность (имена очередей, глубины, возраст головы) — инфра-информация,
// которой на этих поверхностях не место. Отдельный порт — тот же приём, что у
// остальных сервисов платформы.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	outboxmetrics "github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	"github.com/PRO-Robotech/kacho/services/registry/internal/observability/metrics"
)

// outboxMetricsInterval — период скана очереди. Совпадает с остальными сервисами
// платформы: одна величина у всех очередей, чтобы порог тревоги, написанный по
// одной, читался одинаково на любой.
const outboxMetricsInterval = 15 * time.Second

// diagnosticMux — маршруты diagnostic-листенера. Вынесен отдельно, чтобы то, ЧТО
// обслуживается, проверялось без сети: расхождение между объявленным в чарте
// скрейпом и реально обслуживаемым путём иначе замечается только на живом
// Prometheus.
//
// Живость и готовность отдаются РАЗНЫМИ обработчиками. Прежде `/healthz` был
// здесь один и отвечал безусловным 200; чарт при этом пробировал слот готовности
// открытым сокетом. Слушающий сокет открыт и у процесса, который отвергает
// каждый вызов, поэтому обещанного NotReady не наступало ни при каких условиях.
func diagnosticMux(m *metrics.Metrics, agg *health.Aggregator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", agg.LiveHandler())
	mux.Handle("GET /readyz", agg.ReadyHandler())
	mux.Handle("GET /metrics", m.Handler())
	return mux
}

// buildReadinessCheckers — ИМЕНОВАННЫЕ зависимости, без которых registry
// обслуживать не может. Живость их НЕ включает намеренно: блип зависимости
// обязан снять под из ротации, а не перезапустить процесс.
//
// # Почему канал к владельцу прав приезжает ОТЛОЖЕННО
//
// Диагностическая поверхность этого корня поднимается РАНО и намеренно: пока её
// нет, проба живости получает отказ соединения, и медленный старт читается
// kubelet'ом как мёртвый процесс. Соединение с владельцем прав набирается ниже
// по тексту корня, когда поверхность уже отвечает.
//
// Отсюда разделение: ОБЪЯВЛЯЕТ зависимость посадка (адрес известен из конфигурации
// с первой строки), а НОСИТЕЛЯ корень устанавливает позже. Пока носителя нет,
// [health.Slot] отвечает «не готов» — окно старта не зачитывается в готовность
// молча. Адрес не объявлен вовсе — чекера нет: держать под вечно неготовым по
// зависимости, которой на этой посадке не существует, значит выдавать посадку за
// поломку.
func buildReadinessCheckers(pool *pgxpool.Pool, authzDeclared bool, authzSlot *health.Slot) []health.Checker {
	checkers := []health.Checker{
		{Name: "database", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
	}
	if authzDeclared {
		checkers = append(checkers, authzSlot.Checker("iam-authz"))
	}
	return checkers
}

// authzConnHealth — состояние соединения с владельцем прав. Shutdown — отказ;
// прочие состояния (Idle/Connecting/Ready/TransientFailure) считаются рабочими:
// gRPC переподключается лениво, и снятие пода из ротации на кратком
// TransientFailure дало бы ложный флап на каждом перекате соседа.
func authzConnHealth(conn *grpc.ClientConn) error {
	if conn.GetState() == connectivity.Shutdown {
		return errIAMConnShutdown
	}
	return nil
}

// errIAMConnShutdown — причина «не готов» без раскрытия внутренних деталей наружу.
var errIAMConnShutdown = errors.New("connection to kacho-iam is shut down")

// Сроки диагностической поверхности. Названы константами, а не вписаны в
// объявление: они одинаковы у всех диагностических поверхностей платформы, и
// разъехавшиеся числа читались бы как осознанная разница.
const (
	diagReadHeaderBudget = 5 * time.Second
	diagRequestBudget    = 30 * time.Second
	diagIdleBudget       = 60 * time.Second
	diagShutdownBudget   = 5 * time.Second
)

// describeDiagnosticSurface — ОБЪЯВЛЕНИЕ diagnostic-поверхности (/healthz,
// /readyz, /metrics).
//
// Сервера, привязки порта и гашения здесь нет: их держит профиль не-gRPC
// поверхности (`pkg/servicehost.ServeSurface`). Прежнее предупреждение о том,
// что выключенная поверхность оставляет очередь регистраций без сканера
// состояния, никуда не делось — оно переехало в ПРИЧИНУ выключения, то есть
// стало частью объявления, а не отдельной строкой рядом с ним.
func describeDiagnosticSurface(endpoint string, m *metrics.Metrics, agg *health.Aggregator,
	mode servicecontract.Mode, logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_REGISTRY_METRICS_ADDR не задан профилем развёртывания. Цена названа здесь, " +
				"а не в отдельном предупреждении: очередь регистраций останется без сканера " +
				"состояния, застрявшая очередь будет молчать так же, как пустая, а kubelet " +
				"не узнает о неготовности зависимостей ничего")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-registry",
		Name:    "диагностика (/healthz, /readyz, /metrics)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: diagnosticMux(m, agg),

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только на внутренний Service. Именно " +
				"поэтому она и отдельная — внутренняя кардинальность очередей на публичную " +
				"gRPC-поверхность и на плоскость данных не выносится"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}

// runRegisterOutboxMetrics — периодический скан очереди регистраций: глубина,
// возраст самой старой недоставленной строки, число отравленных, и всё то же
// самое ПО НАПРАВЛЕНИЮ.
//
// Разложение здесь не украшение. Очередь несёт обе половины — постановку и
// снятие регистрации, — а реестры и репозитории создаются непрерывно, поэтому
// сводные величины остаются здоровыми при полностью мёртвом снятии: глубина
// мала, потому что выдачи дренятся; голова молода по той же причине. «Работает»
// и «ни разу не отозвано» дают одинаковую картину, и различает их только
// доставленное по направлению снятия.
//
// Скан не мутирует таблицу, не участвует в пути запроса и не может уронить под:
// ошибки логируются, цикл продолжается.
func runRegisterOutboxMetrics(ctx context.Context, pool *pgxpool.Pool, m *metrics.Metrics, logger *slog.Logger) {
	collector := outboxmetrics.NewCollector(pool, m, outboxmetrics.CollectorConfig{
		Table:      registerOutboxTable,
		Interval:   outboxMetricsInterval,
		Directions: outboxmetrics.RegisterOutboxDirections(),
	})
	collector.Run(ctx, func(err error) {
		logger.Warn("register outbox metrics scan failed", "table", registerOutboxTable, "err", err)
	})
}
