// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_cutoff_every_issuer_e2e_test.go — kacho#3171: отсечка уровня
// личности действует на токен ЛЮБОГО издателя, которого принимает край,
// одинаково с основным.
//
// # Что такое «отсечка уровня личности» на проводе
//
// Блокировка, принудительный выход и удаление личности пишут у службы доступа
// одну и ту же отсечку субъекта — момент, не позже которого выданное субъекту
// недействительно (kaname `user/set_blocked.go`, `user/delete.go`,
// `internal_iam/force_logout.go`: `UpsertCutoff`, обе записи отсечки одной
// дверью). Поэтому три события на крае — один вопрос, и проба называет их
// поимённо, задавая каждому одну и ту же запись дублёра.
//
// # Две полосы, один исход
//
// Токен нашей чеканки край спрашивает у авторитета нашей чеканки — тот
// исполняет отсечку сам (дублёр стенда — так же, как служба). Токен другого
// принятого издателя край спрашивает у нашей записи отзыва ПО ИДЕНТИФИКАТОРУ
// токена — и до kacho#3171 отсечки субъекта не спрашивал вовсе: заблокированный
// человек работал токеном второго издателя до конца его срока. Предмет пробы —
// эта вторая полоса; первая стоит рядом положительным контролем исхода.
//
// Рядом с каждым отказом — положительный близнец: тот же токен до отсечки
// принимается, и токен, выданный ПОСЛЕ отсечки, принимается (отсечка действует
// вперёд). Без них проба зеленела бы и на крае, отвергающем всё подряд.
package e2e_test

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

const cutoffExtraIssuer = "https://partner.kacho.example"

// cutoffEvents — события, пишущие отсечку субъекта у службы доступа.
var cutoffEvents = []string{"блокировка", "принудительный выход", "удаление"}

type issuedToken struct {
	issuer string
	mint   func(t *testing.T, iat time.Time) string
}

// cutoffJTISeq — свой идентификатор каждому токену: читатель отзыва нашей
// чеканки держит вердикт по идентификатору в окне, и общий идентификатор у
// токена «до» и «после» отсечки читался бы одним вердиктом.
var cutoffJTISeq atomic.Int64

func cutoffJTI() string { return fmt.Sprintf("jti-cutoff-%d", cutoffJTISeq.Add(1)) }

func cutoffTokens(st *ka1stand.Stand) []issuedToken {
	withIat := func(iat time.Time) func(jwt.MapClaims) {
		return func(c jwt.MapClaims) {
			c["sub"] = ka1stand.User
			c["kaname_principal_id"] = ka1stand.User
			c["iat"] = iat.Unix()
			c["nbf"] = iat.Unix()
		}
	}
	return []issuedToken{
		{ka1stand.PlatformIssuer, func(t *testing.T, iat time.Time) string {
			return st.Ours.Mint(t, middleware.PlatformTokenType, cutoffJTI(), withIat(iat))
		}},
		{cutoffExtraIssuer, func(t *testing.T, iat time.Time) string {
			return st.Extra.Mint(t, middleware.LegacyTokenType, cutoffJTI(), withIat(iat))
		}},
		{ka1stand.LegacyIssuer, func(t *testing.T, iat time.Time) string {
			return st.Legacy.Mint(t, middleware.LegacyTokenType, cutoffJTI(), withIat(iat))
		}},
	}
}

