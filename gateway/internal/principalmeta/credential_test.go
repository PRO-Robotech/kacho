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

// TestCredentialFromRequest_BrowserLaneCarriesSubjectNotInstant — браузерная
// полоса называет человека; момента аутентификации удостоверение НЕ несёт.
//
// Довод свежести (`mfa-at`) в запросе стоит, и проба ставит его нарочно: его
// единица — секунды условия `mfa_fresh`, и прочитанный как момент сессии он
// закрывал бы годную сессию на отсечке, датированной в микросекундах
// (kacho#2690). Момент перепрос берёт из ответа службы о сессии по носителю.
func TestCredentialFromRequest_BrowserLaneCarriesSubjectNotInstant(t *testing.T) {
	at := time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC)
	withInstant := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
		// `jti` не ставится: у браузерной сессии его нет вовсе.
		principalmeta.HeaderTokenMfaAt: strconv.FormatInt(at.Unix(), 10),
	}))
	if withInstant.JTI != "" {
		t.Fatalf("у браузерной сессии появился идентификатор удостоверения %q", withInstant.JTI)
	}
	if withInstant.UserID != "usr00000000000000001" {
		t.Fatalf("человек не назван (%q) — спросить про отсечку было бы не о ком", withInstant.UserID)
	}
	if !withInstant.Askable() {
		t.Fatal("браузерная сессия объявлена неспрашиваемой")
	}
	// Тот же запрос без довода свежести даёт ТО ЖЕ удостоверение: довод в
	// вопрос об отзыве не входит, и потоки одной сессии не делятся по нему.
	without := principalmeta.CredentialFromRequest(req(map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
	}))
	if withInstant != without {
		t.Fatalf("довод свежести изменил удостоверение вопроса об отзыве: %+v против %+v", withInstant, without)
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
