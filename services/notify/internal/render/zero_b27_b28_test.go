// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// zero_b27_b28_test.go — нулевые значения непрозрачных типов у приёмников
// рендера notify: NTF1-B28 (нуль) — сборщик ссылки с form.Path{} и
// form.Token{}, сборщик заголовков с form.HeaderText{}; NTF1-B27 (нуль) —
// address.Normalized{} адресата и отправителя. Нуль — ошибка с причиной
// ErrUnset, вывода нет (голый origin не выдан). Близнец — те же приёмники со
// значениями функций notify/form и notify/address над значениями близнеца
// G24 (в том числе target = /).
//
// Приёмник address.Normalized «ключ сетки на адресата» (B27 (нуль)) — пакет
// limits, полоса N7; здесь не судится.
package render

import (
	"errors"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/form"
	"github.com/PRO-Robotech/kacho/services/notify/internal/render/emlprobe"
)

func TestRender_NTF1B28_ZeroPathAndTokenGiveNoLink(t *testing.T) {
	cases := []struct {
		name string
		call func() (string, error)
	}{
		{"PathLink(form.Path{})", func() (string, error) { return PathLink(testOrigin, form.Path{}) }},
		{"TokenLink(путь, form.Token{})", func() (string, error) {
			return TokenLink(testOrigin, mustPath(t, inviteLiteral), form.Token{})
		}},
		{"TokenLink(form.Path{}, токен)", func() (string, error) {
			return TokenLink(testOrigin, form.Path{}, mustToken(t, testToken))
		}},
	}
	for _, c := range cases {
		link, err := c.call()
		if !errors.Is(err, form.ErrUnset) || link != "" {
			t.Errorf("%s: ссылка %q, ошибка %v — ожидались пустая ссылка и form.ErrUnset", c.name, link, err)
		}
	}
	twins := []struct {
		name string
		call func() (string, error)
		want string
	}{
		{"PathLink(/iam/invitations/inv-…)", func() (string, error) {
			return PathLink(testOrigin, mustPath(t, "/iam/invitations/inv-0123456789abcdefg"))
		}, testOrigin + "/iam/invitations/inv-0123456789abcdefg"},
		{"PathLink(/)", func() (string, error) { return PathLink(testOrigin, mustPath(t, "/")) }, testOrigin + "/"},
		{"TokenLink(accept, токен)", func() (string, error) {
			return TokenLink(testOrigin, mustPath(t, inviteLiteral), mustToken(t, testToken))
		}, testOrigin + inviteLiteral + "?token=" + testToken},
	}
	for _, c := range twins {
		if link, err := c.call(); err != nil || link != c.want {
			t.Errorf("близнец %s: %q (%v), ожидалось %q", c.name, link, err, c.want)
		}
	}
}

func TestRender_NTF1B28_ZeroHeaderTextGivesNoHeader(t *testing.T) {
	if v, err := HeaderValue(form.HeaderText{}); !errors.Is(err, form.ErrUnset) || v != "" {
		t.Errorf("HeaderValue(form.HeaderText{}): %q, %v — ожидались пусто и form.ErrUnset", v, err)
	}
	v, err := HeaderValue(mustHeader(t, "probe"))
	if err != nil {
		t.Fatalf("близнец HeaderValue(probe): %v", err)
	}
	if dec, err := emlprobe.DecodeHeader(v); err != nil || dec != "probe" || !emlprobe.IsASCII(v) {
		t.Errorf("близнец HeaderValue(probe): сырое %q, декодировано %q (%v)", v, dec, err)
	}
	v, err = HeaderValue(mustHeader(t, "Приглашение в облако"))
	if err != nil || !emlprobe.IsASCII(v) || !emlprobe.HasEncodedWord(v) {
		t.Errorf("близнец HeaderValue(не-ASCII): %q (%v) — ожидалось слово RFC 2047", v, err)
	}
}

// TestRender_NTF1B28_ZeroAttrValueGivesNoLetter — значение form.Value{} в
// наборе — не «задано»: письма нет, причина — form.ErrUnset. Близнец — G03.
func TestRender_NTF1B28_ZeroAttrValueGivesNoLetter(t *testing.T) {
	r := newRenderer(t)
	for _, attr := range []string{"token", "inviter_name"} {
		in := inviteLetter(t, testInviter)
		in.Attrs[attr] = form.Value{}
		out, err := r.Render(in)
		if !errors.Is(err, form.ErrUnset) || out != nil {
			t.Errorf("%s = form.Value{}: письмо %d байт, ошибка %v — ожидались nil и form.ErrUnset", attr, len(out), err)
		}
	}
	if _, err := r.Render(inviteLetter(t, testInviter)); err != nil {
		t.Errorf("близнец G03: %v", err)
	}
}

// TestRender_NTF1B27_ZeroAddressGivesNoLetter — address.Normalized{} у
// адресата письма и у отправителя установки.
func TestRender_NTF1B27_ZeroAddressGivesNoLetter(t *testing.T) {
	r := newRenderer(t)
	in := inviteLetter(t, testInviter)
	in.To = address.Normalized{}
	if out, err := r.Render(in); !errors.Is(err, address.ErrUnset) || out != nil {
		t.Errorf("To = address.Normalized{}: письмо %d байт, ошибка %v — ожидались nil и address.ErrUnset", len(out), err)
	}
	if _, err := New(Config{Origin: testOrigin, From: Sender{Name: mustHeader(t, testFromName)}}); !errors.Is(err, address.ErrUnset) {
		t.Errorf("New с From.Address = address.Normalized{}: %v — ожидалась address.ErrUnset", err)
	}
	if _, err := New(Config{Origin: testOrigin, From: Sender{Address: mustNormalize(t, testFromAddress)}}); !errors.Is(err, form.ErrUnset) {
		t.Errorf("New с From.Name = form.HeaderText{}: %v — ожидалась form.ErrUnset", err)
	}
	twin := inviteLetter(t, testInviter)
	out, err := r.Render(twin)
	if err != nil {
		t.Fatalf("близнец To = address.Normalize(user@example.invalid): %v", err)
	}
	l, err := emlprobe.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if to, err := l.Header.AddressList("To"); err != nil || len(to) != 1 || to[0].Address != "user@example.invalid" {
		t.Errorf("близнец: To %v (%v)", to, err)
	}
}

// TestRender_NTF1G18_SecurityLetterHasOneSenderAndNoReplyTo — тот же
// предмет, что перепись эталонов G18, на выводе рендера: независимо от того,
// какие эталоны лежат в дереве.
func TestRender_NTF1G18_SecurityLetterHasOneSenderAndNoReplyTo(t *testing.T) {
	out, err := newRenderer(t).Render(inviteLetter(t, testInviter))
	if err != nil {
		t.Fatalf("Render(kaname/invite): %v", err)
	}
	l, err := emlprobe.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := l.Header["Reply-To"]; ok {
		t.Errorf("у security есть Reply-To %v", v)
	}
	from, err := l.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Address != testFromAddress || from[0].Name != testFromName {
		t.Errorf("From %v (%v), ожидался один %q <%s>", from, err, testFromName, testFromAddress)
	}
}
