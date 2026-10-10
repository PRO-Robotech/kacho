// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cmuxh2

import (
	"io"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// MatchHeaderBudget — сколько байт соединения матчер читает, решая, gRPC ли
// оно. Законный клиент gRPC до первого HEADERS шлёт преамбулу, SETTINGS,
// WINDOW_UPDATE и блок заголовков — сотни байт, с токеном и подписью запроса —
// единицы КиБ. Соединение, не назвавшее content-type в пределах бюджета, не
// gRPC и уходит дальше по мультиплексору.
const MatchHeaderBudget = 64 << 10

// matchReadFrameSize — наибольший кадр, который матчер читает. До SETTINGS
// сервера клиент не вправе слать кадр длиннее 16 КиБ (RFC 9113 §4.2,
// SETTINGS_MAX_FRAME_SIZE по умолчанию), а матчер отвечает пустым SETTINGS и
// умолчания не меняет.
const matchReadFrameSize = 16 << 10

// MatchHeaderFieldSendSettings — матчер мультиплексора: соединение HTTP/2, чей
// первый блок заголовков несёт поле name со значением value. На каждый
// SETTINGS клиента (не подтверждение) он отвечает пустым SETTINGS — клиенты
// gRPC не шлют заголовков, пока не получат SETTINGS сервера.
//
// Повторяет cmux.HTTP2MatchHeaderFieldSendSettings решением и записанными
// байтами (TestMatcherAgreesWithCmux) с двумя отличиями — оба про память,
// которую соединение занимает ДО аутентификации:
//   - кадр длиннее matchReadFrameSize матчер не читает, а решает «не совпало»;
//     у cmux буфер кадра выделяется по длине из его заголовка — до 16 МиБ на
//     соединение, приславшее один заголовок кадра;
//   - соединение читается не дальше MatchHeaderBudget: у cmux каждый
//     прочитанный байт остаётся в буфере мультиплексора, а блок заголовков
//     ничем не ограничен.
func MatchHeaderFieldSendSettings(name, value string) func(io.Writer, io.Reader) bool {
	return func(w io.Writer, r io.Reader) bool {
		return matchField(w, &io.LimitedReader{R: r, N: MatchHeaderBudget}, name, value)
	}
}

func matchField(w io.Writer, r io.Reader, name, value string) (matched bool) {
	if !hasPreface(r) {
		return false
	}
	done := false
	framer := http2.NewFramer(w, r)
	framer.SetMaxReadFrameSize(matchReadFrameSize)
	hdec := hpack.NewDecoder(4<<10, func(hf hpack.HeaderField) {
		if hf.Name == name {
			done = true
			if hf.Value == value {
				matched = true
			}
		}
	})
	for {
		f, err := framer.ReadFrame()
		if err != nil {
			return false
		}
		switch f := f.(type) {
		case *http2.SettingsFrame:
			if f.IsAck() {
				break
			}
			if err := framer.WriteSettings(); err != nil {
				return false
			}
		case *http2.ContinuationFrame:
			if _, err := hdec.Write(f.HeaderBlockFragment()); err != nil {
				return false
			}
			done = done || f.Flags.Has(http2.FlagContinuationEndHeaders)
		case *http2.HeadersFrame:
			if _, err := hdec.Write(f.HeaderBlockFragment()); err != nil {
				return false
			}
			done = done || f.Flags.Has(http2.FlagHeadersEndHeaders)
		}
		if done {
			return matched
		}
	}
}

// hasPreface читает преамбулу по мере прихода и отказывает на первом
// расходящемся байте — короткий запрос HTTP/1.1 не ждёт, пока придёт 24 байта.
func hasPreface(r io.Reader) bool {
	var b [len(http2.ClientPreface)]byte
	last := 0
	for {
		n, err := r.Read(b[last:])
		if err != nil {
			return false
		}
		last += n
		eq := string(b[:last]) == http2.ClientPreface[:last]
		if last == len(b) {
			return eq
		}
		if !eq {
			return false
		}
	}
}
