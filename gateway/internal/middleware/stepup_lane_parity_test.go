// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_lane_parity_test.go — сравнение ПОЛОС аутентификации по одному
// свойству: спрашивает ли полоса пол ступенчатой аутентификации
// (`required_acr_min`), объявленный каталогом прав для вызываемого глагола.
//
// ПОЧЕМУ СРАВНЕНИЕ, А НЕ ПРОБА КАЖДОЙ ПОЛОСЫ ОТДЕЛЬНО. Проба одной полосы
// требует знать, каким свойство ДОЛЖНО быть, — а это и есть спорный вопрос
// («освобождена ли браузерная полоса by design?»). Сравнение спрашивает другое:
// «решал ли кто-нибудь, что полосы различаются», и на это ответ есть всегда.
// Соседний stepup_alwayson_test.go утверждает, что пол применяет слой, через
// который проходит КАЖДЫЙ запрос, — и проверяет это, подавая только подписанного
// предъявителя. «Каждый запрос» и «каждый запрос ЭТОЙ полосы» — разные
// утверждения, и разница между ними ровно та, что осталась незамеченной.
//
// Обе величины переписи печатаются («полос N · спрашивают пол M»): одно число
// скрывает ровно тот случай, ради которого перепись заведена.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// laneOutcome — что край сделал с одним запросом на одной полосе.
type laneOutcome struct {
	lane           string
	status         int
	backendReached bool
	forwardedACR   string // X-Kacho-Token-Acr — вход ВТОРОГО замка (iam ACRFloor)
	challenge      string
}

// askedTheFloor — спросила ли полоса пол: запрос до backend не доехал и край
// вернул вызов ступенчатой аутентификации (RFC 9470).
func (o laneOutcome) askedTheFloor() bool {
	return !o.backendReached &&
		o.status == http.StatusUnauthorized &&
		strings.Contains(o.challenge, "insufficient_user_authentication")
}

// fakeKratos — провайдер сессий, отвечающий ЖИВОЙ сессией уровня aal1: человек
// вошёл, второго фактора НЕ предъявлял. Это тот же уровень уверенности, что
// acr="1" у подписанного предъявителя, — полосы сравниваются на РАВНОМ входе.
func fakeKratos(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
		  "active": true,
		  "authenticated_at": "2026-08-24T10:00:00Z",
		  "authenticator_assurance_level": "aal1",
		  "identity": {
		    "id": "dc609064-d9f3-4e24-b574-d561c9f18359",
		    "traits": {"email": "alice@example.test", "name": {"first": "Alice", "last": "A"}}
		  }
		}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bothLanesAuth — композиционный корень, несущий ОБЕ полосы носителя личности
// (сессия провайдера и подписанный предъявитель) и смонтированный пол, как это
// делает край в production-посадке.
func bothLanesAuth(t *testing.T, fix *jwksFixture, kratosURL string) *middleware.AuthInterceptor {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	return middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "",
		&countingLookup{subj: middleware.Subject{
			Type: "user", ID: "usr_alice_acc_a1b2", DisplayName: "Alice A",
		}},
		authTestLogger(),
	).
		WithVerifier(rs256Verifier(t, fix)).
		WithKratos(middleware.NewKratosClient(kratosURL)).
		WithStepUp(
			middleware.NewStepUpGate(nil),
			middleware.NewCatalogPermissionLookup(catalog),
			middleware.NewRestRouter(),
		)
}

// driveLane прогоняет ОДИН запрос через настоящую точку входа края.
func driveLane(t *testing.T, auth *middleware.AuthInterceptor,
	lane, method, url string, arrange func(*http.Request)) laneOutcome {
	t.Helper()
	out := laneOutcome{lane: lane}
	handler := auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out.backendReached = true
		out.forwardedACR = r.Header.Get("X-Kacho-Token-Acr")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, url, nil)
	arrange(req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	out.status = rec.Code
	out.challenge = rec.Header().Get("WWW-Authenticate")
	return out
}

// catalogFloor — пол ВЫВОДИТСЯ из каталога, а не выписывается константой:
// величина принадлежит каталогу, и проба обязана спрашивать его, а не помнить.
func catalogFloor(t *testing.T, fqn string) string {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	return middleware.NewCatalogPermissionLookup(catalog).Lookup(fqn).RequiredACRMin
}

