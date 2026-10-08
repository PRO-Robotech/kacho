// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// geo_public_admin_stepup_test.go — порог подтверждения личности публичного
// административного глагола каталога размещения исполняется краем (приёмка
// ADM-1 geo, сценарий 17).
//
// На стенде этот сценарий не конструируется: служебные токены проб уровня не
// несут и от порога освобождены. Поэтому он здесь — через тот же
// всегда-смонтированный слой аутентификации, тот же вшитый каталог и ту же
// таблицу маршрутов, что собирает композиционный корень.
//
// Утверждается не «отказ есть», а «отказ ТОТ ЖЕ»: публичный глагол и его
// внутренний близнец при одном и том же токене дают одинаковый код и одинаковый
// текст, на REST и на нативном gRPC. Близнец по уровню — токен ровно на пороге —
// проходит: отказ порождён уровнем, а не путём.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

func TestStepUp_ADM1GEO17_PublicGeoAdminFloorIsEnforcedLikeItsInternalTwin(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	auth := alwaysOnAuth(t, fix)

	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	pubEntry, ok := catalog.Lookup("kacho.cloud.geo.v1.RegionService/Create")
	require.True(t, ok)
	intEntry, ok := catalog.Lookup("kacho.cloud.geo.v1.InternalRegionService/Create")
	require.True(t, ok)
	require.Equal(t, intEntry.RequiredACRMin, pubEntry.RequiredACRMin, "the floor belongs to the action")
	require.Equal(t, "1", pubEntry.RequiredACRMin, "the scenario is constructed against the floor the catalog declares")

	below := fix.sign(t, alwaysOnClaims("0"))
	atFloor := fix.sign(t, alwaysOnClaims(pubEntry.RequiredACRMin))

	type restOutcome struct {
		code      int
		challenge string
		bodyCode  int
		message   string
		hit       bool
	}
	rest := func(url, token string) restOutcome {
		rec, _, hit := serveREST(t, auth, http.MethodPost, url, token)
		var body struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if rec.Code != http.StatusOK {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "refusal body must be google.rpc.Status JSON: %s", rec.Body.String())
		}
		return restOutcome{rec.Code, rec.Header().Get("WWW-Authenticate"), body.Code, body.Message, hit}
	}

	pub := rest("https://api.kacho.cloud/geo/v1/regions", below)
	assert.Equal(t, http.StatusUnauthorized, pub.code)
	assert.Equal(t, int(codes.Unauthenticated), pub.bodyCode)
	assert.Contains(t, pub.challenge, "insufficient_user_authentication", "the challenge names the step-up error")
	assert.Contains(t, pub.challenge, `acr_values="1"`)
	assert.NotEmpty(t, pub.message)
	assert.False(t, pub.hit, "the backend must not be reached below the floor")

	intr := rest("https://api.kacho.cloud/geo/v1/internal/regions", below)
	assert.Equal(t, pub.code, intr.code, "public and internal twin: same HTTP status")
	assert.Equal(t, pub.bodyCode, intr.bodyCode, "same code")
	assert.Equal(t, pub.message, intr.message, "same text")
	assert.Equal(t, pub.challenge, intr.challenge, "same challenge")
	assert.False(t, intr.hit)

	// Нативный gRPC: тот же код и тот же текст, что в теле REST.
	for _, m := range []string{
		"/kacho.cloud.geo.v1.RegionService/Create",
		"/kacho.cloud.geo.v1.InternalRegionService/Create",
	} {
		err := callUnary(t, auth, m, below)
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unauthenticated, st.Code(), m)
		assert.Equal(t, pub.message, st.Message(), "%s: native gRPC text must equal the REST text", m)
		require.NoError(t, callUnary(t, auth, m, atFloor), "%s: a token at the floor must pass", m)
	}

	// Близнец по уровню.
	ok1 := rest("https://api.kacho.cloud/geo/v1/regions", atFloor)
	assert.Equal(t, http.StatusOK, ok1.code)
	assert.True(t, ok1.hit, "a token at the floor must reach the backend")
}
