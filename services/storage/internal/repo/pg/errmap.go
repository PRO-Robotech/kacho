// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
)

// volErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок Volume-репо.
// Точные тексты ошибок (§1.7) зависят от того, КАКОЙ constraint нарушен, поэтому
// mapVolumeErr переключается по PgError.ConstraintName, а id/name/diskType/snapshot
// подставляются в контрактное сообщение.
type volErrCtx struct {
	volumeID   string
	volumeName string
	diskTypeID string
	snapshotID string
	deviceName string // attach-путь: device_name для UNIQUE(instance_id,device_name) текста
	instanceID string // attach-путь: instance_id для device/boot-конфликт текста
}

// Имена DB-constraint'ов (миграция 0003_storage_domain). Inline-FK без CONSTRAINT
// именуются Postgres'ом как <table>_<column>_fkey; именованные — как в ALTER/CREATE.
const (
	cnVolumeNameUniq     = "volumes_name_uniq"                 // partial UNIQUE(project_id,name) WHERE name<>''
	cnVolumeDiskTypeFK   = "volumes_disk_type_id_fkey"         // volumes.disk_type_id → disk_types RESTRICT
	cnVolumeSnapshotFK   = "volumes_source_snapshot_fk"        // volumes.source_snapshot_id → snapshots SET NULL
	cnAttachmentVolumeFK = "volume_attachments_volume_id_fkey" // volume_attachments.volume_id → volumes RESTRICT
	cnAttachDeviceUniq   = "volume_attachments_instance_device_uniq"
	cnAttachOneBoot      = "volume_attachments_one_boot"
)

// mapVolumeErr транслирует pgx/pgconn-ошибку в чистый ports-sentinel с контрактным
// текстом Kachō (§1.7). Сырой pgx/SQL наружу не течёт: некатегоризированный
// SQLSTATE → ports.ErrInternal (serviceerr → фиксированный "internal error"), но
// сам SQLSTATE логируется на repo-границе (operator-trail, CWE-390).
func mapVolumeErr(err error, c volErrCtx) error {
	if err == nil {
		return nil
	}
	// Идемпотентность: уже-замапленный ports-sentinel (напр. hand-crafted
	// "Volume size can only be increased" / NotFound из disambiguation Update)
	// пробрасывается как есть — иначе default-ветка ниже коллапсировала бы его в
	// ErrInternal (теряя контрактный текст).
	switch {
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrAlreadyExists),
		errors.Is(err, ports.ErrFailedPrecondition), errors.Is(err, ports.ErrInvalidArg),
		errors.Is(err, ports.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: Volume %s not found", ports.ErrNotFound, c.volumeID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			switch pgErr.ConstraintName {
			case cnVolumeNameUniq:
				return fmt.Errorf("%w: volume with name %s already exists in project", ports.ErrAlreadyExists, c.volumeName)
			case cnAttachDeviceUniq:
				return fmt.Errorf("%w: device %s is already in use on Instance %s", ports.ErrFailedPrecondition, c.deviceName, c.instanceID)
			}
			return fmt.Errorf("%w: volume already exists", ports.ErrAlreadyExists)
		case "23503": // foreign_key_violation
			switch pgErr.ConstraintName {
			case cnVolumeDiskTypeFK:
				return fmt.Errorf("%w: DiskType %s not found", ports.ErrFailedPrecondition, c.diskTypeID)
			case cnVolumeSnapshotFK:
				return fmt.Errorf("%w: Snapshot %s not found", ports.ErrFailedPrecondition, c.snapshotID)
			case cnAttachmentVolumeFK:
				return fmt.Errorf("%w: Volume %s is in use", ports.ErrFailedPrecondition, c.volumeID)
			}
			return fmt.Errorf("%w: volume violates a reference constraint", ports.ErrFailedPrecondition)
		case "23514": // check_violation (size_bytes>0 / block_size>0 / name / labels)
			return fmt.Errorf("%w: Illegal argument", ports.ErrInvalidArg)
		case "23P01": // exclusion_violation (EXCLUDE … WHERE is_boot)
			if pgErr.ConstraintName == cnAttachOneBoot {
				return fmt.Errorf("%w: Instance %s already has a boot volume", ports.ErrFailedPrecondition, c.instanceID)
			}
			return fmt.Errorf("%w: volume exclusion constraint", ports.ErrFailedPrecondition)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			"sqlstate", pgErr.Code, "constraint", pgErr.ConstraintName, "volume_id", c.volumeID)
		return ports.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "volume_id", c.volumeID)
	return ports.ErrInternal
}

