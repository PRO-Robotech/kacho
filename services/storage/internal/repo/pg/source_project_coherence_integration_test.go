// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// Проекты матрицы source-project-coherence: «жертва» держит приватные Volume/
// Snapshot/Image, «атакующий» пытается засеять ими СВОЙ ресурс (BOLA/cross-project
// data disclosure — содержимое чужого тома материализуется в образ/том атакующего).
const (
	projVictim   = "prj-victim"
	projAttacker = "prj-attacker"
)

// fpText утверждает FailedPrecondition-sentinel и возвращает контрактный текст без
// sentinel-префикса (тон "<Resource> <id> not found", §1.7).
func fpText(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err, "cross-project source must be rejected")
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "want FailedPrecondition, got %v", err)
	const prefix = "failed precondition: "
	require.True(t, strings.HasPrefix(err.Error(), prefix), "got %v", err)
	return err.Error()[len(prefix):]
}

// requireHideExistence — hide-existence lock (security.md §6): ответ на ЧУЖОЙ
// (cross-project) источник обязан быть byte-identical ответу на НЕСУЩЕСТВУЮЩИЙ
// источник — с точностью до самого id. Различимый текст/код = existence-oracle
// («чужой ресурс существует» отличимо от «ресурса нет»).
func requireHideExistence(t *testing.T, foreignErr error, foreignID string, missErr error, missID string) {
	t.Helper()
	require.Equal(t,
		strings.Replace(fpText(t, missErr), missID, "<id>", 1),
		strings.Replace(fpText(t, foreignErr), foreignID, "<id>", 1),
		"cross-project source must be indistinguishable from a genuine miss")
}

