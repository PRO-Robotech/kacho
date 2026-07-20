// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// instSvcKit — фикстура use-case InstanceService со всеми fake-портами. machineTypes
// каталог засеян std/gpu/deprecated/retired flavor'ами (COMP-1 F2/F7 резолв).
type instSvcKit struct {
	svc         *InstanceService
	repo        *portmock.InstanceRepo
	machineType *portmock.MachineTypeRepo
	storage     *portmock.StorageClient
	nic         *portmock.NicClient
	ops         *portmock.OpsRepo
}

const (
	testMTStd        = "mt-std2"
	testMTStdName    = "std-v3-2"
	testMTGpu        = "mt-gpu8"
	testMTDeprecated = "mt-old2"
	testMTRetired    = "mt-gone2"
)

func newInstanceSvc(t *testing.T, folderOK bool) instSvcKit {
	t.Helper()
	instanceRepo := portmock.NewInstanceRepo()
	mtRepo := portmock.NewMachineTypeRepo()
	seedTestMachineTypes(mtRepo)
	storage := portmock.NewStorageClient()
	nic := portmock.NewNicClient()
	ops := portmock.NewOpsRepo()
	svc := NewInstanceService(instanceRepo, mtRepo, portmock.NewZoneRegistry(),
		&portmock.ProjectClient{OK: folderOK}, nic, storage, ops)
	return instSvcKit{svc: svc, repo: instanceRepo, machineType: mtRepo, storage: storage, nic: nic, ops: ops}
}

// seedTestMachineTypes засевает каталог четырьмя flavor'ами (STANDARD/GPU/DEPRECATED/
// RETIRED) — покрывает резолв по slug и по имени, family-gate и status-gate.
func seedTestMachineTypes(r *portmock.MachineTypeRepo) {
	r.Seed(&domain.MachineType{ID: testMTStd, Name: testMTStdName, Family: domain.MachineTypeFamilyStandard,
		Status: domain.MachineTypeStatusAvailable, EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192}})
	r.Seed(&domain.MachineType{ID: testMTGpu, Name: "gpu-a100-8", Family: domain.MachineTypeFamilyGPU,
		Status: domain.MachineTypeStatusAvailable, EffectiveResources: domain.EffectiveResources{VCPU: 96, MemoryMiB: 1146880, GPUs: 8, GPUType: "a100-80g"}})
	r.Seed(&domain.MachineType{ID: testMTDeprecated, Name: "old-v1-2", Family: domain.MachineTypeFamilyStandard,
		Status: domain.MachineTypeStatusDeprecated, EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 4096}})
	r.Seed(&domain.MachineType{ID: testMTRetired, Name: "gone-v0-1", Family: domain.MachineTypeFamilyStandard,
		Status: domain.MachineTypeStatusRetired, EffectiveResources: domain.EffectiveResources{VCPU: 1, MemoryMiB: 2048}})
}

func instanceFromOp(t *testing.T, op *operations.Operation) *computev1.Instance {
	t.Helper()
	require.NotNil(t, op.Response, "operation error=%v", op.Error)
	var in computev1.Instance
	require.NoError(t, op.Response.UnmarshalTo(&in))
	return &in
}

// baseCreateReq — минимальный валидный VM Create-req (kind/sizing/bootSource/net/ssh).
func baseCreateReq() CreateInstanceReq {
	return CreateInstanceReq{
		ProjectID:             "prj-acme",
		Name:                  "vm-1",
		ZoneID:                "ru-central1-a",
		InstanceKind:          domain.InstanceKindVM,
		MachineTypeID:         testMTStd,
		BootSource:            domain.BootSource{Type: bootSourceStorageImage, ID: "img-9k2m4x7q1n8p:22.04-lts"},
		NetworkInterfaceSpecs: []NetworkInterfaceSpec{{SubnetID: "sub-abc", SecurityGroupIDs: []string{"scg-def"}}},
		SSHPublicKeys:         []string{"ssh-ed25519 AAAA ml@team"},
		VMSpec:                &domain.VMSpec{},
	}
}

