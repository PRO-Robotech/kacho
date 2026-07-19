// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar_test.go — unit-тесты SyncRegistrar (adapter поверх
// InternalIAMService.RegisterResource) БЕЗ Postgres: scripted fake-client.
//
// Проверяет sync-primary owner-tuple контракт:
//   - per-tuple RegisterResource с forward'ом mirror-полей + монотонного
//     source_version + per-call deadline;
//   - NON-short-circuit (все tuple'ы атакуются даже при ошибке на предыдущем);
//   - proxy-rejection (PermissionDenied/InvalidArgument на не-registrable
//     relation) — BENIGN, не всплывает наружу (async drainer идентичен);
//   - transient-сбой (Unavailable) — всплывает как error (use-case логирует
//     best-effort);
//   - nil client → no-op.
package iam_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// scriptedRegisterClient — RegisterResourceClient-двойник: пишет каждый
// RegisterResource-запрос (для assert'ов forward'а) + флаг наличия deadline,
// возвращает scripted-ошибку по relation.
type scriptedRegisterClient struct {
	mu            sync.Mutex
	register      []*iampb.RegisterResourceRequest
	hadDeadline   []bool
	errByRelation map[string]error
}

func (c *scriptedRegisterClient) RegisterResource(
	ctx context.Context, in *iampb.RegisterResourceRequest, _ ...grpc.CallOption,
) (*iampb.RegisterResourceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.register = append(c.register, in)
	_, ok := ctx.Deadline()
	c.hadDeadline = append(c.hadDeadline, ok)
	if c.errByRelation != nil {
		if err := c.errByRelation[in.GetRelation()]; err != nil {
			return nil, err
		}
	}
	return &iampb.RegisterResourceResponse{}, nil
}

func (c *scriptedRegisterClient) UnregisterResource(
	_ context.Context, _ *iampb.UnregisterResourceRequest, _ ...grpc.CallOption,
) (*iampb.UnregisterResourceResponse, error) {
	return &iampb.UnregisterResourceResponse{}, nil
}

func (c *scriptedRegisterClient) relations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.register))
	for i, r := range c.register {
		out[i] = r.GetRelation()
	}
	return out
}

const (
	testLBID   = "nlb-aaaaaaaaaaaaaaaaa"
	testProjID = "prj-prod000000000000"
)

// lbUserIntent — LB owner-intent аутентифицированного user'а: project-tuple
// ПЕРВЫМ (containment, iam-proxy accepts) + creator(admin) вторым (iam-proxy
// rejects — не registrable relation).
func lbUserIntent() domain.FGARegisterIntent {
	return domain.FGARegisterIntent{
		Kind:            "NetworkLoadBalancer",
		ResourceID:      testLBID,
		Labels:          map[string]string{"tier": "critical"},
		ParentProjectID: testProjID,
		ParentAccountID: "acc-aaaaaaaaaaaaaaaa",
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, testLBID, testProjID),
			domain.FGACreatorTuple("user:usr-1", domain.FGAObjectTypeLoadBalancer, testLBID),
		},
	}
}

// TestSyncRegistrar_RegistersEachTuple_ForwardsMirrorFields — happy path:
// RegisterResource вызывается по одному разу на tuple; forward'ит mirror-поля
// (labels/parent) + монотонный source_version (stamped registrar'ом) + несёт
// per-call deadline. Возвращает nil.
func TestSyncRegistrar_RegistersEachTuple_ForwardsMirrorFields(t *testing.T) {
	cli := &scriptedRegisterClient{}
	reg := iam.NewSyncRegistrar(cli)

	err := reg.Register(context.Background(), lbUserIntent())
	require.NoError(t, err)

	require.Len(t, cli.register, 2, "one RegisterResource per tuple")
	// project-tuple первым.
	got := cli.register[0]
	assert.Equal(t, domain.FGARelationProject, got.GetRelation())
	assert.Equal(t, "project:"+testProjID, got.GetSubjectId())
	assert.Equal(t, "lb_network_load_balancer:"+testLBID, got.GetObject())
	assert.Equal(t, map[string]string{"tier": "critical"}, got.GetLabels(), "labels forwarded")
	assert.Equal(t, testProjID, got.GetParentProjectId(), "parent_project_id forwarded")
	assert.Equal(t, "acc-aaaaaaaaaaaaaaaa", got.GetParentAccountId(), "parent_account_id forwarded")
	require.NotNil(t, got.GetSourceVersion(), "source_version stamped by sync-registrar")
	// per-call deadline на каждом вызове.
	for i, ok := range cli.hadDeadline {
		assert.Truef(t, ok, "call[%d] must carry a per-call deadline", i)
	}
}

