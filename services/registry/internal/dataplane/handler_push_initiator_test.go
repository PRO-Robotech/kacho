// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/auth"
	"github.com/PRO-Robotech/corelib/ids"
)

// handler_push_initiator_test.go — решение Д115: инициатор записи намерения
// репозитория на первом push — `sub` ПРОВЕРЕННОГО токена реестра. Нет
// проверенного `sub` — отказ записи (fail-closed), без подстановки «system».
//
// Наблюдаемое — код ответа, дошёл ли запрос до движка и что получил писатель
// намерений: фальшивый писатель выполняет контракт настоящего (`journaltx.Begin`
// берёт инициатора из принципала контекста функцией `auth.InitiatorOf`).
// Законный близнец — первый кейс; каждый отрицательный отличается от него одним
// фактом: подписью токена, наличием проверяющего, формой `sub`.

func pushNewRepoStand(verifier TokenVerifier) (*Handler, *fakeForwarder, *fakeRepoReg) {
	az := &fakeAuthz{allow: map[string]bool{"v_create registry_registry:reg-A": true}}
	fw := &fakeForwarder{status: 201}
	rr := &fakeRepoReg{}
	h := newTestHandler(verifier, az, &fakeBackend{exists: map[string]bool{}}, fw, rr)
	return h, fw, rr
}

// TestDataplane_D115_FirstPush_InitiatorIsTheVerifiedSub — push с проверенным
// токеном: инициатор транзакции намерения — его `sub` (оба типа субъекта).
func TestDataplane_D115_FirstPush_InitiatorIsTheVerifiedSub(t *testing.T) {
	for _, c := range []struct{ name, prefix, kind string }{
		{"service account", ids.PrefixServiceAccount, "service_account"},
		{"user", ids.PrefixUser, "user"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sub := ids.NewHyphenID(c.prefix)
			h, fw, rr := pushNewRepoStand(&fakeVerifier{subject: sub})
			rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
			require.Equal(t, http.StatusCreated, rec.Code, "законный близнец: запись принята")
			require.Equal(t, 1, fw.count())
			require.Equal(t, []auth.Initiator{auth.Initiator(c.kind + ":" + sub)}, rr.registeredInitiators(),
				"Д115: инициатор намерения — sub проверенного токена")
		})
	}
}

// TestDataplane_D115_ForgedToken_FirstPushRefused — подделанный или неподписанный
// токен не проходит проверку: 401, до движка и до писателя запрос не доходит.
func TestDataplane_D115_ForgedToken_FirstPushRefused(t *testing.T) {
	h, fw, rr := pushNewRepoStand(&fakeVerifier{err: errors.New("signature invalid")})
	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, fw.count(), "Д115: подделанный токен до движка не доходит")
	require.Empty(t, rr.registered(), "Д115: намерения нет")
	require.Zero(t, rr.refusedCalls(), "Д115: писатель не вызывался")
}

// TestDataplane_D115_NoVerifier_FirstPushRefused — режим без проверяющего
// (`sub` не проверен никем): запись нового репозитория — отказ ДО движка, а не
// намерение от имени «system».
func TestDataplane_D115_NoVerifier_FirstPushRefused(t *testing.T) {
	h, fw, rr := pushNewRepoStand(nil)
	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusForbidden, rec.Code, "Д115: нет проверенного sub — отказ записи")
	require.Equal(t, "DENIED", pushDenyCode(t, rec))
	require.Equal(t, 0, fw.count(), "Д115: манифест до движка не доходит — регистрировать его нечем")
	require.Empty(t, rr.registered(), "Д115: намерения нет")
	require.Zero(t, rr.refusedCalls(), "Д115: писатель не вызывался")
}

// TestDataplane_D115_SubWithoutInitiatorForm_FirstPushRefused — проверенный
// токен, чей `sub` не переводится в инициатора (`auth.InitiatorOf` отказывает):
// отказ записи до движка.
func TestDataplane_D115_SubWithoutInitiatorForm_FirstPushRefused(t *testing.T) {
	h, fw, rr := pushNewRepoStand(&fakeVerifier{subject: "sva-ci"})
	rec := doReq(h, http.MethodPut, "/v2/reg-A/app/manifests/v1", true)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "DENIED", pushDenyCode(t, rec))
	require.Equal(t, 0, fw.count())
	require.Empty(t, rr.registered())
	require.Zero(t, rr.refusedCalls())
}
