// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package smtptest — тестовый узел почты (ретранслятор) для проб отправителя notify.
//
// # Зачем свой узел, а не контейнер приёмника
//
// Предмет проб З26 — что отправитель делает на КАЖДОМ ответе ретранслятора: 535 на
// `AUTH`, три формы 5xx на `RCPT`, 552 на `DATA`, 554 на `MAIL FROM`, разрыв посреди
// сессии, узел без STARTTLS, сертификат не из доверенного набора. Приёмник стенда
// (mailpit) отвечает 250 на всё и такого сценария не исполняет; узел пробы обязан
// ответить ровно названным кодом на ровно названной стадии. Поэтому узел — сценарий
// (Script), а не настройка чужого сервера.
//
// Сеть настоящая: слушатель TCP на собственном адресе петли (internal/privateloopback,
// посторонний до узла не доходит), TLS настоящий — сертификат выпускает УЦ пробы (CA),
// доверие решает набор якорей клиента. Узел записывает каждую принятую команду, и
// проба утверждает не только ответ отправителю, но и то, ЧЕГО узел не получил
// («`MAIL FROM` не отправлен» — NTF1-G07, G08).
//
// # Чем узел НЕ снисходительнее настоящего
//
//   - STARTTLS предлагается только в режиме ModeStartTLS и только до рукопожатия;
//   - `AUTH` предлагается только поверх TLS и только когда сценарий задал проверку
//     удостоверения: без TLS удостоверение по открытому каналу не принимается;
//   - команда вне порядка сессии (`RCPT` до `MAIL`, `DATA` без адресата) получает
//     503, как у настоящего ретранслятора, а не 250.
//
// Исправность узла держит relay_test.go этого пакета — клиентом стандартной
// библиотеки, без отправителя notify: фикстура доказывается раньше, чем ею судят.
package smtptest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// Mode — как узел устанавливает TLS.
type Mode int

const (
	// ModeStartTLS — приветствие открытым текстом, в ответе EHLO предложен STARTTLS
	// (схема адреса `smtp`).
	ModeStartTLS Mode = iota
	// ModePlain — узел без TLS: STARTTLS не предложен, неявного TLS нет (NTF1-G07).
	ModePlain
	// ModeImplicit — TLS с первого байта (схема адреса `smtps`).
	ModeImplicit
)

// Reply — ответ узла на команду: трёхзначный код и текст. Расширенный код (RFC 3463),
// если он есть, — первое слово текста: Reply{550, "5.1.1 no such user"}.
type Reply struct {
	Code int
	Text string
}

// OK — 250 без расширенного кода.
var OK = Reply{Code: 250, Text: "2.0.0 ok"}

// Script — сценарий узла. Нулевое значение — исправный узел STARTTLS без `AUTH`,
// принимающий всё.
type Script struct {
	Mode Mode
	// Issuer выпускает сертификат узла. nil — собственный УЦ узла, его набор якорей —
	// Relay.Roots. Чужой УЦ даёт сертификат не из доверенного набора (NTF1-G08).
	Issuer *CA
	// Greeting — ответ приветствия; нулевой — 220.
	Greeting Reply
	// Auth решает удостоверение `AUTH PLAIN`. nil — `AUTH` не предложен.
	Auth func(user, pass string) bool
	// Mail, Rcpt — ответ на `MAIL FROM` и `RCPT TO` по адресу из команды; nil — 250.
	Mail func(from string) Reply
	Rcpt func(to string) Reply
	// Data — ответ после тела письма; nil — 250.
	Data func() Reply
	// DropOn — глагол (`MAIL`, `RCPT`, `DATA`, `BODY` — конец тела), на котором узел
	// записывает команду и рвёт соединение без ответа. Пусто — не рвёт.
	DropOn string
}

