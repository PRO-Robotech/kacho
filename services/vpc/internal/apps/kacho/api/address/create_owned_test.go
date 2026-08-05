// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

// Адрес, заказанный не тенантом напрямую, а платформой ПОД конкретного
// владельца, рождается уже привязанным — вставка, аллокация IP и referrer-запись
// живут в ОДНОЙ writer-TX.
//
// # Почему это свойство, а не удобство
//
// Пока привязка была отдельным запросом, она гейтилась на объекте, которого в
// момент начала операции не существовало: доступ создателя к своему свежему
// ресурсу материализуется вне мутации, поэтому второй вызов упирался в окно
// видимости. Вызывающий (сага балансировщика) крутил ограниченный ретрай и, когда
// окно оказывалось шире бюджета, получал отказ на СВОЙ ЖЕ адрес, выделенный
// мгновение назад, — а половина уже выполненной работы требовала компенсации.
//
// Здесь проверяется ровно то, что снимает зависимость: привязка выполнена ТОЙ ЖЕ
// транзакцией, что и создание. Поэтому у теста две половины, и вторая — не
// украшение: без неё утверждение «привязано» не отличает «привязано в той же
// транзакции» от «привязано следом вторым запросом».
//
//  1. успех: адрес создан, referrer-строка видна после коммита, `used` взведён;
//  2. откат: создание, упавшее ПОСЛЕ вставки, не оставляет ни адреса, ни
//     referrer-строки — то есть привязка не «дописывается сбоку», а разделяет
//     судьбу транзакции.
//
// Третья половина — контроль «без владельца ничего не меняется»: путь тенанта
// обязан остаться прежним, иначе изменение расширяет поведение молча.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

func TestCreateUseCase_OwnedAddressIsLinkedInTheSameTransaction(t *testing.T) {
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	uc := NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil)
	listUC := NewListAddressesUseCase(kr, nil)

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "vip-owned",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.40"},
		Owner: &AddressOwner{
			ReferrerType: "nlb_network_load_balancer",
			ReferrerID:   "nlbowned00000000001",
			ReferrerName: "lb-owned",
			Owned:        true,
		},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	addrs, _, _ := listUC.Execute(context.Background(), "", AddressFilter{ProjectID: "f1"}, Pagination{})
	require.Len(t, addrs, 1)
	require.True(t, addrs[0].Used,
		"адрес, рождённый под владельца, использован с момента существования")

	ref, err := mustReader(t, kr).Addresses().GetReference(context.Background(), addrs[0].ID)
	require.NoError(t, err, "referrer-строка обязана быть видна тем же коммитом, что и адрес")
	require.Equal(t, "nlb_network_load_balancer", ref.ReferrerType)
	require.Equal(t, "nlbowned00000000001", ref.ReferrerID)
	require.True(t, ref.Owned, "владелец распоряжается жизненным циклом адреса")
}

// Откат: если транзакция создания не дошла до коммита, привязки не остаётся
// тоже. Это и есть разница между «в одной транзакции» и «вторым запросом».
func TestCreateUseCase_OwnedAddressLinkRollsBackWithTheTransaction(t *testing.T) {
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	kr.SetOutboxEmitErr(errors.New("outbox down")) // отказ ПОСЛЕ Insert и привязки, до Commit
	uc := NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil)
	listUC := NewListAddressesUseCase(kr, nil)

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "vip-rollback",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.41"},
		Owner: &AddressOwner{
			ReferrerType: "nlb_network_load_balancer",
			ReferrerID:   "nlbrollback00000001",
			Owned:        true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, repomock.AwaitOpDone(t, or, op.ID).Error,
		"предусловие: операция обязана завершиться ошибкой")

	addrs, _, _ := listUC.Execute(context.Background(), "", AddressFilter{ProjectID: "f1"}, Pagination{})
	require.Empty(t, addrs, "откат снимает вставку адреса")
	_, err = mustReader(t, kr).Addresses().GetReference(context.Background(), "любой")
	require.Error(t, err, "привязки без адреса не остаётся")
	require.Zero(t, kr.ReferenceCount(),
		"ни одной referrer-строки: привязка разделила судьбу транзакции")
}

// Контроль в другую сторону: без владельца путь тенанта не изменился —
// адрес рождается зарезервированным и НЕ использованным, привязки нет.
func TestCreateUseCase_WithoutOwnerNothingIsLinked(t *testing.T) {
	kr := kachomock.NewRepository()
	sr := repomock.NewSubnetRepo()
	or := repomock.NewOpsRepo()
	uc := NewCreateAddressUseCase(kr, sr, &repomock.ProjectClient{OK: true}, or, nil)
	listUC := NewListAddressesUseCase(kr, nil)

	op, err := uc.Execute(context.Background(), CreateInput{
		ProjectID:    "f1",
		Name:         "vip-plain",
		ExternalSpec: &ExternalAddrSpec{Address: "203.0.113.42"},
	})
	require.NoError(t, err)
	require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)

	addrs, _, _ := listUC.Execute(context.Background(), "", AddressFilter{ProjectID: "f1"}, Pagination{})
	require.Len(t, addrs, 1)
	require.True(t, addrs[0].Reserved, "путь тенанта прежний: адрес зарезервирован")
	require.False(t, addrs[0].Used, "никто на него не ссылается")
	require.Zero(t, kr.ReferenceCount(), "привязки нет")
}

// mustReader — читатель mock-репозитория; отдельным помощником, чтобы проба
// утверждала предмет, а не разбирала пару возвратов на каждой строке.
func mustReader(t *testing.T, r *kachomock.Repository) kacho.RepositoryReader {
	t.Helper()
	rd, err := r.Reader(context.Background())
	require.NoError(t, err)
	return rd
}
