// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/observability/health"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/servicecontract"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	"github.com/PRO-Robotech/kacho/services/geo/internal/observability/metrics"
)

// freeAddr резервирует свободный TCP-порт на loopback и отдаёт его адрес
// (слушатель закрыт — профиль переоткроет). Фиксированный адрес нужен по
// существу: «порт освобождён» доказывается повторной привязкой к ТОМУ ЖЕ адресу.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestLROWorkerStaysUndispatched — kacho-geo не поднимает исполнителя длительных
// операций и ничего в него не отправляет: мутации каталога завершаются синхронно
// (shared/syncop), поэтому package-level default-registry corelib остаётся
// незапущенным на всю жизнь процесса. Это и есть основание, по которому у сервиса
// нет рядов терминальной записи и счётчика исполняемых worker'ов — ряд, который
// невозможно сдвинуть, читается дежурным как «отказов нет».
func TestLROWorkerStaysUndispatched(t *testing.T) {
	if operations.Ready() {
		t.Fatal("kacho-geo must not start the corelib LRO dispatcher: it dispatches nothing to it")
	}
	if n := operations.Active(); n != 0 {
		t.Fatalf("operations.Active() = %d, want 0 — geo runs no async operations", n)
	}
}

// TestDiagnosticSurfaceDisabledByDeclaration — пустой эндпоинт выключает
// поверхность, и выключение это ОБЪЯВЛЕНИЕ, а не тишина.
//
// Проба усилена против прежней: та утверждала «задача nil, гашение — пустышка»,
// то есть форму возврата. Здесь утверждается наблюдаемое — профиль принят,
// сообщает, что не поднимается, НАЗЫВАЕТ причину, и порт остаётся свободен.
func TestDiagnosticSurfaceDisabledByDeclaration(t *testing.T) {
	addr := freeAddr(t)
	d, err := describeDiagnosticSurface("", metrics.New("v", "c"), health.New(nil),
		servicecontract.ModeProduction, discardLogger())
	if err != nil {
		t.Fatalf("объявленное выключение отвергнуто: %v", err)
	}
	if d.Enabled() {
		t.Fatal("поверхность объявлена выключенной, а сообщает, что поднимается")
	}
	if d.DisabledBecause() == "" {
		t.Fatal("выключено, а причина не названа — это снова молчаливое выключение")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait, serr := servicehost.ServeSurface(ctx, d)
	if serr != nil {
		t.Fatalf("выключенная поверхность вернула ошибку: %v", serr)
	}
	if werr := wait(); werr != nil {
		t.Fatalf("ожидание выключенной поверхности вернуло ошибку: %v", werr)
	}
	l, berr := net.Listen("tcp", addr)
	if berr != nil {
		t.Fatalf("поверхность выключена, а порт занят: %v", berr)
	}
	_ = l.Close()
}

// TestDiagnosticSurfaceServesMetricsAndReleasesItsPort — непустой эндпоинт
// поднимает поверхность, она отдаёт /metrics, и ПОСЛЕ отмены контекста порт
// свободен.
//
// Вторая половина — усиление против прежней пробы: та звала гашение через
// `defer` и о его исходе не утверждала ничего, поэтому осталась бы зелёной на
// слушателе, который порт всё ещё держит.
func TestDiagnosticSurfaceServesMetricsAndReleasesItsPort(t *testing.T) {
	m := metrics.New("v", "c")
	m.IncOrphansRecovered("done")
	addr := freeAddr(t)
	d, err := describeDiagnosticSurface(addr, m, health.New(nil),
		servicecontract.ModeProduction, discardLogger())
	if err != nil {
		t.Fatalf("законный профиль отвергнут: %v", err)
	}
	if !d.Enabled() {
		t.Fatal("эндпоинт задан, а профиль сообщает, что не поднимается")
	}

	ctx, cancel := context.WithCancel(context.Background())
	wait, serr := servicehost.ServeSurface(ctx, d)
	if serr != nil {
		t.Fatalf("поверхность не поднялась: %v", serr)
	}
	done := make(chan error, 1)
	go func() { done <- wait() }()

	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		r, e := http.Get("http://" + addr + "/metrics") //nolint:noctx // срок держит петля
		if e == nil {
			resp = r
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("GET /metrics ни разу не удался")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("/metrics status=%d, ожидался 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case serr := <-done:
		if serr != nil {
			t.Fatalf("гашение вернуло ошибку: %v", serr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("профиль не вернулся после отмены контекста — слушатель пережил свой контекст")
	}
	l, berr := net.Listen("tcp", addr)
	if berr != nil {
		t.Fatalf("порт %s занят после возврата профиля: %v", addr, berr)
	}
	_ = l.Close()
}

// Диагностическая поверхность geo разводит ЖИВОСТЬ и ГОТОВНОСТЬ.
//
// Прежде она обслуживала только `/metrics`, а чарт пробировал открытый сокет —
// то есть готовности у сервиса не существовало вовсе, и под рапортовал Ready, не
// умея ответить ни на один запрос. Здесь утверждается наблюдаемое: при
// недоступной базе живость остаётся 200 (процесс жив — перезапускать его незачем),
// а готовность становится 503 и НАЗЫВАЕТ упавшую зависимость.
//
// Спрашивается ЖИВОЙ слушатель, а не мультиплексор в памяти: между объявлением и
// поднятой поверхностью стоит профиль носителя, и проба, обходящая его,
// утверждала бы о наборе маршрутов, а не о том, что отвечает по адресу.
//
// Положительный контроль стоит рядом намеренно: без него утверждение зеленело бы
// на обработчике, отвечающем 503 всегда.
func TestDiagnosticSurfaceTellsLivenessFromReadiness(t *testing.T) {
	ask := func(check func(context.Context) error, path string) (int, string) {
		t.Helper()
		addr := freeAddr(t)
		d, err := describeDiagnosticSurface(addr, metrics.New("v", "c"),
			health.New([]health.Checker{{Name: "database", Check: check}}),
			servicecontract.ModeProduction, discardLogger())
		if err != nil {
			t.Fatalf("законный профиль отвергнут: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		wait, serr := servicehost.ServeSurface(ctx, d)
		if serr != nil {
			t.Fatalf("поверхность не поднялась: %v", serr)
		}
		defer func() {
			cancel()
			_ = wait()
		}()

		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			resp, e := http.Get("http://" + addr + path) //nolint:noctx // срок держит петля
			if e != nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode, string(body)
		}
		t.Fatalf("GET %s ни разу не удался", path)
		return 0, ""
	}

	poolDown := func(context.Context) error { return errors.New("pool is down") }
	poolUp := func(context.Context) error { return nil }

	if code, _ := ask(poolDown, "/healthz"); code != http.StatusOK {
		t.Fatalf("живость при недоступной базе = %d, ожидалось 200: блип зависимости не смерть процесса", code)
	}
	code, body := ask(poolDown, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("готовность при недоступной базе = %d, ожидалось 503 — иначе это живость под другим адресом", code)
	}
	if !strings.Contains(body, "database") {
		t.Fatalf("ответ не назвал упавшую зависимость: %q", body)
	}
	if code, _ := ask(poolUp, "/readyz"); code != http.StatusOK {
		t.Fatalf("готовность на здоровой базе = %d, ожидалось 200", code)
	}
}
