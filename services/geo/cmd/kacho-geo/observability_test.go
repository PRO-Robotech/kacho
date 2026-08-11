// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

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
	d, err := describeDiagnosticSurface("", metrics.New("v", "c"),
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
	if serr := servicehost.ServeSurface(ctx, d); serr != nil {
		t.Fatalf("выключенная поверхность вернула ошибку: %v", serr)
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
	d, err := describeDiagnosticSurface(addr, m, servicecontract.ModeProduction, discardLogger())
	if err != nil {
		t.Fatalf("законный профиль отвергнут: %v", err)
	}
	if !d.Enabled() {
		t.Fatal("эндпоинт задан, а профиль сообщает, что не поднимается")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicehost.ServeSurface(ctx, d) }()

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
