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

// referrerVolumeType — kind референса тома в used_by снимка и образа: какие тома
// засеяны из этого источника.
const referrerVolumeType = "storage.volume"

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
		SourceSnapshotId: v.SourceSnapshot,
		SourceImageId:    v.SourceImage,
		Status:           storagev1.Volume_Status(v.Status),
		StatusReason:     statusReason(v.StatusReason),
	}
	// Потребление отдаётся, ТОЛЬКО если бэкенд его сообщил. Ноль на этом месте
	// был бы утверждением о пустом томе — а «не сказали» и «пусто» разные факты,
	// и поле объявлено необязательным именно чтобы их различать.
	if v.Observation.HasUsedBytes {
		used := v.Observation.UsedBytes
		out.UsedBytes = &used
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
		UpdatedAt:      ts(s.UpdatedAt),
		ZoneId:         s.ZoneID,
		SourceVolumeId: s.SourceVolumeID,
		SizeBytes:      s.SizeBytes,
		Status:         storagev1.Snapshot_Status(s.Status),
		StatusReason:   statusReason(s.StatusReason),
		UsedBy:         seededBy(s.SeededVolumeIDs),
	}
}

// seededBy собирает обобщённую проекцию «кем используется» из идентификаторов
// томов, засеянных этим источником.
//
// Поле нужно арендатору ДО удаления: если бэкенд объявил зависимость клона от
// родителя, удаление источника с живыми детьми отвергается, и вызывающий обязан
// иметь возможность узнать, кто эти дети, а не выяснять это отказом.
func seededBy(volumeIDs []string) []*referencev1.Reference {
	if len(volumeIDs) == 0 {
		return nil
	}
	out := make([]*referencev1.Reference, 0, len(volumeIDs))
	for _, id := range volumeIDs {
		out = append(out, &referencev1.Reference{
			Referrer: &referencev1.Referrer{Type: referrerVolumeType, Id: id},
			Type:     referencev1.Reference_USED_BY,
		})
	}
	return out
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
		StatusReason:     statusReason(i.StatusReason),
		UsedBy:           seededBy(i.SeededVolumeIDs),
	}
}

// DiskType конвертирует domain.DiskType → storagev1.DiskType.
func DiskType(d *domain.DiskType) *storagev1.DiskType {
	if d == nil {
		return nil
	}
	return &storagev1.DiskType{
		Id:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		ZoneIds:     d.ZoneIDs,
		Tier:        diskTypeTier(d.PerformanceTier),
		Lifecycle:   diskTypeLifecycle(d.Lifecycle),
		Capabilities: &storagev1.DiskType_Capabilities{
			Snapshots:         d.Capabilities.Snapshots,
			CloneFromSnapshot: d.Capabilities.CloneFromSnapshot,
			CloneFromImage:    d.Capabilities.CloneFromImage,
			OnlineGrow:        d.Capabilities.OnlineGrow,
			MultiAttach:       d.Capabilities.MultiAttach,
			EncryptionAtRest:  d.Capabilities.EncryptionAtRest,
		},
		Limits: &storagev1.DiskType_SizeLimits{
			MinSizeBytes:  d.Limits.MinSizeBytes,
			MaxSizeBytes:  d.Limits.MaxSizeBytes,
			SizeStepBytes: d.Limits.SizeStepBytes,
		},
	}
}

// diskTypeTier переводит ярус в перечисление контракта. Неизвестное значение даёт
// UNSPECIFIED, а не выдумку: домен уже отверг бы такой ярус на записи, и если он
// всё же встретился на чтении, честнее сказать «не назван», чем назначить.
func diskTypeTier(t domain.PerformanceTier) storagev1.DiskType_PerformanceTier {
	switch t {
	case domain.TierCapacity:
		return storagev1.DiskType_CAPACITY
	case domain.TierBalanced:
		return storagev1.DiskType_BALANCED
	case domain.TierFast:
		return storagev1.DiskType_FAST
	case domain.TierSingle:
		return storagev1.DiskType_SINGLE
	case domain.TierIOMax:
		return storagev1.DiskType_IO_MAX
	default:
		return storagev1.DiskType_PERFORMANCE_TIER_UNSPECIFIED
	}
}

