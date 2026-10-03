// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

// runRefused запускает процесс и ждёт отказа старта.
func runRefused(t *testing.T, env map[string]string, want ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, notifyBinary(t), "serve")
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

// Близнец: исправная посадка проходит стража целиком — дескриптор принят,
// удостоверение пира годно, самоотчёт напечатан, — и процесс доходит до
// следующего шага подъёма: соединения со своей базой. Базы у пробы нет
// (порт петли закрыт), поэтому процесс останавливается ИМЕННО на ней, и
// отказ называет пул, а не ручку посадки. Подъём целиком с базой под TLS —
// предмет профиля развёртывания на стенде, а не пробы процесса.
func TestNotifyProcessPassesTheGuardWithSoundPosture(t *testing.T) {
	env := fixtureEnv(t)
	peerTLSFiles(t, env)
	env["KACHO_NOTIFY_DIAG_ADDR"] = freeAddr(t)
	_, closedPort, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatalf("закрытый порт базы: %v", err)
	}
	env["KACHO_NOTIFY_DB_HOST"] = "127.0.0.1"
	env["KACHO_NOTIFY_DB_PORT"] = closedPort

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, notifyBinary(t), "serve")
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
		t.Fatalf("без базы процесс не остановился ненулевым кодом (err=%v):\n%s", runErr, log)
	}
	for _, w := range []string{
		`"msg":"boot security posture"`, `"host_form":"no-grpc"`, `"db_sslmode":"require"`,
		`"auth_mode":"production"`, "пул базы kacho_notify",
	} {
		if !strings.Contains(log, w) {
			t.Fatalf("исправная посадка не дошла до шага базы — нет %s:\n%s", w, log)
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
