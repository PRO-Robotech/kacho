// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dkim_test

// fixture_selfcheck_test.go — положительный контроль фикстуры БЕЗ испытуемого.
// Каждый вопрос, который пробы N15 ставят подписи, здесь задан письму,
// подписанному независимой реализацией: проверка различает pass и fail,
// разбор тегов читает d=, s=, c=, h=, перепись 7bit находит октет ≥ 0x80 и
// длинную строку, письма сборщика несут тот предмет, о котором их спрашивают.
// Сломанная фикстура краснеет здесь своим текстом.
//
// Исполняется без пакета `dkim`:
//
//	go test ./services/notify/internal/dkim/fixture_test.go \
//	        ./services/notify/internal/dkim/fixture_selfcheck_test.go

import (
	"bytes"
	"strings"
	"testing"
)

// Зона отвечает записями Р19 и отличает опубликованный селектор от неопубликованного.
func TestFixtureZonePublishesSelectorRecords(t *testing.T) {
	_, b := keys(t)
	z := soundZone(t)
	if recs := lookup(t, z, selA+"._domainkey."+fromDomain); len(recs) == 0 ||
		!strings.HasPrefix(strings.Join(recs, ""), "v=DKIM1; k=rsa; p=") {
		t.Fatalf("запись селектора %s: %q", selA, recs)
	}
	if recs := lookup(t, z, selB+"._domainkey."+fromDomain); len(recs) != 0 {
		t.Fatalf("селектор %s не публиковался, а зона ответила %q", selB, recs)
	}
	publishSelector(t, z, selB, b)
	// Строки TXT одной записи резолвер склеивает: записей ровно одна.
	if recs := lookup(t, z, selB+"._domainkey."+fromDomain); len(recs) != 1 ||
		!strings.HasPrefix(recs[0], "v=DKIM1; k=rsa; p=") {
		t.Fatalf("запись селектора %s: %q", selB, recs)
	}
}

// Независимая проверка: подпись по записи — pass; по записи другого ключа —
// не pass; изменённый байт тела — именно «тело не сходится»; разбор тегов
// читает d=, s=, c=, h= Р19.
func TestFixtureIndependentVerifierTellsPassFromFail(t *testing.T) {
	a, b := keys(t)
	z := soundZone(t)
	msg := cyrillicLetter(t)

	signed := independentSign(t, msg, selA, a)
	requirePass(t, z, signed)

	tags := sigTags(t, signed)
	for k, want := range map[string]string{"d": fromDomain, "s": selA, "c": "relaxed/relaxed", "a": "rsa-sha256"} {
		if tags[k] != want {
			t.Fatalf("разбор тегов: %s=%q, ожидалось %q (теги %v)", k, tags[k], want, tags)
		}
	}
	hk := headerKeys(tags)
	if count(hk, "from") != 2 {
		t.Fatalf("разбор h=: from встречается %d раз, ожидалось 2 (%v)", count(hk, "from"), hk)
	}
	for _, k := range r19HeaderKeys {
		if count(hk, k) == 0 {
			t.Fatalf("разбор h=: нет %q (%v)", k, hk)
		}
	}

	vs := verify(t, z, flipBodyByte(t, signed))
	if len(vs) != 1 || !bodyHashFail(vs[0]) {
		t.Fatalf("изменённый байт тела: ожидался отказ «body hash did not verify», получено %v", vs)
	}

	wrong := independentSign(t, msg, selA, b) // ключ B под селектором A
	vs = verify(t, z, wrong)
	if len(vs) != 1 || vs[0].Err == nil {
		t.Fatalf("подпись чужим ключом под селектором %s прошла проверку", selA)
	}

	missing := independentSign(t, msg, selB, b) // записи selB в зоне нет
	vs = verify(t, z, missing)
	if len(vs) != 1 || vs[0].Err == nil {
		t.Fatalf("подпись селектором без записи прошла проверку")
	}
}

// Перепись 7bit находит октет ≥ 0x80 и строку длиннее 998 октетов и молчит
// на 7bit-письме; пустое письмо — нарушение (инъекция «часть 8bit» CX1-137
// на синтетике).
func TestFixtureSevenBitCensusFindsEightBit(t *testing.T) {
	ok := []byte("From: a@example.invalid\r\nSubject: x\r\n\r\nplain ascii\r\n")
	if v := sevenBitViolations(ok); len(v) != 0 {
		t.Fatalf("7bit-письмо названо нарушением: %v", v)
	}
	eight := []byte("From: a@example.invalid\r\nContent-Transfer-Encoding: 8bit\r\n\r\nПривет\r\n")
	if v := sevenBitViolations(eight); len(v) == 0 || !strings.Contains(v[0], "октет 0x") {
		t.Fatalf("часть 8bit не найдена: %v", v)
	}
	long := append([]byte("Subject: x\r\n\r\n"), bytes.Repeat([]byte("a"), 999)...)
	if v := sevenBitViolations(long); len(v) == 0 || !strings.Contains(v[0], "длиной 999") {
		t.Fatalf("строка 999 октетов не найдена: %v", v)
	}
	if v := sevenBitViolations(nil); len(v) == 0 {
		t.Fatal("пустое письмо названо 7bit-письмом")
	}
}

// Письма сборщика несут предмет вопроса: кириллическое — октеты ≥ 0x80 в
// раскодированных частях; ASCII-близнец — раскодированные части только ASCII
// со строками ≤ 998, то есть для него 7bit допустим. Обе части — text/plain и
// text/html.
func TestFixtureLettersCarryTheQuestion(t *testing.T) {
	ru := parts(t, cyrillicLetter(t))
	en := parts(t, asciiLetter(t))
	for name, ps := range map[string][]part{"ru": ru, "en": en} {
		if len(ps) != 2 || ps[0].mediaType != "text/plain" || ps[1].mediaType != "text/html" {
			t.Fatalf("письмо %s: части %v, ожидались text/plain и text/html", name, ps)
		}
	}
	for _, p := range ru {
		if isASCII(p.decoded) {
			t.Fatalf("кириллическое письмо: часть %s без октетов ≥ 0x80 — вопроса о кодировке нет", p.mediaType)
		}
	}
	for _, p := range en {
		if !isASCII(p.decoded) {
			t.Fatalf("ASCII-письмо: часть %s несёт октет ≥ 0x80 — близнец не ASCII", p.mediaType)
		}
		if m := maxLine(p.decoded); m > 998 {
			t.Fatalf("ASCII-письмо: часть %s со строкой %d октетов — 7bit для неё недопустим", p.mediaType, m)
		}
	}
	h, _ := headerOf(t, asciiLetter(t))
	if !isASCII([]byte(h.Get("Subject") + h.Get("From"))) {
		t.Fatal("ASCII-письмо: заголовки не ASCII")
	}
}

// Письмо с List-Unsubscribe — близнец письма без него: заголовок вставлен,
// а письмо сборщика его не несёт.
func TestFixtureListUnsubscribeTwin(t *testing.T) {
	msg := withHeader(cyrillicLetter(t), "List-Unsubscribe", listUnsubscribe)
	h, _ := headerOf(t, msg)
	if h.Get("List-Unsubscribe") != listUnsubscribe {
		t.Fatalf("заголовок List-Unsubscribe не вставлен: %q", h.Get("List-Unsubscribe"))
	}
	if strings.Contains(string(cyrillicLetter(t)), "List-Unsubscribe") {
		t.Fatal("письмо сборщика уже несёт List-Unsubscribe — близнец «без заголовка» не построить")
	}
}
