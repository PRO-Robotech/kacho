// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"

	"github.com/PRO-Robotech/kacho/pkg/refusal"
)

// Отказ на снятие доезжает до клиента ПАРОЙ: код И машинный признак полосы.
//
// Утверждать один код недостаточно — им отвечают и отказ учёта, и всякое другое
// предусловие, и по нему «ресурс защищён» неотличим от «на ресурс ссылаются».
// Утверждать один признак недостаточно тоже: признак без кода не говорит
// вызывающему, повторять ли запрос.
//
// ПОЧЕМУ ПРОБА ПАРНАЯ. Отрицательная половина — отказ предусловия БЕЗ полосы:
// он обязан вести себя ровно как прежде, кодом и текстом. Без неё «признак
// ставится» зеленело бы и на мапперe, который лепит признак всему подряд, то
// есть перестал бы что-либо различать.
func TestDeletionRefusalCarriesTheLaneAndTheCode(t *testing.T) {
	t.Parallel()

	reasonOf := func(err error) *errdetails.ErrorInfo {
		t.Helper()
		for _, d := range status.Convert(err).Details() {
			if info, ok := d.(*errdetails.ErrorInfo); ok {
				return info
			}
		}
		return nil
	}

	cases := []struct {
		name     string
		lane     refusal.Lane
		wantKind string
	}{
		{"защита от удаления", refusal.Protected, ""},
		{"контейнер держит своих", refusal.HoldsChildren, "children"},
		{"на лист ссылаются чужие", refusal.ReferredTo, "referrers"},
		{"зависимые есть, вид не назван", refusal.ReferredUnnamed, "unnamed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := refusal.Wrap(tc.lane, refusal.Ref{ResourceType: "thing", ResourceID: "id-1"},
				fmt.Errorf("%w: Volume is in use", storageerr.ErrFailedPrecondition))
			got := ToStatus(src)

			require.Equal(t, codes.FailedPrecondition, status.Code(got),
				"код полосы обязан остаться кодом предусловия")
			require.Equal(t, "Volume is in use", status.Convert(got).Message(),
				"признак не вправе трогать контрактный текст")

			info := reasonOf(got)
			require.NotNil(t, info, "признак полосы не доехал в детали ответа")
			require.Equal(t, tc.lane.Token(), info.GetReason())
			require.Equal(t, serviceDomain+".kacho.cloud", info.GetDomain(),
				"домен отказа обязан называться так же, как у соседних полос этой службы")
			require.Equal(t, "thing", info.GetMetadata()["resource_type"])
			require.Equal(t, "id-1", info.GetMetadata()["resource_id"])
			if tc.wantKind == "" {
				require.NotContains(t, info.GetMetadata(), "reference_kind",
					"вид ссылки назван там, где ссылок нет вовсе")
			} else {
				require.Equal(t, tc.wantKind, info.GetMetadata()["reference_kind"])
			}
		})
	}

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: предусловие без полосы ведёт себя как прежде.
	plain := ToStatus(fmt.Errorf("%w: Volume is in use", storageerr.ErrFailedPrecondition))
	require.Equal(t, codes.FailedPrecondition, status.Code(plain))
	require.Equal(t, "Volume is in use", status.Convert(plain).Message())
	require.Nil(t, reasonOf(plain),
		"признак появился на отказе, полосу которого производитель не называл")

	t.Logf("осмотрено: полос %d, отрицательных контролей 1", len(cases))
}
