// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestIntegration_InstanceUpdate_ColumnScoped_NoLostUpdate — воспроизводит classic
// read-modify-write lost-update: два Update-use-case'а читают одну и ту же строку,
// каждый применяет свою маску к своему СТАЛОМУ снимку, оба пишут. Раньше repo.Update
// писал ВЕСЬ column-set, поэтому второй writer затирал независимое поле, изменённое
// первым (name или description). Column-scoped Update пишет только колонки из
// фактически изменённых полей → оба независимых редактирования выживают.
//
// Последовательность детерминированная (эмулирует interleave use-case'ов), без
// гонки: доказывает семантику scoping, а не тайминг.
func TestIntegration_InstanceUpdate_ColumnScoped_NoLostUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)

	id := ids.NewID(ids.PrefixInstance)
	_, _, err = instRepo.Insert(ctx, &domain.Instance{
		ID: id, ProjectID: "f-lost-upd", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: "old-name", Description: "old-desc",
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning, FQDN: id + ".auto.internal",
		InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
		NetworkInterfaces:  []domain.NetworkInterface{{Index: "0", SubnetID: "e9bsub", PrimaryV4Address: "10.0.0.10"}},
	})
	require.NoError(t, err)

	inA, err := instRepo.Get(ctx, id)
	require.NoError(t, err)
	inA.Name = "new-name"

	inB, err := instRepo.Get(ctx, id)
	require.NoError(t, err)
	inB.Description = "new-desc"

	_, _, err = instRepo.Update(ctx, inA, false, []string{"name"})
	require.NoError(t, err)
	_, _, err = instRepo.Update(ctx, inB, false, []string{"description"})
	require.NoError(t, err)

	final, err := instRepo.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "new-name", final.Name, "A's name edit must survive B's description-only update")
	require.Equal(t, "new-desc", final.Description, "B's description edit must be applied")
}
