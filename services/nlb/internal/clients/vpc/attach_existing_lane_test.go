// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Полосы ответа владельца на link-CAS (`InternalAddressService.SetAddressReference`).
//
// Публичный `AddressClient.Get` этого же пакета полосы уже развёл
// (`address_own_lane_visibility_test.go`): промах и отказ в правах — одно
// («объект мне сейчас не виден», `ErrNotFound`), негодная по мнению владельца
// ссылка — другое (`ErrInvalidArg`, повтором не лечится). `AttachExisting`
// разбирал ответ соседа СВОИМ рукописным `switch` и сводил ТРИ кода в один
// sentinel — то есть отвечал «аргумент незаконен» и на отказ в правах.
//
// Цена схлопывания измерима: у вызывающего не остаётся признака, по которому
// сработал бы ограниченный повтор окна материализации (api-conventions.md
// §By-lane code-split; ban #9 — «создал→сразу мутирую» закрывается повтором
// КЛИЕНТА, не серверным барьером), а в разборе красноты четыре несвязанные
// причины дают один и тот же текст.
//
// Ниже — полоса на каждый код владельца, и у каждой отрицательной есть парный
// положительный контроль: без него «не тот sentinel» зеленело бы на всём.

// attachClient — реальный клиент со СЖАТОЙ каденцией окна видимости. Сжимается
// бюджет, а не подменяется петля: подменённая петля утверждала бы о своей копии.
func attachClient(t *testing.T, conn *grpc.ClientConn) InternalAddressClient {
	t.Helper()
	c, ok := NewInternalAddressClient(conn, conn).(*internalAddressClient)
	require.True(t, ok)
	c.linkBackoff = time.Millisecond
	return c
}

func attachReq() AttachExistingRequest {
	return AttachExistingRequest{
		AddressID: "adr7tp1q22pfqey44m4m",
		Owner:     AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	}
}

// Отказ в правах на link-CAS — это окно материализации пообъектного доступа к
// адресу, а не приговор аргументу. Полоса промаха, повторяемая.
func TestAttachExisting_PermissionDenied_IsPeerMissLaneNotIllegalArgument(t *testing.T) {
	intAddr := &fakeInternalAddressService{setErr: status.Error(codes.PermissionDenied, "no path")}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound),
		"отказ пообъектного authz значит ровно то же, что hide-existence: объект мне не виден")
	assert.False(t, errors.Is(err, domain.ErrInvalidArg),
		"well-formed id существующего ресурса не является незаконным аргументом вызывающего")
}

func TestAttachExisting_NotFound_IsPeerMissLaneNotIllegalArgument(t *testing.T) {
	intAddr := &fakeInternalAddressService{setErr: status.Error(codes.NotFound, "no address")}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound),
		"hide-existence NOT_FOUND — отсутствие у владельца, полоса peer-validate")
	assert.False(t, errors.Is(err, domain.ErrInvalidArg))
}

// Положительный контроль полосы: то, что повтором не лечится, полосу не меняет.
// Без него отрицания выше зеленели бы на маппере, который всё свёл в ErrNotFound.
func TestAttachExisting_InvalidArgument_StaysIllegalArgument(t *testing.T) {
	intAddr := &fakeInternalAddressService{setErr: status.Error(codes.InvalidArgument, "bad referrer")}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg),
		"негодную по мнению владельца ссылку повтор не исправит — полоса терминальная")
	assert.False(t, errors.Is(err, domain.ErrNotFound))
}

// Проигранный CAS (адрес уже занят) — состояние чужого ресурса, не его отсутствие.
func TestAttachExisting_AlreadyExists_StaysFailedPrecondition(t *testing.T) {
	intAddr := &fakeInternalAddressService{setErr: status.Error(codes.AlreadyExists, "used")}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
	assert.False(t, errors.Is(err, domain.ErrNotFound))
	assert.False(t, errors.Is(err, domain.ErrInvalidArg))
}

