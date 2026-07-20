// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// REG-1-21 (F7 DB) — InsertConfig без явного lifecycle → DURABLE (default), round-trip.
func TestRepoConfig_REG_1_21_InsertDefaultDurable(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc21")

	_, err := repo.InsertConfig(ctx, newCfg(regID, "backend/api", domain.VisibilityPrivate, nil))
	require.NoError(t, err)
	got, err := repo.GetConfig(ctx, regID, "backend/api")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, got.Lifecycle, "UNSPECIFIED→DURABLE default persisted")
}

// REG-1-22 (F7 DB) — InsertConfig с Lifecycle=EPHEMERAL → EPHEMERAL round-trip.
func TestRepoConfig_REG_1_22_InsertEphemeral(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc22")

	cfg := newCfg(regID, "scratch/tmp", domain.VisibilityPrivate, nil)
	cfg.Lifecycle = domain.LifecycleEphemeral
	_, err := repo.InsertConfig(ctx, cfg)
	require.NoError(t, err)
	got, err := repo.GetConfig(ctx, regID, "scratch/tmp")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleEphemeral, got.Lifecycle)
}

// REG-1-23 (F7 DB) — overlay-set на EPHEMERAL row → UpdateConfig auto-promote → DURABLE.
func TestRepoConfig_REG_1_23_UpdateAutoPromote(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc23")

	cfg := newCfg(regID, "pushed/img", domain.VisibilityPrivate, nil)
	cfg.Lifecycle = domain.LifecycleEphemeral
	_, err := repo.InsertConfig(ctx, cfg)
	require.NoError(t, err)

	updated, err := repo.UpdateConfig(ctx, registryUpdateDesc(regID, "pushed/img", "configured"))
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, updated.Lifecycle, "overlay-set auto-promote EPHEMERAL→DURABLE")

	got, err := repo.GetConfig(ctx, regID, "pushed/img")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, got.Lifecycle)
}

// REG-1-25 (F7 DB, concurrency lifecycle-CAS) — N конкурентных UpdateConfig одной
// EPHEMERAL-строки: все сходятся к DURABLE без double-insert/23505 (row-lock сериализует
// single-statement SET); финальный lifecycle=DURABLE (data-integrity.md п.5 concurrent-race).
func TestRepoConfig_REG_1_25_ConcurrentPromote_LifecycleCAS(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc25")

	cfg := newCfg(regID, "pushed/img", domain.VisibilityPrivate, nil)
	cfg.Lifecycle = domain.LifecycleEphemeral
	_, err := repo.InsertConfig(ctx, cfg)
	require.NoError(t, err)

	const n = 8
	var wg sync.WaitGroup
	var failed int64
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, uerr := repo.UpdateConfig(ctx, registryUpdateDesc(regID, "pushed/img", "cfg")); uerr != nil {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Zero(t, atomic.LoadInt64(&failed), "конкурентный promote идемпотентен — без ошибок")
	got, err := repo.GetConfig(ctx, regID, "pushed/img")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, got.Lifecycle, "все сошлись к DURABLE")
}

// REG-1-24 (F7 DB-invariant) — lifecycle CHECK отвергает out-of-range значение
// (within-service инвариант на DB-уровне, ban #10).
func TestRepoConfig_REG_1_24_LifecycleCheck_RejectsOutOfRange(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc24")

	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_registry.repository_configs (registry_id, name, visibility, lifecycle)
		VALUES ($1, 'bad/lc', 'PRIVATE', 'BOGUS')`, regID)
	require.Error(t, err)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23514", pgErr.Code, "lifecycle CHECK (DURABLE|EPHEMERAL)")
}

// registryUpdateDesc — RepositoryConfigUpdate, применяющий description (ApplyDescription).
func registryUpdateDesc(regID, name, desc string) registry.RepositoryConfigUpdate {
	return registry.RepositoryConfigUpdate{
		RegistryID:       regID,
		Name:             name,
		Description:      desc,
		ApplyDescription: true,
	}
}
