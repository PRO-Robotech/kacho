// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp_test

// sender_test.go — отправитель SMTP против тестового узла почты по настоящей сети
// петли и настоящему TLS (З26; NTF1-G07…G11, G14, G16 — сторона отправителя).
//
// # Контракт отправителя (полоса N6)
//
//	smtp.Relay    — {Host string, Port int, ImplicitTLS bool, Roots *x509.CertPool,
//	                Username, Credential string, SessionTimeout time.Duration}:
//	                узел, порт и режим из разобранного `notify.smtp.connectionURI`
//	                (`smtp` — STARTTLS, `smtps` — неявный TLS; разбор — N13),
//	                Roots — доверенный набор (`notify.smtp.trustAnchorFile` либо
//	                корневое хранилище образа), имя проверки сертификата — Host.
//	                Поля «не проверять сертификат» нет и быть не должно (G07, G08).
//	smtp.NewSender(Relay) (*Sender, error)
//	(*Sender).Send(ctx, Envelope{From, To string}, msg []byte) Attempt — одна
//	                сессия на письмо: тело уходит как есть, своего Message-ID
//	                отправитель не ставит (Р14, G16).
//
// Фикстура — smtptest.Relay; её исправность доказана отдельно
// (smtptest/relay_test.go) клиентом стандартной библиотеки.

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

const (
	envFrom = "noreply@notify.example.invalid"
	// messageID — значение, которое ставит сборщик заголовков (З25), а не отправитель.
	messageID = "<0f1e2d3c4b5a69788796a5b4c3d2e1f0@notify.example.invalid>"
)

func message(to string) []byte {
	return []byte("From: Kacho <" + envFrom + ">\r\n" +
		"To: " + to + "\r\n" +
		"Message-ID: " + messageID + "\r\n" +
		"Subject: probe\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"line one\r\n" +
		".leading dot\r\n")
}

// relayOf — адрес отправителя на узел пробы; roots — доверенный набор отправителя.
func relayOf(r *smtptest.Relay, implicit bool, roots *x509.CertPool) smtp.Relay {
	return smtp.Relay{
		Host:           r.Host,
		Port:           r.Port,
		ImplicitTLS:    implicit,
		Roots:          roots,
		SessionTimeout: 10 * time.Second,
	}
}

func newSender(t *testing.T, rl smtp.Relay) *smtp.Sender {
	t.Helper()
	s, err := smtp.NewSender(rl)
	if err != nil {
		t.Fatalf("NewSender(%+v): %v", rl, err)
	}
	return s
}

func send(t *testing.T, s *smtp.Sender, to string) (smtp.Attempt, smtp.Cell) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a := s.Send(ctx, smtp.Envelope{From: envFrom, To: to}, message(to))
	return a, smtp.Classify(a)
}

func wantCell(t *testing.T, label string, a smtp.Attempt, c smtp.Cell, want cellWant) {
	t.Helper()
	if c.Outcome != want.outcome || c.Misconfigured != want.misconfigured || c.RelayUnavailable != want.unavailable {
		t.Fatalf("%s: попытка %+v → клетка {%s %+v misconfigured=%v unavailable=%v}, ожидалось {%+v misconfigured=%v unavailable=%v}",
			label, a, c.Name, c.Outcome, c.Misconfigured, c.RelayUnavailable, want.outcome, want.misconfigured, want.unavailable)
	}
}

// NTF1-G07: узел не предлагает STARTTLS — письмо не уходит, MAIL FROM не отправлен,
// DEFER(platform_unavailable) с вкладом в размыкатель. Близнец — тот же узел с
// STARTTLS: письмо отправлено. Изменённый факт — TLS у узла.
func TestSender_G07_RelayWithoutTLSGetsNoMailFrom(t *testing.T) {
	twinRelay := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModeStartTLS})
	a, c := send(t, newSender(t, relayOf(twinRelay, false, twinRelay.Roots)), "user@example.invalid")
	wantCell(t, "близнец: узел с STARTTLS", a, c, cellWant{sent, false, false})
	if n := len(twinRelay.Messages()); n != 1 {
		t.Fatalf("близнец: узел принял писем %d, ожидалось 1", n)
	}

	r := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModePlain})
	a, c = send(t, newSender(t, relayOf(r, false, r.Roots)), "user@example.invalid")
	wantCell(t, "узел без TLS", a, c, cellWant{deferUnav, false, true})
	if a.Failure != smtp.FailureNoTLS {
		t.Errorf("узел без TLS: отказ попытки %v, ожидался FailureNoTLS", a.Failure)
	}
	if n := r.Received("MAIL"); n != 0 {
		t.Fatalf("узел без TLS получил MAIL FROM %d раз; письмо обязано не уходить открытым текстом", n)
	}
	if n := r.Received("AUTH"); n != 0 {
		t.Fatalf("узел без TLS получил AUTH %d раз", n)
	}
}