// TestSyncRegistrar_ProxyRejection_IsBenign — iam-proxy отвергает creator(admin)
// tuple (не registrable relation) как PermissionDenied, но принимает
// project-tuple. Sync-registrar НЕ короткозамыкается: атакует ОБА tuple'а,
// project-tuple применяется (видимость достигнута), а PermissionDenied на admin
// — BENIGN (не всплывает, т.к. async drainer его тоже не применит). Register → nil.
func TestSyncRegistrar_ProxyRejection_IsBenign(t *testing.T) {
	cli := &scriptedRegisterClient{
		errByRelation: map[string]error{
			domain.FGARelationAdmin: status.Error(codes.PermissionDenied, "permission denied"),
		},
	}
	reg := iam.NewSyncRegistrar(cli)

	err := reg.Register(context.Background(), lbUserIntent())
	require.NoError(t, err, "proxy-rejection of non-registrable relation is benign for best-effort sync path")
	require.Equal(t, []string{domain.FGARelationProject, domain.FGARelationAdmin}, cli.relations(),
		"non-short-circuit: both tuples attempted, project-tuple applied")
}

// TestSyncRegistrar_InvalidArgument_IsBenign — InvalidArgument на tuple (malformed
// на стороне proxy) — тоже BENIGN (drainer его бы poison'ил); не всплывает.
func TestSyncRegistrar_InvalidArgument_IsBenign(t *testing.T) {
	cli := &scriptedRegisterClient{
		errByRelation: map[string]error{
			domain.FGARelationAdmin: status.Error(codes.InvalidArgument, "bad tuple"),
		},
	}
	reg := iam.NewSyncRegistrar(cli)
	require.NoError(t, reg.Register(context.Background(), lbUserIntent()))
}

// TestSyncRegistrar_TransientError_Surfaced — iam недоступен (Unavailable) на
// containment-tuple → Register возвращает НЕ-nil (transient), чтобы use-case
// залогировал best-effort. Async drainer досведёт из durable outbox.
func TestSyncRegistrar_TransientError_Surfaced(t *testing.T) {
	cli := &scriptedRegisterClient{
		errByRelation: map[string]error{
			domain.FGARelationProject: status.Error(codes.Unavailable, "iam down"),
		},
	}
	reg := iam.NewSyncRegistrar(cli)

	err := reg.Register(context.Background(), lbUserIntent())
	require.Error(t, err, "transient iam failure surfaced so caller logs best-effort")
}

// TestSyncRegistrar_ContinuesPastTransient — NON-short-circuit: transient на
// ПЕРВОМ tuple не мешает атаковать остальные (project-tuple всегда первый, но
// проверяем инвариант «все tuple'ы attempted»).
func TestSyncRegistrar_ContinuesPastTransient(t *testing.T) {
	cli := &scriptedRegisterClient{
		errByRelation: map[string]error{
			domain.FGARelationProject: status.Error(codes.Unavailable, "iam down"),
		},
	}
	reg := iam.NewSyncRegistrar(cli)

	err := reg.Register(context.Background(), lbUserIntent())
	require.Error(t, err)
	require.Len(t, cli.register, 2, "non-short-circuit: all tuples attempted even after a transient error")
}

// TestSyncRegistrar_NilClient_NoOp — nil client (dev/no-iam) → Register no-op
// (nil, без panic). Async register-drainer остаётся единственным путём.
func TestSyncRegistrar_NilClient_NoOp(t *testing.T) {
	reg := iam.NewSyncRegistrar(nil)
	require.NoError(t, reg.Register(context.Background(), lbUserIntent()))
}
