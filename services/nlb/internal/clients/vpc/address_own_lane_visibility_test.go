// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Публичный AddressClient.Get отвечает на ТУ ЖЕ пару кодов, которую этот пакет
// уже объявил невидимостью собственного свежего ресурса — `ownResourceInvisible`
// (internal_address_client.go): vpc прячет существование через NOT_FOUND там, где
// скрывает, и отвечает PERMISSION_DENIED там, где не скрывает.
//
// Схлопывать их в `ErrInvalidArg` — значит утверждать «аргумент незаконен» про
// well-formed id существующего ресурса и лишать вызывающего единственной полосы,
// по которой он мог бы ретраить (api-conventions §By-lane code-split). Полоса
// отсутствия у владельца — `ErrNotFound`; `ErrInvalidArg` остаётся за тем, что
// повтором не лечится.

func TestAddressClient_Get_NotFound_IsPeerMissLaneNotIllegalArgument(t *testing.T) {
	conn := startFakeVPC(t, nil, nil, &fakeAddressService{err: status.Error(codes.NotFound, "no address")}, nil, nil)
	_, err := NewAddressClient(conn).Get(context.Background(), "adr7tp1q22pfqey44m4m")

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound),
		"hide-existence NOT_FOUND — отсутствие у владельца, полоса peer-validate")
	assert.False(t, errors.Is(err, domain.ErrInvalidArg),
		"well-formed id существующего ресурса не является незаконным аргументом")
}

func TestAddressClient_Get_PermissionDenied_IsPeerMissLaneNotIllegalArgument(t *testing.T) {
	conn := startFakeVPC(t, nil, nil, &fakeAddressService{err: status.Error(codes.PermissionDenied, "scope")}, nil, nil)
	_, err := NewAddressClient(conn).Get(context.Background(), "adr7tp1q22pfqey44m4m")

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound),
		"отказ пообъектного authz на own-lane значит ровно то же, что hide-existence")
	assert.False(t, errors.Is(err, domain.ErrInvalidArg))
}

// Контроль в другую сторону: то, что повтором не лечится, полосу не меняет.
func TestAddressClient_Get_InvalidArgument_StaysIllegalArgument(t *testing.T) {
	conn := startFakeVPC(t, nil, nil, &fakeAddressService{err: status.Error(codes.InvalidArgument, "bad id")}, nil, nil)
	_, err := NewAddressClient(conn).Get(context.Background(), "adr7tp1q22pfqey44m4m")

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg))
	assert.False(t, errors.Is(err, domain.ErrNotFound))
}
