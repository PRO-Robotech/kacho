// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ka1_call_budget_start_test.go — приёмка KA1, сценарии KA1-20 и KA1-21 (Р4):
// бюджет вызова края к соседу объявлен ручкой, судится стражем старта,
// умолчания нет.
//
// Уровень — СТАРТ ПРОЦЕССА: процесс края запускается настоящим `main` (тот же
// двоичный файл пробы, перезапущенный с признаком `KA1_EDGE_PROCESS`), с
// окружением, полным во всём, кроме испытуемой ручки. Утверждается только исход
// старта и его журнал — ни одного запроса, кроме `/healthz` близнеца.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ka1EdgeProcessFlag — признак, по которому двоичный файл пробы исполняет `main`.
const ka1EdgeProcessFlag = "KA1_EDGE_PROCESS"

func TestMain(m *testing.M) {
	if os.Getenv(ka1EdgeProcessFlag) == "1" {
		main()
		return
	}
	// Пробы пакета, собирающие конфигурацию загрузчиком (`config.Load`), видят
	// ручки Р4 в величинах профилей — как процесс на любом стенде. Умолчания в
	// загрузчике нет, и без объявления загрузчик отказывает: это предмет
	// KA1-20/21 (процесс пробы получает своё окружение целиком, а не это).
	for k, v := range map[string]string{
		"KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET": "1s",
		"KACHO_API_GATEWAY_BACKEND_CALL_BUDGET":  "30s",
	} {
		if _, ok := os.LookupEnv(k); !ok {
			_ = os.Setenv(k, v)
		}
	}
	os.Exit(m.Run())
}

// ka1Material — клиентский сертификат и якорь доверия для хопов, которых
// требует посадка `own` (содержимое значения не имеет: на старте край к сети не
// ходит).
func ka1Material(t *testing.T) (cert, key, ca string) {
	t.Helper()
	dir := t.TempDir()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("spiffe://kacho.test/ns/kacho/sa/api-gateway")
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "api-gateway"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		URIs: []*url.URL{u}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	cert, key = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cert, key, cert
}

func ka1FreeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// ka1EdgeEnv — окружение края, полное во всём (посадка `own`, адресат,
// издатели, хопы к службе доступа), с ОБЕИМИ ручками Р4 в годных величинах.
func ka1EdgeEnv(t *testing.T, mode string) (env map[string]string, listen string) {
	t.Helper()
	cert, key, ca := ka1Material(t)
	listen = ka1FreeAddr(t)
	appEnv := "dev"
	if mode != "dev" {
		appEnv = "production"
	}
	return map[string]string{
		"KACHO_APP_ENV":                                         appEnv,
		"KACHO_API_GATEWAY_AUTHN_MODE":                          mode,
		"KACHO_API_GATEWAY_AUTHN_TRUST_DOMAIN":                  "kacho.test",
		"KACHO_API_GATEWAY_LISTEN_ADDR":                         listen,
		"KACHO_API_GATEWAY_INTERNAL_REST_ADDR":                  ka1FreeAddr(t),
		"KACHO_API_GATEWAY_METRICS_ADDR":                        ka1FreeAddr(t),
		"KACHO_API_GATEWAY_IDENTITY_PROVIDER":                   "own",
		"KACHO_API_GATEWAY_TOKEN_AUDIENCE":                      "https://api.kacho.test",
		"KACHO_API_GATEWAY_TOKEN_ISSUERS":                       "https://kaname.kacho.local",
		"KACHO_API_GATEWAY_TOKEN_ISSUER_KEYSETS":                "https://kaname.kacho.local=https://kaname.kacho.local/.well-known/jwks.json",
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_ISSUER":               "https://kaname.kacho.local",
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_URL":       "https://kaname.kacho.local/internal/tokens/introspect",
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CA_FILE":   ca,
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_CERT_FILE": cert,
		"KACHO_API_GATEWAY_PLATFORM_TOKEN_REVOCATION_KEY_FILE":  key,
		"KACHO_API_GATEWAY_IAM_LOGIN_LANE_URL":                  "https://127.0.0.1:1",
		"KACHO_API_GATEWAY_IAM_ISSUANCE_URL":                    "https://127.0.0.1:2",
		"KACHO_API_GATEWAY_MTLS_CLIENT_CERT_FILE":               cert,
		"KACHO_API_GATEWAY_MTLS_CLIENT_KEY_FILE":                key,
		"KACHO_API_GATEWAY_MTLS_CA_FILE":                        ca,
		"KACHO_API_GATEWAY_AUTHN_ENFORCE_STEP_UP":               "true",
		"KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET":                "1s",
		"KACHO_API_GATEWAY_BACKEND_CALL_BUDGET":                 "30s",
	}, listen
}

