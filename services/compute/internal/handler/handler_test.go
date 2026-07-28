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

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

func awaitOps(t *testing.T, ops *portmock.OpsRepo) { t.Helper(); portmock.AwaitAllOpsDone(t, ops) }

func TestCatalogHandler_ReadOnly(t *testing.T) {
	// Region/Zone serving removed — Geography is owned by kacho-geo.
	// DiskType remains a compute-owned read-only catalog.
	dtSvc := service.NewDiskTypeService(portmock.NewDiskTypeRepo("network-ssd"))
	dh := NewDiskTypeHandler(dtSvc)
	ctx := context.Background()

	dt, err := dh.Get(ctx, &computev1.GetDiskTypeRequest{DiskTypeId: "network-ssd"})
	require.NoError(t, err)
	require.Equal(t, "network-ssd", dt.Id)
	dts, err := dh.List(ctx, &computev1.ListDiskTypesRequest{})
	require.NoError(t, err)
	require.Len(t, dts.DiskTypes, 1)
}

func TestInternalCatalogHandler_AdminCRUD(t *testing.T) {
	dtSvc := service.NewDiskTypeService(portmock.NewDiskTypeRepo("network-ssd"))
	h := NewInternalDiskTypeHandler(dtSvc)
	ctx := context.Background()
	created, err := h.Create(ctx, &computev1.CreateDiskTypeRequest{Id: "network-ssd-io-m3", Description: "io-m3"})
	require.NoError(t, err)
	require.Equal(t, "io-m3", created.Description)
	_, err = h.Delete(ctx, &computev1.DeleteDiskTypeRequest{DiskTypeId: "network-ssd-io-m3"})
	require.NoError(t, err)
}

func TestOperationHandler(t *testing.T) {
	ops := portmock.NewOpsRepo()
	h := NewOperationHandler(ops)
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
