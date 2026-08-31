// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
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

func newInstanceSvc(t *testing.T, projectOK bool) instSvcKit {
	t.Helper()
	return newInstanceSvcWithSubnets(t, projectOK, portmock.NewSubnetRegistry())
}

// newInstanceSvcWithSubnets — тот же харнесс с явным реестром подсетей: зона
// подсети NIC-спеки теперь сверяется с зоной инстанса, поэтому фикстура ОБЯЗАНА
// её называть (регион/зона ниоткуда не выводятся).
func newInstanceSvcWithSubnets(t *testing.T, projectOK bool, subnets *portmock.SubnetRegistry) instSvcKit {
	t.Helper()
	instanceRepo := portmock.NewInstanceRepo()
	mtRepo := portmock.NewMachineTypeRepo()
	seedTestMachineTypes(mtRepo)
	storage := portmock.NewStorageClient()
	nic := portmock.NewNicClient()
	ops := portmock.NewOpsRepo()
	svc := NewInstanceService(instanceRepo, mtRepo, portmock.NewZoneRegistry(), subnets,
		&portmock.ProjectClient{OK: projectOK}, nic, storage, ops)
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
		// Страж достижимости снимается ПРИЗНАНИЕМ (sshPublicKeys больше не приём).
		AcknowledgeUnreachable: true,
		VMSpec:                 &domain.VMSpec{},
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

// COMP-1-02: образ РЕЕСТРА как источник ОС отвергается — своей осью, отдельно
// от вида машины.
//
// Прежняя редакция подавала сюда фикстуру, несущую ОБА признака сразу: вид
// CONTAINER И образ реестра. Она зеленела на отказе по источнику и о виде не
// утверждала ничего — поэтому пара «вид CONTAINER + образ ХРАНИЛИЩА» жила
// незамеченной и создавала машину. Признаки разведены: здесь вид
// законный (VM), отвергает источник; вид проверяется своей пробой выше.
func TestInstance_COMP_1_02_RegistryImageSourceRefused(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	req := baseCreateReq()
	req.BootSource = domain.BootSource{Type: bootSourceRegistryImage, ID: "ml/bert-trainer:cu121"}

	_, err := k.svc.Create(ctx, req)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "registry.image is not accepted yet")
	require.Contains(t, fieldViolations(t, err), "boot_source.type",
		"отказ по ИСТОЧНИКУ называет источник — не вид")

	// (+) та же машина с образом хранилища проходит тем же путём.
	op, err := k.svc.Create(ctx, baseCreateReq())
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)
}

// Вид «контейнер» отвергается ПО ВИДУ, а не по источнику ОС.
//
// ПРЕДМЕТ. Контракт объявляет `InstanceKind.CONTAINER` несоздаваемым и говорит,
// что отказ НАЗЫВАЕТ ПОЛЕ. Названо при этом было другое поле: отказ висел на
// `boot_source.type = registry.image`, а связки «вид ↔ источник ОС» в проверке
// входа не было вовсе. Пара «вид CONTAINER + образ ХРАНИЛИЩА» проходила
// проверку целиком и создавала машину — вид «контейнер» с корневой файловой
// системой из образа диска, то есть ресурс, не описываемый ни одной ветвью
// модели.
//
// ПОЧЕМУ ЭТОГО НЕ ЛОВИЛА СОСЕДНЯЯ ПРОБА (COMP-1-02). Её фикстура несёт ОБА
// признака сразу — и вид, и образ реестра, — поэтому она зеленела на отказе по
// источнику и о виде не утверждала ничего. Здесь признаки РАЗВЕДЕНЫ: вид
// контейнерный, источник — законный образ хранилища, и отказать обязан именно
// вид.
//
// Утверждается ИСХОД ВЫЗОВА (машина не создана и отказ называет `instance_kind`),
// а не форма запроса: проверка формы осталась бы зелёной на любом расположении
// отказа.
func TestInstance_ContainerKindRefusedRegardlessOfBootSource(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	req := baseContainerReq()
	// Законный источник ОС: он сам по себе принимается (см. положительный
	// контроль ниже), поэтому отказать может ТОЛЬКО вид.
	req.BootSource = domain.BootSource{Type: bootSourceStorageImage, ID: "img-9k2m4x7q1n8p:22.04-lts"}

	op, err := k.svc.Create(ctx, req)

	require.Nil(t, op, "отвергнутый Create не возвращает Operation — машина не создана")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, fieldViolations(t, err), "instance_kind",
		"контракт обещает отказ ПО ИМЕНИ ПОЛЯ, и поле это — вид, а не источник ОС")

	// (+) положительный контроль: та же машина с видом VM и тем же образом
	// хранилища создаётся. Без него отрицание зеленело бы на создании,
	// сломанном по любой другой причине.
	op, err = k.svc.Create(ctx, baseCreateReq())
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)
}

