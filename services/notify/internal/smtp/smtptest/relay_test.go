// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtptest_test

// relay_test.go — самопроверка тестового узла почты клиентом стандартной
// библиотеки (net/smtp), без отправителя notify. Пробы З26 (internal/smtp) судят
// отправителя этим узлом; если узел отвечает не тем, что объявил сценарий, их
// красное — поломка вопроса, а не отсутствие предмета. Поэтому узел доказывается
// раньше и отдельно: каждый вид сценария, которым пользуются пробы, — своей пробой
// с близнецом.

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

const msg = "From: a@example.invalid\r\nTo: b@example.invalid\r\nMessage-ID: <x@example.invalid>\r\nSubject: s\r\n\r\nbody\r\n.dot\r\n"

func dial(t *testing.T, r *smtptest.Relay) *smtp.Client {
	t.Helper()
	c, err := net.DialTimeout("tcp", r.Addr(), 5*time.Second)
	if err != nil {
		t.Fatalf("узел %s не принял соединение: %v", r.Addr(), err)
	}
	cl, err := smtp.NewClient(c, r.Host)
	if err != nil {
		_ = c.Close()
		t.Fatalf("приветствие узла не принято: %v", err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	if err := cl.Hello("probe.example.invalid"); err != nil {
		t.Fatalf("EHLO: %v", err)
	}
	return cl
}

func startTLS(t *testing.T, cl *smtp.Client, r *smtptest.Relay) {
	t.Helper()
	if err := cl.StartTLS(&tls.Config{RootCAs: r.Roots, ServerName: r.Host, MinVersion: tls.VersionTLS12}); err != nil {
		t.Fatalf("STARTTLS с доверенным набором узла: %v", err)
	}
}

func codeOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var te *textproto.Error
	if !errors.As(err, &te) {
		t.Fatalf("ожидался ответ узла с кодом, получено %v", err)
	}
	return te.Code, te.Msg
}

// Исправный узел STARTTLS: письмо принято, тело записано без точечной экранировки,
// порядок глаголов записан.
func TestRelayStartTLSAcceptsAMessage(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{})
	cl := dial(t, r)
	if ok, _ := cl.Extension("STARTTLS"); !ok {
		t.Fatal("узел ModeStartTLS не предложил STARTTLS")
	}
	startTLS(t, cl, r)
	if err := cl.Mail("from@example.invalid"); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := cl.Rcpt("to@example.invalid"); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	w, err := cl.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("тело: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("конец тела: %v", err)
	}
	if err := cl.Quit(); err != nil {
		t.Fatalf("QUIT: %v", err)
	}
	ss := r.Sessions()
	if len(ss) != 1 || !ss[0].TLS {
		t.Fatalf("сессий %d, TLS %v; ожидалась одна сессия с TLS", len(ss), len(ss) == 1 && ss[0].TLS)
	}
	want := strings.ReplaceAll(msg, "\r\n", "\n")
	if got := r.Messages(); len(got) != 1 || string(got[0]) != want {
		t.Fatalf("тело записано %q, ожидалось %q", got, want)
	}
	if got := strings.Join(ss[0].Verbs, " "); got != "EHLO STARTTLS EHLO MAIL RCPT DATA BODY QUIT" {
		t.Fatalf("порядок глаголов %q", got)
	}
	if ss[0].MailFrom[0] != "from@example.invalid" || ss[0].Rcpts[0] != "to@example.invalid" {
		t.Fatalf("envelope записан %v / %v", ss[0].MailFrom, ss[0].Rcpts)
	}
}

// Узел без TLS: STARTTLS не предложен и отвергнут (NTF1-G07). Близнец — тот же
// сценарий в ModeStartTLS выше.
func TestRelayPlainOffersNoTLS(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModePlain})
	cl := dial(t, r)
	if ok, _ := cl.Extension("STARTTLS"); ok {
		t.Fatal("узел ModePlain предложил STARTTLS")
	}
	err := cl.StartTLS(&tls.Config{RootCAs: r.Roots, ServerName: r.Host, MinVersion: tls.VersionTLS12})
	if code, _ := codeOf(t, err); code != 502 {
		t.Fatalf("STARTTLS на узле без TLS: код %d, ожидался 502", code)
	}
}

// Узел с неявным TLS: соединение открытым текстом не получает приветствия, TLS с
// первого байта — получает.
func TestRelayImplicitTLS(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModeImplicit})
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", r.Addr(),
		&tls.Config{RootCAs: r.Roots, ServerName: r.Host, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("неявный TLS с доверенным набором: %v", err)
	}
	cl, err := smtp.NewClient(c, r.Host)
	if err != nil {
		t.Fatalf("приветствие поверх TLS: %v", err)
	}
	defer func() { _ = cl.Close() }()
	if err := cl.Hello("probe.example.invalid"); err != nil {
		t.Fatalf("EHLO поверх TLS: %v", err)
	}
	if ok, _ := cl.Extension("STARTTLS"); ok {
		t.Fatal("узел с неявным TLS предложил STARTTLS")
	}
}

