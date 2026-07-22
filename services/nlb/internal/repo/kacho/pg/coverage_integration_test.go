// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestCoverage_ListByProject — LB и TG ListByProject (один и тот же query что List, но через wrapper).
func TestCoverage_ListByProject(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const project = "prj01CVR11234567890ll"
	lb := newLB(project, "cov-lb")
	tg := newTG(project, "cov-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()

	lbs, _, err := rd.LoadBalancers().ListByProject(ctx, project, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, lbs, 1)
	assert.Equal(t, lb.ID, lbs[0].ID)

	tgs, _, err := rd.TargetGroups().ListByProject(ctx, project, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, tgs, 1)
	assert.Equal(t, tg.ID, tgs[0].ID)
}

// TestCoverage_HasWiredTargetGroup — оба пути (нет wired-листенера / есть).
// NLB CONTRACT: LB имеет wired TG, если у него есть листенер с непустым
// default_target_group_id (M:N pivot удалён — ассоциация LB↔TG деривится из
// wiring листенеров).
func TestCoverage_HasWiredTargetGroup(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01CVR21234567890ll", "cov2-lb")
	tg := newTG(string(lb.ProjectID), "cov2-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	rd, _ := repo.Reader(ctx)
	has, err := rd.LoadBalancers().HasWiredTargetGroup(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.False(t, has)
	_ = rd.Close()

	// Wire a listener to the TG (default_target_group_id set, direct FK to
	// target_groups(id)) — the NLB CONTRACT replacement for the pivot Attach.
	l := newListener(lb.ID, string(lb.ProjectID), "cov2-lst", 8890)
	l.DefaultTargetGroupID = option.MustNewOption(tg.ID)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, l)
		require.NoError(t, err)
	})

	rd2, _ := repo.Reader(ctx)
	defer func() { _ = rd2.Close() }()
	has2, err := rd2.LoadBalancers().HasWiredTargetGroup(ctx, string(lb.ID))
	require.NoError(t, err)
	assert.True(t, has2)
}

// TestCoverage_ListenerUpdate_SetAllocatedAddress_MoveProject — оставшиеся
// Listener write-методы.
func TestCoverage_ListenerUpdate_SetAllocatedAddress_MoveProject(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01CVR31234567890ll", "cov3-lb")
	tg := newTG(string(lb.ProjectID), "cov3-tg")
	l := newListener(lb.ID, string(lb.ProjectID), "cov3-lst", 8888)
	l.AllocatedAddress = "" // simulate fresh CREATING state
	l.Status = domain.ListenerStatusCreating

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		_, err = w.Listeners().Insert(ctx, l)
		require.NoError(t, err)
	})

	// SetAllocatedAddress — worker-side после VIP-alloc.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.Listeners().SetAllocatedAddress(ctx, string(l.ID), "203.0.113.99")
		require.NoError(t, err)
		assert.Equal(t, domain.IPAddress("203.0.113.99"), rec.AllocatedAddress)
	})

	// Update — name/labels/proxy_protocol_v2.
	l.Name = "cov3-lst-updated"
	l.ProxyProtocolV2 = true
	l.DefaultTargetGroupID = option.MustNewOption(tg.ID)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		cur, gerr := w.Listeners().Get(ctx, string(l.ID))
		require.NoError(t, gerr)
		rec, err := w.Listeners().Update(ctx, l, cur.Xmin)
		require.NoError(t, err)
		assert.Equal(t, domain.LbName("cov3-lst-updated"), rec.Name)
		assert.True(t, rec.ProxyProtocolV2)
		v, ok := rec.DefaultTargetGroupID.Maybe()
		require.True(t, ok)
		assert.Equal(t, tg.ID, v)
	})

	// Listener.MoveProject (cascaded helper, exposed for direct ops).
	const dst = "prj01CVR4DST7890ABCDl"
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rows, err := w.Listeners().MoveProject(ctx, string(lb.ID), dst)
		require.NoError(t, err)
		assert.Equal(t, int64(1), rows, "exactly one listener moved")
	})
}