// fieldViolations — имена полей из google.rpc.BadRequest отказа. Машиночитаемая
// часть контракта: текст сообщения стабилен, но утверждать надо поле.
func fieldViolations(t *testing.T, err error) []string {
	t.Helper()
	var out []string
	for _, d := range status.Convert(err).Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			out = append(out, v.GetField())
		}
	}
	return out
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

	// Зеркальная ветвь («vmSpec при виде CONTAINER») здесь НЕ утверждается: вид
	// CONTAINER отвергается раньше и по своему имени, поэтому до неё нет
	// достижимого пути. Проба на неё зеленела бы, читая чужой отказ как свой, —
	// а вернётся она вместе с самим видом. Отказ по виду держит
	// TestInstance_ContainerKindRefusedRegardlessOfBootSource.

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

	// Фикстура переведена с контейнерной нагрузки на машину: контейнерный вид
	// теперь отвергается по отсутствию durable-координаты образа, а предмет
	// ЭТОЙ пробы — семейство типоразмера, а не вид нагрузки.
	gpu := baseCreateReq()
	gpu.Name = "vm-gpu"
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

	// Голый идентификатор образа хранилища ПРОХОДИТ, и это исправление, а не
	// послабление. Прежняя проба закрепляла требование тега или дайджеста от
	// каждого идентификатора — требование, неисполнимое by construction: у
	// контракта образа хранилища нет ни поля тега, ни поля дайджеста, то есть
	// форма, которую проверка требовала, владельцем не производится.
	//
	// Образ хранилища адресуется своим неизменяемым идентификатором, и этого
	// достаточно: он неизменяем на всю жизнь ресурса, поэтому повторный запуск
	// через месяц берёт тот же образ без дополнительной фиксации.
	bare := baseCreateReq()
	bare.Name = "vm-bare"
	bare.BootSource = domain.BootSource{Type: bootSourceStorageImage, ID: "img-9k2m4x7q1n8p"}
	opBare, err := k.svc.Create(ctx, bare)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, opBare.ID).Error)

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

// COMP-1-14/15: unreachable-guard (VM без external → FP; ack снимает; CONTAINER exempt).
func TestInstance_COMP_1_14_UnreachableGuard(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	// VM без external, без ack → FailedPrecondition.
	guarded := baseCreateReq()
	guarded.AcknowledgeUnreachable = false
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

	// external вместо признания → OK.
	ext := baseCreateReq()
	ext.Name = "vm-ext"
	ext.AcknowledgeUnreachable = false
	ext.AssignExternalAddress = true
	op, err = k.svc.Create(ctx, ext)
	require.NoError(t, err)
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, op.ID).Error)

	// Здесь стояла четвёртая ветвь: контейнерная нагрузка освобождена от стража
	// достижимости. Она снята вместе со своим предметом — контейнерный вид
	// отвергается раньше, чем страж успевает высказаться, поэтому проба
	// утверждала бы исход, которого на этом пути больше не бывает. Освобождение
	// вернётся вместе с видом нагрузки: у обоих один предикат возврата.

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

