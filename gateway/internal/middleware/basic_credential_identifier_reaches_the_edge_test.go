// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// basic_credential_identifier_reaches_the_edge_test.go — ИДЕНТИФИКАТОР строки
// базового удостоверения доезжает до края, который спрашивает про его отзыв
// (kacho#1450).
//
// # Предмет
//
// Перепрос отзыва на ОТКРЫТЫХ соединениях спрашивает нашего авторитета по
// идентификатору строки удостоверения. Собрать идентификатор ему неоткуда: он
// видит `*http.Request`, а полоса приёма ставила только личность и уровень.
// Поток базового секрета оставался НЕСПРАШИВАЕМЫМ, и его окно отзыва равнялось
// сроку жизни соединения.
//
// # Почему проба про ЗАГОЛОВОК, а не про поле структуры
//
// Заголовок — единственный провод между полосой приёма и перепросом: они живут
// в разных пакетах и общаются только запросом. Утверждение про внутреннее поле
// зеленело бы на полосе, которая его не выставляет.
//
// # Пара, а не одно «непусто»
//
// Годное удостоверение ставит СВОЙ идентификатор — «непусто» зеленело бы на
// любом мусоре. Негодная строка нашей марки не ставит НИЧЕГО — иначе
// «идентификатор доехал» было бы верно и о полосе, ставящей его всякому входу,
// то есть перепрос спрашивал бы про удостоверения, которых не принимал.

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/pkg/credsecret"
)

func TestBCL1450_TheCredentialIdentifierReachesTheRevocationReader(t *testing.T) {
	auth := &fakeAuthority{}
	lane := newLane(t, auth, time.Now)
	const credID = "uoc_00000000000001450"
	good := mintFor(t, auth, credID)

	interceptor := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithBasicCredentialLane(lane)

	var seen principalmeta.Credential
	h := interceptor.HTTP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Читается ТЕМ ЖЕ сборщиком, каким читает перепрос: второй читатель,
		// написанный здесь, разошёлся бы с продовым молча.
		seen = principalmeta.CredentialFromRequest(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/iam/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+good)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("годное удостоверение отвергнуто: %d %s", rec.Code, rec.Body.String())
	}
	if seen.BasicCredentialID != credID {
		t.Errorf("идентификатор строки удостоверения = %q, ждали %q — перепросу нечем "+
			"назвать удостоверение открытого потока, и окно его отзыва остаётся равным "+
			"сроку жизни соединения", seen.BasicCredentialID, credID)
	}
	if !seen.Askable() {
		t.Error("поток такого предъявителя считается НЕСПРАШИВАЕМЫМ: закрыть его отзывом " +
			"удостоверения нельзя ни при каких условиях")
	}

	// ОТРИЦАТЕЛЬНЫЙ КОНТРОЛЬ той же поверхности.
	seen = principalmeta.Credential{}
	bad := httptest.NewRequest(http.MethodGet, "/iam/v1/me", nil)
	bad.Header.Set("Authorization", "Bearer "+credsecret.Mark+credID+"_zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusUnauthorized {
		t.Errorf("негодная строка нашей марки дала %d, ожидался 401 — полоса не терминальна", badRec.Code)
	}
	if seen.BasicCredentialID != "" {
		t.Errorf("идентификатор проставлен НЕГОДНОМУ удостоверению: %q", seen.BasicCredentialID)
	}

	// КЛИЕНТ ЕГО НЕ ПОДДЕЛЫВАЕТ: заголовок живёт в пространстве края и
	// снимается со входа до выбора полосы. Без этого перепрос спрашивал бы про
	// чужое удостоверение по строке, которую прислал предъявитель.
	seen = principalmeta.Credential{}
	forged := httptest.NewRequest(http.MethodGet, "/iam/v1/me", nil)
	forged.Header.Set("Authorization", "Bearer "+good)
	forged.Header.Set(principalmeta.HeaderTokenBasicCredentialID, "uoc_someone_elses_row")
	forgedRec := httptest.NewRecorder()
	h.ServeHTTP(forgedRec, forged)
	if seen.BasicCredentialID != credID {
		t.Errorf("присланный клиентом идентификатор пережил полосу: %q", seen.BasicCredentialID)
	}
}
