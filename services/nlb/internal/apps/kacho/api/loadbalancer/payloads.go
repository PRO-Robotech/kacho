// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// lbAddressOwnerKind — Reference.type для NLB LoadBalancer в vpc.Address referrer.
const lbAddressOwnerKind = "network_load_balancer"

// lbAddressOwner — owner-tuple для vpc.Address referrer ("network_load_balancer:<id>").
// name — display-имя LB для used_by-зеркала (пусто на release-пути, где имя не нужно).
func lbAddressOwner(lbID, name string) vpcclient.AddressOwner {
	return vpcclient.AddressOwner{Kind: lbAddressOwnerKind, ID: lbID, Name: name}
}

// lbOutboxPayload — JSON-payload для outbox. Минимальный snapshot.
// Ключи — из единого источника истины kachorepo.LifecyclePayload (тот же
// набор литералов, что читает Subscribe-consumer).
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

// lbMovedPayload — MOVED-event outbox-payload. old_project_id — исходный project
// (canonical-ключ, который Subscribe-consumer читает в
// ResourceLifecycleEvent.OldProjectId для kacho-iam FGA-sync: снос stale
// owner/hierarchy-tuples на старом project). Единый источник имён ключей —
// kachorepo.LifecyclePayload.
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
// least-privilege policy accepts {project, account, parent, owner} and reserves
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