// Session — что узел получил за одно соединение.
type Session struct {
	// Verbs — глаголы принятых команд по порядку (`EHLO`, `STARTTLS`, `AUTH`, …).
	Verbs []string
	// TLS — рукопожатие TLS состоялось.
	TLS bool
	// HandshakeErr — текст отказа рукопожатия, если оно было и не состоялось.
	HandshakeErr string
	// AuthUser — имя, предъявленное в `AUTH PLAIN` (принятое или нет).
	AuthUser string
	MailFrom []string
	Rcpts    []string
	// Messages — тела, принятые после `DATA` (форма textproto.DotReader: концы строк
	// `\n`, точечная экранировка снята).
	Messages [][]byte
}

// Relay — запущенный узел.
type Relay struct {
	// Host — адрес петли узла (IP-литерал); он же имя проверки сертификата.
	Host string
	Port int
	// Roots — набор якорей УЦ, выпустившего сертификат узла.
	Roots *x509.CertPool

	script Script
	cert   tls.Certificate
	ln     net.Listener

	mu       sync.Mutex
	sessions []*Session
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

// Addr — `host:port` узла.
func (r *Relay) Addr() string { return net.JoinHostPort(r.Host, strconv.Itoa(r.Port)) }

// Start поднимает узел по сценарию; узел закрывается по окончании пробы.
func Start(tb testing.TB, s Script) *Relay {
	tb.Helper()
	ln := privateloopback.Listen(tb)
	host, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		tb.Fatalf("smtptest: адрес слушателя %q не разбирается: %v", ln.Addr(), err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		_ = ln.Close()
		tb.Fatalf("smtptest: порт слушателя %q не число: %v", portText, err)
	}
	issuer := s.Issuer
	if issuer == nil {
		issuer = NewCA(tb)
	}
	r := &Relay{
		Host:   host,
		Port:   port,
		Roots:  issuer.Pool(),
		script: s,
		cert:   issuer.Leaf(tb, host),
		ln:     ln,
		conns:  map[net.Conn]struct{}{},
	}
	r.wg.Add(1)
	go r.accept()
	tb.Cleanup(r.close)
	return r
}

// ClosedAddr — адрес петли, на котором никто не слушает: узел недоступен (NTF1-G11).
func ClosedAddr(tb testing.TB) (host string, port int) {
	tb.Helper()
	ln := privateloopback.Listen(tb)
	h, p, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		tb.Fatalf("smtptest: адрес слушателя %q не разбирается: %v", ln.Addr(), err)
	}
	if err := ln.Close(); err != nil {
		tb.Fatalf("smtptest: слушатель %s не закрыт: %v", ln.Addr(), err)
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		tb.Fatalf("smtptest: порт %q не число: %v", p, err)
	}
	return h, port
}

// Sessions — снимок принятых сессий.
func (r *Relay) Sessions() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		c := *s
		c.Verbs = append([]string(nil), s.Verbs...)
		c.MailFrom = append([]string(nil), s.MailFrom...)
		c.Rcpts = append([]string(nil), s.Rcpts...)
		c.Messages = append([][]byte(nil), s.Messages...)
		out = append(out, c)
	}
	return out
}

// Received — сколько раз узел принял команду с этим глаголом во всех сессиях.
func (r *Relay) Received(verb string) int {
	n := 0
	for _, s := range r.Sessions() {
		for _, v := range s.Verbs {
			if v == verb {
				n++
			}
		}
	}
	return n
}

// Messages — все принятые тела по порядку.
func (r *Relay) Messages() [][]byte {
	var out [][]byte
	for _, s := range r.Sessions() {
		out = append(out, s.Messages...)
	}
	return out
}

func (r *Relay) close() {
	_ = r.ln.Close()
	r.mu.Lock()
	for c := range r.conns {
		_ = c.Close()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Relay) accept() {
	defer r.wg.Done()
	for {
		c, err := r.ln.Accept()
		if err != nil {
			return
		}
		r.mu.Lock()
		r.conns[c] = struct{}{}
		s := &Session{}
		r.sessions = append(r.sessions, s)
		r.mu.Unlock()
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer func() {
				r.mu.Lock()
				delete(r.conns, c)
				r.mu.Unlock()
				_ = c.Close()
			}()
			r.serve(c, s)
		}()
	}
}