// TestSourceCrossProjectHiddenAsNotFound — within-service project-coherence источника
// (BOLA-фикс): Image/Volume/Snapshot нельзя засеять источником ЧУЖОГО проекта.
// Проверяется НАБЛЮДАЕМОЕ поведение: (a) sentinel FailedPrecondition + точный
// контрактный текст "<Resource> <id> not found"; (b) byte-identity с настоящим
// miss'ом (hide-existence); (c) строка-результат НЕ создана (нет частичной
// материализации чужих данных).
func TestSourceCrossProjectHiddenAsNotFound(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ir := pg.NewImageRepo(pool)
	ctx := context.Background()

	// Приватные ресурсы «жертвы».
	victimVol := mkVolume(t, vr, projVictim, "victim-vol", 8<<30)
	victimSnapID := mkSnapshotRow(t, pool, projVictim, "victim-snap", 8<<30)
	victimImg := mkImageFromSnapshot(t, ir, projVictim, "victim-img", "ru-central1", victimSnapID)

	t.Run("image from foreign snapshot", func(t *testing.T) {
		imgID := ids.NewID(domain.PrefixImage)
		foreignErr := insertImage(ctx, ir, imgID, projAttacker, "steal-from-snap", domain.Image{SourceSnapshot: victimSnapID})
		require.Equal(t, fmt.Sprintf("Snapshot %s not found", victimSnapID), fpText(t, foreignErr))

		missID := ids.NewID(domain.PrefixSnapshot)
		missErr := insertImage(ctx, ir, ids.NewID(domain.PrefixImage), projAttacker, "miss-snap", domain.Image{SourceSnapshot: missID})
		requireHideExistence(t, foreignErr, victimSnapID, missErr, missID)

		_, gerr := ir.Get(ctx, imgID)
		require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no image row materialised, got %v", gerr)
	})

	t.Run("image from foreign volume", func(t *testing.T) {
		imgID := ids.NewID(domain.PrefixImage)
		foreignErr := insertImage(ctx, ir, imgID, projAttacker, "steal-from-vol", domain.Image{SourceVolume: victimVol.ID})
		require.Equal(t, fmt.Sprintf("Volume %s not found", victimVol.ID), fpText(t, foreignErr))

		missID := ids.NewID(domain.PrefixVolume)
		missErr := insertImage(ctx, ir, ids.NewID(domain.PrefixImage), projAttacker, "miss-vol", domain.Image{SourceVolume: missID})
		requireHideExistence(t, foreignErr, victimVol.ID, missErr, missID)

		_, gerr := ir.Get(ctx, imgID)
		require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no image row materialised, got %v", gerr)
	})

	t.Run("volume from foreign image", func(t *testing.T) {
		volID := ids.NewID(domain.PrefixVolume)
		foreignErr := insertVolume(ctx, vr, volID, projAttacker, "steal-boot", domain.Volume{SourceImage: victimImg.ID})
		require.Equal(t, fmt.Sprintf("Image %s not found", victimImg.ID), fpText(t, foreignErr))

		missID := ids.NewID(domain.PrefixImage)
		missErr := insertVolume(ctx, vr, ids.NewID(domain.PrefixVolume), projAttacker, "miss-boot", domain.Volume{SourceImage: missID})
		requireHideExistence(t, foreignErr, victimImg.ID, missErr, missID)

		_, gerr := vr.Get(ctx, volID)
		require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no volume row materialised, got %v", gerr)
	})

	t.Run("volume from foreign snapshot", func(t *testing.T) {
		volID := ids.NewID(domain.PrefixVolume)
		foreignErr := insertVolume(ctx, vr, volID, projAttacker, "steal-from-snap", domain.Volume{SourceSnapshot: victimSnapID})
		require.Equal(t, fmt.Sprintf("Snapshot %s not found", victimSnapID), fpText(t, foreignErr))

		missID := ids.NewID(domain.PrefixSnapshot)
		missErr := insertVolume(ctx, vr, ids.NewID(domain.PrefixVolume), projAttacker, "miss-from-snap", domain.Volume{SourceSnapshot: missID})
		requireHideExistence(t, foreignErr, victimSnapID, missErr, missID)

		_, gerr := vr.Get(ctx, volID)
		require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no volume row materialised, got %v", gerr)
	})

	t.Run("snapshot from foreign volume", func(t *testing.T) {
		snapID := ids.NewID(domain.PrefixSnapshot)
		_, _, foreignErr := sr.Insert(ctx, &domain.Snapshot{
			ID: snapID, ProjectID: projAttacker, Name: "steal-snap", SourceVolumeID: victimVol.ID,
		})
		require.Equal(t, fmt.Sprintf("Volume %s not found", victimVol.ID), fpText(t, foreignErr))

		missID := ids.NewID(domain.PrefixVolume)
		_, _, missErr := sr.Insert(ctx, &domain.Snapshot{
			ID: ids.NewID(domain.PrefixSnapshot), ProjectID: projAttacker, Name: "miss-snap", SourceVolumeID: missID,
		})
		requireHideExistence(t, foreignErr, victimVol.ID, missErr, missID)

		_, gerr := sr.Get(ctx, snapID)
		require.True(t, stderrors.Is(gerr, storageerr.ErrNotFound), "no snapshot row materialised, got %v", gerr)
	})

	// Состояние ЧУЖОГО тома не должно просвечивать: не-READY чужой том обязан
	// отдавать тот же "not found", а не "is not ready" (иначе state-oracle).
	t.Run("snapshot from foreign non-ready volume leaks no state", func(t *testing.T) {
		notReady := mkVolume(t, vr, projVictim, "victim-vol-creating", 2<<30)
		_, uerr := pool.Exec(ctx, `UPDATE volumes SET state='CREATING' WHERE id=$1`, notReady.ID)
		require.NoError(t, uerr)

		_, _, foreignErr := sr.Insert(ctx, &domain.Snapshot{
			ID: ids.NewID(domain.PrefixSnapshot), ProjectID: projAttacker, Name: "steal-creating", SourceVolumeID: notReady.ID,
		})
		require.Equal(t, fmt.Sprintf("Volume %s not found", notReady.ID), fpText(t, foreignErr),
			"foreign volume state must not leak through a distinct 'is not ready' text")
	})

	// Happy-path не задет: свой источник в СВОЁМ проекте по-прежнему сеет ресурс.
	t.Run("same-project sources still seed", func(t *testing.T) {
		ownVol := mkVolume(t, vr, projAttacker, "own-vol", 4<<30)
		ownSnap := mkSnapshot(t, sr, projAttacker, "own-snap", ownVol.ID)
		require.EqualValues(t, 4<<30, ownSnap.SizeBytes, "size snapshotted from own volume")

		ownImgFromSnap, _, err := ir.Insert(ctx, &domain.Image{
			ID: ids.NewID(domain.PrefixImage), ProjectID: projAttacker, Name: "own-img-snap",
			RegionID: "ru-central1", SourceSnapshot: ownSnap.ID,
		}, fixtureRegionZones)
		require.NoError(t, err)
		require.EqualValues(t, 4<<30, ownImgFromSnap.SizeBytes, "size derived from own snapshot")

		ownImgFromVol, _, err := ir.Insert(ctx, &domain.Image{
			ID: ids.NewID(domain.PrefixImage), ProjectID: projAttacker, Name: "own-img-vol",
			RegionID: "ru-central1", SourceVolume: ownVol.ID,
		}, fixtureRegionZones)
		require.NoError(t, err)
		require.EqualValues(t, 4<<30, ownImgFromVol.SizeBytes, "size derived from own volume")

		bootVol, _, err := vr.Insert(ctx, &domain.Volume{
			ID: ids.NewID(domain.PrefixVolume), ProjectID: projAttacker, Name: "own-boot",
			ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 4 << 30, SourceImage: ownImgFromSnap.ID,
		}, imageRegionFixture)
		require.NoError(t, err)
		require.Equal(t, ownImgFromSnap.ID, bootVol.SourceImage)

		fromSnap, _, err := vr.Insert(ctx, &domain.Volume{
			ID: ids.NewID(domain.PrefixVolume), ProjectID: projAttacker, Name: "own-from-snap",
			ZoneID: "region-1-a", DiskTypeID: seededDiskType, SizeBytes: 4 << 30, SourceSnapshot: ownSnap.ID,
		}, "")
		require.NoError(t, err)
		require.Equal(t, ownSnap.ID, fromSnap.SourceSnapshot)
	})
}

// insertImage — короткий враппер ImageRepo.Insert (id/project/name + source-поля из
// шаблона), возвращает только ошибку: тесты матрицы ключуются на observable-ошибке.
func insertImage(ctx context.Context, r *pg.ImageRepo, id, project, name string, src domain.Image) error {
	_, _, err := r.Insert(ctx, &domain.Image{
		ID: id, ProjectID: project, Name: name, RegionID: "ru-central1",
		SourceSnapshot: src.SourceSnapshot, SourceVolume: src.SourceVolume,
	}, fixtureRegionZones)
	return err
}

// insertVolume — короткий враппер VolumeRepo.Insert (id/project/name + source-поля).
func insertVolume(ctx context.Context, r *pg.VolumeRepo, id, project, name string, src domain.Volume) error {
	_, _, err := r.Insert(ctx, &domain.Volume{
		ID: id, ProjectID: project, Name: name, ZoneID: "region-1-a",
		DiskTypeID: seededDiskType, SizeBytes: 1 << 30,
		SourceSnapshot: src.SourceSnapshot, SourceImage: src.SourceImage,
	}, imageRegionFixture)
	return err
}
