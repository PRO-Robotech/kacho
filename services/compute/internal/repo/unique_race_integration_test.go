// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestGuestKey_ConcurrentSameNameAndSameMaterialLeaveExactlyOne — уникальность
// держат ИНДЕКСЫ, а не порядок вызовов.
//
// Два индекса, два разных предмета, и оба проверяются под конкуренцией:
//
//   - имя различает ключи ДЛЯ ЧЕЛОВЕКА, который выбирает, какой снять; два ключа
//     с одним именем для него неразличимы;
//   - один и тот же материал, заведённый дважды, — это не два ключа, а один, про
//     который забыли.
//
// Без конкурентных горутин проба не свидетельствует ничего: последовательный
// прогон зеленеет и на проверке «прочитал → не нашёл → вставил», между шагами
// которой помещается второй писатель.
func TestGuestKey_ConcurrentSameNameAndSameMaterialLeaveExactlyOne(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := repo.NewGuestAccessKeyRepo(pool)

	t.Run("одно имя — ровно один ключ", func(t *testing.T) {
		const project, name = "proj-race-name", "one-name"
		const n = 8
		var wg sync.WaitGroup
		okCount := make([]bool, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, _, ierr := r.Insert(ctx, &domain.GuestAccessKey{
					ID: ids.NewHyphenID("gak"), ProjectID: project, Name: name,
					// Материал РАЗНЫЙ: предмет этой пробы — имя, и совпадение
					// материала подменило бы проверяемый индекс вторым.
					PublicKey:   "ssh-ed25519 AAAA" + ids.NewHyphenID("gak"),
					Fingerprint: "SHA256:" + ids.NewHyphenID("gak"),
				})
				okCount[i] = ierr == nil
			}(i)
		}
		wg.Wait()

		passed := 0
		for _, ok := range okCount {
			if ok {
				passed++
			}
		}
		require.Equal(t, 1, passed, "ровно одна вставка обязана пройти при любом порядке коммитов")

		var stored int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM guest_access_keys WHERE project_id=$1 AND name=$2`, project, name).Scan(&stored))
		require.Equal(t, 1, stored)
	})

	t.Run("один материал — ровно один ключ", func(t *testing.T) {
		const project = "proj-race-fp"
		fp := "SHA256:" + ids.NewHyphenID("gak")
		const n = 8
		var wg sync.WaitGroup
		okCount := make([]bool, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, _, ierr := r.Insert(ctx, &domain.GuestAccessKey{
					ID: ids.NewHyphenID("gak"), ProjectID: project,
					// Имя РАЗНОЕ: предмет этой пробы — отпечаток.
					Name:        "key-" + ids.NewHyphenID("gak"),
					PublicKey:   "ssh-ed25519 AAAAcommon",
					Fingerprint: fp,
				})
				okCount[i] = ierr == nil
			}(i)
		}
		wg.Wait()

		passed := 0
		for _, ok := range okCount {
			if ok {
				passed++
			}
		}
		require.Equal(t, 1, passed, "один и тот же материал не может дать двух ключей")

		var stored int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM guest_access_keys WHERE project_id=$1 AND fingerprint=$2`, project, fp).Scan(&stored))
		require.Equal(t, 1, stored)
	})

	// Положительный контроль: РАЗНЫЕ проекты вправе держать один и тот же
	// материал. Без него обе пробы выше зеленели бы на уникальности, объявленной
	// глобальной, — а она запретила бы двум арендаторам пользоваться одним
	// рабочим ключом, что не наше дело.
	t.Run("разные проекты держат один материал", func(t *testing.T) {
		fp := "SHA256:" + ids.NewHyphenID("gak")
		for _, p := range []string{"proj-x", "proj-y"} {
			_, _, ierr := r.Insert(ctx, &domain.GuestAccessKey{
				ID: ids.NewHyphenID("gak"), ProjectID: p, Name: "shared",
				PublicKey: "ssh-ed25519 AAAAshared", Fingerprint: fp,
			})
			require.NoError(t, ierr, "проект %s обязан завести свой ключ с тем же материалом", p)
		}
	})
}

// TestPlacementGroup_ConcurrentSameNameLeavesExactlyOne — уникальность имени
// группы держит индекс, а не порядок вызовов.
func TestPlacementGroup_ConcurrentSameNameLeavesExactlyOne(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := repo.NewPlacementGroupRepo(pool)

	const project, name = "proj-plg-race", "one-group"
	const n = 8
	var wg sync.WaitGroup
	okCount := make([]bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, ierr := r.Insert(ctx, &domain.PlacementGroup{
				ID: ids.NewHyphenID("plg"), ProjectID: project, Name: name,
				Strategy: domain.PlacementStrategySpread,
				// Якорь РАЗНЫЙ у половины: предмет пробы — имя, и совпадение
				// якоря ничего к нему не добавляет.
				PlacementType: domain.PlacementTypeZonal,
				ZoneID:        "ru-central1-a",
			})
			okCount[i] = ierr == nil
		}(i)
	}
	wg.Wait()

	passed := 0
	for _, ok := range okCount {
		if ok {
			passed++
		}
	}
	require.Equal(t, 1, passed, "ровно одна вставка обязана пройти при любом порядке коммитов")

	var stored int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM placement_groups WHERE project_id=$1 AND name=$2`, project, name).Scan(&stored))
	require.Equal(t, 1, stored)

	// Положительный контроль: другое имя в том же проекте проходит — иначе
	// «ровно одна» зеленела бы на вставке, сломанной для всех групп сразу.
	_, _, ierr := r.Insert(ctx, &domain.PlacementGroup{
		ID: ids.NewHyphenID("plg"), ProjectID: project, Name: "other-group",
		Strategy: domain.PlacementStrategyPack, PlacementType: domain.PlacementTypeRegional,
		RegionID: "ru-central1",
	})
	require.NoError(t, ierr, "другое имя в том же проекте обязано пройти")
}