// diskTypeLifecycle переводит состояние обращения класса.
func diskTypeLifecycle(l domain.DiskTypeLifecycle) storagev1.DiskType_Lifecycle {
	switch l {
	case domain.LifecycleActive:
		return storagev1.DiskType_ACTIVE
	case domain.LifecycleDeprecated:
		return storagev1.DiskType_DEPRECATED
	case domain.LifecycleRetired:
		return storagev1.DiskType_RETIRED
	default:
		return storagev1.DiskType_LIFECYCLE_UNSPECIFIED
	}
}

// statusReason переводит причину состояния. Словарь ЗАКРЫТ с обеих сторон:
// значение, которого нет в перечислении контракта, не превращается в текст — оно
// становится «не названо», и это видно.
func statusReason(r domain.StatusReason) storagev1.StatusReason {
	switch r {
	case domain.ReasonBackendUnavailable:
		return storagev1.StatusReason_BACKEND_UNAVAILABLE
	case domain.ReasonBackendRejected:
		return storagev1.StatusReason_BACKEND_REJECTED
	case domain.ReasonBackendCapacityExhausted:
		return storagev1.StatusReason_BACKEND_CAPACITY_EXHAUSTED
	case domain.ReasonSourceNotReady:
		return storagev1.StatusReason_SOURCE_NOT_READY
	case domain.ReasonPreconditionFailed:
		return storagev1.StatusReason_PRECONDITION_FAILED
	case domain.ReasonInternalError:
		return storagev1.StatusReason_INTERNAL_ERROR
	default:
		return storagev1.StatusReason_STATUS_REASON_UNSPECIFIED
	}
}

// ts — единый timestamp-формат Kachō: усечение до секунд перед проекцией в proto.
func ts(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t.Truncate(time.Second))
}

// TierFromProto переводит ярус из контракта в домен. Обратное направление
// diskTypeTier; держится рядом с ним намеренно — пара конверсий, разъехавшаяся по
// файлам, расходится и по значениям.
func TierFromProto(t storagev1.DiskType_PerformanceTier) domain.PerformanceTier {
	switch t {
	case storagev1.DiskType_CAPACITY:
		return domain.TierCapacity
	case storagev1.DiskType_BALANCED:
		return domain.TierBalanced
	case storagev1.DiskType_FAST:
		return domain.TierFast
	case storagev1.DiskType_SINGLE:
		return domain.TierSingle
	case storagev1.DiskType_IO_MAX:
		return domain.TierIOMax
	default:
		return ""
	}
}

// LifecycleFromProto переводит состояние обращения класса в домен. UNSPECIFIED
// даёт пустое значение, а НЕ ACTIVE: умолчание проставляет use-case в одном
// названном месте, и конверсия не вправе решать это за него — иначе опечатка
// администратора стала бы намерением.
func LifecycleFromProto(l storagev1.DiskType_Lifecycle) domain.DiskTypeLifecycle {
	switch l {
	case storagev1.DiskType_ACTIVE:
		return domain.LifecycleActive
	case storagev1.DiskType_DEPRECATED:
		return domain.LifecycleDeprecated
	case storagev1.DiskType_RETIRED:
		return domain.LifecycleRetired
	default:
		return ""
	}
}

// SizeLimitsFromProto переводит границы размера в домен. Отсутствие сообщения —
// «класс не сужает», а не нулевые границы.
func SizeLimitsFromProto(l *storagev1.DiskType_SizeLimits) domain.SizeLimits {
	if l == nil {
		return domain.SizeLimits{}
	}
	return domain.SizeLimits{
		MinSizeBytes:  l.GetMinSizeBytes(),
		MaxSizeBytes:  l.GetMaxSizeBytes(),
		SizeStepBytes: l.GetSizeStepBytes(),
	}
}
