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

func reasonOf(t *testing.T, err error) *errdetails.ErrorInfo {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok)
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	return nil
}

// Полоса peer-validate к geo: чужой идентификатор не резолвится у владельца.
// Код — FAILED_PRECONDITION (решение владельца §9 п.1 приёмки XC-6: консумер не
// «не нашёл своё», а «предусловие на чужой ресурс не выполнено»).
//
// Текст НЕ меняется: он часть контракта и утверждается дословно. Меняется код и
// добавляется машинный признак — то, по чему клиент отличает полосу, не парся прозу.
func TestUnknownZone_PeerValidateLane(t *testing.T) {
	err := UnknownZone("zone-z-fake")

	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "unknown zone id 'zone-z-fake'", status.Convert(err).Message())

	info := reasonOf(t, err)
	require.NotNil(t, info, "полоса обязана нести машинный признак")
	require.Equal(t, "PEER_RESOURCE_MISSING", info.GetReason())
	require.Equal(t, "vpc.kacho.cloud", info.GetDomain())
	require.Equal(t, "geo.zone", info.GetMetadata()["resource_type"])
	require.Equal(t, "zone-z-fake", info.GetMetadata()["resource_id"])
}

func TestUnknownRegion_PeerValidateLane(t *testing.T) {
	err := UnknownRegion("region-z-fake")

	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "unknown region id 'region-z-fake'", status.Convert(err).Message())

	info := reasonOf(t, err)
	require.NotNil(t, info)
	require.Equal(t, "PEER_RESOURCE_MISSING", info.GetReason())
	require.Equal(t, "geo.region", info.GetMetadata()["resource_type"])
	require.Equal(t, "region-z-fake", info.GetMetadata()["resource_id"])
}

// Код и текст обязаны утверждать ОДНУ полосу. Проверка стоит отдельным
// утверждением, потому что расхождение этих двух половин — самостоятельный
// дефект: ответ, машинно заявляющий одно, а прозой другое, нельзя ни отладить,
// ни автоматически разобрать.
func TestPeerLaneCodeAndTextAgree(t *testing.T) {
	for _, err := range []error{UnknownZone("z"), UnknownRegion("r")} {
		st := status.Convert(err)
		require.Equal(t, codes.FailedPrecondition, st.Code())
		require.NotContains(t, st.Message(), "not found",
			"контракт-тон отсутствия РЕСУРСА принадлежит own-полосе NOT_FOUND; "+
				"на peer-полосе текст говорит о неизвестной ССЫЛКЕ")
	}
}
