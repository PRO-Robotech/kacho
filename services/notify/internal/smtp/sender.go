// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Relay — ретранслятор и удостоверение notify на нём. Узел, порт и режим — из
// разобранной ручки `notify.smtp.connectionURI` (`smtp` — STARTTLS, `smtps` —
// неявный TLS; разбор — у загрузчика конфигурации). Поля «не проверять
// сертификат» нет (NTF1-G07, G08).
type Relay struct {
	// Host — узел из адреса; он же имя проверки сертификата (ServerName).
	Host string
	Port int
	// ImplicitTLS — `smtps`: TLS с первого байта; false — `smtp`: STARTTLS обязателен.
	ImplicitTLS bool
	// Roots — доверенный набор: сертификаты `notify.smtp.trustAnchorFile` либо
	// корневое хранилище образа (x509.SystemCertPool). nil — отказ NewSender:
	// «набор не передан» не подменяется молча системным.
	Roots *x509.CertPool
	// Username, Credential — `AUTH PLAIN`; оба пусты — без `AUTH`, иначе оба заданы.
	Username   string
	Credential string
	// SessionTimeout — предел одной сессии от соединения до ответа на конец тела
	// (`notify.smtp.sessionTimeout`).
	SessionTimeout time.Duration
}

// Sender — отправитель: одна сессия SMTP на письмо. Безопасен для одновременного
// вызова: сессии не делят состояния.
type Sender struct {
	relay Relay
	addr  string
	tls   *tls.Config
}

// NewSender проверяет адрес ретранслятора и собирает отправителя.
func NewSender(r Relay) (*Sender, error) {
	var bad []string
	if r.Host == "" {
		bad = append(bad, "узел пуст")
	}
	if r.Port < 1 || r.Port > 65535 {
		bad = append(bad, fmt.Sprintf("порт %d вне [1..65535]", r.Port))
	}
	if r.Roots == nil {
		bad = append(bad, "доверенный набор не передан")
	}
	if (r.Username == "") != (r.Credential == "") {
		bad = append(bad, "имя и удостоверение AUTH заданы не парой")
	}
	if r.SessionTimeout <= 0 {
		bad = append(bad, fmt.Sprintf("срок сессии %v не положителен", r.SessionTimeout))
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("smtp: ретранслятор: %s", strings.Join(bad, "; "))
	}
	return &Sender{
		relay: r,
		addr:  net.JoinHostPort(r.Host, strconv.Itoa(r.Port)),
		tls: &tls.Config{
			ServerName: r.Host,
			RootCAs:    r.Roots,
			MinVersion: tls.VersionTLS12,
		},
	}, nil
}

// Send ведёт одну сессию: соединение, TLS, `AUTH` (если задано удостоверение),
// `MAIL FROM`, `RCPT TO`, `DATA`. Тело уходит как есть — заголовки, в том числе
// `Message-ID`, ставит сборщик (Р14, NTF1-G16); отправитель лишь экранирует точку
// в начале строки. Срок сессии — меньшее из срока ctx и Relay.SessionTimeout.
//
// Решения об исходе Send не принимает: попытку судит [Classify].
func (s *Sender) Send(ctx context.Context, env Envelope, msg []byte) Attempt {
	// Адрес с управляющим символом или угловой скобкой разорвал бы команду SMTP.
	// Форму адреса судит сборщик раньше; здесь — защита конверта, и узел не
	// получает ничего: попытка — «код вне таблицы» (misconfigured), не недоступность.
	helo, ok := domainOf(env.From)
	if !ok || !envelopeSafe(env.From) {
		return Attempt{Stage: StageMail}
	}
	if !envelopeSafe(env.To) {
		return Attempt{Stage: StageRcpt}
	}

	ctx, cancel := context.WithTimeout(ctx, s.relay.SessionTimeout)
	defer cancel()

	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return Attempt{Stage: StageConnect, Failure: FailureUnreachable}
	}
	defer func() { _ = raw.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	// Отмена ctx обрывает чтение и запись сразу, не дожидаясь срока.
	stop := context.AfterFunc(ctx, func() { _ = raw.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	c := &session{conn: raw}
	if s.relay.ImplicitTLS {
		if a, ok := c.handshake(ctx, s.tls); !ok {
			return a
		}
	}
	return c.run(ctx, s, helo, env, msg)
}

// session — одно соединение с ретранслятором.
type session struct {
	conn net.Conn
	tp   *textproto.Conn
	tls  bool
}

func (c *session) run(ctx context.Context, s *Sender, helo string, env Envelope, msg []byte) Attempt {
	c.tp = textproto.NewConn(c.conn)
	if a, ok := c.expect(StageConnect, 220); !ok {
		return a
	}
	ext, a, ok := c.ehlo(helo)
	if !ok {
		return a
	}
	if !c.tls {
		if _, offered := ext["STARTTLS"]; !offered {
			c.quit()
			return Attempt{Stage: StageTLS, Failure: FailureNoTLS}
		}
		// Отказ STARTTLS — попытка стадии TLS с кодом узла; клетку решает таблица.
		if a, ok := c.cmd(StageTLS, 220, "STARTTLS"); !ok {
			return a
		}
		if a, ok := c.handshake(ctx, s.tls); !ok {
			return a
		}
		c.tp = textproto.NewConn(c.conn)
		if _, a, ok = c.ehlo(helo); !ok {
			return a
		}
	}
	if s.relay.Username != "" {
		resp := base64.StdEncoding.EncodeToString([]byte("\x00" + s.relay.Username + "\x00" + s.relay.Credential))
		if a, ok := c.cmd(StageAuth, 235, "AUTH PLAIN "+resp); !ok {
			return a
		}
	}
	if a, ok := c.cmd(StageMail, 2, "MAIL FROM:<"+env.From+">"); !ok {
		return a
	}
	if a, ok := c.cmd(StageRcpt, 2, "RCPT TO:<"+env.To+">"); !ok {
		return a
	}
	if a, ok := c.cmd(StageData, 354, "DATA"); !ok {
		return a
	}
	w := c.tp.DotWriter()
	if _, err := w.Write(msg); err != nil {
		return Attempt{Stage: StageData, Failure: FailureDisconnect}
	}
	if err := w.Close(); err != nil {
		return Attempt{Stage: StageData, Failure: FailureDisconnect}
	}
	code, _, err := c.tp.ReadResponse(2)
	if err != nil {
		return attemptOf(StageData, err)
	}
	c.quit()
	// Расширенный код — ключ таблицы только для отказов; у принятого письма его нет.
	return Attempt{Stage: StageDone, Code: code}
}

// handshake — клиент TLS поверх соединения; имя проверки — узел адреса.
func (c *session) handshake(ctx context.Context, conf *tls.Config) (Attempt, bool) {
	tc := tls.Client(c.conn, conf)
	if err := tc.HandshakeContext(ctx); err != nil {
		return Attempt{Stage: StageTLS, Failure: handshakeFailure(err)}, false
	}
	c.conn = tc
	c.tls = true
	return Attempt{}, true
}

// handshakeFailure: сертификат не проверен — недоверие; узел не говорит TLS
// (ответил открытым текстом, отверг рукопожатие) — нет TLS; оборвал соединение
// или истёк срок — разрыв.
func handshakeFailure(err error) Failure {
	var verr *tls.CertificateVerificationError
	if errors.As(err, &verr) {
		return FailureUntrusted
	}
	var nerr net.Error
	if errors.As(err, &nerr) || isClosed(err) || errors.Is(err, context.Canceled) {
		return FailureDisconnect
	}
	return FailureNoTLS
}

// ehlo — `EHLO` и перечень расширений узла (имя расширения прописными → параметры).
func (c *session) ehlo(name string) (map[string]string, Attempt, bool) {
	if err := c.tp.PrintfLine("EHLO %s", name); err != nil {
		return nil, Attempt{Stage: StageConnect, Failure: FailureDisconnect}, false
	}
	_, text, err := c.tp.ReadResponse(250)
	if err != nil {
		return nil, attemptOf(StageConnect, err), false
	}
	ext := map[string]string{}
	lines := strings.Split(text, "\n")
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, " ")
		ext[strings.ToUpper(k)] = v
	}
	return ext, Attempt{}, true
}