// baseContainerReq — минимальный валидный CONTAINER Create-req (no ssh needed — guard exempt).
func baseContainerReq() CreateInstanceReq {
	return CreateInstanceReq{
		ProjectID:             "prj-acme",
		Name:                  "job-1",
		ZoneID:                "ru-central1-b",
		InstanceKind:          domain.InstanceKindContainer,
		MachineTypeID:         testMTGpu,
		BootSource:            domain.BootSource{Type: bootSourceRegistryImage, ID: "ml/bert-trainer:cu121"},
		NetworkInterfaceSpecs: []NetworkInterfaceSpec{{SubnetID: "sub-b", SecurityGroupIDs: []string{"scg-b"}}},
		ContainerSpec:         &domain.ContainerSpec{Command: []string{"python", "train.py"}, RestartPolicy: domain.RestartPolicyOnFailure},
	}
}

// COMP-1-01: Create VM → Operation с instanceId (ins-) в metadata сразу; после done —
// durable Instance rest=PROVISIONING, vmSpec присутствует, containerSpec отсутствует.
func TestInstance_COMP_1_01_CreateVM(t *testing.T) {
	k := newInstanceSvc(t, true)
	op, err := k.svc.Create(context.Background(), baseCreateReq())
	require.NoError(t, err)
	// instanceId в metadata до done.
	var meta computev1.CreateInstanceMetadata
	require.NoError(t, op.Metadata.UnmarshalTo(&meta))
	require.True(t, strings.HasPrefix(meta.InstanceId, "ins-"), "instanceId must be ins- prefixed, got %s", meta.InstanceId)

	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.True(t, strings.HasPrefix(in.Id, "ins-"))
	require.Equal(t, computev1.InstanceKind_VM, in.InstanceKind)
	require.Equal(t, computev1.Instance_PROVISIONING, in.Status, "resting status PROVISIONING (durable, materialize=COMP-2)")
	require.NotNil(t, in.GetVmSpec(), "vmSpec present for VM")
	require.Nil(t, in.GetContainerSpec(), "containerSpec absent (oneof)")
	require.Equal(t, testMTStd, in.MachineTypeId)
	require.Equal(t, int32(2), in.EffectiveResources.VCpu)
	require.Equal(t, int64(8192), in.EffectiveResources.MemoryMib)
	require.Equal(t, bootSourceStorageImage, in.BootSource.Type)
	require.Contains(t, in.Fqdn, ".auto.internal")
}

// COMP-1-02: Create CONTAINER → containerSpec present (command/restartPolicy), vmSpec absent.
func TestInstance_COMP_1_02_CreateContainer(t *testing.T) {
	k := newInstanceSvc(t, true)
	op, err := k.svc.Create(context.Background(), baseContainerReq())
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, computev1.InstanceKind_CONTAINER, in.InstanceKind)
	require.NotNil(t, in.GetContainerSpec())
	require.Equal(t, []string{"python", "train.py"}, in.GetContainerSpec().Command)
	require.Equal(t, computev1.RestartPolicy_ON_FAILURE, in.GetContainerSpec().RestartPolicy)
	require.Nil(t, in.GetVmSpec())
	require.Nil(t, in.BootSource.MaterializedVolume, "CONTAINER ephemeral rootfs — no materializedVolume")
}

// COMP-1-03: kind ↔ spec mismatch + missing kind → sync InvalidArgument (spoken XOR).
func TestInstance_COMP_1_03_KindSpecMismatch(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	vmWithCtr := baseCreateReq()
	vmWithCtr.ContainerSpec = &domain.ContainerSpec{}
	_, err := k.svc.Create(ctx, vmWithCtr)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "containerSpec is not allowed when instanceKind is VM")

	ctrWithVM := baseContainerReq()
	ctrWithVM.VMSpec = &domain.VMSpec{}
	_, err = k.svc.Create(ctx, ctrWithVM)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "vmSpec is not allowed when instanceKind is CONTAINER")

	noKind := baseCreateReq()
	noKind.InstanceKind = domain.InstanceKindUnspecified
	_, err = k.svc.Create(ctx, noKind)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "instanceKind is required")
}

// COMP-1-05: machineTypeId (slug) → effectiveResources° mirror; canonical mt- echo.
func TestInstance_COMP_1_05_MachineTypeSlug(t *testing.T) {
	k := newInstanceSvc(t, true)
	req := baseCreateReq()
	req.MachineTypeID = testMTStd
	req.CPUGuaranteePercent = 100
	op, err := k.svc.Create(context.Background(), req)
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, testMTStd, in.MachineTypeId)
	require.Equal(t, int32(2), in.EffectiveResources.VCpu)
	require.Equal(t, int64(8192), in.EffectiveResources.MemoryMib)
	require.Equal(t, int32(0), in.EffectiveResources.Gpus)
	require.Equal(t, int32(100), in.CpuGuaranteePercent)
}

