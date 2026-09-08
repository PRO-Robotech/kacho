// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/config"
)

// b64uJSON — base64url(JSON(v)) без padding (сегмент JOSE).
func b64uJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TestDataplaneVerifier_FetchesEachIssuerKeySetFromItsDeclaredURL — сквозная
// провязка «настройка → проверяющий» на ДВУХ записях.
//
// Утверждается не «фетч случился», а «фетч ушёл ПО СВОЕМУ адресу для КАЖДОГО
// издателя»: при объединённом наборе или при адресе, выведенном из издателя, оба
// обращения пришли бы в одно место, и проба этого бы не заметила, если бы
// смотрела только на факт обращения.
func TestDataplaneVerifier_FetchesEachIssuerKeySetFromItsDeclaredURL(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}

	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer iam.Close()

	const (
		platformIssuer = "https://kaname.kacho.local"
		legacyIssuer   = "https://hydra.api.kacho.cloud"
		platformPath   = "/.well-known/kaname/jwks.json"
		legacyPath     = "/.well-known/jwks.json"
	)

	t.Setenv("KACHO_REGISTRY_DB_PASSWORD", "s3cr3t")
	t.Setenv("KACHO_REGISTRY_AUTH_MODE", "dev") // адреса пробного сервера — открытый HTTP
	t.Setenv("KACHO_REGISTRY_TOKEN_ISSUERS", platformIssuer+","+legacyIssuer)
	t.Setenv("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS",
		platformIssuer+"="+iam.URL+platformPath+","+legacyIssuer+"="+iam.URL+legacyPath)
	t.Setenv("KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER", platformIssuer)
	t.Setenv("KACHO_REGISTRY_TOKEN_REVOCATION_URL", iam.URL+"/internal/tokens/introspect")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	v, err := buildTokenVerifier(cfg)
	if err != nil {
		t.Fatalf("buildTokenVerifier: %v", err)
	}

	// Токен с законным заголовком и неизвестным идентификатором ключа: обращение
	// к источнику происходит ДО проверки подписи, поэтому подпись нерелевантна.
	probe := func(issuer, typ string) {
		header := b64uJSON(t, map[string]any{"alg": "RS256", "typ": typ, "kid": "probe"})
		claims := b64uJSON(t, map[string]any{"sub": "cid", "aud": cfg.ServiceAud, "iss": issuer, "exp": 1 << 40})
		tok := header + "." + claims + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
		_, _ = v.Verify(context.Background(), tok) // ожидаемо отвергается — важен адрес обращения
	}
	probe(platformIssuer, "at+jwt")
	probe(legacyIssuer, "JWT")

	mu.Lock()
	defer mu.Unlock()
	if paths[platformPath] == 0 {
		t.Fatalf("набор нашего издателя не запрашивался по объявленному адресу %q; обращения: %v", platformPath, paths)
	}
	if paths[legacyPath] == 0 {
		t.Fatalf("набор прежнего издателя не запрашивался по объявленному адресу %q; обращения: %v", legacyPath, paths)
	}
}

// TestDataplaneVerifier_UndeclaredIssuerNeverReachesAnySource — издатель без
// объявленной записи не приводит НИ К ОДНОМУ обращению: ни перебора записей, ни
// адреса, выведенного из самого издателя.
func TestDataplaneVerifier_UndeclaredIssuerNeverReachesAnySource(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer iam.Close()

	const platformIssuer = "https://kaname.kacho.local"
	t.Setenv("KACHO_REGISTRY_DB_PASSWORD", "s3cr3t")
	t.Setenv("KACHO_REGISTRY_AUTH_MODE", "dev")
	t.Setenv("KACHO_REGISTRY_TOKEN_ISSUERS", platformIssuer)
	t.Setenv("KACHO_REGISTRY_TOKEN_ISSUER_KEYSETS", platformIssuer+"="+iam.URL+"/.well-known/kaname/jwks.json")
	t.Setenv("KACHO_REGISTRY_PLATFORM_TOKEN_ISSUER", platformIssuer)
	t.Setenv("KACHO_REGISTRY_TOKEN_REVOCATION_URL", iam.URL+"/internal/tokens/introspect")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	v, err := buildTokenVerifier(cfg)
	if err != nil {
		t.Fatalf("buildTokenVerifier: %v", err)
	}

	header := b64uJSON(t, map[string]any{"alg": "RS256", "typ": "at+jwt", "kid": "probe"})
	claims := b64uJSON(t, map[string]any{
		"sub": "cid", "aud": cfg.ServiceAud, "iss": "https://not-declared.example", "exp": 1 << 40,
	})
	tok := header + "." + claims + "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))
	if _, verr := v.Verify(context.Background(), tok); verr == nil {
		t.Fatalf("издатель без объявленной записи обязан быть отвергнут")
	}

	mu.Lock()
	afterUndeclared := hits
	mu.Unlock()
	if afterUndeclared != 0 {
		t.Fatalf("издатель без записи стоил %d обращений к источнику; перебора записей быть не должно", afterUndeclared)
	}

	// Положительный контроль: объявленный издатель обращение всё-таки вызывает —
	// иначе «ноль обращений» верно и для проверяющего, который не ходит никуда.
	okClaims := b64uJSON(t, map[string]any{"sub": "cid", "aud": cfg.ServiceAud, "iss": platformIssuer, "exp": 1 << 40})
	_, _ = v.Verify(context.Background(), header+"."+okClaims+"."+base64.RawURLEncoding.EncodeToString([]byte("sig")))

	mu.Lock()
	afterDeclared := hits
	mu.Unlock()
	if afterDeclared == 0 {
		t.Fatalf("объявленный издатель обязан приводить к обращению по своему адресу")
	}
}
