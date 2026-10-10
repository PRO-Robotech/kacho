// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cmuxh2

import (
	"bytes"

	"golang.org/x/net/http2"
)

// Байты кадров HTTP/2 для проб матчера — собираются руками, чтобы вход пробы
// не зависел от кодировщика, которым пользуется сам матчер.

const (
	// clientPreface — преамбула клиента HTTP/2 (RFC 9113 §3.4).
	clientPreface   = http2.ClientPreface
	frameHeaders    = byte(http2.FrameHeaders)
	frameSettings   = byte(http2.FrameSettings)
	flagSettingsAck = byte(http2.FlagSettingsAck)
)

func frame(typ, flags byte, payload ...byte) []byte {
	n := len(payload)
	h := []byte{byte(n >> 16), byte(n >> 8), byte(n), typ, flags, 0, 0, 0, 0}
	return append(h, payload...)
}

var (
	settings    = frame(frameSettings, 0, 0, 3, 0, 0, 0, 100) // MAX_CONCURRENT_STREAMS=100
	settingsAck = frame(frameSettings, flagSettingsAck)
	windowUp    = frame(0x8, 0, 0, 0, 0x10, 0)
)

// frameHeaderOfLength — заголовок кадра заявленной длины без тела.
func frameHeaderOfLength(n int, typ byte) []byte {
	return []byte{byte(n >> 16), byte(n >> 8), byte(n), typ, 0, 0, 0, 0, 0}
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
