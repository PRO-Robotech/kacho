// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_strip_test.go — оператор снятия перед РЕТРАНСЛЯЦИЕЙ на полосу формы
// (приёмка Ф3 Р2; круг 2 Б-2): тот же оператор, что снимает удостоверение,
// расширенный на ВСЁ пространство `x-kacho-` в обеих формах написания.
//
// Две поверхности пересылки — два оператора одного ядра, и различие названо:
//   - REST→gRPC мост (`StripCredentialBeforeForwarding`) снимает ТОЛЬКО
//     удостоверение: за краем действует переданная личность, и её заголовки
//     обязаны доехать до владельца;
//   - ретрансляция на слушатель формы (`StripCredentialAndIdentityHeaders`)
//     снимает удостоверение И пространство личности: на той поверхности личность
//     производит один механизм — носитель, — и переданную личность там не читает
//     никто (Р16). Уехавшие шесть заголовков принципала были бы личностью, которую
//     полоса выставила по проверенному носителю, на поверхности, где её никто не
//     проверял бы заново.
package principalmeta_test

import (
	"net/http"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

func relayInbound() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer presented")
	h.Set("Grpc-Metadata-Authorization", "Bearer bridged")
	// Шесть заголовков принципала, которые полоса личности пишет в запрос до
	// его продолжения (§1.10 приёмки), плюс присланное клиентом.
	h.Set(principalmeta.HeaderPrincipalType, "user")
	h.Set(principalmeta.HeaderPrincipalID, "usr-1")
	h.Set(principalmeta.HeaderPrincipalDisplay, "A")
	h.Set(principalmeta.HeaderGRPCMetaPrincipalType, "user")
	h.Set(principalmeta.HeaderGRPCMetaPrincipalID, "usr-1")
	h.Set(principalmeta.HeaderGRPCMetaPrincipalDisplay, "A")
	h.Set(principalmeta.HeaderTokenACR, "1")
	h.Set("X-Kacho-Admin", "true")
	h.Set("grpc-metadata-x-kacho-project-id", "prj-1")
	// То, что ОБЯЗАНО доехать.
	h.Set("Cookie", "kaname_session=abc; kaname_form=def")
	h.Set("Content-Type", "application/json")
	h.Set("X-Forwarded-For", "10.0.0.1")
	return h
}

func kachoNamespaceHeaders(h http.Header) []string {
	var out []string
	for name := range h {
		if _, ok := principalmeta.KachoNamespaceKey(name); ok {
			out = append(out, name)
		}
	}
	return out
}

// TestRelayStrip_F3_51_RemovesTheCredentialAndTheWholeIdentityNamespace —
// после снятия в запросе нет ни удостоверения (обе формы), ни одного заголовка
// пространства `x-kacho-` (обе формы); печенья, тип содержимого и адрес источника
// на месте.
func TestRelayStrip_F3_51_RemovesTheCredentialAndTheWholeIdentityNamespace(t *testing.T) {
	h := relayInbound()
	before := len(kachoNamespaceHeaders(h))
	if before != 9 {
		t.Fatalf("фикстура несёт %d заголовков пространства, ожидалось 9 — положительный контроль оператора", before)
	}
	principalmeta.StripCredentialAndIdentityHeaders(h)

	if left := kachoNamespaceHeaders(h); len(left) != 0 {
		t.Fatalf("после снятия остались заголовки пространства x-kacho-: %v", left)
	}
	for _, name := range []string{"Authorization", "Grpc-Metadata-Authorization"} {
		if h.Get(name) != "" {
			t.Fatalf("удостоверение уехало бы на слушатель формы: %s=%q", name, h.Get(name))
		}
	}
	for name, want := range map[string]string{
		"Cookie":          "kaname_session=abc; kaname_form=def",
		"Content-Type":    "application/json",
		"X-Forwarded-For": "10.0.0.1",
	} {
		if h.Get(name) != want {
			t.Fatalf("%s снят или изменён: %q", name, h.Get(name))
		}
	}
}

// Положительный контроль различия операторов: мост REST→gRPC пространство НЕ
// снимает — иначе за краем действовала бы анонимность вместо переданной
// личности. Различие двух операторов — решение, а не побочный эффект.
func TestRelayStrip_F3_51_TheBridgeOperatorKeepsTheIdentityNamespace(t *testing.T) {
	h := relayInbound()
	req, _ := http.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
	req.Header = h
	var seen http.Header
	principalmeta.StripCredentialBeforeForwarding(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.Header
	})).ServeHTTP(nil, req)
	if seen.Get("Authorization") != "" {
		t.Fatal("мост обязан снимать удостоверение")
	}
	if seen.Get(principalmeta.HeaderPrincipalID) != "usr-1" {
		t.Fatal("мост снял переданную личность — за краем действовала бы анонимность")
	}
}

// Регистр не различается: `AUTHORIZATION` и `X-KACHO-ADMIN` снимаются так же.
func TestRelayStrip_F3_51_StripIsCaseInsensitive(t *testing.T) {
	h := http.Header{}
	h["AUTHORIZATION"] = []string{"Bearer x"}
	h["X-KACHO-ADMIN"] = []string{"true"}
	h["GRPC-METADATA-X-KACHO-PRINCIPAL-ID"] = []string{"usr-forged"}
	principalmeta.StripCredentialAndIdentityHeaders(h)
	if len(h) != 0 {
		t.Fatalf("остались заголовки в нестандартном регистре: %v", h)
	}
}
