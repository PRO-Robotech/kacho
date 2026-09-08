// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package protoconv — единственное место конверсии domain-сущностей kacho-compute
// в proto-сообщения. Используется и service-слоем (для Operation.response), и
// handler-слоем (для Get/List) — НЕ два дублирующих конвертера (как в kacho-vpc).
//
// Контракт: `created_at` всегда truncate до секунд (конвенция Kachō по
// timestamp-точности в proto-ответах).
package protoconv

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	referencev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
)

// serviceAccountRefType — canonical class-C Referrer type для serviceAccountId
// (COMP-1 F4/B2; graceful-dangling reference на iam.service_account).
const serviceAccountRefType = "iam.service_account"

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t.Truncate(time.Second)) }

// MachineType конвертирует domain.MachineType → computev1.MachineType (COMP-1 F7).
// effective_resources / available_zones — output-only зеркала; created_at усечён
// до секунд (конвенция Kachō).
func MachineType(mt *domain.MachineType) *computev1.MachineType {
	return &computev1.MachineType{
		Id:          mt.ID,
		Name:        mt.Name,
		Description: mt.Description,
		Family:      computev1.MachineType_Family(mt.Family), // domain.MachineTypeFamily зеркалит computev1.MachineType_Family (малые константы)
		EffectiveResources: &computev1.EffectiveResources{
			VCpu:      mt.EffectiveResources.VCPU,
			MemoryMib: mt.EffectiveResources.MemoryMiB,
			Gpus:      mt.EffectiveResources.GPUs,
			GpuType:   mt.EffectiveResources.GPUType,
		},
		AvailableZones: mt.AvailableZones,
		Status:         computev1.MachineType_Status(mt.Status), // domain.MachineTypeStatus зеркалит computev1.MachineType_Status
		Labels:         mt.Labels,
		CreatedAt:      ts(mt.CreatedAt),
	}
}

// Instance конвертирует domain.Instance → computev1.Instance (COMP-1 redesign).
// Vendor-cruft (platform_id/resources/scheduling_policy/gpu_settings/application/...)
// НЕ маппится (retired, ban 2). serviceAccountId эхается как class-C Referrer;
// effectiveResources — output-зеркало каталога; boot_source — единый вход ОС.
func Instance(in *domain.Instance) *computev1.Instance {
	out := &computev1.Instance{
		Id:                  in.ID,
		ProjectId:           in.ProjectID,
		CreatedAt:           ts(in.CreatedAt),
		Name:                in.Name,
		Description:         in.Description,
		Labels:              in.Labels,
		ZoneId:              in.ZoneID,
		Status:              computev1.Instance_Status(in.Status), // #nosec G115 -- domain.InstanceStatus зеркалит computev1.Instance_Status
		StatusReason:        in.StatusReason,
		Fqdn:                in.FQDN,
		GuestAccessKeyIds:   in.GuestAccessKeyIDs,
		CpuGuaranteePercent: in.CPUGuaranteePercent,
		InstanceKind:        computev1.InstanceKind(in.InstanceKind), // domain.InstanceKind зеркалит computev1.InstanceKind
		MachineTypeId:       in.MachineTypeID,
		EffectiveResources: &computev1.EffectiveResources{
			VCpu:      in.EffectiveResources.VCPU,
			MemoryMib: in.EffectiveResources.MemoryMiB,
			Gpus:      in.EffectiveResources.GPUs,
			GpuType:   in.EffectiveResources.GPUType,
		},
		BootSource:       bootSource(in.BootSource),
		PlacementGroupId: in.PlacementGroupID,
	}
	if in.ServiceAccountID != "" {
		out.ServiceAccount = &referencev1.Referrer{
			Type: serviceAccountRefType,
			Id:   in.ServiceAccountID,
		}
	}
	switch {
	case in.VMSpec != nil:
		out.Spec = &computev1.Instance_VmSpec{VmSpec: vmSpec(in.VMSpec)}
	case in.ContainerSpec != nil:
		out.Spec = &computev1.Instance_ContainerSpec{ContainerSpec: containerSpec(in.ContainerSpec)}
	}
	if boot := in.BootDiskMirror(); boot != nil {
		out.BootDisk = attachedDisk(boot)
	}
	for i := range in.AttachedDisks {
		ad := &in.AttachedDisks[i]
		if ad.IsBoot {
			continue
		}
		out.SecondaryDisks = append(out.SecondaryDisks, attachedDisk(ad))
	}
	for i := range in.NetworkInterfaces {
		out.NetworkInterfaces = append(out.NetworkInterfaces, networkInterface(&in.NetworkInterfaces[i]))
	}
	return out
}

// bootSource конвертирует domain.BootSource → computev1.BootSource. Output-only
// поля (name/resolvedDigest/materializedVolume) — заполняет resolve/materialize
// сага COMP-2; в COMP-1 пусты.
func bootSource(bs domain.BootSource) *computev1.BootSource {
	out := &computev1.BootSource{
		Type:           bs.Type,
		Id:             bs.ID,
		Name:           bs.Name,
		ResolvedDigest: bs.ResolvedDigest,
		ImageKind:      computev1.ImageKind(bs.ImageKind), // domain.ImageKind зеркалит computev1.ImageKind
	}
	if mv := bs.MaterializedVolume; mv != nil {
		out.MaterializedVolume = &computev1.MaterializedVolume{
			VolumeId:     mv.VolumeID,
			SizeBytes:    mv.SizeBytes,
			SizeGib:      mv.SizeGiB,
			VolumeTypeId: mv.VolumeTypeID,
		}
	}
	return out
}

