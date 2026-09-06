// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/api/instance"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
)

// InstanceHandler реализует computev1.InstanceServiceServer (тонкий transport-слой).
//
// Восемь методов СНЯТЫ волной 1 вместе с файлом их отказов: семь не несли
// реализации, восьмой (обновление метаданных) реализован, но его предмет —
// свободная карта — снят той же волной. Владельцы возможностей названы в
// контракте; перепись снятых имён — internal/repohygiene/retiredrpcsurface.go.
//
// Привязка и отвязка сетевого интерфейса ОСТАЮТСЯ публичными и реализованы: у
// домена сети глагол привязки живёт только на внутреннем слушателе, публичного
// пути у арендатора нет и не будет — иначе цикл между доменами. Формула, чтобы
// асимметрия не завелась снова: привязка — глагол потребителя, свойства — глагол
// владельца.
type InstanceHandler struct {
	computev1.UnimplementedInstanceServiceServer
	svc        *instance.InstanceService
	listFilter *listnarrow.Narrower
}

// NewInstanceHandler создаёт InstanceHandler. listFilter может быть nil — тогда
// FGA-фильтрация на List отключена (dev/breakglass).
func NewInstanceHandler(s *instance.InstanceService, listFilter *listnarrow.Narrower) *InstanceHandler {
	return &InstanceHandler{svc: s, listFilter: listFilter}
}

// Get возвращает Instance по id.
func (h *InstanceHandler) Get(ctx context.Context, req *computev1.GetInstanceRequest) (*computev1.Instance, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	return protoconv.Instance(in), nil
}

