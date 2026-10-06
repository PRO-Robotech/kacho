// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// dkim_fixture_selfcheck_test.go — положительный контроль условий проб
// подписи БЕЗ испытуемого: страж на зоне испытания переходит на пару B при
// записи B (P17) и остаётся на A без неё (P18); письмо, прошедшее настоящий
// SMTP-узел пробы, после wireForm проверяется независимой реализацией —
// pass, а изменённый байт тела — «тело не сходится». Сломанное условие
// краснеет здесь, а не выдаёт себя за отсутствующую подпись.
//
// Исполняется без испытуемого (harness_test.go и пакет `dkim` не входят):
//
//	cd services/notify/internal/deliver && go test -count=1 -run 'TestDKIMFixture' \
//	    $(ls *.go | grep -v _test.go) fixture_test.go dkim_fixture_test.go dkim_fixture_selfcheck_test.go

import (
	"context"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

// Узел пробы принимает письмо, подписанное независимой реализацией;
// принятое письмо в форме провода проходит проверку; изменённый байт тела —
// «тело не сходится»; envelope MAIL FROM узел записывает.
func TestDKIMFixtureRelayedLetterVerifies(t *testing.T) {
	g := newGuardRig(t)
	a, _ := signKeys(t)
	signed := independentlySigned(t, renderedLetter(t), signSelA, a)

	relay, snd := relayWith(t, smtptest.Script{}, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	at := snd.Send(ctx, smtp.Envelope{From: fixtureFrom, To: fixtureTo}, signed)
	msgs := relay.Messages()
	if len(msgs) != 1 {
		t.Fatalf("узел принял писем %d, ожидалось 1 (сессия: %+v)", len(msgs), at)
	}
	got := wireForm(msgs[0])
	vs := verifyOn(t, g.zone, got)
	if len(vs) != 1 || vs[0].Err != nil || vs[0].Domain != signDomain {
		t.Fatalf("принятое письмо, подписанное независимой реализацией, не прошло проверку: %v", vs)
	}
	tags := dkimTags(t, got)
	if tags["s"] != signSelA || tags["d"] != signDomain || tags["c"] != "relaxed/relaxed" {
		t.Fatalf("разбор тегов: %v", tags)
	}
	hk := signedHeaderKeys(tags)
	if countOf(hk, "from") != 2 || countOf(hk, "content-type") != 1 {
		t.Fatalf("разбор h=: %v", hk)
	}
	if vs := verifyOn(t, g.zone, flipOneBodyByte(t, got)); len(vs) != 1 || !bodyHashFailed(vs[0]) {
		t.Fatalf("изменённый байт тела: ожидалось «body hash did not verify», получено %v", vs)
	}
	if mf := relay.Sessions()[0].MailFrom; len(mf) != 1 || mf[0] != fixtureFrom {
		t.Fatalf("узел записал MAIL FROM %v, ожидалось [%s]", mf, fixtureFrom)
	}
	if dkimTags(t, renderedLetter(t)) != nil {
		t.Fatal("письмо сборщика уже несёт DKIM-Signature — вопрос о подписи не задан")
	}
}

// Страж (N14) на зоне испытания: запись B есть — после такта пара B, dkim = 1
// (условие P17); записи B нет — пара A, dkim = 0 (условие P18).
func TestDKIMFixtureGuardRotationConditions(t *testing.T) {
	_, b := signKeys(t)
	t.Run("P17: запись B опубликована", func(t *testing.T) {
		g := newGuardRig(t)
		g.publish(signSelB, b)
		g.boot()
		g.runRecheck()
		g.rotateTo(dkimGenB, signSelB, b)
		g.tick()
		if p := g.guard.Pair(); p.Selector != signSelB || p.Key.N.Cmp(b.N) != 0 {
			t.Fatalf("страж не перешёл на пару B: селектор %q", p.Selector)
		}
		if v := g.posture("dkim"); v != 1 {
			t.Fatalf("dkim = %v, ожидалось 1", v)
		}
	})
	t.Run("P18: записи B нет", func(t *testing.T) {
		g := newGuardRig(t)
		g.boot()
		g.runRecheck()
		g.rotateTo(dkimGenB, signSelB, b)
		g.tick()
		if p := g.guard.Pair(); p.Selector != signSelA {
			t.Fatalf("страж принял пару без записи: селектор %q", p.Selector)
		}
		if v := g.posture("dkim"); v != 0 {
			t.Fatalf("dkim = %v, ожидалось 0", v)
		}
	})
}
