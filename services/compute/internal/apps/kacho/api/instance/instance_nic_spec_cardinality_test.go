// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package instance

// Повторяемое поле публичного запроса, каждый элемент которого стоит ОДНОГО
// последовательного вызова к соседу.
//
// Число интерфейсов в запросе создания машины не ограничивал никто: ни проверка
// запроса, ни хендлер, ни контракт. Длину резал лишь предел размера сообщения —
// порядок десятков тысяч элементов. Дальше проверка размещения шла по списку и на
// КАЖДЫЙ элемент делала отдельный вызов в vpc, последовательно, без склейки
// повторов. То есть один корректный запрос выпускал десятки тысяч авторизованных
// вызовов к соседу и держал слот ОБЩЕГО пула исполнителей до его собственного
// предела в четыре минуты. Шестьдесят четыре таких запроса (около трети запроса в
// секунду — в метриках это не видно) занимали весь пул, и ни одна асинхронная
// мутация машин НИ ОДНОГО тенанта в это окно не исполнялась.
//
// Канонический образец ограничения уже есть у соседа (nlb, набор групп
// безопасности): предел проверяется в валидации ДО фазы обращений к пиру, поэтому
// список сверх предела не стоит НИ ОДНОГО внешнего вызова.
//
// Здесь проверяется наблюдаемое: (а) список сверх предела отвергается СИНХРОННО,
// названным полем, и не стоит ни одного обращения к соседу; (б) законный список
// проходит; (в) повторяющаяся подсеть спрашивается у соседа ОДИН раз.

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// nicSpecs — n интерфейсов, все на одну и ту же существующую подсеть вызывающего
// (именно повтор и не был прикрыт: цикл спрашивал соседа заново на каждый).
func nicSpecs(n int, subnetID string) []NetworkInterfaceSpec {
	out := make([]NetworkInterfaceSpec, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, NetworkInterfaceSpec{SubnetID: subnetID})
	}
	return out
}

// TestValidateCreate_NetworkInterfaceSpecsOverLimitRejectedByName — предел есть, он
// синхронный и называет поле.
func TestValidateCreate_NetworkInterfaceSpecsOverLimitRejectedByName(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = nicSpecs(domain.MaxNetworkInterfaceSpecsPerInstance+1, "sub-abc")

	err := ValidateCreateInstanceReq(req)
	require.Error(t, err, "an unbounded interface list is one sequential peer call per element")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "networkInterfaceSpecs",
		"отказ обязан называть поле: вызывающий должен узнать, что именно урезать")
}

// TestCreate_OverLimitInterfaceListCostsNoPeerCall — отказ наступает ДО фазы
// обращений к соседу. Иначе предел защищает от роста строки, но не от нагрузки.
func TestCreate_OverLimitInterfaceListCostsNoPeerCall(t *testing.T) {
	subnets := portmock.NewSubnetRegistry()
	kit := newInstanceSvcWithSubnets(t, true, subnets)
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = nicSpecs(domain.MaxNetworkInterfaceSpecsPerInstance+1, "sub-abc")

	_, err := kit.svc.Create(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Zero(t, subnets.Calls(),
		"список сверх предела стоил %d обращений к соседу: отказ обязан наступать ДО фазы peer-валидации",
		subnets.Calls())
}

// TestCreate_LegitimateInterfaceListPasses — обратная сторона: список в пределах
// нормы проходит. Без неё предыдущие два зеленели бы и на «отвергать всё».
func TestCreate_LegitimateInterfaceListPasses(t *testing.T) {
	subnets := portmock.NewSubnetRegistry()
	kit := newInstanceSvcWithSubnets(t, true, subnets)
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}, {SubnetID: "sub-a"}}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.Nil(t, done.Error, "законный список интерфейсов обязан проходить: %v", done.Error)
}

// TestCreate_RepeatedSubnetIsAskedOnce — стоимость запроса определяется числом
// РАЗНЫХ подсетей, а не длиной списка.
//
// До склейки: 8 элементов на одну подсеть = 8 обращений к соседу.
// После: 1 обращение.
func TestCreate_RepeatedSubnetIsAskedOnce(t *testing.T) {
	subnets := portmock.NewSubnetRegistry()
	kit := newInstanceSvcWithSubnets(t, true, subnets)
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = nicSpecs(domain.MaxNetworkInterfaceSpecsPerInstance, "sub-abc")

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.Nil(t, done.Error, "повтор одной своей подсети законен: %v", done.Error)
	require.Equal(t, 1, subnets.Calls(),
		"одна и та же подсеть спрошена %d раз(а) при %d элементах: повтор обязан склеиваться, "+
			"иначе длина списка напрямую умножает обращения к соседу",
		subnets.Calls(), domain.MaxNetworkInterfaceSpecsPerInstance)
}

