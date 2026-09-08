// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// credential_strip_test.go — приёмка KAN-AUTHN-1, семейство STRIP.
//
// Предмет: край перестаёт ПЕРЕСЫЛАТЬ арендаторское удостоверение за себя,
// установив личность сам. Требование к вызывающему при этом не меняется ни на
// байт — меняется только то, что уезжает за край.

// TestKAN_STRIP_01_ForwardedRequestCarriesNoCredential — за краем удостоверения
// нет ни в какой форме и ни в каком регистре имени, а переданная личность есть
// и не изменилась.
func TestKAN_STRIP_01_ForwardedRequestCarriesNoCredential(t *testing.T) {
	t.Parallel()

	in := metadata.MD{}
	in.Set("authorization", "Bearer tenant-token")
	in.Set("Authorization", "Bearer tenant-token")
	in.Set(principalmeta.MetaBridgedCredential, "Bearer tenant-token")
	in.Set(principalmeta.MetaPrincipalType, "user")
	in.Set(principalmeta.MetaPrincipalID, "usr-1")
	in.Set(principalmeta.MetaTokenACR, "1")

	out, ok := metadata.FromOutgoingContext(
		principalmeta.OutgoingFromIncoming(metadata.NewIncomingContext(context.Background(), in)))
	if !ok {
		t.Fatal("исходящих метаданных нет — узел не собрал контекст пересылки")
	}

	for _, key := range []string{"authorization", "Authorization", principalmeta.MetaBridgedCredential} {
		if got := out.Get(key); len(got) != 0 {
			t.Errorf("удостоверение уехало за край под ключом %q: %v", key, got)
		}
	}
	if got := out.Get(principalmeta.MetaPrincipalType); len(got) != 1 || got[0] != "user" {
		t.Errorf("переданная личность (тип) изменилась: %v", got)
	}
	if got := out.Get(principalmeta.MetaPrincipalID); len(got) != 1 || got[0] != "usr-1" {
		t.Errorf("переданная личность (идентификатор) изменилась: %v", got)
	}
	if got := out.Get(principalmeta.MetaTokenACR); len(got) != 1 || got[0] != "1" {
		t.Errorf("контекст проверенного удостоверения изменился: %v", got)
	}
}

// TestKAN_STRIP_01_HTTPSurfaceCarriesNoCredential — та же полоса на HTTP-мосту.
//
// Ключ здесь именно в форме: мост библиотеки переносит удостоверение СВОИМ
// особым случаем, ДО обращения к сопоставителю заголовков, поэтому сузить его
// сопоставителем нельзя — снимается сам заголовок запроса.
func TestKAN_STRIP_01_HTTPSurfaceCarriesNoCredential(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	h := principalmeta.StripCredentialBeforeForwarding(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r }))

	r := httptest.NewRequest(http.MethodGet, "/iam/v1/accounts/acc-1", nil)
	r.Header.Set("Authorization", "Bearer tenant-token")
	r.Header.Set("Grpc-Metadata-Authorization", "Bearer tenant-token")
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-1")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if seen == nil {
		t.Fatal("обёртка не позвала следующего — пересылки не произошло")
	}
	for _, name := range []string{"Authorization", "authorization", "Grpc-Metadata-Authorization"} {
		if v := seen.Header.Get(name); v != "" {
			t.Errorf("удостоверение уехало за край в заголовке %q: %q", name, v)
		}
	}
	if got := seen.Header.Get(principalmeta.HeaderPrincipalType); got != "user" {
		t.Errorf("переданная личность (тип) изменилась: %q", got)
	}
	if got := seen.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-1" {
		t.Errorf("переданная личность (идентификатор) изменилась: %q", got)
	}
}

// TestKAN_STRIP_02_DirectCallKeepsTheCredential — ПОЛОЖИТЕЛЬНЫЙ БЛИЗНЕЦ.
//
// Отличается от KAN-STRIP-01 ровно ОДНИМ фактом: запрос не пересылается краем.
// Без него «за краем удостоверения нет» зеленело бы на сборке, где удостоверение
// не доезжает НИКУДА, — то есть на состоянии, где снятия нет вовсе, а сломан сам
// перенос.
func TestKAN_STRIP_02_DirectCallKeepsTheCredential(t *testing.T) {
	t.Parallel()

	// Ровно тот же вход, что у близнеца выше, минус пересылка краем.
	r := httptest.NewRequest(http.MethodGet, "/iam/v1/accounts/acc-1", nil)
	r.Header.Set("Authorization", "Bearer tenant-token")
	if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
		t.Fatalf("прямой запрос удостоверения не донёс: %q", got)
	}

	in := metadata.MD{}
	in.Set("authorization", "Bearer tenant-token")
	if got := in.Get("authorization"); len(got) != 1 {
		t.Fatalf("прямые метаданные удостоверения не донесли: %v", got)
	}

	// И снятие НЕ трогает ничего, кроме удостоверения: узел, снимающий лишнее,
	// был бы тем же дефектом с другой стороны.
	in.Set(principalmeta.MetaPrincipalID, "usr-1")
	stripped := principalmeta.StripPresentedCredential(in)
	if got := stripped.Get(principalmeta.MetaPrincipalID); len(got) != 1 || got[0] != "usr-1" {
		t.Errorf("узел снял не только удостоверение: %v", got)
	}
	if got := in.Get("authorization"); len(got) != 1 {
		t.Errorf("узел изменил ИСХОДНЫЕ метаданные вместо копии: %v", got)
	}
}

// TestKAN_STRIP_01_StripIsCaseInsensitive — «ни в каком регистре имени».
//
// Метаданные gRPC регистр ключа схлопывают сами, а заголовки HTTP — нет:
// `http.Header.Del` канонизирует имя, но клиент вправе прислать любое написание,
// и разбор обязан судить по нормализованному имени, а не по написанному.
func TestKAN_STRIP_01_StripIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	h := principalmeta.StripCredentialBeforeForwarding(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r }))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	// Минуя Set: он канонизирует имя, а предмет пробы — именно неканоническое.
	r.Header["AUTHORIZATION"] = []string{"Bearer tenant-token"}
	r.Header["grpc-metadata-AUTHORIZATION"] = []string{"Bearer tenant-token"}
	h.ServeHTTP(httptest.NewRecorder(), r)

	for name, vals := range seen.Header {
		if strings.Contains(strings.ToLower(name), "authorization") {
			t.Errorf("удостоверение уцелело под неканоническим именем %q: %v", name, vals)
		}
	}
}
