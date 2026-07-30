// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// jwks_hop_client_test.go — ХОП ЗА КЛЮЧАМИ ВЕРИФИКАЦИИ ОБЯЗАН УМЕТЬ ЗАЩИЩЁННЫЙ
// ТРАНСПОРТ, И ЭТО ПРОВЕРЯЕТСЯ ПОВЕДЕНИЕМ, А НЕ НАСТРОЙКОЙ.
//
// По этому хопу край забирает материал, которым проверяет ПОДПИСЬ каждого
// предъявителя. Подменивший его в пути подменяет и решение о доступе — дальше край
// верит собственному ответу. Значит хоп обязан ходить по защищённому транспорту, а
// сертификат на внутрикластерном адресе выписан внутренним центром доверия,
// которого в корнях процесса по умолчанию НЕТ.
//
// Пока ручки доверия у края не было, «перевести хоп на защищённый транспорт» было
// невозможно в принципе: клиент шёл транспортом по умолчанию и отвергал внутренний
// сертификат как выданный неизвестным центром. Обойти это можно было двумя
// способами, и оба плохи — увести край НАПРЯМУЮ к провайдеру мимо фасада (рецидив
// уже однажды найденного и починенного обхода) либо снять проверку сертификата (то
// есть объявить защиту и не выполнять её). Поэтому ручка, а не флаг.
//
// ПОЧЕМУ ТЕСТ ПОВЕДЕНЧЕСКИЙ. «В значениях указан путь к связке» не говорит ничего о
// том, что связка ПРИМЕНЕНА: путь можно указать и не прочитать. Здесь поднимается
// настоящий сервер с настоящим сертификатом и проверяется, что происходит С
// МАТЕРИАЛОМ: с правильной связкой он доезжает, с чужой — нет.

const testJWKSBody = `{"keys":[{"kty":"EC","crv":"P-256","kid":"probe",` +
	`"x":"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU",` +
	`"y":"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"}]}`

func jwksTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testJWKSBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// caFileOf writes the server's own certificate out as the PEM bundle an operator
// would hand the process.
func caFileOf(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	blob := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, blob, 0o600); err != nil {
		t.Fatalf("запись %s: %v", name, err)
	}
	return p
}

// writeForeignCA выписывает валидный, но ЧУЖОЙ самоподписанный сертификат и кладёт
// его как связку оператора.
func writeForeignCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ключ: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "foreign-ca.invalid"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	p := filepath.Join(t.TempDir(), "foreign-ca.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("запись: %v", err)
	}
	return p
}

func resolveThrough(t *testing.T, url, caFile string) error {
	t.Helper()
	client, err := newJWKSHopClient(caFile, 5*time.Second)
	if err != nil {
		t.Fatalf("клиент хопа за ключами: %v", err)
	}
	cache := middleware.NewJWKSCache(url, time.Minute, client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = cache.Resolve(ctx, "probe")
	return err
}

// TestJWKSHopClient_TrustBundleIsApplied.
func TestJWKSHopClient_TrustBundleIsApplied(t *testing.T) {
	srv := jwksTLSServer(t)
	url := srv.URL + "/.well-known/jwks.json"

	t.Run("со связкой оператора материал доезжает", func(t *testing.T) {
		// ЭТО И ЕСТЬ ТО, ЧЕГО НЕ БЫЛО. Без ручки доверия край не мог забрать ключи по
		// защищённому внутрикластерному адресу вовсе — поэтому профиль стенда оставался
		// на незащищённом транспорте, а канонический уходил в обход фасада.
		if err := resolveThrough(t, url, caFileOf(t, srv, "ca.pem")); err != nil {
			t.Fatalf("с доверенной связкой ключ обязан резолвиться, получено: %v", err)
		}
	})

	t.Run("с ЧУЖОЙ связкой материал отвергается", func(t *testing.T) {
		// Связка синтаксически корректна, но это ЧУЖОЙ сертификат. Материал, пришедший
		// не по доверенному транспорту, приниматься не должен — иначе ручка доверия
		// была бы украшением.
		//
		// Чужой сертификат выписывается здесь, а не берётся у второго тестового сервера:
		// вспомогательный сервер стандартной библиотеки предъявляет ОДИН И ТОТ ЖЕ
		// встроенный сертификат на всех экземплярах, поэтому «связка соседа» оказалась бы
		// той же самой связкой — и проба, которая обязана краснеть, зеленела бы. Первый
		// прогон этой пробы именно так и прошёл; сама проба и была дефектной.
		if err := resolveThrough(t, url, writeForeignCA(t)); err == nil {
			t.Fatal("материал, пришедший не по доверенному транспорту, ПРИНЯТ — " +
				"сертификат не проверяется")
		}
	})

	t.Run("нечитаемая связка — ОТКАЗ, а не тихий откат к системным корням", func(t *testing.T) {
		if _, err := newJWKSHopClient(filepath.Join(t.TempDir(), "нет-такого.pem"), time.Second); err == nil {
			t.Fatal("отсутствующая связка обязана отказывать: продолжить на системных " +
				"корнях значит объявить проверку и не выполнять её")
		}
	})

	t.Run("связка без сертификата — ОТКАЗ", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "пусто.pem")
		if err := os.WriteFile(p, []byte("не сертификат"), 0o600); err != nil {
			t.Fatalf("запись: %v", err)
		}
		if _, err := newJWKSHopClient(p, time.Second); err == nil {
			t.Fatal("связка без единого сертификата даёт ПУСТОЕ хранилище доверия — " +
				"каждое рукопожатие отказывало бы навсегда")
		}
	})

	t.Run("пустой путь — прежнее поведение, транспорт по умолчанию", func(t *testing.T) {
		// Незащищённый внутрикластерный адрес связки не требует, и выдумывать её значило
		// бы отказать стенду, настроенному так намеренно.
		if _, err := newJWKSHopClient("   ", time.Second); err != nil {
			t.Fatalf("пустой путь обязан оставлять прежнее поведение, получено: %v", err)
		}
	})
}