// COMP-1-06: machineTypeId стабильное имя → резолвится, canonical echo всегда mt-slug.
func TestInstance_COMP_1_06_MachineTypeName(t *testing.T) {
	k := newInstanceSvc(t, true)
	req := baseCreateReq()
	req.MachineTypeID = testMTStdName // "std-v3-2"
	op, err := k.svc.Create(context.Background(), req)
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, testMTStd, in.MachineTypeId, "name must resolve to canonical mt- slug")
}

// COMP-1-07: machineTypeId required / unknown / RETIRED → reject.
func TestInstance_COMP_1_07_MachineTypeReject(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	empty := baseCreateReq()
	empty.MachineTypeID = ""
	_, err := k.svc.Create(ctx, empty)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "machineTypeId is required")

	unknown := baseCreateReq()
	unknown.MachineTypeID = "mt-nonexistent"
	op, err := k.svc.Create(ctx, unknown)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, k.ops, op.ID)
	require.NotNil(t, done.Error)
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.Code)
	require.Contains(t, done.Error.Message, "machine type mt-nonexistent not found")

	retired := baseCreateReq()
	retired.MachineTypeID = testMTRetired
	op, err = k.svc.Create(ctx, retired)
	require.NoError(t, err)
	done = portmock.AwaitOpDone(t, k.ops, op.ID)
	require.NotNil(t, done.Error)
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.Code)
}

// COMP-1-08: cpuGuaranteePercent {0..100} family-gated; 101 → InvalidArgument; GPU accepted-and-ignored.
func TestInstance_COMP_1_08_CPUGuarantee(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	burst := baseCreateReq()
	burst.CPUGuaranteePercent = 0
	op, err := k.svc.Create(ctx, burst)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)

	over := baseCreateReq()
	over.CPUGuaranteePercent = 101
	_, err = k.svc.Create(ctx, over)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	gpu := baseContainerReq()
	gpu.MachineTypeID = testMTGpu
	gpu.CPUGuaranteePercent = 50 // accepted-and-ignored for GPU family
	op, err = k.svc.Create(ctx, gpu)
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, int32(8), in.EffectiveResources.Gpus, "GPU count from catalog granularity")
}

// COMP-1-09/10/11: bootSource grammar/type-whitelist/output-field-reject.
func TestInstance_COMP_1_09_BootSourceGrammar(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	// happy echo (storage.image).
	op, err := k.svc.Create(ctx, baseCreateReq())
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, bootSourceStorageImage, in.BootSource.Type)
	require.Equal(t, "img-9k2m4x7q1n8p:22.04-lts", in.BootSource.Id)
	require.Equal(t, computev1.ImageKind_STORAGE_IMAGE, in.BootSource.ImageKind, "server-derived imageKind routes storage")
	require.Empty(t, in.BootSource.ResolvedDigest, "resolvedDigest empty in COMP-1 (resolve=COMP-2)")

	// bare-untagged → 400 with grammar.
	bare := baseCreateReq()
	bare.BootSource = domain.BootSource{Type: bootSourceStorageImage, ID: "img-9k2m4x7q1n8p"}
	_, err = k.svc.Create(ctx, bare)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "needs a tag or digest")

	// unknown type → 400.
	badType := baseCreateReq()
	badType.BootSource = domain.BootSource{Type: "vm.image", ID: "img-x:tag"}
	_, err = k.svc.Create(ctx, badType)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// empty bootSource → 400 "bootSource is required".
	empty := baseCreateReq()
	empty.BootSource = domain.BootSource{}
	_, err = k.svc.Create(ctx, empty)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "bootSource is required")

	// output-only field on input → 400.
	outField := baseCreateReq()
	outField.BootSource = domain.BootSource{Type: bootSourceStorageImage, ID: "img-x:tag", ResolvedDigest: "sha256:deadbeef"}
	_, err = k.svc.Create(ctx, outField)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "output-only")
}

