// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// boot_test.go — NTF1-N08 на уровне процесса: `notify-probe serve` без
// KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED не стартует и называет переменную;
// с true — стартует (готовность поверхности /readyz = 200 на базе с
// миграциями бинаря) и гаснет по отмене без ошибки.

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

// lockedBuffer — журнал процесса: пишет носитель из своих горутин.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// NTF1-N08 — переменная флага не задана: отказ старта с её именем ДО подъёма
// чего-либо — диагностическая поверхность не слушает.
func TestNTF1N08_WithoutTheFlagTheProbeRefusesToStart(t *testing.T) {
	ca := newTestCA(t)
	env := standEnv(t, pgtest.NewDB(t), serveModel(t, &model{}), "", ca)
	setEnv(t, env)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var log lockedBuffer
	err := run(ctx, []string{"serve"}, &log)
	if err == nil {
		t.Fatal("процесс без флага доставки стартовал, а обязан отказать с именем переменной")
	}
	if !strings.Contains(err.Error(), config.FlagKnob) {
		t.Fatalf("отказ старта не называет %s:\n%v", config.FlagKnob, err)
	}
	if c, derr := net.DialTimeout("tcp", env["KACHO_NOTIFYPROBE_METRICS_ADDR"], 300*time.Millisecond); derr == nil {
		_ = c.Close()
		t.Fatal("отказ старта наступил ПОСЛЕ подъёма диагностической поверхности")
	}
}

// Близнец NTF1-N08 — со значением true процесс стартует: готовность отвечает
// 200, отмена гасит его без ошибки.
func TestNTF1N08_WithTrueTheProbeStarts(t *testing.T) {
	for _, flag := range []string{"true"} {
		t.Run(flag, func(t *testing.T) {
			ca := newTestCA(t)
			env := standEnv(t, pgtest.NewDB(t), serveModel(t, &model{}), flag, ca)
			setEnv(t, env)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var log lockedBuffer
			done := make(chan error, 1)
			go func() { done <- run(ctx, []string{"serve"}, &log) }()

			ready := "http://" + env["KACHO_NOTIFYPROBE_METRICS_ADDR"] + "/readyz"
			deadline := time.Now().Add(60 * time.Second)
			for {
				select {
				case err := <-done:
					t.Fatalf("процесс с флагом %s завершился до готовности: %v\nжурнал:\n%s", flag, err, log.String())
				default:
				}
				if readyOK(ready) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("процесс с флагом %s не стал готов за 60 с\nжурнал:\n%s", flag, log.String())
				}
				time.Sleep(100 * time.Millisecond)
			}

			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("гашение по отмене вернуло ошибку: %v\nжурнал:\n%s", err, log.String())
				}
			case <-time.After(30 * time.Second):
				t.Fatal("процесс не погас за 30 с после отмены")
			}
			if !strings.Contains(log.String(), "servicehost: start refusals passed") {
				t.Fatalf("носитель не напечатал перепись отказов старта — отказы не исполнялись:\n%s", log.String())
			}
		})
	}
}

func readyOK(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