// cmd — команда и ответ, ожидаемый кодом want (трёхзначный — точный, однозначный —
// класс). Иной ответ — попытка этой стадии.
func (c *session) cmd(st Stage, want int, line string) (Attempt, bool) {
	if err := c.tp.PrintfLine("%s", line); err != nil {
		return Attempt{Stage: st, Failure: FailureDisconnect}, false
	}
	return c.expect(st, want)
}

func (c *session) expect(st Stage, want int) (Attempt, bool) {
	if _, _, err := c.tp.ReadResponse(want); err != nil {
		return attemptOf(st, err), false
	}
	return Attempt{}, true
}

// quit — вежливое завершение; его исход на попытку не влияет.
func (c *session) quit() {
	if c.tp.PrintfLine("QUIT") == nil {
		_, _, _ = c.tp.ReadResponse(221)
	}
}

// attemptOf — ответ, не совпавший с ожидаемым, либо отказ чтения. Ответ с кодом —
// код и расширенный код; неразборный ответ — код 0 («код вне таблицы»); отказ
// соединения и истёкший срок — разрыв.
func attemptOf(st Stage, err error) Attempt {
	var terr *textproto.Error
	if errors.As(err, &terr) {
		return Attempt{Stage: st, Code: terr.Code, Enhanced: enhancedCode(terr.Code, terr.Msg)}
	}
	var perr textproto.ProtocolError
	if errors.As(err, &perr) {
		return Attempt{Stage: st}
	}
	return Attempt{Stage: st, Failure: FailureDisconnect}
}

// isClosed — соединение закрыто узлом или нами.
func isClosed(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}

// enhancedCode — расширенный код RFC 3463 из начала текста ответа: `класс.тема.деталь`,
// класс совпадает с первой цифрой кода ответа (2, 4 или 5), тема и деталь — 1–3
// цифры. Иное — "" (ответ кода не несёт).
func enhancedCode(code int, text string) string {
	first, _, _ := strings.Cut(text, "\n")
	word, _, _ := strings.Cut(first, " ")
	parts := strings.Split(word, ".")
	if len(parts) != 3 {
		return ""
	}
	if parts[0] != strconv.Itoa(code/100) || (parts[0] != "2" && parts[0] != "4" && parts[0] != "5") {
		return ""
	}
	for _, p := range parts[1:] {
		if len(p) < 1 || len(p) > 3 || strings.Trim(p, "0123456789") != "" {
			return ""
		}
	}
	return word
}

// envelopeSafe — адрес конверта не пуст и не несёт символов, разрывающих команду
// SMTP: управляющих, пробела, угловых скобок.
func envelopeSafe(addr string) bool {
	if addr == "" {
		return false
	}
	for i := 0; i < len(addr); i++ {
		b := addr[i]
		if b <= ' ' || b == 0x7f || b == '<' || b == '>' {
			return false
		}
	}
	return true
}

// domainOf — домен адреса отправителя установки; он же имя в `EHLO`. Адрес без
// домена — false: имени не подставляется.
func domainOf(from string) (string, bool) {
	i := strings.LastIndexByte(from, '@')
	if i < 1 || i+1 >= len(from) {
		return "", false
	}
	return from[i+1:], true
}
