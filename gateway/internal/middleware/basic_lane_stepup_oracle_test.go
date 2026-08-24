// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// basic_lane_stepup_oracle_test.go — ТРЕТЬЯ ПОЛОСА НЕ СТАЛА ОРАКУЛОМ (#1215).
//
// Полоса базового удостоверения объявляет свой контракт сама
// (`ErrCredentialRefused`): неизвестный идентификатор, неверный секрет,
// истёкший срок, отозванное, неактивный владелец — ОДИН исход, потому что
// «различимый был бы оракулом».
//
// Применение пола едва не завело ШЕСТОЙ исход, и он говорит ровно то, чего
// избегают остальные пять: «предъявленное годно, его уровень прочитан, не
// хватает силы». Это подтверждение годности строки — по глаголу, который
// вызывающему НЕ доступен, бесплатно и без следа в аудите. Для угадываемого
// долгоживущего предъявительского секрета это ровно тот оракул, ради закрытия
// которого пять исходов и сведены в один.
//
// Здесь утверждается БАЙТ-ИДЕНТИЧНОСТЬ двух отказов на ОБЕИХ поверхностях
// края. Утверждать только код бессмысленно: коды совпадали и до починки —
// расходились заголовок вызова и тело.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// basicLaneRefusal — полный наблюдаемый ответ края на одно REST-обращение.
type basicLaneRefusal struct {
	status    int
	challenge string
	body      string
	reached   bool
}

func driveBasicREST(t *testing.T, rig *laneRig, route, presented string) basicLaneRefusal {
	t.Helper()
	out := basicLaneRefusal{}
	h := rig.auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		out.reached = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, route, nil)
	req.Header.Set("Authorization", "Bearer "+presented)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out.status, out.challenge, out.body = rec.Code, rec.Header().Get("WWW-Authenticate"), rec.Body.String()
	return out
}

// spoiledSecret — строка НАШЕЙ марки, которая удостоверением не является.
// Марка сохраняется намеренно: полоса выбирается по ней, и подделка без марки
// ушла бы на другую полосу, то есть сравнивались бы разные механизмы.
func spoiledSecret(valid string) string {
	return valid[:len(valid)-4] + "zzzz"
}

// РЕСТ-ПОВЕРХНОСТЬ. Годное удостоверение уровня «1» на глаголе с полом «2»
// обязано быть неотличимо от негодного секрета — статусом, заголовком и телом.
func TestBasicLaneStepUp_FloorRefusalIsIndistinguishableFromCredentialRefusal_REST(t *testing.T) {
	rig := newLaneRig(t)

	byFloor := driveBasicREST(t, rig, elevatedRoute, rig.basicSecret)
	bySecret := driveBasicREST(t, rig, elevatedRoute, spoiledSecret(rig.basicSecret))

	t.Logf("перепись: сравнено отказов 2 · осей сравнения 3 (статус · заголовок вызова · тело)")
	t.Logf("  отказ по полу:       статус=%d бэкенд=%v challenge=%q тело=%q",
		byFloor.status, byFloor.reached, byFloor.challenge, byFloor.body)
	t.Logf("  отказ в удостоверении: статус=%d бэкенд=%v challenge=%q тело=%q",
		bySecret.status, bySecret.reached, bySecret.challenge, bySecret.body)

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Оба ответа обязаны быть НАСТОЯЩИМИ отказами.
	// Без него «неотличимы» удовлетворялось бы двумя успехами — то есть самым
	// опасным исходом: полосой, которая не отвергает ничего.
	require.Equal(t, http.StatusUnauthorized, byFloor.status,
		"годное удостоверение уровня «1» на глаголе с полом «2» обязано быть отвергнуто")
	require.False(t, byFloor.reached, "запрос не обязан доезжать до backend")
	require.Equal(t, http.StatusUnauthorized, bySecret.status,
		"негодный секрет обязан быть отвергнут")
	require.False(t, bySecret.reached, "запрос не обязан доезжать до backend")

	assert.Equal(t, bySecret.status, byFloor.status, "различимый статус — оракул годности")
	assert.Equal(t, bySecret.challenge, byFloor.challenge,
		"различимый заголовок вызова — оракул годности: он сообщает, что предъявленное "+
			"годно и лишь недостаточно сильно")
	assert.Equal(t, bySecret.body, byFloor.body, "различимое тело — оракул годности")
}

