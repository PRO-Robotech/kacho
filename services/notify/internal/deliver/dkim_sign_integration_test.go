// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

// dkim_sign_integration_test.go — подпись DKIM на пути клетки 10 (полоса N15;
// приёмка NTF-1 Р19 «Подпись», «Смена ключа во время работы», «Адрес возврата
// в NTF-1»; NTF1-P02, P03, P17, P18 на integration-уровне; Д101; замысел
// issue-2915 §12а «Подпись»).
//
// Испытуемый — `deliver` с подписчиком `dkim.Signer`, чей источник пары —
// настоящий страж DNS установки (`*dnscheck.Guard`). Письмо принимает
// настоящий SMTP-узел пробы; подпись принятого письма проверяет НЕЗАВИСИМАЯ
// реализация по записям зоны испытания. Условия (страж, зона, узел, проверка)
// проверены самопроверкой dkim_fixture_selfcheck_test.go без испытуемого.

import (
	"strings"
	"testing"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dkim"
)

// signRow — строка, которая доходит до клетки 10 (шаблон probe-bhello).
func signRow() *notifyv1.ClaimedNotification {
	return claimed(rowSpec{template: "probe-bhello", rev: 1, attrs: map[string]string{"target": "/"}})
}

// sendSigned — одна строка через испытуемого с подписчиком над источником
// пары pairs и письмом настоящего сборщика; исход SENT, письмо принято узлом.
// Возвращает принятое письмо в форме провода и сессию узла.
func sendSigned(t *testing.T, pairs dkim.PairSource) ([]byte, *rig) {
	t.Helper()
	r := newRig(t, rigOpts{
		build:  buildOf(t, buildEntry{nsProbe, "probe-bhello", 1}),
		pairs:  pairs,
		letter: renderedLetter(t),
	})
	row := signRow()
	r.wantSent(t, row, r.one(nsProbe, row))
	return wireForm(r.relay.Messages()[0]), r
}

// TestDeliver_NTF1P02_RelayedLetterIsSignedAndPasses — письмо, принятое
// узлом, несёт подпись Р19: независимая проверка по записи зоны — pass;
// d= — домен From, s= — активный селектор, c=relaxed/relaxed, a=rsa-sha256;
// h= — from дважды, to, subject, date, message-id, mime-version, content-type;
// envelope MAIL FROM — адрес отправителя установки (Д101).
func TestDeliver_NTF1P02_RelayedLetterIsSignedAndPasses(t *testing.T) {
	g := newGuardRig(t)
	g.boot()
	got, r := sendSigned(t, g.guard)

	tags := dkimTags(t, got)
	if tags == nil {
		t.Fatalf("письмо, принятое узлом, без DKIM-Signature:\n%s", headerBlock(got))
	}
	vs := verifyOn(t, g.zone, got)
	if len(vs) != 1 || vs[0].Err != nil {
		t.Fatalf("независимая проверка подписи принятого письма: %v (ожидался pass)", vs)
	}
	for k, want := range map[string]string{"v": "1", "a": "rsa-sha256", "c": "relaxed/relaxed", "d": signDomain, "s": signSelA} {
		if tags[k] != want {
			t.Fatalf("тег %s=%q, ожидалось %q (теги %v)", k, tags[k], want, tags)
		}
	}
	hk := signedHeaderKeys(tags)
	if n := countOf(hk, "from"); n != 2 {
		t.Fatalf("h=: from встречается %d раз, ожидалось 2 (повтор против второго From) — %v", n, hk)
	}
	for _, k := range r19SignedHeaders {
		if countOf(hk, k) == 0 {
			t.Fatalf("h= без %q: %v", k, hk)
		}
	}
	if mf := r.relay.Sessions()[0].MailFrom; len(mf) != 1 || mf[0] != fixtureFrom {
		t.Fatalf("envelope MAIL FROM %v, ожидался адрес отправителя установки [%s] (Д101)", mf, fixtureFrom)
	}
}

