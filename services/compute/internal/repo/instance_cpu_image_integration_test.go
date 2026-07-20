// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// TestIntegration_InstanceCPUGuarantee_RoundTrip — the cpu_guarantee_percent column
// round-trips through Insert→Get, and a re-pin of it is a sizing change guarded by the
// same STOPPED CAS as machine_type_id (requireStopped in repo.Update). (The former
// image/image_digest/resources_spec columns were dropped by migration 0016 — sizing is
// now driven by machine_type_id and the OS by bootSource.)
func TestIntegration_InstanceCPUGuarantee_RoundTrip(t *testing.T) {
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

	in := newRunningInstance(inID)
	in.CPUGuaranteePercent = 50
	_, err = instRepo.Insert(ctx, in)
	require.NoError(t, err)

	got, err := instRepo.Get(ctx, inID)
	require.NoError(t, err)
	assert.Equal(t, int32(50), got.CPUGuaranteePercent)

	// cpu_guarantee_percent re-pin is a sizing change → requires STOPPED.
	stopped, err := instRepo.SetStatusCAS(ctx, inID, domain.InstanceStatusRunning, domain.InstanceStatusStopped)
	require.NoError(t, err)
	stopped.CPUGuaranteePercent = 20
	resized, err := instRepo.Update(ctx, stopped, false, []string{"cpu_guarantee_percent"})
	require.NoError(t, err)
	assert.Equal(t, int32(20), resized.CPUGuaranteePercent)
}

// TestIntegration_InstanceCPUGuarantee_CheckConstraint — the DB CHECK (0..100)
// rejects an out-of-range cpu_guarantee_percent at Insert (defence-in-depth beyond
// the service-layer validation).
func TestIntegration_InstanceCPUGuarantee_CheckConstraint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	instRepo := repo.NewInstanceRepo(pool)
	in := newRunningInstance(ids.NewID(ids.PrefixInstance))
	in.CPUGuaranteePercent = 101 // violates CHECK (cpu_guarantee_percent BETWEEN 0 AND 100)
	_, err = instRepo.Insert(ctx, in)
	require.Error(t, err, "cpu_guarantee_percent=101 must be rejected by the DB CHECK")
}
