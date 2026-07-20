// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// TestIntegration_Network_VPC_1_22_ConcurrentSameName_OneWinner — VPC-1-22 (F5):
// две конкурентные Network.Create с одинаковыми (project_id, name) → ровно одна
// коммитится, вторая ловит UNIQUE(project_id,name) 23505 → ErrAlreadyExists.
// DB-backstop authoritative под concurrency (software-precheck пропустил бы обе —
// TOCTOU). Ноль дублей в таблице.
//
// Детерминизм (blocker держит слот, НЕ time.Sleep): TX-A вставляет строку и НЕ
// коммитит; фоновая TX-B пытается вставить дубль и блокируется на уникальном
// индексе (ждёт исхода TX-A) — waitForLockWaiter ловит это ungranted-состояние;
// затем TX-A коммитит → TX-B получает 23505. contested-interleaving исполняется
// на каждом прогоне.
func TestIntegration_Network_VPC_1_22_ConcurrentSameName_OneWinner(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	r := kachopg.New(pool, nil)
	defer r.Close()

	const projectID, name = "prj-namerace", "core-prod"
	mk := func() *domain.Network {
		return &domain.Network{ID: ids.NewID(ids.PrefixNetwork), ProjectID: projectID, Name: domain.RcNameVPC(name)}
	}

	// TX-A: вставляет и держит (без commit).
	wa, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = wa.Networks().Insert(ctx, mk())
	require.NoError(t, err)

	// TX-B в горутине: тот же (project,name) → блокируется на UNIQUE-индексе.
	bDone := make(chan error, 1)
	go func() {
		wb, werr := r.Writer(ctx)
		if werr != nil {
			bDone <- werr
			return
		}
		defer wb.Abort()
		if _, ierr := wb.Networks().Insert(ctx, mk()); ierr != nil {
			bDone <- ierr
			return
		}
		bDone <- wb.Commit()
	}()

	// Дождаться, пока TX-B реально встанет в очередь за индекс-lock'ом.
	waitForLockWaiter(t, ctx, pool)

	// TX-A коммитит → выигрывает; TX-B получает 23505.
	require.NoError(t, wa.Commit())

	bErr := <-bDone
	require.Error(t, bErr, "second concurrent Create with same (project,name) must fail")
	require.ErrorIs(t, bErr, repo.ErrAlreadyExists, "loser must map to ErrAlreadyExists (23505), not a generic error")

	// Ровно одна строка — ноль дублей.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	nets, _, err := rd.Networks().List(ctx, kacho.NetworkFilter{ProjectID: projectID, Name: name}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, nets, 1, "exactly one network must survive the race")
}
