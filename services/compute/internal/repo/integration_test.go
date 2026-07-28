// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

func setupTestDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	pgc, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("kacho_compute_test"),
		postgres.WithUsername("compute"),
		postgres.WithPassword("compute"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgc.Terminate(ctx) })

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(db, "."))

	seedFixtureMachineTypes(t, db)
	return dsn
}

// seedFixtureMachineTypes заводит каталожные строки, на которые ссылаются
// instance-фикстуры этого пакета ("mt-std2" в comp1Instance, "mt-highcpu8" в
// resize-кейсах). Нужно с миграции 0017: instances.machine_type_id — FK на
// machine_types(id) ON DELETE RESTRICT, поэтому без каталога КАЖДЫЙ Insert
// инстанса падал бы 23503 и маскировал предмет своего теста.
//
// created_at намеренно в далёком будущем, а имена — служебные: строки не должны
// сдвигать cursor-порядок (created_at ASC, id ASC) и name-фильтры в тестах
// самого каталога (machine_type_repo_integration_test.go), которые опираются на
// собственные сиды.
func seedFixtureMachineTypes(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO machine_types (id, name, family, v_cpu, memory_mib, status, created_at) VALUES
			('mt-std2',     'itfixture-std2',     0, 2, 8192,  1, TIMESTAMPTZ '2999-01-01 00:00:00Z'),
			('mt-highcpu8', 'itfixture-highcpu8', 0, 8, 16384, 1, TIMESTAMPTZ '2999-01-02 00:00:00Z')
		ON CONFLICT (id) DO NOTHING`)
	require.NoError(t, err)
}

// TestIntegration_InstanceRepo_Lifecycle покрывает post-cutover repo-поверхность
// InstanceRepo: Insert (без привязок — attached_disks удалена), GateForAttach
// state-CAS, SetStatusCAS, MarkDeleting, Delete (финальный row-delete). Attach-state
// живёт в kacho-storage — здесь его нет (см. storage S2 integration).
func TestIntegration_InstanceRepo_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	instRepo := repo.NewInstanceRepo(pool)

	inID := ids.NewID(ids.PrefixInstance)
	in := &domain.Instance{
		ID: inID, ProjectID: "f", CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Name: "vm-1",
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning, FQDN: inID + ".auto.internal",
		InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
	}
	created, err := instRepo.Insert(ctx, in)
	require.NoError(t, err)
	require.Empty(t, created.AttachedDisks)

	// GateForAttach: RUNNING → возвращает self-describing payload.
	zone, project, name, err := instRepo.GateForAttach(ctx, inID)
	require.NoError(t, err)
	require.Equal(t, "ru-central1-a", zone)
	require.Equal(t, "f", project)
	require.Equal(t, "vm-1", name)

	// SetStatusCAS: RUNNING → STOPPED; повтор → FailedPrecondition.
	updated, err := instRepo.SetStatusCAS(ctx, inID, domain.InstanceStatusRunning, domain.InstanceStatusStopped)
	require.NoError(t, err)
	require.Equal(t, domain.InstanceStatusStopped, updated.Status)
	_, err = instRepo.SetStatusCAS(ctx, inID, domain.InstanceStatusRunning, domain.InstanceStatusStopped)
	require.ErrorIs(t, err, service.ErrFailedPrecondition)

	// GateForAttach всё ещё проходит (STOPPED ∈ {RUNNING, STOPPED}).
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.NoError(t, err)

	// MarkDeleting → DELETING; GateForAttach теперь падает (attach-vs-delete гейт).
	di, err := instRepo.MarkDeleting(ctx, inID)
	require.NoError(t, err)
	require.Equal(t, domain.InstanceStatusDeleting, di.Status)
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.ErrorIs(t, err, service.ErrFailedPrecondition)

	// Delete (финальный row-delete) → NotFound на повторном Get.
	require.NoError(t, instRepo.Delete(ctx, inID))
	_, err = instRepo.Get(ctx, inID)
	require.ErrorIs(t, err, service.ErrNotFound)
	// GateForAttach на удалённом → NotFound.
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.ErrorIs(t, err, service.ErrNotFound)
}

func TestIntegration_CatalogRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	// Region/Zone serving (ZoneRepo/RegionRepo) removed — Geography is
	// owned by kacho-geo; the local zones/regions tables are dropped by migration
	// 0011_drop_geography (see TestIntegration_DropGeographyMigration). DiskType
	// stays compute-owned.
	dtr := repo.NewDiskTypeRepo(pool)
	list, _, err := dtr.List(ctx, service.Pagination{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(list), 4) // seeded
	ssd, err := dtr.Get(ctx, "network-ssd")
	require.NoError(t, err)
	require.NotEmpty(t, ssd.ZoneIDs)
}
