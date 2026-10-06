// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// refusal_names_next_step_test.go — ОТКАЗ ПО НЕДОСТАТКУ УРОВНЯ НАЗЫВАЕТ СЛЕДУЮЩИЙ
// ШАГ, И ТЕКСТ У НЕГО ОДИН НА ВСЕХ ПОЛОСАХ И ОБЕИХ ПОВЕРХНОСТЯХ КРАЯ (сторона
// края kaname#511, решение R36 п. 1; тексты отказов меняются тикетом — эта
// задача и есть тикет, `api-conventions.md` §«Error-format»).
//
// Пара (статус, code) здесь та, что закреплена приёмкой KA1 (Р3, KA1-15) для
// указания повысить уровень: `401` / `16` с вызовом RFC 9470. Проба утверждает
// её дословно, вместе с телом: тело прежде было машинным признаком
// (`insufficient_user_authentication`), который шага не называл, — клиент,
// получивший его, шёл «войти заново» и получал тот же ответ.
//
// Отказ модели прав по свежести второго фактора (`403` / `7`, тоже с вызовом)
// называет тот же шаг тем же текстом: недостаток уровня — один предмет, и два
// текста о нём были бы двумя решениями.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stepUpNextStepText — дословно. Выписан здесь, а не взят у продукта.
const stepUpNextStepText = "authentication level is insufficient: step up with a second factor, or present a credential of another kind"

const stepUpFloorBody = `{"code":16,"message":"` + stepUpNextStepText + `","details":[]}`

func TestStepUpRefusal_511_NamesTheNextStep_REST_BearerLane(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	rec, _, hit := serveREST(t, alwaysOnAuth(t, fix), http.MethodPost,
		"https://api.kacho.cloud/iam/v1/users/usr-abc/tokens", fix.sign(t, alwaysOnClaims("1")))
	require.False(t, hit)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="insufficient_user_authentication"`)
	require.Equal(t, stepUpFloorBody, rec.Body.String())

	// Близнец: тот же токен на глаголе без поднятого пола проходит.
	rec, _, hit = serveREST(t, alwaysOnAuth(t, fix), http.MethodPost,
		"https://api.kacho.cloud/vpc/v1/networks", fix.sign(t, alwaysOnClaims("1")))
	require.True(t, hit, "близнец: пол «1» уровнем «1» проходится — отказ выше дан полом, а не чем-то ещё")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestStepUpRefusal_511_NamesTheNextStep_REST_SessionLane(t *testing.T) {
	rec, _, hit := serveSession(t, alwaysOnSessionAuth(t, "1"), http.MethodPost, sessionElevatedRoute)
	require.False(t, hit)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="insufficient_user_authentication"`)
	require.Equal(t, stepUpFloorBody, rec.Body.String())
}

func TestStepUpRefusal_511_NamesTheNextStep_GRPC(t *testing.T) {
	fix := newJWKSFixture(t, "RS256")
	err := callUnary(t, alwaysOnAuth(t, fix), "/kaname.cloud.iam.v1.UserTokenService/Issue", fix.sign(t, alwaysOnClaims("1")))
	st, ok := status.FromError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, codes.Unauthenticated, st.Code())
	require.Equal(t, stepUpNextStepText, st.Message())
}