// COMP-1-12: serviceAccountId опционален; эхается как Referrer.
func TestInstance_COMP_1_12_ServiceAccount(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	// без SA → OK, пусто.
	op, err := k.svc.Create(ctx, baseCreateReq())
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Nil(t, in.ServiceAccount, "serviceAccount empty for public image")

	// с SA → Referrer{type:iam.service_account, id}.
	withSA := baseCreateReq()
	withSA.Name = "vm-sa"
	withSA.ServiceAccountID = "sva-4k8m2q9x1n7p3r5t"
	op, err = k.svc.Create(ctx, withSA)
	require.NoError(t, err)
	in = instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.NotNil(t, in.ServiceAccount)
	require.Equal(t, "iam.service_account", in.ServiceAccount.Type)
	require.Equal(t, "sva-4k8m2q9x1n7p3r5t", in.ServiceAccount.Id)
}

// COMP-1-13: malformed SA id → sync InvalidArgument.
func TestInstance_COMP_1_13_ServiceAccountMalformed(t *testing.T) {
	k := newInstanceSvc(t, true)
	req := baseCreateReq()
	req.ServiceAccountID = "not!!a!!sa!!id"
	_, err := k.svc.Create(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// COMP-1-14/15: unreachable-guard (VM без ssh И external → FP; ack снимает; CONTAINER exempt).
func TestInstance_COMP_1_14_UnreachableGuard(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	// VM без ssh, без external, без ack → FailedPrecondition.
	guarded := baseCreateReq()
	guarded.SSHPublicKeys = nil
	guarded.AssignExternalAddress = false
	_, err := k.svc.Create(ctx, guarded)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "unreachable")

	// + acknowledgeUnreachable → OK.
	acked := guarded
	acked.Name = "vm-ack"
	acked.AcknowledgeUnreachable = true
	op, err := k.svc.Create(ctx, acked)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)

	// external вместо ssh → OK.
	ext := baseCreateReq()
	ext.Name = "vm-ext"
	ext.SSHPublicKeys = nil
	ext.AssignExternalAddress = true
	op, err = k.svc.Create(ctx, ext)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)

	// CONTAINER без ssh/external → OK (guard exempt).
	op, err = k.svc.Create(ctx, baseContainerReq())
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)
}

// COMP-1-16: ни networkInterfaceSpecs, ни useDefaultNetwork → FailedPrecondition runbook.
func TestInstance_COMP_1_16_NetRunbook(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	noNet := baseCreateReq()
	noNet.NetworkInterfaceSpecs = nil
	noNet.UseDefaultNetwork = false
	_, err := k.svc.Create(ctx, noNet)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "ru-central1-a")
	require.Contains(t, status.Convert(err).Message(), "useDefaultNetwork")

	// useDefaultNetwork → форма принята.
	def := baseCreateReq()
	def.NetworkInterfaceSpecs = nil
	def.UseDefaultNetwork = true
	op, err := k.svc.Create(ctx, def)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)
}

// COMP-1-17: secondaryVolumeSpecs structural (sizeGiB>0).
func TestInstance_COMP_1_17_SecondaryVolumeSpecs(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	bad := baseCreateReq()
	bad.SecondaryVolumeSpecs = []SecondaryVolumeSpec{{SizeGiB: 0, VolumeTypeID: "vt-ssd", MountPath: "/data"}}
	_, err := k.svc.Create(ctx, bad)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	ok := baseCreateReq()
	ok.SecondaryVolumeSpecs = []SecondaryVolumeSpec{{SizeGiB: 100, VolumeTypeID: "vt-ssd", MountPath: "/data"}}
	op, err := k.svc.Create(ctx, ok)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)
}

func TestInstance_Create_Fqdn_HostnameSuffix(t *testing.T) {
	k := newInstanceSvc(t, true)
	req := baseCreateReq()
	req.Hostname = "web1"
	op, err := k.svc.Create(context.Background(), req)
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, "web1.kacho.internal", in.Fqdn)
	require.NotContains(t, in.Fqdn, "ru-central1", "Fqdn must not leak a foreign-cloud region token")
}

// COMP-1-22 (placementGroupId format): malformed slug → InvalidArgument; well-formed/empty → OK.
func TestInstance_COMP_1_22_PlacementGroupFormat(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	bad := baseCreateReq()
	bad.PlacementGroupID = "not-a-plg!!"
	_, err := k.svc.Create(ctx, bad)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "invalid placement group id")

	ok := baseCreateReq()
	ok.PlacementGroupID = "plg-4k8m2q9x1n7p3r5t"
	op, err := k.svc.Create(ctx, ok)
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, "plg-4k8m2q9x1n7p3r5t", in.PlacementGroupId)
}

