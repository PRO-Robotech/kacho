// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// reasonTokenOf — машинный признак полосы из деталей отказа; пустая строка,
// если детали нет. Отсутствие токена значимо и обязано быть отличимо от
// «токен не тот».
func reasonTokenOf(t *testing.T, err error) string {
	t.Helper()
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

// TestMapZoneRefErr_NotFound_PeerValidateLane — geo вернул NOT_FOUND (через
// ZoneRegistry.ErrNotFound) → полоса peer-validate: FailedPrecondition.
//
// Прежде здесь стоял InvalidArgument, и это была ЕДИНСТВЕННАЯ клетка общего
// ребра к geo, где код и текст утверждали разные полосы: код говорил «ввод
// неверен», а текст — контракт-тоном отсутствия ресурса. Теперь обе половины
// говорят одно, и клиент отличает полосу машинно.
func TestMapZoneRefErr_NotFound_PeerValidateLane(t *testing.T) {
	err := MapZoneRefErr(ErrNotFound, "no-such-zone")
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.FailedPrecondition, st.Code())
	require.Equal(t, "Zone no-such-zone not found", st.Message(),
		"текст — часть контракта и от смены полосы не меняется")
	require.Equal(t, "PEER_RESOURCE_MISSING", reasonTokenOf(t, err))
}

// TestMapZoneRefErr_GeoNotFoundStatus_PeerValidateLane — geo-клиент пробросил
// gRPC NOT_FOUND как status (не sentinel) → та же полоса. Две ветки одной
// функции обязаны отвечать одинаково: расхождение между ними и есть тот дефект,
// который в дереве уже встречался — один сервис, два кода, один текст.
func TestMapZoneRefErr_GeoNotFoundStatus_PeerValidateLane(t *testing.T) {
	err := MapZoneRefErr(status.Error(codes.NotFound, "Zone x not found"), "ru-central1-z")
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.FailedPrecondition, st.Code())
	require.Contains(t, st.Message(), "ru-central1-z")
	require.Equal(t, "PEER_RESOURCE_MISSING", reasonTokenOf(t, err))
}

// TestMapZoneRefErr_GeoDown_Unavailable — geo недоступен (transport-ошибка, не
// NOT_FOUND) → Unavailable "zone check: ..." (fail-closed на мутации Instance:
// peer недоступен → Unavailable, не «зона ок»), с машинным признаком своей
// полосы. Утечки сырого peer-текста по-прежнему нет.
func TestMapZoneRefErr_GeoDown_Unavailable(t *testing.T) {
	err := MapZoneRefErr(status.Error(codes.Unavailable, "connection refused to 10.4.2.7:9091"), "ru-central1-a")
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unavailable, st.Code())
	require.Contains(t, st.Message(), "zone check")
	require.Equal(t, "PEER_UNAVAILABLE", reasonTokenOf(t, err))
	// Raw peer transport detail (endpoint / dial error) must NOT be echoed to the
	// tenant — opaque message only (CWE-209), mirroring MapRepoErr discipline.
	require.NotContains(t, st.Message(), "connection refused")
	require.NotContains(t, st.Message(), "10.4.2.7")
}
