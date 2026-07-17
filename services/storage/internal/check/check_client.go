// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package check — per-RPC authz-гейт для kacho-storage. Оборачивает authz-интерсептор
// из corelib storage-шной PermissionMap и CheckClient поверх IAM
// (InternalIAMService.Check → OpenFGA/ReBAC). storage — CONSUMER iam-authz (ребро
// storage→iam Check; iam владеет authz). AuthN+AuthZ на ОБОИХ листенерах (:9090 +
// :9091), internal НЕ освобождён (security.md).
package check

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// IAMCheckClient адаптирует kacho-iam.InternalIAMService.Check под authz.CheckClient.
type IAMCheckClient struct {
	cli iamv1.InternalIAMServiceClient
}

// NewIAMCheckClient строит адаптер поверх conn к internal-листенеру kacho-iam (:9091).
func NewIAMCheckClient(conn grpc.ClientConnInterface) *IAMCheckClient {
	return &IAMCheckClient{cli: iamv1.NewInternalIAMServiceClient(conn)}
}

// Check вызывает InternalIAMService.Check. Исходящий ctx оборачивается
// auth.PropagateOutgoing, чтобы на стороне iam principal-extract видел реального
// вызывающего.
func (c *IAMCheckClient) Check(ctx context.Context, subjectID, relation, object string) (bool, error) {
	return c.check(ctx, subjectID, relation, object, iamv1.CheckRequest_CONSISTENCY_UNSPECIFIED)
}

// CheckConsistent — Check forcing OpenFGA HIGHER_CONSISTENCY (strong
// read-after-write). Used by the Volume owner-tuple confirm-gate probe: the tuple
// was written synchronously to the same OpenFGA store on the create path, so under
// the multi-replica deployment the probe must not read a stale-replica negative.
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
