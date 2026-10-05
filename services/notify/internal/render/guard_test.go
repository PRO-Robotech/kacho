// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// guard_test.go — сторожа рендера сверх клеток приёмки, без которых письмо
// З25 собиралось бы не той формы: слово RFC 2047, выписанное значением
// атрибута в ASCII-теме; письмо не 7bit; origin вне формы; адрес, который
// нельзя записать в заголовок без SMTPUTF8; набор атрибутов, не совпадающий
// с объявлением шаблона. Каждый отказ — с близнецом, отличающимся одним
// фактом; текст отказа значения атрибута не несёт.
package render

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/form"

	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

// Значение атрибута, похожее на слово RFC 2047, в ASCII-теме читатель
// почты раскрыл бы как слово: тема показала бы не то, что выписал источник.
// Значение заголовка кодируется целиком, и декодирование возвращает литерал.
func TestHeaderValue_EncodedWordLookalikeStaysLiteral(t *testing.T) {
	const lookalike = "=?utf-8?q?=D0=9F?= probe"
	v, err := HeaderValue(mustHeader(t, lookalike))
	if err != nil {
		t.Fatalf("HeaderValue(%q): %v", lookalike, err)
	}
	if dec, err := emlprobe.DecodeHeader(v); err != nil || dec != lookalike {
		t.Errorf("сырое %q декодировано в %q (%v) — ожидался литерал %q", v, dec, err, lookalike)
	}
	// близнец — тот же текст без «=?»: пишется как есть
	plain := "utf-8 probe"
	if v, err := HeaderValue(mustHeader(t, plain)); err != nil || v != plain {
		t.Errorf("близнец %q: сырое %q (%v), ожидалось как есть", plain, v, err)
	}
}

// Длинная тема не даёт строки заголовка длиннее 78 знаков: слова RFC 2047
// переносятся продолжением строки, каждое — целыми символами.
func TestHeaderValue_LongSubjectIsFolded(t *testing.T) {
	long := strings.Repeat("Приглашение в облако ", 12)
	long = strings.TrimSpace(long)
	v, err := HeaderValue(mustHeader(t, long))
	if err != nil {
		t.Fatalf("HeaderValue(длинная): %v", err)
	}
	for i, line := range strings.Split(v, "\r\n") {
		if n := len(line) + len("Subject: "); i == 0 && n > 78 || i > 0 && len(line) > 78 {
			t.Errorf("строка %d длиной %d > 78: %q", i, len(line), line)
		}
	}
	if dec, err := emlprobe.DecodeHeader(strings.ReplaceAll(v, "\r\n", "")); err != nil || dec != long {
		t.Errorf("перенесённая тема декодирована %q (%v)", dec, err)
	}
}

// Письмо — 7bit: все октеты < 0x80, переводы строк только CRLF, строк длиннее
// 78 нет (N15 подписывает окончательные байты и их не перекодирует).
func TestRender_LetterIsSevenBitWithBoundedLines(t *testing.T) {
	out, err := newRenderer(t).Render(inviteLetter(t, testInviter))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for i, b := range out {
		if b >= 0x80 {
			t.Fatalf("октет %#x на позиции %d — письмо не 7bit", b, i)
		}
	}
	if bytes.Contains(bytes.ReplaceAll(out, []byte("\r\n"), nil), []byte("\n")) ||
		bytes.Contains(bytes.ReplaceAll(out, []byte("\r\n"), nil), []byte("\r")) {
		t.Fatal("перевод строки не CRLF")
	}
	for i, line := range bytes.Split(out, []byte("\r\n")) {
		if len(line) > 78 {
			t.Errorf("строка %d длиной %d > 78", i+1, len(line))
		}
	}
}

