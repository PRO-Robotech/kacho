// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// lbAddressOwnerKind — Reference.type для NLB LoadBalancer в vpc.Address referrer.
//
// Не собственная константа, а ССЫЛКА на значение из клиента vpc: им заводится
// аренда и им же она предъявляется при снятии — в том числе реконсайлером из
// соседнего пакета. Своя копия здесь разошлась бы с ним молча, и сверка
// владения перестала бы совпадать.
const lbAddressOwnerKind = vpcclient.OwnerKindLoadBalancer

// lbAddressOwner — owner-tuple для vpc.Address referrer ("network_load_balancer:<id>").
// name — display-имя LB для used_by-зеркала (пусто на release-пути, где имя не нужно).
func lbAddressOwner(lbID, name string) vpcclient.AddressOwner {
	return vpcclient.AddressOwner{Kind: lbAddressOwnerKind, ID: lbID, Name: name}
}

// Строитель нагрузки вида `nlb_load_balancer` живёт НЕ ЗДЕСЬ, а в repo-leaf —
// `kachorepo.LoadBalancerStatePayload`, рядом со своим читателем.
//
// # Почему оттуда, а не отсюда
//
// Точки эмиссии этого вида лежат в ДВУХ пакетах use-case: пять в этом
// (создание · правка · снятие · переезд, у которого их две) и две — в пакете
// СЛУШАТЕЛЯ, где создание и снятие слушателя объявляют правку своего
// балансировщика. Строитель, спрятанный здесь, второму пакету недоступен, и
// второй завёл бы свой — вторую форму нагрузки того же вида.
//
// Здесь стояли ДВА строителя: `lbOutboxPayload` (пять полей строки) и
// `lbMovedPayload` (идентификатор плюс исходный и целевой проекты). Оба сняты
// вместе с минимальным снимком: вид объявлен несущим ПОЛНОЕ состояние, а
// контракт формы разрешает читать непустое состояние как полное — значит одна
// частичная нагрузка делает ложным весь вид, и делает тихо.
//
// # `old_project_id` СНЯТ, и это решение, а не потеря по дороге
//
// Он был единственным местом журнала, где записан исходный проект переезда.
// Читателя у него не было ни одного (названный прежде потребитель — снятый
// задачей #814 контракт), а состояние события несёт проект ЦЕЛЕВОЙ, то есть
// ровно то, ради чего подписчик событие и читает: прежний проект у него уже
// есть — это то, что лежит в его собственном состоянии до применения события.
// Тот же выбор сделан у соседнего вида на его переезде (#1551).

// lbRegisterIntent — FGA-register-intent свежесозданного LB (project-hierarchy).
//
// A durable intent carries ONLY proxy-registrable tuples. kaname's
// least-privilege policy accepts the ownership/parent relations declared in
// pkg/authz/proxytuple and reserves
// privilege relations for the AccessBinding flow, so the creator (`admin`) tuple
// this used to append was refused on every delivery.
//
// A refusal from the model owner is TERMINAL: the applier maps it to
// drainer.ErrPermanent (clients/iam/register_applier.go) and the shared drainer
// classifies it the same way for every service (pkg/outbox/drainer/classify.go).
// So such a row poisons on its FIRST attempt and leaves the partition-head
// blocking set at once — it does not hold later intents for this LB for any
// deadline. What it costs is the registration: the applier stops at the first
// rejection, so nothing after the rejected tuple ships, and the row stays
// undelivered until reconciler.RedrivePoisoned comes back for it. Creator access
// is materialised per-object by IAM's reconciler (flat Contract-A), not by a
// module-written admin tuple.
func lbRegisterIntent(lb *kachorepo.LoadBalancerRecord) domain.FGARegisterIntent {
	id := string(lb.ID)
	return domain.FGARegisterIntent{
		Kind:       "NetworkLoadBalancer",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, id, string(lb.ProjectID)),
		},
		Labels:          domain.LabelsToMap(lb.Labels),
		ParentProjectID: string(lb.ProjectID),
	}
}

// lbMirrorIntent — mirror-feed register-intent для UPDATED LB (project-hierarchy
// re-register с обновлёнными labels; без creator-tuple).
func lbMirrorIntent(lb *kachorepo.LoadBalancerRecord) domain.FGARegisterIntent {
	id := string(lb.ID)
	return domain.FGARegisterIntent{
		Kind:       "NetworkLoadBalancer",
		ResourceID: id,
		Tuples: []domain.FGATuple{
			domain.FGAProjectTuple(domain.FGAObjectTypeLoadBalancer, id, string(lb.ProjectID)),
		},
		Labels:          domain.LabelsToMap(lb.Labels),
		ParentProjectID: string(lb.ProjectID),
	}
}
