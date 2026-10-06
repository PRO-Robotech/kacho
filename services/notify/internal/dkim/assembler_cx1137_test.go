// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkim_test

// assembler_cx1137_test.go — условие CX1-137 (R44-3; замысел §12а «Части
// тела — 7bit до подписи»), сторона СБОРЩИКА: окончательные байты письма —
// 7bit до подписи. Каждая текстовая часть: все октеты < 0x80 и строки ≤ 998 —
// `7bit`, иначе — `quoted-printable`; `8bit` и `binary` сборщик не выпускает.
// Пакет `dkim` этот файл не называет: испытуемый здесь — сборщик `render`.
// Подпись над теми же байтами и её проверка — sign_cx1137_test.go.

import (
	"strings"
	"testing"
)

// TestDKIM_CX1137_CyrillicLetterIsSevenBitQuotedPrintable — письмо с
// кириллицей в теме и теле: каждый октет < 0x80, строк длиннее 998 нет,
// обе части — quoted-printable.
func TestDKIM_CX1137_CyrillicLetterIsSevenBitQuotedPrintable(t *testing.T) {
	msg := cyrillicLetter(t)
	if v := sevenBitViolations(msg); len(v) != 0 {
		t.Fatalf("письмо с кириллицей не 7bit: %s", strings.Join(v, "; "))
	}
	for _, p := range parts(t, msg) {
		if p.cte != "quoted-printable" {
			t.Fatalf("часть %s с октетами ≥ 0x80: Content-Transfer-Encoding %q, ожидался quoted-printable", p.mediaType, p.cte)
		}
	}
}

// TestDKIM_CX1137_AsciiLetterPartsAreSevenBit — близнец: ASCII-письмо со
// строками ≤ 998 — обе части `7bit`, письмо 7bit.
func TestDKIM_CX1137_AsciiLetterPartsAreSevenBit(t *testing.T) {
	msg := asciiLetter(t)
	if v := sevenBitViolations(msg); len(v) != 0 {
		t.Fatalf("ASCII-письмо не 7bit: %s", strings.Join(v, "; "))
	}
	for _, p := range parts(t, msg) {
		if p.cte != "7bit" {
			t.Fatalf("ASCII-часть %s (строка не длиннее %d): Content-Transfer-Encoding %q, ожидался 7bit", p.mediaType, maxLine(p.decoded), p.cte)
		}
	}
}
