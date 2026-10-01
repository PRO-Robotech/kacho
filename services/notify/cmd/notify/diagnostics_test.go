// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/observability/health"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/corelib/servicehost"
)

// Диагностическая поверхность notify: живость, готовность по базе, метрики —
// на ОДНОМ внутреннем адресе (З15, NTF1-G19). Готовность честна: база
// недоступна — /readyz не 200, доступна — 200; живость от базы не зависит.
func TestDiagnosticSurfaceServesProbesAndMetrics(t *testing.T) {
	var dbUp atomic.Bool
	agg := health.New([]health.Checker{{Name: "database", Check: func(context.Context) error {
		if dbUp.Load() {
			return nil
		}
		return errors.New("база недоступна")
	}}})
	addr := freeAddr(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := describeDiagnosticSurface(addr, newRegistry(), agg, servicecontract.ModeProduction, logger)
	if err != nil {
		t.Fatalf("профиль поверхности отвергнут: %v", err)
	}
	if spec := d.Spec(); spec.Reach != servicecontract.ReachClusterInternal {
		t.Fatalf("досягаемость %s, ожидалась cluster-internal", spec.Reach)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wait, err := servicehost.ServeSurface(ctx, d)
	if err != nil {
		cancel()
		t.Fatalf("поверхность не поднята: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if err := wait(); err != nil {
			t.Errorf("поверхность остановлена с ошибкой: %v", err)
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	get := func(path string) (int, string) {
		t.Helper()
		var lastErr error
		for range 100 {
			resp, err := client.Get("http://" + addr + path)
			if err != nil {
				lastErr = err
				time.Sleep(20 * time.Millisecond)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode, string(body)
		}
		t.Fatalf("GET %s: %v", path, lastErr)
		return 0, ""
	}

	if code, _ := get("/healthz"); code != http.StatusOK {
		t.Fatalf("/healthz = %d при недоступной базе; живость от базы не зависит", code)
	}
	if code, _ := get("/readyz"); code == http.StatusOK {
		t.Fatal("/readyz = 200 при недоступной базе")
	}
	dbUp.Store(true)
	if code, _ := get("/readyz"); code != http.StatusOK {
		t.Fatalf("/readyz = %d при доступной базе", code)
	}
	code, body := get("/metrics")
	if code != http.StatusOK || !strings.Contains(body, "kacho_notify_build_info") {
		t.Fatalf("/metrics = %d без ряда kacho_notify_build_info", code)
	}
	if !strings.Contains(body, `version="unstamped"`) {
		t.Fatalf("бинарь пробы без штампа обязан отвечать unstamped, а не правдоподобным значением")
	}
	if code, _ := get("/debug/pprof/"); code != http.StatusNotFound {
		t.Fatalf("поверхность отвечает на путь вне перечня (%d)", code)
	}
}
