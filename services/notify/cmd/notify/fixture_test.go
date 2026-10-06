// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// bootFixture — та же единственная фикстура проб старта, что у пакета
// конфигурации (CX1-79): второй копии нет.
const bootFixture = "../../internal/config/testdata/boot.env"

// fixtureEnv читает фикстуру в карту «переменная → значение».
func fixtureEnv(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(bootFixture)
	if err != nil {
		t.Fatalf("фикстура проб старта %s не открыта: %v", bootFixture, err)
	}
	defer func() { _ = f.Close() }()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("строка фикстуры без «=»: %q", line)
		}
		env[k] = v
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("фикстура не дочитана: %v", err)
	}
	if len(env) == 0 {
		t.Fatal("фикстура пуста")
	}
	// Пару DKIM фикстура не несёт: её выпускает проба в каталоге формы kubelet
	// (§12а, полоса N14), как peerTLSFiles — сертификат пира.
	dkimFiles(t, env)
	addressKeyFiles(t, env)
	return env
}

// addressKeyFiles выпускает в каталоге пробы формы kubelet файл ключа
// отпечатка адреса (Р15, Д23) и подставляет каталог в окружение: фикстура его
// не несёт, как пару DKIM.
func addressKeyFiles(t *testing.T, env map[string]string) {
	t.Helper()
	dir := t.TempDir()
	gen := "..2026_10_07_00_00_00.000000001"
	if err := os.Mkdir(filepath.Join(dir, gen), 0o755); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: поколение тома ключа отпечатка: %v", err)
	}
	name := config.AddressKeyFileName
	key := []byte(strings.Repeat("0123456789abcdef", 2*config.AddressKeyMinBytes/16))
	if err := os.WriteFile(filepath.Join(dir, gen, name), key, 0o600); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файл %s: %v", name, err)
	}
	if err := os.Symlink(filepath.Join("..data", name), filepath.Join(dir, name)); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ссылка %s: %v", name, err)
	}
	if err := os.Symlink(gen, filepath.Join(dir, "..data")); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ссылка ..data: %v", err)
	}
	env["KACHO_NOTIFY_ADDRESS_KEY_DIR"] = dir
}

// peerTLSFiles выпускает в каталоге пробы УЦ и сертификат службы notify с
// URI SAN SPIFFE и подставляет их пути в окружение.
func peerTLSFiles(t *testing.T, env map[string]string) {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ключ УЦ: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "notify-probe-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("сертификат УЦ: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ключ службы: %v", err)
	}
	spiffe, err := url.Parse("spiffe://kacho.cloud/ns/kacho/sa/kacho-notify")
	if err != nil {
		t.Fatalf("SPIFFE ID пробы: %v", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "kacho-notify"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{spiffe},
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("разбор УЦ: %v", err)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("сертификат службы: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("ключ службы в DER: %v", err)
	}

	write := func(name, typ string, der []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
			t.Fatalf("запись %s: %v", name, err)
		}
		return p
	}
	env["KACHO_NOTIFY_PEER_TLS_CA_FILE"] = write("ca.crt", "CERTIFICATE", caDER)
	env["KACHO_NOTIFY_PEER_TLS_CERT_FILE"] = write("tls.crt", "CERTIFICATE", leafDER)
	env["KACHO_NOTIFY_PEER_TLS_KEY_FILE"] = write("tls.key", "EC PRIVATE KEY", keyDER)
}

// freeAddr — свободный адрес петли для диагностической поверхности пробы.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("свободный порт: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("освободить порт: %v", err)
	}
	return addr
}
