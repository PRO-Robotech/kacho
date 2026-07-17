// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// IAMCheckClient — gRPC adapter, реализующий port `authz.CheckClient`
// поверх `kacho-iam.InternalIAMService.Check`.
type IAMCheckClient struct {
	cli iamv1.InternalIAMServiceClient
}

// NewIAMCheckClient создаёт adapter. conn — `*grpc.ClientConn`/`ClientConnInterface`
// к internal-port'у kacho-iam (обычно `kacho-iam.kacho.svc.cluster.local:9091`).
func NewIAMCheckClient(conn grpc.ClientConnInterface) *IAMCheckClient {
	return &IAMCheckClient{cli: iamv1.NewInternalIAMServiceClient(conn)}
}

// Check вызывает `InternalIAMService.Check`.
//
// outgoing ctx обёрнут `auth.PropagateOutgoing`, чтобы iam-side
// `grpcsrv.UnaryPrincipalExtract` увидел реального caller'а, а не SystemPrincipal()
// = user:bootstrap.
func (c *IAMCheckClient) Check(ctx context.Context, subjectID, relation, object string) (bool, error) {
	return c.check(ctx, subjectID, relation, object, iamv1.CheckRequest_CONSISTENCY_UNSPECIFIED)
}

// CheckConsistent — Check forcing OpenFGA HIGHER_CONSISTENCY (strong
// read-after-write). Used by the owner-tuple confirm-gate probe (Instance/Disk): the
// tuple was written synchronously to the same OpenFGA store on the create path, so
// under the multi-replica deployment the probe must not read a stale-replica
// negative (the confirm-op tail, Koren-1).
func (c *IAMCheckClient) CheckConsistent(ctx context.Context, subjectID, relation, object string) (bool, error) {
	return c.check(ctx, subjectID, relation, object, iamv1.CheckRequest_HIGHER_CONSISTENCY)
}

func (c *IAMCheckClient) check(ctx context.Context, subjectID, relation, object string, consistency iamv1.CheckRequest_Consistency) (bool, error) {
	resp, err := c.cli.Check(auth.PropagateOutgoing(ctx), &iamv1.CheckRequest{
		SubjectId:   subjectID,
		Relation:    relation,
		Object:      object,
		Consistency: consistency,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

var _ authz.CheckClient = (*IAMCheckClient)(nil)
