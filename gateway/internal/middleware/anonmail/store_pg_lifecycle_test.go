// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestPg_CX2_44_ShutdownClosesThePoolAfterTheServerAndTheDecisionFinishes —
// проба гашения (CX2-44 (2)): решение, начатое до остановки HTTP-сервера,
// дорабатывает на живом пуле; пул ограничителя закрывается ПОСЛЕ остановки
// сервера (порядок корня: Shutdown, затем отложенные закрытия хранилищ).
func TestPg_CX2_44_ShutdownClosesThePoolAfterTheServerAndTheDecisionFinishes(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	r, ps, _ := pgRig(t, dsn, l)
	holder, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close(context.Background()) }()
	tx, err := holder.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: r.h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	// Звено читает TCP-пира: запрос идёт с 127.0.0.1, ключ — его.
	k, _ := KeysFor("127.0.0.1")
	p := keyLockPair(k.Source)
	if _, err := tx.Exec(context.Background(), `SELECT pg_advisory_xact_lock($1::int4, $2::int4)`, p.class, p.obj); err != nil {
		t.Fatal(err)
	}
	var code atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := http.Post("http://"+ln.Addr().String()+pathRecovery, "application/json", nil)
		if err == nil {
			code.Store(int64(resp.StatusCode))
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond) // решение начато и ждёт ключ
	shutDone := make(chan struct{})
	go func() {
		_ = srv.Shutdown(context.Background())
		close(shutDone)
	}()
	time.Sleep(50 * time.Millisecond)
	_ = tx.Rollback(context.Background()) // ключ свободен в пределах ожидания
	<-shutDone
	wg.Wait()
	if err := ps.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("решение, начатое до остановки: %d · соединений пула после закрытия %d", code.Load(), ps.b.pool.Stat().TotalConns())
	if code.Load() != http.StatusOK {
		t.Fatalf("решение, начатое до остановки сервера, не доработало: %d", code.Load())
	}
	if ps.b.pool.Stat().TotalConns() != 0 {
		t.Fatalf("пул не закрыт после остановки сервера")
	}
}

// TestPg_LoadRunFleetOfTwo — нагрузочный прогон звена с флотом 2 на postgres
// (предикат полосы E2; замысел З8 «Общий поток» — число до кода не
// называлось): две реплики, каждая со своим пулом, общая база; запросы с
// разных источников без доказательства; печатается число решений в секунду и
// раскладка по исходам.
func TestPg_LoadRunFleetOfTwo(t *testing.T) {
	dsn := edgeDB(t)
	l := testLimits()
	l.Global.RatePerSecond, l.Global.Burst = 100000, 100000
	a, _, _ := pgRig(t, dsn, l)
	b, _, _ := pgRig(t, dsn, l)
	const workers = 16
	const span = 3 * time.Second
	var pass, challenge, unavailable, other atomic.Int64
	deadline := time.Now().Add(span)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		r := a
		if w%2 == 1 {
			r = b
		}
		go func(w int) {
			defer wg.Done()
			for i := 0; time.Now().Before(deadline); i++ {
				ip := fmt.Sprintf("10.%d.%d.%d", w, (i/250)%250, i%250+1)
				switch rec := r.send(pathRecovery, ip, "", ""); rec.Code {
				case http.StatusOK:
					pass.Add(1)
				case http.StatusTooManyRequests:
					challenge.Add(1)
				case http.StatusServiceUnavailable:
					unavailable.Add(1)
				default:
					other.Add(1)
				}
			}
		}(w)
	}
	wg.Wait()
	total := pass.Load() + challenge.Load() + unavailable.Load() + other.Load()
	t.Logf("флот 2 · исполнителей %d · %s: решений %d (%.0f в секунду) · пропуск %d · вызов %d · 503 %d · прочее %d",
		workers, span, total, float64(total)/span.Seconds(), pass.Load(), challenge.Load(), unavailable.Load(), other.Load())
	if pass.Load() == 0 || other.Load() != 0 {
		t.Fatalf("нагрузочный прогон без пропусков либо с исходами вне закрытого набора")
	}
}
