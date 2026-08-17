// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// АВТО-АЛЛОКАЦИЯ VIP БОЛЬШЕ НЕ ЗАВИСИТ ОТ ОКНА МАТЕРИАЛИЗАЦИИ.
//
// Раньше путь был парой: `AddressService.Create` коммитил строку адреса, а
// `InternalAddressService.SetAddressReference` привязывал её к балансировщику.
// Между этими вызовами адрес — уже СОБСТВЕННЫЙ закоммиченный ресурс nlb,
// поэтому отказ второго вызова не мог означать «такой id не резолвится»: это был
// ответ vpc, скрывающий существование, пока per-object доступ создателя ещё
// материализуется. Лечилось ограниченным клиентским ретраем — и упиралось в
// него же: когда окно оказывалось шире бюджета, здоровое создание
// балансировщика падало отказом на СВОЙ ЖЕ адрес, выделенный мгновение назад.
// Замер прогона CI 31002239590: 8 таких отказов утянули 44 каскадных
// утверждения (фикстура не получала id, дальше по кейсу рушилось всё).
//
// Поднимать бюджет ретрая значило бы маскировать. Снята сама зависимость: на
// пути ОДНА мутация — `CreateOwnedAddress`, — и решение о доступе принимается
// на project'е, который существует задолго до запроса. Второго решения на
// свежесозданном объекте нет, значит нет и окна, которое можно не дождаться.
//
// Пробы ниже утверждают СВОЙСТВО, а не механику:
//  1. на успешном пути ровно ОДНА мутация, и она несёт владельца;
//  2. отдельной привязки не делается вовсе — иначе зависимость от окна
//     вернулась бы незаметно, и именно это здесь краснеет;
//  3. отказ единственной мутации не оставляет ничего компенсировать: клиента
//     больше не просят возвращать half-allocated адрес в пул, потому что его
//     не бывает;
//  4. недоступность соседа остаётся transient-полосой (не отказом ёмкости) —
//     контракт, который был верен и до изменения и обязан пережить его.

func TestAllocateInternalIP_UsesOneMutationCarryingTheOwner(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{}
	intAddrSvc := &fakeInternalAddressService{
		createOwnedResp: &vpcpb.Address{
			Id: "adr-own-1",
			Address: &vpcpb.Address_InternalIpv4Address{
				InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.0.0.7"},
			},
		},
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	resp, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1", Name: "lb-one"},
	})
	require.NoError(t, err)
	require.Equal(t, "adr-own-1", resp.AddressID)
	require.Equal(t, "10.0.0.7", resp.Value)

	require.Equal(t, 1, intAddrSvc.createOwnedCallCount(),
		"создание и привязка — одно намерение и одна транзакция")
	assert.Zero(t, intAddrSvc.setCallCount(),
		"отдельной привязки быть не должно: именно она гейтилась на свежесозданном "+
			"объекте и вносила зависимость от окна материализации")
	assert.Zero(t, addrSvc.createCallCount(),
		"публичный Create на этом пути больше не используется")

	got := intAddrSvc.lastCreateOwnedReq()
	require.NotNil(t, got)
	assert.Equal(t, "nlb_load_balancer", got.GetReferrerType())
	assert.Equal(t, "nlb-1", got.GetReferrerId())
	assert.Equal(t, "lb-one", got.GetReferrerName())
	assert.True(t, got.GetOwned(),
		"адрес заказан балансировщиком неявно — жизненный цикл связан с владельцем")
	assert.Equal(t, "prj-1", got.GetAddress().GetProjectId(),
		"решение о доступе принимается на project'е из тела создания")
	assert.Equal(t, "sub-1", got.GetAddress().GetInternalIpv4AddressSpec().GetSubnetId())
}

func TestAllocateExternalIP_UsesOneMutationCarryingTheOwner(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{}
	intAddrSvc := &fakeInternalAddressService{
		createOwnedResp: &vpcpb.Address{
			Id: "adr-own-ext",
			Address: &vpcpb.Address_ExternalIpv4Address{
				ExternalIpv4Address: &vpcpb.ExternalIpv4Address{Address: "198.51.100.9"},
			},
		},
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	resp, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", Name: "vip", ZoneID: "zone-a",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-2"},
	})
	require.NoError(t, err)
	require.Equal(t, "198.51.100.9", resp.Value)
	require.Equal(t, 1, intAddrSvc.createOwnedCallCount())
	assert.Zero(t, intAddrSvc.setCallCount())
}

// Отказ единственной мутации: компенсировать нечего. Клиент НЕ шлёт
// компенсирующий Delete — половины работы не существует, транзакция на стороне
// vpc не коммитилась. Это и есть разница с прежним двухшаговым путём, где
// half-allocated адрес приходилось возвращать в пул отдельным вызовом (и где
// сам возврат упирался в то же окно).
func TestAllocate_FailureLeavesNothingToCompensate(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{}
	intAddrSvc := &fakeInternalAddressService{
		createOwnedErr: status.Error(codes.FailedPrecondition, "pool exhausted"),
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-3"},
	})
	require.Error(t, err)
	assert.Zero(t, addrSvc.deleteCallCount(),
		"компенсирующего Delete нет: откатывать нечего — вставка, аллокация и "+
			"привязка живут в одной транзакции на стороне vpc")
	assert.Equal(t, 1, intAddrSvc.createOwnedCallCount(),
		"терминальный отказ не ретраится")
}

// Контракт полос переживает изменение: недоступность соседа остаётся
// transient-полосой и НЕ выдаётся за отказ ёмкости и не за «плохой id».
func TestAllocate_PeerUnavailableStaysTransient(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{}
	intAddrSvc := &fakeInternalAddressService{
		createOwnedErr: status.Error(codes.Unavailable, "vpc down"),
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-4"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable),
		"недоступность владельца — transient, а не отказ ёмкости; получено: %v", err)
	assert.False(t, errors.Is(err, domain.ErrInvalidArg),
		"id адреса минтит vpc — он не может быть неверным аргументом вызывающего")
}

// newFastRetryInternalAddressClient — реальный клиент поверх дублёров.
// Сжимать каденцию ретраев больше нечего: собственной петли ожидания на пути
// авто-аллокации не осталось — она и была зависимостью от окна.
func newFastRetryInternalAddressClient(t *testing.T, conn *grpc.ClientConn) InternalAddressClient {
	t.Helper()
	c, ok := NewInternalAddressClient(conn, conn).(*internalAddressClient)
	require.True(t, ok)
	return c
}

// Race-safe counters over the shared fakes.

func (f *fakeInternalAddressService) setCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.setCalls)
}

func (f *fakeInternalAddressService) clearCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.clearCalls)
}

func (f *fakeInternalAddressService) createOwnedCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createOwnedCalls
}

func (f *fakeInternalAddressService) lastCreateOwnedReq() *vpcpb.CreateOwnedAddressRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCreateOwned
}

func (f *fakeAddressForAlloc) createCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls
}

func (f *fakeAddressForAlloc) deleteCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleteCalls
}