// record меняет сессию под замком узла: снимок Sessions читается из другой горутины.
func (r *Relay) record(f func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f()
}

// sessionDeadline — предел одной сессии узла: зависший клиент не держит пробу вечно.
const sessionDeadline = 30 * time.Second

func (r *Relay) serve(raw net.Conn, s *Session) {
	_ = raw.SetDeadline(time.Now().Add(sessionDeadline))
	conn := raw
	tlsConf := &tls.Config{Certificates: []tls.Certificate{r.cert}, MinVersion: tls.VersionTLS12}
	if r.script.Mode == ModeImplicit {
		tc := tls.Server(raw, tlsConf)
		if err := tc.Handshake(); err != nil {
			r.record(func() { s.HandshakeErr = err.Error() })
			return
		}
		r.record(func() { s.TLS = true })
		conn = tc
	}
	tp := textproto.NewConn(conn)
	greeting := r.script.Greeting
	if greeting.Code == 0 {
		greeting = Reply{Code: 220, Text: "relay.smtptest ESMTP"}
	}
	if !reply(tp, greeting) || greeting.Code != 220 {
		return
	}
	isTLS := r.script.Mode == ModeImplicit
	var haveMail, haveRcpt bool
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg := splitCommand(line)
		r.record(func() { s.Verbs = append(s.Verbs, verb) })
		if r.script.DropOn != "" && verb == r.script.DropOn {
			return
		}
		switch verb {
		case "EHLO", "HELO":
			lines := []string{"relay.smtptest"}
			if r.script.Mode == ModeStartTLS && !isTLS {
				lines = append(lines, "STARTTLS")
			}
			if r.script.Auth != nil && isTLS {
				lines = append(lines, "AUTH PLAIN")
			}
			lines = append(lines, "8BITMIME")
			if !replyMulti(tp, 250, lines) {
				return
			}
			haveMail, haveRcpt = false, false
		case "STARTTLS":
			if r.script.Mode != ModeStartTLS || isTLS {
				if !reply(tp, Reply{Code: 502, Text: "5.5.1 STARTTLS not offered"}) {
					return
				}
				continue
			}
			if !reply(tp, Reply{Code: 220, Text: "2.0.0 ready to start TLS"}) {
				return
			}
			tc := tls.Server(raw, tlsConf)
			if err := tc.Handshake(); err != nil {
				r.record(func() { s.HandshakeErr = err.Error() })
				return
			}
			r.record(func() { s.TLS = true })
			isTLS = true
			conn = tc
			tp = textproto.NewConn(conn)
			haveMail, haveRcpt = false, false
		case "AUTH":
			if r.script.Auth == nil || !isTLS {
				if !reply(tp, Reply{Code: 502, Text: "5.5.1 AUTH not offered"}) {
					return
				}
				continue
			}
			user, pass, ok := decodePlain(arg)
			r.record(func() { s.AuthUser = user })
			if ok && r.script.Auth(user, pass) {
				if !reply(tp, Reply{Code: 235, Text: "2.7.0 authentication succeeded"}) {
					return
				}
				continue
			}
			if !reply(tp, Reply{Code: 535, Text: "5.7.8 authentication credentials invalid"}) {
				return
			}
		case "MAIL":
			from := pathOf(arg, "FROM:")
			r.record(func() { s.MailFrom = append(s.MailFrom, from) })
			rep := OK
			if r.script.Mail != nil {
				rep = r.script.Mail(from)
			}
			haveMail = rep.Code/100 == 2
			haveRcpt = false
			if !reply(tp, rep) {
				return
			}
		case "RCPT":
			if !haveMail {
				if !reply(tp, Reply{Code: 503, Text: "5.5.1 need MAIL first"}) {
					return
				}
				continue
			}
			to := pathOf(arg, "TO:")
			r.record(func() { s.Rcpts = append(s.Rcpts, to) })
			rep := OK
			if r.script.Rcpt != nil {
				rep = r.script.Rcpt(to)
			}
			if rep.Code/100 == 2 {
				haveRcpt = true
			}
			if !reply(tp, rep) {
				return
			}
		case "DATA":
			if !haveRcpt {
				if !reply(tp, Reply{Code: 503, Text: "5.5.1 need RCPT first"}) {
					return
				}
				continue
			}
			if !reply(tp, Reply{Code: 354, Text: "end data with <CR><LF>.<CR><LF>"}) {
				return
			}
			body, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			r.record(func() { s.Verbs = append(s.Verbs, "BODY") })
			if r.script.DropOn == "BODY" {
				return
			}
			rep := OK
			if r.script.Data != nil {
				rep = r.script.Data()
			}
			if rep.Code/100 == 2 {
				r.record(func() { s.Messages = append(s.Messages, body) })
			}
			haveMail, haveRcpt = false, false
			if !reply(tp, rep) {
				return
			}
		case "RSET":
			haveMail, haveRcpt = false, false
			if !reply(tp, OK) {
				return
			}
		case "NOOP":
			if !reply(tp, OK) {
				return
			}
		case "QUIT":
			_ = reply(tp, Reply{Code: 221, Text: "2.0.0 bye"})
			return
		default:
			if !reply(tp, Reply{Code: 502, Text: "5.5.2 command not recognized"}) {
				return
			}
		}
	}
}

