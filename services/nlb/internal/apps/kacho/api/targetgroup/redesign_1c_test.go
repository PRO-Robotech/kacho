// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// makeHTTPProbeTG — seed a TG with an HTTP probe and tuned scalars, for the
// oneof-replace merge scenarios.
func makeHTTPProbeTG(t *testing.T, repo *fakeRepo, name string) *kachorepo.TargetGroupRecord {
	t.Helper()
	tg := makeTG("prj-acme", name)
	tg.Port = 8080
	tg.HealthCheck = domain.HealthCheck{
		Interval:           domain.LbDuration(3 * time.Second),
		Timeout:            domain.LbDuration(1 * time.Second),
		HealthyThreshold:   5,
		UnhealthyThreshold: 4,
		HTTP:               &domain.HealthCheckHTTP{Path: "/healthz", ExpectedCodes: "200-299"},
	}
	repo.seedTG(tg)
	return tg
}

func readTG(t *testing.T, repo *fakeRepo, id string) *kachorepo.TargetGroupRecord {
	t.Helper()
	rd, err := repo.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.TargetGroups().Get(context.Background(), id)
	require.NoError(t, err)
	return got
}

// NLB-1-36: HealthCheck scalar dotted-mask PATCH — merge-validated. interval
// change re-validated against the STORED timeout; probe + siblings untouched.
func TestUpdate_HealthCheckDottedMask_MergeValidated_NLB_1_36(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-dotted")
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check.interval"}},
		HealthCheck:   &lbv1.HealthCheck{Interval: durationpb.New(2 * time.Second)},
	})
	require.NoErrorf(t, err, "details=%s", fieldViolationsText(err))
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	got := readTG(t, repo, string(tg.ID))
	assert.Equal(t, domain.LbDuration(2*time.Second), got.HealthCheck.Interval, "interval merged")
	assert.Equal(t, domain.LbDuration(1*time.Second), got.HealthCheck.Timeout, "stored timeout preserved")
	require.NotNil(t, got.HealthCheck.HTTP, "probe type preserved (HTTP)")
	assert.Equal(t, "/healthz", got.HealthCheck.HTTP.Path, "probe body preserved")
	assert.EqualValues(t, 5, got.HealthCheck.HealthyThreshold, "sibling scalar preserved")
}

// NLB-1-36 (negative): merged cross-field violation (timeout > interval on the
// merged object) → INVALID_ARGUMENT.
func TestUpdate_HealthCheckDottedMask_MergedCrossFieldViolation_NLB_1_36(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-xfield")
	// store interval=2s so a timeout=4s merge is invalid (timeout > interval).
	stored := tg.HealthCheck
	stored.Interval = domain.LbDuration(2 * time.Second)
	tg.HealthCheck = stored
	repo.seedTG(tg)
	uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check.timeout"}},
		HealthCheck:   &lbv1.HealthCheck{Timeout: durationpb.New(4 * time.Second)},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// NLB-1-37: probe atomic-replace (http → grpc) preserves tuned sibling scalars
// (regression-lock "probe-type switch preserves tuned scalars").
func TestUpdate_ProbeAtomicReplace_ScalarPreservation_NLB_1_37(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-probe-swap")
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check.grpc"}},
		HealthCheck: &lbv1.HealthCheck{Options: &lbv1.HealthCheck_Grpc{
			Grpc: &lbv1.HealthCheck_GrpcOptions{ServiceName: "grpc.health.v1.Health"},
		}},
	})
	require.NoErrorf(t, err, "details=%s", fieldViolationsText(err))
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	got := readTG(t, repo, string(tg.ID)).HealthCheck
	require.NotNil(t, got.GRPC, "probe replaced to grpc")
	assert.Equal(t, "grpc.health.v1.Health", got.GRPC.ServiceName)
	assert.Nil(t, got.HTTP, "old http probe cleared")
	// Tuned sibling scalars survive the probe-type switch.
	assert.Equal(t, domain.LbDuration(3*time.Second), got.Interval)
	assert.Equal(t, domain.LbDuration(1*time.Second), got.Timeout)
	assert.EqualValues(t, 5, got.HealthyThreshold)
	assert.EqualValues(t, 4, got.UnhealthyThreshold)
}

// NLB-1-38 (negative): probe mask path without a discriminator body →
// INVALID_ARGUMENT (not silent-clear).
func TestUpdate_ProbeMaskMissingDiscriminator_NLB_1_38(t *testing.T) {
	repo := newFakeRepo()
	tg := makeHTTPProbeTG(t, repo, "hc-nodisc")
	uc := NewUpdateTargetGroupUseCase(repo, newFakeOpsRepo(), nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"health_check.http"}},
		HealthCheck:   &lbv1.HealthCheck{Interval: durationpb.New(2 * time.Second)}, // no http body
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "health_check.http requires the http probe body")
}

// NLB-1-42: deregistration_delay LIVE-mutable via mask (Duration form).
func TestUpdate_DeregistrationDelayLiveMutable_NLB_1_42(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "hc-dereg")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId:       string(tg.ID),
		UpdateMask:          &fieldmaskpb.FieldMask{Paths: []string{"deregistration_delay"}},
		DeregistrationDelay: durationpb.New(300 * time.Second),
	})
	require.NoErrorf(t, err, "details=%s", fieldViolationsText(err))
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	got := readTG(t, repo, string(tg.ID))
	assert.Equal(t, domain.LbDuration(300*time.Second), got.DeregistrationDelay)
	assert.Equal(t, domain.LbDuration(0), got.SlowStart, "slow_start untouched (0s)")
}

// NLB-1-56: TargetGroup.port LIVE-mutable via mask. (The listener
// resolved_backend_port re-echo is a derived read-only SQL projection of
// tg.port — locked at the integration level; here we lock port mutability.)
func TestUpdate_PortLiveMutable_NLB_1_56(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-acme", "hc-port")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)

	op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
		TargetGroupId: string(tg.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"port"}},
		Port:          9090,
	})
	require.NoErrorf(t, err, "details=%s", fieldViolationsText(err))
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	got := readTG(t, repo, string(tg.ID))
	assert.EqualValues(t, 9090, got.Port)
}

// NLB-1-39: probe.port override surfaces via effective_port; omitted port
// inherits TargetGroup.port. Domain-level lock of the derivation.
func TestHealthCheck_EffectivePort_NLB_1_39(t *testing.T) {
	tgPort := domain.LbPort(8080)

	// override present → effective_port reflects the override.
	over := domain.HealthCheck{HTTPS: &domain.HealthCheckHTTPS{Port: 8443}}
	assert.EqualValues(t, 8443, over.EffectivePort(tgPort))

	// no probe port → inherit TargetGroup.port.
	inherit := domain.HealthCheck{HTTP: &domain.HealthCheckHTTP{Path: "/healthz"}}
	assert.EqualValues(t, 8080, inherit.EffectivePort(tgPort))

	// tcp with no override also inherits.
	tcp := domain.HealthCheck{TCP: &domain.HealthCheckTCP{}}
	assert.EqualValues(t, 8080, tcp.EffectivePort(tgPort))
}
