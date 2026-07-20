// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

func newMachineTypeSvc(t *testing.T) (*MachineTypeService, *portmock.MachineTypeRepo, *portmock.OpsRepo) {
	t.Helper()
	repo := portmock.NewMachineTypeRepo()
	ops := portmock.NewOpsRepo()
	return NewMachineTypeService(repo, ops), repo, ops
}

func machineTypeFromOp(t *testing.T, op *operations.Operation) *computev1.MachineType {
	t.Helper()
	require.NotNil(t, op.Response, "operation response is nil; error=%v", op.Error)
	var mt computev1.MachineType
	require.NoError(t, op.Response.UnmarshalTo(&mt))
	return &mt
}

func validCreateReq() CreateMachineTypeReq {
	return CreateMachineTypeReq{
		Name:               "std-v3-2",
		Family:             domain.MachineTypeFamilyStandard,
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
	}
}

// TestMachineType_COMP_1_18_CreateGet — F7 COMP-1-18: Create seeds a catalog entry;
// the op response + Get expose the flat public projection with a canonical mt- id
// and mirrored effectiveResources (memory in MiB).
func TestMachineType_COMP_1_18_CreateGet(t *testing.T) {
	svc, _, ops := newMachineTypeSvc(t)
	op, err := svc.Create(context.Background(), validCreateReq())
	require.NoError(t, err)
	require.NotEmpty(t, op.Metadata)

	done := portmock.AwaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	mt := machineTypeFromOp(t, done)
	require.True(t, len(mt.Id) > 3 && mt.Id[:3] == "mt-", "canonical mt- id, got %q", mt.Id)
	require.Equal(t, "std-v3-2", mt.Name)
	require.Equal(t, computev1.MachineType_STANDARD, mt.Family)
	require.Equal(t, int32(2), mt.EffectiveResources.VCpu)
	require.Equal(t, int64(8192), mt.EffectiveResources.MemoryMib)
	require.Equal(t, computev1.MachineType_AVAILABLE, mt.Status, "status defaults to AVAILABLE")

	got, err := svc.Get(context.Background(), mt.Id)
	require.NoError(t, err)
	require.Equal(t, "std-v3-2", got.Name)
}

// TestMachineType_COMP_1_07_CreateValidation — F2/F7 sync validation: family
// required (UNSPECIFIED rejected), name required, effectiveResources positive.
func TestMachineType_COMP_1_07_CreateValidation(t *testing.T) {
	svc, _, _ := newMachineTypeSvc(t)
	ctx := context.Background()

	// family UNSPECIFIED → INVALID_ARGUMENT.
	r := validCreateReq()
	r.Family = domain.MachineTypeFamilyUnspecified
	_, err := svc.Create(ctx, r)
	require.Equal(t, codes.InvalidArgument, status.Code(err), "family required")

	// empty name → INVALID_ARGUMENT.
	r = validCreateReq()
	r.Name = ""
	_, err = svc.Create(ctx, r)
	require.Equal(t, codes.InvalidArgument, status.Code(err), "name required")

	// zero vCpu → INVALID_ARGUMENT.
	r = validCreateReq()
	r.EffectiveResources.VCPU = 0
	_, err = svc.Create(ctx, r)
	require.Equal(t, codes.InvalidArgument, status.Code(err), "v_cpu must be > 0")

	// zero memory → INVALID_ARGUMENT.
	r = validCreateReq()
	r.EffectiveResources.MemoryMiB = 0
	_, err = svc.Create(ctx, r)
	require.Equal(t, codes.InvalidArgument, status.Code(err), "memory_mib must be > 0")
}

// TestMachineType_COMP_1_30_DuplicateName — UNIQUE(name): a second Create with the
// same name lands ALREADY_EXISTS in the op error (DB-backstop, 23505).
func TestMachineType_DuplicateName_AlreadyExists(t *testing.T) {
	svc, _, ops := newMachineTypeSvc(t)
	ctx := context.Background()
	op1, err := svc.Create(ctx, validCreateReq())
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, ops, op1.ID).Error)

	op2, err := svc.Create(ctx, validCreateReq())
	require.NoError(t, err) // sync ok — collision surfaces async
	done2 := portmock.AwaitOpDone(t, ops, op2.ID)
	require.NotNil(t, done2.Error)
	require.Equal(t, int32(codes.AlreadyExists), done2.Error.Code)
}

