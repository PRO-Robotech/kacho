// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkim_test

// sign_n15_test.go — подпись DKIM письма notify (полоса N15; приёмка NTF-1
// Р19 «Подпись», NTF1-P02, P03, P17; замысел §12а «Подпись»). Каждое
// утверждение о подписи судит НЕЗАВИСИМАЯ реализация (go-msgauth) по записи
// зоны испытания.

import (
	"bytes"
	"strings"
	"testing"
)

// TestDKIM_NTF1P02_SignaturePassesIndependentVerifier — подпись письма
// сборщика проходит независимую проверку; теги Р19: d= — домен From,
// s= — активный селектор, c=relaxed/relaxed, a=rsa-sha256, h= — from дважды,
// to, subject, date, message-id, mime-version, content-type; List-Unsubscribe
// в h= нет, потому что заголовка в письме нет.
func TestDKIM_NTF1P02_SignaturePassesIndependentVerifier(t *testing.T) {
	a, _ := keys(t)
	z := soundZone(t)
	s := newSigner(t, &switchablePairs{p: pairOf(selA, a)})
	signed := sign(t, s, cyrillicLetter(t))

	requirePass(t, z, signed)
	tags := sigTags(t, signed)
	for k, want := range map[string]string{"v": "1", "a": "rsa-sha256", "c": "relaxed/relaxed", "d": fromDomain, "s": selA} {
		if tags[k] != want {
			t.Fatalf("тег %s=%q, ожидалось %q (теги %v)", k, tags[k], want, tags)
		}
	}
	if tags["bh"] == "" || tags["b"] == "" {
		t.Fatalf("теги bh= и b= обязаны быть непустыми: %v", tags)
	}
	hk := headerKeys(tags)
	if n := count(hk, "from"); n != 2 {
		t.Fatalf("h=: from встречается %d раз, ожидалось 2 (%v)", n, hk)
	}
	for _, k := range r19HeaderKeys {
		if count(hk, k) == 0 {
			t.Fatalf("h= без %q: %v", k, hk)
		}
	}
	if count(hk, "list-unsubscribe") != 0 {
		t.Fatalf("h= называет list-unsubscribe, а заголовка в письме нет: %v", hk)
	}
}

// TestDKIM_NTF1P02_ListUnsubscribeSignedWhenPresent — близнец: письмо с
// заголовком List-Unsubscribe — он в h=, и подпись проходит.
func TestDKIM_NTF1P02_ListUnsubscribeSignedWhenPresent(t *testing.T) {
	a, _ := keys(t)
	z := soundZone(t)
	s := newSigner(t, &switchablePairs{p: pairOf(selA, a)})
	signed := sign(t, s, withHeader(cyrillicLetter(t), "List-Unsubscribe", listUnsubscribe))

	requirePass(t, z, signed)
	if hk := headerKeys(sigTags(t, signed)); count(hk, "list-unsubscribe") != 1 {
		t.Fatalf("h= без list-unsubscribe при заголовке в письме: %v", hk)
	}
}

// TestDKIM_NTF1P03_ModifiedBodyByteFails — близнец P02: в подписанном письме
// изменён один октет тела — проверка «тело не сходится». Положительный
// контроль — то же письмо без изменения — pass.
func TestDKIM_NTF1P03_ModifiedBodyByteFails(t *testing.T) {
	a, _ := keys(t)
	z := soundZone(t)
	s := newSigner(t, &switchablePairs{p: pairOf(selA, a)})
	signed := sign(t, s, cyrillicLetter(t))
	requirePass(t, z, signed)

	vs := verify(t, z, flipBodyByte(t, signed))
	if len(vs) != 1 || !bodyHashFail(vs[0]) {
		t.Fatalf("изменённый байт тела: ожидался fail «body hash did not verify», получено %v", vs)
	}
}

// TestDKIM_N15_SignPrependsHeaderAndKeepsLetterBytes — подпись добавляет
// ровно один заголовок DKIM-Signature в начало и байтов письма не меняет
// (§12а: после подписи — только dot-stuffing транспорта).
func TestDKIM_N15_SignPrependsHeaderAndKeepsLetterBytes(t *testing.T) {
	a, _ := keys(t)
	s := newSigner(t, &switchablePairs{p: pairOf(selA, a)})
	msg := cyrillicLetter(t)
	signed := sign(t, s, msg)

	if !bytes.HasPrefix(signed, []byte("DKIM-Signature:")) {
		t.Fatalf("подписанное письмо начинается не с DKIM-Signature:\n%s", head(signed))
	}
	if !bytes.HasSuffix(signed, msg) {
		t.Fatal("байты письма после подписи изменены: подписанное письмо не оканчивается исходным")
	}
	prefix := string(signed[:len(signed)-len(msg)])
	if !strings.HasSuffix(prefix, "\r\n") {
		t.Fatalf("заголовок подписи не оканчивается CRLF: %q", prefix)
	}
	// Одно поле: продолжения — только строки, начинающиеся пробелом или табуляцией.
	lines := strings.Split(strings.TrimSuffix(prefix, "\r\n"), "\r\n")
	for i, l := range lines[1:] {
		if l == "" || (l[0] != ' ' && l[0] != '\t') {
			t.Fatalf("перед письмом не одно поле заголовка: строка %d %q", i+2, l)
		}
	}
	if n := strings.Count(strings.ToLower(string(signed)), "\r\ndkim-signature:") + 1; n != 1 {
		t.Fatalf("заголовков DKIM-Signature %d, ожидался 1", n)
	}
}

// TestDKIM_NTF1P17_SignerFollowsPairSourceSwitch — подписчик берёт пару у
// источника на каждом письме: источник перешёл на «селектор B, ключ B» —
// следующее письмо подписано B, проверка по записи B — pass; письмо до
// перехода — A по записи A.
func TestDKIM_NTF1P17_SignerFollowsPairSourceSwitch(t *testing.T) {
	a, b := keys(t)
	z := soundZone(t)
	publishSelector(t, z, selB, b)
	src := &switchablePairs{p: pairOf(selA, a)}
	s := newSigner(t, src)

	before := sign(t, s, cyrillicLetter(t))
	if got := sigTags(t, before)["s"]; got != selA {
		t.Fatalf("до перехода s=%q, ожидался %q", got, selA)
	}
	requirePass(t, z, before)

	src.set(pairOf(selB, b))
	after := sign(t, s, cyrillicLetter(t))
	if got := sigTags(t, after)["s"]; got != selB {
		t.Fatalf("после перехода источника s=%q, ожидался %q", got, selB)
	}
	requirePass(t, z, after)
}

// TestDKIM_N15_ZeroPairRefused — источник ещё без пары (страж старта не
// пройден): подпись — ошибка, письма нет; пустой домен и nil-источник —
// отказ конструктора. Близнец — пара A: подпись есть (P02).
func TestDKIM_N15_ZeroPairRefused(t *testing.T) {
	s := newSigner(t, &switchablePairs{})
	out, err := s.Sign(cyrillicLetter(t))
	if err == nil || out != nil {
		t.Fatalf("подпись нулевой парой: ошибка %v, письмо %d байт — ожидалась ошибка и нет письма", err, len(out))
	}
	if _, err := newSignerErr("", &switchablePairs{}); err == nil {
		t.Fatal("NewSigner с пустым доменом собран")
	}
	if _, err := newSignerErr(fromDomain, nil); err == nil {
		t.Fatal("NewSigner без источника пары собран")
	}
}