// NTF1-G07 (вариант «не слушает неявный TLS»): адрес `smtps` против узла,
// говорящего открытым текстом, — рукопожатие не состоялось, MAIL FROM не
// отправлен. Близнец — узел с неявным TLS: письмо отправлено.
func TestSender_G07_ImplicitTLSAgainstAPlainRelayGetsNoMailFrom(t *testing.T) {
	twinRelay := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModeImplicit})
	a, c := send(t, newSender(t, relayOf(twinRelay, true, twinRelay.Roots)), "user@example.invalid")
	wantCell(t, "близнец: узел с неявным TLS", a, c, cellWant{sent, false, false})

	r := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModePlain})
	a, c = send(t, newSender(t, relayOf(r, true, r.Roots)), "user@example.invalid")
	wantCell(t, "smtps против узла без TLS", a, c, cellWant{deferUnav, false, true})
	if n := r.Received("MAIL"); n != 0 {
		t.Fatalf("узел без TLS получил MAIL FROM %d раз", n)
	}
}

// NTF1-G08: сертификат узла не из доверенного набора — DEFER(platform_unavailable),
// MAIL FROM не отправлен. Близнец — тот же узел, набор отправителя содержит
// выпустивший УЦ. Изменённый факт — доверие сертификату.
func TestSender_G08_UntrustedRelayCertificateGetsNoMailFrom(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{Issuer: smtptest.NewCA(t)})

	a, c := send(t, newSender(t, relayOf(r, false, r.Roots)), "user@example.invalid")
	wantCell(t, "близнец: доверенный УЦ", a, c, cellWant{sent, false, false})

	before := r.Received("MAIL")
	other := smtptest.NewCA(t).Pool()
	a, c = send(t, newSender(t, relayOf(r, false, other)), "user@example.invalid")
	wantCell(t, "недоверенный сертификат", a, c, cellWant{deferUnav, false, true})
	if a.Failure != smtp.FailureUntrusted {
		t.Errorf("недоверенный сертификат: отказ попытки %v, ожидался FailureUntrusted", a.Failure)
	}
	if n := r.Received("MAIL") - before; n != 0 {
		t.Fatalf("узел с недоверенным сертификатом получил MAIL FROM %d раз", n)
	}
}

// NTF1-G09: 535 на AUTH — все 20 строк DEFER(platform_unavailable), misconfigured,
// терминальных 0; после смены удостоверения на принимаемое все 20 отправлены.
// Изменённый факт — удостоверение.
func TestSender_G09_AuthRefusalIsNotPoisoning(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{Auth: func(u, p string) bool { return u == "notify" && p == "rotated" }})
	rl := relayOf(r, false, r.Roots)
	rl.Username = "notify"

	rl.Credential = "stale"
	stale := newSender(t, rl)
	terminal := 0
	for i := range 20 {
		a, c := send(t, stale, fmt.Sprintf("u%02d@example.invalid", i))
		wantCell(t, fmt.Sprintf("строка %d, старое удостоверение", i), a, c, cellWant{deferUnav, true, false})
		if c.Outcome.Kind != deferUnav.Kind {
			terminal++
		}
	}
	if terminal != 0 {
		t.Fatalf("терминальных исходов при отказе AUTH: %d", terminal)
	}
	if n := r.Received("MAIL"); n != 0 {
		t.Fatalf("после отказа AUTH узел получил MAIL FROM %d раз", n)
	}

	rl.Credential = "rotated"
	fresh := newSender(t, rl)
	for i := range 20 {
		a, c := send(t, fresh, fmt.Sprintf("u%02d@example.invalid", i))
		wantCell(t, fmt.Sprintf("строка %d, новое удостоверение", i), a, c, cellWant{sent, false, false})
	}
	if n := len(r.Messages()); n != 20 {
		t.Fatalf("после смены удостоверения узел принял писем %d, ожидалось 20", n)
	}
}

