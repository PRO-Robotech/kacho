// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// anchor_test.go — КОРЕНЬ ЦЕПОЧКИ РЕШАЕТ, ЗВЕНО ЭТО ИЛИ НАГРУЗКА УСТАНОВКИ
// (kacho#3028, круг 5). Цепочки строятся настоящими сертификатами: якорь
// узнаёт корень по байтам, и синтетический лист без байтов якорем не
// признаётся никогда.
package linktls_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/linktls"
)

func testCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c, key
}

func testLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dns string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{dns},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Лист якоря звеньев — звено, не нагрузка; лист якоря установки с ТЕМ ЖЕ
// именем — нагрузка, не звено. Отличие близнецов — один факт: корень цепочки.
func TestAnchor_TheRootDecidesLinkOrWorkload(t *testing.T) {
	linkCA, linkKey := testCA(t, "front-link-ca")
	instCA, instKey := testCA(t, "kacho-internal-ca")
	const name = "api-gateway-front-console.front-link.kacho.internal"
	a := linktls.NewAnchor(linkCA)

	linkChain := [][]*x509.Certificate{{testLeaf(t, linkCA, linkKey, name), linkCA}}
	instChain := [][]*x509.Certificate{{testLeaf(t, instCA, instKey, name), instCA}}

	if a.Issued(linkChain) == nil {
		t.Error("лист якоря звеньев не признан звеном")
	}
	if a.Foreign(linkChain) != nil {
		t.Error("лист якоря звеньев отдан полосе личности как нагрузка")
	}
	if a.Issued(instChain) != nil {
		t.Error("лист якоря установки с именем звена признан звеном")
	}
	if a.Foreign(instChain) == nil {
		t.Error("лист якоря установки не отдан полосе личности")
	}
}

// Корень, совпадающий с корнем якоря именем субъекта, но не байтами, — чужой:
// имя субъекта пишет тот, кто выпустил корень.
func TestAnchor_SameSubjectOtherKeyIsForeign(t *testing.T) {
	linkCA, _ := testCA(t, "front-link-ca")
	twinCA, twinKey := testCA(t, "front-link-ca")
	a := linktls.NewAnchor(linkCA)
	chain := [][]*x509.Certificate{{testLeaf(t, twinCA, twinKey, "x.front-link.kacho.internal"), twinCA}}
	if a.Issued(chain) != nil {
		t.Fatal("корень с тем же субъектом и другим ключом признан якорем звеньев")
	}
}

// Нулевой якорь — звеньев нет: Issued пуст всегда, Foreign отдаёт лист.
func TestAnchor_ZeroValueHasNoLinks(t *testing.T) {
	ca, key := testCA(t, "any")
	chain := [][]*x509.Certificate{{testLeaf(t, ca, key, "x.example"), ca}}
	var a linktls.Anchor
	if !a.Empty() || a.Issued(chain) != nil || a.Foreign(chain) == nil {
		t.Fatalf("нулевой якорь: Empty=%v Issued=%v Foreign=%v", a.Empty(), a.Issued(chain), a.Foreign(chain))
	}
}
