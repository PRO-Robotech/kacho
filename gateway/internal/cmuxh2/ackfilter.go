// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package cmuxh2 — HTTP/2 за мультиплексором порта: матчер gRPC с ограниченной
// памятью (matcher.go) и фильтр, возвращающий соединение, которое матчер уже
// «начал» за сервер, в состояние, пригодное для настоящего HTTP/2-сервера.
//
// ПРЕДМЕТ (kacho#3125). Внешние слушатели края делят порт между gRPC и REST
// мультиплексором cmux. Чтобы узнать gRPC, матчер обязан прочитать заголовки
// первого потока, а клиенты gRPC не шлют их, пока не получат SETTINGS сервера, —
// поэтому матчер САМ отвечает клиенту пустым SETTINGS на каждый его SETTINGS
// (MatchHeaderFieldSendSettings). Когда соединение оказывается не
// gRPC, оно уходит к REST-серверу, но клиент уже получил чужой SETTINGS и
// подтверждает его. Сервер HTTP/2 стандартной библиотеки этого SETTINGS не
// посылал и на его подтверждение рвёт соединение («ack_mystery», PROTOCOL_ERROR).
//
// Фильтр выбрасывает из входящего потока по одному подтверждению SETTINGS на
// каждый SETTINGS клиента, прочитанный до первого HEADERS: матчер
// (MatchHeaderFieldSendSettings) отвечает именно на них. Клиент подтверждает
// SETTINGS в порядке получения, а SETTINGS матчера ушли в соединение раньше
// любых байтов настоящего сервера, поэтому первые N подтверждений — его. Дальше
// поток проходит без разбора.
//
// Счёт ведётся по прочитанному фильтром, а не по отправленному матчером. Они
// расходятся, когда матчер вышел раньше, чем прочитал все SETTINGS до HEADERS:
// по бюджету первого байта мультиплексора, по бюджету чтения
// (MatchHeaderBudget) либо на длинном кадре. Тогда фильтр снимает и
// подтверждение SETTINGS настоящего сервера. Это безвредно: сервер не получает
// подтверждения своих SETTINGS, у него остаётся лишь отметка о неподтверждённых
// параметрах, и меняется только код отказа при превышении числа потоков —
// для этого клиента.
//
// Соединение, не начинающееся преамбулой HTTP/2 (HTTP/1.1), проходит как есть:
// расхождение с преамбулой видно на первом расходящемся байте (у метода GET —
// на первом, у PRIVATE — на четвёртом).
//
// Память. Пока фильтр разбирает поток, он читает из соединения не больше
// filterReadMax за раз и держит не больше одного такого куска; перейдя в проход,
// отпускает свои буферы, и чтения идут прямо в буфер сервера. Соединение до
// аутентификации поэтому не стоит фильтру больше единиц КиБ, сколько бы ни
// просил сервер за одно чтение.
package cmuxh2

import (
	"net"

	"golang.org/x/net/http2"
)

// clientPreface — преамбула клиента HTTP/2 (RFC 9113 §3.4).
const clientPreface = http2.ClientPreface

const (
	frameHeaderLen = 9

	frameHeaders    = byte(http2.FrameHeaders)
	frameSettings   = byte(http2.FrameSettings)
	flagSettingsAck = byte(http2.FlagSettingsAck)
)

// filterReadMax — потолок одного чтения из соединения, пока фильтр разбирает
// поток.
const filterReadMax = 4 << 10

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
			c.buf, c.pending = nil, nil
			return c.Conn.Read(p)
		}
		if c.buf == nil {
			c.buf = make([]byte, filterReadMax)
		}
		n, err := c.Conn.Read(c.buf[:min(len(p), filterReadMax)])
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