// КОНТРОЛЬ САМОГО СРАВНЕНИЯ. Сравнение обязано УМЕТЬ увидеть различие — иначе
// «неотличимы» ничего не значит: сравнение, слепое ко всему, зеленеет всегда.
//
// Опорой берётся полоса, которой вызов RFC 9470 законно ПОЛОЖЕН: у её носителя
// церемония повышения существует, и назвать её — работа вызова.
func TestBasicLaneStepUp_TheComparisonCanSeeADifference(t *testing.T) {
	rig := newLaneRig(t)

	bearerByFloor := driveLane(t, rig, http.MethodPost, "tryHydraJWT", elevatedRoute,
		laneDrivers()["tryHydraJWT"])
	basicBySecret := driveBasicREST(t, rig, elevatedRoute, spoiledSecret(rig.basicSecret))

	require.Equal(t, http.StatusUnauthorized, bearerByFloor.status)
	t.Logf("контроль: у полосы с достижимой церемонией вызов остаётся: challenge=%q",
		bearerByFloor.challenge)
	assert.NotEqual(t, basicBySecret.challenge, bearerByFloor.challenge,
		"сравнение слепо: оно не отличает вызов ступенчатой аутентификации от единого "+
			"отказа полосы, значит утверждение о неотличимости выше ничего не измеряет")
	assert.Contains(t, bearerByFloor.challenge, "insufficient_user_authentication",
		"закрытие оракула на одной полосе не вправе снимать вызов там, где он адресуем")
}

// НАТИВНАЯ ПОВЕРХНОСТЬ. Обе поверхности края обязаны отвечать одинаково:
// расхождение между ними никто бы не решал, оно возникло бы побочным эффектом.
func TestBasicLaneStepUp_FloorRefusalIsIndistinguishableFromCredentialRefusal_GRPC(t *testing.T) {
	rig := newLaneRig(t)

	drive := func(presented string) (codes.Code, string, bool) {
		called := false
		ctx := metadata.NewIncomingContext(context.Background(),
			metadata.Pairs("authorization", "Bearer "+presented))
		_, err := rig.auth.Unary()(ctx, nil,
			&grpc.UnaryServerInfo{FullMethod: "/" + elevatedFQN},
			func(context.Context, any) (any, error) { called = true; return nil, nil })
		st, _ := status.FromError(err)
		return st.Code(), st.Message(), called
	}

	fCode, fMsg, fCalled := drive(rig.basicSecret)
	sCode, sMsg, sCalled := drive(spoiledSecret(rig.basicSecret))

	t.Logf("перепись: сравнено отказов 2 · осей сравнения 2 (код · сообщение)")
	t.Logf("  отказ по полу:         код=%s сообщение=%q обработчик-вызван=%v", fCode, fMsg, fCalled)
	t.Logf("  отказ в удостоверении: код=%s сообщение=%q обработчик-вызван=%v", sCode, sMsg, sCalled)

	require.Equal(t, codes.Unauthenticated, fCode,
		"годное удостоверение уровня «1» на глаголе с полом «2» обязано быть отвергнуто "+
			"и на нативной поверхности — иначе полоса обходит пол по второму входу")
	require.False(t, fCalled, "обработчик не обязан вызываться")
	require.Equal(t, codes.Unauthenticated, sCode)
	require.False(t, sCalled)

	assert.Equal(t, sCode, fCode, "различимый код — оракул годности")
	assert.Equal(t, sMsg, fMsg, "различимое сообщение — оракул годности")
}
