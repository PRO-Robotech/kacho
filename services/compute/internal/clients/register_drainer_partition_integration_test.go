// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

// Integration lock for the register-drainer's per-resource apply ORDER over the
// REAL compute_fga_register_outbox table + the REAL partition-head index
// (migration 0018), with the REAL applier (clients.IAMRegisterApplier) and a fake
// kaname that models the resource_mirror it feeds.
//
// The modelled mirror is faithful to services/iam/.../resource_mirror/emitter.go:
//
//	register(v)      → INSERT when absent; else UPDATE only if stored < v
//	                   (source_version-LWW — a stale register is a no-op)
//	unregister(tomb) → DELETE only if stored <= tomb; ABSENT ROW ⇒ PLAIN NO-OP,
//	                   leaving NO tombstone behind.
//
// That asymmetry is the whole point: LWW guards the ON-CONFLICT-UPDATE branch
// only. Once the row is gone there is nothing to compare against, so a reordered
// STALE register falls into the INSERT branch and RESURRECTS the mirror row of a
// DELETED resource — permanently, since iam's reconciler is level-triggered off
// the mirror and re-materialises the owner-tuple forever.
//
// Run: go test ./services/compute/internal/clients/... -race -run PartitionHead

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
)

// ---- fake kaname modelling the resource_mirror ------------------------

type mirrorIAM struct {
	mu      sync.Mutex
	version map[string]time.Time // object → stored source_version (row present)
	log     []string             // ordered apply log, for failure diagnostics
}

func newMirrorIAM() *mirrorIAM { return &mirrorIAM{version: map[string]time.Time{}} }

func (m *mirrorIAM) RegisterResource(_ context.Context, in *iamv1.RegisterResourceRequest, _ ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error) {
	v := in.GetSourceVersion().AsTime()
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, present := m.version[in.GetObject()]
	if !present || stored.Before(v) {
		// INSERT branch (absent) — unconditional; UPDATE branch — LWW-gated.
		m.version[in.GetObject()] = v
	}
	m.log = append(m.log, fmt.Sprintf("register:%s@%s", in.GetObject(), v.UTC().Format(time.RFC3339Nano)))
	return &iamv1.RegisterResourceResponse{}, nil
}

func (m *mirrorIAM) UnregisterResource(_ context.Context, in *iamv1.UnregisterResourceRequest, _ ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error) {
	v := in.GetSourceVersion().AsTime()
	m.mu.Lock()
	defer m.mu.Unlock()
	if stored, present := m.version[in.GetObject()]; present && !stored.After(v) {
		delete(m.version, in.GetObject()) // hard delete — NO tombstone retained
	}
	m.log = append(m.log, fmt.Sprintf("unregister:%s@%s", in.GetObject(), v.UTC().Format(time.RFC3339Nano)))
	return &iamv1.UnregisterResourceResponse{}, nil
}

func (m *mirrorIAM) isPresent(object string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.version[object]
	return ok
}

func (m *mirrorIAM) snapshotLog() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.log...)
}

// ---- helpers --------------------------------------------------------------

