// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

func awaitOps(t *testing.T, ops *portmock.OpsRepo) { t.Helper(); portmock.AwaitAllOpsDone(t, ops) }

func TestOperationHandler(t *testing.T) {
	ops := portmock.NewOpsRepo()
	h := operationspb.NewHandler(ops)
	// Owner poll: op principal must match the caller principal in ctx.
	owner := operations.Principal{Type: "user", ID: "usr-A", DisplayName: "test"}
	ctx := operations.WithPrincipal(context.Background(), owner)
	op, err := operations.New("epd", "test op", &computev1.CreateInstanceMetadata{InstanceId: "ins-x"})
	require.NoError(t, err)
	require.NoError(t, ops.CreateWithPrincipal(ctx, op, owner))
	got, err := h.Get(ctx, &operationpb.GetOperationRequest{OperationId: op.ID})
	require.NoError(t, err)
	require.Equal(t, op.ID, got.Id)
	_, err = h.Get(ctx, &operationpb.GetOperationRequest{OperationId: "missing"})
	require.Equal(t, codes.NotFound, status.Code(err))
}
