// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts && consolefront

// console_public_front_client_address_test.go — РАЗДАЧА КОНСОЛИ ДОНОСИТ ДО
// КРАЯ АДРЕС КЛИЕНТА, А НЕ ТО, ЧТО КЛИЕНТ О СЕБЕ НАПИСАЛ (kacho#3028).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ И ГРАНИЦА
//
// Край выводит адрес клиента из `X-Forwarded-For`, принятого от раздачи
// консоли (доверенное звено), и этим же адресом служба доступа ключует
// ограничение частоты входа «на источник». Значит, всё, что раздача кладёт в
// этот заголовок, становится источником. Поднимается НАСТОЯЩАЯ раздача —
// образ и карта настройки из рендера цепочки, несущей внешний вход, — а за ней
// дублёр края, который записывает, что раздача ему прислала.
//
// Два клиента с РАЗНЫХ адресов (хост через проброшенный порт и отдельный
// контейнер в той же сети) идут на TLS-вход, и каждый прикладывает один и тот
// же подделанный `X-Forwarded-For`. Утверждается:
//
//	предмет  — `X-Forwarded-For`, дошедший до края, равен ровно адресу TCP-пира
//	           раздачи (его же раздача кладёт в `X-Real-IP`) и подделанного
//	           значения не несёт;
//	близнец  — два клиента дают два разных источника: подделка, одинаковая у
//	           обоих, не сводит их в один.
//
// Подмену адреса на Service (`externalTrafficPolicy`) эта проба не видит —
// kube-proxy в ней нет; ту часть цепочки держит рендер
// (console_public_front_render_test.go, п. 5).
//
// Запуск: `go test -tags 'helmcharts consolefront' -run TestConsolePublicFront ./deploy/`.
// Нет docker, helm или образа раздачи — отказ пробы («условие не создано»), а
// не пропуск.
package deploy_test

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

const forgedForwardedFor = "192.0.2.200"

// seenByEdge — что раздача прислала дублёру края на один запрос.
type seenByEdge struct{ ForwardedFor, RealIP string }

// forwardRecorder — дублёр края: запоминает заголовки адреса по метке запроса.
type forwardRecorder struct {
	mu   sync.Mutex
	seen map[string]seenByEdge
}

func (f *forwardRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen[r.Header.Get("X-Request-ID")] = seenByEdge{
		ForwardedFor: r.Header.Get("X-Forwarded-For"), RealIP: r.Header.Get("X-Real-IP"),
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (f *forwardRecorder) get(id string) (seenByEdge, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.seen[id]
	return s, ok
}

func TestConsolePublicFrontCarriesTheClientAddressNotTheClientClaim(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker не в PATH — раздачу поднять нечем, условие пробы не создано")
	}
	front := frontFromRender(t)
	rec := &forwardRecorder{seen: map[string]seenByEdge{}}
	run := startFront(t, front, rec)
	httpsAddr := run.Mapped(front.HTTPS)
	frontIP := dockerOut(t, "inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", run.ID)
	const path = "/iam/v1/auth/login"

	// Клиент A — хост, через проброшенный порт TLS-входа.
	hostClient := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: run.Pool, MinVersion: tls.VersionTLS12}}}
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := hostClient.Get("https://" + httpsAddr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("раздача не поднялась на %s за 30 с (последняя ошибка %v)", httpsAddr, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	req, _ := http.NewRequest(http.MethodPost, "https://"+httpsAddr+path, strings.NewReader(`{}`))
	req.Header.Set("X-Forwarded-For", forgedForwardedFor)
	req.Header.Set("X-Request-ID", "client-a")
	resp, err := hostClient.Do(req)
	if err != nil {
		t.Fatalf("клиент A: %v", err)
	}
	resp.Body.Close()

	// Клиент B — отдельный контейнер той же сети: другой адрес источника.
	sidecar := dockerOut(t, "run", "-d", "--entrypoint", "sleep", front.Image, "120")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", sidecar).Run() })
	clientBIP := dockerOut(t, "inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", sidecar)
	dockerOut(t, "exec", sidecar, "curl", "-sk", "--max-time", "10", "-o", "/dev/null",
		"-X", "POST", "-H", "X-Forwarded-For: "+forgedForwardedFor, "-H", "X-Request-ID: client-b",
		"-d", "{}", fmt.Sprintf("https://%s/%s", net.JoinHostPort(frontIP, fmt.Sprint(front.HTTPS)), strings.TrimPrefix(path, "/")))

	a, okA := rec.get("client-a")
	b, okB := rec.get("client-b")
	if !okA || !okB {
		t.Fatalf("до края дошли не оба запроса (A %v, B %v) — условие пробы не создано", okA, okB)
	}
	t.Logf("край получил: A X-Forwarded-For=%q X-Real-IP=%q · B X-Forwarded-For=%q X-Real-IP=%q (адрес B %s)",
		a.ForwardedFor, a.RealIP, b.ForwardedFor, b.RealIP, clientBIP)

	t.Run("предмет: заголовок к краю — адрес пира раздачи, а не заявленный клиентом", func(t *testing.T) {
		for name, s := range map[string]seenByEdge{"A": a, "B": b} {
			if strings.Contains(s.ForwardedFor, forgedForwardedFor) {
				t.Errorf("клиент %s: подделанный адрес дошёл до края в X-Forwarded-For=%q", name, s.ForwardedFor)
			}
			if s.ForwardedFor != s.RealIP {
				t.Errorf("клиент %s: X-Forwarded-For=%q не равен адресу пира раздачи %q", name, s.ForwardedFor, s.RealIP)
			}
		}
		if b.RealIP != clientBIP {
			t.Errorf("клиент B: раздача видит пир %q, а клиент пришёл с %s — проба меряет не тот адрес", b.RealIP, clientBIP)
		}
	})
	t.Run("близнец: два клиента — два источника", func(t *testing.T) {
		if a.ForwardedFor == b.ForwardedFor {
			t.Errorf("два клиента с разных адресов дали краю один источник %q", a.ForwardedFor)
		}
	})
}