func TestIdentityCutoffRefusesTheTokenOfEveryAcceptedIssuer(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{ExtraIssuer: cutoffExtraIssuer})
	before := st.Ident.T0.Add(-time.Minute) // выдан до события
	after := time.Now()                     // выдан после события
	tokens := cutoffTokens(st)
	if len(tokens) < 2 {
		t.Fatalf("издателей %d — двух объявленных нет, проба не о предмете", len(tokens))
	}

	// Положительный близнец: отсечки нет — токен каждого издателя принят.
	st.Ident.CutoffAtT0.Store(false)
	for _, tok := range tokens {
		raw := tok.mint(t, before)
		if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(raw)); got.Status != http.StatusOK {
			t.Fatalf("%s до отсечки: REST %s, ожидалось 200 — условие пробы не создано", tok.issuer, got)
		}
		if got := st.GRPC(t, ka1stand.PingMethod, raw, nil); got.St.Code() != codes.OK {
			t.Fatalf("%s до отсечки: gRPC %s, ожидалось OK — условие пробы не создано", tok.issuer, got)
		}
	}

	refused, forward := 0, 0
	for _, event := range cutoffEvents {
		st.Ident.CutoffAtT0.Store(true) // событие записало отсечку субъекта
		for _, tok := range tokens {
			raw := tok.mint(t, before)
			ka1stand.RequireRefusal(t, event+" · "+tok.issuer+" · REST",
				st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(raw)), false)
			ka1stand.RequireNativeRefusal(t, event+" · "+tok.issuer+" · gRPC",
				st.GRPC(t, ka1stand.PingMethod, raw, nil))
			refused++

			// Отсечка действует вперёд: выданный после события токен принят.
			fresh := tok.mint(t, after)
			if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(fresh)); got.Status != http.StatusOK {
				t.Errorf("%s · %s: токен, выданный после отсечки, отвергнут REST: %s", event, tok.issuer, got)
			}
			if got := st.GRPC(t, ka1stand.PingMethod, fresh, nil); got.St.Code() != codes.OK {
				t.Errorf("%s · %s: токен, выданный после отсечки, отвергнут gRPC: %s", event, tok.issuer, got)
			}
			forward++
		}
	}
	t.Logf("перепись: издателей %d · событий %d · отказов утверждено %d · выданных после принято %d",
		len(tokens), len(cutoffEvents), refused, forward)
}

// Молчание службы на вопросе об отсечке для токена второго издателя — ответ Р1
// (503, носитель цел), а не проход: «не смог спросить» не есть «отсечки нет».
func TestIdentityCutoffUnansweredRefusesTheSecondIssuerWithR1(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{ExtraIssuer: cutoffExtraIssuer})
	raw := cutoffTokens(st)[1].mint(t, st.Ident.T0.Add(-time.Minute))
	st.Ident.CutoffQ.Set(ka1stand.Unavailable)
	ka1stand.RequireUnavailable(t, "служба молчит об отсечке · REST",
		st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(raw)))
	ka1stand.RequireNativeUnavailable(t, "служба молчит об отсечке · gRPC",
		st.GRPC(t, ka1stand.PingMethod, raw, nil))
}

// Личность токена второго издателя без утверждений принципала резолвится по
// `sub` — и отсечка спрашивается о ней же: иначе издатель, не кладущий
// утверждений, обходил бы отсечку их отсутствием. Близнец — служебная учётка:
// отсечка ключуется людьми, вопроса о ней нет, и токен принят.
func TestIdentityCutoffFollowsThePrincipalTheLaneResolves(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{ExtraIssuer: cutoffExtraIssuer})
	st.Ident.CutoffAtT0.Store(true)
	iat := st.Ident.T0.Add(-time.Minute).Unix()

	byLookup := st.Extra.Mint(t, middleware.LegacyTokenType, cutoffJTI(), func(c jwt.MapClaims) {
		delete(c, "kaname_principal_type")
		delete(c, "kaname_principal_id")
		c["sub"] = "ext-" + ka1stand.User
		c["iat"], c["nbf"] = iat, iat
	})
	ka1stand.RequireRefusal(t, "резолв по sub · REST",
		st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(byLookup)), false)
	ka1stand.RequireNativeRefusal(t, "резолв по sub · gRPC", st.GRPC(t, ka1stand.PingMethod, byLookup, nil))

	machine := st.Extra.Mint(t, middleware.LegacyTokenType, cutoffJTI(), func(c jwt.MapClaims) {
		c["kaname_principal_type"] = "service_account"
		c["kaname_principal_id"] = "sva-00000000000000ka1"
		c["sub"] = "sva-00000000000000ka1"
		c["iat"], c["nbf"] = iat, iat
	})
	if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, ka1stand.Bearer(machine)); got.Status != http.StatusOK {
		t.Errorf("служебная учётка отвергнута отсечкой людей: %s", got)
	}
}
