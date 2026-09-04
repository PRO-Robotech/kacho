// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

const (
	zoneA   = "ru-central1-a"
	zoneB   = "ru-central1-b"
	regionA = "ru-central1"
	regionB = "ru-central2"
)

func seedGroup(t *testing.T, ctx context.Context, r *repo.PlacementGroupRepo,
	projectID, name string, pt domain.PlacementAnchorType, zone, region string,
) string {
	t.Helper()
	g, _, err := r.Insert(ctx, &domain.PlacementGroup{
		ID:            ids.NewHyphenID("plg"),
		ProjectID:     projectID,
		Name:          name,
		Strategy:      domain.PlacementStrategySpread,
		PlacementType: pt,
		ZoneID:        zone,
		RegionID:      region,
	})
	require.NoError(t, err)
	return g.ID
}

func seedInstanceInGroup(t *testing.T, ctx context.Context, r *repo.InstanceRepo,
	projectID, zone, region, groupID string,
) (*domain.Instance, error) {
	t.Helper()
	inID := ids.NewID(ids.PrefixInstance)
	in, _, err := r.Insert(ctx, &domain.Instance{
		ID: inID, Name: inID, ProjectID: projectID, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		ZoneID: zone, RegionID: region, Status: domain.InstanceStatusRunning,
		FQDN: inID + ".auto.internal", InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
		PlacementGroupID:   groupID,
	})
	return in, err
}

// TestPlacementCoherence_ZoneMustMatchAndRegionalIsTheControl — несовпадение
// координаты отвергается, а совпадение проходит.
//
// Региональная ветвь стоит ПОЛОЖИТЕЛЬНЫМ контролем: без неё «отвергнуто»
// зеленело бы на реализации, отвергающей любую группу, и региональный якорь,
// у которого зоны нет by construction, выглядел бы работающим, будучи мёртвым.
func TestPlacementCoherence_ZoneMustMatchAndRegionalIsTheControl(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	grpRepo := repo.NewPlacementGroupRepo(pool)

	zonalA := seedGroup(t, ctx, grpRepo, "proj-plg", "plg-zonal-a", domain.PlacementTypeZonal, zoneA, "")
	regional := seedGroup(t, ctx, grpRepo, "proj-plg", "plg-regional", domain.PlacementTypeRegional, "", regionA)
	foreign := seedGroup(t, ctx, grpRepo, "proj-other", "plg-foreign", domain.PlacementTypeZonal, zoneA, "")

	t.Run("зональная группа своей зоны принимает машину", func(t *testing.T) {
		in, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", zoneA, regionA, zonalA)
		require.NoError(t, ierr)
		require.Equal(t, zonalA, in.PlacementGroupID)
	})

	t.Run("зональная группа ДРУГОЙ зоны отвергает", func(t *testing.T) {
		_, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", zoneB, regionA, zonalA)
		require.ErrorIs(t, ierr, repo.ErrPlacementIncoherent)
	})

	// Положительный контроль якоря: региональная группа принимает машину ИЗ ДРУГОЙ
	// ЗОНЫ того же региона — ровно того, ради чего региональный якорь существует.
	t.Run("региональная группа принимает машину другой зоны своего региона", func(t *testing.T) {
		in, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", zoneB, regionA, regional)
		require.NoError(t, ierr, "региональный якорь зоны не различает — в этом его смысл")
		require.Equal(t, regional, in.PlacementGroupID)
	})

	t.Run("региональная группа ДРУГОГО региона отвергает", func(t *testing.T) {
		_, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", zoneA, regionB, regional)
		require.ErrorIs(t, ierr, repo.ErrPlacementIncoherent)
	})

	t.Run("чужая группа отвергается ТЕМ ЖЕ отказом", func(t *testing.T) {
		_, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", zoneA, regionA, foreign)
		require.ErrorIs(t, ierr, repo.ErrPlacementIncoherent,
			"различимый ответ на «чужая» и «не той зоны» давал бы справочник по чужому проекту")
	})

	t.Run("несуществующая группа отвергается тем же отказом", func(t *testing.T) {
		_, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", zoneA, regionA, ids.NewHyphenID("plg"))
		require.ErrorIs(t, ierr, repo.ErrPlacementIncoherent)
	})

	// Байт-идентичность: четыре внутренних исхода дают ОДИН текст. Утверждается
	// сообщение, а не только сентинел — различимый текст вернул бы через прозу
	// ровно то, что закрыто выбором одного исхода.
	t.Run("четыре внутренних исхода дают один текст", func(t *testing.T) {
		var msgs []string
		for _, c := range []struct{ zone, region, group string }{
			{zoneB, regionA, zonalA},
			{zoneA, regionB, regional},
			{zoneA, regionA, foreign},
			{zoneA, regionA, ids.NewHyphenID("plg")},
		} {
			_, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-plg", c.zone, c.region, c.group)
			require.Error(t, ierr)
			msgs = append(msgs, ierr.Error())
		}
		for i := 1; i < len(msgs); i++ {
			require.Equal(t, msgs[0], msgs[i],
				"тексты разошлись: по различию читается состав чужого проекта")
		}
	})
}

