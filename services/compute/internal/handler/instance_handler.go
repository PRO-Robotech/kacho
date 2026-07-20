// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
	svc "github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// InstanceHandler реализует computev1.InstanceServiceServer (тонкий transport-слой).
//
// Unimplemented RPC (наследуются из UnimplementedInstanceServiceServer →
// codes.Unimplemented): AttachFilesystem/DetachFilesystem (blocked:kacho-filesystem),
// UpdateNetworkInterface/AddOneToOneNat/RemoveOneToOneNat (NIC first-class в kacho-vpc —
// адресация/NAT редактируются через vpc NetworkInterface, не через Instance),
// Relocate (blocked: cross-zone disk move), ListAccessBindings/SetAccessBindings/
// UpdateAccessBindings (AAA-скелет). См. docs/architecture/07-known-divergences.md.
// AttachNetworkInterface/DetachNetworkInterface — реализованы (S4, NIC-attach saga →
// kacho-vpc InternalNetworkInterfaceService).
type InstanceHandler struct {
	computev1.UnimplementedInstanceServiceServer
	svc        *svc.InstanceService
	listFilter authzfilter.Filter
}

// NewInstanceHandler создаёт InstanceHandler. listFilter может быть nil — тогда
// FGA-фильтрация на List отключена (dev/breakglass).
func NewInstanceHandler(s *svc.InstanceService, listFilter authzfilter.Filter) *InstanceHandler {
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
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	p := protoconv.Instance(in)
	// GetInstanceRequest.view — metadata возвращается только при view=FULL.
	if req.View != computev1.InstanceView_FULL {
		p.Metadata = nil
	}
	return p, nil
}

