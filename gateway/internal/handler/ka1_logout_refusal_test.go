// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
)

// Отказ Р2 приёмки KA1 — дословно (`sub-phase-KA1-edge-refusals-and-call-budgets-acceptance.md`).
const (
	ka1RefusalBody      = `{"code":16,"message":"authentication failed","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"AUTHN_REQUIRED","domain":"kaname.cloud.iam.v1"}]}`
	ka1RefusalChallenge = `Bearer realm="kacho", error="invalid_token"`
)

func ka1LogoutRequest() *http.Request {
	form := url.Values{"revoke_all": {"true"}}
	r := httptest.NewRequest(http.MethodPost, "/oauth/logout", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Authorization", "Bearer ka1-presented")
	return r
}

// TestKA1_13a_LogoutRefusalIsTheOneRefusal — KA1-13 (а): `POST /oauth/logout` с
// `revoke_all=true` и предъявителем, которого проверяющий вызывающего отвергает,
// отвечает ТЕМ ЖЕ отказом `401`, что слой аутентификации края (KA1-10). Отказ
// решает проверяющий (П12), а не проверка подписи.
//
// Близнец (строка выхода KA1-14): годный предъявитель, `Revoke` отвечает — `200`
// с `ok:true`.
func TestKA1_13a_LogoutRefusalIsTheOneRefusal(t *testing.T) {
	rev := &recordingRevocations{}
	h, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger: newLogger(), Revocations: rev,
		Verifier: &fakeVerifier{err: errors.New("ka1: presenter refused")},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ka1LogoutRequest())
	if rec.Code != http.StatusUnauthorized ||
		rec.Header().Get("Content-Type") != "application/json" ||
		rec.Header().Get("WWW-Authenticate") != ka1RefusalChallenge ||
		string(bytes.TrimSpace(rec.Body.Bytes())) != ka1RefusalBody {
		t.Errorf("KA1-13 (а): отказ выхода не Р2\n  статус %d, WWW-Authenticate %q, тело %s",
			rec.Code, rec.Header().Get("WWW-Authenticate"), rec.Body.String())
	}
	if rev.calls.Load() != 0 {
		t.Errorf("KA1-13 (а): отвергнутый вызывающий дошёл до отзыва (%d вызовов)", rev.calls.Load())
	}

	twin, err := handler.NewLogoutHandler(handler.LogoutHandlerConfig{CallBudget: time.Second,
		Logger: newLogger(), Revocations: rev,
		Verifier: &fakeVerifier{caller: &handler.VerifiedCaller{Subject: "usr-00000000000000ka1", JTI: "jti-ka1-live"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	twin.ServeHTTP(rec, ka1LogoutRequest())
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || out["ok"] != true {
		t.Errorf("KA1-14 строка выхода: ожидался 200 с ok:true, получено %d %s", rec.Code, rec.Body.String())
	}
}