func seedInst(repo *portmock.InstanceRepo, id string, st domain.InstanceStatus) *domain.Instance {
	in := &domain.Instance{
		ID: id, ProjectID: "prj-acme", Name: "vm", ZoneID: "ru-central1-a", Status: st,
		InstanceKind: domain.InstanceKindVM, MachineTypeID: testMTStd,
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: bootSourceStorageImage, ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
		FQDN:               id + ".auto.internal",
	}
	repo.Seed(in)
	return in
}

const seedID = "ins-vm1seed000000000"

// seedRunningInstance — compat-хелпер для legacy-RPC тестов (ops/nic): сеет инстанс
// под фиксированным legacy id "epdvm1" в заданном статусе.
func seedRunningInstance(repo *portmock.InstanceRepo, st domain.InstanceStatus) *domain.Instance {
	return seedInst(repo, "epdvm1", st)
}

// COMP-1-25: LIVE-mutable name/labels применяются.
func TestInstance_COMP_1_25_UpdateLive(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
	op, err := k.svc.Update(context.Background(), UpdateInstanceReq{
		InstanceID: seedID, Name: "renamed", Labels: map[string]string{"team": "ml"},
		UpdateMask: []string{"name", "labels"},
	})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, "renamed", in.Name)
	require.Equal(t, "ml", in.Labels["team"])
}

// COMP-1-26: immutable (zoneId/instanceKind) + unknown-mask → InvalidArgument (immutable до UpdateMask).
func TestInstance_COMP_1_26_UpdateImmutable(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
	ctx := context.Background()

	_, err := k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, UpdateMask: []string{"zone_id"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "zoneId is immutable after Instance.Create")

	_, err = k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, UpdateMask: []string{"instance_kind"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "instanceKind is immutable after Instance.Create")

	// unknown / output-only in mask → reject.
	_, err = k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, UpdateMask: []string{"fqdn"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// COMP-1-27: next-boot deferral (ssh/vmSpec) accepted with statusReason; bootSource → Reinstall-only;
// STOPPED-gate (machineTypeId) на не-STOPPED → sync FailedPrecondition (always-reject in COMP-1).
func TestInstance_COMP_1_27_UpdateDeferralAndGate(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
	ctx := context.Background()

	// next-boot deferred: sshPublicKeys → done, statusReason set.
	op, err := k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, SSHPublicKeys: []string{"ssh-ed25519 AAAA"}, UpdateMask: []string{"ssh_public_keys"}})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Contains(t, in.StatusReason, "takes effect on next boot")

	// bootSource → Reinstall-only reject.
	_, err = k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, UpdateMask: []string{"boot_source"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "bootSource cannot be changed via Update; use Reinstall")

	// STOPPED-gated machineTypeId on non-STOPPED → sync FailedPrecondition (always-reject).
	_, err = k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, MachineTypeID: "mt-bigger", UpdateMask: []string{"machine_type_id"}})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "instance must be STOPPED to change sizing or placement")
}

// COMP-1-38 (malformed) / F8: malformed instanceId → InvalidArgument first-statement.
func TestInstance_COMP_1_MalformedID(t *testing.T) {
	k := newInstanceSvc(t, true)
	_, err := k.svc.Get(context.Background(), "not-an-ins-id!!")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "invalid instance id 'not-an-ins-id!!'")
}

// COMP-1-37: Delete → done → Get NOT_FOUND (hard-delete).
func TestInstance_COMP_1_37_DeleteHard(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
	op, err := k.svc.Delete(context.Background(), seedID)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)
	_, err = k.svc.Get(context.Background(), seedID)
	require.Equal(t, codes.NotFound, status.Code(err))
}

