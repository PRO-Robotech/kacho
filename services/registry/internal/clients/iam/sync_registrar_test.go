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

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// deadlineCapturingRegisterClient — fake RegisterResourceClient, записывающий per-call
// ctx-deadline (проверка per-call 5s timeout sync-registrar'а, architecture.md
// «per-call deadline на КАЖДОМ внешнем вызове»).
// fixtureStamp — подставной штамп writer-транзакции. Намеренно НЕ похож на
// «сейчас»: значение, отличимое от настоящего, не даёт спутать проброс с
// выдумыванием на месте.
var fixtureStamp = time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)

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
	sr, cerr := NewSyncRegistrar(fake)
	require.NoError(t, cerr)

	// Create-registry intent несёт [project-tuple, owner-tuple] + Labels + ParentProjectID.
	intent := domain.RegisterIntentForCreate(
		&domain.Registry{ID: "reg-1", ProjectID: "prj-1", Labels: map[string]string{"team": "core"}},
		"user", "usr-abc")
	// Версия writer-транзакции обязательна: без неё доставки не происходит вовсе,
	// и все утверждения ниже стали бы вакуумными.
	intent.SourceVersion = domain.SourceVersion{Time: fixtureStamp}
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
	sr, cerr := NewSyncRegistrar(fake)
	require.NoError(t, cerr)

	push := domain.RegisterIntentForRepoPush("reg-1", "team/app", "prj-1", "service_account:sva-x")
	push.SourceVersion = domain.SourceVersion{Time: fixtureStamp}
	pub := domain.RegisterIntentForRepoPublicGrant("reg-1", "team/app")
	pub.SourceVersion = domain.SourceVersion{Time: fixtureStamp.Add(time.Millisecond)}
	total := len(push.Tuples) + len(pub.Tuples)

	require.NoError(t, sr.Register(context.Background(), []domain.RegisterIntent{push, pub}))
	require.Len(t, fake.registerReqs, total, "все tuple обоих intent'ов зарегистрированы")
}

// TestSyncRegistrar_SurfacesFailureWithoutAbandoningTheRest — отказ на одном
// tuple'е возвращается наверх и при этом НЕ обрывает набор.
//
// Прежняя редакция утверждала обратное («первая ошибка обрывает остаток») — это
// была семантика собственного регистратора registry. Общая форма пробует ВСЕ
// строки: первым в наборе идёт указатель принадлежности объекта, через который
// администратор аккаунта достаёт ресурс вообще, и терять соседей по набору из-за
// отказа на одном значит терять этот доступ до дренажа.
func TestSyncRegistrar_SurfacesFailureWithoutAbandoningTheRest(t *testing.T) {
	boom := errors.New("iam unavailable")
	fake := &scriptedRegisterClient{registerErrs: []error{boom}}
	sr, cerr := NewSyncRegistrar(fake)
	require.NoError(t, cerr)

	intent := domain.RegisterIntentForCreate(&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	intent.SourceVersion = domain.SourceVersion{Time: fixtureStamp}
	err := sr.Register(context.Background(), []domain.RegisterIntent{intent})
	require.Error(t, err)
	require.ErrorIs(t, err, boom, "отказ обязан быть виден вызывающему (wrapped %w)")
	require.Len(t, fake.registerReqs, 2, "отказ на первом tuple'е не отменяет попытки по остальным")
}

// TestSyncRegistrar_NilClientRefusedByConstructor — нулевой клиент отвергается
// КОНСТРУКТОРОМ, а не проявляется отказом на первой доставке.
//
// Разница не косметическая: отказ при сборке роняет старт, а отказ на доставке
// — всего лишь строка в логе на пути, который объявлен best-effort. Пустая
// операция здесь неотличима от исправно работающего ускорителя, который никогда
// ничего не ускорял.
func TestSyncRegistrar_NilClientRefusedByConstructor(t *testing.T) {
	_, err := NewSyncRegistrar(nil)
	require.ErrorIs(t, err, ownerregister.ErrNoClient)
}

// TestSyncRegistrar_EmptyIntents — пустой набор → без вызовов, nil-error.
func TestSyncRegistrar_EmptyIntents(t *testing.T) {
	fake := &scriptedRegisterClient{}
	sr, cerr := NewSyncRegistrar(fake)
	require.NoError(t, cerr)
	require.NoError(t, sr.Register(context.Background(), nil))
	require.Empty(t, fake.registerReqs)
}

// TestSyncRegistrar_PerCallDeadline — каждый RegisterResource несёт собственный
// per-call deadline (~5s), не сырой request-ctx (неотвечающий iam иначе повис бы).
func TestSyncRegistrar_PerCallDeadline(t *testing.T) {
	fake := &deadlineCapturingRegisterClient{}
	sr, cerr := NewSyncRegistrar(fake)
	require.NoError(t, cerr)

	intent := domain.RegisterIntentForCreate(&domain.Registry{ID: "reg-1", ProjectID: "prj-1"}, "user", "usr-abc")
	intent.SourceVersion = domain.SourceVersion{Time: fixtureStamp}
	require.NoError(t, sr.Register(context.Background(), []domain.RegisterIntent{intent}))
	require.Len(t, fake.hadDL, 2)
	for i := range fake.hadDL {
		require.True(t, fake.hadDL[i], "call[%d]: per-call deadline установлен", i)
		d := time.Until(fake.deadlines[i])
		assert.Greater(t, d, 4*time.Second, "call[%d]: deadline ~5s (нижняя граница)", i)
		assert.LessOrEqual(t, d, 5*time.Second, "call[%d]: deadline ~5s (верхняя граница)", i)
	}
}