func TestNew_OriginOutOfFormIsRefused(t *testing.T) {
	for _, origin := range []string{
		"", "http://console.example.invalid", "https://console.example.invalid/",
		"https://console.example.invalid/x", "https://u@console.example.invalid",
		"https://console.example.invalid?q", "https://console.example.invalid#f", "https://",
	} {
		if _, err := New(Config{Origin: origin, From: testSender(t)}); !errors.Is(err, ErrOrigin) {
			t.Errorf("origin %q: %v — ожидалась ErrOrigin", origin, err)
		}
		if l, err := PathLink(origin, mustPath(t, "/a")); !errors.Is(err, ErrOrigin) || l != "" {
			t.Errorf("PathLink(origin %q): %q, %v — ожидалась ErrOrigin", origin, l, err)
		}
	}
	for _, origin := range []string{testOrigin, "https://console.example.invalid:8443"} {
		if _, err := New(Config{Origin: origin, From: testSender(t)}); err != nil {
			t.Errorf("близнец origin %q: %v", origin, err)
		}
	}
}

// Адрес с не-ASCII локальной частью нормализуется, но записать его в
// заголовок 7bit-письма нельзя: письмо не собирается, а не уходит с 8-битным
// заголовком. Близнец — ASCII-адрес.
func TestRender_NonASCIIAddressIsRefused(t *testing.T) {
	r := newRenderer(t)
	in := inviteLetter(t, testInviter)
	in.To = mustNormalize(t, "пользователь@example.invalid")
	if out, err := r.Render(in); !errors.Is(err, ErrAddressNotASCII) || out != nil {
		t.Errorf("To с не-ASCII локальной частью: %d байт, %v — ожидалась ErrAddressNotASCII", len(out), err)
	}
	if _, err := New(Config{Origin: testOrigin, From: Sender{
		Name: mustHeader(t, testFromName), Address: mustNormalize(t, "отправитель@example.invalid"),
	}}); !errors.Is(err, ErrAddressNotASCII) {
		t.Errorf("From с не-ASCII локальной частью: %v — ожидалась ErrAddressNotASCII", err)
	}
	// IDNA-домен приводится Normalize к A-label — ASCII, письмо собирается
	in.To = mustNormalize(t, "user@пример.испытание")
	if _, err := r.Render(in); err != nil {
		t.Errorf("близнец To с IDNA-доменом: %v", err)
	}
}

// Набор атрибутов судится по объявлению шаблона: ключ вне объявления, вид не
// тот, обязательного нет — письма нет. Текст отказа называет атрибут, но не
// его значение.
func TestRender_AttrSetMustMatchTheTemplate(t *testing.T) {
	const secretish = "value-must-not-leak-0123456789"
	r := newRenderer(t)
	text, err := form.Require(form.KindText, secretish)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(map[string]form.Value)
		want error
	}{
		{"ключ вне объявления", func(m map[string]form.Value) { m["stranger"] = text }, ErrAttrUndeclared},
		{"вид не тот", func(m map[string]form.Value) { m["token"] = text }, ErrAttrKind},
		{"обязательного нет", func(m map[string]form.Value) { delete(m, "token") }, ErrAttrMissing},
	}
	for _, c := range cases {
		in := inviteLetter(t, testInviter)
		c.edit(in.Attrs)
		out, err := r.Render(in)
		if !errors.Is(err, c.want) || out != nil {
			t.Errorf("%s: %d байт, %v — ожидалась %v", c.name, len(out), err, c.want)
			continue
		}
		if strings.Contains(err.Error(), secretish) || strings.Contains(err.Error(), testInviter) {
			t.Errorf("%s: текст отказа несёт значение атрибута: %q", c.name, err)
		}
	}
	if _, err := r.Render(inviteLetter(t, testInviter)); err != nil {
		t.Errorf("близнец G03: %v", err)
	}
}

func TestRender_LocaleWithoutBodyIsRefused(t *testing.T) {
	in := inviteLetter(t, testInviter)
	in.Locale = "de"
	if out, err := newRenderer(t).Render(in); !errors.Is(err, ErrLocale) || out != nil {
		t.Errorf("локаль de: %d байт, %v — ожидалась ErrLocale", len(out), err)
	}
	in.Locale = "en"
	if _, err := newRenderer(t).Render(in); err != nil {
		t.Errorf("близнец en: %v", err)
	}
}
