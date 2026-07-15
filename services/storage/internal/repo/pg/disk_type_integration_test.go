// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/disktype"
)

// TestDiskTypeGetSeeded — Get seeded-типа (миграция 0004) читает поля.
func TestDiskTypeGetSeeded(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newTestPool(t))
	got, err := dr.Get(context.Background(), seededDiskType)
	require.NoError(t, err)
	require.Equal(t, seededDiskType, got.ID)
	require.Equal(t, "balanced", got.PerformanceTier)
	require.NotNil(t, got.ZoneIDs)
}

// TestDiskTypeGetNotFound — well-formed-но-нет → ErrNotFound "DiskType <id> not found".
func TestDiskTypeGetNotFound(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newTestPool(t))
	_, err := dr.Get(context.Background(), "dtp-nonexistent")
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "got %v", err)
	require.Equal(t, "DiskType dtp-nonexistent not found", err.Error()[len("not found: "):])
}

// TestDiskTypeCreateUpdateAdmin — admin Create (slug id, zone_ids/tier) → Get; повтор
// того же id → AlreadyExists; Update (full-replace name/desc/zone_ids/tier).
func TestDiskTypeCreateUpdateAdmin(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newTestPool(t))
	ctx := context.Background()

	created, err := dr.Insert(ctx, &domain.DiskType{
		ID: "block-nvme", Name: "block-nvme", Description: "nvme",
		ZoneIDs: []string{"region-1-a", "region-1-b"}, PerformanceTier: "nvme",
	})
	require.NoError(t, err)
	require.Equal(t, "block-nvme", created.ID)
	require.Equal(t, []string{"region-1-a", "region-1-b"}, created.ZoneIDs)

	got, err := dr.Get(ctx, "block-nvme")
	require.NoError(t, err)
	require.Equal(t, "nvme", got.PerformanceTier)

	_, err = dr.Insert(ctx, &domain.DiskType{ID: "block-nvme", Name: "dup"})
	require.True(t, stderrors.Is(err, ports.ErrAlreadyExists), "duplicate slug → AlreadyExists, got %v", err)

	upd, err := dr.Update(ctx, "block-nvme", "renamed", "new-desc", []string{"region-2-a"}, "io-max")
	require.NoError(t, err)
	require.Equal(t, "renamed", upd.Name)
	require.Equal(t, "new-desc", upd.Description)
	require.Equal(t, []string{"region-2-a"}, upd.ZoneIDs)
	require.Equal(t, "io-max", upd.PerformanceTier)

	_, err = dr.Update(ctx, "dtp-missing", "x", "", nil, "")
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "update missing → NotFound, got %v", err)
}

// TestDiskTypeDeleteFKRestrict — Q4: Delete типа, на который ссылается том →
// FailedPrecondition "DiskType <id> is in use" (FK RESTRICT 23503); после удаления
// тома delete проходит; несуществующий → NotFound.
func TestDiskTypeDeleteFKRestrict(t *testing.T) {
	pool := newTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, err := dr.Insert(ctx, &domain.DiskType{ID: "block-temp", Name: "temp"})
	require.NoError(t, err)
	vol, err := vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-ondtp",
		ZoneID: "region-1-a", DiskTypeID: "block-temp", SizeBytes: 1 << 30,
	})
	require.NoError(t, err)

	err = dr.Delete(ctx, "block-temp")
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "in-use → FailedPrecondition, got %v", err)
	require.Equal(t, "DiskType block-temp is in use", err.Error()[len("failed precondition: "):])
	_, gerr := dr.Get(ctx, "block-temp")
	require.NoError(t, gerr, "in-use disk type still present")

	require.NoError(t, vr.Delete(ctx, vol.ID))
	require.NoError(t, dr.Delete(ctx, "block-temp"), "delete allowed once no volume references it")
	_, gerr = dr.Get(ctx, "block-temp")
	require.True(t, stderrors.Is(gerr, ports.ErrNotFound))

	err = dr.Delete(ctx, "dtp-missing")
	require.True(t, stderrors.Is(err, ports.ErrNotFound), "delete missing → NotFound, got %v", err)
}

// TestDiskTypeDeleteFKRestrictRace — FK RESTRICT под concurrency (data-integrity п.5):
// пока том ссылается на тип, N конкурентных Delete(тип) все получают
// FailedPrecondition (RESTRICT держит); тип не исчезает. Под -race.
func TestDiskTypeDeleteFKRestrictRace(t *testing.T) {
	pool := newTestPool(t)
	dr := pg.NewDiskTypeRepo(pool)
	vr := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	_, err := dr.Insert(ctx, &domain.DiskType{ID: "block-race", Name: "race"})
	require.NoError(t, err)
	_, err = vr.Insert(ctx, &domain.Volume{
		ID: ids.NewID(domain.PrefixVolume), ProjectID: "prj-1", Name: "vol-race-ref",
		ZoneID: "region-1-a", DiskTypeID: "block-race", SizeBytes: 1 << 30,
	})
	require.NoError(t, err)

	const n = 6
	var blocked, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			derr := dr.Delete(context.Background(), "block-race")
			switch {
			case stderrors.Is(derr, ports.ErrFailedPrecondition):
				blocked.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, n, blocked.Load(), "every delete blocked by live FK reference")
	require.EqualValues(t, 0, other.Load())
	_, gerr := dr.Get(ctx, "block-race")
	require.NoError(t, gerr, "disk type intact while referenced")
}

// TestDiskTypeListCursor — cursor (created_at,id) ASC пагинация каталога.
func TestDiskTypeListCursor(t *testing.T) {
	dr := pg.NewDiskTypeRepo(newTestPool(t))
	ctx := context.Background()

	page1, next, err := dr.List(ctx, disktype.Pagination{PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2, "seed has 5 types → first page of 2")
	require.NotEmpty(t, next)

	seen := map[string]struct{}{}
	for _, d := range page1 {
		seen[d.ID] = struct{}{}
	}
	token := next
	for token != "" {
		var pg2 []*domain.DiskType
		pg2, token, err = dr.List(ctx, disktype.Pagination{PageSize: 2, PageToken: token})
		require.NoError(t, err)
		for _, d := range pg2 {
			_, dup := seen[d.ID]
			require.False(t, dup, "cursor yields no duplicates: %s", d.ID)
			seen[d.ID] = struct{}{}
		}
	}
	require.GreaterOrEqual(t, len(seen), 5, "all seeded types paged through")
}