// insertPartitionIntent writes one intent row exactly as repo.emitFGARegisterIntent
// does (resource_id = the tuple object's id), with a caller-chosen attempt_count so
// a transient-retry history can be seeded deterministically.
func insertPartitionIntent(ctx context.Context, t *testing.T, pool *pgxpool.Pool,
	eventType, resourceID string, version time.Time, attempt int,
) {
	t.Helper()
	tuple, ok := fgaintent.ProjectHierarchyTuple("Instance", resourceID, "prj-partition")
	require.True(t, ok)
	b, err := fgaintent.Encode(fgaintent.Payload{
		Tuples:          []fgaintent.Tuple{tuple},
		ParentProjectID: "prj-partition",
		SourceVersion:   version,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO compute_fga_register_outbox
		   (event_type, resource_kind, resource_id, payload, attempt_count)
		 VALUES ($1, 'Instance', $2, $3, $4)`,
		eventType, resourceID, b, attempt)
	require.NoError(t, err)
}

func newPartitionDrainer(t *testing.T, pool *pgxpool.Pool, applier *clients.IAMRegisterApplier,
	partitionColumn string, applyConcurrency int,
) *drainer.Drainer[fgaintent.Payload] {
	t.Helper()
	d, err := drainer.New[fgaintent.Payload](
		pool,
		drainer.Config{
			// Mirrors the production wiring in cmd/compute/main.go.
			Table:            "public." + fgaRegisterTable,
			Channel:          fgaRegisterChannel,
			BatchSize:        16,
			MaxAttempts:      10,
			BackoffMin:       50 * time.Millisecond,
			BackoffMax:       200 * time.Millisecond,
			PollFallback:     300 * time.Millisecond,
			ApplyConcurrency: applyConcurrency,
			PartitionColumn:  partitionColumn,
		},
		func(b []byte) (fgaintent.Payload, error) {
			p, decErr := fgaintent.Decode(b)
			if decErr != nil {
				return fgaintent.Payload{}, errors.Join(drainer.ErrPermanent, decErr)
			}
			return p, nil
		},
		applier.Apply,
		nil,
	)
	require.NoError(t, err)
	return d
}

// ---- tests ----------------------------------------------------------------

// TestRegisterDrainer_PartitionHead_MigrationCreatesIndex — migration 0018 applies
// and creates the partial index the partition-head CLAIM depends on. Without it the
// claim's correlated NOT EXISTS degrades to a seq-scan per candidate row (quadratic
// under a backlog), so the index is part of the contract, not an optimisation.
func TestRegisterDrainer_PartitionHead_MigrationCreatesIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := setupDrainerDB(t)

	var indexDef string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT indexdef FROM pg_indexes
		  WHERE tablename = 'compute_fga_register_outbox'
		    AND indexname = 'compute_fga_register_outbox_partition_head_idx'`).Scan(&indexDef))

	// Leading key must equal drainer.Config.PartitionColumn; the trailing id column
	// serves `p.id < t.id`; the partial predicate keeps it sized to the backlog.
	assert.Contains(t, indexDef, "(resource_id, id)")
	assert.Contains(t, indexDef, "WHERE (sent_at IS NULL)")
}

// TestRegisterDrainer_PartitionHead_UnregisterThenStaleRegister — decision table
// pinning WHY cmd/compute wires Config.PartitionColumn = "resource_id".
//
// Both cases seed the same reorder pressure the claim's `ORDER BY (attempt_count,
// id)` creates in production: a register bumped to attempt_count=5 by a transient
// iam outage precedes (smaller id) a fresh attempt_count=0 unregister of the SAME
// instance, with filler rows of OTHER resources in between. The producer's intent is
// register(t1) THEN unregister(t2>t1) — the instance was created and then deleted —
// so the mirror must end up ABSENT.
//
//   - WITHOUT PartitionColumn the fresh unregister is claimed first, no-ops on the
//     absent row (leaving no tombstone), and the stale register then INSERTs → the
//     mirror row of a DELETED instance is RESURRECTED and never reclaimed.
//   - WITH PartitionColumn per-resource FIFO holds cross-batch, so the register
//     applies before the unregister and the mirror ends ABSENT (correct).
func TestRegisterDrainer_PartitionHead_UnregisterThenStaleRegister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	const resourceID = "epd-partition-head"
	const object = "compute_instance:" + resourceID
	t1 := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC) // register (create)
	t2 := t1.Add(time.Minute)                           // unregister (delete), strictly newer

	cases := []struct {
		name            string
		partitionColumn string
		wantPresent     bool
		rationale       string
	}{
		{
			name:            "no PartitionColumn: stale register resurrects the deleted instance",
			partitionColumn: "",
			wantPresent:     true,
			rationale: "source_version-LWW guards only the ON-CONFLICT-UPDATE branch; the hard delete " +
				"leaves no tombstone, so the reordered stale register takes the INSERT branch",
		},
		{
			name:            "PartitionColumn=resource_id: per-resource FIFO keeps register before unregister",
			partitionColumn: "resource_id",
			wantPresent:     false,
			rationale:       "claim never takes a successor ahead of a deliverable same-resource predecessor",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pool := setupDrainerDB(t)
			iam := newMirrorIAM()
			applier := clients.NewIAMRegisterApplierWithClient(iam)

			// R: register(t1), already bumped by a transient iam outage. Inserted
			// FIRST → smallest id (create precedes delete).
			insertPartitionIntent(ctx, t, pool, fgaintent.EventRegister, resourceID, t1, 5)
			// U: unregister(t2), fresh. Inserted SECOND → id > R.
			insertPartitionIntent(ctx, t, pool, fgaintent.EventUnregister, resourceID, t2, 0)
			// Filler rows of OTHER resources, all attempt_count=0, so the bumped
			// register (attempt=5) sorts behind ALL of them and cannot be claimed
			// until they are drained.
			//
			// The count matters and four is not enough. The claim takes a RANDOM
			// small limit (1..4) per iteration and the appliers run concurrently, so
			// with a handful of fillers the batch carrying the fresh unregister and
			// the batch carrying the stale register are in flight AT THE SAME TIME —
			// they race, and the race lands on the non-inverting side often enough
			// that this case stopped reproducing the defect it exists to demonstrate.
			// A control that cannot construct its scenario is not a control: the
			// guard below stays green while nothing proves it is needed.
			//
			// Sizing it past BOTH the claim batch size and the apply concurrency
			// makes the inversion deterministic: the unregister is claimed in the
			// first iteration and long applied before the register is reached.
			const fillers = 20 // > BatchSize (16) and > ApplyConcurrency (16)
			for i := 0; i < fillers; i++ {
				insertPartitionIntent(ctx, t, pool, fgaintent.EventRegister,
					fmt.Sprintf("epd-filler-%02d", i), t1, 0)
			}

			// ApplyConcurrency mirrors the production default (16): a throughput knob,
			// never the source of the reorder.
			d := newPartitionDrainer(t, pool, applier, tc.partitionColumn, 16)
			go func() { _ = d.Run(ctx) }()

			require.Eventually(t, func() bool {
				var pending int
				require.NoError(t, pool.QueryRow(ctx,
					`SELECT count(*) FROM compute_fga_register_outbox WHERE sent_at IS NULL`).Scan(&pending))
				return pending == 0
			}, 30*time.Second, 100*time.Millisecond, "outbox never fully drained")

			assert.Equalf(t, tc.wantPresent, iam.isPresent(object),
				"mirror row present=%v, want %v — %s; apply log=%v",
				iam.isPresent(object), tc.wantPresent, tc.rationale, iam.snapshotLog())
		})
	}
}

