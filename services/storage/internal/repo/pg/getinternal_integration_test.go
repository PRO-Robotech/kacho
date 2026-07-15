// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestVolumeGetInternalUnimplemented — §0.4 anchor: infra-проекция (VolumeInternal)
// отсутствует в CS-1 (data-plane нет). Контрактный ответ GetInternal — codes.Unimplemented,
// НЕ tech-debt: путь осознанно out-of-scope. Lock фиксирует, что repo.GetInternal
// возвращает ErrUnimplemented и serviceerr маппит его в codes.Unimplemented.
func TestVolumeGetInternalUnimplemented(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	_, err := r.GetInternal(context.Background(), "vol00000000000000000")
	require.Error(t, err)
	require.True(t, stderrors.Is(err, ports.ErrUnimplemented), "got %v", err)
	require.Equal(t, codes.Unimplemented, status.Code(serviceerr.ToStatus(err)),
		"GetInternal is the contractual UNIMPLEMENTED answer of CS-1 (data-plane absent)")
}
