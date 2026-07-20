// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

func newMachineType(name string, family domain.MachineTypeFamily, gpus int32) *domain.MachineType {
	return &domain.MachineType{
		ID:          ids.NewHyphenID(ids.PrefixMachineTypeHyphen),
		Name:        name,
		Description: "test flavor",
		Family:      family,
		EffectiveResources: domain.EffectiveResources{
			VCPU: 2, MemoryMiB: 8192, GPUs: gpus,
		},
		AvailableZones: []string{"ru-central1-a", "ru-central1-b"},
		Status:         domain.MachineTypeStatusAvailable,
		Labels:         map[string]string{"tier": "gp"},
		CreatedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}
}

// TestIntegration_MachineTypeRepo_CRUD — COMP-1-18/21: Insert/Get/Update/Delete
// round-trip + duplicate name → AlreadyExists (SQLSTATE 23505 via UNIQUE(name)).
func TestIntegration_MachineTypeRepo_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	r := repo.NewMachineTypeRepo(pool)
	mt := newMachineType("std-v3-2", domain.MachineTypeFamilyStandard, 0)
	created, err := r.Insert(ctx, mt)
	require.NoError(t, err)
	assert.Equal(t, "std-v3-2", created.Name)

	got, err := r.Get(ctx, mt.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.MachineTypeFamilyStandard, got.Family)
	assert.Equal(t, int64(8192), got.EffectiveResources.MemoryMiB)
	assert.Equal(t, []string{"ru-central1-a", "ru-central1-b"}, got.AvailableZones)
	assert.Equal(t, "gp", got.Labels["tier"])

	// duplicate name → AlreadyExists (UNIQUE(name), 23505).
	dup := newMachineType("std-v3-2", domain.MachineTypeFamilyStandard, 0)
	_, err = r.Insert(ctx, dup)
	require.ErrorIs(t, err, service.ErrAlreadyExists)

	// update: family + status (name immutable — not in SET).
	got.Family = domain.MachineTypeFamilyCompute
	got.Status = domain.MachineTypeStatusDeprecated
	updated, err := r.Update(ctx, got)
	require.NoError(t, err)
	assert.Equal(t, domain.MachineTypeFamilyCompute, updated.Family)
	assert.Equal(t, domain.MachineTypeStatusDeprecated, updated.Status)

	// delete → Get NotFound.
	require.NoError(t, r.Delete(ctx, mt.ID))
	_, err = r.Get(ctx, mt.ID)
	require.ErrorIs(t, err, service.ErrNotFound)
	// delete of absent → NotFound.
	require.ErrorIs(t, r.Delete(ctx, mt.ID), service.ErrNotFound)
}

// TestIntegration_MachineTypeRepo_ListFilterAndCursor — COMP-1-19: family=/minGpus=/
// name= filters + cursor pagination (created_at, id) ASC.
func TestIntegration_MachineTypeRepo_ListFilterAndCursor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	r := repo.NewMachineTypeRepo(pool)
	// Seed deterministic created_at ordering.
	base := time.Now().UTC().Truncate(time.Microsecond)
	seed := func(name string, fam domain.MachineTypeFamily, gpus int32, off time.Duration) {
		mt := newMachineType(name, fam, gpus)
		mt.CreatedAt = base.Add(off)
		_, ierr := r.Insert(ctx, mt)
		require.NoError(t, ierr)
	}
	seed("std-v3-2", domain.MachineTypeFamilyStandard, 0, 0)
	seed("mem-v2-4", domain.MachineTypeFamilyMemory, 0, time.Second)
	seed("gpu-a100-1", domain.MachineTypeFamilyGPU, 1, 2*time.Second)
	seed("gpu-a100-8", domain.MachineTypeFamilyGPU, 8, 3*time.Second)

	// family=GPU → 2 rows.
	gpus, _, err := r.List(ctx, ports.MachineTypeFilter{Family: domain.MachineTypeFamilyGPU}, ports.Pagination{})
	require.NoError(t, err)
	require.Len(t, gpus, 2)

	// family=GPU & minGpus=4 → only gpu-a100-8.
	big, _, err := r.List(ctx, ports.MachineTypeFilter{Family: domain.MachineTypeFamilyGPU, MinGPUs: 4}, ports.Pagination{})
	require.NoError(t, err)
	require.Len(t, big, 1)
	assert.Equal(t, "gpu-a100-8", big[0].Name)

	// name=mem-v2-4 → exact match.
	named, _, err := r.List(ctx, ports.MachineTypeFilter{Name: "mem-v2-4"}, ports.Pagination{})
	require.NoError(t, err)
	require.Len(t, named, 1)

	// cursor: page_size 2 → first two (by created_at ASC) + next token.
	p1, tok, err := r.List(ctx, ports.MachineTypeFilter{}, ports.Pagination{PageSize: 2})
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.NotEmpty(t, tok)
	assert.Equal(t, "std-v3-2", p1[0].Name)
	assert.Equal(t, "mem-v2-4", p1[1].Name)
	p2, _, err := r.List(ctx, ports.MachineTypeFilter{}, ports.Pagination{PageSize: 2, PageToken: tok})
	require.NoError(t, err)
	require.Len(t, p2, 2)
	assert.Equal(t, "gpu-a100-1", p2[0].Name)
}

// TestIntegration_MachineTypeRepo_ConcurrentNameRace — data-integrity within-service
// invariant: N concurrent Insert with the SAME name → exactly ONE winner, N-1
// AlreadyExists (UNIQUE(name) on the DB level, not software check-then-act). Deterministic —
// a start barrier releases all goroutines simultaneously (no time.Sleep).
func TestIntegration_MachineTypeRepo_ConcurrentNameRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	r := repo.NewMachineTypeRepo(pool)

	const N = 8
	var (
		wg           sync.WaitGroup
		successCnt   atomic.Int32
		existsCnt    atomic.Int32
		otherErrs    []error
		otherErrsMu  sync.Mutex
		startBarrier = make(chan struct{})
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mt := newMachineType("contended-flavor", domain.MachineTypeFamilyStandard, 0)
			<-startBarrier
			_, ierr := r.Insert(ctx, mt)
			switch {
			case ierr == nil:
				successCnt.Add(1)
			case errors.Is(ierr, service.ErrAlreadyExists):
				existsCnt.Add(1)
			default:
				otherErrsMu.Lock()
				otherErrs = append(otherErrs, ierr)
				otherErrsMu.Unlock()
			}
		}()
	}
	close(startBarrier)
	wg.Wait()

	assert.Equal(t, int32(1), successCnt.Load(), "exactly one concurrent Insert must win the UNIQUE(name) slot")
	assert.Equal(t, int32(N-1), existsCnt.Load(), "the other %d must get AlreadyExists", N-1)
	assert.Empty(t, otherErrs, "no unexpected errors")

	list, _, err := r.List(ctx, ports.MachineTypeFilter{Name: "contended-flavor"}, ports.Pagination{})
	require.NoError(t, err)
	assert.Len(t, list, 1, "exactly one row persisted")
}
