// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware/anonmail"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// TestAnonMail_NTF2_59_StoreUnavailableCounterExistsWithZeroAndGrows —
// `kacho_api_gateway_anon_mail_store_unavailable_total` (приёмка NTF-2, Р5,
// NTF2-59): серия стоит с нулём до первого отказа и растёт на каждый ответ 503
// ограничителя.
func TestAnonMail_NTF2_59_StoreUnavailableCounterExistsWithZeroAndGrows(t *testing.T) {
	var st anonmail.Stats
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAnonMail(func() anonmail.Stats { return st })
	require.Contains(t, expose(t, m), "kacho_api_gateway_anon_mail_store_unavailable_total 0")
	st.StoreUnavailable = 4
	require.Contains(t, expose(t, m), "kacho_api_gateway_anon_mail_store_unavailable_total 4")
}
