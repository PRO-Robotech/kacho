// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// wire_test.go — то, чего запись ответа в память доказать НЕ МОЖЕТ.
//
// `httptest.NewRecorder` соединением не является: он всегда умеет сбрасывать
// буфер и никогда не отсоединяется. Поэтому на нём не исполняются ни уход
// клиента, ни кадрирование на настоящем проводе, ни заголовки в том виде, в
// каком их увидит посредник. Здесь стоит НАСТОЯЩИЙ сервер и настоящий сокет.

// liveEdge поднимает ручку на настоящем сокете.
func liveEdge(t *testing.T, owner *ownerStub, tune ...func(*subscriptionstream.Config)) *httptest.Server {
	t.Helper()
	h := newHandler(t, owner, tune...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Личность ставит полоса аутентификации; здесь она подставлена так же,
		// как её увидит ручка в бою.
		r.Header.Set(principalmeta.HeaderPrincipalType, "user")
		r.Header.Set(principalmeta.HeaderPrincipalID, "usr-probe")
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFramingSurvivesARealSocket — кадры доезжают по проводу ПО ОДНОМУ, а не
// одним куском в конце.
//
// Это и есть предмет: поток, приезжающий целиком по закрытии, работает во всех
// пробах на записи в память и не работает у клиента.
func TestFramingSurvivesARealSocket(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
		openedMessage("pos-0", true),
		eventWithState(t, "pos-1", &vpcv1.Network{Id: "net-1", Name: "probe"}),
	}, hold: true}
	srv := liveEdge(t, owner, func(c *subscriptionstream.Config) {
		c.StreamBudget = 3 * time.Second
		c.Heartbeat = time.Second
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		srv.URL+subscriptionstream.Path+"?owner=probe", nil)
	if err != nil {
		t.Fatalf("сборка запроса: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("запрос к живому краю: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if got := res.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("на проводе X-Accel-Buffering = %q — посредник копил бы ответ", got)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("на проводе Content-Type = %q", got)
	}

	// Читаем ДО закрытия потока: если бы кадры копились, здесь был бы таймаут.
	reader := bufio.NewReader(res.Body)
	deadline := time.After(15 * time.Second)
	got := make([]string, 0, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for len(got) < 2 {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			if strings.HasPrefix(line, "event: ") {
				got = append(got, strings.TrimSpace(strings.TrimPrefix(line, "event: ")))
			}
		}
	}()
	select {
	case <-done:
	case <-deadline:
		t.Fatal("кадры не приехали по проводу до закрытия потока — ответ копится, " +
			"и поток перестал быть потоком")
	}
	if len(got) != 2 || got[0] != "opened" || got[1] != "event" {
		t.Errorf("на проводе кадры %v", got)
	}
}

// TestStateOnTheWireReachesTheSubscriber — состояние предмета доезжает
// разобранным, а не строкой.
//
// Ветвь носителя иначе не исполняется ни разу, и «поток работает» держалось бы
// на событиях без нагрузки — то есть на том единственном случае, где разбирать
// нечего.
func TestStateOnTheWireReachesTheSubscriber(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
		openedMessage("pos-0", true),
		eventWithState(t, "pos-1", &vpcv1.Network{Id: "net-42", Name: "probe-net"}),
	}}
	rec := serve(t, newHandler(t, owner), request("owner=probe"))

	got := frames(t, rec.Body.String())
	if len(got) != 2 {
		t.Fatalf("кадров %d: %q", len(got), rec.Body.String())
	}
	if !strings.Contains(got[1].data, "net-42") || !strings.Contains(got[1].data, "probe-net") {
		t.Errorf("состояние предмета не доехало: %q", got[1].data)
	}
	if !strings.Contains(got[1].data, "vpc.v1.Network") {
		t.Errorf("нагрузка не назвала своего типа: %q — подписчик не отличит её от чужой",
			got[1].data)
	}
}

// TestUnresolvableStateBecomesTheContractsOwnSignal — состояние, которого край
// разобрать не может, заменяется значением КОНТРАКТА, а не выдумкой края.
//
// Поток при этом ПРОДОЛЖАЕТСЯ: одно неразбираемое событие не есть причина
// оборвать подписку, и контракт для этого случая завёл своё значение.
func TestUnresolvableStateBecomesTheContractsOwnSignal(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{
		openedMessage("pos-0", true),
		eventWithUnresolvableState("pos-1"),
		eventMessage("pos-2", "compute.placement_group", "pg-after"),
	}}
	rec := serve(t, newHandler(t, owner), request("owner=probe"))

	got := frames(t, rec.Body.String())
	if len(got) != 3 {
		t.Fatalf("кадров %d, ожидалось 3 — неразбираемое событие оборвало поток: %q",
			len(got), rec.Body.String())
	}
	if !strings.Contains(got[1].data, "NOT_SERIALIZABLE") {
		t.Errorf("край не назвал причину недоступности состояния значением контракта: %q", got[1].data)
	}
	if got[1].id != "pos-1" {
		t.Errorf("событие потеряло позицию %q — возобновиться с него было бы нечем", got[1].id)
	}
	if !strings.Contains(got[2].data, "pg-after") {
		t.Errorf("поток не продолжился после неразбираемого события: %q", got[2].data)
	}
}

// TestClientGoingAwayReleasesTheStream — ушедший клиент освобождает слот.
//
// На записи в память это невыразимо: она не отсоединяется никогда. Между тем
// именно так поток и заканчивается чаще всего — вкладку закрыли.
func TestClientGoingAwayReleasesTheStream(t *testing.T) {
	owner := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("pos-0", true)},
		hold:    true,
		started: make(chan struct{}),
	}
	h := newHandler(t, owner, func(c *subscriptionstream.Config) {
		c.MaxStreams = 1
		c.MaxStreamsPerSubject = 1
		c.StreamBudget = 30 * time.Second
		c.Heartbeat = 10 * time.Second
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(principalmeta.HeaderPrincipalType, "user")
		r.Header.Set(principalmeta.HeaderPrincipalID, "usr-probe")
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+subscriptionstream.Path+"?owner=probe", nil)
	if err != nil {
		t.Fatalf("сборка запроса: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	select {
	case <-owner.started:
	case <-time.After(10 * time.Second):
		t.Fatal("поток не открылся")
	}

	// Клиент ушёл, не дожидаясь ни срока, ни закрытия владельцем.
	cancel()
	_ = res.Body.Close()

	deadline := time.After(15 * time.Second)
	for {
		if h.Stats().Open == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("после ухода клиента на учёте %d потоков — слот и соединение "+
				"остались бы заняты до истечения срока", h.Stats().Open)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