// COMP-1-33: zone peer-validate — unknown zone → Operation error.
func TestInstance_COMP_1_33_ZoneReject(t *testing.T) {
	instanceRepo := portmock.NewInstanceRepo()
	mtRepo := portmock.NewMachineTypeRepo()
	seedTestMachineTypes(mtRepo)
	ops := portmock.NewOpsRepo()
	zoneSrc := portmock.NewZoneRegistry("ru-central1-a")
	svc := NewInstanceService(instanceRepo, mtRepo, zoneSrc, &portmock.ProjectClient{OK: true},
		portmock.NewNicClient(), portmock.NewStorageClient(), ops)

	op, err := svc.Create(context.Background(), baseCreateReq())
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, ops, op.ID).Error, "create with known zone must succeed")

	bad := baseCreateReq()
	bad.ZoneID = "no-such-zone"
	op2, err := svc.Create(context.Background(), bad)
	require.NoError(t, err)
	done2 := portmock.AwaitOpDone(t, ops, op2.ID)
	require.NotNil(t, done2.Error)
	require.Equal(t, int32(codes.InvalidArgument), done2.Error.Code)
	require.Contains(t, done2.Error.Message, "no-such-zone")
}

// ---- kept legacy-RPC behaviour (power-ops/attach/metadata/delete-saga/mirror) ----
// These RPCs persist through COMP-1 (redesigned/retired in COMP-2/COMP-4). Adapted to
// the redesigned domain model; the resting Create status is PROVISIONING so power-ops
// use seeded RUNNING/STOPPED instances.

func TestInstance_Legacy_StopStartRestart(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusRunning)

	op, err := k.svc.Stop(context.Background(), seedID)
	require.NoError(t, err)
	require.Equal(t, computev1.Instance_STOPPED, instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID)).Status)

	op, err = k.svc.Stop(context.Background(), seedID)
	require.NoError(t, err)
	require.Equal(t, int32(codes.FailedPrecondition), portmock.AwaitOpDone(t, k.ops, op.ID).Error.Code)

	op, err = k.svc.Start(context.Background(), seedID)
	require.NoError(t, err)
	require.Equal(t, computev1.Instance_RUNNING, instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID)).Status)
}

func TestInstance_Legacy_UpdateLabels_EmitsRegister(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
	op, err := k.svc.Update(context.Background(), UpdateInstanceReq{
		InstanceID: seedID, Labels: map[string]string{"env": "prod"}, UpdateMask: []string{"labels"},
	})
	require.NoError(t, err)
	portmock.AwaitOpDone(t, k.ops, op.ID)
	require.NotNil(t, k.repo.LastUpdateEmitLabels)
	require.True(t, *k.repo.LastUpdateEmitLabels, "labels in mask → emit register intent")
}

func TestInstance_Legacy_AttachDisk_Happy(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusRunning)
	op, err := k.svc.AttachDisk(context.Background(), seedID, AttachDiskReq{VolumeID: "voldata1", DeviceName: "sdb"})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Len(t, in.SecondaryDisks, 1)
	require.Equal(t, "voldata1", in.SecondaryDisks[0].VolumeId)
}

func TestInstance_Legacy_UpdateMetadata(t *testing.T) {
	k := newInstanceSvc(t, true)
	in0 := seedInst(k.repo, seedID, domain.InstanceStatusRunning)
	in0.Metadata = map[string]string{"a": "1", "b": "2"}
	op, err := k.svc.UpdateMetadata(context.Background(), seedID, []string{"a"}, map[string]string{"c": "3"})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.NotContains(t, in.Metadata, "a")
	require.Equal(t, "3", in.Metadata["c"])
}

func TestInstance_Legacy_Delete_ReleasesNicAndVolume(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusRunning)
	_, err := k.storage.Attach(context.Background(), VolumeAttachSpec{VolumeID: "voldata1", InstanceID: seedID})
	require.NoError(t, err)
	_, err = k.nic.Attach(context.Background(), NicAttachSpec{NICID: "nicaaa1", InstanceID: seedID})
	require.NoError(t, err)

	op, err := k.svc.Delete(context.Background(), seedID)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)

	vAtts, _ := k.storage.ListAttachments(context.Background(), []string{seedID})
	require.Empty(t, vAtts)
	nAtts, _ := k.nic.ListByInstance(context.Background(), []string{seedID})
	require.Empty(t, nAtts)
}

func TestInstance_Legacy_Get_VolumeMirror(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusRunning)
	_, err := k.storage.Attach(context.Background(), VolumeAttachSpec{VolumeID: "voldata1", InstanceID: seedID, DeviceName: "sdb"})
	require.NoError(t, err)
	got, err := k.svc.Get(context.Background(), seedID)
	require.NoError(t, err)
	require.Len(t, got.AttachedDisks, 1)
	require.Equal(t, "voldata1", got.AttachedDisks[0].DiskID)
}
