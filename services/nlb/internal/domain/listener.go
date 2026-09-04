// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"github.com/PRO-Robotech/kacho/pkg/option"
	"go.uber.org/multierr"
)

// Listener — domain entity Listener. Принадлежит LoadBalancer'у;
// `RegionID` денормализован from-LB (same-region constraint — DB-CHECK).
//
// Собственного адреса у листенера НЕТ: VIP консолидирован на LoadBalancer'е
// (один anycast-VIP на семейство — `LoadBalancer.AddressIDV4/V6`), а листенер —
// это (port, protocol) на этом VIP и ничего не аллоцирует. Address-поля
// (ip_version/address_id/allocated_address/subnet_id/vip_origin) сняты и с
// proto (reserved 12-15), и со схемы (миграция 0028).
//
// Обрамления PROXY-протокола у листенера тоже нет: заголовок уходит до любых
// данных соединения, то есть его вставляет владелец байтового потока к бекенду, а
// балансировщик четвёртого уровня потоком не владеет. Поле снято с контракта
// (reserved 16 в `Listener`, 10 и 6 в запросах) и со схемы (миграция 0030).
type Listener struct {
	ID                   ResourceID
	ProjectID            ProjectID
	LoadBalancerID       ResourceID
	RegionID             RegionID
	Name                 LbName
	Description          LbDescription
	Labels               LbLabels
	Protocol             LbProto
	Port                 LbPort
	DefaultTargetGroupID option.ValueOf[ResourceID]
	Status               ListenerStatus
}

// Validate — все семантически-нагруженные поля (форма; кросс-полевые
// инварианты, требующие знания LB-родителя, живут в use-case-слое).
func (l Listener) Validate() error {
	return multierr.Combine(
		l.Name.Validate(),
		l.Description.Validate(),
		ValidateLabels(l.Labels),
		l.Protocol.Validate(),
		l.Port.Validate(),
		l.Status.Validate(),
	)
}

// Equal — deep equality (Update no-op detection).
func (l Listener) Equal(other Listener) bool {
	return l.ID == other.ID &&
		l.ProjectID == other.ProjectID &&
		l.LoadBalancerID == other.LoadBalancerID &&
		l.RegionID == other.RegionID &&
		l.Name == other.Name &&
		l.Description == other.Description &&
		LabelsEqual(l.Labels, other.Labels) &&
		l.Protocol == other.Protocol &&
		l.Port == other.Port &&
		optEqual(l.DefaultTargetGroupID, other.DefaultTargetGroupID) &&
		l.Status == other.Status
}

// optEqual — equality двух option.ValueOf[T] по semantic-значению (some/none +
// inner). option.ValueOf.IsEq требует callback, эта обёртка дает удобный API
// для comparable T.
func optEqual[T comparable](a, b option.ValueOf[T]) bool {
	av, aok := a.Maybe()
	bv, bok := b.Maybe()
	if aok != bok {
		return false
	}
	if !aok {
		return true
	}
	return av == bv
}
