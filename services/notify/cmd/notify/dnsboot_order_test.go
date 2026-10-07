// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// dnsboot_order_test.go — порядок подъёма runServe и страж DNS установки
// (полоса N14; приёмка NTF-1 NTF1-P08 «всё время ожидания /healthz отвечает
// «жив»», «/readyz ни разу не ответил «готов»»; замысел §12а «Порядок подъёма»
// (CX1-131 (а)), «Резолвер и срок запроса» (CX1-131 (б))).
//
// runServe поднимается В ПРОЦЕССЕ пробы с резолвером на молчащую зону и сроком
// старта 10 с. Пока страж ждёт, диагностическая поверхность поднята: /healthz
// отвечает 200 в каждой точке опроса, /readyz — не 200 (компонент `dns` не
// готов). По исчерпании срока runServe возвращает отказ с исчерпанием срока и
// именем проверки. Страж, поставленный до поверхности, даёт пробе ни одного
// ответа /healthz — красный.
//
// Контракт испытуемого: runServe принимает резолвер параметром, main передаёт
// net.DefaultResolver:
//
//	func runServe(cfg config.Config, logger *slog.Logger, resolver *net.Resolver) error
//
// База пробы — закрытый порт петли: пул базы собирается ПОСЛЕ стража DNS
// (corelib db.NewPool не ленив — он проверяет связь при сборке), и до прохода
// стража база не нужна.

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck/dnstest"
)

// silentZone — зона испытания (`dnstest`), которая принимает запросы и не
// отвечает.
func silentZone(t *testing.T) *net.Resolver {
	t.Helper()
	z := dnstest.Start(t)
	z.SetMode(dnstest.Silent)
	return z.Resolver()
}

func TestNTF1P08DiagnosticSurfaceAnswersWhileTheDNSGuardWaits(t *testing.T) {
	diag := freeAddr(t)
	_, closedPort, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: закрытый порт базы: %v", err)
	}
	cfg := loadConfig(t, map[string]string{
		"KACHO_NOTIFY_DIAG_ADDR":         diag,
		"KACHO_NOTIFY_DB_HOST":           "127.0.0.1",
		"KACHO_NOTIFY_DB_PORT":           closedPort,
		"KACHO_NOTIFY_DNS_BOOT_DEADLINE": "10s",
	})
	// Удостоверение пира — настоящие файлы: шаг 2 runServe их читает.
	env := map[string]string{}
	peerTLSFiles(t, env)
	cfg.PeerTLSCertFile = env["KACHO_NOTIFY_PEER_TLS_CERT_FILE"]
	cfg.PeerTLSKeyFile = env["KACHO_NOTIFY_PEER_TLS_KEY_FILE"]
	cfg.PeerTLSCAFile = env["KACHO_NOTIFY_PEER_TLS_CA_FILE"]

	logger := slog.New(slog.NewJSONHandler(&strings.Builder{}, nil))
	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- runServe(cfg, logger, silentZone(t)) }()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	get := func(path string) int {
		resp, err := client.Get("http://" + diag + path)
		if err != nil {
			return 0
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	var polls, healthOK int
	var runErr error
	budget := time.After(40 * time.Second)
loop:
	for {
		select {
		case runErr = <-done:
			break loop
		case <-budget:
			t.Fatal("runServe не вернулся за 40 с при сроке стража 10 с — отказа с исчерпанием срока нет")
		case <-time.After(250 * time.Millisecond):
			// Первые 500 мс — подъём поверхности; дальше каждая точка опроса судится.
			if time.Since(started) < 500*time.Millisecond {
				continue
			}
			polls++
			h, rd := get("/healthz"), get("/readyz")
			select {
			case runErr = <-done:
				break loop // процесс уже выходит: точка опроса не судится
			default:
			}
			if h != http.StatusOK {
				t.Fatalf("точка опроса %d (%v от старта): /healthz ответил %d, ожидалось 200 — живость не отвечает, пока страж ждёт",
					polls, time.Since(started).Round(time.Millisecond), h)
			}
			healthOK++
			if rd == http.StatusOK {
				t.Fatalf("точка опроса %d: /readyz ответил «готов», пока страж DNS не прошёл", polls)
			}
		}
	}
	if healthOK < 5 {
		t.Fatalf("точек опроса с ответом /healthz %d (< 5) — поверхность не отвечала всё ожидание", healthOK)
	}
	if runErr == nil {
		t.Fatal("runServe вернулся без отказа при молчащей зоне")
	}
	msg := runErr.Error()
	if !strings.Contains(msg, "срок") {
		t.Fatalf("отказ не называет исчерпание срока: %v", runErr)
	}
	if !strings.Contains(msg, "dkim") && !strings.Contains(msg, "spf") && !strings.Contains(msg, "dmarc") {
		t.Fatalf("отказ не называет проверку: %v", runErr)
	}
	if el := time.Since(started); el < 9*time.Second {
		t.Fatalf("отказ пришёл через %v — раньше срока старта 10 с", el)
	}
}
