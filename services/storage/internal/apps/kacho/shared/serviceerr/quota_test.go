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
)

// Отказ учёта наружу: код И машинный признак, а не один код.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-3 и DoD S4 п.1.
//
// Утверждается ПАРА, потому что кода не хватает: `RESOURCE_EXHAUSTED` придёт и
// от предела ЧИСЛА ресурсов, и от предела ОБЪЁМА — это разные оси (V2-11), и
// арендатору они велят разное («удали лишний том» против «возьми том поменьше»).
// Различает их `reason`-токен; проза для этого непригодна
// (`api-conventions.md` §By-lane code-split).

// reasonOf достаёт признак из деталей статуса; пустая строка — признака нет.
func reasonOf(t *testing.T, err error) string {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "ошибка обязана быть gRPC-статусом")
	for _, d := range st.Details() {
		if info, isInfo := d.(*errdetails.ErrorInfo); isInfo {
			return info.GetReason()
		}
	}
	return ""
}

func domainOf(t *testing.T, err error) string {
	t.Helper()
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if info, isInfo := d.(*errdetails.ErrorInfo); isInfo {
			return info.GetDomain()
		}
	}
	return ""
}

// TestQuotaRefusal_CodeAndReasonAreBothAsserted — три состояния, три разных
// ответа.
func TestQuotaRefusal_CodeAndReasonAreBothAsserted(t *testing.T) {
	t.Run("исчерпание", func(t *testing.T) {
		err := ToStatus(fmt.Errorf("%w: project prj-1 has reached its limit of 2 storage.volumes",
			storageerr.ErrQuotaExceeded))
		require.Equal(t, codes.ResourceExhausted, status.Code(err),
			"край отобразит это в 429")
		require.Equal(t, "QUOTA_EXCEEDED", reasonOf(t, err))
		require.Equal(t, "storage.kacho.cloud", domainOf(t, err))
		require.Equal(t, "project prj-1 has reached its limit of 2 storage.volumes",
			status.Convert(err).Message(),
			"текст производителя выносится ДОСЛОВНО: он и есть контракт — называет "+
				"носителя, предел и вид")
	})

	t.Run("потолок не назван", func(t *testing.T) {
		err := ToStatus(fmt.Errorf("%w: project prj-1 has no ceiling stated for storage.volumes",
			storageerr.ErrQuotaNotProvisioned))
		require.Equal(t, codes.FailedPrecondition, status.Code(err),
			"FAILED_PRECONDITION (край → 400), а не INVALID_ARGUMENT: ввод арендатора "+
				"корректен, не выполнено предусловие ПЛАТФОРМЫ")
		require.Equal(t, "QUOTA_NOT_PROVISIONED", reasonOf(t, err))
		require.NotEqual(t, "QUOTA_EXCEEDED", reasonOf(t, err),
			"признак отличим от исчерпания: администратору они велят разное — "+
				"завести потолок против поднять его")
	})

	t.Run("предел ОБЪЁМА остаётся своей осью", func(t *testing.T) {
		// Отрицательный контроль к паре выше: предел провизионированного объёма
		// даёт ТОТ ЖЕ код и НЕ несёт признака учёта числа. Без этого утверждения
		// «клиент различает оси по признаку» было бы словами: обе оси отвечают 429.
		err := ToStatus(fmt.Errorf("%w: project provisioned bytes limit reached",
			storageerr.ErrResourceExhausted))
		require.Equal(t, codes.ResourceExhausted, status.Code(err))
		require.NotEqual(t, "QUOTA_EXCEEDED", reasonOf(t, err),
			"признак учёта ЧИСЛА не приклеивается к отказу по ОБЪЁМУ — иначе клиент "+
				"пошёл бы удалять тома там, где надо взять том поменьше")
	})

	t.Run("положительный контроль: не отказ учёта — общая классификация", func(t *testing.T) {
		err := ToStatus(fmt.Errorf("%w: Volume vol-1 not found", storageerr.ErrNotFound))
		require.Equal(t, codes.NotFound, status.Code(err))
		require.Empty(t, reasonOf(t, err),
			"чужой ошибке признак учёта не приклеивается")
	})
}
