// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
)

// IAMClient — клиент ребра storage→iam (валидация project_id через
// ProjectService.Get, fail-closed). Реализует и volume.IAMClient, и
// snapshot.IAMClient (идентичная сигнатура EnsureProjectExists).
type IAMClient struct {
	cli iamv1.ProjectServiceClient
}

// NewIAMClient создаёт IAMClient поверх готового *grpc.ClientConn к kacho-iam.
// conn может быть nil в dev-скелете — тогда fail-closed Unavailable.
func NewIAMClient(conn *grpc.ClientConn) *IAMClient {
	c := &IAMClient{}
	if conn != nil {
		c.cli = iamv1.NewProjectServiceClient(conn)
	}
	return c
}

// EnsureProjectExists валидирует project_id через kacho-iam (ProjectService.Get) на
// request-path Create. Несуществующий проект → FailedPrecondition "Project <id> not
// found" (конвенция *→iam). Peer недоступен → Unavailable (fail-closed для мутации).
func (c *IAMClient) EnsureProjectExists(ctx context.Context, projectID string) error {
	if c.cli == nil {
		return status.Error(codes.Unavailable, "storage→iam ProjectService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	if _, err := c.cli.Get(auth.PropagateOutgoing(cctx), &iamv1.GetProjectRequest{ProjectId: projectID}); err != nil {
		switch status.Code(err) {
		case codes.NotFound, codes.InvalidArgument:
			return fmt.Errorf("%w: Project %s not found", ports.ErrFailedPrecondition, projectID)
		default:
			return status.Error(codes.Unavailable, "iam project validation unavailable")
		}
	}
	return nil
}

var (
	_ volume.IAMClient   = (*IAMClient)(nil)
	_ snapshot.IAMClient = (*IAMClient)(nil)
	_ image.IAMClient    = (*IAMClient)(nil)
)