// TestPlacementGroupDelete_RefusesWhileItHoldsInstances — снятие группы с
// машинами отвергается перечнем машин.
func TestPlacementGroupDelete_RefusesWhileItHoldsInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	grpRepo := repo.NewPlacementGroupRepo(pool)

	held := seedGroup(t, ctx, grpRepo, "proj-del-plg", "plg-held", domain.PlacementTypeZonal, zoneA, "")
	free := seedGroup(t, ctx, grpRepo, "proj-del-plg", "plg-free", domain.PlacementTypeZonal, zoneA, "")

	in, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-del-plg", zoneA, regionA, held)
	require.NoError(t, ierr)

	t.Run("занятая группа не снимается и называет машину", func(t *testing.T) {
		derr := grpRepo.Delete(ctx, held)
		require.Error(t, derr)
		var inUse *repo.ErrPlacementGroupInUse
		require.ErrorAs(t, derr, &inUse)
		require.Equal(t, []string{in.ID}, inUse.InstanceIDs)
	})

	// Положительный контроль: свободная группа того же проекта снимается.
	t.Run("свободная группа снимается", func(t *testing.T) {
		require.NoError(t, grpRepo.Delete(ctx, free))
	})
}

// TestPlacementCoherence_ConcurrentClaimsAreDecidedByTheStatement — свойство
// держит стейтмент, а не порядок вызовов.
//
// Машины создаются одновременно: половина в согласованной зоне, половина в
// чужой. Пройти обязаны РОВНО согласованные — независимо от того, кто
// закоммитился первым. Без конкурентных горутин проба не свидетельствует
// ничего: последовательный прогон зеленеет и на проверке «прочитал → сравнил».
func TestPlacementCoherence_ConcurrentClaimsAreDecidedByTheStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	grpRepo := repo.NewPlacementGroupRepo(pool)
	group := seedGroup(t, ctx, grpRepo, "proj-race", "plg-zonal", domain.PlacementTypeZonal, zoneA, "")

	const pairs = 8
	var wg sync.WaitGroup
	okCount := make([]bool, pairs*2)
	for i := 0; i < pairs*2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			zone := zoneA
			if i%2 == 1 {
				zone = zoneB // несогласованная половина
			}
			_, ierr := seedInstanceInGroup(t, ctx, instRepo, "proj-race", zone, regionA, group)
			okCount[i] = ierr == nil
		}(i)
	}
	wg.Wait()

	passed, refused := 0, 0
	for i, ok := range okCount {
		if ok {
			passed++
			require.Equal(t, 0, i%2, "прошла машина несогласованной зоны — стейтмент не решил")
		} else {
			refused++
		}
	}
	require.Equal(t, pairs, passed, "согласованные машины обязаны пройти ВСЕ")
	require.Equal(t, pairs, refused, "несогласованные обязаны быть отвергнуты ВСЕ")

	var stored int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM instances WHERE placement_group_id = $1`, group).Scan(&stored))
	require.Equal(t, pairs, stored, "в группе обязаны остаться только согласованные машины")
}
