// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

func reasonTokenOf(t *testing.T, err error) string {
	t.Helper()
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

// Готовая полоса проходит маппер НЕТРОНУТОЙ. Без этого peer-клиент, собравший
// полосу сам, терял бы её здесь: собранный статус не совпадает ни с одним
// sentinel'ом и уезжал бы в общую ветку INTERNAL — то есть машинный признак
// пропадал бы ровно на пути к клиенту, а отказ переставал быть отличимым.
func TestPeerErrToStatus_PassesFormedLaneThrough(t *testing.T) {
	lane := kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: "nlb", ResourceType: "geo.region", ResourceID: "reg-x"},
		"Region reg-x not found")

	got := PeerErrToStatus(lane, "region", "reg-x")

	require.Equal(t, codes.FailedPrecondition, status.Code(got))
	require.Equal(t, "Region reg-x not found", status.Convert(got).Message(),
		"проза не обрастает kind-префиксом и текстом внутреннего sentinel")
	require.Equal(t, "PEER_RESOURCE_MISSING", reasonTokenOf(t, got))
}

// Положительный контроль к предыдущему: sentinel-полосы, которые маппер
// собирает САМ, продолжают работать. Без этой пары «проход насквозь» зеленел бы
// и на мапере, который вообще перестал классифицировать.
func TestPeerErrToStatus_StillClassifiesSentinels(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{"not found", fmt.Errorf("%w: x", domain.ErrNotFound), codes.InvalidArgument},
		{"invalid arg", fmt.Errorf("%w: x", domain.ErrInvalidArg), codes.InvalidArgument},
		{"failed precondition", fmt.Errorf("%w: x", domain.ErrFailedPrecondition), codes.FailedPrecondition},
		{"unavailable", fmt.Errorf("%w: x", domain.ErrUnavailable), codes.Unavailable},
		{"unclassified", fmt.Errorf("boom"), codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, status.Code(PeerErrToStatus(tc.err, "project", "p1")))
		})
	}
}

// nil остаётся nil — маппер не превращает успех в отказ.
func TestPeerErrToStatus_NilStaysNil(t *testing.T) {
	require.NoError(t, PeerErrToStatus(nil, "project", "p1"))
}
