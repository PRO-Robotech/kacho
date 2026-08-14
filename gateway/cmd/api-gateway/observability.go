// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Провязка наблюдаемости композиционного корня края: cluster-internal
// диагностическая поверхность (`GET /metrics`). Клиент Prometheus импортируется
// только в адаптере `gateway/internal/observability/metrics` — здесь лишь
// объявление и подъём.

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/servicecontract"

	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// Сведения о сборке — инжектируются через `-ldflags "-X main.buildVersion=… -X
// main.buildCommit=…"`; умолчания для локальной сборки. Ровно та же форма, что у
// семи остальных процессов платформы.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

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
// поверхности края (`GET /metrics`).
//
// Сервера, привязки порта и гашения здесь нет: их держит профиль не-gRPC
// поверхности (`pkg/servicehost.ServeSurface`). Корень отвечает на четыре
// вопроса — что обслуживается, откуда досягаемо, чем аутентифицировано и на
// сколько рассчитан каждый срок.
//
// # Почему обработчик экспозиции стоит ТОЛЬКО здесь
//
// Публичный слушатель края обслуживает трафик арендаторов и досягаем снаружи
// кластера; счётчики процесса на нём — это сведения об инфраструктуре на
// арендаторской поверхности (`security.md` §«Инфра-чувствительные данные»).
// Провязка того же обработчика на второй слушатель роняет гейт дерева
// `TestMetricsHandlerIsMountedOnTheDiagnosticSurfaceOnly`.
//
// # Почему аутентификации нет, и почему это ОБЪЯВЛЕНО
//
// Задокументированное исключение `security.md` §AuthN+AuthZ ВЕЗДЕ: поверхность
// выставлена только внутрь кластера, на проводе — счётчики процесса, ни
// секретов, ни данных арендатора, ни сведений о размещении. Объявление с
// причиной отличает снятую осознанно аутентификацию от забытой: снятие требует
// слов, забытое слов не имеет.
func describeDiagnosticSurface(endpoint string, m *gwmetrics.Metrics,
	mode servicecontract.Mode, logger *slog.Logger) (servicecontract.SurfaceDescriptor, error) {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())

	addr := servicecontract.Value(endpoint)
	if endpoint == "" {
		addr = servicecontract.NotApplicable[string](
			"KACHO_API_GATEWAY_METRICS_ADDR объявлен профилем развёртывания ПУСТЫМ: сбора " +
				"величин на этой посадке нет — решения о доступе, принятые краем, остаются " +
				"ненаблюдаемыми, и «ноль отказов» будет неотличим от «край не спрашивали»")
	}
	return servicecontract.NewSurface(servicecontract.Surface{
		Service: "kacho-api-gateway",
		Name:    "диагностика (/metrics)",
		Mode:    mode,
		Logger:  logger,

		Addr:    addr,
		Handler: mux,

		Reach: servicecontract.ReachClusterInternal,
		Auth: servicecontract.NotApplicable[servicecontract.SurfaceAuthMech](
			"снята осознанно: поверхность выставлена только внутрь кластера, её читает сбор " +
				"величин. На проводе счётчики процесса — ни секретов, ни данных арендатора, ни " +
				"сведений о размещении (security.md §«Инфра-чувствительные данные»)"),

		ReadHeaderBudget: diagReadHeaderBudget,
		RequestBudget:    servicecontract.Value(diagRequestBudget),
		IdleBudget:       diagIdleBudget,
		ShutdownBudget:   diagShutdownBudget,
	})
}

// surfaceMode — посадка, которую край объявляет о себе диагностической
// поверхности.
//
// Выводится из метки режима аутентификации, а не из отдельной ручки: вторая
// ручка о той же посадке разъехалась бы с первой, и самоотчёт процесса начал бы
// называть посадку, в которой он не работает. Неизвестная метка — БОЕВАЯ: тот же
// fail-closed, что у загрузочного стража края (`authz_validation.go`), где
// пустая метка среды тоже считается боевой.
func surfaceMode(authnMode string) servicecontract.Mode {
	mode, err := servicecontract.ParseMode(authnMode)
	if err != nil {
		return servicecontract.ModeProduction
	}
	return mode
}