// COMP-1-27: next-boot deferral (vmSpec) accepted with statusReason; bootSource → immutable;
// STOPPED-gate (machineTypeId) на не-STOPPED → sync FailedPrecondition (always-reject in COMP-1).
func TestInstance_COMP_1_27_UpdateDeferralAndGate(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
	ctx := context.Background()

	// next-boot deferred: vmSpec → done, statusReason set.
	//
	// sshPublicKeys из этого набора СНЯТ: он ничего не откладывал — ключи не
	// доставлялись вовсе, а метка «вступит в силу при следующей загрузке»
	// подтверждала приём того, чего не будет.
	op, err := k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, VMSpec: &domain.VMSpec{}, UpdateMask: []string{"vm_spec"}})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Contains(t, in.StatusReason, "takes effect on next boot")

	// bootSource → immutable reject (нет операции, которой его меняют).
	_, err = k.svc.Update(ctx, UpdateInstanceReq{InstanceID: seedID, UpdateMask: []string{"boot_source"}})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "bootSource is immutable after Instance.Create")

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
	svc := NewInstanceService(instanceRepo, mtRepo, zoneSrc, portmock.NewSubnetRegistry(), &portmock.ProjectClient{OK: true},
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
	// Полоса peer-validate: зона названа корректно, но у владельца Geography не
	// резолвится — предусловие на ЧУЖОЙ ресурс не выполнено, а не «ввод неверен».
	require.Equal(t, int32(codes.FailedPrecondition), done2.Error.Code)
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
	require.Equal(t, "voldata1", got.AttachedDisks[0].VolumeID)
}

// Пустая маска — full-object PATCH (api-conventions: «mask пустой → применяются все
// mutable-поля»), поэтому проверяться обязаны ТЕ ЖЕ поля, которые PATCH применяет.
// Раньше валидация шла циклом по самой маске, а применение при пустой маске
// подставляло полный список LIVE-mutable полей — значит при пустой маске поля
// применялись НЕВАЛИДИРОВАННЫМИ, и в машину уезжало имя, которое её собственный
// Create отверг бы, а ссылка на служебную учётку принималась в любом виде.
func TestInstance_EmptyMaskValidatesEveryAppliedField(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		req   UpdateInstanceReq
		field string
	}{
		{
			name:  "name",
			req:   UpdateInstanceReq{InstanceID: seedID, Name: "ПлохоеИмя!"},
			field: "name",
		},
		{
			name:  "service_account_id",
			req:   UpdateInstanceReq{InstanceID: seedID, ServiceAccountID: "not-an-id!!"},
			field: "service account",
		},
		{
			name:  "labels",
			req:   UpdateInstanceReq{InstanceID: seedID, Labels: map[string]string{"ПЛОХОЙ КЛЮЧ": "v"}},
			field: "labels",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newInstanceSvc(t, true)
			seeded := seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)
			seededName := seeded.Name

			require.Empty(t, tc.req.UpdateMask, "фикстура обязана быть именно full-object PATCH")
			_, err := k.svc.Update(ctx, tc.req)
			require.Equal(t, codes.InvalidArgument, status.Code(err),
				"full-object PATCH применяет это поле, значит обязан его и проверить")
			require.True(t, rejectedField(t, err, tc.field),
				"отказ обязан называть поле, из-за которого он произошёл; got: %v", err)

			// Наблюдаемое следствие: машина не изменилась. Без этого утверждения тест
			// удовлетворялся бы отказом, наступившим уже ПОСЛЕ записи.
			after, gerr := k.svc.Get(ctx, seedID)
			require.NoError(t, gerr)
			require.Equal(t, seededName, after.Name)
			require.Empty(t, after.ServiceAccountID)
			require.Empty(t, after.Labels)
		})
	}
}