// Целевой глагол: чеканка удостоверения пользователя. Каталог объявляет ему пол
// уровня «2»; REST-адрес разрешает та же таблица маршрутов, что и в проде.
const (
	elevatedFQN   = "kacho.cloud.iam.v1.UserTokenService/Issue"
	elevatedRoute = "https://api.kacho.cloud/iam/v1/users/usr-abc/tokens"
	routineRoute  = "https://api.kacho.cloud/vpc/v1/networks"
)

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Обе полосы действительно аутентифицируют: на обычном
// глаголе (пол не поднят) обе доносят запрос до backend. Без этого сравнение
// ниже зеленело бы на полосе, которая просто не работает.
func TestStepUpLaneParity_BothLanesAuthenticate_OnlyOneCarriesAssurance(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := bothLanesAuth(t, fix, fakeKratos(t).URL)

	bearer := driveLane(t, auth, "подписанный предъявитель (Hydra JWT, acr=1)",
		http.MethodPost, routineRoute, func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+fix.sign(t, alwaysOnClaims("1")))
		})
	session := driveLane(t, auth, "сессия провайдера (ory_kratos_session, aal1)",
		http.MethodPost, routineRoute, func(r *http.Request) {
			r.Header.Set("Cookie", "ory_kratos_session=opaque-a")
		})

	require.True(t, bearer.backendReached, "полоса предъявителя обязана аутентифицировать")
	require.True(t, session.backendReached, "полоса сессии обязана аутентифицировать")

	// ВТОРАЯ ОСЬ того же расхождения, и спрашивается она ЗДЕСЬ, а не на глаголе
	// с поднятым полом: там отвергнутая полоса до backend не доходит, поэтому её
	// «acr не донесён» тождественно истинно, и сравнение было бы вакуумным.
	// На обычном глаголе backend достигают ОБЕ полосы — значит разница в том,
	// что они донесли, наблюдаема.
	//
	// x-kacho-token-acr — вход ВТОРОГО замка (iam authzguard.ACRFloor) на
	// внутреннем слушателе. Полоса, не выставляющая его, оставляет внутренний
	// замок без входа.
	t.Logf("перепись: полос аутентификации 2 · доносят уровень уверенности до внутреннего замка %d",
		map[bool]int{true: 1, false: 0}[bearer.forwardedACR != ""]+
			map[bool]int{true: 1, false: 0}[session.forwardedACR != ""])
	t.Logf("  %-44s acr-вперёд=%q", bearer.lane, bearer.forwardedACR)
	t.Logf("  %-44s acr-вперёд=%q", session.lane, session.forwardedACR)

	require.NotEmpty(t, bearer.forwardedACR,
		"положительный контроль оси: полоса предъявителя доносит уровень уверенности")
	assert.Equal(t, bearer.forwardedACR != "", session.forwardedACR != "",
		"полосы расходятся по тому, доносят ли они уровень уверенности до внутреннего "+
			"замка: %q → %q, %q → %q",
		bearer.lane, bearer.forwardedACR, session.lane, session.forwardedACR)
}

// СРАВНЕНИЕ ПОЛОС. Один и тот же глагол с полом уровня «2», один и тот же
// уровень уверенности предъявленного (второго фактора НЕ было) — полосы обязаны
// дать один и тот же вердикт. Расхождение здесь означает, что механизм
// разошёлся сам, без чьего-либо решения.
func TestStepUpLaneParity_EveryLaneAsksTheDeclaredFloor(t *testing.T) {
	floor := catalogFloor(t, elevatedFQN)
	require.Equal(t, "2", floor,
		"предпосылка пробы: каталог объявляет этому глаголу пол уровня 2 (иначе сравнивать нечего)")

	fix := newJWKSFixture(t, "RS256")
	auth := bothLanesAuth(t, fix, fakeKratos(t).URL)

	lanes := []laneOutcome{
		driveLane(t, auth, "подписанный предъявитель (Hydra JWT, acr=1)",
			http.MethodPost, elevatedRoute, func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+fix.sign(t, alwaysOnClaims("1")))
			}),
		driveLane(t, auth, "сессия провайдера (ory_kratos_session, aal1)",
			http.MethodPost, elevatedRoute, func(r *http.Request) {
				r.Header.Set("Cookie", "ory_kratos_session=opaque-b")
			}),
	}

	asked := 0
	for _, l := range lanes {
		if l.askedTheFloor() {
			asked++
		}
	}
	t.Logf("перепись: полос аутентификации %d · спрашивают пол %d (глагол %s, пол %q)",
		len(lanes), asked, elevatedFQN, floor)
	for _, l := range lanes {
		t.Logf("  %-44s статус=%d бэкенд-достигнут=%-5v acr-вперёд=%q пол-спрошен=%v",
			l.lane, l.status, l.backendReached, l.forwardedACR, l.askedTheFloor())
	}

	// Положительный контроль механизма: пол действует хотя бы на одной полосе.
	// Без него попарное равенство зеленело бы на выключенном поле — то есть на
	// самом опасном исходе.
	require.Positive(t, asked,
		"пол не спросила НИ ОДНА полоса — сравнение ниже было бы вакуумным")

	// Собственно сравнение: попарно, к первой полосе как к опорной.
	for i := 1; i < len(lanes); i++ {
		assert.Equal(t, lanes[0].askedTheFloor(), lanes[i].askedTheFloor(),
			"полосы одного механизма разошлись, и этого никто не объявлял: "+
				"%q пол-спрошен=%v, а %q пол-спрошен=%v — при одном глаголе, одном поле "+
				"каталога и одном уровне предъявленной уверенности",
			lanes[0].lane, lanes[0].askedTheFloor(), lanes[i].lane, lanes[i].askedTheFloor())
	}

}
