// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkim_test

// sign_cx1137_test.go — условие CX1-137, сторона ПОДПИСИ: подпись считается
// над 7bit-байтами сборщика и сама их 7bit-формы не нарушает; независимая
// проверка — pass. Ретранслятору нечего перекодировать.

import (
	"strings"
	"testing"
)

// TestDKIM_CX1137_SignedCyrillicLetterStaysSevenBitAndPasses — письмо с
// кириллицей в теме и теле после подписи: каждый октет < 0x80, строк длиннее
// 998 нет, части quoted-printable, проверка — pass.
func TestDKIM_CX1137_SignedCyrillicLetterStaysSevenBitAndPasses(t *testing.T) {
	a, _ := keys(t)
	z := soundZone(t)
	signed := sign(t, newSigner(t, &switchablePairs{p: pairOf(selA, a)}), cyrillicLetter(t))
	if v := sevenBitViolations(signed); len(v) != 0 {
		t.Fatalf("подписанное письмо не 7bit: %s", strings.Join(v, "; "))
	}
	for _, p := range parts(t, signed) {
		if p.cte != "quoted-printable" {
			t.Fatalf("часть %s: Content-Transfer-Encoding %q, ожидался quoted-printable", p.mediaType, p.cte)
		}
	}
	requirePass(t, z, signed)
}

// TestDKIM_CX1137_SignedAsciiLetterIsSevenBitAndPasses — близнец: ASCII-письмо
// после подписи — части 7bit, проверка — pass.
func TestDKIM_CX1137_SignedAsciiLetterIsSevenBitAndPasses(t *testing.T) {
	a, _ := keys(t)
	z := soundZone(t)
	signed := sign(t, newSigner(t, &switchablePairs{p: pairOf(selA, a)}), asciiLetter(t))
	if v := sevenBitViolations(signed); len(v) != 0 {
		t.Fatalf("подписанное ASCII-письмо не 7bit: %s", strings.Join(v, "; "))
	}
	for _, p := range parts(t, signed) {
		if p.cte != "7bit" {
			t.Fatalf("ASCII-часть %s: Content-Transfer-Encoding %q, ожидался 7bit", p.mediaType, p.cte)
		}
	}
	requirePass(t, z, signed)
}
