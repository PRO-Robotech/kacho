// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// refusal_names_next_step_internal_test.go — производитель отказа, который
// пакетные пробы зовут напрямую: отказ модели прав по свежести второго фактора
// называет тот же шаг, что указание пола (сторона края kaname#511, R36 п. 1).

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

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
