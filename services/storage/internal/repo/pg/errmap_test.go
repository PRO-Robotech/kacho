// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	stderrors "errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestMapVolumeErrLeakGuard — некатегоризированный SQLSTATE → ports.ErrInternal, и
// serviceerr.ToStatus отдаёт РОВНО "internal error" без утечки pgx/connection-текста
// (S2-10, security.md hardening-инвариант #1; behaviour-level, не только код).
func TestMapVolumeErrLeakGuard(t *testing.T) {
	raw := &pgconn.PgError{
		Code:    "XX000", // internal_error class — не в whitelist mapper'а
		Message: "connection to server at \"10.0.0.7\", port 5432 failed: FATAL: password for user \"storage\"",
	}
	mapped := mapVolumeErr(raw, volErrCtx{volumeID: "vol00000000000000001"})
	require.True(t, stderrors.Is(mapped, ports.ErrInternal), "uncategorized SQLSTATE → ErrInternal")

	st := serviceerr.ToStatus(mapped)
	require.Equal(t, codes.Internal, status.Code(st))
	require.Equal(t, "internal error", status.Convert(st).Message(), "fixed opaque text")
	require.NotContains(t, status.Convert(st).Message(), "10.0.0.7", "no host leak")
	require.NotContains(t, status.Convert(st).Message(), "password", "no credential leak")
	require.NotContains(t, status.Convert(st).Message(), "storage", "no db/user leak")
}

// TestMapVolumeErrDeviceCollision — 23505 на device-UNIQUE → FailedPrecondition с
// точным контрактным текстом device+instance (S2-06).
func TestMapVolumeErrDeviceCollision(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "volume_attachments_instance_device_uniq"}
	mapped := mapVolumeErr(pgErr, volErrCtx{deviceName: "sdb", instanceID: "ins-1"})
	require.True(t, stderrors.Is(mapped, ports.ErrFailedPrecondition), "got %v", mapped)
	require.Equal(t, "device sdb is already in use on Instance ins-1",
		mapped.Error()[len("failed precondition: "):])
}

// TestMapVolumeErrSecondBoot — 23P01 на boot-EXCLUDE → FailedPrecondition с точным
// контрактным текстом instance (S2-07).
func TestMapVolumeErrSecondBoot(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23P01", ConstraintName: "volume_attachments_one_boot"}
	mapped := mapVolumeErr(pgErr, volErrCtx{instanceID: "ins-1"})
	require.True(t, stderrors.Is(mapped, ports.ErrFailedPrecondition), "got %v", mapped)
	require.Equal(t, "Instance ins-1 already has a boot volume",
		mapped.Error()[len("failed precondition: "):])
}

// TestMapVolumeErrSourceImageFK — STOR-1-19: 23503 на volumes_source_image_fk →
// FailedPrecondition "Image <id> not found" (контрактный тон).
func TestMapVolumeErrSourceImageFK(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503", ConstraintName: "volumes_source_image_fk"}
	mapped := mapVolumeErr(pgErr, volErrCtx{imageID: "img00000000000000001"})
	require.True(t, stderrors.Is(mapped, ports.ErrFailedPrecondition), "got %v", mapped)
	require.Equal(t, "Image img00000000000000001 not found", mapped.Error()[len("failed precondition: "):])
}

// TestMapImageErrLeakGuard — STOR-1-26: некатегоризированный SQLSTATE Image-репо →
// ports.ErrInternal, serviceerr → РОВНО "internal error" без утечки pgx/connection-текста
// (security.md hardening-инвариант #1; behaviour-level, не только код).
func TestMapImageErrLeakGuard(t *testing.T) {
	raw := &pgconn.PgError{
		Code:    "XX000",
		Message: "connection to server at \"10.0.0.9\", port 5432 failed: FATAL: password for user \"storage\"",
	}
	mapped := mapImageErr(raw, imgErrCtx{imageID: "img00000000000000001"})
	require.True(t, stderrors.Is(mapped, ports.ErrInternal), "uncategorized SQLSTATE → ErrInternal")

	st := serviceerr.ToStatus(mapped)
	require.Equal(t, codes.Internal, status.Code(st))
	require.Equal(t, "internal error", status.Convert(st).Message(), "fixed opaque text")
	require.NotContains(t, status.Convert(st).Message(), "10.0.0.9", "no host leak")
	require.NotContains(t, status.Convert(st).Message(), "password", "no credential leak")
}

// TestMapImageErrSourceFK — STOR-1-24: 23503 на images_source_snapshot_id_fkey /
// images_source_volume_id_fkey → FailedPrecondition "<Resource> <id> not found".
func TestMapImageErrSourceFK(t *testing.T) {
	snapFK := &pgconn.PgError{Code: "23503", ConstraintName: "images_source_snapshot_id_fkey"}
	mapped := mapImageErr(snapFK, imgErrCtx{snapshotID: "snp00000000000000001"})
	require.True(t, stderrors.Is(mapped, ports.ErrFailedPrecondition), "got %v", mapped)
	require.Equal(t, "Snapshot snp00000000000000001 not found", mapped.Error()[len("failed precondition: "):])

	volFK := &pgconn.PgError{Code: "23503", ConstraintName: "images_source_volume_id_fkey"}
	mapped = mapImageErr(volFK, imgErrCtx{volumeID: "vol00000000000000001"})
	require.True(t, stderrors.Is(mapped, ports.ErrFailedPrecondition), "got %v", mapped)
	require.Equal(t, "Volume vol00000000000000001 not found", mapped.Error()[len("failed precondition: "):])
}

// TestMapImageErrNameUnique — STOR-1-21: 23505 на images_name_uniq → AlreadyExists с
// контрактным текстом.
func TestMapImageErrNameUnique(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "images_name_uniq"}
	mapped := mapImageErr(pgErr, imgErrCtx{imageName: "ubuntu-24-04"})
	require.True(t, stderrors.Is(mapped, ports.ErrAlreadyExists), "got %v", mapped)
	require.Equal(t, "image with name ubuntu-24-04 already exists in project",
		mapped.Error()[len("already exists: "):])
}
