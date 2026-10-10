// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cmuxh2

import (
	"bytes"
	"io"
	"net"
	"testing"
)

func frame(typ, flags byte, payload ...byte) []byte {
	n := len(payload)
	h := []byte{byte(n >> 16), byte(n >> 8), byte(n), typ, flags, 0, 0, 0, 0}
	return append(h, payload...)
}

var (
	settings    = frame(frameSettings, 0, 0, 3, 0, 0, 0, 100) // MAX_CONCURRENT_STREAMS=100
	settingsAck = frame(frameSettings, flagSettingsAck)
	// settingsAckWithPayload — ACK с телом: сервер ответит FRAME_SIZE_ERROR,
	// фильтр же обязан пропустить кадр целиком.
	settingsAckWithPayload = frame(frameSettings, flagSettingsAck, 0, 3, 0, 0, 0, 1)
	headers                = frame(frameHeaders, 0x4|0x1, 0x82, 0x86, 0x84) // END_HEADERS|END_STREAM
	windowUp               = frame(0x8, 0, 0, 0, 0x10, 0)
	ping                   = frame(0x6, 0, 1, 2, 3, 4, 5, 6, 7, 8)
)

// frameHeaderOfLength — заголовок кадра заявленной длины без тела.
func frameHeaderOfLength(n int, typ byte) []byte {
	return []byte{byte(n >> 16), byte(n >> 8), byte(n), typ, 0, 0, 0, 0, 0}
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// run пропускает поток через фильтр кусками заданного размера — граница куска
// не должна менять результат.
func run(t *testing.T, in []byte, chunk int) []byte {
	t.Helper()
	var f filter
	var out []byte
	for len(in) > 0 {
		n := min(chunk, len(in))
		out = f.process(in[:n], out)
		in = in[n:]
	}
	return out
}

func TestFilter(t *testing.T) {
	pre := []byte(clientPreface)
	cases := []struct {
		name    string
		in, out []byte
	}{
		{
			// Предмет: подтверждение SETTINGS матчера снято, подтверждение
			// настоящего сервера (второе) дошло.
			name: "ack after headers: first dropped, second kept",
			in:   cat(pre, settings, windowUp, headers, settingsAck, ping, settingsAck),
			out:  cat(pre, settings, windowUp, headers, ping, settingsAck),
		},
		{
			// Клиент, ждущий SETTINGS сервера до заголовков: подтверждение
			// приходит раньше HEADERS — оно тоже матчеру.
			name: "ack before headers",
			in:   cat(pre, settings, settingsAck, headers, settingsAck),
			out:  cat(pre, settings, headers, settingsAck),
		},
		{
			// Предмет: SETTINGS клиента после HEADERS матчер не читал и не
			// отвечал — в долг он не идёт, пока долг ещё не погашен и фильтр
			// разбирает поток. Снимается ровно одно подтверждение (матчера),
			// подтверждение настоящего сервера на второй SETTINGS доходит.
			name: "settings after headers are not owed",
			in:   cat(pre, settings, headers, settings, settingsAck, settingsAck),
			out:  cat(pre, settings, headers, settings, settingsAck),
		},
		{
			// Законный близнец: долг погашен до второго SETTINGS — фильтр уже
			// в проходе, поток идёт без разбора.
			name: "settings after the debt is paid pass through",
			in:   cat(pre, settings, headers, settingsAck, settings, settingsAck),
			out:  cat(pre, settings, headers, settings, settingsAck),
		},
		{
			// Предмет: подтверждение SETTINGS с телом — не ответ матчеру (RFC
			// 9113 §6.5: у ACK длина 0), оно доходит до сервера целиком, а
			// снимается ACK нулевой длины. Снять один заголовок без тела значило
			// бы отдать серверу тело как начало следующего кадра.
			name: "ack with a payload is not the matcher's",
			in:   cat(pre, settings, settingsAckWithPayload, headers, settingsAck),
			out:  cat(pre, settings, settingsAckWithPayload, headers),
		},
		{
			// Два SETTINGS до заголовков — два ответа матчера, два снятых.
			name: "two settings before headers",
			in:   cat(pre, settings, settings, headers, settingsAck, settingsAck, settingsAck),
			out:  cat(pre, settings, settings, headers, settingsAck),
		},
		{
			// Законный близнец: SETTINGS до заголовков не было — нечего снимать.
			name: "no settings before headers drops nothing",
			in:   cat(pre, headers, settingsAck),
			out:  cat(pre, headers, settingsAck),
		},
		{
			// Подтверждение, на которое матчер ничего не посылал, доходит:
			// снимается только долг, а не любой ACK до заголовков.
			name: "ack with nothing owed passes",
			in:   cat(pre, settingsAck, settings, headers, settingsAck),
			out:  cat(pre, settingsAck, settings, headers),
		},
		{
			// Законный близнец: HTTP/1.1 проходит как есть.
			name: "http1 passes",
			in:   []byte("PRIVATE / HTTP/1.1\r\nHost: x\r\n\r\n" + string(settingsAck)),
			out:  []byte("PRIVATE / HTTP/1.1\r\nHost: x\r\n\r\n" + string(settingsAck)),
		},
		{
			name: "http1 GET passes",
			in:   []byte("GET /iam/v1/me HTTP/1.1\r\nHost: x\r\n\r\n"),
			out:  []byte("GET /iam/v1/me HTTP/1.1\r\nHost: x\r\n\r\n"),
		},
	}
	for _, tc := range cases {
		for _, chunk := range []int{1, 2, 7, 9, 10, 24, 33, 4096} {
			got := run(t, tc.in, chunk)
			if !bytes.Equal(got, tc.out) {
				t.Errorf("%s (кусок %d):\n got %q\nwant %q", tc.name, chunk, got, tc.out)
			}
		}
	}
}

// Conn: отданное сервером совпадает с отфильтрованным потоком, ошибка конца
// потока доезжает после последнего байта, а соединение разворачивается.
func TestConnReadsFilteredStreamAndUnwraps(t *testing.T) {
	pre := []byte(clientPreface)
	in := cat(pre, settings, headers, settingsAck, ping, settingsAck)
	want := cat(pre, settings, headers, ping, settingsAck)

	client, server := net.Pipe()
	go func() {
		for len(in) > 0 {
			n := min(5, len(in))
			_, _ = client.Write(in[:n])
			in = in[n:]
		}
		_ = client.Close()
	}()
	c := &Conn{Conn: server}
	if c.NetConn() != server {
		t.Fatal("NetConn не отдаёт обёрнутое соединение — читатели происхождения его не развернут")
	}
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// recordingConn запоминает наибольший запрос на чтение к обёрнутому соединению.
type recordingConn struct {
	net.Conn
	maxAsked int
}

func (r *recordingConn) Read(p []byte) (int, error) {
	r.maxAsked = max(r.maxAsked, len(p))
	return r.Conn.Read(p)
}

// Предмет: пока фильтр разбирает поток, он не держит буфер размера запроса
// сервера. Сервер HTTP/2 читает кадр целиком в буфер своей длины; фильтр,
// заводящий копию такого же размера, удваивал бы память соединения до
// аутентификации. Наблюдаемое — сколько фильтр просит у соединения за раз и что
// после разбора у него не осталось своих буферов.
func TestConnHoldsNoBufferOfTheReadersSize(t *testing.T) {
	const big = 1<<20 - 1
	pre := []byte(clientPreface)
	// Заголовок длинного кадра до HEADERS, его тело, затем HEADERS: после них
	// долг погашен ACK и фильтр уходит в проход.
	body := bytes.Repeat([]byte{0xab}, big)
	bigFrame := append(frameHeaderOfLength(big, 0xfa), body...)
	in := cat(pre, settings, bigFrame, headers, settingsAck, ping)
	want := cat(pre, settings, bigFrame, headers, ping)

	client, server := net.Pipe()
	go func() {
		_, _ = client.Write(in)
		_ = client.Close()
	}()
	rec := &recordingConn{Conn: server}
	c := &Conn{Conn: rec}
	var got []byte
	p := make([]byte, big)
	maxBuf := 0
	for {
		n, err := c.Read(p)
		got = append(got, p[:n]...)
		maxBuf = max(maxBuf, cap(c.buf), cap(c.pending))
		if err != nil {
			break
		}
	}
	t.Logf("запрос сервера %d Б · наибольший запрос фильтра к соединению %d Б · наибольший свой буфер %d Б",
		len(p), rec.maxAsked, maxBuf)
	if !bytes.Equal(got, want) {
		t.Fatalf("поток искажён: получено %d Б, ждали %d", len(got), len(want))
	}
	if maxBuf > 2*filterReadMax {
		t.Fatalf("фильтр завёл буфер %d Б — он растёт по запросу сервера, а не ограничен %d", maxBuf, filterReadMax)
	}
	if c.f.st != statePass || c.buf != nil || c.pending != nil {
		t.Fatalf("после разбора фильтр держит свои буферы: состояние %d, buf %d Б, pending %d Б", c.f.st, cap(c.buf), cap(c.pending))
	}
}
