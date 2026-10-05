// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/dnscheck/dnstest"
)

// Пробы этого файла запускают НАСТОЯЩИЙ бинарь notify: отказ стража доказан
// исходом процесса (ненулевой код и имя ручки), а не чтением кода стража.

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
	buildDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// notifyBinary собирает бинарь один раз на прогон пакета.
func notifyBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "notify-boot-")
		if buildErr != nil {
			return
		}
		binPath = filepath.Join(buildDir, "kacho-notify")
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("go build: %w\n%s", err, out.String())
		}
	})
	if buildErr != nil {
		t.Fatalf("бинарь notify не собран — проба старта не исполнилась: %v", buildErr)
	}
	return binPath
}

// processEnv — окружение процесса: только фикстура и то, без чего не
// исполняется бинарь. Окружение прогона пробы в процесс не протекает.
func processEnv(env map[string]string) []string {
	out := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// processDeadline — срок одного процесса notify в пробе. Он судит только
// процесс: бинарь собирается ДО того, как срок взведён (notifyBinary), иначе
// холодная сборка первого подслучая съедает срок и отказ стража неотличим от
// зависшего старта.
const processDeadline = 30 * time.Second

// runRefused запускает процесс и ждёт отказа старта.
func runRefused(t *testing.T, env map[string]string, want ...string) {
	t.Helper()
	bin := notifyBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), processDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = processEnv(env)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("процесс не завершился за срок — старт не отвергнут:\n%s", out.String())
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() == 0 {
		t.Fatalf("старт не отвергнут ненулевым кодом (err=%v):\n%s", err, out.String())
	}
	for _, w := range want {
		if !strings.Contains(out.String(), w) {
			t.Fatalf("отказ старта не называет %q:\n%s", w, out.String())
		}
	}
	if strings.Contains(out.String(), "servicehost: поверхность поднята") {
		t.Fatalf("отказ пришёл после подъёма поверхности, а не до него:\n%s", out.String())
	}
}

func TestNotifyProcessRefusesUnsafePosture(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]string)
		want []string
	}{
		{"E04: таймер Claim не задан", func(e map[string]string) { delete(e, "KACHO_NOTIFY_CLAIM_INTERVAL") },
			[]string{"notify.claimInterval", "KACHO_NOTIFY_CLAIM_INTERVAL"}},
		{"G01: перечень источников пуст", func(e map[string]string) { e["KACHO_NOTIFY_SOURCES"] = "[]" },
			[]string{"notify.sources", "пуст"}},
		{"G06: origin не задан", func(e map[string]string) { delete(e, "KACHO_NOTIFY_ORIGIN") },
			[]string{"notify.origin"}},
		{"G15: authMode не production", func(e map[string]string) { e["KACHO_NOTIFY_AUTH_MODE"] = "dev" },
			[]string{"notify.authMode", "ось authMode"}},
		{"G15: mTLS выключен", func(e map[string]string) { delete(e, "KACHO_NOTIFY_PEER_TLS_CERT_FILE") },
			[]string{"notify.peerTLS.certFile", "ось mTLS"}},
		{"G15: файл удостоверения пира не читается", func(e map[string]string) {
			e["KACHO_NOTIFY_PEER_TLS_CERT_FILE"] = filepath.Join(t.TempDir(), "absent.crt")
		}, []string{"notify.peerTLS.certFile", "ось mTLS"}},
		{"G15: секрет почты не смонтирован", func(e map[string]string) {
			e["KACHO_NOTIFY_SMTP_CONNECTION_URI"] = "smtp://u@h:587/"
		}, []string{"notify.smtp.credential", "секрет почты не смонтирован"}},
		{"G15: sslmode не require", func(e map[string]string) { e["KACHO_NOTIFY_DB_SSLMODE"] = "disable" },
			[]string{"DBSSLMode", "disable", "require, verify-ca, verify-full"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := fixtureEnv(t)
			peerTLSFiles(t, env)
			env["KACHO_NOTIFY_DIAG_ADDR"] = freeAddr(t)
			c.edit(env)
			runRefused(t, env, c.want...)
		})
	}
}