// NTF1-G10: 550 5.1.1 на RCPT одному адресату, 250 остальным — строка отвергнутого
// RECIPIENT_REJECTED без misconfigured, остальные отправлены. Изменённый факт —
// ответ на RCPT.
func TestSender_G10_RecipientRefusalIsTerminalAndSeparate(t *testing.T) {
	const gone = "gone@example.invalid"
	r := smtptest.Start(t, smtptest.Script{Rcpt: func(to string) smtptest.Reply {
		if to == gone {
			return smtptest.Reply{Code: 550, Text: "5.1.1 no such user"}
		}
		return smtptest.OK
	}})
	s := newSender(t, relayOf(r, false, r.Roots))
	misconfigured := 0
	for _, to := range []string{"a@example.invalid", gone, "b@example.invalid", "c@example.invalid"} {
		a, c := send(t, s, to)
		if c.Misconfigured {
			misconfigured++
		}
		if to == gone {
			wantCell(t, "отвергнутый адресат", a, c, cellWant{rejected, false, false})
			if a.Stage != smtp.StageRcpt || a.Code != 550 || a.Enhanced != "5.1.1" {
				t.Fatalf("отвергнутый адресат: попытка %+v, ожидалась RCPT 550 5.1.1", a)
			}
			continue
		}
		wantCell(t, "адресат "+to, a, c, cellWant{sent, false, false})
	}
	if misconfigured != 0 {
		t.Fatalf("misconfigured при отказе получателя: %d", misconfigured)
	}
	if n := len(r.Messages()); n != 3 {
		t.Fatalf("узел принял писем %d, ожидалось 3", n)
	}
}

// NTF1-G11 (сторона отправителя): узел недоступен — DEFER(platform_unavailable) с
// вкладом в размыкатель, терминального исхода нет. Близнец — узел доступен.
func TestSender_G11_UnreachableRelayIsDeferWithBreakerContribution(t *testing.T) {
	up := smtptest.Start(t, smtptest.Script{})
	a, c := send(t, newSender(t, relayOf(up, false, up.Roots)), "user@example.invalid")
	wantCell(t, "близнец: узел доступен", a, c, cellWant{sent, false, false})

	host, port := smtptest.ClosedAddr(t)
	rl := relayOf(up, false, up.Roots)
	rl.Host, rl.Port = host, port
	a, c = send(t, newSender(t, rl), "user@example.invalid")
	wantCell(t, "узел недоступен", a, c, cellWant{deferUnav, false, true})
	if a.Failure != smtp.FailureUnreachable {
		t.Errorf("узел недоступен: отказ попытки %v, ожидался FailureUnreachable", a.Failure)
	}
}

