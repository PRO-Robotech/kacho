// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"go.uber.org/multierr"
)

// TargetGroup — domain entity TargetGroup.
//
// Targets — embedded child (физически живут в отдельной таблице `targets` с
// FK ON DELETE RESTRICT, но domain-модель удобнее держать flat для Validate
// и use-case-операций AddTargets/RemoveTargets).
//
// HealthCheck сериализуется JSONB-колонкой; embedded — потому что у TG ровно
// один HC.
type TargetGroup struct {
	ID                  ResourceID
	ProjectID           ProjectID
	RegionID            RegionID
	Name                LbName
	Description         LbDescription
	Labels              LbLabels
	Targets             []Target
	HealthCheck         HealthCheck
	DeregistrationDelay LbDuration
	SlowStart           LbDuration
	Status              TargetGroupStatus
	// Port — single backend port of the group (NLB-1b F6-co-req). Required,
	// 1..65535. Echoed by Listener.resolved_backend_port. LIVE-mutable (NLB-1c).
	Port LbPort
}

// Validate — все семантически-нагруженные поля + cardinality лимит + bound checks.
// Покрывает.
func (tg TargetGroup) Validate() error {
	deregErr := error(nil)
	if tg.DeregistrationDelay < DeregistrationDelayMin ||
		tg.DeregistrationDelay > DeregistrationDelayMax {
		deregErr = coreerrors.InvalidArgument().
			AddFieldViolation("deregistration_delay",
				"deregistration_delay must be in range [0s, 3600s]").
			Err()
	}
	slowErr := error(nil)
	if tg.SlowStart < SlowStartMin || tg.SlowStart > SlowStartMax {
		slowErr = coreerrors.InvalidArgument().
			AddFieldViolation("slow_start",
				"slow_start must be in range [0s, 900s]").
			Err()
	}
	cardErr := error(nil)
	if len(tg.Targets) > MaxTargetsPerGroup {
		cardErr = coreerrors.InvalidArgument().
			AddFieldViolation("targets",
				"too many targets (max 100)").
			Err()
	}

	// Per-target Validate. Останавливаемся на первой проблеме (early-exit)
	// иначе error-message раздуется до 100*N FieldViolations.
	var perTargetErr error
	for i := range tg.Targets {
		if err := tg.Targets[i].Validate(); err != nil {
			perTargetErr = err
			break
		}
	}

	return multierr.Combine(
		tg.Name.Validate(),
		tg.Description.Validate(),
		ValidateLabels(tg.Labels),
		tg.Status.Validate(),
		tg.HealthCheck.Validate(),
		tg.Port.Validate(),
		deregErr,
		slowErr,
		cardErr,
		perTargetErr,
	)
}
