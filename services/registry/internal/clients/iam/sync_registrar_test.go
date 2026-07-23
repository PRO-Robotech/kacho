// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// deadlineCapturingRegisterClient — fake RegisterResourceClient, записывающий per-call
// ctx-deadline (проверка per-call 5s timeout sync-registrar'а, architecture.md
// «per-call deadline на КАЖДОМ внешнем вызове»).
type deadlineCapturingRegisterClient struct {
	reqs      []*iamv1.RegisterResourceRequest
	deadlines []time.Time
	hadDL     []bool
}

func (f *deadlineCapturingRegisterClient) RegisterResource(
	ctx context.Context, in *iamv1.RegisterResourceRequest, _ ...grpc.CallOption,
) (*iamv1.RegisterResourceResponse, error) {
	f.reqs = append(f.reqs, in)
	dl, ok := ctx.Deadline()
	f.deadlines = append(f.deadlines, dl)
	f.hadDL = append(f.hadDL, ok)
	return &iamv1.RegisterResourceResponse{}, nil
}

func (f *deadlineCapturingRegisterClient) UnregisterResource(
	_ context.Context, _ *iamv1.UnregisterResourceRequest, _ ...grpc.CallOption,
) (*iamv1.UnregisterResourceResponse, error) {
	return &iamv1.UnregisterResourceResponse{}, nil
}

// TestSyncRegistrar_OneCallPerTupleWithMapping — sync-registrar вызывает RegisterResource
// РОВНО один раз на каждый tuple каждого intent'а, с EXACT field-mapping parity с
// NewRegisterApplier (SubjectId/Relation/Object/TraceId=ResourceID/Labels/ParentProjectId).
func TestSyncRegistrar_OneCallPerTupleWithMapping(t *testing.T) {
	fake := &scriptedRegisterClient{}
	sr := NewSyncRegistrar(fake)

	// Create-registry intent несёт [project-tuple, owner-tuple] + Labels + ParentProjectID.
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1", Labels: map[string]string{"team": "core"}},
		"user", "usr-abc")
	require.Len(t, intent.Tuples, 2, "project-tuple + owner-tuple")

	err := sr.Register(context.Background(), []domain.RegisterIntent{intent})
	require.NoError(t, err)
	require.Len(t, fake.registerReqs, 2, "один RegisterResource на каждый tuple")

	for i, tup := range intent.Tuples {
		req := fake.registerReqs[i]
		assert.Equal(t, tup.SubjectID, req.GetSubjectId(), "tuple[%d] subject", i)
		assert.Equal(t, tup.Relation, req.GetRelation(), "tuple[%d] relation", i)
		assert.Equal(t, tup.Object, req.GetObject(), "tuple[%d] object", i)
		assert.Equal(t, intent.ResourceID, req.GetTraceId(), "tuple[%d] trace_id=ResourceID", i)
		assert.Equal(t, intent.ParentProjectID, req.GetParentProjectId(), "tuple[%d] parent_project_id", i)
		assert.Equal(t, intent.Labels, req.GetLabels(), "tuple[%d] labels mirror", i)
	}
}

// TestSyncRegistrar_MultipleIntents_AllTuplesRegistered — набор из нескольких intent'ов
// (напр. RepoPush + public-grant) регистрирует все tuple всех intent'ов.
func TestSyncRegistrar_MultipleIntents_AllTuplesRegistered(t *testing.T) {
	fake := &scriptedRegisterClient{}
	sr := NewSyncRegistrar(fake)

	push := domain.RegisterIntentForRepoPush("reg-1", "team/app", "prj-1", "service_account:sva-x")
	pub := domain.RegisterIntentForRepoPublicGrant("reg-1", "team/app")
	total := len(push.Tuples) + len(pub.Tuples)

	require.NoError(t, sr.Register(context.Background(), []domain.RegisterIntent{push, pub}))
	require.Len(t, fake.registerReqs, total, "все tuple обоих intent'ов зарегистрированы")
}

// TestSyncRegistrar_PropagatesFirstError — ошибка RegisterResource прекращает набор и
// возвращается (wrapped) наверх; вызывающий логирует WARN (best-effort — не здесь).
func TestSyncRegistrar_PropagatesFirstError(t *testing.T) {
	boom := errors.New("iam unavailable")
	fake := &scriptedRegisterClient{registerErrs: []error{boom}}
	sr := NewSyncRegistrar(fake)

	intent := domain.RegisterIntentForCreate(&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	err := sr.Register(context.Background(), []domain.RegisterIntent{intent})
	require.Error(t, err)
	require.ErrorIs(t, err, boom, "первая ошибка проброшена (wrapped %w)")
	require.Len(t, fake.registerReqs, 1, "первая ошибка обрывает остаток набора")
}

// TestSyncRegistrar_NilClient — nil RegisterResourceClient → error (defensive; в проде
// serve.go подключает sync-registrar только при непустом iamConn).
func TestSyncRegistrar_NilClient(t *testing.T) {
	sr := NewSyncRegistrar(nil)
	intent := domain.RegisterIntent{Tuples: []domain.FGATuple{
		{SubjectID: "user:usr-1", Relation: "owner", Object: "registry_registry:reg-1"},
	}}
	require.Error(t, sr.Register(context.Background(), []domain.RegisterIntent{intent}))
}

// TestSyncRegistrar_EmptyIntents — пустой набор → без вызовов, nil-error.
func TestSyncRegistrar_EmptyIntents(t *testing.T) {
	fake := &scriptedRegisterClient{}
	sr := NewSyncRegistrar(fake)
	require.NoError(t, sr.Register(context.Background(), nil))
	require.Empty(t, fake.registerReqs)
}

// TestSyncRegistrar_PerCallDeadline — каждый RegisterResource несёт собственный
// per-call deadline (~5s), не сырой request-ctx (неотвечающий iam иначе повис бы).
func TestSyncRegistrar_PerCallDeadline(t *testing.T) {
	fake := &deadlineCapturingRegisterClient{}
	sr := NewSyncRegistrar(fake)

	intent := domain.RegisterIntentForCreate(&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	require.NoError(t, sr.Register(context.Background(), []domain.RegisterIntent{intent}))
	require.Len(t, fake.hadDL, 2)
	for i := range fake.hadDL {
		require.True(t, fake.hadDL[i], "call[%d]: per-call deadline установлен", i)
		d := time.Until(fake.deadlines[i])
		assert.Greater(t, d, 4*time.Second, "call[%d]: deadline ~5s (нижняя граница)", i)
		assert.LessOrEqual(t, d, 5*time.Second, "call[%d]: deadline ~5s (верхняя граница)", i)
	}
}