// NTF1-G14 через сеть: каждый ответ узла доходит до своей клетки с той стадией и
// тем расширенным кодом, которые узел произнёс.
func TestSender_G14_EachRelayAnswerReachesItsCell(t *testing.T) {
	type tc struct {
		name   string
		script smtptest.Script
		stage  smtp.Stage
		code   int
		enh    string
		fail   smtp.Failure
		want   cellWant
	}
	refuse := func(rep smtptest.Reply) func(string) smtptest.Reply {
		return func(string) smtptest.Reply { return rep }
	}
	cases := []tc{
		{"250", smtptest.Script{}, smtp.StageDone, 250, "", smtp.FailureNone, cellWant{sent, false, false}},
		{"421 на приветствии", smtptest.Script{Greeting: smtptest.Reply{Code: 421, Text: "4.3.2 shutting down"}},
			smtp.StageConnect, 421, "4.3.2", smtp.FailureNone, cellWant{deferUnav, false, true}},
		{"451 на RCPT", smtptest.Script{Rcpt: refuse(smtptest.Reply{Code: 451, Text: "4.3.0 try later"})},
			smtp.StageRcpt, 451, "4.3.0", smtp.FailureNone, cellWant{deferUnav, false, true}},
		{"550 5.1.1 на RCPT", smtptest.Script{Rcpt: refuse(smtptest.Reply{Code: 550, Text: "5.1.1 no such user"})},
			smtp.StageRcpt, 550, "5.1.1", smtp.FailureNone, cellWant{rejected, false, false}},
		{"550 5.7.1 на RCPT", smtptest.Script{Rcpt: refuse(smtptest.Reply{Code: 550, Text: "5.7.1 policy refusal"})},
			smtp.StageRcpt, 550, "5.7.1", smtp.FailureNone, cellWant{rejected, false, false}},
		{"550 без расширенного кода на RCPT", smtptest.Script{Rcpt: refuse(smtptest.Reply{Code: 550, Text: "mailbox unavailable"})},
			smtp.StageRcpt, 550, "", smtp.FailureNone, cellWant{rejected, false, false}},
		{"552 на DATA", smtptest.Script{Data: func() smtptest.Reply { return smtptest.Reply{Code: 552, Text: "5.3.4 too big"} }},
			smtp.StageData, 552, "5.3.4", smtp.FailureNone, cellWant{deferUnav, true, false}},
		{"554 на MAIL FROM", smtptest.Script{Mail: refuse(smtptest.Reply{Code: 554, Text: "5.7.1 sender refused"})},
			smtp.StageMail, 554, "5.7.1", smtp.FailureNone, cellWant{deferUnav, true, false}},
		{"разрыв после конца тела", smtptest.Script{DropOn: "BODY"},
			smtp.StageData, 0, "", smtp.FailureDisconnect, cellWant{deferUnav, false, true}},
		{"разрыв на RCPT", smtptest.Script{DropOn: "RCPT"},
			smtp.StageRcpt, 0, "", smtp.FailureDisconnect, cellWant{deferUnav, false, true}},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			r := smtptest.Start(t, k.script)
			a, c := send(t, newSender(t, relayOf(r, false, r.Roots)), "user@example.invalid")
			wantCell(t, k.name, a, c, k.want)
			if a.Stage != k.stage || a.Code != k.code || a.Enhanced != k.enh || a.Failure != k.fail {
				t.Fatalf("%s: попытка %+v, ожидалась {Stage:%v Code:%d Enhanced:%q Failure:%v}", k.name, a, k.stage, k.code, k.enh, k.fail)
			}
		})
	}
	// 535 на AUTH — отдельно: нужен узел с проверкой удостоверения.
	t.Run("535 на AUTH", func(t *testing.T) {
		r := smtptest.Start(t, smtptest.Script{Auth: func(string, string) bool { return false }})
		rl := relayOf(r, false, r.Roots)
		rl.Username, rl.Credential = "notify", "x"
		a, c := send(t, newSender(t, rl), "user@example.invalid")
		wantCell(t, "535 на AUTH", a, c, cellWant{deferUnav, true, false})
		if a.Stage != smtp.StageAuth || a.Code != 535 || a.Enhanced != "5.7.8" {
			t.Fatalf("535 на AUTH: попытка %+v", a)
		}
	})
}

// NTF1-G16 (сторона отправителя): тело уходит как есть — Message-ID сборщика
// заголовков, ровно один, у повторной отправки той же строки — тот же; envelope —
// From установки и ровно один адресат.
func TestSender_G16_MessageIDIsTheBuildersAndStableAcrossResend(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{})
	s := newSender(t, relayOf(r, false, r.Roots))
	const to = "user@example.invalid"
	for i := range 2 {
		a, c := send(t, s, to)
		wantCell(t, fmt.Sprintf("отправка %d", i+1), a, c, cellWant{sent, false, false})
	}
	msgs := r.Messages()
	if len(msgs) != 2 {
		t.Fatalf("узел принял писем %d, ожидалось 2", len(msgs))
	}
	want := []byte(strings.ReplaceAll(string(message(to)), "\r\n", "\n"))
	for i, m := range msgs {
		if !bytes.Equal(m, want) {
			t.Fatalf("письмо %d изменено отправителем:\nпринято %q\nотдано  %q", i+1, m, want)
		}
		if n := bytes.Count(bytes.ToLower(m), []byte("\nmessage-id:")); n != 1 {
			t.Fatalf("письмо %d: заголовков Message-ID %d, ожидался 1", i+1, n)
		}
	}
	for _, ss := range r.Sessions() {
		if len(ss.MailFrom) != 1 || ss.MailFrom[0] != envFrom || len(ss.Rcpts) != 1 || ss.Rcpts[0] != to {
			t.Fatalf("envelope сессии: MAIL %v RCPT %v; ожидалось %s → %s", ss.MailFrom, ss.Rcpts, envFrom, to)
		}
		if !ss.TLS {
			t.Fatal("сессия без TLS")
		}
	}
}
