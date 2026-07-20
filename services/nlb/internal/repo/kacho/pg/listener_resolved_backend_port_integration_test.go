// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/H-BF/corlib/pkg/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestListener_NLB_1_19_ResolvedBackendPort — NLB-1b EXPAND: the derived
// resolved_backend_port read-column echoes the wired TargetGroup.port. Validates the
// scalar subquery in listenerCols on BOTH the INSERT ... RETURNING path (wired at
// creation) and the plain-SELECT Get path; a listener with no wired TG resolves to
// NULL → nil (→ substatus° MISCONFIGURED in type2pb).
func TestListener_NLB_1_19_ResolvedBackendPort(t *testing.T) {
	repo, cleanup := newRepo(t, setupTestDB(t))
	defer cleanup()
	ctx := context.Background()

	const projectID = "prj01RBP000000000001"
	lb := newLB(projectID, "rbp-lb")
	tg := newTG(projectID, "rbp-tg") // newTG default Port = 8080

	// Unwired listener (no default TG) → resolved_backend_port NULL → nil.
	unwired := newListener(lb.ID, projectID, "unwired", 443)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
		_, err = w.TargetGroups().Insert(ctx, tg)
		require.NoError(t, err)
		rec, err := w.Listeners().Insert(ctx, unwired)
		require.NoError(t, err)
		assert.Nil(t, rec.ResolvedBackendPort, "no wired TG → resolved_backend_port nil on INSERT RETURNING")
	})

	// NLB-1b MIGRATE: the direct FK (listeners.default_target_group_id →
	// target_groups(id), migration 0018) requires only that the TG exist — no pivot
	// attach. INSERT a listener wired at creation → INSERT ... RETURNING computes the
	// subquery over the freshly inserted row's default_target_group_id.
	wired := newListener(lb.ID, projectID, "wired", 8443)
	wired.DefaultTargetGroupID = option.MustNewOption(tg.ID)
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		rec, err := w.Listeners().Insert(ctx, wired)
		require.NoError(t, err)
		require.NotNil(t, rec.ResolvedBackendPort, "wired TG → resolved_backend_port set on INSERT RETURNING")
		assert.Equal(t, int32(8080), *rec.ResolvedBackendPort)
	})

	// Plain-SELECT Get path.
	rd, err := repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	gotWired, err := rd.Listeners().Get(ctx, string(wired.ID))
	require.NoError(t, err)
	require.NotNil(t, gotWired.ResolvedBackendPort, "wired TG → resolved_backend_port set on Get")
	assert.Equal(t, int32(8080), *gotWired.ResolvedBackendPort)

	gotUnwired, err := rd.Listeners().Get(ctx, string(unwired.ID))
	require.NoError(t, err)
	assert.Nil(t, gotUnwired.ResolvedBackendPort, "no wired TG → resolved_backend_port nil on Get")

	// Sanity: unwired listener still carries the right domain fields (scan order intact).
	assert.Equal(t, domain.LbPort(443), gotUnwired.Port)
}
