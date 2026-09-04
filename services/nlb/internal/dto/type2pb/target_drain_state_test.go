// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package type2pb

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Публичная проекция группы целей строится из набора, который несёт
// lifecycle-состояние. Раньше она собиралась из доменного набора, у которого
// состояния нет вовсе, — и снятая цель, живущая в группе до истечения задержки,
// была неотличима от обычной.

func drainTG(states []kachorepo.TargetRecord) kachorepo.TargetGroupRecord {
	return kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID: "tgr01DRAIN12345678xx", ProjectID: "p1", RegionID: "r1",
			Status:              domain.TargetGroupStatusActive,
			DeregistrationDelay: domain.LbDuration(300 * time.Second),
		},
		CreatedAt:    time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
		TargetStates: states,
	}
}

func TestTargetGroup_Projection_CarriesDrainState(t *testing.T) {
	drainAt := time.Date(2026, 7, 31, 12, 0, 30, 0, time.UTC)
	rec := drainTG([]kachorepo.TargetRecord{
		{
			Target: domain.Target{
				InstanceID: option.MustNewOption(domain.InstanceID("epd0ACTIVE0000000001")),
				Weight:     100,
			},
			ID: "t-active", Status: kachorepo.TargetStatusActive,
		},
		{
			Target: domain.Target{
				NicID:  option.MustNewOption(domain.NicID("e9b0DRAIN00000000001")),
				Weight: 50,
			},
			ID: "t-draining", Status: kachorepo.TargetStatusDraining, DrainStartedAt: &drainAt,
		},
	})

	var pb *lbv1.TargetGroup
	require.NoError(t, dto.Transfer(dto.FromTo(rec, &pb)))
	require.Len(t, pb.Targets, 2)

	assert.Equal(t, lbv1.Target_ACTIVE, pb.Targets[0].GetStatus())
	assert.Nil(t, pb.Targets[0].GetDrainStartedAt(), "у активной цели момента слива нет")

	assert.Equal(t, lbv1.Target_DRAINING, pb.Targets[1].GetStatus())
	require.NotNil(t, pb.Targets[1].GetDrainStartedAt())
	assert.Equal(t, drainAt, pb.Targets[1].GetDrainStartedAt().AsTime())
}

// Проекция читает ТОЛЬКО набор с состоянием: запасного пути на доменный набор
// нет намеренно — «состояние неизвестно» отдалось бы как «активна», то есть
// ровно тем утверждением, которое и было неверным. Тест закрепляет это
// решение, а не наблюдает случайность.
func TestTargetGroup_Projection_DomainSetAloneProjectsNothing(t *testing.T) {
	rec := kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID: "tgr01NOSTATE123456xx", ProjectID: "p1", RegionID: "r1",
			Status: domain.TargetGroupStatusActive,
			Targets: []domain.Target{
				{InstanceID: option.MustNewOption(domain.InstanceID("epd0NOSTATE000000001")), Weight: 100},
			},
		},
		CreatedAt: time.Now(),
	}
	var pb *lbv1.TargetGroup
	require.NoError(t, dto.Transfer(dto.FromTo(rec, &pb)))
	assert.Empty(t, pb.Targets,
		"путь чтения, не заполнивший состояние, обязан быть заметен пустым набором, "+
			"а не выдать сливающуюся цель за обычную")
}