func vmSpec(v *domain.VMSpec) *computev1.VmSpec {
	out := &computev1.VmSpec{UserData: v.UserData}
	if v.MetadataEndpoint != domain.MetadataOptionUnspecified {
		out.MetadataOptions = &computev1.MetadataOptions{
			MetadataEndpoint: computev1.MetadataOption(v.MetadataEndpoint), // domain.MetadataOption зеркалит computev1.MetadataOption
		}
	}
	return out
}

func containerSpec(c *domain.ContainerSpec) *computev1.ContainerSpec {
	out := &computev1.ContainerSpec{
		Command:       c.Command,
		Args:          c.Args,
		Env:           c.Env,
		WorkingDir:    c.WorkingDir,
		RestartPolicy: computev1.RestartPolicy(c.RestartPolicy), // domain.RestartPolicy зеркалит computev1.RestartPolicy
		ExitCode:      c.ExitCode,
	}
	for i := range c.Ports {
		out.Ports = append(out.Ports, &computev1.ContainerPort{
			ContainerPort: c.Ports[i].ContainerPort,
			Protocol:      c.Ports[i].Protocol,
		})
	}
	return out
}

func attachedDisk(ad *domain.AttachedDisk) *computev1.AttachedDisk {
	return &computev1.AttachedDisk{
		Mode:       computev1.AttachedDisk_Mode(ad.Mode), // #nosec G115 -- domain AttachedDisk Mode зеркалит computev1.AttachedDisk_Mode
		DeviceName: ad.DeviceName,
		AutoDelete: ad.AutoDelete,
		// A real kacho-storage volume id. It is read from that service's
		// ListAttachments and mirrored read-only onto the instance (see
		// api/instance.volumeMirror); compute has no disk row of its own to surface
		// here, and has had none since the block-storage retire. The domain field
		// was called DiskID while that was still an open question — it is VolumeID
		// now, because a name that says "disk" over a volume id is the kind of
		// detail the next reader trusts instead of checking.
		VolumeId: ad.VolumeID,
	}
}

func networkInterface(nic *domain.NetworkInterface) *computev1.NetworkInterface {
	out := &computev1.NetworkInterface{
		Index:            nic.Index,
		NicId:            nic.NICID,
		MacAddress:       nic.MACAddress,
		SubnetId:         nic.SubnetID,
		SecurityGroupIds: nic.SecurityGroupIDs,
	}
	if nic.PrimaryV4Address != "" || nic.PrimaryV4Nat != nil {
		out.PrimaryV4Address = &computev1.PrimaryAddress{
			Address:     nic.PrimaryV4Address,
			OneToOneNat: oneToOneNat(nic.PrimaryV4Nat),
		}
	}
	if nic.PrimaryV6Address != "" || nic.PrimaryV6Nat != nil {
		out.PrimaryV6Address = &computev1.PrimaryAddress{
			Address:     nic.PrimaryV6Address,
			OneToOneNat: oneToOneNat(nic.PrimaryV6Nat),
		}
	}
	return out
}

func oneToOneNat(n *domain.OneToOneNat) *computev1.OneToOneNat {
	if n == nil {
		return nil
	}
	return &computev1.OneToOneNat{
		Address:   n.Address,
		IpVersion: computev1.IpVersion(n.IPVersion),
	}
}

// GuestAccessKey конвертирует domain.GuestAccessKey → computev1.GuestAccessKey.
//
// Закрытой половины ключа здесь нет и быть не может: её нет в доменной сущности,
// поэтому «забыть не отдать» тут нечего — свойство держится составом типа, а не
// внимательностью этой функции.
func GuestAccessKey(k *domain.GuestAccessKey) *computev1.GuestAccessKey {
	return &computev1.GuestAccessKey{
		Id:          k.ID,
		ProjectId:   k.ProjectID,
		Name:        k.Name,
		PublicKey:   k.PublicKey,
		Fingerprint: k.Fingerprint,
		Labels:      k.Labels,
		CreatedAt:   ts(k.CreatedAt),
	}
}

// PlacementGroup конвертирует domain.PlacementGroup → computev1.PlacementGroup.
//
// Числового параметра разнесения в ответе нет и быть не может: его нет ни в
// доменной сущности, ни в контракте — свойство держится составом типов, а не
// внимательностью этой функции.
func PlacementGroup(g *domain.PlacementGroup) *computev1.PlacementGroup {
	return &computev1.PlacementGroup{
		Id:            g.ID,
		ProjectId:     g.ProjectID,
		Name:          g.Name,
		Description:   g.Description,
		CreatedAt:     ts(g.CreatedAt),
		Labels:        g.Labels,
		Strategy:      computev1.PlacementGroup_Strategy(g.Strategy),           // domain зеркалит контракт
		PlacementType: computev1.PlacementGroup_PlacementType(g.PlacementType), // domain зеркалит контракт
		ZoneId:        g.ZoneID,
		RegionId:      g.RegionID,
	}
}
