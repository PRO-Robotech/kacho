// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta_test

// credential_test.go — что край СНИМАЕТ с запроса, собираясь спросить авторитет
// отзыва про предъявленное (kacho#1410).
//
// Каждое отрицание здесь стоит в паре с положительным контролем: без пары
// «величина не снята» зеленело бы на читателе, не снимающем ничего.

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

func req(headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/subscription/v1/events", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestCredentialFromRequest_TokenLaneCarriesItsIdentifier(t *testing.T) {
	c := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType: "service_account",
		principalmeta.HeaderPrincipalID:   "sva00000000000000001",
		principalmeta.HeaderTokenJti:      "jti-abc",
	}))
	if c.JTI != "jti-abc" {
		t.Fatalf("идентификатор удостоверения %q — спросить авторитет было бы не о чем", c.JTI)
	}
	// Отсечка ключуется ЛЮДЬМИ: спрашивать про служебную учётку по словарю людей
	// значило бы задавать вопрос про субъекта, которого в таблице не бывает.
	if c.UserID != "" {
		t.Fatalf("служебная учётка попала в вопрос про человека: %q", c.UserID)
	}
	if !c.Askable() {
		t.Fatal("удостоверение с идентификатором объявлено неспрашиваемым")
	}
}

func TestCredentialFromRequest_BrowserLaneCarriesSubjectAndInstant(t *testing.T) {
	at := time.Now().Add(-time.Hour).Truncate(time.Second).UTC()
	c := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
		// `jti` не ставится: у браузерной сессии его нет вовсе.
		principalmeta.HeaderTokenMfaAt: strconv.FormatInt(at.Unix(), 10),
	}))
	if c.JTI != "" {
		t.Fatalf("у браузерной сессии появился идентификатор удостоверения %q", c.JTI)
	}
	if c.UserID != "usr00000000000000001" {
		t.Fatalf("человек не назван (%q) — спросить про отсечку было бы не о ком", c.UserID)
	}
	if !c.AuthenticatedAt.Equal(at) {
		t.Fatalf("момент подтверждения %v, ожидался %v — сравнивать с отсечкой было бы нечего",
			c.AuthenticatedAt, at)
	}
	if !c.Askable() {
		t.Fatal("браузерная сессия объявлена неспрашиваемой")
	}
}

func TestCredentialFromRequest_BridgeFormIsReadWhereItExists(t *testing.T) {
	c := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderGRPCMetaPrincipalType: "user",
		principalmeta.HeaderGRPCMetaPrincipalID:   "usr00000000000000002",
		principalmeta.HeaderGRPCMetaTokenJti:      "jti-bridge",
	}))
	if c.JTI != "jti-bridge" || c.UserID != "usr00000000000000002" {
		t.Fatalf("мостовая форма заголовков не прочитана: %+v — полоса аутентификации ставит "+
			"обе формы, и читатель одной пропускал бы удостоверение целиком", c)
	}
}

// TestCredentialFromRequest_InstantHasNoBridgeFormAndIsNotReadFromOne — ключ
// момента подтверждения объявлен ОСТАЮЩИМСЯ НА КРАЮ: мостовой формы у него нет
// by construction.
//
// Проба утверждает не вкус, а следствие решения: читать несуществующую форму
// значило бы завести чтение, которое не сработает никогда, а выглядеть будет
// полным. Положительный контроль — голая форма в том же запросе читается.
func TestCredentialFromRequest_InstantHasNoBridgeFormAndIsNotReadFromOne(t *testing.T) {
	at := time.Now().Add(-2 * time.Hour).Truncate(time.Second).UTC()
	c := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType:                 "user",
		principalmeta.HeaderPrincipalID:                   "usr00000000000000003",
		"Grpc-Metadata-" + principalmeta.HeaderTokenMfaAt: strconv.FormatInt(at.Unix(), 10),
	}))
	if !c.AuthenticatedAt.IsZero() {
		t.Fatalf("момент прочитан из мостовой формы (%v), которой у этого ключа нет — "+
			"производителя за краем у неё тоже нет, значит прочитанное положил кто-то другой",
			c.AuthenticatedAt)
	}
	// Положительный контроль: голая форма читается.
	c = principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000003",
		principalmeta.HeaderTokenMfaAt:    strconv.FormatInt(at.Unix(), 10),
	}))
	if !c.AuthenticatedAt.Equal(at) {
		t.Fatalf("голая форма момента не прочитана (%v) — тогда отрицание выше ничего не означает",
			c.AuthenticatedAt)
	}
}

func TestCredentialFromRequest_UnusableInstantStaysAbsent(t *testing.T) {
	for _, raw := range []string{"", "  ", "не-число", "0", "-1"} {
		c := principalmeta.CredentialFromRequest(req(map[string]string{
			principalmeta.HeaderPrincipalType: "user",
			principalmeta.HeaderPrincipalID:   "usr00000000000000004",
			principalmeta.HeaderTokenMfaAt:    raw,
		}))
		if !c.AuthenticatedAt.IsZero() {
			t.Fatalf("значение %q принято за момент подтверждения (%v) — «провайдер момента "+
				"не назвал» обязано остаться отсутствием величины, а не подтверждением в 1970 году",
				raw, c.AuthenticatedAt)
		}
	}
}

// TestCredentialFromRequest_UnnamedCredentialIsNotAskable — поток, чьё
// удостоверение себя не назвало, отзывом закрыть нельзя. Величина обязана
// отвечать «нет», а не притворяться спрашиваемой.
func TestCredentialFromRequest_UnnamedCredentialIsNotAskable(t *testing.T) {
	c := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType: "service_account",
		principalmeta.HeaderPrincipalID:   "sva00000000000000002",
	}))
	if c.Askable() {
		t.Fatalf("удостоверение без идентификатора и без человека объявлено спрашиваемым: %+v — "+
			"перепрос задал бы вопрос ни о ком и записал бы ответ в исполненный контроль", c)
	}
}