// Близнец: исправная посадка проходит стража конфигурации целиком —
// дескриптор принят, удостоверение пира годно, самоотчёт напечатан, — и
// НАСТОЯЩИЙ бинарь доходит до следующего шага подъёма после поверхности:
// стража DNS установки (§12а «Порядок подъёма», шаг 5). Резолвер бинаря —
// резолвер машины прогона, зоны испытания у него нет и быть не может
// (резолвер — порт `runServe`, а не ручка), поэтому процесс останавливается
// ИМЕННО на страже DNS: отказ называет его, а не ручку посадки. Срок стража
// сужен до нижней границы, чтобы исход «ответа нет» укладывался в срок пробы.
// Шаг пула базы за стражем судит проба в процессе
// (TestNotifyServeReachesTheDatabaseStepPastTheDNSGuard) — на зоне испытания.
func TestNotifyProcessPassesTheGuardWithSoundPosture(t *testing.T) {
	env := fixtureEnv(t)
	peerTLSFiles(t, env)
	env["KACHO_NOTIFY_DIAG_ADDR"] = freeAddr(t)
	env["KACHO_NOTIFY_DNS_BOOT_DEADLINE"] = "10s"

	bin := notifyBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), processDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = processEnv(env)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("процесс не завершился за срок:\n%s", out.String())
	}
	log := out.String()

	var ee *exec.ExitError
	if !errors.As(runErr, &ee) || ee.ExitCode() == 0 {
		t.Fatalf("без зоны испытания процесс не остановился ненулевым кодом (err=%v):\n%s", runErr, log)
	}
	for _, w := range []string{
		`"msg":"boot security posture"`, `"host_form":"no-grpc"`, `"db_sslmode":"require"`,
		`"auth_mode":"production"`, "servicehost: поверхность поднята", "страж DNS установки",
	} {
		if !strings.Contains(log, w) {
			t.Fatalf("исправная посадка не дошла до стража DNS — нет %s:\n%s", w, log)
		}
	}
	for _, refusal := range []string{"notify отказывается стартовать", "дескриптор не принят", "ось mTLS"} {
		if strings.Contains(log, refusal) {
			t.Fatalf("исправную посадку отверг страж (%q):\n%s", refusal, log)
		}
	}
	if strings.Contains(log, env["KACHO_NOTIFY_DB_PASSWORD"]) {
		t.Fatalf("журнал процесса раскрыл пароль базы:\n%s", log)
	}
}

// Близнец в процессе: на зоне испытания с исправными записями Р19 (NTF1-P01)
// страж DNS пройден, и подъём доходит до шага пула своей базы. Базы у пробы
// нет (порт петли закрыт), поэтому runServe возвращает отказ, называющий
// пул, а не ручку посадки и не проверку DNS. Подъём целиком с базой под TLS —
// предмет профиля развёртывания на стенде, а не пробы процесса.
func TestNotifyServeReachesTheDatabaseStepPastTheDNSGuard(t *testing.T) {
	_, closedPort, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: закрытый порт базы: %v", err)
	}
	cfg := loadConfig(t, map[string]string{
		"KACHO_NOTIFY_DIAG_ADDR": freeAddr(t),
		"KACHO_NOTIFY_DB_HOST":   "127.0.0.1",
		"KACHO_NOTIFY_DB_PORT":   closedPort,
	})
	env := map[string]string{}
	peerTLSFiles(t, env)
	cfg.PeerTLSCertFile = env["KACHO_NOTIFY_PEER_TLS_CERT_FILE"]
	cfg.PeerTLSKeyFile = env["KACHO_NOTIFY_PEER_TLS_KEY_FILE"]
	cfg.PeerTLSCAFile = env["KACHO_NOTIFY_PEER_TLS_CA_FILE"]

	zone := dnstest.Start(t)
	zone.PublishSound(cfg.FromDomain(), fixtureDKIMSelector, fixtureDKIMPublicKey(t))

	var buf syncBuffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	done := make(chan error, 1)
	go func() { done <- runServe(cfg, logger, zone.Resolver()) }()
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(processDeadline):
		t.Fatalf("runServe не вернулся за %v:\n%s", processDeadline, buf.String())
	}
	log := buf.String()
	if runErr == nil || !strings.Contains(runErr.Error(), "пул базы kacho_notify") {
		t.Fatalf("подъём не дошёл до шага пула базы за стражем DNS (err=%v):\n%s", runErr, log)
	}
	for _, w := range []string{`"msg":"boot security posture"`, "страж DNS установки пройден"} {
		if !strings.Contains(log, w) {
			t.Fatalf("журнал подъёма не несёт %s:\n%s", w, log)
		}
	}
	if strings.Contains(log, cfg.DBPassword) {
		t.Fatalf("журнал раскрыл пароль базы:\n%s", log)
	}
}

// syncBuffer — буфер журнала, в который пишут горутины подъёма.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