// TestRegisterDrainerCompute_PendingIndexSetIsExactlyTwo — предмет: НАБОР частичных индексов по
// неотправленным строкам, а не наличие двух нужных.
//
// Проверка «эти два индекса есть» утверждает наличие и никогда — отсутствие,
// поэтому третий индекс по тем же строкам она пропускает. А третий индекс здесь
// не безобиден: очередь почти всё время пуста, последний сбор статистики почти
// всегда пришёлся на пустой бэклог, и во всплеск планировщик входит с оценкой в
// одну строку. На такой оценке сортировка бесплатна, поэтому любой более узкий
// частичный индекс по тем же строкам выглядит дешевле упорядоченного — и план
// перестаёт останавливаться рано: анти-соединение по партиции прогоняется по
// разу на КАЖДУЮ неотправленную строку, то есть выборка читает всю очередь,
// которую разгребает.
//
// Поэтому утверждается равенство множества: перечисляем определения всех
// частичных индексов по `sent_at IS NULL` и требуем ровно два — по ключу
// партиции и по порядку выборки.
func TestRegisterDrainerCompute_PendingIndexSetIsExactlyTwo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := setupDrainerDB(t)

	var defs []string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT coalesce(array_agg(indexdef ORDER BY indexname), '{}')
		   FROM pg_indexes
		  WHERE tablename = 'compute_fga_register_outbox'
		    AND indexdef LIKE '%WHERE (sent_at IS NULL)%'`).Scan(&defs))

	assert.Lenf(t, defs, 2,
		"compute_fga_register_outbox обязана нести РОВНО два частичных индекса по неотправленным строкам — "+
			"(resource_id, id) для поиска головы партиции и (attempt_count, id) для "+
			"упорядоченного внешнего прохода. Любой третий порядок по тем же строкам планировщик "+
			"берёт при статистике пустой очереди, и выборка теряет раннюю остановку по LIMIT. "+
			"Найдено: %v", defs)

	joined := ""
	for _, d := range defs {
		joined += d + "\n"
	}
	assert.Contains(t, joined, "(resource_id, id)")
	assert.Contains(t, joined, "(attempt_count, id)")
}
