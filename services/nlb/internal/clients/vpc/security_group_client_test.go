// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// fakeSGService — minimal SecurityGroupServiceClient stub: only Get is meaningful;
// the other methods are never called by SecurityGroupClient (nil returns).
type fakeSGService struct {
	resp *vpcpb.SecurityGroup
	err  error
}

func (f *fakeSGService) Get(_ context.Context, _ *vpcpb.GetSecurityGroupRequest, _ ...grpc.CallOption) (*vpcpb.SecurityGroup, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}
func (f *fakeSGService) List(context.Context, *vpcpb.ListSecurityGroupsRequest, ...grpc.CallOption) (*vpcpb.ListSecurityGroupsResponse, error) {
	return nil, nil
}
func (f *fakeSGService) Create(context.Context, *vpcpb.CreateSecurityGroupRequest, ...grpc.CallOption) (*operationpb.Operation, error) {
	return nil, nil
}
func (f *fakeSGService) Update(context.Context, *vpcpb.UpdateSecurityGroupRequest, ...grpc.CallOption) (*operationpb.Operation, error) {
	return nil, nil
}
func (f *fakeSGService) UpdateRules(context.Context, *vpcpb.UpdateSecurityGroupRulesRequest, ...grpc.CallOption) (*operationpb.Operation, error) {
	return nil, nil
}
func (f *fakeSGService) UpdateRule(context.Context, *vpcpb.UpdateSecurityGroupRuleRequest, ...grpc.CallOption) (*operationpb.Operation, error) {
	return nil, nil
}
func (f *fakeSGService) Delete(context.Context, *vpcpb.DeleteSecurityGroupRequest, ...grpc.CallOption) (*operationpb.Operation, error) {
	return nil, nil
}
func (f *fakeSGService) ListOperations(context.Context, *vpcpb.ListSecurityGroupOperationsRequest, ...grpc.CallOption) (*vpcpb.ListSecurityGroupOperationsResponse, error) {
	return nil, nil
}

func TestSecurityGroupClient_Get_Happy(t *testing.T) {
	c := NewSecurityGroupClientFromStub(&fakeSGService{resp: &vpcpb.SecurityGroup{Id: "sg-1", ProjectId: "prj-1"}})
	sg, err := c.Get(context.Background(), "sg-1")
	require.NoError(t, err)
	require.Equal(t, "sg-1", sg.ID)
	require.Equal(t, "prj-1", sg.ProjectID)
}

func TestSecurityGroupClient_Get_ErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		code    codes.Code
		wantIs  error
		wantErr bool
	}{
		// NB: codes.Unavailable is retryable (retry.OnUnavailable) → exhausts to a
		// context deadline; the fail-closed Unavailable mapping is locked at the
		// use-case level (TestLoadBalancer_NLB_1_52 vpc-unavailable). Here we lock the
		// non-retryable anti-oracle mappings.
		{"NotFound → FailedPrecondition", codes.NotFound, domain.ErrFailedPrecondition, true},
		{"PermissionDenied → FailedPrecondition (anti-oracle)", codes.PermissionDenied, domain.ErrFailedPrecondition, true},
		{"InvalidArgument → ErrInvalidArg", codes.InvalidArgument, domain.ErrInvalidArg, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewSecurityGroupClientFromStub(&fakeSGService{err: status.Error(tc.code, "boom")})
			_, err := c.Get(context.Background(), "sg-1")
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.wantIs), "got %v", err)
		})
	}
}

func TestSecurityGroupClient_Get_EmptyID(t *testing.T) {
	c := NewSecurityGroupClientFromStub(&fakeSGService{})
	_, err := c.Get(context.Background(), "")
	require.True(t, errors.Is(err, domain.ErrInvalidArg))
}
