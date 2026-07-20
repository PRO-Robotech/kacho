// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fakeSecurityGroupClient — in-memory SecurityGroupClient double. byID returns a
// same-project SG; getErr forces an error (peer down / not found).
type fakeSecurityGroupClient struct {
	byID   map[string]*vpcclient.SecurityGroup
	getErr error
}

func (f *fakeSecurityGroupClient) Get(_ context.Context, id string) (*vpcclient.SecurityGroup, error) {
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
