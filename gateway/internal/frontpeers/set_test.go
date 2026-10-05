// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// set_test.go — ЗВЕНЬЯ ФРОНТА — АДРЕСА ПОДОВ, А НЕ СЕТЬ (kacho#3028, круг 3).
//
// Предмет — множество адресов, которые край признаёт доверенными звеньями:
// адреса подов, выбранных безголовыми службами фронта (раздача консоли,
// контроллер входа). Под кластера вне этих служб — не звено, даже если его
// адрес лежит в сети круга. Каждое отрицательное утверждение — в паре с
// положительным близнецом, отличным в один факт.
package frontpeers_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/frontpeers"
)

var (
	frontPod  = netip.MustParseAddr("10.244.1.17")
	frontPod2 = netip.MustParseAddr("10.244.2.9")
	otherPod  = netip.MustParseAddr("10.244.3.4")
)

// dns — управляемый разрешатель: ответ на имя задаётся пробой.
type dns struct {
	mu    sync.Mutex
	addrs map[string][]netip.Addr
	err   error
	calls int
	asked chan string
}

func (d *dns) resolve(_ context.Context, name string) ([]netip.Addr, error) {
	d.mu.Lock()
	d.calls++
	addrs, err := d.addrs[name], d.err
	d.mu.Unlock()
	if d.asked != nil {
		select {
		case d.asked <- name:
		default:
		}
	}
	if err != nil {
		return nil, err
	}
	if addrs == nil {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	return addrs, nil
}

func (d *dns) set(name string, addrs ...netip.Addr) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.addrs == nil {
		d.addrs = map[string][]netip.Addr{}
	}
	d.addrs[name] = addrs
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newSet(t *testing.T, d *dns, c *clock, names ...string) *frontpeers.Set {
	t.Helper()
	s, err := frontpeers.New(frontpeers.Options{
		Names: names, Refresh: 5 * time.Second, Resolve: d.resolve, Now: c.now, MinGap: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// Под в сети круга, но не выбранный службой фронта, — не звено; близнец —
// под, который служба выбирает, — звено.
func TestSet_TrustsOnlyAddressesTheFrontServicesSelect(t *testing.T) {
	d, c := &dns{}, &clock{t: time.Unix(1_800_000_000, 0)}
	d.set("api-gateway-front-console", frontPod)
	d.set("api-gateway-front-ingress", frontPod2)
	s := newSet(t, d, c, "api-gateway-front-console", "api-gateway-front-ingress")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	for _, a := range []netip.Addr{frontPod, frontPod2, netip.AddrFrom16(frontPod.As16())} {
		if !s.Trusts(a) {
			t.Errorf("адрес %s выбран службой фронта, а звеном не признан", a)
		}
	}
	if s.Trusts(otherPod) {
		t.Errorf("под %s службой фронта не выбран, а признан звеном", otherPod)
	}
}

// До первого разрешения звеньев нет ни одного: доверие не выдаётся авансом.
func TestSet_BeforeTheFirstResolutionTrustsNobody(t *testing.T) {
	d, c := &dns{}, &clock{t: time.Unix(1_800_000_000, 0)}
	d.set("api-gateway-front-console", frontPod)
	s := newSet(t, d, c, "api-gateway-front-console")
	if s.Trusts(frontPod) {
		t.Fatal("до первого разрешения адрес признан звеном")
	}
	if err := s.Refresh(context.Background()); err != nil || !s.Trusts(frontPod) {
		t.Fatalf("близнец: после разрешения звено не признано (ошибка %v)", err)
	}
}

// Под ушёл из службы (служба без подов отвечает «нет такого имени») — его
// адрес больше не звено: адрес переиспользуется другим подом.
func TestSet_AddressLeavingTheServiceStopsBeingALink(t *testing.T) {
	d, c := &dns{}, &clock{t: time.Unix(1_800_000_000, 0)}
	d.set("api-gateway-front-console", frontPod, frontPod2)
	s := newSet(t, d, c, "api-gateway-front-console")
	if err := s.Refresh(context.Background()); err != nil || !s.Trusts(frontPod) {
		t.Fatalf("близнец: звено не признано (ошибка %v)", err)
	}
	d.set("api-gateway-front-console", frontPod2)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if s.Trusts(frontPod) {
		t.Error("адрес, ушедший из службы фронта, остался звеном")
	}
	d.set("api-gateway-front-console")
	d.mu.Lock()
	delete(d.addrs, "api-gateway-front-console")
	d.mu.Unlock()
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("служба без подов — законное «звеньев нет», а не ошибка: %v", err)
	}
	if s.Trusts(frontPod2) {
		t.Error("служба без подов, а прежний адрес остался звеном")
	}
}

// Сбой разрешения (не «нет имени») оставляет прежний перечень — но не дольше
// предела давности: дальше доверия нет никому.
func TestSet_ResolutionFailureKeepsTheLastAnswerOnlyWithinItsStaleness(t *testing.T) {
	d, c := &dns{}, &clock{t: time.Unix(1_800_000_000, 0)}
	d.set("api-gateway-front-console", frontPod)
	s := newSet(t, d, c, "api-gateway-front-console")
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	d.mu.Lock()
	d.err = errors.New("i/o timeout")
	d.mu.Unlock()
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("сбой разрешения не назван ошибкой")
	}
	c.add(10 * time.Second)
	if !s.Trusts(frontPod) {
		t.Fatal("близнец: в пределах давности прежний ответ обязан действовать")
	}
	c.add(10 * time.Second) // 20 с > 3 × 5 с
	if s.Trusts(frontPod) {
		t.Fatal("ответ старше предела давности всё ещё признаёт звено")
	}
}

// Промах по адресу будит обновление (под фронта сменил адрес), а Run
// завершается вместе с контекстом.
func TestSet_MissNudgesARefreshAndRunStopsWithItsContext(t *testing.T) {
	d, c := &dns{asked: make(chan string, 8)}, &clock{t: time.Unix(1_800_000_000, 0)}
	s, err := frontpeers.New(frontpeers.Options{
		Names: []string{"api-gateway-front-console"}, Refresh: time.Hour, Resolve: d.resolve, Now: c.now,
		MinGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitAsked := func(what string) {
		t.Helper()
		select {
		case <-d.asked:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: разрешатель не спрошен", what)
		}
	}
	waitAsked("первое разрешение при запуске")
	d.set("api-gateway-front-console", frontPod)
	if s.Trusts(frontPod) {
		t.Fatal("адрес признан звеном до разрешения, его назвавшего")
	}
	waitAsked("промах по адресу")
	deadline := time.Now().Add(5 * time.Second)
	for !s.Trusts(frontPod) {
		if time.Now().After(deadline) {
			t.Fatal("после обновления по промаху звено не признано")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run не завершился с контекстом")
	}
}

func TestNew_RefusesWhatCannotWork(t *testing.T) {
	d, c := &dns{}, &clock{}
	for name, o := range map[string]frontpeers.Options{
		"нет имён":           {Refresh: time.Second, Resolve: d.resolve, Now: c.now},
		"нулевое обновление": {Names: []string{"a"}, Resolve: d.resolve, Now: c.now},
		"нет разрешателя":    {Names: []string{"a"}, Refresh: time.Second, Now: c.now},
	} {
		if _, err := frontpeers.New(o); err == nil {
			t.Errorf("%s: New без отказа", name)
		}
	}
	if _, err := frontpeers.New(frontpeers.Options{Names: []string{"a"}, Refresh: time.Second, Resolve: d.resolve}); err != nil {
		t.Errorf("близнец: законные настройки отвергнуты: %v", err)
	}
}