// Обратная сторона: валидный full-object PATCH обязан примениться целиком — иначе
// предыдущий тест удовлетворялся бы валидацией, отвергающей пустую маску как таковую
// (это сломало бы контракт full-object PATCH).
func TestInstance_EmptyMaskAppliesValidFullObject(t *testing.T) {
	k := newInstanceSvc(t, true)
	seedInst(k.repo, seedID, domain.InstanceStatusProvisioning)

	op, err := k.svc.Update(context.Background(), UpdateInstanceReq{
		InstanceID: seedID, Name: "renamed", Description: "full patch",
		Labels: map[string]string{"team": "ml"}, ServiceAccountID: "sva-4k8m2q9x1n7p3r5t",
	})
	require.NoError(t, err)
	in := instanceFromOp(t, portmock.AwaitOpDone(t, k.ops, op.ID))
	require.Equal(t, "renamed", in.Name)
	require.Equal(t, "full patch", in.Description)
	require.Equal(t, "ml", in.Labels["team"])

	after, err := k.svc.Get(context.Background(), seedID)
	require.NoError(t, err)
	require.Equal(t, "sva-4k8m2q9x1n7p3r5t", after.ServiceAccountID)
}

// rejectedField — имя поля, названное отказом так, как его увидит клиент: либо в
// google.rpc.BadRequest.field_violations (structured-форма corevalidate), либо в
// тексте сообщения (форма serviceerr.InvalidArg). Утверждать «отказ назвал поле»
// по одной лишь подстроке в сообщении нельзя — половина валидаторов Kachō кладёт
// имя в details, и сообщение у них дословно «invalid argument».
func rejectedField(t *testing.T, err error, field string) bool {
	t.Helper()
	st := status.Convert(err)
	if strings.Contains(st.Message(), field) {
		return true
	}
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, v := range br.GetFieldViolations() {
			if strings.Contains(v.GetField(), field) || strings.Contains(v.GetDescription(), field) {
				return true
			}
		}
	}
	return false
}

// Отказ по output-only полю bootSource обязан НАЗЫВАТЬ то поле, которое клиент
// прислал, — иначе он не восстанавливает следующий шаг.
//
// Условие отказа включало четыре поля, текст перечислял три: клиент, приславший
// `imageKind`, читал «name/resolvedDigest/materializedVolume are output-only»,
// снимал названные три, которых он не слал, и получал тот же отказ снова.
//
// Проба утверждает СООБЩЕНИЕ, а не только код: по коду она зеленела бы и на
// прежнем тексте. Рядом — положительный контроль: без output-only полей тот же
// запрос проходит, иначе «отвергнуто» было бы верно и на страже, отвергающем всё.
func TestInstance_BootSourceRefusalNamesTheFieldTheCallerSet(t *testing.T) {
	k := newInstanceSvc(t, true)
	ctx := context.Background()

	// Положительный контроль: тот же bootSource БЕЗ output-only полей проходит.
	okOp, err := k.svc.Create(ctx, baseCreateReq())
	require.NoError(t, err, "положительный контроль: законный bootSource обязан проходить")
	require.Nil(t, portmock.AwaitOpDone(t, k.ops, okOp.ID).Error)

	// Каждое output-only поле по отдельности: отказ называет ИМЕННО его.
	for _, tc := range []struct {
		field string
		mut   func(*domain.BootSource)
	}{
		{"name", func(bs *domain.BootSource) { bs.Name = "ubuntu-22-04" }},
		{"resolvedDigest", func(bs *domain.BootSource) { bs.ResolvedDigest = "sha256:deadbeef" }},
		{"materializedVolume", func(bs *domain.BootSource) {
			bs.MaterializedVolume = &domain.MaterializedVolume{VolumeID: "vol0am5d8q1w4e7r2t6y"}
		}},
		{"imageKind", func(bs *domain.BootSource) { bs.ImageKind = domain.ImageKindStorageImage }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			req := baseCreateReq()
			req.Name = "vm-" + strings.ToLower(tc.field)
			tc.mut(&req.BootSource)

			_, err := k.svc.Create(ctx, req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			msg := status.Convert(err).Message()
			require.Contains(t, msg, tc.field,
				"отказ обязан назвать поле, которое прислал клиент; получено: %q", msg)
		})
	}
}
