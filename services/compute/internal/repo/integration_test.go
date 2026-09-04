// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// setupTestDB выдаёт тесту собственную базу на одном контейнере пакета: миграции
// уже применены в шаблоне, клон — отдельная база (свой каталог, свои строки, своё
// пространство advisory-lock), поэтому CAS/UNIQUE/race-доказательства этого
// пакета видят ровно ту же изоляцию, что давал отдельный контейнер.
func setupTestDB(t *testing.T) string {
	t.Helper()
	dsn := pgtest.NewDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	seedFixtureMachineTypes(t, db)
	// Строки учёта квоты — по той же причине, что и каталог машинных типов выше:
	// без них КАЖДАЯ вставка ресурса отвергалась бы «потолок не назван» и
	// маскировала предмет своей пробы.
	repo.SeedFixtureQuotas(t, dsn)
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
// (одностейтментная предпроверка состояния, НЕ compare-and-swap), SetStatusCAS,
// MarkDeleting, Delete (финальный row-delete). Attach-state
// живёт в kacho-storage — здесь его нет (см. storage S2 integration).
func TestIntegration_InstanceRepo_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)

	inID := ids.NewID(ids.PrefixInstance)
	in := &domain.Instance{
		ID: inID, ProjectID: "f", CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Name: "vm-1",
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning, FQDN: inID + ".auto.internal",
		InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
	}
	created, _, err := instRepo.Insert(ctx, in)
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
	require.ErrorIs(t, err, serviceerr.ErrFailedPrecondition)

	// GateForAttach всё ещё проходит (STOPPED ∈ {RUNNING, STOPPED}).
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.NoError(t, err)

	// MarkDeleting → DELETING; GateForAttach теперь падает (attach-vs-delete гейт).
	di, err := instRepo.MarkDeleting(ctx, inID)
	require.NoError(t, err)
	require.Equal(t, domain.InstanceStatusDeleting, di.Status)
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.ErrorIs(t, err, serviceerr.ErrFailedPrecondition)

	// Delete (финальный row-delete) → NotFound на повторном Get.
	require.NoError(t, instRepo.Delete(ctx, inID))
	_, err = instRepo.Get(ctx, inID)
	require.ErrorIs(t, err, serviceerr.ErrNotFound)
	// GateForAttach на удалённом → NotFound.
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.ErrorIs(t, err, serviceerr.ErrNotFound)
}

// countingTracer считает запросы, ушедшие в Postgres, отбирая их по подстроке. Нужен,
// чтобы проверить ЧИСЛО стейтментов, а не только ответ: именно число и есть предмет.
type countingTracer struct {
	match string
	n     int
}

func (c *countingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, c.match) {
		c.n++
	}
	return ctx
}

func (c *countingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TestIntegration_InstanceGateForAttach_OneStatementDecidesBothLanes — предпроверка
// обязана решать существование и пригодность состояния ОДНИМ стейтментом.
//
// Почему проверяется ЧИСЛО запросов, а не гонка. Прежняя форма спрашивала дважды
// (conditional SELECT, затем EXISTS на 0 rows), и полоса ответа могла назвать не то, что
// произошло: строка, исчезнувшая между запросами, отвечала FailedPrecondition вместо
// NotFound. Проба «погонять параллельно с удалением» на это НЕ ловится — окно между
// двумя локальными запросами слишком узкое, сорок раундов его не задели, и такая проба
// была бы утверждением, которое не может упасть. Число стейтментов — тот же предмет,
// выраженный детерминированно: два запроса означают два момента времени, один запрос
// означает один снимок.
func TestIntegration_InstanceGateForAttach_OneStatementDecidesBothLanes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	tr := &countingTracer{match: "FROM instances"}
	cfg.ConnConfig.Tracer = tr
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	instRepo := repo.NewInstanceRepo(pool)

	// Полоса «нет инстанса» — та, на которой прежняя форма делала второй запрос.
	tr.n = 0
	_, _, _, err = instRepo.GateForAttach(ctx, ids.NewID(ids.PrefixInstance))
	require.ErrorIs(t, err, serviceerr.ErrNotFound)
	require.Equal(t, 1, tr.n,
		"полоса «инстанса нет» решена %d стейтментами — существование и состояние обязаны "+
			"решаться одним снимком, иначе полоса ответа может назвать не то, что произошло", tr.n)

	// Полоса «есть, но состояние не то» — тот же счёт.
	inID := ids.NewID(ids.PrefixInstance)
	_, _, err = instRepo.Insert(ctx, &domain.Instance{
		ID: inID, ProjectID: "f", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: "vm-lane", ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning,
		FQDN: inID + ".auto.internal", InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
	})
	require.NoError(t, err)
	_, err = instRepo.MarkDeleting(ctx, inID)
	require.NoError(t, err)

	tr.n = 0
	_, _, _, err = instRepo.GateForAttach(ctx, inID)
	require.ErrorIs(t, err, serviceerr.ErrFailedPrecondition)
	require.Equal(t, 1, tr.n,
		"полоса «состояние не то» решена %d стейтментами", tr.n)

	// Положительная полоса — тоже один стейтмент (контроль: правка не превратила
	// счётчик в «чем меньше, тем лучше» за счёт потери проверки состояния).
	okID := ids.NewID(ids.PrefixInstance)
	_, _, err = instRepo.Insert(ctx, &domain.Instance{
		ID: okID, ProjectID: "f", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: "vm-lane-ok", ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning,
		FQDN: okID + ".auto.internal", InstanceKind: domain.InstanceKindVM, MachineTypeID: "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
	})
	require.NoError(t, err)
	tr.n = 0
	zone, project, name, err := instRepo.GateForAttach(ctx, okID)
	require.NoError(t, err)
	require.Equal(t, "ru-central1-a", zone)
	require.Equal(t, "f", project)
	require.Equal(t, "vm-lane-ok", name)
	require.Equal(t, 1, tr.n, "положительная полоса решена %d стейтментами", tr.n)
}
