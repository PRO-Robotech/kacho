// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"time"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// TargetGroupRecord — repo-entity TargetGroup. domain.TargetGroup + DB-managed
// CreatedAt/UpdatedAt. Поле Targets (TargetRecord) заполняется при Get/List
// через JOIN на child-таблицу `targets` (см. pg/target_group_repo.go).
type TargetGroupRecord struct {
	domain.TargetGroup
	CreatedAt time.Time
	UpdatedAt time.Time
	// Xmin — `xmin::text` OCC snapshot; see LoadBalancerRecord.Xmin.
	Xmin string
}

// TargetGroupFilter — фильтр для List target_groups.
// Per-object RBAC-видимость решается на СТРАНИЦЕ (см. LoadBalancerFilter).
type TargetGroupFilter struct {
	ProjectID string
	Name      string
	Filter    string
}

// TargetRecord — repo-entity для одного target внутри TG. domain.Target +
// DB-managed CreatedAt/UpdatedAt + Status (ACTIVE | DRAINING) + DrainStartedAt
// (NULL когда Status='ACTIVE'; NOT NULL когда 'DRAINING').
//
// Status и DrainStartedAt живут в repo-leaf (а не в domain.Target), потому что
// это lifecycle-поля управляемые worker'ом (фаза A drain mark / фаза B delete);
// domain.Target — что просит tenant на AddTargets (identity + weight).
type TargetRecord struct {
	domain.Target
	ID             string
	TargetGroupID  string
	Status         string
	DrainStartedAt *time.Time // nil if Status == TargetStatusActive
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Значения TargetRecord.Status. Названы здесь, а не повторены литералом у
// каждого читателя: по этому полю принимаются решения (напр. TargetGroup.Delete
// считает только живые цели), и опечатка в литерале дала бы молча обратный смысл.
const (
	TargetStatusActive   = "ACTIVE"
	TargetStatusDraining = "DRAINING"
)