// TestCoverage_TGUpdate_MoveProject_SetStatusCAS — TG-write coverage.
func TestCoverage_TGUpdate_MoveProject_SetStatusCAS(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj01CVR41234567890ll", "cov4-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	tg.Name = "cov4-tg-renamed"
	tg.DeregistrationDelay = domain.LbDuration(60 * time.Second)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		cur, gerr := w.TargetGroups().Get(ctx, string(tg.ID))
		require.NoError(t, gerr)
		rec, err := w.TargetGroups().Update(ctx, tg, cur.Xmin)
		require.NoError(t, err)
		assert.Equal(t, domain.LbName("cov4-tg-renamed"), rec.Name)
		assert.Equal(t, domain.LbDuration(60*time.Second), rec.DeregistrationDelay)
	})

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.TargetGroups().MoveProject(ctx, string(tg.ID), "prj01CVR4DST7890ABCDl")
		require.NoError(t, err)
		assert.Equal(t, domain.ProjectID("prj01CVR4DST7890ABCDl"), rec.ProjectID)
	})

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.TargetGroups().SetStatusCAS(ctx, string(tg.ID),
			domain.TargetGroupStatusActive, domain.TargetGroupStatusDeleting)
		require.NoError(t, err)
		assert.Equal(t, domain.TargetGroupStatusDeleting, rec.Status)
	})

	// CAS-miss
	w, _ := repo.Writer(ctx)
	defer w.Abort()
	_, err := w.TargetGroups().SetStatusCAS(ctx, string(tg.ID),
		domain.TargetGroupStatusActive, domain.TargetGroupStatusDeleting)
	require.Error(t, err)
	assert.True(t, errors.Is(err, kacho.ErrFailedPrecondition))
}

// TestCoverage_LB_Delete_Success — happy-path delete (без детей).
func TestCoverage_LB_Delete_Success(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	lb := newLB("prj01CVR71234567890ll", "cov7-lb")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		err := w.LoadBalancers().Delete(ctx, string(lb.ID))
		require.NoError(t, err)
	})

	w, _ := repo.Writer(ctx)
	defer w.Abort()
	err := w.LoadBalancers().Delete(ctx, string(lb.ID))
	assert.True(t, errors.Is(err, kacho.ErrNotFound))
}

// TestCoverage_TG_Delete_Success — happy-path TG delete (no children).
func TestCoverage_TG_Delete_Success(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj01CVR81234567890ll", "cov8-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		err := w.TargetGroups().Delete(ctx, string(tg.ID))
		require.NoError(t, err)
	})

	w, _ := repo.Writer(ctx)
	defer w.Abort()
	err := w.TargetGroups().Delete(ctx, string(tg.ID))
	assert.True(t, errors.Is(err, kacho.ErrNotFound))
}

// TestCoverage_PageSize_Range — pageSizeOrDefault граничные cases.
// Тестируется косвенно через List с разными PageSize: 0 (default), -1 (invalid).
func TestCoverage_PageSize_Range(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()
	// 0 → default 50; OK.
	_, _, err := rd.LoadBalancers().List(ctx, kacho.LoadBalancerFilter{}, kacho.Pagination{PageSize: 0})
	require.NoError(t, err)
	// Negative → InvalidArgument.
	_, _, err = rd.LoadBalancers().List(ctx, kacho.LoadBalancerFilter{}, kacho.Pagination{PageSize: -1})
	require.Error(t, err)
}

// TestCoverage_PageToken_Malformed → InvalidArgument.
func TestCoverage_PageToken_Malformed(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()
	_, _, err := rd.LoadBalancers().List(ctx, kacho.LoadBalancerFilter{},
		kacho.Pagination{PageSize: 10, PageToken: "!!!not-base64!!!"})
	require.Error(t, err)
}

// TestCoverage_OutboxEmit_NilPayload — Emit с nil payload → пустой `{}`.
func TestCoverage_OutboxEmit_NilPayload(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		err := w.Outbox().Emit(ctx, "nlb_load_balancer", "nlb01ABC", "prj01ABC", "CREATED", nil)
		require.NoError(t, err)
	})
}

// TestCoverage_ListDrainingExpired_NoRows — empty result path.
func TestCoverage_ListDrainingExpired_NoRows(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	tg := newTG("prj01CVR91234567890ll", "cov9-tg")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
	})

	rd, _ := repo.Reader(ctx)
	defer func() { _ = rd.Close() }()
	out, err := rd.TargetGroups().ListDrainingExpired(ctx, string(tg.ID), 60)
	require.NoError(t, err)
	assert.Empty(t, out)
}