// List возвращает список ВМ в проекте.
//
// Страница читается из БД ПЕРВОЙ, затем per-object фильтруется через
// iam.AuthorizeService.BatchCheck (viewer ∪ v_list) — см. list_filter.go.
// Ничего не видно → пустая страница (NOT 403 — конвенция Kachō для list-empty).
func (h *InstanceHandler) List(ctx context.Context, req *computev1.ListInstancesRequest) (*computev1.ListInstancesResponse, error) {
	// Validate pagination BEFORE anything authz-related (see disk_handler).
	if err := instance.ValidateListPagination(instance.Pagination{PageToken: req.PageToken, PageSize: req.PageSize}); err != nil {
		return nil, err
	}
	ins, nextToken, err := h.svc.List(ctx, instance.InstanceFilter{ProjectID: req.ProjectId, Filter: req.Filter},
		instance.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	visible, err := listnarrow.Page(ctx, h.listFilter,
		authzfilter.ResourceTypeInstance, authzfilter.ActionInstanceRead, ins,
		func(in *domain.Instance) string { return in.ID })
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListInstancesResponse{NextPageToken: nextToken}
	for _, in := range visible {
		resp.Instances = append(resp.Instances, protoconv.Instance(in))
	}
	return resp, nil
}

// Create инициирует создание Instance (COMP-1 redesign).
func (h *InstanceHandler) Create(ctx context.Context, req *computev1.CreateInstanceRequest) (*operationpb.Operation, error) {
	// Непринимаемые поля — ПЕРВЫМ стейтментом, до конвертации и до какой-либо
	// другой валидации: они не доезжают до use-case, поэтому отвергнуть их можно
	// только здесь (см. RejectUnsupportedCreateFields).
	if err := RejectUnsupportedCreateFields(req); err != nil {
		return nil, err
	}
	op, err := h.svc.Create(ctx, CreateReqFromProto(req))
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// RejectUnsupportedCreateFields отвергает поля CreateInstanceRequest, за которыми
// в compute нет ни одной подсистемы: `network_settings`, `filesystem_specs`,
// `local_disk_specs`, `maintenance_policy`, `maintenance_grace_period`,
// `serial_port_settings`. Ни одно из них не читается нигде в services/compute:
// раньше они принимались и молча выбрасывались, то есть вызывающий получал успех
// и был уверен, что параметр применён. Решение и разбор по каждому полю (включая
// то, почему они оставлены в контракте, а не сняты) —
// `docs/engineering/architecture/07-known-divergences.md`, раздел 7.1.
//
// Форма отказа: код INVALID_ARGUMENT, сообщение обобщённое, имена полей — в
// google.rpc.BadRequest.field_violations (машиночитаемо; текст сообщения — часть
// стабильного контракта и полей не называет). Сообщаются ВСЕ непринимаемые поля
// запроса сразу, чтобы легаси-клиент узнал о них за один заход.
//
// Граница: proto3 не отличает «поле не прислано» от «прислано нулевое значение»
// на non-optional enum и на пустом repeated, поэтому зацепка — заданное значение
// (непустой список / ненулевой enum / непустое вложенное сообщение).
//
// Живёт в transport-слое намеренно: у use-case-DTO этих полей нет и быть не должно
// (шесть мёртвых полей ради отказа — тот самый vestigial-код), поэтому отвергнуть
// их можно только там, где ещё виден сам proto-запрос.
func RejectUnsupportedCreateFields(req *computev1.CreateInstanceRequest) error {
	b := coreerrors.InvalidArgument()
	n := 0
	add := func(field, desc string) {
		b.AddFieldViolation(field, desc)
		n++
	}
	if req.GetNetworkSettings() != nil {
		add("network_settings", "networkSettings is not supported: compute does not configure network acceleration")
	}
	if len(req.GetFilesystemSpecs()) > 0 {
		add("filesystem_specs", "filesystemSpecs is not supported: compute has no Filesystem domain")
	}
	if len(req.GetLocalDiskSpecs()) > 0 {
		add("local_disk_specs", "localDiskSpecs is not supported: compute does not provision host-local disks")
	}
	if req.GetMaintenancePolicy() != computev1.MaintenancePolicy_MAINTENANCE_POLICY_UNSPECIFIED {
		add("maintenance_policy", "maintenancePolicy is not supported: compute does not schedule host maintenance")
	}
	if req.GetMaintenanceGracePeriod() != nil {
		add("maintenance_grace_period", "maintenanceGracePeriod is not supported: compute does not schedule host maintenance")
	}
	if req.GetSerialPortSettings() != nil {
		add("serial_port_settings", "serialPortSettings is not supported: compute does not configure serial-port access")
	}
	// Четыре поля ВНУТРИ network_interface_specs[], того же класса и найденные тем же
	// обходом. Родитель читается — из него берутся subnetId и securityGroupIds, — а
	// отображение proto → domain (`nicSpecsFromProto`) переносит ровно эти два и молча
	// теряет остальные. Поэтому обход по верхнему уровню их не видел.
	//
	// Почему отказ, а не реализация. Адрес интерфейса выделяет IPAM подсети в vpc, и
	// пути «занять названный адрес при создании машины» в compute нет ни на одном слое:
	// доменная спека интерфейса несёт ровно subnetId и securityGroupIds. Индекс
	// назначает сервер (это же говорит контракт). Существующий NIC подключается
	// отдельным реализованным RPC `AttachNetworkInterface`, а не при создании: приём
	// `nicId` здесь обещал второй путь, которого нет, — и обещал вопреки собственной
	// валидации, требующей subnetId безусловно.
	//
	// Отказ назван ПУТЁМ поля (`network_interface_specs.<поле>`), без индекса элемента:
	// имя поля — часть контракта, а номер элемента к причине отказа не относится.
	for _, nic := range req.GetNetworkInterfaceSpecs() {
		if nic == nil {
			continue
		}
		if nic.GetPrimaryV4AddressSpec() != nil {
			add("network_interface_specs.primary_v4_address_spec",
				"networkInterfaceSpecs[].primaryV4AddressSpec is not supported: the address is "+
					"allocated by the subnet's IPAM, compute cannot pin a requested one")
		}
		if nic.GetPrimaryV6AddressSpec() != nil {
			add("network_interface_specs.primary_v6_address_spec",
				"networkInterfaceSpecs[].primaryV6AddressSpec is not supported: the address is "+
					"allocated by the subnet's IPAM, compute cannot pin a requested one")
		}
		if nic.GetNicId() != "" {
			add("network_interface_specs.nic_id",
				"networkInterfaceSpecs[].nicId is not supported at Create: attach an existing "+
					"NetworkInterface with InstanceService.AttachNetworkInterface instead")
		}
		if nic.GetIndex() != "" {
			add("network_interface_specs.index",
				"networkInterfaceSpecs[].index is not supported: the server assigns interface "+
					"indexes (0,1,2…)")
		}
	}
	// sshPublicKeys — седьмое поле того же класса, найденное обходом дескрипторов.
	// Ключи не персистятся ни в одной колонке и никому не выдаются: подсистемы их
	// доставки в гостя (metadata-сервис / guest-agent) в control-plane нет. Хуже
	// того, поле УДОВЛЕТВОРЯЛО страж достижимости ниже по потоку — «машина будет
	// запущена и недостижима» снималось списком ключей, который никуда не доедет,
	// то есть страж отпускал ровно тот случай, ради которого заведён.
	if len(req.GetSshPublicKeys()) > 0 {
		add("ssh_public_keys",
			"sshPublicKeys is not accepted: a key carried as a field lives exactly as long as the "+
				"instance and can be neither revoked nor replaced — reference GuestAccessKey resources "+
				"by id in guestAccessKeyIds instead")
	}
	// containerSpec.exitCode — восьмое поле того же класса, найденное обходом
	// полей внутри `oneof` (прежде обход их не раскрывал). Код возврата —
	// величина ВЫХОДНАЯ: её выставляет терминальное состояние задания, и сервис
	// заполняет её на пути ответа. `ContainerSpec` стоит и в теле создания, а
	// `containerSpecFromProto` этого поля не переносит, — то есть присланное
	// значение принималось и молча выбрасывалось.
	if req.GetContainerSpec().GetExitCode() != 0 {
		add("container_spec.exit_code",
			"containerSpec.exitCode is output-only: it is set by the job's terminal state")
	}
	if n == 0 {
		return nil
	}
	return b.Err()
}

// RejectUnsupportedUpdateFields отвергает поля UpdateInstanceRequest, за которыми
// в compute нет ни одной подсистемы: `ssh_public_keys`, `metadata`,
// `network_settings`, `maintenance_policy`, `maintenance_grace_period`,
// `serial_port_settings`.
//
// Прикрытие маской было ЧАСТИЧНЫМ и потому обманчивым: known-set
// (instanceUpdateKnown) этих полей не содержит, поэтому явное упоминание в маске
// давало generic «unknown field», — но при ПУСТОЙ маске (full-object PATCH,
// api-conventions) тело снова принималось и выбрасывалось. Один и тот же параметр
// отвечал по-разному в зависимости от маски, а в «тихой» ветке — успехом.
// `ssh_public_keys` вдобавок штамповал метку «вступит в силу при следующей
// загрузке»: продукт не просто игнорировал параметр, он подтверждал его приём.
//
// Здесь стоял отказ по `metadata`, отсылавший к RPC `UpdateMetadata`. Такого RPC
// в контракте НЕТ — он снят волной 1, — а поле снято целиком вместе с приёмом на
// создании. Отказ, называющий адрес возможности, которого не существует, хуже
// молчания: вызывающий уходит искать и не находит, а способа задать эти данные у
// него нет вовсе.
//
// Форма отказа и её обоснование — общие с RejectUnsupportedCreateFields.
// Вызывается ПЕРВЫМ стейтментом Update: иначе поле, названное в маске, ответило
// бы про `update_mask`, а не про себя.
func RejectUnsupportedUpdateFields(req *computev1.UpdateInstanceRequest) error {
	b := coreerrors.InvalidArgument()
	n := 0
	add := func(field, desc string) {
		b.AddFieldViolation(field, desc)
		n++
	}
	if len(req.GetSshPublicKeys()) > 0 {
		add("ssh_public_keys",
			"sshPublicKeys is not accepted: a key carried as a field lives exactly as long as the "+
				"instance and can be neither revoked nor replaced — reference GuestAccessKey resources "+
				"by id in guestAccessKeyIds instead")
	}
	if req.GetNetworkSettings() != nil {
		add("network_settings", "networkSettings is not supported: compute does not configure network acceleration")
	}
	if req.GetMaintenancePolicy() != computev1.MaintenancePolicy_MAINTENANCE_POLICY_UNSPECIFIED {
		add("maintenance_policy", "maintenancePolicy is not supported: compute does not schedule host maintenance")
	}
	if req.GetMaintenanceGracePeriod() != nil {
		add("maintenance_grace_period", "maintenanceGracePeriod is not supported: compute does not schedule host maintenance")
	}
	if req.GetSerialPortSettings() != nil {
		add("serial_port_settings", "serialPortSettings is not supported: compute does not configure serial-port access")
	}
	if n == 0 {
		return nil
	}
	return b.Err()
}

// RejectUnsupportedSerialPortFields отвергает `port` у GetSerialPortOutput.
// Ответ RPC синтетический и от порта не зависит (control-plane консолей гостя не
// держит), поэтому принятый номер порта — обещание выбора, которого нет.
//
// Зацепка — ЗАДАННОЕ значение: proto3 не отличает «не прислано» от нуля, а
// документированный дефолт поля равен 1, поэтому любой ненулевой номер означает,
// что вызывающий порт выбрал.
func RejectUnsupportedSerialPortFields(req *computev1.GetInstanceSerialPortOutputRequest) error {
	if req.GetPort() == 0 {
		return nil
	}
	b := coreerrors.InvalidArgument()
	b.AddFieldViolation("port",
		"port is not supported: the serial-port output is control-plane synthetic and does not depend on the port")
	return b.Err()
}

// CreateReqFromProto — чистая proto→use-case конвертация CreateInstanceRequest в
// instance.CreateInstanceReq (без auth/transport). Тот же маппинг, что выполняет RPC
// Create; выделен, чтобы fuzz (internal/fuzz) прогонял ровно этот путь на
// hostile-входах. Launch-*Specs передаются как форма (структурная валидация в
// use-case; materialize — COMP-2).
func CreateReqFromProto(req *computev1.CreateInstanceRequest) instance.CreateInstanceReq {
	cr := instance.CreateInstanceReq{
		ProjectID:              req.ProjectId,
		Name:                   req.Name,
		Description:            req.Description,
		Labels:                 req.Labels,
		ZoneID:                 req.ZoneId,
		Hostname:               req.Hostname,
		InstanceKind:           domain.InstanceKind(req.InstanceKind), // proto enum зеркалит domain
		MachineTypeID:          req.MachineTypeId,
		CPUGuaranteePercent:    req.CpuGuaranteePercent,
		BootSource:             bootSourceFromProto(req.BootSource),
		ServiceAccountID:       req.ServiceAccountId,
		PlacementGroupID:       req.PlacementGroupId,
		UseDefaultNetwork:      req.UseDefaultNetwork,
		AssignExternalAddress:  req.AssignExternalAddress,
		AcknowledgeUnreachable: req.AcknowledgeUnreachable,
		NetworkInterfaceSpecs:  nicSpecsFromProto(req.NetworkInterfaceSpecs),
		GuestAccessKeyIDs:      req.GuestAccessKeyIds,
		SecondaryVolumeSpecs:   secVolSpecsFromProto(req.SecondaryVolumeSpecs),
	}
	switch sp := req.Spec.(type) {
	case *computev1.CreateInstanceRequest_VmSpec:
		cr.VMSpec = vmSpecFromProto(sp.VmSpec)
	case *computev1.CreateInstanceRequest_ContainerSpec:
		cr.ContainerSpec = containerSpecFromProto(sp.ContainerSpec)
	}
	return cr
}

// Update инициирует обновление Instance.
func (h *InstanceHandler) Update(ctx context.Context, req *computev1.UpdateInstanceRequest) (*operationpb.Operation, error) {
	// Непринимаемые поля — ПЕРВЫМ стейтментом (см. RejectUnsupportedUpdateFields):
	// названное в маске поле обязано отвечать про себя, а не про update_mask.
	if err := RejectUnsupportedUpdateFields(req); err != nil {
		return nil, err
	}
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	ur := instance.UpdateInstanceReq{
		InstanceID:          req.InstanceId,
		Name:                req.Name,
		Description:         req.Description,
		Labels:              req.Labels,
		ServiceAccountID:    req.ServiceAccountId,
		MachineTypeID:       req.MachineTypeId,
		CPUGuaranteePercent: req.CpuGuaranteePercent,
		PlacementGroupID:    req.PlacementGroupId,
		VMSpec:              vmSpecFromProto(req.VmSpec),
		GuestAccessKeyIDs:   req.GuestAccessKeyIds,
		UpdateMask:          mask,
	}
	op, err := h.svc.Update(ctx, ur)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Start инициирует запуск ВМ.
func (h *InstanceHandler) Start(ctx context.Context, req *computev1.StartInstanceRequest) (*operationpb.Operation, error) {
	return h.lifecycle(ctx, req.InstanceId, h.svc.Start)
}

// Stop инициирует остановку ВМ.
func (h *InstanceHandler) Stop(ctx context.Context, req *computev1.StopInstanceRequest) (*operationpb.Operation, error) {
	return h.lifecycle(ctx, req.InstanceId, h.svc.Stop)
}

// Restart инициирует перезапуск ВМ.
func (h *InstanceHandler) Restart(ctx context.Context, req *computev1.RestartInstanceRequest) (*operationpb.Operation, error) {
	return h.lifecycle(ctx, req.InstanceId, h.svc.Restart)
}

func (h *InstanceHandler) lifecycle(ctx context.Context, id string, fn func(context.Context, string) (*operations.Operation, error)) (*operationpb.Operation, error) {
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, id); err != nil {
		return nil, err
	}
	op, err := fn(ctx, id)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// AttachDisk инициирует подключение диска к ВМ.
func (h *InstanceHandler) AttachDisk(ctx context.Context, req *computev1.AttachInstanceDiskRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	op, err := h.svc.AttachDisk(ctx, req.InstanceId, attachDiskReqFromSpec(req.AttachedDiskSpec))
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// DetachDisk инициирует отвязку диска от ВМ.
func (h *InstanceHandler) DetachDisk(ctx context.Context, req *computev1.DetachInstanceDiskRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	op, err := h.svc.DetachDisk(ctx, req.InstanceId, req.GetVolumeId(), req.GetDeviceName())
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// AttachNetworkInterface привязывает существующий kacho-vpc NIC к ВМ (async saga,
// S4). Владелец привязки — kacho-vpc; compute держит ноль local attach-state.
func (h *InstanceHandler) AttachNetworkInterface(ctx context.Context, req *computev1.AttachInstanceNetworkInterfaceRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	spec := req.GetAttachedNicSpec()
	if spec == nil {
		return nil, status.Error(codes.InvalidArgument, "attached_nic_spec is required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	op, err := h.svc.AttachNetworkInterface(ctx, req.InstanceId, spec.GetNicId(), spec.GetIndex())
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// DetachNetworkInterface отвязывает NIC от ВМ по nic_id ЛИБО index (oneof, async
// saga, S4). Пустой oneof → sync InvalidArgument (exactly_one).
func (h *InstanceHandler) DetachNetworkInterface(ctx context.Context, req *computev1.DetachInstanceNetworkInterfaceRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	var (
		op  *operations.Operation
		err error
	)
	switch req.GetNetworkInterface().(type) {
	case *computev1.DetachInstanceNetworkInterfaceRequest_NicId:
		op, err = h.svc.DetachNetworkInterface(ctx, req.InstanceId, req.GetNicId(), 0, false)
	case *computev1.DetachInstanceNetworkInterfaceRequest_Index:
		op, err = h.svc.DetachNetworkInterface(ctx, req.InstanceId, "", req.GetIndex(), true)
	default:
		return nil, status.Error(codes.InvalidArgument, "exactly one of nic_id or index is required")
	}
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// SimulateMaintenanceEvent — no-op (control-plane).
func (h *InstanceHandler) SimulateMaintenanceEvent(ctx context.Context, req *computev1.SimulateInstanceMaintenanceEventRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	op, err := h.svc.SimulateMaintenanceEvent(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// Delete инициирует удаление ВМ.
func (h *InstanceHandler) Delete(ctx context.Context, req *computev1.DeleteInstanceRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	op, err := h.svc.Delete(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// GetSerialPortOutput — sync RPC: синтетический текст.
func (h *InstanceHandler) GetSerialPortOutput(ctx context.Context, req *computev1.GetInstanceSerialPortOutputRequest) (*computev1.GetInstanceSerialPortOutputResponse, error) {
	if err := RejectUnsupportedSerialPortFields(req); err != nil {
		return nil, err
	}
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	contents, err := h.svc.GetSerialPortOutput(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	return &computev1.GetInstanceSerialPortOutputResponse{Contents: contents}, nil
}

// ListOperations возвращает операции для ВМ.
func (h *InstanceHandler) ListOperations(ctx context.Context, req *computev1.ListInstanceOperationsRequest) (*computev1.ListInstanceOperationsResponse, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	if _, err := h.svc.Get(ctx, req.InstanceId); err != nil {
		return nil, err
	}
	ops, nextToken, err := h.svc.ListOperations(ctx, req.InstanceId, instance.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListInstanceOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, operationToProto(&ops[i]))
	}
	return resp, nil
}

// ---- conversion helpers ----

// bootSourceFromProto — proto BootSource → domain.BootSource (COMP-1 F3). Output-only
// поля (name/resolvedDigest/materializedVolume/imageKind) передаются как есть, чтобы
// use-case отверг их на входе (COMP-1-11).
func bootSourceFromProto(bs *computev1.BootSource) domain.BootSource {
	if bs == nil {
		return domain.BootSource{}
	}
	out := domain.BootSource{
		Type:           bs.Type,
		ID:             bs.Id,
		Name:           bs.Name,
		ResolvedDigest: bs.ResolvedDigest,
		ImageKind:      domain.ImageKind(bs.ImageKind), // proto enum зеркалит domain
	}
	if mv := bs.MaterializedVolume; mv != nil {
		out.MaterializedVolume = &domain.MaterializedVolume{
			VolumeID:     mv.VolumeId,
			SizeBytes:    mv.SizeBytes,
			SizeGiB:      mv.SizeGib,
			VolumeTypeID: mv.VolumeTypeId,
		}
	}
	return out
}

// vmSpecFromProto — proto VmSpec → domain.VMSpec (nil → nil).
func vmSpecFromProto(v *computev1.VmSpec) *domain.VMSpec {
	if v == nil {
		return nil
	}
	out := &domain.VMSpec{UserData: v.UserData}
	if mo := v.MetadataOptions; mo != nil {
		out.MetadataEndpoint = domain.MetadataOption(mo.MetadataEndpoint) // proto enum зеркалит domain
	}
	return out
}

// containerSpecFromProto — proto ContainerSpec → domain.ContainerSpec (nil → nil).
func containerSpecFromProto(c *computev1.ContainerSpec) *domain.ContainerSpec {
	if c == nil {
		return nil
	}
	out := &domain.ContainerSpec{
		Command:       c.Command,
		Args:          c.Args,
		Env:           c.Env,
		WorkingDir:    c.WorkingDir,
		RestartPolicy: domain.RestartPolicy(c.RestartPolicy), // proto enum зеркалит domain
	}
	for i := range c.Ports {
		out.Ports = append(out.Ports, domain.ContainerPort{
			ContainerPort: c.Ports[i].ContainerPort,
			Protocol:      c.Ports[i].Protocol,
		})
	}
	return out
}

// nicSpecsFromProto — proto NetworkInterfaceSpec[] → instance.NetworkInterfaceSpec[] (F6
// launch skeleton; структурная валидация в use-case).
func nicSpecsFromProto(specs []*computev1.NetworkInterfaceSpec) []instance.NetworkInterfaceSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]instance.NetworkInterfaceSpec, 0, len(specs))
	for _, s := range specs {
		if s == nil {
			continue
		}
		out = append(out, instance.NetworkInterfaceSpec{
			SubnetID:         s.SubnetId,
			SecurityGroupIDs: s.SecurityGroupIds,
		})
	}
	return out
}

// secVolSpecsFromProto — proto SecondaryVolumeSpec[] → instance.SecondaryVolumeSpec[].
func secVolSpecsFromProto(specs []*computev1.SecondaryVolumeSpec) []instance.SecondaryVolumeSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]instance.SecondaryVolumeSpec, 0, len(specs))
	for _, s := range specs {
		if s == nil {
			continue
		}
		out = append(out, instance.SecondaryVolumeSpec{
			SizeGiB:      s.SizeGib,
			VolumeTypeID: s.VolumeTypeId,
			MountPath:    s.MountPath,
			AutoDelete:   s.AutoDelete,
		})
	}
	return out
}

// attachDiskReqFromSpec — proto AttachedDiskSpec → instance.AttachDiskReq. Только
// volume_id-arm (storage-split: inline disk_spec на AttachDisk не поддерживается —
// подключаются только уже созданные storage-Volume; sec.2.2). is_boot берётся из
// AttachedDiskSpec (mirror: boot vs secondary на Instance).
func attachDiskReqFromSpec(s *computev1.AttachedDiskSpec) instance.AttachDiskReq {
	if s == nil {
		return instance.AttachDiskReq{}
	}
	return instance.AttachDiskReq{
		VolumeID:   s.GetVolumeId(),
		DeviceName: s.GetDeviceName(),
		AutoDelete: s.GetAutoDelete(),
		Mode:       int32(s.GetMode()),
	}
}
