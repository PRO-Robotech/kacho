// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// comp1Instance — валидный redesign-Instance (COMP-1) для integration-сидов.
func comp1Instance(id, project, name string) *domain.Instance {
	return &domain.Instance{
		ID: id, ProjectID: project, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: name, ZoneID: "ru-central1-a", Status: domain.InstanceStatusProvisioning,
		InstanceKind:       domain.InstanceKindVM,
		MachineTypeID:      "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-9k2m4x7q1n8p:22.04-lts", ImageKind: domain.ImageKindStorageImage},
		FQDN:               id + ".auto.internal",
	}
}

// TestIntegration_Instance_COMP_1_30_ConcurrentNameRace (COMP-1-30, mandatory race):
// N конкурентных Insert с ОДИНАКОВЫМ непустым name в одном проекте — ровно один
// выигрывает, остальные получают ErrAlreadyExists (partial UNIQUE(project_id,name)
// WHERE name<>” на DB-уровне, SQLSTATE 23505). Детерминированно: startBarrier
// синхронизирует старт всех goroutine (не time.Sleep). Другой проект / пустой name —
// не коллизят.
func TestIntegration_Instance_COMP_1_30_ConcurrentNameRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)

	const N = 6
	var (
		wg            sync.WaitGroup
		successCnt    atomic.Int32
		alreadyExists atomic.Int32
		otherErr      atomic.Int32
		startBarrier  = make(chan struct{})
		project       = "prj-race"
		contestName   = "trainer-node-01"
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), project, contestName)
			<-startBarrier // все стартуют одновременно
			_, _, ierr := instRepo.Insert(ctx, in)
			switch {
			case ierr == nil:
				successCnt.Add(1)
			case errors.Is(ierr, serviceerr.ErrAlreadyExists):
				alreadyExists.Add(1)
			default:
				otherErr.Add(1)
			}
		}()
	}
	close(startBarrier)
	wg.Wait()

	require.Equal(t, int32(1), successCnt.Load(), "ровно один writer выигрывает UNIQUE-слот")
	require.Equal(t, int32(N-1), alreadyExists.Load(), "остальные → AlreadyExists (DB-backstop)")
	require.Equal(t, int32(0), otherErr.Load(), "никаких неожиданных ошибок")

	// другой проект с тем же name → OK (UNIQUE scoped проектом).
	_, _, err = instRepo.Insert(ctx, comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), "prj-other", contestName))
	require.NoError(t, err)

	// Здесь стояло «пустой name дважды в одном проекте → оба OK»: частичный
	// UNIQUE действительно не ловил name=''. Ни того, ни другого больше нет —
	// миграция 715001 заменила частичный индекс полным и запретила пустое имя
	// единой формой имени. Escape-hatch «машина без имени» упразднён вместе с ним:
	// имя проставляется от id на создании. Утверждение перевёрнуто, а не снято,
	// иначе исчез бы контроль на то, что безымянная строка не заводится.
	_, _, err = instRepo.Insert(ctx, comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), project, ""))
	require.Error(t, err, "пустое имя больше не является допустимым значением")
}

// TestIntegration_Instance_COMP_1_37_DeleteNameRecycle (COMP-1-37): hard-delete строки
// освобождает partial-UNIQUE(project,name)-слот → тот же непустой name снова
// Create-able в проекте (не soft-tombstone). Get после delete → NotFound.
func TestIntegration_Instance_COMP_1_37_DeleteNameRecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)
	const project, name = "prj-recycle", "trainer-node-01"

	id1 := ids.NewHyphenID(ids.PrefixInstanceHyphen)
	_, _, err = instRepo.Insert(ctx, comp1Instance(id1, project, name))
	require.NoError(t, err)

	// дубль до удаления → AlreadyExists (слот занят).
	_, _, err = instRepo.Insert(ctx, comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), project, name))
	require.ErrorIs(t, err, serviceerr.ErrAlreadyExists)

	// hard-delete → строка снята, слот освобождён.
	require.NoError(t, instRepo.Delete(ctx, id1))
	_, err = instRepo.Get(ctx, id1)
	require.ErrorIs(t, err, serviceerr.ErrNotFound, "hard-delete, не tombstone")

	// name-recycle: тот же непустой name снова Create-able.
	id2 := ids.NewHyphenID(ids.PrefixInstanceHyphen)
	_, _, err = instRepo.Insert(ctx, comp1Instance(id2, project, name))
	require.NoError(t, err, "непустой name освобождён hard-delete'ом (partial-UNIQUE slot released)")
}
