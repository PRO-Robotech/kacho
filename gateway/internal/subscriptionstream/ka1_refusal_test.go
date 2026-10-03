// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"bytes"
	"net/http"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// Отказ Р2 приёмки KA1 — дословно (`sub-phase-KA1-edge-refusals-and-call-budgets-acceptance.md`).
// Литерал, а не значение производителя: утверждается текст приёмки.
const (
	ka1RefusalBody      = `{"code":16,"message":"authentication failed","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"AUTHN_REQUIRED","domain":"kaname.cloud.iam.v1"}]}`
	ka1RefusalChallenge = `Bearer realm="kacho", error="invalid_token"`
)

// TestKA1_13b_AnonymousStreamOpenIsTheOneRefusal — KA1-13 (б): открытие потока
// обработчиком без личности вызывающего отвечает ТЕМ ЖЕ отказом `401`, что слой
// аутентификации края (KA1-10). Уровень — обработчик: через боевую цепочку такой
// запрос до него не доходит.
//
// Близнец (строка потока KA1-14) — тот же запрос с личностью: поток открыт.
func TestKA1_13b_AnonymousStreamOpenIsTheOneRefusal(t *testing.T) {
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	h := newHandler(t, owner)

	anonymous := request("owner=probe")
	anonymous.Header.Del(principalmeta.HeaderPrincipalID)
	rec := serve(t, h, anonymous)
	if rec.Code != http.StatusUnauthorized ||
		rec.Header().Get("Content-Type") != "application/json" ||
		rec.Header().Get("WWW-Authenticate") != ka1RefusalChallenge ||
		string(bytes.TrimSpace(rec.Body.Bytes())) != ka1RefusalBody {
		t.Errorf("KA1-13 (б): отказ потока не Р2\n  статус %d, WWW-Authenticate %q, тело %s",
			rec.Code, rec.Header().Get("WWW-Authenticate"), rec.Body.String())
	}

	if twin := serve(t, h, request("owner=probe")); twin.Code != http.StatusOK {
		t.Errorf("KA1-14 строка потока: вызывающий с личностью получил %d, поток обязан открыться", twin.Code)
	}
}
