// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"

	// Blank-import регистрирует трансферы TargetGroup/Target через init().
	_ "github.com/PRO-Robotech/kacho/services/nlb/internal/dto/type2pb"
)

// Снятие цели двухфазно: фаза A помечает строку сливающейся, фаза B удаляет её
// по истечении задержки группы. Между фазами цель ОСТАЁТСЯ в наборе, и без
// состояния в публичной проекции вызывающий видит её обычной — то есть снятие
// выглядит так, будто ничего не произошло.
//
// Тест идёт до самого конца — от строки в базе до JSON, который увидит клиент:
// проверять только запись репозитория значило бы остановиться там, где дефект и
// жил (состояние читалось из базы и терялось при сборке ответа).

func TestTargetDrainProjection_DrainingIsVisibleEndToEnd(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj01TGDR1234567890ll", "drain-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	staying := domain.Target{
		InstanceID: option.MustNewOption(domain.InstanceID("epd0STAY000000000001")),
		Weight:     100,
	}
	leaving := domain.Target{
		InstanceID: option.MustNewOption(domain.InstanceID("epd0LEAVE00000000001")),
		Weight:     50,
	}
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		n, err := w.TargetGroups().AddTargets(ctx, string(tg.ID), []domain.Target{staying, leaving})
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})

	// Фаза A — резолв идентичности в id строки и пометка «сливается».
	var leavingID string
	rd0, err := repo.Reader(ctx)
	require.NoError(t, err)
	rows, err := rd0.TargetGroups().ListTargets(ctx, string(tg.ID))
	require.NoError(t, err)
	require.NoError(t, rd0.Close())
	for _, r := range rows {
		if v, ok := r.InstanceID.Maybe(); ok && string(v) == "epd0LEAVE00000000001" {
			leavingID = r.ID
		}
	}
	require.NotEmpty(t, leavingID, "предпосылка теста: цель, которую снимаем, обязана существовать")

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		n, err := w.TargetGroups().RemoveTargetsMarkDraining(ctx, string(tg.ID), []string{leavingID})
		require.NoError(t, err)
		assert.Equal(t, 1, n, "фаза A обязана пометить ровно одну строку")
	})

	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.TargetGroups().Get(ctx, string(tg.ID))
	require.NoError(t, err)
	require.Len(t, got.TargetStates, 2, "обе цели остаются в наборе до истечения задержки")

	var pb *lbv1.TargetGroup
	require.NoError(t, dto.Transfer(dto.FromTo(*got, &pb)))
	require.Len(t, pb.Targets, 2)

	byID := map[string]*lbv1.Target{}
	for _, tp := range pb.Targets {
		byID[tp.GetInstanceId()] = tp
	}
	stayPb := byID["epd0STAY000000000001"]
	leavePb := byID["epd0LEAVE00000000001"]
	require.NotNil(t, stayPb)
	require.NotNil(t, leavePb)

	assert.Equal(t, lbv1.Target_ACTIVE, stayPb.GetStatus(), "нетронутая цель обязана остаться активной")
	assert.Nil(t, stayPb.GetDrainStartedAt(), "у активной цели момента слива нет")

	assert.Equal(t, lbv1.Target_DRAINING, leavePb.GetStatus())
	require.NotNil(t, leavePb.GetDrainStartedAt(), "сливающаяся цель обязана назвать момент начала слива")
	assert.False(t, leavePb.GetDrainStartedAt().AsTime().IsZero())

	// То, что реально увидит клиент.
	b, err := protojson.Marshal(leavePb)
	require.NoError(t, err)
	t.Logf("проекция снятой цели: %s", string(b))
	assert.Contains(t, string(b), "DRAINING")
}