// TestCreate_DistinctSubnetsAreEachAsked — обратная сторона склейки: РАЗНЫЕ
// подсети по-прежнему проверяются каждая. Иначе «спросить один раз» выполнялось бы
// пропуском проверки.
func TestCreate_DistinctSubnetsAreEachAsked(t *testing.T) {
	subnets := portmock.NewSubnetRegistry()
	kit := newInstanceSvcWithSubnets(t, true, subnets)
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{
		{SubnetID: "sub-abc"}, {SubnetID: "sub-a"}, {SubnetID: "sub-abc"}, {SubnetID: "sub-a"},
	}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.Nil(t, done.Error, "%v", done.Error)
	require.Equal(t, 2, subnets.Calls(),
		"две разные подсети обязаны быть спрошены обе (получено %d обращений)", subnets.Calls())
}

// TestCreate_DedupDoesNotHideAForeignZoneSubnet — склейка не должна прятать
// несовпадение: чужая подсеть в повторяющемся списке по-прежнему отвергается.
func TestCreate_DedupDoesNotHideAForeignZoneSubnet(t *testing.T) {
	kit := newInstanceSvcWithSubnets(t, true, portmock.NewSubnetRegistry())
	req := baseCreateReq() // zone ru-central1-a
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{
		{SubnetID: "sub-abc"}, {SubnetID: "sub-b"}, {SubnetID: "sub-abc"}, // sub-b — зона -b
	}

	op, err := kit.svc.Create(context.Background(), req)
	require.NoError(t, err)
	done := portmock.AwaitOpDone(t, kit.ops, op.ID)
	require.NotNil(t, done.Error, "подсеть чужой зоны обязана быть отвергнута и в повторяющемся списке")
	require.Equal(t, int32(codes.FailedPrecondition), done.Error.Code)
	require.Equal(t,
		"NetworkInterface subnet is in zone ru-central1-b, instance zone is ru-central1-a",
		done.Error.Message)
}

// TestValidateCreate_SecondaryVolumeSpecsOverLimitRejectedByName — предел, который
// контракт УЖЕ обещал («<=8»), но не проверял никто: расширения проверок в
// объявлениях не читает ни одна строка прод-кода.
func TestValidateCreate_SecondaryVolumeSpecsOverLimitRejectedByName(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}
	specs := make([]SecondaryVolumeSpec, 0, domain.MaxSecondaryVolumeSpecsPerInstance+1)
	for i := 0; i <= domain.MaxSecondaryVolumeSpecsPerInstance; i++ {
		specs = append(specs, SecondaryVolumeSpec{SizeGiB: 10, MountPath: fmt.Sprintf("/mnt/%d", i)})
	}
	req.SecondaryVolumeSpecs = specs

	err := ValidateCreateInstanceReq(req)
	require.Error(t, err, "контракт объявляет предел 8; объявление без проверки — обещание, за которым ничего нет")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "secondaryVolumeSpecs")
}

// TestValidateCreate_SecondaryVolumeSpecsAtLimitPasses — ровно предел законен
// (граничное значение включительно, как и объявлено).
func TestValidateCreate_SecondaryVolumeSpecsAtLimitPasses(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = []NetworkInterfaceSpec{{SubnetID: "sub-abc"}}
	specs := make([]SecondaryVolumeSpec, 0, domain.MaxSecondaryVolumeSpecsPerInstance)
	for i := 0; i < domain.MaxSecondaryVolumeSpecsPerInstance; i++ {
		specs = append(specs, SecondaryVolumeSpec{SizeGiB: 10, MountPath: fmt.Sprintf("/mnt/%d", i)})
	}
	req.SecondaryVolumeSpecs = specs

	require.NoError(t, ValidateCreateInstanceReq(req))
}

// TestValidateCreate_NetworkInterfaceSpecsAtLimitPasses — то же для интерфейсов.
func TestValidateCreate_NetworkInterfaceSpecsAtLimitPasses(t *testing.T) {
	req := baseCreateReq()
	req.NetworkInterfaceSpecs = nicSpecs(domain.MaxNetworkInterfaceSpecsPerInstance, "sub-abc")
	require.NoError(t, ValidateCreateInstanceReq(req))
}
