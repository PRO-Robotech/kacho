// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// external_route_gate_chain_test.go — тот же предикат kacho#3053 на НАСТОЯЩИХ
// слоях края: аутентификация боевой посадки (харнесс ka1stand: полоса сессии
// спрашивает настоящий gRPC-сервер службы доступа) и проверка прав с вшитым
// каталогом и настоящей таблицей резолва; за ними — диспетчер restmux. Порядок
// звеньев — тот, что собирает cmd/api-gateway/main.go (его держит гейт корня
// TestExternalRouteGateIsMountedOutsideAuthentication).
//
// Вызывающий — арендатор с ДЕЙСТВУЮЩЕЙ сессией. Проверка прав отказывает
// (арендатор не администратор облака), поэтому без сторожа внутренний путь
// отвечал 403 с именем внутреннего метода и его правом в подробностях.
//
// Перечень — тот же вывод из дескрипторов, что у соседней пробы
// (deriveInternalSpaces), а не выписанный.
package restmux

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
	"github.com/PRO-Robotech/kacho/gateway/internal/listenerorigin"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// tenantDenied — проверка прав для арендатора без административного отношения.
type tenantDenied struct{}

func (tenantDenied) Check(context.Context, middleware.AuthzCheckInput) (middleware.AuthzCheckResult, error) {
	return middleware.AuthzCheckResult{Allowed: false, CheckedAt: time.Now()}, nil
}

// realEdge собирает край в порядке корня: сторож → аутентификация → права →
// диспетчер.
func realEdge(t *testing.T) http.Handler {
	t.Helper()
	stand := ka1stand.New(t, ka1stand.Options{})
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("каталог прав: %v", err)
	}
	authz, err := middleware.NewAuthzMiddleware(middleware.AuthzMiddlewareConfig{
		Enabled: true, Catalog: catalog,
		Subjects:        middleware.NewSubjectExtractor(true),
		Context:         middleware.NewContextExtractor(time.Now, true),
		Resources:       middleware.NewResourceExtractor(nil),
		Checker:         tenantDenied{},
		Logger:          silentTestLogger(),
		CacheTTL:        time.Millisecond,
		CacheMaxEntries: 16,
		PublicAllowlist: middleware.DefaultPublicAllowlist(),
		RestRouter:      middleware.NewRestRouter(),
	})
	if err != nil {
		t.Fatalf("проверка прав: %v", err)
	}
	m, err := NewMux(context.Background(), probeAddrsAll(t), nil, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("NewMux: %v", err)
	}
	var inner http.Handler = m
	inner = authz.HTTP(inner)
	inner = stand.Auth.HTTP(inner)
	return m.ExternalRouteGate(nil, inner)
}

type presented int

const (
	withSession presented = iota
	anonymous
)

func askEdge(edge http.Handler, method, path string, who presented, internal bool) answerShape {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if who == withSession {
		ka1stand.SessionCarrier(ka1stand.SessionLive)(req)
	}
	if internal {
		req = req.WithContext(listenerorigin.WithInternal(req.Context()))
	}
	rec := httptest.NewRecorder()
	edge.ServeHTTP(rec, req)
	return answerShape{
		code:   rec.Code,
		ctype:  rec.Header().Get("Content-Type"),
		body:   rec.Body.String(),
		nosnif: rec.Header().Get("X-Content-Type-Options"),
	}
}

// TestRealEdge_InternalSpacesAnswerNoRouteWithALiveSession — предикат на
// настоящих слоях, для сессии и для анонима.
func TestRealEdge_InternalSpacesAnswerNoRouteWithALiveSession(t *testing.T) {
	c := deriveInternalSpaces()
	if len(c.rows) == 0 {
		t.Fatal("перечень внутренних пространств пуст — пробе нечего утверждать")
	}
	edge := realEdge(t)

	// Положительный контроль: сессия действующая — публичный путь проходит
	// аутентификацию (ответ слоя прав, не 401), а без неё — 401.
	pub := c.public[0]
	if got := askEdge(edge, pub.method, pub.path, withSession, false); got.code == http.StatusUnauthorized {
		t.Fatalf("сессия не принята слоем аутентификации (%s) — «с действующей сессией» не создано", got)
	}
	if got := askEdge(edge, pub.method, pub.path, anonymous, false); got.code != http.StatusUnauthorized {
		t.Fatalf("аноним на публичном пути получил %s, а не 401 — слой аутентификации не в цепочке", got)
	}

	var findings []string
	spaces := map[string]bool{}
	for _, row := range c.rows {
		spaces[row.space] = true
		twinMethod, twinPath := row.method, absentTwinPath
		want := http.StatusNotFound
		if row.class == "otherMethod" {
			twinMethod, twinPath, want = unservedMethod, row.path, http.StatusNotImplemented
		}
		for _, who := range []presented{withSession, anonymous} {
			got := askEdge(edge, row.method, row.path, who, false)
			twin := askEdge(edge, twinMethod, twinPath, who, false)
			if got != twin || got.code != want || strings.Contains(got.body, row.fqn) {
				findings = append(findings, "  "+whoName(who)+": "+row.method+" "+row.path+" ("+row.fqn+")\n"+
					"      внутренний: "+got.String()+"\n"+
					"      близнец   : "+twin.String())
			}
		}
		if inside := askEdge(edge, row.method, row.path, withSession, true); inside.code == http.StatusNotFound && row.class != "unbound" {
			findings = append(findings, "  близнец внутри отвечает 404: "+row.method+" "+row.path+" ("+row.fqn+")")
		}
	}
	t.Logf("перепись: опрошено внутренних адресов %d в %d пространствах, для сессии и анонима — %d запросов снаружи; находок %d",
		len(c.rows), len(spaces), 2*len(c.rows), len(findings))
	if len(findings) > 0 {
		t.Errorf("%d находок: на настоящих слоях края внутреннее пространство снаружи отвечает не «маршрута нет»:\n%s",
			len(findings), strings.Join(findings, "\n"))
	}
}

func whoName(p presented) string {
	if p == withSession {
		return "сессия"
	}
	return "аноним"
}

func silentTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}
