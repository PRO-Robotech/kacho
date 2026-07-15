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
