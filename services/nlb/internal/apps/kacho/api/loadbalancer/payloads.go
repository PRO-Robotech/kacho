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

// lbOutboxPayload — нагрузка журнала для балансировщика. Минимальный снимок.
// Ключи — из словаря `kachorepo.LifecyclePayload`; читателя у нагрузки сегодня
// нет ни одного (задача #1452, там же перепись и предикат его появления).
func lbOutboxPayload(lb *kachorepo.LoadBalancerRecord) map[string]any {
	if lb == nil {
		return nil
	}
	return kachorepo.LifecyclePayload{
		ID:        string(lb.ID),
		ProjectID: string(lb.ProjectID),
		RegionID:  string(lb.RegionID),
		Name:      string(lb.Name),
		Status:    string(lb.Status),
		Type:      string(lb.Type),
	}.Map()
}

// lbMovedPayload — нагрузка события переезда. `old_project_id` — исходный
// проект: единственное место журнала, где он вообще записан, потому что колонка
// якоря несёт уже целевой. Читателя у него сегодня нет; названный прежде
// потребитель (снос кортежей прав на старом проекте через снятый
// `InternalResourceLifecycleService.Subscribe`) снят задачей #814, а зеркало
// прав ходит очередью `fga_register_outbox`.
func lbMovedPayload(id, srcProject, dstProject string) map[string]any {
	return kachorepo.LifecyclePayload{
		ID:           id,
		OldProjectID: srcProject,
		NewProjectID: dstProject,
	}.Map()
}

// lbRegisterIntent — FGA-register-intent свежесозданного LB (project-hierarchy).
//
// A durable intent carries ONLY proxy-registrable tuples. kacho-iam's
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
