// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics_test

import (
	"testing"
	"time"

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

// TestAnonMail_D66_SaturationCounterExistsWithZeroAndGrows —
// `kacho_api_gateway_anon_mail_bucket_wait_timeouts_total` (решение Д66): серия
// стоит с нулём до первого отказа «строка ведра не получена» и растёт отдельно от общего
// счётчика недоступности — 503 по иной причине её не двигает.
func TestAnonMail_D66_SaturationCounterExistsWithZeroAndGrows(t *testing.T) {
	var st anonmail.Stats
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAnonMail(func() anonmail.Stats { return st })
	require.Contains(t, expose(t, m), "kacho_api_gateway_anon_mail_bucket_wait_timeouts_total 0")
	st.StoreUnavailable, st.BucketWaitTimeouts = 5, 2
	out := expose(t, m)
	require.Contains(t, out, "kacho_api_gateway_anon_mail_bucket_wait_timeouts_total 2")
	require.Contains(t, out, "kacho_api_gateway_anon_mail_store_unavailable_total 5")
}

// TestAnonMail_D66_BucketHoldSecondsExistsWithZeroAndGrows —
// `kacho_api_gateway_anon_mail_bucket_hold_seconds_total` (Д66; ревью
// system-design CRIT-1): опережающая серия насыщения — секунды, пока решения
// реплики держат строку ведра. Стоит с нулём до первого решения и растёт в
// секундах, а не в штуках.
func TestAnonMail_D66_BucketHoldSecondsExistsWithZeroAndGrows(t *testing.T) {
	var st anonmail.Stats
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAnonMail(func() anonmail.Stats { return st })
	require.Contains(t, expose(t, m), "kacho_api_gateway_anon_mail_bucket_hold_seconds_total 0")
	st.BucketHold = 1500 * time.Millisecond
	require.Contains(t, expose(t, m), "kacho_api_gateway_anon_mail_bucket_hold_seconds_total 1.5")
}

// TestAnonMail_I2_ClockOffsetIsAbsentUntilMeasured —
// `kacho_api_gateway_anon_mail_clock_offset_seconds` (ревью system-design I-2,
// решение Д71): смещение часов реплики от часов базы. Пока ни одно решение его
// не измерило (или хранилище — memory, у которого часов базы нет), серии нет:
// ноль значил бы «часы сверены». Измеренное — со знаком: реплика впереди базы —
// плюс.
func TestAnonMail_I2_ClockOffsetIsAbsentUntilMeasured(t *testing.T) {
	var st anonmail.Stats
	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAnonMail(func() anonmail.Stats { return st })
	require.NotContains(t, expose(t, m), "kacho_api_gateway_anon_mail_clock_offset_seconds ")
	st.ClockOffset, st.ClockOffsetMeasured = -2500*time.Millisecond, true
	require.Contains(t, expose(t, m), "kacho_api_gateway_anon_mail_clock_offset_seconds -2.5")
}