// TestMachineType_COMP_1_20_GetMalformedID — F7/F8 COMP-1-20: malformed id →
// sync INVALID_ARGUMENT first-statement; well-formed-but-absent → NOT_FOUND.
func TestMachineType_COMP_1_20_GetMalformedID(t *testing.T) {
	svc, _, _ := newMachineTypeSvc(t)
	ctx := context.Background()

	_, err := svc.Get(ctx, "bad!!id")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "invalid machine type id 'bad!!id'")

	_, err = svc.Get(ctx, "mt-7k3q9x2m4n8p1r5t")
	require.Equal(t, codes.NotFound, status.Code(err), "well-formed-but-absent → NOT_FOUND")
}

// TestMachineType_COMP_1_19_ListFilter — F7 COMP-1-19: family=/minGpus= filters.
func TestMachineType_COMP_1_19_ListFilter(t *testing.T) {
	svc, repo, _ := newMachineTypeSvc(t)
	repo.Seed(&domain.MachineType{ID: "mt-std", Name: "std-v3-2", Family: domain.MachineTypeFamilyStandard})
	repo.Seed(&domain.MachineType{ID: "mt-g1", Name: "gpu-a100-1", Family: domain.MachineTypeFamilyGPU, EffectiveResources: domain.EffectiveResources{GPUs: 1}})
	repo.Seed(&domain.MachineType{ID: "mt-g8", Name: "gpu-a100-8", Family: domain.MachineTypeFamilyGPU, EffectiveResources: domain.EffectiveResources{GPUs: 8}})
	ctx := context.Background()

	gpus, _, err := svc.List(ctx, MachineTypeFilter{Family: domain.MachineTypeFamilyGPU}, Pagination{})
	require.NoError(t, err)
	require.Len(t, gpus, 2, "family=GPU excludes STANDARD")

	big, _, err := svc.List(ctx, MachineTypeFilter{Family: domain.MachineTypeFamilyGPU, MinGPUs: 4}, Pagination{})
	require.NoError(t, err)
	require.Len(t, big, 1)
	require.Equal(t, "gpu-a100-8", big[0].Name, "minGpus=4 keeps only gpus>=4")
}

// TestMachineType_COMP_1_26_UpdateImmutableUnknownMask — F10-style: immutable name
// (before UpdateMask) and unknown mask field both → INVALID_ARGUMENT.
func TestMachineType_UpdateImmutableUnknownMask(t *testing.T) {
	svc, repo, _ := newMachineTypeSvc(t)
	repo.Seed(&domain.MachineType{ID: "mt-x", Name: "std-v3-2", Family: domain.MachineTypeFamilyStandard, Status: domain.MachineTypeStatusAvailable})
	ctx := context.Background()

	_, err := svc.Update(ctx, UpdateMachineTypeReq{ID: "mt-x", UpdateMask: []string{"name"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "immutable after MachineType.Create")

	_, err = svc.Update(ctx, UpdateMachineTypeReq{ID: "mt-x", UpdateMask: []string{"nonsense"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err), "unknown mask field rejected")
}

// TestMachineType_UpdateApplies — a masked mutable field is applied via the worker.
func TestMachineType_UpdateApplies(t *testing.T) {
	svc, repo, ops := newMachineTypeSvc(t)
	repo.Seed(&domain.MachineType{ID: "mt-x", Name: "std-v3-2", Family: domain.MachineTypeFamilyStandard, Status: domain.MachineTypeStatusAvailable})
	ctx := context.Background()

	op, err := svc.Update(ctx, UpdateMachineTypeReq{ID: "mt-x", Status: domain.MachineTypeStatusDeprecated, UpdateMask: []string{"status"}})
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, ops, op.ID)
	require.Nil(t, done.Error)
	mt := machineTypeFromOp(t, done)
	require.Equal(t, computev1.MachineType_DEPRECATED, mt.Status)
}

// TestMachineType_COMP_1_21_DeleteThenGet — Delete → op done → Get NOT_FOUND.
func TestMachineType_COMP_1_21_DeleteThenGet(t *testing.T) {
	svc, _, ops := newMachineTypeSvc(t)
	ctx := context.Background()
	cop, err := svc.Create(ctx, validCreateReq())
	require.NoError(t, err)
	mt := machineTypeFromOp(t, portmock.AwaitOpDone(t, ops, cop.ID))

	dop, err := svc.Delete(ctx, mt.Id)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, ops, dop.ID).Error)

	_, err = svc.Get(ctx, mt.Id)
	require.Equal(t, codes.NotFound, status.Code(err))
}
