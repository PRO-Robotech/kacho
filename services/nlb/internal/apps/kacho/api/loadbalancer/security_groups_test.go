// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// fakeSecurityGroupClient — in-memory SecurityGroupClient double. byID returns a
// same-project SG; getErr forces an error (peer down / not found). `calls`
// counts peer round-trips — the observable the cardinality cap and the intake
// dedup must bound (a leak there is invisible to code-level assertions).
type fakeSecurityGroupClient struct {
	byID   map[string]*vpcclient.SecurityGroup
	getErr error
	calls  int
}

func (f *fakeSecurityGroupClient) Get(_ context.Context, id string) (*vpcclient.SecurityGroup, error) {
	f.calls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if sg, ok := f.byID[id]; ok {
		return sg, nil
	}
	return nil, fmt.Errorf("%w: SecurityGroup %s not found", domain.ErrFailedPrecondition, id)
}

// baseInternalSGReq — INTERNAL_REGIONAL Create (SGs are INTERNAL-only) with the
// given security_group_ids.
func baseInternalSGReq(sgIDs ...string) *lbv1.CreateNetworkLoadBalancerRequest {
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)
	req.SecurityGroupIds = sgIDs
	return req
}

// NLB-1-51 (F2, MIGRATE): securityGroupIds set@Create, same-project existence
// peer-validated; echoed on read. No region-coherence.
func TestLoadBalancer_NLB_1_51_SecurityGroupIds_Happy(t *testing.T) {
	t.Parallel()
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{
		"sg-0k4m7t2y9u1i3o5p": {ID: "sg-0k4m7t2y9u1i3o5p", ProjectID: "prj-a"},
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
	req := baseInternalSGReq("sg-0k4m7t2y9u1i3o5p")
	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	require.Equal(t, []string{"sg-0k4m7t2y9u1i3o5p"}, lbByName(t, repo, "lb-1").SecurityGroupIDs)
}

// NLB-1-52 (F2, MIGRATE): non-existent / cross-project SG → FAILED_PRECONDITION.
func TestLoadBalancer_NLB_1_52_SecurityGroupIds_PeerValidate(t *testing.T) {
	t.Parallel()

	t.Run("non-existent SG → FailedPrecondition", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{}}
		uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
		_, err := uc.Execute(context.Background(), baseInternalSGReq("sg-00000000000000"))
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("cross-project SG → FailedPrecondition (anti-oracle)", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{
			"sg-other": {ID: "sg-other", ProjectID: "prj-other"},
		}}
		uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
		_, err := uc.Execute(context.Background(), baseInternalSGReq("sg-other"))
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("vpc unavailable → Unavailable (fail-closed)", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{getErr: fmt.Errorf("%w: vpc down", domain.ErrUnavailable)}
		uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
		_, err := uc.Execute(context.Background(), baseInternalSGReq("sg-0k4m7t2y9u1i3o5p"))
		require.Equal(t, codes.Unavailable, status.Code(err))
	})
}

// NLB-1-52 (edge): securityGroupIds on a non-INTERNAL LB → InvalidArgument
// (SGs are network-scoped; mirrors DB CHECK load_balancers_sg_internal_check).
func TestLoadBalancer_NLB_1_52_SecurityGroupIds_ExternalRejected(t *testing.T) {
	t.Parallel()
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{
		"sg-x": {ID: "sg-x", ProjectID: "prj-a"},
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{sg: sg, addr: &fakeAddressClient{}})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipPublic()
	req.SecurityGroupIds = []string{"sg-x"}
	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "only valid for INTERNAL")
}

// seedInternalLB seeds an in-memory INTERNAL/REGIONAL LB (SGs are INTERNAL-only,
// so the shared `seedLB` EXTERNAL fixture cannot drive the SG Update lane).
func seedInternalLB(t *testing.T, repo *fakeRepo, projectID, name string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixLoadBalancer)
	repo.lbs[id] = &kachorepo.LoadBalancerRecord{
		LoadBalancer: domain.LoadBalancer{
			ID: domain.ResourceID(id), ProjectID: domain.ProjectID(projectID),
			RegionID: "ru-central1", Name: domain.LbName(name),
			Type: domain.LBTypeInternal, PlacementType: domain.PlacementRegional,
			Placement:       domain.PlacementInternalRegional,
			Status:          domain.LBStatusInactive,
			SessionAffinity: domain.SessionAffinity5Tuple,
			AdminState:      domain.AdminStateEnabled,
		},
	}
	return id
}

// sgFieldViolations — "<field>: <description>" из BadRequest-details статуса
// (текст cardinality-отказа по конвенции Kachō живёт в field-violation, не в
// status.Message).
func sgFieldViolations(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	var parts []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			parts = append(parts, v.GetField()+": "+v.GetDescription())
		}
	}
	return strings.Join(parts, " | ")
}

// sgIDs генерирует n различающихся SG-id'шников.
func sgIDs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("sg-%06d", i))
	}
	return out
}

