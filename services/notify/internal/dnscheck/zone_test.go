// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dnscheck_test

// zone_test.go — зона DNS испытания для проб стража (полоса N14, §12а). Это
// фикстура RED-полосы, а не двойник `dnscheck/dnstest`: тот заводит полоса
// реализации для КАЖДОГО in-process старта notify; эта зона нужна только
// пробам этого пакета и испытуемого не содержит.
//
// Сеть настоящая: UDP-сервер на петле, ответы — настоящие сообщения DNS
// (golang.org/x/net/dns/dnsmessage). Резолвер пробы — `*net.Resolver` с
// `PreferGo` и `Dial` на адрес зоны: стандартный разбор ответа, склейка строк
// TXT и род ошибки — те же, что в бою. Зона не снисходительнее настоящей:
// имени без записей она отвечает NXDOMAIN, а не пустым «успехом».

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// zoneMode — как зона отвечает.
type zoneMode int

const (
	zoneAnswer   zoneMode = iota // записи зоны
	zoneServfail                 // SERVFAIL на каждый запрос
	zoneSilent                   // запрос принят, ответа нет
)

// arrival — запрос, пришедший в зону.
type arrival struct {
	name string
	at   time.Time
}

type zone struct {
	t  *testing.T
	pc net.PacketConn

	mu       sync.Mutex
	records  map[string][][]string // абсолютное имя (нижний регистр) → записи TXT → строки записи
	mode     zoneMode
	failing  func() bool // при не-nil и true — SERVFAIL (поверх mode)
	arrivals []arrival
}

func startZone(t *testing.T) *zone {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: зона испытания не поднята: %v", err)
	}
	z := &zone{t: t, pc: pc, records: map[string][][]string{}}
	go z.serve()
	t.Cleanup(func() { _ = pc.Close() })
	return z
}

// set заменяет записи имени; nil снимает имя из зоны.
func (z *zone) set(name string, records ...[]string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	name = strings.ToLower(name)
	if records == nil {
		delete(z.records, name)
		return
	}
	z.records[name] = records
}

func (z *zone) setMode(m zoneMode) {
	z.mu.Lock()
	z.mode = m
	z.mu.Unlock()
}

func (z *zone) setFailing(f func() bool) {
	z.mu.Lock()
	z.failing = f
	z.mu.Unlock()
}

func (z *zone) seen() []arrival {
	z.mu.Lock()
	defer z.mu.Unlock()
	return append([]arrival(nil), z.arrivals...)
}

// resolver — резолвер пробы: каждый запрос уходит в зону испытания.
func (z *zone) resolver() *net.Resolver {
	addr := z.pc.LocalAddr().String()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

func (z *zone) serve() {
	buf := make([]byte, 4096)
	for {
		n, from, err := z.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		var p dnsmessage.Parser
		h, err := p.Start(buf[:n])
		if err != nil {
			continue
		}
		q, err := p.Question()
		if err != nil {
			continue
		}
		name := strings.ToLower(q.Name.String())

		z.mu.Lock()
		z.arrivals = append(z.arrivals, arrival{name: name, at: time.Now()})
		mode := z.mode
		if z.failing != nil && z.failing() {
			mode = zoneServfail
		}
		recs, found := z.records[name]
		z.mu.Unlock()

		if mode == zoneSilent {
			continue
		}
		rh := dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionDesired: h.RecursionDesired, RecursionAvailable: true}
		switch {
		case mode == zoneServfail:
			rh.RCode = dnsmessage.RCodeServerFailure
		case !found:
			rh.RCode = dnsmessage.RCodeNameError
		}
		b := dnsmessage.NewBuilder(make([]byte, 0, 1232), rh)
		b.EnableCompression()
		if err := b.StartQuestions(); err != nil {
			continue
		}
		if err := b.Question(q); err != nil {
			continue
		}
		if err := b.StartAnswers(); err != nil {
			continue
		}
		if mode == zoneAnswer && found && q.Type == dnsmessage.TypeTXT {
			for _, r := range recs {
				if err := b.TXTResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60},
					dnsmessage.TXTResource{TXT: r}); err != nil {
					z.t.Errorf("НЕ ВЫПОЛНИЛОСЬ: зона не собрала ответ %s: %v", name, err)
				}
			}
		}
		msg, err := b.Finish()
		if err != nil {
			continue
		}
		_, _ = z.pc.WriteTo(msg, from)
	}
}

// split255 режет значение записи на строки TXT: запись DKIM приходит
// несколькими строками, и проверка обязана их склеить (§12а).
func split255(s string, size int) []string {
	var out []string
	for len(s) > size {
		out = append(out, s[:size])
		s = s[size:]
	}
	return append(out, s)
}

// TestZoneFixtureAnswersLikeARealServer — фикстура доказывается раньше, чем ею
// судят: зона отдаёт склеенную запись TXT, на имя без записей — «записи нет»,
// в режиме SERVFAIL — временный отказ, в режиме молчания — истечение срока.
// Испытуемого проба не зовёт: её красное — сломанная фикстура, а не предмет.
func TestZoneFixtureAnswersLikeARealServer(t *testing.T) {
	z := startZone(t)
	z.set("rec.example.test.", []string{"v=DKIM1; ", "p=AAAA"})
	res := z.resolver()
	ctx := context.Background()

	got, err := res.LookupTXT(ctx, "rec.example.test.")
	if err != nil || len(got) != 1 || got[0] != "v=DKIM1; p=AAAA" {
		t.Fatalf("зона не отдала склеенную запись: %q, %v", got, err)
	}
	var dnsErr *net.DNSError
	if _, err := res.LookupTXT(ctx, "absent.example.test."); !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("имя без записей не дало IsNotFound: %v", err)
	}
	z.setMode(zoneServfail)
	if _, err := res.LookupTXT(ctx, "rec.example.test."); !errors.As(err, &dnsErr) || dnsErr.IsNotFound || !(dnsErr.IsTemporary || dnsErr.IsTimeout) {
		t.Fatalf("SERVFAIL не дал временного отказа: %v", err)
	}
	z.setMode(zoneSilent)
	sctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if _, err := res.LookupTXT(sctx, "rec.example.test."); err == nil {
		t.Fatal("молчащая зона дала ответ")
	}
	if n := len(z.seen()); n < 4 {
		t.Fatalf("зона записала %d запросов, ожидалось не меньше 4", n)
	}
}
