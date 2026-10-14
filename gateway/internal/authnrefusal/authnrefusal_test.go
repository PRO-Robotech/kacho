// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authnrefusal_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/PRO-Robotech/kacho/gateway/internal/authnrefusal"
)

// TestRESTBodyIsTheNativeStatus — тело REST-отказа и статус нативной
// поверхности говорят одно и то же: код, текст, одна ErrorInfo с теми же
// reason/domain и без metadata. Тело держится литералом, и без этой сверки
// правка одной формы расходилась бы с другой молча.
func TestRESTBodyIsTheNativeStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	authnrefusal.WriteHTTP(rec)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != authnrefusal.Challenge ||
		rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("REST-отказ: %d %v", rec.Code, rec.Header())
	}
	var got struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Type     string            `json:"@type"`
			Reason   string            `json:"reason"`
			Domain   string            `json:"domain"`
			Metadata map[string]string `json:"metadata"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело не JSON: %v — %s", err, rec.Body)
	}
	st := authnrefusal.Status()
	if got.Code != int(st.Code()) || got.Message != st.Message() || len(got.Details) != 1 || len(st.Details()) != 1 {
		t.Fatalf("тело %+v расходится со статусом %v", got, st.Proto())
	}
	info, ok := st.Details()[0].(*errdetails.ErrorInfo)
	if !ok {
		t.Fatalf("деталь статуса не ErrorInfo: %T", st.Details()[0])
	}
	d := got.Details[0]
	if d.Type != "type.googleapis.com/google.rpc.ErrorInfo" || d.Reason != info.GetReason() ||
		d.Domain != info.GetDomain() || len(d.Metadata) != 0 || len(info.GetMetadata()) != 0 {
		t.Errorf("деталь тела %+v расходится с деталью статуса %v", d, info)
	}
}
