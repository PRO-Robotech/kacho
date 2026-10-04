// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp_test

// sender_guard_test.go — стражи отправителя вне сценариев G: адрес ретранслятора,
// целость конверта, срок сессии. Каждый отрицательный кейс стоит рядом с
// близнецом, отличающимся одним фактом.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

// NewSender отвергает адрес, по которому отправитель не может работать проверенно:
// без доверенного набора (молчаливой подмены системным нет), без узла, с портом
// вне диапазона, с удостоверением без пары, без срока сессии. Близнец — исправный
// адрес.
func TestNewSender_RefusesAnIncompleteRelay(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{})
	good := relayOf(r, false, r.Roots)
	if _, err := smtp.NewSender(good); err != nil {
		t.Fatalf("близнец: исправный адрес отвергнут: %v", err)
	}
	cases := []struct {
		name string
		edit func(*smtp.Relay)
		want string
	}{
		{"без доверенного набора", func(rl *smtp.Relay) { rl.Roots = nil }, "доверенный набор"},
		{"узел пуст", func(rl *smtp.Relay) { rl.Host = "" }, "узел"},
		{"порт 0", func(rl *smtp.Relay) { rl.Port = 0 }, "порт"},
		{"порт 65536", func(rl *smtp.Relay) { rl.Port = 65536 }, "порт"},
		{"имя без удостоверения", func(rl *smtp.Relay) { rl.Username = "notify" }, "парой"},
		{"удостоверение без имени", func(rl *smtp.Relay) { rl.Credential = "x" }, "парой"},
		{"срок сессии 0", func(rl *smtp.Relay) { rl.SessionTimeout = 0 }, "срок"},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			rl := good
			k.edit(&rl)
			s, err := smtp.NewSender(rl)
			if err == nil || s != nil {
				t.Fatalf("адрес %+v принят", rl)
			}
			if !strings.Contains(err.Error(), k.want) {
				t.Fatalf("отказ %q не называет предмет %q", err, k.want)
			}
		})
	}
}

// Адрес конверта с переводом строки разорвал бы команду SMTP и дописал свою: узел
// не получает ни соединения, клетка — «код вне таблицы» (misconfigured, без
// размыкателя). Близнец — тот же адресат без перевода строки: письмо отправлено.
func TestSender_EnvelopeWithALineBreakNeverReachesTheRelay(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{})
	s := newSender(t, relayOf(r, false, r.Roots))

	a, c := send(t, s, "user@example.invalid")
	wantCell(t, "близнец: обычный адресат", a, c, cellWant{sent, false, false})
	before := len(r.Sessions())

	cases := []struct {
		name string
		env  smtp.Envelope
	}{
		{"RCPT с CRLF", smtp.Envelope{From: envFrom, To: "user@example.invalid>\r\nRCPT TO:<other@example.invalid"}},
		{"RCPT с угловой скобкой", smtp.Envelope{From: envFrom, To: "user@example.invalid> NOTIFY=NEVER"}},
		{"MAIL с LF", smtp.Envelope{From: "noreply@notify.example.invalid\nRSET", To: "user@example.invalid"}},
		{"MAIL без домена", smtp.Envelope{From: "noreply", To: "user@example.invalid"}},
	}
	for _, k := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a := s.Send(ctx, k.env, message(k.env.To))
		cancel()
		c := smtp.Classify(a)
		if c.Outcome != deferUnav || !c.Misconfigured || c.RelayUnavailable {
			t.Errorf("%s: попытка %+v → %+v; ожидался DEFER с misconfigured без размыкателя", k.name, a, c)
		}
	}
	if n := len(r.Sessions()) - before; n != 0 {
		t.Fatalf("узел получил соединений %d от конвертов с разрывом команды", n)
	}
}

// Узел, который молчит после приветствия, не держит сессию дольше срока: попытка —
// разрыв на стадии, где узел замолчал, за время порядка SessionTimeout. Близнец —
// тот же узел без сценария молчания.
func TestSender_SessionTimeoutBoundsASilentRelay(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{})
	rl := relayOf(r, false, r.Roots)
	rl.SessionTimeout = 300 * time.Millisecond
	a, c := send(t, newSender(t, rl), "user@example.invalid")
	wantCell(t, "близнец: узел отвечает", a, c, cellWant{sent, false, false})

	release := make(chan struct{})
	silent := smtptest.Start(t, smtptest.Script{Rcpt: func(string) smtptest.Reply {
		<-release
		return smtptest.OK
	}})
	// Отпустить узел до его закрытия: очистка идёт в обратном порядке.
	t.Cleanup(func() { close(release) })
	rl = relayOf(silent, false, silent.Roots)
	rl.SessionTimeout = 300 * time.Millisecond
	start := time.Now()
	a, c = send(t, newSender(t, rl), "user@example.invalid")
	took := time.Since(start)
	wantCell(t, "узел молчит на RCPT", a, c, cellWant{deferUnav, false, true})
	if a.Stage != smtp.StageRcpt || a.Failure != smtp.FailureDisconnect {
		t.Fatalf("узел молчит на RCPT: попытка %+v, ожидался разрыв на RCPT", a)
	}
	if took > 1500*time.Millisecond {
		t.Fatalf("сессия длилась %v при сроке 300ms", took)
	}
}
