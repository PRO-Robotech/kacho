// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Integration-тест конкурентной гонки Create по инварианту partial
// UNIQUE(global_slug) WHERE status<>'DELETING' (REG-1 F3, REG-1-12): N goroutines
// вставляют namespace с РАЗНЫМИ (project_id, name), но ОДНИМ explicit global_slug.
// Арбитр глобальной уникальности — DB partial UNIQUE(global_slug): ровно одна tx
// коммитит, остальные ловят 23505 → ErrAlreadyExists (exactly-one-wins, не software
// check-then-act, ban #10). data-integrity.md чек-лист п.5 — на каждый спорный DB-инвариант.
package pg_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// REG-1-12 — concurrent Create РАЗНЫХ (project,name), но SAME explicit global_slug:
// partial UNIQUE(global_slug) — единственный арбитр. Ровно одна tx коммитит; проигравшие
// откатываются целиком (ни строки в registries, ни orphan register-intent в outbox).
func TestNamespace_REG_1_12_ConcurrentCreate_GlobalSlugRace(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewNamespaceRepo(pool)
	ctx := context.Background()

	const (
		n    = 8
		slug = "shared-slug"
	)

	var wg sync.WaitGroup
	var succeeded int64
	start := make(chan struct{})
	errs := make([]error, n)
	ids := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// distinct (project_id, name) — SAME global_slug: конфликт решает partial
			// UNIQUE(global_slug), НЕ UNIQUE(project,name) (те у всех разные).
			reg := newReg(fmt.Sprintf("prj-%02d", i), fmt.Sprintf("images-%02d", i), nil)
			reg.GlobalSlug = slug
			ids[i] = reg.ID
			<-start
			_, err := repo.Insert(ctx, reg, domain.RegisterIntentForCreate(reg, "user", "usr-alice"))
			errs[i] = err
			if err == nil {
				atomic.AddInt64(&succeeded, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	winner, dup := -1, 0
	for i, err := range errs {
		switch {
		case err == nil:
			winner = i
		case errors.Is(err, regerrors.ErrAlreadyExists):
			dup++
		default:
			t.Fatalf("goroutine %d: unexpected error (ожидался ErrAlreadyExists): %v", i, err)
		}
	}
	require.Equal(t, int64(1), atomic.LoadInt64(&succeeded), "ровно одна Create коммитит (global_slug — арбитр)")
	require.NotEqual(t, -1, winner, "должен быть ровно один победитель")
	require.Equal(t, n-1, dup, "остальные n-1 → ALREADY_EXISTS (partial UNIQUE(global_slug))")

	// DB-арбитр: ровно одна живая строка с этим global_slug.
	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_registry.registries WHERE global_slug=$1`, slug).Scan(&rows))
	require.Equal(t, 1, rows, "в persist ровно одна строка с этим global_slug")

	// Проигравшие откатываются целиком — их register-intent'ов в outbox нет.
	for i, id := range ids {
		if i == winner {
			continue
		}
		require.Equal(t, 0, countOutbox(t, pool, id, domain.FGAEventRegister),
			"loser: rollback — нет orphan register-intent")
	}
}
