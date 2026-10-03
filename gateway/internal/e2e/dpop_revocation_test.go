// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package e2e_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/PRO-Robotech/kacho/gateway/internal/e2e/ka1stand"
)

// TestDPoPSchemePresentationReadsTheRevocationVerdict — поверхность
// предъявления по схеме `DPoP` (включён KACHO_API_GATEWAY_AUTHN_ENABLE_DPOP)
// читает вердикт отзыва тем же общим словарём, что и схема `Bearer`
// (kacho#2742): отозванный токен с годным доказательством владения отвергается
// единым отказом края. Близнец — тот же токен с живым `jti`: проходит.
func TestDPoPSchemePresentationReadsTheRevocationVerdict(t *testing.T) {
	st := ka1stand.New(t, ka1stand.Options{DPoP: true})
	priv, jkt := dpopKey(t)
	present := func(jti string) ka1stand.Presented {
		tok := st.OurToken(t, jti, func(c jwt.MapClaims) { c["cnf"] = map[string]any{"jkt": jkt} })
		return func(r *http.Request) {
			r.Header.Set("Authorization", "DPoP "+tok)
			htu := "http://" + r.URL.Host + r.URL.Path
			r.Header.Set("DPoP", signDPoPHeader(t, priv, r.Method, htu, "proof-"+jti, time.Now(), ""))
		}
	}
	ka1stand.RequireRefusal(t, "DPoP: отозванный токен с годным доказательством",
		st.REST(t, http.MethodGet, ka1stand.ListRoute, present(ka1stand.JTIRevoked)), false)
	if got := st.REST(t, http.MethodGet, ka1stand.ListRoute, present(ka1stand.JTILive)); got.Status != http.StatusOK {
		t.Errorf("DPoP близнец: живой токен с годным доказательством обязан проходить\n  получено: %s", got)
	}
	// Молчащий авторитет — ответ Р1, как на схеме Bearer.
	st.OurAuth.Q.Set(ka1stand.Silent)
	ka1stand.RequireUnavailable(t, "DPoP: авторитет молчит",
		st.REST(t, http.MethodGet, ka1stand.ListRoute, present("jti-dpop-silent")))
}
