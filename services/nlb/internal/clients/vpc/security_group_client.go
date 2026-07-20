// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// DefaultSecurityGroupGetTimeout — per-call deadline for SecurityGroupService.Get
// when the client is built without an explicit timeout (mirrors
// DefaultSubnetGetTimeout; architecture.md "per-call deadline на КАЖДОМ вызове").
const DefaultSecurityGroupGetTimeout = 5 * time.Second

// SecurityGroup — projection of kacho-vpc.SecurityGroup limited to the fields nlb
// needs to peer-validate NetworkLoadBalancer.security_group_ids (existence +
// same-project ownership; NLB-1b MIGRATE F2).
type SecurityGroup struct {
	ID        string
	ProjectID string
}

// SecurityGroupClient — port for the service layer. Error semantics:
//   - vpc NotFound / PermissionDenied → domain.ErrFailedPrecondition (peer-validate
//     lane: the referenced SG does not exist or is not accessible; anti-oracle — no
//     authz leak).
//   - Unavailable / DeadlineExceeded  → domain.ErrUnavailable (fail-closed mutation).
//   - InvalidArgument                 → domain.ErrInvalidArg.
type SecurityGroupClient interface {
	Get(ctx context.Context, securityGroupID string) (*SecurityGroup, error)
}

// securityGroupClient — gRPC implementation of SecurityGroupClient.
type securityGroupClient struct {
	cli     vpcpb.SecurityGroupServiceClient
	timeout time.Duration
}

// NewSecurityGroupClient wraps a grpc conn (clients.Build) in a typed adapter with
// the default per-call timeout. nil conn → nil client (peer-validate skipped).
func NewSecurityGroupClient(conn grpc.ClientConnInterface) SecurityGroupClient {
	return NewSecurityGroupClientWithTimeout(conn, DefaultSecurityGroupGetTimeout)
}

// NewSecurityGroupClientWithTimeout — as NewSecurityGroupClient with an explicit
// per-call timeout. timeout<=0 → DefaultSecurityGroupGetTimeout.
func NewSecurityGroupClientWithTimeout(conn grpc.ClientConnInterface, timeout time.Duration) SecurityGroupClient {
	if conn == nil {
		return nil
	}
	return &securityGroupClient{cli: vpcpb.NewSecurityGroupServiceClient(conn), timeout: resolveSGTimeout(timeout)}
}

// NewSecurityGroupClientFromStub — test constructor taking a stub.
func NewSecurityGroupClientFromStub(cli vpcpb.SecurityGroupServiceClient) SecurityGroupClient {
	if cli == nil {
		return nil
	}
	return &securityGroupClient{cli: cli, timeout: resolveSGTimeout(DefaultSecurityGroupGetTimeout)}
}

func resolveSGTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultSecurityGroupGetTimeout
	}
	return d
}

// Get — см. контракт SecurityGroupClient.Get.
func (c *securityGroupClient) Get(ctx context.Context, securityGroupID string) (*SecurityGroup, error) {
	if securityGroupID == "" {
		return nil, fmt.Errorf("%w: security_group_id is empty", domain.ErrInvalidArg)
	}
	ctx = auth.PropagateOutgoing(ctx)

	// Per-call deadline bounds the ENTIRE retry.OnUnavailable operation independent of
	// the caller's ctx (architecture.md).
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var resp *vpcpb.SecurityGroup
	if err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		var rerr error
		resp, rerr = c.cli.Get(ctx, &vpcpb.GetSecurityGroupRequest{SecurityGroupId: securityGroupID})
		return rerr
	}); err != nil {
		return nil, mapSecurityGroupErr(securityGroupID, err)
	}
	return &SecurityGroup{ID: resp.GetId(), ProjectID: resp.GetProjectId()}, nil
}

// mapSecurityGroupErr — vpc gRPC-status → nlb domain-sentinel (peer-validate lane).
func mapSecurityGroupErr(sgID string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("vpc security group get %q: %w", sgID, err)
	}
	switch st.Code() {
	case codes.NotFound, codes.PermissionDenied:
		// anti-oracle: absent or not-accessible → same generic precondition failure.
		return fmt.Errorf("%w: SecurityGroup %s not found", domain.ErrFailedPrecondition, sgID)
	case codes.Unavailable, codes.DeadlineExceeded:
		return fmt.Errorf("%w: vpc security group %s: %s", domain.ErrUnavailable, sgID, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: vpc security group %s: %s", domain.ErrInvalidArg, sgID, st.Message())
	default:
		return fmt.Errorf("vpc security group get %q: %w", sgID, err)
	}
}
