// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package health_test

// Готовность края опрашивает `Health/Check` у каждого бэкенда. Бэкенд платформы
// собран конструктором фундамента (`grpcsrv.NewServer`) со звеном решения о
// доступе (`authz.Interceptor`) в цепочке, а карта прав сервиса выводится из
// аннотаций доменного контракта — записи о службе здоровья в ней нет by
// construction. До corelib v1.11.0 звено отвергало такую пробу как
// неразмеченную (PermissionDenied), и край числил здоровый бэкенд NOT_SERVING
// (kacho#3032). Пробы стоят на проводе: предмет — «бэкенд, собранный
// фундаментом, глазами готовности края», а не прямой вызов звена.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/grpcsrv"

	"github.com/PRO-Robotech/kacho/gateway/internal/health"
	"github.com/PRO-Robotech/kacho/gateway/internal/proxy"
)

// lockedBuffer — журнал, в который пишут обработчики из своих горутин.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// foundationBackend поднимает бэкенд конструктором фундамента со звеном решения
// о доступе в обеих цепочках и возвращает соединение края к нему. checks
// считает вопросы к модели прав; allow — её ответ.
func foundationBackend(t *testing.T, m authz.RPCMap, allow bool, checks *int) *grpc.ClientConn {
	t.Helper()
	var mu sync.Mutex
	intr := authz.NewInterceptor(authz.InterceptorOptions{
		Cache: authz.NewCache(0),
		Map:   m,
		Client: authz.CheckClientFunc(func(context.Context, string, string, string) (bool, error) {
			mu.Lock()
			*checks++
			mu.Unlock()
			return allow, nil
		}),
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	})
	lis := bufconn.Listen(1 << 20)
	srv := grpcsrv.NewServer(
		grpc.ChainUnaryInterceptor(intr.Unary()),
		grpc.ChainStreamInterceptor(intr.Stream()),
	)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("соединение края с бэкендом: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// domainMap — карта прав, какой её выводит доменный контракт: один доменный
// метод и ни одной записи о службе здоровья.
func domainMap() authz.RPCMap {
	return authz.RPCMap{
		"/kacho.cloud.vpc.v1.NetworkService/Get": authz.RPCEntry{
			Relation: "viewer",
			Extract: authz.StaticExtractor("project", func(any) (string, error) {
				return "prj_x", nil
			}),
		},
	}
}

type readyzBody struct {
	Status   string            `json:"status"`
	Backends map[string]string `json:"backends"`
}

func probeReadyz(t *testing.T, backends proxy.Backends, critical map[string]bool, logs *lockedBuffer) (int, readyzBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	health.HTTPReadyz(backends, critical, slog.New(slog.NewTextHandler(logs, nil))).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body readyzBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело /readyz не JSON: %v\n%s", err, rec.Body.String())
	}
	return rec.Code, body
}

// TestReadyzSeesAFoundationBackendWithoutAHealthEntryAsServing — предикат
// kacho#3032: бэкенд со звеном доступа фундамента и картой без Health/Check
// готовность края видит SERVING, модель прав о пробе не спрашивают, строки
// «backend not serving» нет. Домен объявлен критичным, чтобы отказ пробы был
// виден и кодом ответа, а не только телом.
func TestReadyzSeesAFoundationBackendWithoutAHealthEntryAsServing(t *testing.T) {
	checks := 0
	conn := foundationBackend(t, domainMap(), true, &checks)
	logs := &lockedBuffer{}

	code, body := probeReadyz(t, proxy.Backends{"vpc": conn}, map[string]bool{"vpc": true}, logs)

	if got := body.Backends["vpc"]; got != "SERVING" {
		t.Fatalf("бэкенд фундамента без записи о здоровье: %q, ожидался SERVING; журнал:\n%s", got, logs)
	}
	if code != http.StatusOK {
		t.Fatalf("/readyz: код %d, ожидался 200", code)
	}
	if checks != 0 {
		t.Fatalf("о пробе живости спросили модель прав %d раз", checks)
	}
	if strings.Contains(logs.String(), "backend not serving") {
		t.Fatalf("здоровый бэкенд записан неготовым:\n%s", logs)
	}
}

// TestReadyzSeesABackendThatRefusesTheProbeAsNotServing — законный близнец,
// отличающийся одним фактом: контракт САМ объявил Health/Check с отношением, и
// модель прав отказывает. Карта побеждает освобождение фундамента, поэтому проба
// отвергнута, и готовность обязана это увидеть: иначе первая проба зеленела бы
// при любом исходе вызова.
func TestReadyzSeesABackendThatRefusesTheProbeAsNotServing(t *testing.T) {
	checks := 0
	m := domainMap()
	m[healthpb.Health_Check_FullMethodName] = authz.RPCEntry{
		Relation: "viewer",
		Extract: authz.StaticExtractor("project", func(any) (string, error) {
			return "prj_x", nil
		}),
	}
	conn := foundationBackend(t, m, false, &checks)
	logs := &lockedBuffer{}

	code, body := probeReadyz(t, proxy.Backends{"vpc": conn}, map[string]bool{"vpc": true}, logs)

	if got := body.Backends["vpc"]; got != "NOT_SERVING" {
		t.Fatalf("бэкенд, отвергший пробу: %q, ожидался NOT_SERVING", got)
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz при отказе критичного бэкенда: код %d, ожидался 503", code)
	}
	if !strings.Contains(logs.String(), "backend not serving") {
		t.Fatalf("отказ критичного бэкенда не записан в журнал:\n%s", logs)
	}
}
