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

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// newInstanceHandlerForValidation собирает InstanceHandler на fake-портах (без DB/peer)
// — достаточно для проверки СИНХРОННОЙ pre-flight валидации Create (до Operation).
// Каталог засеян mt-std2, чтобы валидный Create резолвил sizing в doCreate.
func newInstanceHandlerForValidation(t *testing.T) (*InstanceHandler, *portmock.OpsRepo) {
	t.Helper()
	ops := portmock.NewOpsRepo()
	mtRepo := portmock.NewMachineTypeRepo()
	mtRepo.Seed(&domain.MachineType{
		ID: "mt-std2", Name: "std2", Family: domain.MachineTypeFamilyStandard,
		Status:             domain.MachineTypeStatusAvailable,
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
	})
	svc := service.NewInstanceService(
		portmock.NewInstanceRepo(),
		mtRepo,
		portmock.NewZoneRegistry("ru-central1-a"),
		&portmock.ProjectClient{OK: true},
		portmock.NewNicClient(),
		portmock.NewStorageClient(),
		ops,
	)
	return NewInstanceHandler(svc, nil), ops
}

// validCreateReq — минимальный ВАЛИДНЫЙ CreateInstanceRequest (COMP-1 redesign, VM):
// kind, sizing (machineTypeId), bootSource grammar, net-spec, unreachable-guard —
// всё удовлетворено. Отдельные негативы мутируют одно поле.
func validCreateReq() *computev1.CreateInstanceRequest {
	return &computev1.CreateInstanceRequest{
		ProjectId:     "f",
		Name:          "vm-1",
		ZoneId:        "ru-central1-a",
		InstanceKind:  computev1.InstanceKind_VM,
		MachineTypeId: "mt-std2",
		BootSource:    &computev1.BootSource{Type: "storage.image", Id: "img-x:22.04"},
		SshPublicKeys: []string{"ssh-ed25519 AAAA user@h"},
		NetworkInterfaceSpecs: []*computev1.NetworkInterfaceSpec{
			{SubnetId: "sub-a", SecurityGroupIds: []string{"scg-a"}},
		},
		Spec: &computev1.CreateInstanceRequest_VmSpec{VmSpec: &computev1.VmSpec{}},
	}
}

// TestInstanceHandler_Create_Valid_Operation — валидный Create проходит sync-
// валидацию и возвращает Operation, worker-fn коммитит ресурс (done без error).
func TestInstanceHandler_Create_Valid_Operation(t *testing.T) {
	h, ops := newInstanceHandlerForValidation(t)
	op, err := h.Create(context.Background(), validCreateReq())
	require.NoError(t, err)
	require.NotEmpty(t, op.Id)
	done := portmock.AwaitOpDone(t, ops, op.Id)
	require.Nil(t, done.Error, "valid Create must succeed: %v", done.Error)
}

// TestInstanceHandler_Create_MissingKind — instanceKind — сильный первый required-
// дискриминатор (F1): отсутствие → sync InvalidArgument ДО создания Operation.
func TestInstanceHandler_Create_MissingKind(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.InstanceKind = computev1.InstanceKind_INSTANCE_KIND_UNSPECIFIED
	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"missing instanceKind must be sync InvalidArgument, not an async Operation")
	require.Contains(t, status.Convert(err).Message(), "instanceKind is required")
}

// TestInstanceHandler_Create_KindSpecMismatch — kind-oneof XOR (F1): VM +
// ContainerSpec → sync InvalidArgument (containerSpec not allowed when kind is VM).
func TestInstanceHandler_Create_KindSpecMismatch(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq() // kind = VM
	req.Spec = &computev1.CreateInstanceRequest_ContainerSpec{ContainerSpec: &computev1.ContainerSpec{}}
	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"kind/spec mismatch (VM + ContainerSpec) must be sync InvalidArgument")
}

// TestInstanceHandler_Create_MissingMachineType — machineTypeId — единственный канал
// sizing (F2): пусто → sync InvalidArgument ДО Operation.
func TestInstanceHandler_Create_MissingMachineType(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.MachineTypeId = ""
	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"empty machineTypeId must be sync InvalidArgument")
	require.Contains(t, status.Convert(err).Message(), "machineTypeId is required")
}

// TestInstanceHandler_Create_BareBootSourceID — bootSource.id без tag/digest (F3
// grammar) → sync InvalidArgument (bare "img-x" двусмыслен без :tag / @digest).
func TestInstanceHandler_Create_BareBootSourceID(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.BootSource = &computev1.BootSource{Type: "storage.image", Id: "img-x"} // no tag/digest
	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"bare bootSource.id (no tag/digest) must be sync InvalidArgument")
}

// TestInstanceHandler_Create_VMUnreachableGuard — VM без ssh И без external-адреса И
// без acknowledgeUnreachable (F5) → sync FailedPrecondition (unreachable-guard).
// Net-spec остаётся валидным (проверка net-spec идёт раньше guard'а).
func TestInstanceHandler_Create_VMUnreachableGuard(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.SshPublicKeys = nil
	req.AssignExternalAddress = false
	req.AcknowledgeUnreachable = false
	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.FailedPrecondition, status.Code(err),
		"VM with no ssh/external/ack must be sync FailedPrecondition (unreachable-guard)")
	require.Contains(t, status.Convert(err).Message(), "unreachable")
}

// TestInstanceHandler_Create_MissingNetwork — ни networkInterfaceSpecs, ни
// useDefaultNetwork (F6) → sync FailedPrecondition (launch net-spec required).
func TestInstanceHandler_Create_MissingNetwork(t *testing.T) {
	h, _ := newInstanceHandlerForValidation(t)
	req := validCreateReq()
	req.NetworkInterfaceSpecs = nil
	req.UseDefaultNetwork = false
	_, err := h.Create(context.Background(), req)
	require.Equal(t, codes.FailedPrecondition, status.Code(err),
		"no NIC specs and no useDefaultNetwork must be sync FailedPrecondition")
	require.Contains(t, status.Convert(err).Message(), "subnet")
}