// List возвращает список ВМ в folder.
//
// Вызов фильтруется через iam.AuthorizeService.ListObjects
// (caller subject → allowed instance_ids). admin / dev-bypass → no filtering.
// Empty grant → empty list (NOT 403 — конвенция Kachō для list-empty).
func (h *InstanceHandler) List(ctx context.Context, req *computev1.ListInstancesRequest) (*computev1.ListInstancesResponse, error) {
	if err := AssertProjectOwnership(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	// Validate pagination BEFORE the listauthz empty-grant short-circuit (see disk_handler).
	if err := svc.ValidateListPagination(svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize}); err != nil {
		return nil, err
	}
	dec, err := resolveListFilter(ctx, h.listFilter, authzfilter.ResourceTypeInstance, authzfilter.ActionInstanceRead)
	if err != nil {
		return nil, err
	}
	filter := svc.InstanceFilter{ProjectID: req.ProjectId, Filter: req.Filter}
	if !dec.IsBypass() {
		if len(dec.IDs()) == 0 {
			return &computev1.ListInstancesResponse{}, nil
		}
		filter.AllowedIDs = dec.IDs()
	}
	ins, nextToken, err := h.svc.List(ctx, filter,
		svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
	if err != nil {
		return nil, err
	}
	resp := &computev1.ListInstancesResponse{NextPageToken: nextToken}
	for _, in := range ins {
		p := protoconv.Instance(in)
		// metadata всегда опускается в List response (в ListInstancesRequest
		// нет view-параметра — это документировано в instance.proto комментарии к Instance.metadata).
		p.Metadata = nil
		resp.Instances = append(resp.Instances, p)
	}
	return resp, nil
}

// Create инициирует создание Instance (COMP-1 redesign).
func (h *InstanceHandler) Create(ctx context.Context, req *computev1.CreateInstanceRequest) (*operationpb.Operation, error) {
	if err := AssertProjectOwnership(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	op, err := h.svc.Create(ctx, CreateReqFromProto(req))
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// CreateReqFromProto — чистая proto→use-case конвертация CreateInstanceRequest в
// svc.CreateInstanceReq (без auth/transport). Тот же маппинг, что выполняет RPC
// Create; выделен, чтобы fuzz (internal/fuzz) прогонял ровно этот путь на
// hostile-входах. Launch-*Specs передаются как форма (структурная валидация в
// use-case; materialize — COMP-2).
func CreateReqFromProto(req *computev1.CreateInstanceRequest) svc.CreateInstanceReq {
	cr := svc.CreateInstanceReq{
		ProjectID:              req.ProjectId,
		Name:                   req.Name,
		Description:            req.Description,
		Labels:                 req.Labels,
		ZoneID:                 req.ZoneId,
		Metadata:               req.Metadata,
		Hostname:               req.Hostname,
		InstanceKind:           domain.InstanceKind(req.InstanceKind), // #nosec G115 -- proto enum зеркалит domain
		MachineTypeID:          req.MachineTypeId,
		CPUGuaranteePercent:    req.CpuGuaranteePercent,
		BootSource:             bootSourceFromProto(req.BootSource),
		ServiceAccountID:       req.ServiceAccountId,
		PlacementGroupID:       req.PlacementGroupId,
		SSHPublicKeys:          req.SshPublicKeys,
		UseDefaultNetwork:      req.UseDefaultNetwork,
		AssignExternalAddress:  req.AssignExternalAddress,
		AcknowledgeUnreachable: req.AcknowledgeUnreachable,
		NetworkInterfaceSpecs:  nicSpecsFromProto(req.NetworkInterfaceSpecs),
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
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	ur := svc.UpdateInstanceReq{
		InstanceID:          req.InstanceId,
		Name:                req.Name,
		Description:         req.Description,
		Labels:              req.Labels,
		ServiceAccountID:    req.ServiceAccountId,
		MachineTypeID:       req.MachineTypeId,
		CPUGuaranteePercent: req.CpuGuaranteePercent,
		PlacementGroupID:    req.PlacementGroupId,
		SSHPublicKeys:       req.SshPublicKeys,
		VMSpec:              vmSpecFromProto(req.VmSpec),
		UpdateMask:          mask,
	}
	op, err := h.svc.Update(ctx, ur)
	if err != nil {
		return nil, err
	}
	return operationToProto(op), nil
}

// UpdateMetadata инициирует обновление metadata ВМ.
func (h *InstanceHandler) UpdateMetadata(ctx context.Context, req *computev1.UpdateInstanceMetadataRequest) (*operationpb.Operation, error) {
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	op, err := h.svc.UpdateMetadata(ctx, req.InstanceId, req.Delete, req.Upsert)
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
	in, err := h.svc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	var op *operations.Operation
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	if req.InstanceId == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id required")
	}
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
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
	in, err := h.svc.Get(ctx, req.InstanceId)
	if err != nil {
		return nil, err
	}
	if err := AssertProjectOwnership(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	ops, nextToken, err := h.svc.ListOperations(ctx, req.InstanceId, svc.Pagination{PageToken: req.PageToken, PageSize: req.PageSize})
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
		ImageKind:      domain.ImageKind(bs.ImageKind), // #nosec G115 -- proto enum зеркалит domain
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
		out.MetadataEndpoint = domain.MetadataOption(mo.MetadataEndpoint) // #nosec G115 -- proto enum зеркалит domain
		out.MetadataTokenRequired = mo.MetadataTokenRequired
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
		RestartPolicy: domain.RestartPolicy(c.RestartPolicy), // #nosec G115 -- proto enum зеркалит domain
	}
	for i := range c.Ports {
		out.Ports = append(out.Ports, domain.ContainerPort{
			ContainerPort: c.Ports[i].ContainerPort,
			Protocol:      c.Ports[i].Protocol,
		})
	}
	return out
}

// nicSpecsFromProto — proto NetworkInterfaceSpec[] → svc.NetworkInterfaceSpec[] (F6
// launch skeleton; структурная валидация в use-case).
func nicSpecsFromProto(specs []*computev1.NetworkInterfaceSpec) []svc.NetworkInterfaceSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]svc.NetworkInterfaceSpec, 0, len(specs))
	for _, s := range specs {
		if s == nil {
			continue
		}
		out = append(out, svc.NetworkInterfaceSpec{
			SubnetID:         s.SubnetId,
			SecurityGroupIDs: s.SecurityGroupIds,
		})
	}
	return out
}

// secVolSpecsFromProto — proto SecondaryVolumeSpec[] → svc.SecondaryVolumeSpec[].
func secVolSpecsFromProto(specs []*computev1.SecondaryVolumeSpec) []svc.SecondaryVolumeSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]svc.SecondaryVolumeSpec, 0, len(specs))
	for _, s := range specs {
		if s == nil {
			continue
		}
		out = append(out, svc.SecondaryVolumeSpec{
			SizeGiB:      s.SizeGib,
			VolumeTypeID: s.VolumeTypeId,
			MountPath:    s.MountPath,
			AutoDelete:   s.AutoDelete,
		})
	}
	return out
}

// attachDiskReqFromSpec — proto AttachedDiskSpec → svc.AttachDiskReq. Только
// volume_id-arm (storage-split: inline disk_spec на AttachDisk не поддерживается —
// подключаются только уже созданные storage-Volume; sec.2.2). is_boot берётся из
// AttachedDiskSpec (mirror: boot vs secondary на Instance).
func attachDiskReqFromSpec(s *computev1.AttachedDiskSpec) svc.AttachDiskReq {
	if s == nil {
		return svc.AttachDiskReq{}
	}
	return svc.AttachDiskReq{
		VolumeID:   s.GetVolumeId(),
		DeviceName: s.GetDeviceName(),
		AutoDelete: s.GetAutoDelete(),
		Mode:       int32(s.GetMode()),
	}
}
