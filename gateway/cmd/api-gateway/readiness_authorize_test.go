// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// Готовность края и КЛИЕНТ РЕШЕНИЯ О ПРАВЕ (kacho#3200).
//
// Решение о праве край спрашивает своим соединением (`NewIAMAuthorizeClient`),
// а не теми, что лежат в карте бэкендов. `/readyz` опрашивал только карту:
// после переката службы прав соединения `iam` и `iamInternal` уже обслуживали,
// а соединение решения ещё нет — край отвечал «готов» и на первый вопрос о праве
// отдавал 503 «решение не принято». Пробы стоят на проводе: соединение решения —
// настоящее gRPC-соединение к серверу, который не отвечает, а не флажок.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/health"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// readinessAuthorizeTarget — имя цели, под которым соединение решения
// дожидается ответа в теле; адреса в теле нет.
const readinessAuthorizeTarget = "iamAuthorize"

// healthyConn — соединение к серверу, чья служба здоровья отвечает SERVING.
func healthyConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	// Умолчание службы здоровья — SERVING.
	healthpb.RegisterHealthServer(srv, grpchealth.NewServer())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return bufConn(t, lis)
}

// deadConn — соединение к адресу, за которым никто не принимает: так выглядит
// соединение решения, пока под службы прав сменяется.
func deadConn(t *testing.T) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	_ = lis.Close()
	return bufConn(t, lis)
}

func bufConn(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///authorize-target",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("соединение: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func authorizeClientOver(conn *grpc.ClientConn) *clients.IAMAuthorizeClient {
	return clients.NewIAMAuthorizeClientFromConn(conn, slog.New(slog.NewTextHandler(io.Discard, nil)), 200*time.Millisecond, 1)
}

type readinessAnswer struct {
	code int
	raw  string
	body struct {
		Status   string            `json:"status"`
		Backends map[string]string `json:"backends"`
	}
}

// askReadyz собирает `/readyz` ТЕМ ЖЕ выводом целей, что композиционный корень.
func askReadyz(t *testing.T, authorize *clients.IAMAuthorizeClient, failOpen bool) readinessAnswer {
	t.Helper()
	backends := proxy.Backends{"iam": healthyConn(t), "iamInternal": healthyConn(t)}
	targets, critical := readinessTargets(backends, authorize, failOpen)
	rec := httptest.NewRecorder()
	health.HTTPReadyz(targets, critical, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var a readinessAnswer
	a.code = rec.Code
	a.raw = rec.Body.String()
	if err := json.Unmarshal(rec.Body.Bytes(), &a.body); err != nil {
		t.Fatalf("тело /readyz не JSON: %v\n%s", err, a.raw)
	}
	return a
}

// TestReadyzIsFalseWhileTheAuthorizeClientIsNotServing — предикат kacho#3200:
// соединения карты обслуживают, соединение решения о праве — нет; край обязан
// ответить 503 и назвать цель решения NOT_SERVING.
func TestReadyzIsFalseWhileTheAuthorizeClientIsNotServing(t *testing.T) {
	a := askReadyz(t, authorizeClientOver(deadConn(t)), false)

	if a.code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz при неготовом клиенте решения о праве: код %d, ожидался 503; тело %s", a.code, a.raw)
	}
	if got := a.body.Backends[readinessAuthorizeTarget]; got != "NOT_SERVING" {
		t.Fatalf("цель %q в теле: %q, ожидался NOT_SERVING; тело %s", readinessAuthorizeTarget, got, a.raw)
	}
	for _, d := range []string{"iam", "iamInternal"} {
		if got := a.body.Backends[d]; got != "SERVING" {
			t.Fatalf("красное обязано держаться одним фактом: %q = %q, ожидался SERVING; тело %s", d, got, a.raw)
		}
	}
	if strings.Contains(a.raw, "authorize-target") {
		t.Fatalf("тело /readyz называет адрес соединения: %s", a.raw)
	}
}

// TestReadyzIsTrueWhenTheAuthorizeClientServes — законный близнец: тот же
// вывод целей, соединение решения обслуживает — край готов.
func TestReadyzIsTrueWhenTheAuthorizeClientServes(t *testing.T) {
	a := askReadyz(t, authorizeClientOver(healthyConn(t)), false)

	if a.code != http.StatusOK {
		t.Fatalf("/readyz при готовом клиенте решения: код %d, ожидался 200; тело %s", a.code, a.raw)
	}
	if got := a.body.Backends[readinessAuthorizeTarget]; got != "SERVING" {
		t.Fatalf("цель %q в теле: %q, ожидался SERVING; тело %s", readinessAuthorizeTarget, got, a.raw)
	}
}

// TestReadyzUnderFailOpenShowsTheAuthorizeClientWithoutFailing — при открытом
// исходе неотвеченный вопрос о праве не даёт 503, поэтому и готовность от
// соединения решения не зависит; цель всё равно видна в теле.
func TestReadyzUnderFailOpenShowsTheAuthorizeClientWithoutFailing(t *testing.T) {
	a := askReadyz(t, authorizeClientOver(deadConn(t)), true)

	if a.code != http.StatusOK {
		t.Fatalf("/readyz при открытом исходе: код %d, ожидался 200; тело %s", a.code, a.raw)
	}
	if got := a.body.Backends[readinessAuthorizeTarget]; got != "NOT_SERVING" {
		t.Fatalf("цель %q в теле: %q, ожидался NOT_SERVING; тело %s", readinessAuthorizeTarget, got, a.raw)
	}
}

// TestReadyzWithoutAuthorizeClientHasNoAuthorizeTarget — проверка прав
// выключена, клиента нет: цели нет, готовность — по карте.
func TestReadyzWithoutAuthorizeClientHasNoAuthorizeTarget(t *testing.T) {
	a := askReadyz(t, nil, false)

	if a.code != http.StatusOK {
		t.Fatalf("/readyz без клиента решения: код %d, ожидался 200; тело %s", a.code, a.raw)
	}
	if _, ok := a.body.Backends[readinessAuthorizeTarget]; ok {
		t.Fatalf("цель %q без клиента решения: тело %s", readinessAuthorizeTarget, a.raw)
	}
}
