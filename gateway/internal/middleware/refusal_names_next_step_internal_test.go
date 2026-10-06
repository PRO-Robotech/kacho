// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// refusal_names_next_step_internal_test.go — производители отказа, которые
// пакетные пробы зовут напрямую:
//
//   - отказ адреса почты называет шаг подтверждения (сторона края kaname#526,
//     решение R36 п. 2) — на HTTP и на gRPC одним текстом;
//   - отказ модели прав по свежести второго фактора называет тот же шаг, что
//     указание пола (сторона края kaname#511, R36 п. 1).

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

const addressNextStepText = "email address is not verified: confirm it with the code from the letter (POST /iam/v1/auth/verify-email/confirm)"

func TestAddressRefusal_526_NamesTheConfirmationStep_HTTPAndGRPC(t *testing.T) {
	rec := httptest.NewRecorder()
	writeHTTPAddressRefusal(rec)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, `{"code":7,"message":"`+addressNextStepText+
		`","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"EMAIL_NOT_VERIFIED","domain":"iam.kaname.cloud"}]}`,
		rec.Body.String())

	st := addressRefusalStatus()
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.Equal(t, addressNextStepText, st.Message())
}

func TestModelStepUpDeny_511_NamesTheSameStepAsTheFloor_HTTPAndGRPC(t *testing.T) {
	const stepText = "authentication level is insufficient: step up with a second factor, or present a credential of another kind"
	desc := permissionDeniedDescriptor{Subject: "user:usr_x", Action: "vpc.networks.delete",
		ResourceType: "vpc_network", ResourceID: "enp_x", FQN: "kacho.cloud.vpc.v1.NetworkService/Delete"}
	reasons := []string{"mfa_fresh: acr=2 (need 3)"}

	st := buildGRPCDenyStatus(desc, reasons)
	require.Equal(t, codes.PermissionDenied, st.Code())
	require.Equal(t, stepText, st.Message())

	rec := httptest.NewRecorder()
	writeHTTPDeny(rec, desc, reasons, `Bearer error="insufficient_user_authentication", acr_values="3"`)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), `"message":"`+stepText+`"`)

	// Близнец: отказ без причины уровня — прежний текст по каталогу.
	plain := buildGRPCDenyStatus(desc, []string{"no path: account"})
	require.Equal(t, "permission denied: vpc.networks.delete", plain.Message())
}
