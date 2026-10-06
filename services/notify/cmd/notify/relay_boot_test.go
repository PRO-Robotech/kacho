// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// relay_boot_test.go — узел почты в композиционном корне (полоса N13; Д44, Д45;
// CX1-77, CX1-78, CX1-80, CX1-84; приёмка NTF-1 NTF1-G07; замысел З20 «Узел
// почты», З26 «TLS обязателен»).
//
// Что держат пробы: разобранный адрес `notify.smtp.connectionURI` ЧИТАЕТ набор
// соединения SMTP — узел и порт адреса становятся адресом соединения, схема —
// способом установки TLS, узел — именем проверки сертификата, раскодированное
// имя пользователя — именем `AUTH`. Валидатор, который адрес принимает, а набор
// не читает, эти пробы не проходит (CX1-78).
//
// Контракт испытуемого (шов корня; имя — этой пробы):
//
//	// newRelaySender — отправитель из загруженной и проверенной конфигурации:
//	// узел, порт, режим и имя — из адреса, удостоверение — cfg.Credential(),
//	// срок — notify.smtp.sessionTimeout, доверенный набор — roots.
//	func newRelaySender(cfg config.Config, roots *x509.CertPool) (*smtp.Sender, error)

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp/smtptest"
)

const (
	relayEnvFrom = "notify@example.invalid"
	relayEnvTo   = "user@example.invalid"
)

// loadConfig загружает конфигурацию процесса из окружения пробы: фикстура с
// правками, окружение прогона в неё не протекает. Отказ стража на законном
// адресе — отказ испытуемого, а не фикстуры: фикстура проходит стража
// (TestBootFixtureStarts пакета config).
func loadConfig(t *testing.T, edits map[string]string) config.Config {
	t.Helper()
	for _, k := range config.Knobs() {
		t.Setenv(k.Env, "")
		if err := os.Unsetenv(k.Env); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: снять %s: %v", k.Env, err)
		}
	}
	env := fixtureEnv(t)
	for k, v := range edits {
		env[k] = v
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("загрузчик отверг законную конфигурацию: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("страж отверг законную конфигурацию: %v", err)
	}
	return cfg
}

func relayURI(scheme, userinfo, host string, port int) string {
	u := scheme + "://"
	if userinfo != "" {
		u += userinfo + "@"
	}
	return u + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
}

func sendOne(t *testing.T, s *smtp.Sender) smtp.Attempt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	msg := []byte("From: <" + relayEnvFrom + ">\r\nTo: <" + relayEnvTo + ">\r\nSubject: relay probe\r\n\r\nbody\r\n")
	return s.Send(ctx, smtp.Envelope{From: relayEnvFrom, To: relayEnvTo}, msg)
}

func senderFor(t *testing.T, cfg config.Config, roots *x509.CertPool) *smtp.Sender {
	t.Helper()
	s, err := newRelaySender(cfg, roots)
	if err != nil {
		t.Fatalf("отправитель из законного адреса не собран: %v", err)
	}
	return s
}

// NTF1-G07 и Д44 на процессе: адреса нет — процесс не стартует, отказ называет
// ручку; пустая строка — то же. Близнец — TestNotifyProcessPassesTheGuardWithSoundPosture
// на фикстуре с действительным адресом.
func TestNotifyProcessRefusesWithoutRelayAddress(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(map[string]string)
	}{
		{"переменная не задана", func(e map[string]string) { delete(e, "KACHO_NOTIFY_SMTP_CONNECTION_URI") }},
		{"пустая строка", func(e map[string]string) { e["KACHO_NOTIFY_SMTP_CONNECTION_URI"] = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := fixtureEnv(t)
			peerTLSFiles(t, env)
			env["KACHO_NOTIFY_DIAG_ADDR"] = freeAddr(t)
			c.edit(env)
			runRefused(t, env, "notify.smtp.connectionURI", "KACHO_NOTIFY_SMTP_CONNECTION_URI")
		})
	}
}

// CX1-78 — режим TLS читается из схемы: `smtps` против узла только со STARTTLS
// — писем 0; близнец `smtp` — письмо отправлено. И зеркально для узла с
// неявным TLS. Инъекция «режим не читается» красит одну из двух пар.
func TestRelayTLSModeIsReadFromTheScheme(t *testing.T) {
	cases := []struct {
		name   string
		mode   smtptest.Mode
		scheme string
		sent   bool
	}{
		{"smtps против узла только со STARTTLS", smtptest.ModeStartTLS, "smtps", false},
		{"близнец: smtp против узла со STARTTLS", smtptest.ModeStartTLS, "smtp", true},
		{"smtp против узла с неявным TLS", smtptest.ModeImplicit, "smtp", false},
		{"близнец: smtps против узла с неявным TLS", smtptest.ModeImplicit, "smtps", true},
		{"NTF1-G07: smtp против узла без TLS", smtptest.ModePlain, "smtp", false},
		{"NTF1-G07: smtps против узла без TLS", smtptest.ModePlain, "smtps", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := smtptest.Start(t, smtptest.Script{Mode: c.mode})
			// Срок сессии — нижний в своей границе: при несовпадении режима обе
			// стороны ждут первого байта друг от друга, и попытку кончает срок.
			cfg := loadConfig(t, map[string]string{
				"KACHO_NOTIFY_SMTP_CONNECTION_URI":  relayURI(c.scheme, "", r.Host, r.Port),
				"KACHO_NOTIFY_SMTP_SESSION_TIMEOUT": "2s",
			})
			a := sendOne(t, senderFor(t, cfg, r.Roots))
			got := len(r.Messages())
			if c.sent && (got != 1 || a.Stage != smtp.StageDone) {
				t.Fatalf("письмо не отправлено (писем %d, попытка %+v): режим адреса %s не установил соединение", got, a, c.scheme)
			}
			if !c.sent {
				if got != 0 || r.Received("MAIL") != 0 {
					t.Fatalf("узел режима %v получил MAIL FROM при адресе %s (писем %d)", c.mode, c.scheme, got)
				}
				if a.Stage == smtp.StageDone {
					t.Fatalf("попытка доложила отправку при несовпадении режима: %+v", a)
				}
			}
		})
	}
}

