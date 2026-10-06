// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp

import "testing"

// Расширенный код берётся только в форме RFC 3463 и только когда его класс
// совпадает с классом кода ответа: иначе слово текста («mailbox», «4.3.0» у 550)
// легло бы ключом таблицы. Близнец каждого отказа — законная форма той же длины.
func TestEnhancedCode_OnlyTheRFC3463FormOfTheSameClass(t *testing.T) {
	cases := []struct {
		code int
		text string
		want string
	}{
		{550, "5.1.1 no such user", "5.1.1"},
		{550, "5.7.1 policy refusal", "5.7.1"},
		{451, "4.3.0 try later", "4.3.0"},
		{554, "5.123.456 long parts", "5.123.456"},
		{535, "5.7.8 bad\nsecond line", "5.7.8"},
		{550, "mailbox unavailable", ""},
		{550, "4.3.0 class differs from the reply", ""},
		{550, "5.1 two parts", ""},
		{550, "5.1.1.1 four parts", ""},
		{550, "5.1234.1 subject too long", ""},
		{550, "5..1 empty subject", ""},
		{550, "5.a.1 not a digit", ""},
		{699, "6.0.0 class outside RFC 3463", ""},
		{550, "", ""},
	}
	for _, k := range cases {
		if got := enhancedCode(k.code, k.text); got != k.want {
			t.Errorf("enhancedCode(%d, %q) = %q, ожидалось %q", k.code, k.text, got, k.want)
		}
	}
}
