// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package cmuxh2 возвращает HTTP/2-соединение, которое мультиплексор порта уже
// «начал» за сервер, в состояние, пригодное для настоящего HTTP/2-сервера.
//
// ПРЕДМЕТ (kacho#3125). Внешние слушатели края делят порт между gRPC и REST
// мультиплексором cmux. Чтобы узнать gRPC, матчер обязан прочитать заголовки
// первого потока, а клиенты gRPC не шлют их, пока не получат SETTINGS сервера, —
// поэтому матчер САМ отвечает клиенту пустым SETTINGS на каждый его SETTINGS
// (cmux.HTTP2MatchHeaderFieldSendSettings). Когда соединение оказывается не
// gRPC, оно уходит к REST-серверу, но клиент уже получил чужой SETTINGS и
// подтверждает его. Сервер HTTP/2 стандартной библиотеки этого SETTINGS не
// посылал и на его подтверждение рвёт соединение («ack_mystery», PROTOCOL_ERROR).
//
// Фильтр выбрасывает из входящего потока ровно столько подтверждений SETTINGS,
// сколько SETTINGS ответил клиенту матчер: по одному на каждый SETTINGS клиента
// до первого HEADERS — именно их матчер прочитал и на них ответил. Клиент
// подтверждает SETTINGS в порядке получения, а SETTINGS матчера ушли в
// соединение раньше любых байтов настоящего сервера, поэтому первые N
// подтверждений — его. Дальше поток проходит без разбора.
//
// Соединение, не начинающееся преамбулой HTTP/2 (HTTP/1.1), проходит как есть:
// расхождение с преамбулой видно на втором байте любой строки запроса.
package cmuxh2

import (
	"net"
)

// clientPreface — преамбула клиента HTTP/2 (RFC 9113 §3.4).
const clientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

const (
	frameHeaderLen = 9

	frameHeaders  = 0x1
	frameSettings = 0x4

	flagSettingsAck = 0x1
)

type state uint8

const (
	statePreface state = iota // сверяем преамбулу
	stateHeader               // копим заголовок кадра
	statePayload              // пропускаем тело кадра
	statePass                 // разбор окончен, поток проходит как есть
)

// filter — разбор входящего потока одного соединения. Не потокобезопасен:
// соединение читает одна горутина сервера.
type filter struct {
	st          state
	prefaceAt   int
	hdr         [frameHeaderLen]byte
	hdrAt       int
	payloadLeft int // длина кадра — 24 бита, в int помещается всегда
	headersSeen bool
	// owed — подтверждения SETTINGS, ещё не пришедшие на SETTINGS матчера.
	owed int
}

// process дописывает к out байты in, которые должны дойти до сервера.
func (f *filter) process(in, out []byte) []byte {
	for len(in) > 0 {
		switch f.st {
		case statePass:
			return append(out, in...)
		case statePreface:
			if in[0] != clientPreface[f.prefaceAt] {
				f.st = statePass
				continue
			}
			out = append(out, in[0])
			in = in[1:]
			f.prefaceAt++
			if f.prefaceAt == len(clientPreface) {
				f.st = stateHeader
			}
		case stateHeader:
			n := copy(f.hdr[f.hdrAt:], in)
			f.hdrAt += n
			in = in[n:]
			if f.hdrAt < frameHeaderLen {
				continue
			}
			f.hdrAt = 0
			length := uint32(f.hdr[0])<<16 | uint32(f.hdr[1])<<8 | uint32(f.hdr[2])
			if !f.answeredAck(f.hdr[3], f.hdr[4], length) {
				out = append(out, f.hdr[:]...)
			}
			f.payloadLeft = int(length)
			f.st = statePayload
			if length == 0 {
				f.frameDone()
			}
		case statePayload:
			n := min(len(in), f.payloadLeft)
			out = append(out, in[:n]...)
			in = in[n:]
			f.payloadLeft -= n
			if f.payloadLeft == 0 {
				f.frameDone()
			}
		}
	}
	return out
}

// answeredAck учитывает заголовок кадра и сообщает, что кадр — подтверждение
// SETTINGS матчера и до сервера доходить не должен.
func (f *filter) answeredAck(typ, flags byte, length uint32) bool {
	switch typ {
	case frameHeaders:
		f.headersSeen = true
	case frameSettings:
		if flags&flagSettingsAck == 0 {
			if !f.headersSeen {
				f.owed++
			}
			return false
		}
		if length == 0 && f.owed > 0 {
			f.owed--
			return true
		}
	}
	return false
}

func (f *filter) frameDone() {
	if f.headersSeen && f.owed == 0 {
		f.st = statePass
		return
	}
	f.st = stateHeader
}

// Conn — соединение с фильтром входящего потока.
type Conn struct {
	net.Conn
	f       filter
	pending []byte
	buf     []byte
	err     error
}

// NetConn отдаёт обёрнутое соединение: читатели происхождения слушателя и
// состояния TLS разворачивают обёртки по этому методу.
func (c *Conn) NetConn() net.Conn { return c.Conn }

func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if len(c.pending) > 0 {
			n := copy(p, c.pending)
			c.pending = c.pending[n:]
			return n, nil
		}
		if c.err != nil {
			return 0, c.err
		}
		if c.f.st == statePass {
			return c.Conn.Read(p)
		}
		if cap(c.buf) < len(p) {
			c.buf = make([]byte, len(p))
		}
		n, err := c.Conn.Read(c.buf[:len(p)])
		c.pending = c.f.process(c.buf[:n], c.pending[:0])
		c.err = err
	}
}

// Listener оборачивает каждое принятое соединение фильтром. Предназначен ТОЛЬКО
// для под-слушателя, чьи соединения прошли через HTTP/2-матчер с ответом
// SETTINGS: на соединении, которому SETTINGS никто не посылал, фильтр выбросил
// бы подтверждение настоящего сервера.
func Listener(l net.Listener) net.Listener { return &listener{Listener: l} }

type listener struct{ net.Listener }

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &Conn{Conn: c}, nil
}