// TestDeliver_NTF1P03_ModifiedBodyByteFails — близнец P02: то же принятое
// письмо с одним изменённым октетом тела — проверка «тело не сходится».
// Положительный контроль — в той же пробе: без изменения — pass.
func TestDeliver_NTF1P03_ModifiedBodyByteFails(t *testing.T) {
	g := newGuardRig(t)
	g.boot()
	got, _ := sendSigned(t, g.guard)
	if vs := verifyOn(t, g.zone, got); len(vs) != 1 || vs[0].Err != nil {
		t.Fatalf("положительный контроль: подпись принятого письма не pass: %v", vs)
	}
	vs := verifyOn(t, g.zone, flipOneBodyByte(t, got))
	if len(vs) != 1 || !bodyHashFailed(vs[0]) {
		t.Fatalf("изменённый байт тела: ожидался fail «body hash did not verify», получено %v", vs)
	}
}

// TestDeliver_NTF1P17_LetterAfterRotationSignedByNewPair — пара в объекте
// заменена на «селектор B, ключ B», запись B опубликована; часы прошли
// интервал перепроверки + 1 с; следующее письмо подписано B: s=B, проверка
// независимой реализацией по записи B — pass.
func TestDeliver_NTF1P17_LetterAfterRotationSignedByNewPair(t *testing.T) {
	_, b := signKeys(t)
	g := newGuardRig(t)
	g.publish(signSelB, b)
	g.boot()
	g.runRecheck()
	g.rotateTo(dkimGenB, signSelB, b)
	g.tick()
	if p := g.guard.Pair(); p.Selector != signSelB {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: страж не перешёл на пару B (селектор %q) — условие P17 не создано", p.Selector)
	}

	got, _ := sendSigned(t, g.guard)
	tags := dkimTags(t, got)
	if tags["s"] != signSelB {
		t.Fatalf("письмо после смены пары подписано селектором %q, ожидался %q (теги %v)", tags["s"], signSelB, tags)
	}
	if vs := verifyOn(t, g.zone, got); len(vs) != 1 || vs[0].Err != nil {
		t.Fatalf("подпись новой парой по записи B: %v (ожидался pass)", vs)
	}
	if v := g.posture("dkim"); v != 1 {
		t.Fatalf("notify_mail_dns_posture{check=\"dkim\"} = %v, ожидалось 1", v)
	}
}

// TestDeliver_NTF1P18_NewPairWithoutRecordLetterStaysOnOldPair — близнец
// P17: записи селектора B нет; письмо подписано прежней парой: s=A,
// проверка по записи A — pass; отправка не остановлена (SENT), dkim = 0.
func TestDeliver_NTF1P18_NewPairWithoutRecordLetterStaysOnOldPair(t *testing.T) {
	_, b := signKeys(t)
	g := newGuardRig(t)
	g.boot()
	g.runRecheck()
	g.rotateTo(dkimGenB, signSelB, b)
	g.tick()
	if p := g.guard.Pair(); p.Selector != signSelA {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: страж принял пару без записи (селектор %q) — условие P18 не создано", p.Selector)
	}

	got, _ := sendSigned(t, g.guard)
	tags := dkimTags(t, got)
	if tags["s"] != signSelA {
		t.Fatalf("письмо при отвергнутой паре подписано селектором %q, ожидался прежний %q (теги %v)", tags["s"], signSelA, tags)
	}
	if vs := verifyOn(t, g.zone, got); len(vs) != 1 || vs[0].Err != nil {
		t.Fatalf("подпись прежней парой по записи A: %v (ожидался pass)", vs)
	}
	if v := g.posture("dkim"); v != 0 {
		t.Fatalf("notify_mail_dns_posture{check=\"dkim\"} = %v, ожидалось 0", v)
	}
}

func headerBlock(msg []byte) string {
	s := string(msg)
	if i := strings.Index(s, "\r\n\r\n"); i > 0 {
		return s[:i]
	}
	return s
}