type ka1Start struct {
	exited  bool
	code    int
	journal string
	healthz int
}

// ka1RunEdge запускает процесс края. Ждёт либо завершения (исход старта), либо
// ответа `/healthz` (процесс стартовал) — и тогда останавливает его сам.
func ka1RunEdge(t *testing.T, env map[string]string, listen string) ka1Start {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), ka1EdgeProcessFlag + "=1"}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				code = -1
			}
			return ka1Start{exited: true, code: code, journal: out.String()}
		case <-tick.C:
			resp, err := http.Get("http://" + listen + "/healthz")
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			_ = cmd.Process.Kill()
			<-done
			return ka1Start{healthz: resp.StatusCode, journal: out.String()}
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-done
			t.Fatalf("процесс края не завершился и не ответил /healthz за 30s:\n%s", out.String())
		}
	}
}

// ka1StartGuard — сценарий стража старта для одной ручки (KA1-20, KA1-21).
func ka1StartGuard(t *testing.T, knob, twin string) {
	t.Helper()
	variants := []struct {
		name     string
		set      bool
		value    string
		declared bool
	}{
		{"(а) не задана", false, "", false},
		{"(б) пустая строка", true, "", true},
		{"(в) abc", true, "abc", true},
		{"(г) 0s", true, "0s", true},
		{"(д) -1s", true, "-1s", true},
	}
	for _, mode := range []string{"production", "dev"} {
		for _, v := range variants {
			t.Run(mode+" "+v.name, func(t *testing.T) {
				env, listen := ka1EdgeEnv(t, mode)
				delete(env, knob)
				if v.set {
					env[knob] = v.value
				}
				got := ka1RunEdge(t, env, listen)
				if !got.exited || got.code == 0 {
					t.Fatalf("процесс обязан завершиться с ненулевым кодом до открытия слушателей; стартовал (/healthz %d)", got.healthz)
				}
				if strings.Contains(got.journal, `"msg":"api-gateway started"`) {
					t.Errorf("отказ пришёл после открытия слушателя:\n%s", got.journal)
				}
				if !strings.Contains(got.journal, knob) {
					t.Errorf("текст отказа не называет %s:\n%s", knob, ka1Tail(got.journal))
				}
				if v.declared {
					want := "объявлено, но не годится"
					if !strings.Contains(got.journal, want) || !strings.Contains(got.journal, fmt.Sprintf("%q", v.value)) {
						t.Errorf("текст отказа обязан сказать «%s» и привести значение %q:\n%s", want, v.value, ka1Tail(got.journal))
					}
				} else if !strings.Contains(got.journal, "не объявлено") {
					t.Errorf("текст отказа обязан сказать «не объявлено»:\n%s", ka1Tail(got.journal))
				}
			})
		}
	}
	t.Run("близнец "+twin, func(t *testing.T) {
		env, listen := ka1EdgeEnv(t, "dev")
		env[knob] = twin
		got := ka1RunEdge(t, env, listen)
		if got.exited || got.healthz != http.StatusOK {
			t.Fatalf("с %s=%s процесс обязан стартовать и отвечать /healthz 200; исход: завершился=%v код=%d\n%s",
				knob, twin, got.exited, got.code, ka1Tail(got.journal))
		}
	})
}

func ka1Tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

// KA1-20 — страж старта: бюджет вызова края к службе доступа.
func TestKA1_20_IdentityCallBudgetIsJudgedAtStart(t *testing.T) {
	ka1StartGuard(t, "KACHO_API_GATEWAY_IDENTITY_CALL_BUDGET", "1s")
}

// KA1-21 — страж старта: бюджет вызова моста к бэкенду.
func TestKA1_21_BackendCallBudgetIsJudgedAtStart(t *testing.T) {
	ka1StartGuard(t, "KACHO_API_GATEWAY_BACKEND_CALL_BUDGET", "30s")
}
