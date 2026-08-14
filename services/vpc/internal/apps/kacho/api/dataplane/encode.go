// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"fmt"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует трансферы record → proto через init(). Без него
	// реестр пуст, и КАЖДАЯ строка намерения превратилась бы в ошибку «трансфер
	// не зарегистрирован» уже в рантайме.
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// intentMessage переводит строку проекции в сообщение контракта.
//
// # Вид объекта выражен ТОЛЬКО ветвью oneof
//
// Отдельного поля «вид» в контракте нет: вид, названный дважды, однажды назовут
// по-разному, и получателю будет нечем решить, какому утверждению верить. Здесь
// это стоит одной цены — ветвь приходится выбирать в двух местах (у снятого
// намерения и у живого), и обе выборки обходит проба
// `TestEveryKindHasAContractBranch`: вид, забытый в любой из них, краснеет.
//
// # Тело снятого намерения — только идентификатор
//
// У удалённого ресурса полей нет, и заполнять их нечем. Идентификатор для
// адресации достаточен: он неизменяем и глобально уникален (запрет #15), то
// есть исполнитель по нему однозначно находит применённое и снимает его.
func intentMessage(row IntentRow) (*vpcv1.DataplaneIntent, error) {
	if err := row.Validate(); err != nil {
		return nil, err
	}
	out := &vpcv1.DataplaneIntent{Revision: row.Revision, Withdrawn: row.Withdrawn}
	if row.Withdrawn {
		if err := setWithdrawnBody(out, row.Kind, row.ResourceID); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := setLiveBody(out, row); err != nil {
		return nil, err
	}
	return out, nil
}

// setWithdrawnBody — ветвь снятого намерения: идентичность и ничего больше.
//
// Ветвь ставится ЗДЕСЬ, а не возвращается значением: тип ветви oneof
// сгенерирован неэкспортируемым, и вернуть его наружу нечем. Это ограничение
// генератора, а не выбор формы.
func setWithdrawnBody(out *vpcv1.DataplaneIntent, kind Kind, id string) error {
	switch kind {
	case KindNetwork:
		out.Object = &vpcv1.DataplaneIntent_Network{Network: &vpcv1.NetworkIntent{Network: &vpcv1.Network{Id: id}}}
	case KindSubnet:
		out.Object = &vpcv1.DataplaneIntent_Subnet{Subnet: &vpcv1.Subnet{Id: id}}
	case KindNetworkInterface:
		out.Object = &vpcv1.DataplaneIntent_NetworkInterface{NetworkInterface: &vpcv1.NetworkInterface{Id: id}}
	case KindSecurityGroup:
		out.Object = &vpcv1.DataplaneIntent_SecurityGroup{SecurityGroup: &vpcv1.SecurityGroup{Id: id}}
	case KindRouteTable:
		out.Object = &vpcv1.DataplaneIntent_RouteTable{RouteTable: &vpcv1.RouteTable{Id: id}}
	case KindGateway:
		out.Object = &vpcv1.DataplaneIntent_Gateway{Gateway: &vpcv1.Gateway{Id: id}}
	case KindAddress:
		out.Object = &vpcv1.DataplaneIntent_Address{Address: &vpcv1.Address{Id: id}}
	default:
		return fmt.Errorf("%w: у вида %q нет ветви контракта", ErrRowShape, kind)
	}
	return nil
}

// setLiveBody — ветвь объявленного намерения: тело ресурса целиком.
func setLiveBody(out *vpcv1.DataplaneIntent, row IntentRow) error {
	switch {
	case row.Network != nil:
		var pb *vpcv1.Network
		if err := dto.Transfer(dto.FromTo(*row.Network, &pb)); err != nil {
			return err
		}
		// vrf_id едет ТОЛЬКО здесь и в InternalNetworkService.GetNetwork — обе
		// поверхности cluster-internal. Публичная проекция сети его не несёт.
		out.Object = &vpcv1.DataplaneIntent_Network{
			Network: &vpcv1.NetworkIntent{Network: pb, VrfId: row.Network.VRFID},
		}
	case row.Subnet != nil:
		var pb *vpcv1.Subnet
		if err := dto.Transfer(dto.FromTo(*row.Subnet, &pb)); err != nil {
			return err
		}
		out.Object = &vpcv1.DataplaneIntent_Subnet{Subnet: pb}
	case row.NetworkInterface != nil:
		var pb *vpcv1.NetworkInterface
		if err := dto.Transfer(dto.FromTo(*row.NetworkInterface, &pb)); err != nil {
			return err
		}
		out.Object = &vpcv1.DataplaneIntent_NetworkInterface{NetworkInterface: pb}
	case row.SecurityGroup != nil:
		var pb *vpcv1.SecurityGroup
		if err := dto.Transfer(dto.FromTo(*row.SecurityGroup, &pb)); err != nil {
			return err
		}
		out.Object = &vpcv1.DataplaneIntent_SecurityGroup{SecurityGroup: pb}
	case row.RouteTable != nil:
		var pb *vpcv1.RouteTable
		if err := dto.Transfer(dto.FromTo(*row.RouteTable, &pb)); err != nil {
			return err
		}
		out.Object = &vpcv1.DataplaneIntent_RouteTable{RouteTable: pb}
	case row.Gateway != nil:
		var pb *vpcv1.Gateway
		if err := dto.Transfer(dto.FromTo(*row.Gateway, &pb)); err != nil {
			return err
		}
		out.Object = &vpcv1.DataplaneIntent_Gateway{Gateway: pb}
	case row.Address != nil:
		var pb *vpcv1.Address
		if err := dto.Transfer(dto.FromTo(*row.Address, &pb)); err != nil {
			return err
		}
		out.Object = &vpcv1.DataplaneIntent_Address{Address: pb}
	default:
		return fmt.Errorf("%w: у вида %q нет ветви контракта", ErrRowShape, row.Kind)
	}
	return nil
}

// intentEvent заворачивает намерение в сообщение потока.
func intentEvent(in *vpcv1.DataplaneIntent) *vpcv1.WatchIntentResponse {
	return &vpcv1.WatchIntentResponse{Event: &vpcv1.WatchIntentResponse_Intent{Intent: in}}
}

// syncedEvent — выдача доведена до конца на названной ревизии.
func syncedEvent(revision int64) *vpcv1.WatchIntentResponse {
	return &vpcv1.WatchIntentResponse{
		Event: &vpcv1.WatchIntentResponse_Synced{
			Synced: &vpcv1.IntentStreamSynced{Revision: revision},
		},
	}
}

// resyncEvent — продолжение невозможно, начинай с полной выдачи.
func resyncEvent(cause vpcv1.ResyncCause) *vpcv1.WatchIntentResponse {
	return &vpcv1.WatchIntentResponse{
		Event: &vpcv1.WatchIntentResponse_Resync{
			Resync: &vpcv1.IntentResyncRequired{Cause: cause},
		},
	}
}
