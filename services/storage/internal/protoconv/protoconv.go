// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package protoconv — ЕДИНЫЙ источник конверсии domain→proto для kacho-storage
// (domain.Volume/Snapshot/DiskType/VolumeAttachment → storagev1.*). Централизация
// убирает риск дрейфа полей между handler, use-case-marshaller (Operation.response)
// и LRO-recovery: новое поле добавляется в ОДНОМ месте.
//
// Единый timestamp-формат Kachō: created_at/updated_at/attached_at усекаются до
// секунд (микросекунды с БД не текут на wire — api-conventions.md, на КАЖДОМ
// ресурсе И под-записи).
package protoconv

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	referencev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/reference"
	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// referrerInstanceType — kind референса compute-инстанса в Volume.used_by
// (generic reference.Reference; source of truth = volume_attachments).
const referrerInstanceType = "compute.instance"

// Volume конвертирует domain.Volume → storagev1.Volume. Output-only коллекции
// attachments/used_by деривятся из domain.Attachments (repo derive-on-read). Публичная
// проекция lean (INV-7): инфра-полей нет — они только в internal-проекции :9091.
func Volume(v *domain.Volume) *storagev1.Volume {
	if v == nil {
		return nil
	}
	out := &storagev1.Volume{
		Id:               v.ID,
		ProjectId:        v.ProjectID,
		CreatedAt:        ts(v.CreatedAt),
		UpdatedAt:        ts(v.UpdatedAt),
		Name:             v.Name,
		Description:      v.Description,
		Labels:           v.Labels,
		ZoneId:           v.ZoneID,
		DiskTypeId:       v.DiskTypeID,
		SizeBytes:        v.SizeBytes,
		BlockSize:        v.BlockSize,
		SourceSnapshotId: v.SourceSnapshot,
		SourceImageId:    v.SourceImage,
		Status:           storagev1.Volume_Status(v.Status),
	}
	for i := range v.Attachments {
		a := &v.Attachments[i]
		out.Attachments = append(out.Attachments, VolumeAttachment(a))
		// used_by — generic derived-проекция attachments (§1.5): referrer =
		// {compute.instance, instance_id, instance_name}, type=USED_BY, owned=auto_delete.
		out.UsedBy = append(out.UsedBy, &referencev1.Reference{
			Referrer: &referencev1.Referrer{
				Type: referrerInstanceType,
				Id:   a.InstanceID,
				Name: a.InstanceName,
			},
			Type:  referencev1.Reference_USED_BY,
			Owned: a.AutoDelete,
		})
	}
	return out
}

// VolumeAttachment конвертирует domain.VolumeAttachment → storagev1.VolumeAttachment.
func VolumeAttachment(a *domain.VolumeAttachment) *storagev1.VolumeAttachment {
	if a == nil {
		return nil
	}
	return &storagev1.VolumeAttachment{
		InstanceId:   a.InstanceID,
		InstanceName: a.InstanceName,
		DeviceName:   a.DeviceName,
		IsBoot:       a.IsBoot,
		Mode:         storagev1.VolumeAttachment_Mode(a.Mode),
		AutoDelete:   a.AutoDelete,
		AttachedAt:   ts(a.AttachedAt),
	}
}

// VolumeAttachmentInfo конвертирует domain.VolumeAttachment → storagev1.
// VolumeAttachmentInfo (internal batched-read проекция для compute-mirror, :9091).
func VolumeAttachmentInfo(a *domain.VolumeAttachment) *storagev1.VolumeAttachmentInfo {
	if a == nil {
		return nil
	}
	return &storagev1.VolumeAttachmentInfo{
		VolumeId:     a.VolumeID,
		InstanceId:   a.InstanceID,
		InstanceName: a.InstanceName,
		DeviceName:   a.DeviceName,
		IsBoot:       a.IsBoot,
		Mode:         storagev1.VolumeAttachment_Mode(a.Mode),
		AutoDelete:   a.AutoDelete,
		AttachedAt:   ts(a.AttachedAt),
	}
}

// Snapshot конвертирует domain.Snapshot → storagev1.Snapshot.
func Snapshot(s *domain.Snapshot) *storagev1.Snapshot {
	if s == nil {
		return nil
	}
	return &storagev1.Snapshot{
		Id:             s.ID,
		ProjectId:      s.ProjectID,
		CreatedAt:      ts(s.CreatedAt),
		Name:           s.Name,
		Description:    s.Description,
		Labels:         s.Labels,
		SourceVolumeId: s.SourceVolumeID,
		SizeBytes:      s.SizeBytes,
		Status:         storagev1.Snapshot_Status(s.Status),
	}
}

// Image конвертирует domain.Image → storagev1.Image. Публичная проекция lean
// (INV-1): infra-полей (blob-layout/bucket/engine-namespace/storage-node) нет — они
// только в internal-проекции :9091 (ImageInternal). placement_type всегда REGIONAL;
// size_bytes°/min_disk_bytes°/format° — output-only (derived).
func Image(i *domain.Image) *storagev1.Image {
	if i == nil {
		return nil
	}
	return &storagev1.Image{
		Id:               i.ID,
		ProjectId:        i.ProjectID,
		CreatedAt:        ts(i.CreatedAt),
		UpdatedAt:        ts(i.UpdatedAt),
		Name:             i.Name,
		Description:      i.Description,
		Labels:           i.Labels,
		RegionId:         i.RegionID,
		PlacementType:    storagev1.Image_PlacementType(i.Placement),
		SourceSnapshotId: i.SourceSnapshot,
		SourceVolumeId:   i.SourceVolume,
		SizeBytes:        i.SizeBytes,
		MinDiskBytes:     i.MinDiskBytes,
		Format:           storagev1.Image_Format(i.Format),
		Status:           storagev1.Image_Status(i.Status),
	}
}

// DiskType конвертирует domain.DiskType → storagev1.DiskType.
func DiskType(d *domain.DiskType) *storagev1.DiskType {
	if d == nil {
		return nil
	}
	return &storagev1.DiskType{
		Id:              d.ID,
		Name:            d.Name,
		Description:     d.Description,
		ZoneIds:         d.ZoneIDs,
		PerformanceTier: d.PerformanceTier,
	}
}

// ts — единый timestamp-формат Kachō: усечение до секунд перед проекцией в proto.
func ts(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t.Truncate(time.Second))
}
