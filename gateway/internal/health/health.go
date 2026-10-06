// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// Server реализует grpc.health.v1.Health для самого gateway.
// Встроен в gRPC-сервер, чтобы отвечать на gRPC Health.Check (сценарий G5).
type Server struct {
	healthpb.UnimplementedHealthServer
	backends proxy.Backends
}

// NewServer создает health-сервер для gateway.
func NewServer(backends proxy.Backends) *Server {
	return &Server{backends: backends}
}

// Check реализует grpc.health.v1.Health/Check.
// Проверяет статус самого gateway (не backends — это задача /readyz).
func (s *Server) Check(_ context.Context, _ *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

// statusResponse — тело JSON-ответа для /healthz и /readyz.
type statusResponse struct {
	Status   string            `json:"status"`
	Backends map[string]string `json:"backends,omitempty"`
}

// HTTPHealthz обрабатывает GET /healthz.
// Всегда возвращает 200 — liveness не зависит от состояния backends (сценарии G1, G4).
func HTTPHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(statusResponse{Status: "ok"})
}

// HTTPReadyz обрабатывает GET /readyz. Опрашивает grpc.health.v1.Health.Check у
// каждого backend и решает готовность по критичным зависимостям: 503 только если
// недоступен CRITICAL-backend (iam фронтит authN+authZ на каждом запросе);
// падение НЕкритичного backend (vpc/compute/geo/nlb) — деградация одного домена,
// реплика остается Ready (иначе одно-доменный сбой амплифицируется в полный
// отказ edge). Тело ответа всегда содержит per-backend статус для диагностики.
//
// critical — множество domain-ключей, чья недоступность валит готовность. nil/
// пустое → готовность зависит только от собственной способности обслуживать
// (всегда 200, backends лишь отражаются в теле).
func HTTPReadyz(backends proxy.Backends, critical map[string]bool, logger *slog.Logger) http.HandlerFunc {
	return newReadyz(backends, critical, logger, time.Now, notServingReminderWindow)
}

// notServingReminderWindow — как часто держащееся «бэкенд не готов» напоминает о
// себе строкой журнала. Проба готовности ходит раз в 10 с, и строка на каждую
// пробу давала 360 одинаковых строк в час на домен (kacho#3034); окно в 10 минут
// оставляет 6 строк в час — состояние видно в журнале любого получаса, а смена
// состояния не тонет в повторах. Ручкой не вынесено: величина не меняет ни
// готовности, ни ответа, только плотность журнала.
const notServingReminderWindow = 10 * time.Minute

func newReadyz(backends proxy.Backends, critical map[string]bool, logger *slog.Logger, now func() time.Time, window time.Duration) http.HandlerFunc {
	journal := newServingJournal(logger, now, window)
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		serving := make(map[string]bool, len(backends))
		for domain, conn := range backends {
			client := healthpb.NewHealthClient(conn)
			resp, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
			ok := err == nil && resp.Status == healthpb.HealthCheckResponse_SERVING
			serving[domain] = ok
			journal.observe(domain, ok, err, critical[domain])
		}

		backendStatus, criticalDown := EvaluateReadiness(serving, critical)

		w.Header().Set("Content-Type", "application/json")
		if criticalDown {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(statusResponse{Status: "NOT_SERVING", Backends: backendStatus})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(statusResponse{Status: "ok", Backends: backendStatus})
	}
}

// servingJournal пишет в журнал СМЕНУ состояния домена, а не каждую пробу.
//
// Исходное состояние домена — «обслуживает»: первая проба в SERVING молчит,
// первая в NOT_SERVING пишет переход. Пока NOT_SERVING держится, раз в окно
// пишется напоминание ТЕМ ЖЕ текстом (`state_change=false`) со счётчиком проб,
// за которые оно стоит, — так счёт строк `backend not serving` в час на домен
// ограничен числом окон в часе плюс переходы. Возврат в SERVING — одна строка
// уровня Info с числом проб, проведённых в NOT_SERVING.
//
// Пробы готовности могут идти конкурентно (несколько опрашивающих), поэтому
// состояние под мьютексом; вызов журнала — тоже под ним, чтобы порядок строк
// совпадал с порядком переходов.
type servingJournal struct {
	logger *slog.Logger
	now    func() time.Time
	window time.Duration

	mu      sync.Mutex
	domains map[string]*domainServing
}

type domainServing struct {
	notServing bool
	// outageProbes — проб в NOT_SERVING с последнего перехода в него.
	outageProbes int
	// sinceLine — проб в NOT_SERVING с последней записанной строки.
	sinceLine int
	since     time.Time // начало текущего NOT_SERVING
	lastLine  time.Time
}

func newServingJournal(logger *slog.Logger, now func() time.Time, window time.Duration) *servingJournal {
	return &servingJournal{logger: logger, now: now, window: window, domains: map[string]*domainServing{}}
}

func (j *servingJournal) observe(domain string, ok bool, err error, critical bool) {
	if j.logger == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	d := j.domains[domain]
	if d == nil {
		d = &domainServing{}
		j.domains[domain] = d
	}
	switch {
	case ok && !d.notServing:
		return
	case ok && d.notServing:
		j.logger.Info("backend serving again", "domain", domain, "critical", critical,
			"not_serving_probes", d.outageProbes)
		*d = domainServing{}
	case !ok && !d.notServing:
		t := j.now()
		*d = domainServing{notServing: true, outageProbes: 1, since: t, lastLine: t}
		j.logger.Warn("backend not serving", "domain", domain, "error", err, "critical", critical,
			"state_change", true)
	default: // NOT_SERVING holds
		d.outageProbes++
		d.sinceLine++
		t := j.now()
		if t.Sub(d.lastLine) < j.window {
			return
		}
		j.logger.Warn("backend not serving", "domain", domain, "error", err, "critical", critical,
			"state_change", false, "probes_since_last_line", d.sinceLine,
			"not_serving_for", t.Sub(d.since).String())
		d.sinceLine = 0
		d.lastLine = t
	}
}

// EvaluateReadiness — чистое readiness-решение поверх карты «domain → serving».
// Возвращает per-backend статус-строки и флаг criticalDown=true, если хотя бы
// один CRITICAL-backend не обслуживается. Вынесена отдельно, чтобы политику
// готовности можно было проверить без поднятия gRPC.
func EvaluateReadiness(serving map[string]bool, critical map[string]bool) (status map[string]string, criticalDown bool) {
	status = make(map[string]string, len(serving))
	for domain, ok := range serving {
		if ok {
			status[domain] = "SERVING"
			continue
		}
		status[domain] = "NOT_SERVING"
		if critical[domain] {
			criticalDown = true
		}
	}
	return status, criticalDown
}

// RegisterGRPCHealth регистрирует Health-сервер в gRPC-сервере.
func RegisterGRPCHealth(s *grpc.Server, backends proxy.Backends) {
	healthpb.RegisterHealthServer(s, NewServer(backends))
}
