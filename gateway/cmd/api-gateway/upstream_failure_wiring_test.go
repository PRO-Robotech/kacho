// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// upstream_failure_wiring_test.go — kacho#3221, предикат задачи на полосе
// личности: вопрос службе доступа, не уложившийся в бюджет 1ms, и вопрос
// остановленной службе дают вызывающему один и тот же 503 «состояние
// удостоверения не установлено», а на приборе — разные клетки. Проба идёт через
// НАСТОЯЩИЕ соединения корня (dialBackends, клиент субъекта) и настоящий
// адаптер полосы, а не через перехватчик, поставленный руками.

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/upstreamfailure"
)

func stoppedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// silentAddr — слушатель службы, который принимает вызов и молчит до срока.
func silentAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
		<-ss.Context().Done()
		return ss.Context().Err()
	}))
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return l.Addr().String()
}

func identityCells(tally *upstreamfailure.Tally) map[upstreamfailure.Cause]uint64 {
	out := map[upstreamfailure.Cause]uint64{}
	for _, c := range tally.Snapshot() {
		if c.Upstream == "iamInternal" {
			out[c.Cause] = c.Count
		}
	}
	return out
}

func TestIdentityBudgetAndStoppedServiceAreDifferentCells(t *testing.T) {
	cases := []struct {
		name   string
		addr   string
		budget time.Duration
		want   upstreamfailure.Cause
	}{
		{"stopped", stoppedAddr(t), time.Second, upstreamfailure.CauseUnavailable},
		{"budget_1ms", silentAddr(t), time.Millisecond, upstreamfailure.CauseDeadline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tally := upstreamfailure.NewTally(upstreamKeys(config.Config{}))
			backends, closeAll, err := dialBackends(config.Config{IAMInternalAddr: tc.addr, ListenAddr: ":0"}, tally)
			if err != nil {
				t.Fatal(err)
			}
			defer closeAll()
			adapter := clients.NewSessionRevocationsAdapter(backends["iamInternal"], tc.budget)
			if _, _, err := adapter.ResolveHumanSession(context.Background(), "carrier"); err == nil {
				t.Fatal("вопрос прошёл — условия отказа не создано")
			}
			cells := identityCells(tally)
			for _, c := range upstreamfailure.Causes() {
				want := uint64(0)
				if c == tc.want {
					want = 1
				}
				if cells[c] != want {
					t.Errorf("клетка iamInternal/%s = %d, ожидалась %d (%v)", c, cells[c], want, cells)
				}
			}
		})
	}
}

// Клиент субъекта и базовой полосы набирает службу доступа СВОИМ соединением —
// и оно обязано нести тот же учёт под тем же соседом.
func TestSubjectClientConnectionCarriesTheTally(t *testing.T) {
	tally := upstreamfailure.NewTally(upstreamKeys(config.Config{}))
	c, err := clients.NewIAMSubjectClient(stoppedAddr(t), slog.New(slog.DiscardHandler), nil, tally.DialOptions("iamInternal")...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.LookupByExternalID(context.Background(), "ext-1"); err == nil {
		t.Fatal("вопрос прошёл — условия отказа не создано")
	}
	if got := identityCells(tally)[upstreamfailure.CauseUnavailable]; got != 1 {
		t.Fatalf("клетка iamInternal/unavailable = %d, ожидалась 1", got)
	}
}

// Словарь соседей учёта — ключи адресов корня, все, а не только заданные.
func TestUpstreamKeysAreTheBackendAddressKeys(t *testing.T) {
	keys := upstreamKeys(config.Config{})
	want := config.Config{}.BackendAddrs()
	if len(keys) != len(want) || len(keys) == 0 {
		t.Fatalf("соседей %d, ключей адресов %d", len(keys), len(want))
	}
	for _, k := range keys {
		if _, ok := want[k]; !ok {
			t.Errorf("сосед %q не ключ адреса", k)
		}
	}
}
