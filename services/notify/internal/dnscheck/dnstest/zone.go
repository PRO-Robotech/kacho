// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package dnstest — двойник зоны DNS испытания и резолвер на него (замысел
// issue-2915 §12а «Процессные пробы и integration-старты»). Им пользуется
// КАЖДЫЙ in-process старт notify: страж DNS установки стоит в порядке подъёма,
// и старт без зоны судил бы резолвер машины прогона, а не предмет пробы.
//
// Сеть настоящая: UDP-сервер на петле, ответы — настоящие сообщения DNS
// (golang.org/x/net/dns/dnsmessage). Резолвер — *net.Resolver с PreferGo и
// Dial на адрес зоны: разбор ответа, склейка строк TXT и род ошибки — те же,
// что в бою. Зона не снисходительнее настоящей: имени без записей она отвечает
// NXDOMAIN, а не пустым «успехом».
package dnstest

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Mode — как зона отвечает.
type Mode int

const (
	// Answer — записи зоны (имя без записей — NXDOMAIN).
	Answer Mode = iota
	// Servfail — SERVFAIL на каждый запрос.
	Servfail
	// Silent — запрос принят, ответа нет.
	Silent
)

// Arrival — запрос, пришедший в зону.
type Arrival struct {
	Name string
	At   time.Time
}

// Zone — зона испытания.
type Zone struct {
	t  testing.TB
	pc net.PacketConn

	mu       sync.Mutex
	records  map[string][][]string // абсолютное имя (нижний регистр) → записи TXT → строки записи
	mode     Mode
	failing  func() bool
	arrivals []Arrival
}

// Start поднимает зону на петле; снимается по окончании пробы.
func Start(t testing.TB) *Zone {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: зона испытания не поднята: %v", err)
	}
	z := &Zone{t: t, pc: pc, records: map[string][][]string{}}
	done := make(chan struct{})
	go func() { defer close(done); z.serve() }()
	t.Cleanup(func() { _ = pc.Close(); <-done })
	return z
}

// Set заменяет записи имени (каждая запись — строки TXT); без записей —
// снимает имя из зоны.
func (z *Zone) Set(name string, records ...[]string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	name = strings.ToLower(name)
	if records == nil {
		delete(z.records, name)
		return
	}
	z.records[name] = records
}

// SetMode меняет режим ответа.
func (z *Zone) SetMode(m Mode) {
	z.mu.Lock()
	z.mode = m
	z.mu.Unlock()
}

// SetFailing — при не-nil и истине функции — SERVFAIL поверх режима.
func (z *Zone) SetFailing(f func() bool) {
	z.mu.Lock()
	z.failing = f
	z.mu.Unlock()
}

// Seen — запросы, пришедшие в зону, в порядке прихода.
func (z *Zone) Seen() []Arrival {
	z.mu.Lock()
	defer z.mu.Unlock()
	return append([]Arrival(nil), z.arrivals...)
}

// Resolver — резолвер, каждый запрос которого уходит в эту зону.
func (z *Zone) Resolver() *net.Resolver {
	addr := z.pc.LocalAddr().String()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

// PublishSound публикует исправные записи Р19 домена (NTF1-P01): DKIM
// селектора с ключом pub (несколькими строками TXT), DMARC `p=reject`, SPF `-all`.
func (z *Zone) PublishSound(domain, selector string, pub *rsa.PublicKey) {
	z.t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		z.t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: SPKI записи DKIM: %v", err)
	}
	domain = strings.TrimSuffix(domain, ".")
	z.Set(selector+"._domainkey."+domain+".", Split("v=DKIM1; k=rsa; p="+base64.StdEncoding.EncodeToString(der), 200))
	z.Set("_dmarc."+domain+".", []string{"v=DMARC1; p=reject"})
	z.Set(domain+".", []string{"v=spf1 ip4:192.0.2.0/24 -all"})
}

// Split режет значение записи на строки TXT по size октетов: запись DKIM
// приходит несколькими строками, и проверка обязана их склеить.
func Split(s string, size int) []string {
	var out []string
	for len(s) > size {
		out = append(out, s[:size])
		s = s[size:]
	}
	return append(out, s)
}

func (z *Zone) serve() {
	buf := make([]byte, 4096)
	for {
		n, from, err := z.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if msg, ok := z.answer(buf[:n]); ok {
			_, _ = z.pc.WriteTo(msg, from)
		}
	}
}

// answer — ответ на запрос; false — ответа нет (режим молчания либо запрос
// не разбирается).
func (z *Zone) answer(req []byte) ([]byte, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(req)
	if err != nil {
		return nil, false
	}
	q, err := p.Question()
	if err != nil {
		return nil, false
	}
	name := strings.ToLower(q.Name.String())

	z.mu.Lock()
	z.arrivals = append(z.arrivals, Arrival{Name: name, At: time.Now()})
	mode := z.mode
	if z.failing != nil && z.failing() {
		mode = Servfail
	}
	recs, found := z.records[name]
	z.mu.Unlock()

	if mode == Silent {
		return nil, false
	}
	rh := dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionDesired: h.RecursionDesired,
		RecursionAvailable: true}
	switch {
	case mode == Servfail:
		rh.RCode = dnsmessage.RCodeServerFailure
	case !found:
		rh.RCode = dnsmessage.RCodeNameError
	}
	b := dnsmessage.NewBuilder(make([]byte, 0, 1232), rh)
	b.EnableCompression()
	if b.StartQuestions() != nil || b.Question(q) != nil || b.StartAnswers() != nil {
		return nil, false
	}
	if mode == Answer && found && q.Type == dnsmessage.TypeTXT {
		for _, r := range recs {
			if err := b.TXTResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60},
				dnsmessage.TXTResource{TXT: r}); err != nil {
				z.t.Errorf("НЕ ВЫПОЛНИЛОСЬ: зона не собрала ответ %s: %v", name, err)
				return nil, false
			}
		}
	}
	msg, err := b.Finish()
	if err != nil {
		return nil, false
	}
	return msg, true
}
