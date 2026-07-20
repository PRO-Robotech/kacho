// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Integration-тесты F7 Repository lifecycle (REG-1-27/28/30): персистентность
// EPHEMERAL round-trip, overlay-set auto-promote EPHEMERAL→DURABLE, и concurrent
// promote lifecycle-CAS (partial-race, data-integrity п.5). testcontainers Postgres.
package pg_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// REG-1-27/28 — lifecycle round-trip: InsertConfig(EPHEMERAL) → GetConfig EPHEMERAL
// (персист); UpdateConfig (overlay-set) → auto-promote → DURABLE (наблюдаемо через колонку).
func TestRepoConfig_REG_1_27_28_LifecycleRoundTrip(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc")

	// Явный EPHEMERAL-create → persisted EPHEMERAL (REG-1-27).
	eph := newCfg(regID, "scratch/tmp", domain.VisibilityPrivate, nil)
	eph.Lifecycle = domain.LifecycleEphemeral
	ins, err := repo.InsertConfig(ctx, eph)
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleEphemeral, ins.Lifecycle, "EPHEMERAL сохранён на INSERT")

	got, err := repo.GetConfig(ctx, regID, "scratch/tmp")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleEphemeral, got.Lifecycle, "EPHEMERAL round-trip через GetConfig")

	// overlay-set (UpdateConfig description) → auto-promote DURABLE (REG-1-28).
	upd, err := repo.UpdateConfig(ctx, registry.RepositoryConfigUpdate{
		NamespaceID: regID, Name: "scratch/tmp", Description: "configured", ApplyDescription: true,
	})
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, upd.Lifecycle, "overlay-set auto-promotes EPHEMERAL→DURABLE")

	after, err := repo.GetConfig(ctx, regID, "scratch/tmp")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, after.Lifecycle, "promote персистентен")
}

// REG-1-30 — concurrent promote одного ephemeral-repo: N конкурентных InsertConfig
// durable-overlay того же (namespace_id,name) стартуют одновременно. PRIMARY KEY —
// арбитр: ровно один INSERT коммитит; остальные n-1 ловят 23505 → ErrAlreadyExists
// (на уровне use-case это idempotent-merge: проигравший ретраит UpdateConfig). Финальная
// строка — lifecycle=DURABLE, без double-insert-fail. concurrent-goroutines (data-integrity п.5).
func TestRepoConfig_REG_1_30_ConcurrentPromote_LifecycleCAS(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRepositoryConfigRepo(pool)
	ctx := context.Background()
	regID := seedRegistry(t, pool, "prj-P", "reg-lc30")

	const n = 8
	var wg sync.WaitGroup
	var succeeded int64
	start := make(chan struct{})
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg := newCfg(regID, "pushed/img", domain.VisibilityPrivate, nil)
			cfg.Lifecycle = domain.LifecycleDurable // promote INSERT
			<-start
			_, err := repo.InsertConfig(ctx, cfg)
			errs[i] = err
			if err == nil {
				atomic.AddInt64(&succeeded, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	dup := 0
	for i, err := range errs {
		switch {
		case err == nil:
			// winner
		case errorsIs(err, regerrors.ErrAlreadyExists):
			dup++
		default:
			t.Fatalf("goroutine %d: unexpected error (ожидался ErrAlreadyExists): %v", i, err)
		}
	}
	require.Equal(t, int64(1), atomic.LoadInt64(&succeeded), "ровно один promote-INSERT коммитит")
	require.Equal(t, n-1, dup, "остальные n-1 → ALREADY_EXISTS (idempotent-merge на use-case)")

	got, err := repo.GetConfig(ctx, regID, "pushed/img")
	require.NoError(t, err)
	require.Equal(t, domain.LifecycleDurable, got.Lifecycle, "converge к DURABLE без double-insert-fail")
}