// TestLoadBalancer_SecurityGroupIds_CardinalityCap — DoS-амплификация через
// security_group_ids: каждый элемент = один синхронный vpc-Get (+FGA-Check) на
// handler-потоке, ДО создания Operation. Без cap'а `securityGroupIds: [...]*5000`
// превращает один дешёвый запрос в 5000 peer-round-trip'ов (при деградации vpc —
// 5000×5s дедлайна на одной удерживаемой горутине).
//
// Наблюдаемое, которое локает тест: (a) код+точный текст отказа; (b) счётчик
// вызовов peer-двойника == 0 — доказывает, что cap срабатывает ДО фазы
// peer-validate, а не «после первых 50».
func TestLoadBalancer_SecurityGroupIds_CardinalityCap(t *testing.T) {
	t.Parallel()

	t.Run("Create over the cap → InvalidArgument before any peer call", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{}}
		uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
		_, err := uc.Execute(context.Background(),
			baseInternalSGReq(sgIDs(domain.MaxSecurityGroupsPerLB+1)...))
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, sgFieldViolations(err), "security_group_ids: too many security groups (max 50)")
		require.Zero(t, sg.calls,
			"the cap must reject before the peer-validate phase — no vpc round-trip may be spent")
	})

	t.Run("Create at the cap → accepted, exactly N peer calls", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		byID := map[string]*vpcclient.SecurityGroup{}
		ids := sgIDs(domain.MaxSecurityGroupsPerLB)
		for _, id := range ids {
			byID[id] = &vpcclient.SecurityGroup{ID: id, ProjectID: "prj-a"}
		}
		sg := &fakeSecurityGroupClient{byID: byID}
		uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
		op, err := uc.Execute(context.Background(), baseInternalSGReq(ids...))
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, domain.MaxSecurityGroupsPerLB, sg.calls,
			"the worst case must stay bounded by the cap")
	})

	t.Run("Update over the cap → InvalidArgument before any peer call", func(t *testing.T) {
		t.Parallel()
		repo := newFakeRepo()
		lbID := seedInternalLB(t, repo, "prj-a", "edge-sg")
		opsRepo := newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{}}
		uc := NewUpdateLoadBalancerUseCase(repo, opsRepo, &fakeZoneClient{}, slog.Default()).
			WithSecurityGroupClient(sg)
		_, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
			NetworkLoadBalancerId: lbID,
			SecurityGroupIds:      sgIDs(domain.MaxSecurityGroupsPerLB + 1),
			UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"security_group_ids"}},
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Contains(t, sgFieldViolations(err), "security_group_ids: too many security groups (max 50)")
		require.Zero(t, sg.calls,
			"the cap must reject before the peer-validate phase — no vpc round-trip may be spent")
	})
}

// TestLoadBalancer_SecurityGroupIds_DedupOnIntake — security_group_ids —
// МНОЖЕСТВО ссылок; дубликаты не несут семантики, но стоят полного peer-Get
// каждый. Набор нормализуется на intake (dedup + отбрасывание пустых, стабильный
// порядок) — тем же приёмом, что disabled_announce_zones (`normalizeZones`),
// поэтому дедуп попадает и в peer-фазу, и в персист, и в Equal (noop-detection).
func TestLoadBalancer_SecurityGroupIds_DedupOnIntake(t *testing.T) {
	t.Parallel()

	t.Run("Create dedups → one peer call per distinct id, deduped persist", func(t *testing.T) {
		t.Parallel()
		repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{
			"sg-0k4m7t2y9u1i3o5p": {ID: "sg-0k4m7t2y9u1i3o5p", ProjectID: "prj-a"},
			"sg-1a2b3c4d5e6f7g8h": {ID: "sg-1a2b3c4d5e6f7g8h", ProjectID: "prj-a"},
		}}
		uc := newCreateUC(repo, opsRepo, createDeps{sg: sg})
		req := baseInternalSGReq(
			"sg-0k4m7t2y9u1i3o5p", "sg-0k4m7t2y9u1i3o5p", "sg-1a2b3c4d5e6f7g8h",
			"sg-0k4m7t2y9u1i3o5p", "", "sg-1a2b3c4d5e6f7g8h",
		)
		op, err := uc.Execute(context.Background(), req)
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, 2, sg.calls, "each DISTINCT security group costs exactly one peer-Get")
		require.Equal(t,
			[]string{"sg-0k4m7t2y9u1i3o5p", "sg-1a2b3c4d5e6f7g8h"},
			lbByName(t, repo, "lb-1").SecurityGroupIDs,
			"the persisted set is deduped in first-seen order (empties dropped)")
	})

	t.Run("Update dedups → one peer call per distinct id", func(t *testing.T) {
		t.Parallel()
		repo := newFakeRepo()
		lbID := seedInternalLB(t, repo, "prj-a", "edge-sg-upd")
		opsRepo := newFakeOpsRepo()
		sg := &fakeSecurityGroupClient{byID: map[string]*vpcclient.SecurityGroup{
			"sg-0k4m7t2y9u1i3o5p": {ID: "sg-0k4m7t2y9u1i3o5p", ProjectID: "prj-a"},
		}}
		uc := NewUpdateLoadBalancerUseCase(repo, opsRepo, &fakeZoneClient{}, slog.Default()).
			WithSecurityGroupClient(sg)
		op, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
			NetworkLoadBalancerId: lbID,
			SecurityGroupIds: []string{
				"sg-0k4m7t2y9u1i3o5p", "sg-0k4m7t2y9u1i3o5p", "sg-0k4m7t2y9u1i3o5p",
			},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"security_group_ids"}},
		})
		require.NoError(t, err)
		require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		require.Equal(t, 1, sg.calls, "each DISTINCT security group costs exactly one peer-Get")
		require.Equal(t, []string{"sg-0k4m7t2y9u1i3o5p"}, repo.lbs[lbID].SecurityGroupIDs)
	})
}