// Сертификат чужого УЦ: рукопожатие с набором якорей своего УЦ отвергнуто клиентом
// (NTF1-G08); близнец — набор якорей выпустившего УЦ — рукопожатие состоялось.
func TestRelayForeignIssuerIsUntrusted(t *testing.T) {
	foreign := smtptest.NewCA(t)
	trusted := smtptest.NewCA(t)
	r := smtptest.Start(t, smtptest.Script{Issuer: foreign})

	cl := dial(t, r)
	err := cl.StartTLS(&tls.Config{RootCAs: trusted.Pool(), ServerName: r.Host, MinVersion: tls.VersionTLS12})
	var ua x509.UnknownAuthorityError
	if err == nil || !errors.As(err, &ua) {
		t.Fatalf("рукопожатие с чужим набором якорей: %v; ожидался отказ неизвестного УЦ", err)
	}

	twin := dial(t, r)
	startTLS(t, twin, r) // r.Roots — набор foreign
}

// Удостоверение: принятое — 235, иное — 535; AUTH не предложен открытым текстом.
func TestRelayAuthAcceptsOnlyTheScriptedCredential(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{Auth: func(u, p string) bool { return u == "notify" && p == "new" }})

	cl := dial(t, r)
	if ok, _ := cl.Extension("AUTH"); ok {
		t.Fatal("AUTH предложен до TLS")
	}
	startTLS(t, cl, r)
	err := cl.Auth(smtp.PlainAuth("", "notify", "old", r.Host))
	if code, text := codeOf(t, err); code != 535 || !strings.HasPrefix(text, "5.7.8") {
		t.Fatalf("неверное удостоверение: %d %q, ожидалось 535 5.7.8", code, text)
	}

	twin := dial(t, r)
	startTLS(t, twin, r)
	if err := twin.Auth(smtp.PlainAuth("", "notify", "new", r.Host)); err != nil {
		t.Fatalf("принимаемое удостоверение отвергнуто: %v", err)
	}
	ss := r.Sessions()
	if ss[0].AuthUser != "notify" || ss[1].AuthUser != "notify" {
		t.Fatalf("имя AUTH записано %q / %q", ss[0].AuthUser, ss[1].AuthUser)
	}
}

// Ответы сценария на MAIL, RCPT, DATA доходят до клиента кодом и расширенным кодом.
func TestRelayScriptedRepliesReachTheClient(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{
		Mail: func(from string) smtptest.Reply {
			if from == "bad@example.invalid" {
				return smtptest.Reply{Code: 554, Text: "5.7.1 sender refused"}
			}
			return smtptest.OK
		},
		Rcpt: func(to string) smtptest.Reply {
			switch to {
			case "gone@example.invalid":
				return smtptest.Reply{Code: 550, Text: "5.1.1 no such user"}
			case "bare@example.invalid":
				return smtptest.Reply{Code: 550, Text: "mailbox unavailable"}
			case "busy@example.invalid":
				return smtptest.Reply{Code: 451, Text: "4.3.0 try later"}
			}
			return smtptest.OK
		},
		Data: func() smtptest.Reply { return smtptest.Reply{Code: 552, Text: "5.3.4 too big"} },
	})
	cl := dial(t, r)
	startTLS(t, cl, r)

	if code, text := codeOf(t, cl.Mail("bad@example.invalid")); code != 554 || !strings.HasPrefix(text, "5.7.1") {
		t.Fatalf("MAIL: %d %q", code, text)
	}
	if err := cl.Mail("ok@example.invalid"); err != nil {
		t.Fatalf("MAIL близнец: %v", err)
	}
	for to, want := range map[string]int{"gone@example.invalid": 550, "bare@example.invalid": 550, "busy@example.invalid": 451} {
		if code, _ := codeOf(t, cl.Rcpt(to)); code != want {
			t.Fatalf("RCPT %s: код %d, ожидался %d", to, code, want)
		}
	}
	if err := cl.Rcpt("ok@example.invalid"); err != nil {
		t.Fatalf("RCPT близнец: %v", err)
	}
	w, err := cl.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	_, _ = w.Write([]byte(msg))
	if code, text := codeOf(t, w.Close()); code != 552 || !strings.HasPrefix(text, "5.3.4") {
		t.Fatalf("конец тела: %d %q", code, text)
	}
	if n := len(r.Messages()); n != 0 {
		t.Fatalf("отвергнутое тело записано как принятое: %d", n)
	}
}

// Разрыв на названном глаголе: команда записана, ответа нет.
func TestRelayDropsOnTheScriptedVerb(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{DropOn: "RCPT"})
	cl := dial(t, r)
	startTLS(t, cl, r)
	if err := cl.Mail("ok@example.invalid"); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	err := cl.Rcpt("ok@example.invalid")
	var te *textproto.Error
	if err == nil || errors.As(err, &te) {
		t.Fatalf("RCPT при разрыве: %v; ожидалась ошибка соединения без кода", err)
	}
	if r.Received("RCPT") != 1 {
		t.Fatalf("команда разрыва не записана: %v", r.Sessions())
	}
}

// Недоступный узел: соединение отвергнуто.
func TestClosedAddrRefusesConnections(t *testing.T) {
	host, port := smtptest.ClosedAddr(t)
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err == nil {
		_ = c.Close()
		t.Fatalf("на закрытом адресе %s:%d кто-то слушает", host, port)
	}
}
