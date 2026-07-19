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

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// newInstanceHandlerForValidation собирает InstanceHandler на fake-портах (без DB/peer)
// — достаточно для проверки СИНХРОННОЙ pre-flight валидации Create (до Operation).
func newInstanceHandlerForValidation(t *testing.T) (*InstanceHandler, *portmock.OpsRepo) {
	t.Helper()
	ops := portmock.NewOpsRepo()
	svc := service.NewInstanceService(
		portmock.NewInstanceRepo(),
		portmock.NewZoneRegistry("ru-central1-a"),
		&portmock.ProjectClient{OK: true},
		portmock.NewNicClient(),
		portmock.NewStorageClient(),
		ops,
	)
	return NewInstanceHandler(svc, nil), ops
}

func validBootDiskSpec() *computev1.AttachedDiskSpec {
	return &computev1.AttachedDiskSpec{Disk: &computev1.AttachedDiskSpec_VolumeId{VolumeId: "volabcdefghijklmno"}}
}

// TestInstanceHandler_Create_SyncValidation_CoresSet — cores вне proto-set
// {2,4,6,...,80} (напр. 3) отвергается СИНХРОННО (InvalidArgument) ДО создания
// Operation (APICONV: enum/range-валидация первым стейтментом RPC). Regression:
// раньше cores=3 проходил как async-op (HTTP 200) вместо sync 400.
func TestInstanceHandler_Create_SyncValidation_CoresSet(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	ctx := context.Background()

	badCores := &computev1.CreateInstanceRequest{
		ProjectId: "f", Name: "vm-oddcores", ZoneId: "ru-central1-a", PlatformId: "standard-v3",
		ResourcesSpec: &computev1.ResourcesSpec{Cores: 3, Memory: 2 << 30, CoreFraction: 100},
		BootDiskSpec:  validBootDiskSpec(),
	}
	_, err := h.Create(ctx, badCores)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"cores=3 (not in proto vCPU set) must be sync InvalidArgument, not an async Operation")

	// A supported even vCPU count from the proto set is accepted (async Operation).
	ok := &computev1.CreateInstanceRequest{
		ProjectId: "f", Name: "vm-evencores", ZoneId: "ru-central1-a", PlatformId: "standard-v3",
		ResourcesSpec: &computev1.ResourcesSpec{Cores: 4, Memory: 2 << 30, CoreFraction: 100},
		BootDiskSpec:  validBootDiskSpec(),
	}
	op, err := h.Create(ctx, ok)
	require.NoError(t, err)
	require.NotEmpty(t, op.Id)
	portmock.AwaitAllOpsDone(t, ops)
}

// TestInstanceHandler_Create_SyncValidation_BootDiskRequired — boot_disk_spec required
// (proto (required)=true): отсутствие → sync InvalidArgument ДО создания Operation.
// Regression: раньше отсутствие boot_disk_spec проходило как async-op (HTTP 200)
// вместо sync 400.
func TestInstanceHandler_Create_SyncValidation_BootDiskRequired(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	ctx := context.Background()

	noBoot := &computev1.CreateInstanceRequest{
		ProjectId: "f", Name: "vm-noboot", ZoneId: "ru-central1-a", PlatformId: "standard-v3",
		ResourcesSpec: &computev1.ResourcesSpec{Cores: 2, Memory: 2 << 30, CoreFraction: 100},
		// BootDiskSpec intentionally omitted.
	}
	_, err := h.Create(ctx, noBoot)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"missing boot_disk_spec must be sync InvalidArgument, not an async Operation")
}
