// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dto

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func TestLabelsRoundtrip(t *testing.T) {
	in := domain.LabelsFromMap(map[string]string{
		"env":     "prod",
		"region":  "eu-west",
		"version": "v1.2.3",
	})
	b, err := LabelsToJSONB(in)
	require.NoError(t, err)
	require.NotEmpty(t, b)

	out, err := LabelsFromJSONB(b)
	require.NoError(t, err)
	assert.Equal(t, 3, len(out))
	env, ok := out["env"]
	require.True(t, ok)
	assert.Equal(t, domain.LbLabelVal("prod"), env)
}

func TestLabelsNilEmpty(t *testing.T) {
	b, err := LabelsToJSONB(domain.LbLabels{})
	require.NoError(t, err)
	assert.Equal(t, "{}", string(b))

	out, err := LabelsFromJSONB(nil)
	require.NoError(t, err)
	assert.Equal(t, 0, len(out))

	out2, err := LabelsFromJSONB([]byte("null"))
	require.NoError(t, err)
	assert.Equal(t, 0, len(out2))
}

func TestHealthCheckTCP_Roundtrip(t *testing.T) {
	in := domain.HealthCheck{
		Interval:           domain.LbDuration(3 * time.Second),
		Timeout:            domain.LbDuration(time.Second),
		UnhealthyThreshold: 3,
		HealthyThreshold:   2,
		TCP:                &domain.HealthCheckTCP{Port: 8443},
	}
	b, err := HealthCheckToJSONB(in)
	require.NoError(t, err)
	out, err := HealthCheckFromJSONB(b)
	require.NoError(t, err)
	assert.Equal(t, in.Interval, out.Interval)
	assert.Equal(t, in.UnhealthyThreshold, out.UnhealthyThreshold)
	require.NotNil(t, out.TCP)
	assert.Equal(t, domain.LbPort(8443), out.TCP.Port)
}

func TestHealthCheckHTTPS_Roundtrip(t *testing.T) {
	in := domain.HealthCheck{
		Interval:           domain.LbDuration(5 * time.Second),
		Timeout:            domain.LbDuration(2 * time.Second),
		UnhealthyThreshold: 2,
		HealthyThreshold:   2,
		HTTPS: &domain.HealthCheckHTTPS{
			Port:          443,
			Path:          "/_health",
			ExpectedCodes: "200,204",
		},
	}
	b, err := HealthCheckToJSONB(in)
	require.NoError(t, err)
	out, err := HealthCheckFromJSONB(b)
	require.NoError(t, err)
	require.NotNil(t, out.HTTPS)
	assert.Equal(t, domain.LbPort(443), out.HTTPS.Port)
	assert.Equal(t, "/_health", out.HTTPS.Path)
	assert.Equal(t, "200,204", out.HTTPS.ExpectedCodes)
}

func TestHealthCheckZero_NoTypeRoundtrip(t *testing.T) {
	b, err := HealthCheckToJSONB(domain.HealthCheck{})
	require.NoError(t, err)
	assert.Equal(t, "{}", string(b))

	out, err := HealthCheckFromJSONB(b)
	require.NoError(t, err)
	assert.Nil(t, out.TCP)
	assert.Nil(t, out.HTTP)
}

func TestOptString_RoundTrip(t *testing.T) {
	type myID string
	v := OptFromStr[myID]("hello")
	val, ok := v.Maybe()
	require.True(t, ok)
	assert.Equal(t, myID("hello"), val)
	assert.Equal(t, "hello", OptString(v))

	empty := OptFromStr[myID]("")
	_, ok2 := empty.Maybe()
	assert.False(t, ok2, "empty string → None")
	assert.Equal(t, "", OptString(empty))
}

// TestDurationToSeconds_RejectsWhatTheColumnCannotHold locks the conversion's
// own guarantee: a duration whose whole seconds fall outside the column's range
// is reported, never handed to the driver wrapped around. Before this was
// enforced here, +2147483648s arrived as -2147483648.
func TestDurationToSeconds_RejectsWhatTheColumnCannotHold(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   domain.LbDuration
	}{
		{"one second past the top", domain.LbDuration((int64(math.MaxInt32) + 1) * int64(time.Second))},
		{"one second past the bottom", domain.LbDuration((int64(math.MinInt32) - 1) * int64(time.Second))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DurationToSeconds(tc.in)
			require.Error(t, err, "out-of-range duration must be reported, not truncated")
			assert.Zero(t, got)
		})
	}
}

// TestDurationToSeconds_AcceptsTheDomainRange — the two fields this conversion
// serves are bounded by domain.TargetGroup.Validate to [0s,3600s] and [0s,900s];
// the range check must not disturb them, nor the column's own edges.
func TestDurationToSeconds_AcceptsTheDomainRange(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   domain.LbDuration
		want int32
	}{
		{"zero", domain.LbDuration(0), 0},
		{"deregistration delay max", domain.DeregistrationDelayMax, 3600},
		{"slow start max", domain.SlowStartMax, 900},
		{"column top", domain.LbDuration(int64(math.MaxInt32) * int64(time.Second)), math.MaxInt32},
		{"column bottom", domain.LbDuration(int64(math.MinInt32) * int64(time.Second)), math.MinInt32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DurationToSeconds(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.in, SecondsToDuration(got), "round-trip through the column")
		})
	}
}