// cnSnapshotNameUniq — partial UNIQUE(project_id,name) WHERE name<>” снапшотов.
const cnSnapshotNameUniq = "snapshots_name_uniq"

// snapErrCtx — контекстные hint'ы для constraint-aware маппинга ошибок Snapshot-репо.
type snapErrCtx struct {
	snapshotID     string
	snapshotName   string
	sourceVolumeID string
}

// mapSnapshotErr транслирует pgx/pgconn-ошибку Snapshot-репо в чистый ports-sentinel
// с контрактным текстом Kachō. Сырой pgx/SQL наружу не течёт (uncategorized →
// ports.ErrInternal, SQLSTATE логируется на границе). Уже-замапленный sentinel
// (from-READY disambiguation / NotFound) пробрасывается как есть.
func mapSnapshotErr(err error, c snapErrCtx) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrAlreadyExists),
		errors.Is(err, ports.ErrFailedPrecondition), errors.Is(err, ports.ErrInvalidArg),
		errors.Is(err, ports.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: Snapshot %s not found", ports.ErrNotFound, c.snapshotID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			if pgErr.ConstraintName == cnSnapshotNameUniq {
				return fmt.Errorf("%w: snapshot with name %s already exists in project", ports.ErrAlreadyExists, c.snapshotName)
			}
			return fmt.Errorf("%w: snapshot already exists", ports.ErrAlreadyExists)
		case "23503": // foreign_key_violation — source_volume_id → volumes
			return fmt.Errorf("%w: Volume %s not found", ports.ErrFailedPrecondition, c.sourceVolumeID)
		case "23514": // check_violation (name / description / size / labels)
			return fmt.Errorf("%w: Illegal argument", ports.ErrInvalidArg)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			"sqlstate", pgErr.Code, "constraint", pgErr.ConstraintName, "snapshot_id", c.snapshotID)
		return ports.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "snapshot_id", c.snapshotID)
	return ports.ErrInternal
}

// dtErrCtx — контекстный hint (id) для constraint-aware маппинга ошибок DiskType-репо.
type dtErrCtx struct {
	diskTypeID string
}

// mapDiskTypeErr транслирует pgx/pgconn-ошибку DiskType-репо в чистый ports-sentinel
// с контрактным текстом Kachō (Q4). FK RESTRICT со стороны volumes → "DiskType <id>
// is in use". Сырой pgx/SQL наружу не течёт (uncategorized → ports.ErrInternal).
func mapDiskTypeErr(err error, c dtErrCtx) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrNotFound), errors.Is(err, ports.ErrAlreadyExists),
		errors.Is(err, ports.ErrFailedPrecondition), errors.Is(err, ports.ErrInvalidArg),
		errors.Is(err, ports.ErrInternal):
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: DiskType %s not found", ports.ErrNotFound, c.diskTypeID)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation — дубликат PK-слага
			return fmt.Errorf("%w: DiskType %s already exists", ports.ErrAlreadyExists, c.diskTypeID)
		case "23503": // foreign_key_violation — volumes.disk_type_id RESTRICT (delete in-use, Q4)
			if pgErr.ConstraintName == cnVolumeDiskTypeFK {
				return fmt.Errorf("%w: DiskType %s is in use", ports.ErrFailedPrecondition, c.diskTypeID)
			}
			return fmt.Errorf("%w: disk type violates a reference constraint", ports.ErrFailedPrecondition)
		case "23514": // check_violation (description length / zone_ids array)
			return fmt.Errorf("%w: Illegal argument", ports.ErrInvalidArg)
		}
		slog.Error("uncategorized postgres error mapped to internal",
			"sqlstate", pgErr.Code, "constraint", pgErr.ConstraintName, "disk_type_id", c.diskTypeID)
		return ports.ErrInternal
	}
	slog.Error("uncategorized db error mapped to internal", "err", err.Error(), "disk_type_id", c.diskTypeID)
	return ports.ErrInternal
}