func reply(tp *textproto.Conn, r Reply) bool {
	return tp.PrintfLine("%03d %s", r.Code, r.Text) == nil
}

func replyMulti(tp *textproto.Conn, code int, lines []string) bool {
	for i, l := range lines {
		sep := "-"
		if i == len(lines)-1 {
			sep = " "
		}
		if err := tp.PrintfLine("%03d%s%s", code, sep, l); err != nil {
			return false
		}
	}
	return true
}

func splitCommand(line string) (verb, arg string) {
	verb, arg, _ = strings.Cut(line, " ")
	return strings.ToUpper(verb), arg
}

// pathOf достаёт адрес из `FROM:<a@b> SIZE=…` / `TO:<a@b>`.
func pathOf(arg, prefix string) string {
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return ""
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSuffix(strings.TrimPrefix(rest, "<"), ">")
}

// decodePlain разбирает `PLAIN <base64(authzid \0 user \0 pass)>`.
func decodePlain(arg string) (user, pass string, ok bool) {
	mech, resp, _ := strings.Cut(arg, " ")
	if !strings.EqualFold(mech, "PLAIN") || resp == "" {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(resp)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// CA — удостоверяющий центр пробы.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewCA выпускает корневой сертификат пробы.
func NewCA(tb testing.TB) *CA {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		tb.Fatalf("smtptest: ключ УЦ не создан: %v", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial(tb),
		Subject:               pkix.Name{CommonName: "smtptest CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(crand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		tb.Fatalf("smtptest: сертификат УЦ не выпущен: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		tb.Fatalf("smtptest: сертификат УЦ не разбирается: %v", err)
	}
	return &CA{cert: cert, key: key}
}

// Pool — набор якорей из одного этого УЦ.
func (ca *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.cert)
	return p
}

// PEM — сертификат УЦ в форме PEM: файл якоря проверки листа узла
// (`notify.smtp.trustAnchorFile`), каким его монтирует чарт.
func (ca *CA) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}

// Leaf выпускает сертификат сервера на host (IP-литерал или DNS-имя).
func (ca *CA) Leaf(tb testing.TB, host string) tls.Certificate {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		tb.Fatalf("smtptest: ключ узла не создан: %v", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial(tb),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(crand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		tb.Fatalf("smtptest: сертификат узла не выпущен: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func serial(tb testing.TB) *big.Int {
	tb.Helper()
	n, err := crand.Int(crand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		tb.Fatalf("smtptest: серийный номер не выбран: %v", err)
	}
	return n
}

// String — для сообщений проб.
func (r Reply) String() string { return fmt.Sprintf("%03d %s", r.Code, r.Text) }