// Узел и порт адреса — адрес соединения: сессия приходит на узел и порт адреса.
func TestRelaySessionGoesToTheAddressHostAndPort(t *testing.T) {
	r := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModeStartTLS})
	cfg := loadConfig(t, map[string]string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": relayURI("smtp", "", r.Host, r.Port)})
	sendOne(t, senderFor(t, cfg, r.Roots))
	if n := len(r.Sessions()); n != 1 {
		t.Fatalf("сессий на узле адреса %d, ожидалась 1", n)
	}
	if n := len(r.Messages()); n != 1 {
		t.Fatalf("узел адреса принял писем %d, ожидалось 1", n)
	}
}

// CX1-84 — имя `AUTH` — раскодированное userinfo адреса (`%40` → `@`), пара с
// удостоверением. Близнец — адрес без имени и без удостоверения: сессия без AUTH.
func TestRelayAuthUsesTheDecodedUserName(t *testing.T) {
	var mu sync.Mutex
	var users []string
	r := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModeStartTLS, Auth: func(user, pass string) bool {
		mu.Lock()
		users = append(users, user)
		mu.Unlock()
		return pass == "relay-credential"
	}})
	cfg := loadConfig(t, map[string]string{
		"KACHO_NOTIFY_SMTP_CONNECTION_URI": relayURI("smtp", "a%40b", r.Host, r.Port),
		"KACHO_NOTIFY_SMTP_CREDENTIAL":     "relay-credential",
	})
	a := sendOne(t, senderFor(t, cfg, r.Roots))
	mu.Lock()
	got := append([]string(nil), users...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "a@b" {
		t.Fatalf("AUTH предъявил имена %q, ожидалось ровно [\"a@b\"] (раскодированное userinfo)", got)
	}
	if a.Stage != smtp.StageDone || len(r.Messages()) != 1 {
		t.Fatalf("письмо с AUTH не отправлено: %+v", a)
	}

	t.Run("близнец: адрес без имени — сессия без AUTH", func(t *testing.T) {
		r2 := smtptest.Start(t, smtptest.Script{Mode: smtptest.ModeStartTLS})
		cfg2 := loadConfig(t, map[string]string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": relayURI("smtp", "", r2.Host, r2.Port)})
		sendOne(t, senderFor(t, cfg2, r2.Roots))
		if n := r2.Received("AUTH"); n != 0 {
			t.Fatalf("адрес без имени дал AUTH %d раз", n)
		}
		if n := len(r2.Messages()); n != 1 {
			t.Fatalf("письмо без AUTH не отправлено: писем %d", n)
		}
	})
}

// tlsNode — узел неявного TLS с сертификатом на ИМЯ (без IP SAN): записывает,
// состоялось ли рукопожатие. Предмет — только имя проверки сертификата.
type tlsNode struct {
	host string
	port int

	mu     sync.Mutex
	shakes []error
}

func startTLSNode(t *testing.T, cert tls.Certificate) *tlsNode {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: узел TLS не поднят: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(p)
	n := &tlsNode{host: "127.0.0.1", port: port}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			herr := c.(*tls.Conn).Handshake()
			n.mu.Lock()
			n.shakes = append(n.shakes, herr)
			n.mu.Unlock()
			_ = c.Close()
		}
	}()
	return n
}

func (n *tlsNode) handshakes() (ok, failed int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, e := range n.shakes {
		if e == nil {
			ok++
		} else {
			failed++
		}
	}
	return ok, failed
}

// CX1-80 — имя проверки сертификата (ServerName) — узел адреса: IP-литерал
// против сертификата на имя — рукопожатие отвергнуто (недоверие); близнец —
// имя `localhost` на тот же узел — рукопожатие состоялось.
func TestRelayServerNameIsTheAddressHost(t *testing.T) {
	if addrs, err := net.LookupHost("localhost"); err != nil || len(addrs) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: имя localhost не разрешается на машине пробы: %v", err)
	}
	ca := smtptest.NewCA(t)
	node := startTLSNode(t, ca.Leaf(t, "localhost"))

	t.Run("IP-литерал против сертификата на имя", func(t *testing.T) {
		cfg := loadConfig(t, map[string]string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": relayURI("smtps", "", node.host, node.port)})
		a := sendOne(t, senderFor(t, cfg, ca.Pool()))
		if a.Failure != smtp.FailureUntrusted {
			t.Fatalf("сертификат на имя принят для IP-литерала адреса: попытка %+v (ожидалось недоверие)", a)
		}
	})
	t.Run("близнец: имя localhost", func(t *testing.T) {
		before, _ := node.handshakes()
		cfg := loadConfig(t, map[string]string{"KACHO_NOTIFY_SMTP_CONNECTION_URI": relayURI("smtps", "", "localhost", node.port)})
		a := sendOne(t, senderFor(t, cfg, ca.Pool()))
		if a.Failure == smtp.FailureUntrusted {
			t.Fatalf("сертификат на имя адреса не принят: попытка %+v — имя проверки не узел адреса", a)
		}
		waitHandshake(t, node, before)
	})
}

func waitHandshake(t *testing.T, n *tlsNode, before int) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if ok, _ := n.handshakes(); ok > before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("рукопожатие TLS на узле адреса не состоялось")
}
