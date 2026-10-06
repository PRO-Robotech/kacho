// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package health

// readyz_log_transition_test.go — журнал готовности пишет СМЕНУ состояния, а не
// каждую пробу (kacho#3034).
//
// Проба готовности ходит раз в 10 с. Строка на каждую пробу превращала
// устойчивое «бэкенд не готов» в 360 одинаковых строк в час на домен, и сама
// смена состояния в них терялась. Предикат задачи: 10 проб подряд в NOT_SERVING
// дают одну строку о переходе; возврат в SERVING — ещё одну; пока состояние
// держится, раз в окно — напоминание со счётчиком проб.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// backendStub is one domain backend whose health the test toggles.
func backendStub(t *testing.T) (*grpc.ClientConn, *grpchealth.Server) {
	t.Helper()
	lis := bufconn.Listen(1 << 16)
	srv := grpc.NewServer()
	hs := grpchealth.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, hs
}

// logLines is a concurrency-safe JSON log sink returning one map per line.
type logLines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logLines) records(t *testing.T) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(l.buf.Bytes()))
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		out = append(out, m)
	}
	return out
}

func probe(t *testing.T, h http.Handler) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

// The public constructor: steady NOT_SERVING over ten probes is one line, the
// way back is one line, steady SERVING is silent.
func TestReadyz_SteadyNotServingLogsTheTransitionOnce(t *testing.T) {
	conn, hs := backendStub(t)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	sink := &logLines{}
	h := HTTPReadyz(proxy.Backends{"operation": conn}, nil, slog.New(slog.NewJSONHandler(sink, nil)))

	for range 10 {
		probe(t, h)
	}
	recs := sink.records(t)
	require.Len(t, recs, 1, "10 probes in NOT_SERVING give one line: %v", recs)
	assert.Equal(t, "backend not serving", recs[0]["msg"])
	assert.Equal(t, "operation", recs[0]["domain"])

	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	for range 5 {
		probe(t, h)
	}
	recs = sink.records(t)
	require.Len(t, recs, 2, "the way back is one line, steady SERVING is silent: %v", recs)
	assert.Equal(t, "backend serving again", recs[1]["msg"])
	assert.Equal(t, "operation", recs[1]["domain"])
	assert.EqualValues(t, 10, recs[1]["not_serving_probes"], "the recovery line says how long the outage was in probes")
}

// Twin, one fact changed — the backend never leaves SERVING: no line at all.
func TestReadyz_SteadyServingIsSilent(t *testing.T) {
	conn, hs := backendStub(t)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	sink := &logLines{}
	h := HTTPReadyz(proxy.Backends{"operation": conn}, nil, slog.New(slog.NewJSONHandler(sink, nil)))
	for range 10 {
		probe(t, h)
	}
	assert.Empty(t, sink.records(t))
}

// While NOT_SERVING holds, one reminder per window carries the probe count
// since the previous line — the stand predicate (lines per hour per domain ≤
// windows per hour) is this property.
func TestReadyz_SteadyNotServingRemindsOncePerWindow(t *testing.T) {
	conn, hs := backendStub(t)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	sink := &logLines{}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	const window = 10 * time.Minute
	h := newReadyz(proxy.Backends{"operation": conn}, nil, slog.New(slog.NewJSONHandler(sink, nil)),
		func() time.Time { return now }, window)

	// One hour of probes, every 10 s: 360 probes.
	for range 360 {
		probe(t, h)
		now = now.Add(10 * time.Second)
	}
	recs := sink.records(t)
	require.Len(t, recs, 6, "one hour at a 10-minute window: the transition plus 5 reminders")
	for i, r := range recs {
		assert.Equal(t, "backend not serving", r["msg"], "line %d", i)
	}
	assert.Equal(t, true, recs[0]["state_change"])
	for i, r := range recs[1:] {
		assert.Equal(t, false, r["state_change"], "reminder %d", i+1)
		assert.EqualValues(t, 60, r["probes_since_last_line"], "reminder %d counts the probes it stood for", i+1)
	}
}
