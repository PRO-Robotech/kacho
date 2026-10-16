// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

// upstream_failure_cause_test.go — kacho#3221, предикат задачи на мосту REST:
// неразрешимый адрес бэкенда домена и бюджет вызова 1ms дают ряды величин,
// отличимые от ряда при остановленной службе. Ответ вызывающему у первых двух
// неисправностей один и тот же 503 (замер посылки: «no children to pick from»
// против «connection refused»), поэтому различает их прибор — клетка
// соседа и причины.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"

	geopb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/upstreamfailure"
)

// zeroAddressScheme — разрешатель, который на любое имя отдаёт пустой набор
// адресов: ровно так разрешатель dns библиотеки отвечает на имя, которого нет.
// Регистрируется глобально (адрес моста — строка), схема своя у этого файла.
const zeroAddressScheme = "kachoprobeunresolved"

func init() {
	r := manual.NewBuilderWithScheme(zeroAddressScheme)
	r.BuildCallback = func(resolver.Target, resolver.ClientConn, resolver.BuildOptions) {
		go r.UpdateState(resolver.State{})
	}
	resolver.Register(r)
}

// slowGeo — бэкенд домена, который принимает вызов и молчит до срока.
type slowGeo struct {
	geopb.UnimplementedRegionServiceServer
}

func (slowGeo) List(ctx context.Context, _ *geopb.ListRegionsRequest) (*geopb.ListRegionsResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func cellsOf(t *upstreamfailure.Tally) map[string]uint64 {
	out := map[string]uint64{}
	for _, c := range t.Snapshot() {
		out[c.Upstream+"/"+string(c.Cause)] = c.Count
	}
	return out
}

func TestBridgeFailureCauseIsVisibleOnTheInstrument(t *testing.T) {
	stopped, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stoppedAddr := stopped.Addr().String()
	_ = stopped.Close()

	slow, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	geopb.RegisterRegionServiceServer(srv, slowGeo{})
	go func() { _ = srv.Serve(slow) }()
	t.Cleanup(srv.Stop)

	cases := []struct {
		name       string
		addr       string
		budget     time.Duration
		wantStatus int
		wantCause  upstreamfailure.Cause
	}{
		{"stopped", stoppedAddr, 30 * time.Second, http.StatusServiceUnavailable, upstreamfailure.CauseUnavailable},
		{"unresolvable", zeroAddressScheme + ":///kacho-geo-absent.kacho.svc:9090", 30 * time.Second, http.StatusServiceUnavailable, upstreamfailure.CauseUnresolved},
		{"budget_1ms", slow.Addr().String(), time.Millisecond, http.StatusGatewayTimeout, upstreamfailure.CauseDeadline},
	}
	seen := map[upstreamfailure.Cause]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addrs := geoMuxAddrs()
			addrs["geo"] = tc.addr
			tally := upstreamfailure.NewTally([]string{"geo", "geoInternal"})
			h, err := NewMux(t.Context(), addrs, nil, nil, tc.budget, WithUpstreamFailures(tally))
			if err != nil {
				t.Fatalf("NewMux: %v", err)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/geo/v1/regions", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("предпосылка: ответ вызывающему %d, ожидался %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			cells := cellsOf(tally)
			for _, c := range upstreamfailure.Causes() {
				want := uint64(0)
				if c == tc.wantCause {
					want = 1
				}
				if got := cells["geo/"+string(c)]; got != want {
					t.Errorf("клетка geo/%s = %d, ожидалась %d (все клетки: %v)", c, got, want, cells)
				}
			}
			seen[tc.wantCause] = true
		})
	}
	if len(seen) != len(cases) {
		t.Fatalf("неисправностей %d, различных причин на приборе %d — ряды неразличимы", len(cases), len(seen))
	}
}
