// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testPKI — удостоверяющий центр пробы: корень и ключ.
type testPKI struct {
	ca  *x509.Certificate
	key *ecdsa.PrivateKey
}

func newPKI(t *testing.T) testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "probe-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testPKI{ca: ca, key: key}
}

// writeCA — корень в файл PEM.
func (p testPKI) writeCA(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.ca.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// issue — DER листа с именем DNS, подписанного корнем (клиентский).
func (p testPKI) issue(t *testing.T, dns string) []byte {
	t.Helper()
	der, _ := p.leaf(t, dns, x509.ExtKeyUsageClientAuth)
	return der
}

func (p testPKI) leaf(t *testing.T, dns string, eku x509.ExtKeyUsage) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), DNSNames: []string{dns},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{eku}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, p.ca, &key.PublicKey, p.key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// pair — пара «лист + ключ» для tls.Config.
func (p testPKI) pair(t *testing.T, dns string, eku x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	der, key := p.leaf(t, dns, eku)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// pipe — пара соединённых сокетов петли: у них есть буферы, и рукопожатие,
// которое одна сторона кончает раньше другой, не стоит на записи.
func pipe() (net.Conn, net.Conn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := l.Accept()
		accepted <- c
	}()
	cc, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		panic(err)
	}
	return cc, <-accepted
}
