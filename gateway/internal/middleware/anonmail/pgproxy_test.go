// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pgProxy — TCP-посредник пробы между пулом ограничителя и базой (CX2-46). Он
// узнаёт сообщение COMMIT решения (простой протокол, `Q` с текстом `commit`) и
// по режиму доставляет, задерживает или отбрасывает его; запросы отмены
// (CancelRequest) не пропускает никогда — иначе сервер прервал бы задержанный
// оператор и клиент прочёл бы настоящий отказ, то есть исход был бы известен.
type pgProxy struct {
	t      testing.TB
	ln     net.Listener
	target string

	mu sync.Mutex
	// commitAction — что делать с COMMIT: deliver · delay · drop · blackhole.
	commitAction string
	commitDelay  time.Duration
	// refuse — новые соединения закрываются сразу, существующие рвутся.
	refuse bool
	// muteUntil — ответы сервера клиенту отбрасываются до этого момента на
	// всех соединениях, включая новые.
	muteUntil time.Time
	// acceptDelay — задержка установки нового соединения (холодный пул).
	acceptDelay time.Duration
	conns       map[net.Conn]bool

	commits        atomic.Int64
	cancelsDropped atomic.Int64
	delivered      chan struct{}
}

func newPGProxy(t testing.TB, dsn string) (*pgProxy, string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &pgProxy{t: t, ln: ln, target: u.Host, commitAction: "deliver", conns: map[net.Conn]bool{},
		delivered: make(chan struct{}, 16)}
	go p.serve()
	t.Cleanup(func() { _ = ln.Close(); p.dropAll() })
	u.Host = ln.Addr().String()
	return p, u.String()
}

func (p *pgProxy) set(f func(p *pgProxy)) {
	p.mu.Lock()
	f(p)
	p.mu.Unlock()
}

func (p *pgProxy) dropAll() {
	p.mu.Lock()
	for c := range p.conns {
		_ = c.Close()
	}
	p.conns = map[net.Conn]bool{}
	p.mu.Unlock()
}

func (p *pgProxy) muteFor(d time.Duration) {
	p.mu.Lock()
	p.muteUntil = time.Now().Add(d)
	p.mu.Unlock()
}

func (p *pgProxy) muted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Now().Before(p.muteUntil)
}

func (p *pgProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go p.handle(c)
	}
}

func (p *pgProxy) track(c net.Conn) {
	p.mu.Lock()
	p.conns[c] = true
	p.mu.Unlock()
}

func (p *pgProxy) handle(client net.Conn) {
	p.mu.Lock()
	refuse, delay := p.refuse, p.acceptDelay
	p.mu.Unlock()
	if refuse {
		_ = client.Close()
		return
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	// Стартовое сообщение — без байта типа: длина, затем код.
	head := make([]byte, 8)
	if _, err := io.ReadFull(client, head); err != nil {
		_ = client.Close()
		return
	}
	n := int(binary.BigEndian.Uint32(head[:4]))
	if code := binary.BigEndian.Uint32(head[4:8]); code == 80877102 {
		// Запрос отмены не пропускается.
		p.cancelsDropped.Add(1)
		_ = client.Close()
		return
	}
	rest := make([]byte, n-8)
	if _, err := io.ReadFull(client, rest); err != nil {
		_ = client.Close()
		return
	}
	server, err := net.Dial("tcp", p.target)
	if err != nil {
		_ = client.Close()
		return
	}
	p.track(client)
	p.track(server)
	if _, err := server.Write(append(head, rest...)); err != nil {
		_ = client.Close()
		_ = server.Close()
		return
	}
	go func() {
		buf := make([]byte, 32<<10)
		for {
			k, err := server.Read(buf)
			if k > 0 && !p.muted() {
				if _, werr := client.Write(buf[:k]); werr != nil {
					// Клиент ушёл — сервер дочитываем, чтобы доставленная
					// фиксация исполнилась до конца.
					continue
				}
			}
			if err != nil {
				_ = client.Close()
				return
			}
		}
	}()
	p.pumpClient(client, server)
}

// pumpClient читает сообщения клиента и пересылает их серверу; COMMIT — по
// режиму.
func (p *pgProxy) pumpClient(client, server net.Conn) {
	hdr := make([]byte, 5)
	for {
		if _, err := io.ReadFull(client, hdr); err != nil {
			// Клиент закрыл соединение; серверное держит тот, кто задерживает
			// COMMIT, либо закрываем сразу.
			return
		}
		body := make([]byte, int(binary.BigEndian.Uint32(hdr[1:]))-4)
		if _, err := io.ReadFull(client, body); err != nil {
			return
		}
		msg := append(append([]byte(nil), hdr...), body...)
		if hdr[0] == 'Q' && strings.EqualFold(strings.TrimRight(string(body), "\x00"), "commit") {
			p.commits.Add(1)
			p.mu.Lock()
			action, d := p.commitAction, p.commitDelay
			p.mu.Unlock()
			switch action {
			case "drop", "dropMute":
				// Сервер не получает ни COMMIT, ни что-либо после него на этом
				// соединении (обрыв связи): транзакция висит, пока сервер не
				// снимет её пределом простоя.
				if action == "dropMute" {
					p.muteFor(d)
				}
				_, _ = io.Copy(io.Discard, client)
				return
			case "delay":
				// Синхронно: следующие сообщения клиента (в том числе
				// Terminate после его отказа) уходят серверу только ПОСЛЕ
				// доставленного COMMIT — иначе сервер закрыл бы сессию откатом.
				time.Sleep(d)
				_, _ = server.Write(msg)
				p.delivered <- struct{}{}
				continue
			case "outage":
				// База недоступна всё окно: новые соединения отвергаются, ответы
				// существующих теряются; COMMIT уходит серверу после окна.
				p.mu.Lock()
				p.refuse = true
				p.mu.Unlock()
				p.muteFor(d)
				time.Sleep(d)
				_, _ = server.Write(msg)
				p.delivered <- struct{}{}
				continue
			case "blackhole":
				p.muteFor(d)
				_, _ = server.Write(msg)
				p.delivered <- struct{}{}
				continue
			}
		}
		if _, err := server.Write(msg); err != nil {
			return
		}
	}
}
