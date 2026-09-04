// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestIntegration_MachineType_Delete_InUse_Restricted — within-service ссылка
// instances.machine_type_id → machine_types(id) обязана держаться FK RESTRICT на
// DB-уровне (data-integrity, раздел within-service, п.1), а не одной software-проверкой
// на Instance.Create.
//
// RED (баг): FK не было — InternalMachineTypeService.Delete делал голый DELETE
// без единой проверки ссылок, поэтому каталожная запись, на которую ссылаются
// живые инстансы, удалялась молча: GET instance продолжал отдавать
// machineTypeId='mt-…', а GET machineTypes/mt-… → NOT_FOUND (dangling ref),
// повторный Create на этом же id падал FAILED_PRECONDITION, а восстановить
// запись с тем же (server-assigned) id было нельзя.
// GREEN: 23503 → ErrFailedPrecondition; вывод типа из эксплуатации — status=RETIRED.
func TestIntegration_MachineType_Delete_InUse_Restricted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	mtRepo := repo.NewMachineTypeRepo(pool)
	instRepo := repo.NewInstanceRepo(pool)

	inUse := newMachineType("std-v3-2", domain.MachineTypeFamilyStandard, 0)
	_, err = mtRepo.Insert(ctx, inUse)
	require.NoError(t, err)
	unused := newMachineType("std-v3-4", domain.MachineTypeFamilyStandard, 0)
	_, err = mtRepo.Insert(ctx, unused)
	require.NoError(t, err)

	in := comp1Instance(ids.NewHyphenID(ids.PrefixInstanceHyphen), "prj-mt-fk", "node-01")
	in.MachineTypeID = inUse.ID
	_, _, err = instRepo.Insert(ctx, in)
	require.NoError(t, err)

	// Занятый тип удалить нельзя — FK RESTRICT.
	err = mtRepo.Delete(ctx, inUse.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, serviceerr.ErrFailedPrecondition, "delete занятого machine-type → FAILED_PRECONDITION")
	assert.Contains(t, err.Error(), "machine type "+inUse.ID+" is in use",
		"конвенционный тон сообщения (не generic FK-текст, не raw pgx)")
	// Каталожная запись на месте — dangling ref не образовался.
	still, gerr := mtRepo.Get(ctx, inUse.ID)
	require.NoError(t, gerr)
	assert.Equal(t, inUse.ID, still.ID)

	// Незанятый тип удаляется как прежде (FK не ломает штатный delete).
	require.NoError(t, mtRepo.Delete(ctx, unused.ID))

	// После удаления инстанса тип освобождается.
	require.NoError(t, instRepo.Delete(ctx, in.ID))
	require.NoError(t, mtRepo.Delete(ctx, inUse.ID))
}

// TestIntegration_MachineType_InsertVsDelete_Race — второе направление того же
// инварианта: software-guard на delete-пути («DELETE … WHERE NOT EXISTS») закрыл
// бы только его, оставив Create-side TOCTOU (INSERT инстанса и DELETE каталога
// под READ COMMITTED не конфликтуют). FK берёт row-share-lock на родительской
// строке при INSERT, поэтому конкурентная пара сериализуется: либо инстанс есть
// и тип не удалён, либо тип удалён и инстанса нет — dangling-строки не остаётся.
func TestIntegration_MachineType_InsertVsDelete_Race(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	mtRepo := repo.NewMachineTypeRepo(pool)
	instRepo := repo.NewInstanceRepo(pool)

	const N = 8
	for i := 0; i < N; i++ {
		mt := newMachineType("race-"+ids.NewID("mtn"), domain.MachineTypeFamilyStandard, 0)
		_, err = mtRepo.Insert(ctx, mt)
		require.NoError(t, err)

		var (
			wg           sync.WaitGroup
			barrier      = make(chan struct{})
			insertOK     atomic.Bool
			deleteOK     atomic.Bool
			unexpectedIn atomic.Int32
			unexpectedDe atomic.Int32
		)
		instID := ids.NewHyphenID(ids.PrefixInstanceHyphen)
		wg.Add(2)
		go func() {
			defer wg.Done()
			in := comp1Instance(instID, "prj-mt-race", "mt-race-inst")
			in.MachineTypeID = mt.ID
			<-barrier
			if _, _, ierr := instRepo.Insert(ctx, in); ierr == nil {
				insertOK.Store(true)
			} else if !errors.Is(ierr, serviceerr.ErrFailedPrecondition) {
				unexpectedIn.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			<-barrier
			if derr := mtRepo.Delete(ctx, mt.ID); derr == nil {
				deleteOK.Store(true)
			} else if !errors.Is(derr, serviceerr.ErrFailedPrecondition) {
				unexpectedDe.Add(1)
			}
		}()
		close(barrier)
		wg.Wait()

		assert.Zero(t, unexpectedIn.Load(), "Insert: только nil или FAILED_PRECONDITION")
		assert.Zero(t, unexpectedDe.Load(), "Delete: только nil или FAILED_PRECONDITION")
		assert.False(t, insertOK.Load() && deleteOK.Load(),
			"нельзя, чтобы и инстанс создался, и его machine-type был удалён — это dangling ref")

		// Пост-инвариант: ни одной instances-строки со ссылкой на отсутствующий тип.
		var dangling int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT count(*) FROM instances i
			 WHERE i.machine_type_id IS NOT NULL
			   AND NOT EXISTS (SELECT 1 FROM machine_types m WHERE m.id = i.machine_type_id)`).Scan(&dangling))
		require.Zero(t, dangling, "dangling instances.machine_type_id")

		// Уборка для следующей итерации.
		_ = instRepo.Delete(ctx, instID)
		_ = mtRepo.Delete(ctx, mt.ID)
	}
}
