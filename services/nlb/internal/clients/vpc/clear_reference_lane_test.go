// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Полосы ответа владельца на снятие привязки
// (`InternalAddressService.ClearAddressReference`).
//
// Полосу ПРИВЯЗКИ уже развела соседняя проба (`attach_existing_lane_test.go`,
// задача #434). Здесь — полоса СНЯТИЯ, и у неё другой производитель отказа: не
// скрытие существования, а недоступность модели прав.
//
// Разбор ответа здесь был рукописным `switch` по кодам с веткой «всё остальное»,
// а корзины «прочее» у классификатора чужого отказа не бывает: она не нейтральна,
// она ВЫБИРАЕТ политику. Выбирала она терминальную — и это наблюдалось на стенде:
// звено решения о доступе у владельца отвечало на СВОЮ недоступность кодом отказа
// в правах, код попадал в корзину, а реконсайлер по правилу
// `jobs.isTransientReleaseErr` относил к транзиентным только `ErrUnavailable`.
// Итог: перебой модели прав на доли секунды ИЗОЛИРОВАЛ балансировщик как
// отравленный — необратимо для самого пути освобождения аренды.
//
// Что где утверждается, чтобы не утверждать дважды:
//   - полоса → sentinel — здесь;
//   - sentinel → судьба строки (транзиент оставляет строку на повтор, терминальное
//     изолирует) — уже утверждено в `jobs/free_ip_runner_integration_test.go`
//     (`TestFreeIP_TransientReleaseErrorLeavesRowForRetry` и соседняя проба про
//     отравленную строку). Шов между половинами — сам sentinel.
//
// У каждой отрицательной полосы есть парный положительный контроль: без него
// «не тот sentinel» зеленело бы на клиенте, который свёл все ответы в один.

func clearReq() string { return "adr7tp1q22pfqey44m4m" }

// Недоступность модели прав у владельца — переходное состояние, а не приговор
// привязке. Полоса обязана быть транзиентной, иначе освобождение аренды
// прекращается навсегда из-за перебоя длиной в доли секунды.
func TestClearReference_AuthzOutage_IsTransientLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		clearErr: status.Error(codes.Unavailable, "authorization service unavailable"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable),
		"владелец не смог принять решение — повтор осмыслен, строка обязана дожить до следующего тика")
	assert.False(t, errors.Is(err, domain.ErrInvalidArg))
	assert.False(t, errors.Is(err, domain.ErrFailedPrecondition))
}

// Отказ в правах — решение владельца, и оно терминально: повтор идентичного
// запроса его не изменит. Полоса обязана быть НАЗВАННОЙ, а не проваливаться в
// корзину «прочее»: у корзины нет ни имени, ни контракта, и следующий код,
// попавший в неё, унаследует политику, которую никто не выбирал.
func TestClearReference_PermissionDenied_IsNamedTerminalLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		clearErr: status.Error(codes.PermissionDenied, "permission denied"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition),
		"решение модели названо полосой состояния: терминально и различимо")
	assert.False(t, errors.Is(err, domain.ErrUnavailable),
		"отказ в правах НЕ транзиентный — вечный повтор был бы ошибкой в другую сторону")
}

// Состояние чужого ресурса не позволяет снять привязку — тоже терминально и тоже
// названо.
func TestClearReference_FailedPrecondition_IsNamedTerminalLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		clearErr: status.Error(codes.FailedPrecondition, "address is not linkable"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
	assert.False(t, errors.Is(err, domain.ErrUnavailable))
}

// Ответ, которому носитель полосы не назначил (внутренняя ошибка владельца,
// исчерпание бюджета проверок, нереализованный метод), — это СОСТОЯНИЕ «ответ не
// понят», а не третья политика повтора. Он обязан быть терминальным и НЕ выдавать
// себя ни за недоступность (вечный повтор), ни за успех (тихая потеря аренды).
func TestClearReference_UnclassifiedPeerAnswer_IsNeitherTransientNorSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		peer error
	}{
		{"внутренняя ошибка владельца", status.Error(codes.Internal, "boom")},
		{"бюджет проверок исчерпан", status.Error(codes.ResourceExhausted, "too many authorization checks")},
		{"метод не реализован", status.Error(codes.Unimplemented, "no such method")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intAddr := &fakeInternalAddressService{clearErr: tc.peer}
			conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

			err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

			require.Error(t, err, "непонятый ответ не может означать «привязка снята»")
			assert.False(t, errors.Is(err, domain.ErrUnavailable),
				"непонятый ответ не объявляется повторяемым: вечный повтор дороже отказа")
		})
	}
}

// Положительный контроль набора #1: привязки уже нет — постусловие выполнено.
// Без него все отрицания выше зеленели бы на клиенте, который отвечает ошибкой
// вообще всегда.
func TestClearReference_NotFound_StaysIdempotentSuccess(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		clearErr: status.Error(codes.NotFound, "no address"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

	assert.NoError(t, err, "снимать нечего — постусловие выполнено, это успех, а не отказ")
}

// Положительный контроль набора #2: негодная ссылка остаётся негодной.
func TestClearReference_InvalidArgument_StaysIllegalArgument(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		clearErr: status.Error(codes.InvalidArgument, "bad address id"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg))
	assert.False(t, errors.Is(err, domain.ErrUnavailable))
}

// Положительный контроль набора #3: чистый ответ — чистый успех.
func TestClearReference_Success(t *testing.T) {
	intAddr := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	require.NoError(t, NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq()))
	assert.Equal(t, 1, intAddr.clearCallCount())
}

// Проза владельца наружу через sentinel-обёртку не течёт: адрес назван, внутренности
// решения о доступе — нет (security.md §Hardening-инварианты п.1).
func TestClearReference_PeerProseDoesNotLeak(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		clearErr: status.Error(codes.PermissionDenied, "relation v_update on vpc_address denied"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	err := NewInternalAddressClient(conn, conn).ClearReference(ctxBackground(), clearReq())

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "v_update",
		"полоса отказа не пересказывает внутренности решения о доступе у владельца")
}
