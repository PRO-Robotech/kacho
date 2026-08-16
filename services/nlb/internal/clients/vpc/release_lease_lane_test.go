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

// Полосы ответа владельца на СНЯТИЕ АРЕНДЫ
// (`InternalAddressService.ReleaseOwnedAddress`).
//
// Файл переименован из полосы `ClearAddressReference` вместе со своим предметом:
// пара «снять ссылку» + «удалить адрес» заменена одним глаголом (#439). Все
// утверждения ниже пережили замену и относятся к нему; меняется вызываемый
// метод и ОДНО существенное различие, названное у своей пробы: полосы «нет
// ресурса» у нового глагола нет, потому что отсутствие он сообщает ПОЛЕМ ответа,
// а не кодом ошибки.
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
//     изолирует) — уже утверждено в `jobs/free_ip_runner_integration_test.go`.
//     Шов между половинами — сам sentinel.
//
// У каждой отрицательной полосы есть парный положительный контроль: без него
// «не тот sentinel» зеленело бы на клиенте, который свёл все ответы в один.

func releaseReq() ReleaseLeaseRequest {
	return ReleaseLeaseRequest{
		ProjectID: "prj-1",
		AddressID: "adr7tp1q22pfqey44m4m",
		Owner:     AddressOwner{Kind: OwnerKindLoadBalancer, ID: "nlb7tp1q22pfqey44m4m"},
	}
}

// Недоступность модели прав у владельца — переходное состояние, а не приговор
// аренде. Полоса обязана быть транзиентной, иначе освобождение аренды
// прекращается навсегда из-за перебоя длиной в доли секунды.
func TestReleaseLease_AuthzOutage_IsTransientLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		releaseErr: status.Error(codes.Unavailable, "authorization service unavailable"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

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
func TestReleaseLease_PermissionDenied_IsNamedTerminalLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		releaseErr: status.Error(codes.PermissionDenied, "permission denied"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition),
		"решение модели названо полосой состояния: терминально и различимо")
	assert.False(t, errors.Is(err, domain.ErrUnavailable),
		"отказ в правах НЕ транзиентный — вечный повтор был бы ошибкой в другую сторону")
}

// Предъявленное владение не подтвердилось (аренда чужая либо адрес другого
// проекта) — терминально и названо.
func TestReleaseLease_FailedPrecondition_IsNamedTerminalLane(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		releaseErr: status.Error(codes.FailedPrecondition, "address is not leased by network_load_balancer nlb-x"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
	assert.False(t, errors.Is(err, domain.ErrUnavailable))
}

// Ответ, которому носитель полосы не назначил (внутренняя ошибка владельца,
// исчерпание бюджета проверок, нереализованный метод), — это СОСТОЯНИЕ «ответ не
// понят», а не третья политика повтора. Он обязан быть терминальным и НЕ выдавать
// себя ни за недоступность (вечный повтор), ни за успех (тихая потеря аренды).
func TestReleaseLease_UnclassifiedPeerAnswer_IsNeitherTransientNorSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		peer error
	}{
		{"внутренняя ошибка владельца", status.Error(codes.Internal, "boom")},
		{"бюджет проверок исчерпан", status.Error(codes.ResourceExhausted, "too many authorization checks")},
		{"метод не реализован", status.Error(codes.Unimplemented, "no such method")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intAddr := &fakeInternalAddressService{releaseErr: tc.peer}
			conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

			out, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

			require.Error(t, err, "непонятый ответ не может означать «аренда снята»")
			assert.False(t, errors.Is(err, domain.ErrUnavailable),
				"непонятый ответ не объявляется повторяемым: вечный повтор дороже отказа")
			assert.Empty(t, out, "исход не называется там, где работа не сделана")
		})
	}
}

// ЗДЕСЬ ПОЛОСА ПЕРЕВЁРНУТА ОТНОСИТЕЛЬНО СНЯТОГО ПРЕДМЕТА, и это главное различие
// между старым глаголом и новым.
//
// У `ClearAddressReference` ответ «не найдено» законно означал «снимать нечего»:
// постусловие выполнено. У `ReleaseOwnedAddress` он не означает НИЧЕГО о
// состоянии аренды — глагол этой полосы не производит вовсе, а «аренды уже нет»
// приезжает НАЗВАННЫМ ИСХОДОМ в поле ответа. Значит получить `NOT_FOUND` можно
// только говоря не с тем глаголом (владелец не перекатан, поверхность не та), и
// это НАСТРОЙКА. Прочитать её как «уже снято» значит вернуть ровно тот дефект,
// ради которого глагол заведён (#439).
func TestReleaseLease_NotFound_IsRefusalNotSilentSuccess(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		releaseErr: status.Error(codes.NotFound, "unknown method"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	out, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

	require.Error(t, err, "«не найдено» НЕ доказывает, что аренда снята")
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
	assert.Empty(t, out, "исход не называется там, где работа не сделана")
}

// Положительный контроль набора: негодная ссылка остаётся негодной.
func TestReleaseLease_InvalidArgument_StaysIllegalArgument(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		releaseErr: status.Error(codes.InvalidArgument, "bad address id"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg))
	assert.False(t, errors.Is(err, domain.ErrUnavailable))
}

// Положительный контроль набора: чистый ответ — НАЗВАННЫЙ исход.
//
// Без него все отрицания выше зеленели бы на клиенте, который отвечает ошибкой
// вообще всегда.
func TestReleaseLease_Success(t *testing.T) {
	intAddr := &fakeInternalAddressService{}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	out, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

	require.NoError(t, err)
	assert.Equal(t, LeaseReleased, out)
	require.Len(t, intAddr.releaseCalls, 1)
}

// Проза владельца наружу через sentinel-обёртку не течёт: адрес назван,
// внутренности решения о доступе — нет (security.md §Hardening-инварианты п.1).
func TestReleaseLease_PeerProseDoesNotLeak(t *testing.T) {
	intAddr := &fakeInternalAddressService{
		releaseErr: status.Error(codes.PermissionDenied, "relation editor on project denied"),
	}
	conn := startFakeVPC(t, nil, nil, nil, intAddr, nil)

	_, err := NewInternalAddressClient(conn, conn).ReleaseLease(ctxBackground(), releaseReq())

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "relation editor",
		"полоса отказа не пересказывает внутренности решения о доступе у владельца")
}