// Недоступность владельца — единственная полоса, где он не установил НИЧЕГО.
func TestAttachExisting_Unavailable_StaysTransient(t *testing.T) {
	intAddr := &fakeInternalAddressService{setErr: status.Error(codes.Unavailable, "vpc down")}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable))
	assert.False(t, errors.Is(err, domain.ErrNotFound))
	assert.False(t, errors.Is(err, domain.ErrInvalidArg))
}

// Проза соседа не течёт наружу через sentinel-обёртку: адрес назван, ответ
// владельца — нет (security.md §Hardening #1 — фиксированный текст, без
// подробностей чужой реализации).
func TestAttachExisting_PeerProseDoesNotLeakOnMissLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		setErr: status.Error(codes.PermissionDenied, "relation v_update on vpc_address denied"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "v_update",
		"полоса промаха не пересказывает внутренности решения о доступе у владельца")
}

// ---- бюджет окна видимости: повторяется РОВНО одна полоса --------------------
//
// Замер стенда 2026-08-16 (прогон e2e 31965970965) показал, что окно у чужого,
// уже существующего адреса реально: пообъектный доступ к нему материализуется
// ПО ЧАСТЯМ, и `SetAddressReference` попадал в промежуток. Терминальный ответ на
// такой промежуток убивал здоровое создание балансировщика.
//
// Повторяется поэтому только полоса промаха. Проверяется это СЧЁТОМ вызовов у
// владельца, а не наличием паузы: «повторили» и «не повторили» иначе неотличимы.

// Окно, которое закрывается само, повтор закрывает — и привязка проходит.
func TestAttachExisting_VisibilityWindowThatClosesIsRetried(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		setErr:      status.Error(codes.NotFound, "hide-existence"),
		setErrTimes: 3,
	}
	addrs := &fakeAddressService{resp: &vpcpb.Address{
		Id: "adr7tp1q22pfqey44m4m",
		Address: &vpcpb.Address_InternalIpv4Address{
			InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.0.0.9"},
		},
	}}
	conn := startFakeVPC(t, nil, nil, addrs, intAddr, nil)

	resp, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.NoError(t, err, "окно материализации закрылось — привязка обязана пройти")
	assert.Equal(t, "10.0.0.9", resp.Value)
	assert.Equal(t, 4, intAddr.setCallCount(),
		"три отказа окна и один успех: бюджет тратится ровно на промах")
}

// Окно, которое не закрылось за бюджет, отвечает СВОЕЙ полосой и не выдаёт себя
// ни за незаконный аргумент, ни за недоступность платформы. Бюджет конечен —
// это утверждается счётом.
func TestAttachExisting_VisibilityWindowThatNeverCloses_StaysPeerMissAndBounded(t *testing.T) {
	intAddr := &fakeInternalAddressService{setErr: status.Error(codes.NotFound, "hide-existence")}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
	assert.Equal(t, linkVisibilityAttempts, intAddr.setCallCount(),
		"ожидание ограничено бюджетом, а не длится, пока не надоест")
}

// Положительный контроль бюджета: терминальные полосы НЕ повторяются. Без него
// «повторяем промах» зеленело бы на клиенте, который повторяет вообще всё, — а
// он оттягивал бы каждый законный отказ на весь бюджет.
func TestAttachExisting_TerminalLanesAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		name string
		peer error
	}{
		{"негодная ссылка", status.Error(codes.InvalidArgument, "bad referrer")},
		{"адрес занят", status.Error(codes.AlreadyExists, "used")},
		{"состояние не позволяет", status.Error(codes.FailedPrecondition, "not linkable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intAddr := &fakeInternalAddressService{setErr: tc.peer}
			conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

			_, err := attachClient(t, conn).AttachExisting(ctxBackground(), attachReq())

			require.Error(t, err)
			assert.Equal(t, 1, intAddr.setCallCount(),
				"повтор идентичного запроса этот отказ не изменит — ждать нечего")
		})
	}
}
