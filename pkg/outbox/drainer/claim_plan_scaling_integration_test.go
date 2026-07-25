// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package drainer

// Integration test: the partition-head-only CLAIM must stay CONSTANT-cost in
// backlog depth.
//
// # Root cause this locks (throughput inversion of kacho-iam fga_outbox)
//
//	The partition-head-only claim (Config.PartitionColumn) reads
//
//	    SELECT t.id FROM <t> t
//	     WHERE t.sent_at IS NULL AND t.attempt_count < $1
//	       AND NOT EXISTS (SELECT 1 FROM <t> p
//	                        WHERE p.sent_at IS NULL AND p.attempt_count < $1
//	                          AND p.id < t.id AND p.<part> = t.<part>)
//	     ORDER BY t.attempt_count, t.id
//	     FOR UPDATE OF t SKIP LOCKED
//	     LIMIT $2
//
//	The service migration supplied the index for the correlated NOT EXISTS
//	(`((<part>), id) WHERE sent_at IS NULL`) but NOT one for the OUTER ordered
//	scan `ORDER BY (attempt_count, id)` over `WHERE sent_at IS NULL AND
//	attempt_count < $1`. With no index able to produce that order, Postgres never
//	reaches the nested-loop anti-join the partition index was built for: it
//	seq-scans the WHOLE pending backlog, sorts it, and hash-anti-joins it against
//	a second full seq-scan of the same backlog. The partition index is then dead
//	weight and the claim degrades to O(backlog).
//
//	Measured on a live stand (kind, Postgres 16, real fga_outbox rows):
//
//	    backlog   claim (no ordering index)   claim (with ordering index)
//	      5 000               11.7 ms                     0.81 ms
//	     10 000               28.2 ms                     0.66 ms
//	     20 000               61.6 ms                     0.72 ms
//	     40 000              105.7 ms                     0.75 ms
//	     80 000              327.0 ms                     0.82 ms
//
//	That shape is the throughput INVERSION mechanism, not merely slowness: the
//	deeper the queue, the slower each claim, the slower the drain, the deeper the
//	queue. A producer only marginally faster than the consumer therefore diverges
//	without bound instead of settling at a small standing lag. On the measured
//	e2e run a ~150 rows/s producer against a ~147 rows/s consumer built a 70+
//	second materialization lag — long enough for a first repository create to
//	answer 404 for 64 s while the gateway scope-extractor could not yet resolve
//	it.
//
// # What is asserted (behaviour, not structure)
//
//	The claim's EXECUTION does not scale with backlog depth: rows examined at 8x
//	the depth stay within a small constant factor, and no plan node seq-scans the
//	outbox table. Asserting the plan rather than wall-clock keeps the test
//	deterministic (no timing flake) while still failing on exactly the regression
//	that hurt: a claim whose work grows with the queue it is meant to drain.
//
// Run: go test ./pkg/outbox/drainer/... -run ClaimPlan -race

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// claimPlanTable is the canonical outbox table under test, created with EXACTLY
// the index set kacho-iam ships for kacho_iam.fga_outbox (migrations 0002 +
// 0061 + 0063). Kept in sync with services/iam/internal/migrations — the iam
// side owns the migration, this mirror lets the drainer prove the claim plan the
// index set produces.
const claimPlanTable = `
CREATE TABLE outbox (
    id            bigserial    PRIMARY KEY,
    event_type    text         NOT NULL,
    payload       jsonb        NOT NULL,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    sent_at       timestamptz,
    last_error    text,
    attempt_count integer      NOT NULL DEFAULT 0
);

-- migration 0002: oldest-pending-age metric support.
CREATE INDEX outbox_pending_idx
    ON outbox (created_at) WHERE sent_at IS NULL;

-- migration 0061: correlated NOT EXISTS (partition-head predecessor lookup).
CREATE INDEX outbox_partition_head_idx
    ON outbox ((payload->>'object'), id) WHERE sent_at IS NULL;

-- migration 0063: OUTER ordered scan ORDER BY (attempt_count, id). Without it the
-- planner cannot use outbox_partition_head_idx at all and the claim degrades to a
-- double seq-scan of the whole pending backlog.
CREATE INDEX outbox_claim_order_idx
    ON outbox (attempt_count, id) WHERE sent_at IS NULL;
`

// Test_ClaimPlan_DoesNotScaleWithBacklogDepth pins the claim to constant cost in
// queue depth. RED before the ordering index exists: the plan seq-scans the whole
// pending backlog twice and the rows examined grow ~linearly with it.
func Test_ClaimPlan_DoesNotScaleWithBacklogDepth(t *testing.T) {
	if testing.Short() || os.Getenv("SKIP_INTEGRATION") == "1" {
		t.Skip("integration tests skipped (SKIP_INTEGRATION=1)")
	}

	const (
		shallowBacklog = 5_000
		deepBacklog    = 40_000 // 8x deeper
		// rowsPerPartition mirrors the live stand: kacho_iam.fga_outbox carried
		// ~25-40 rows per object during the burst, so only ~1 row in 30 is a
		// claimable partition head. A claim must still find its 16 heads without
		// reading the backlog.
		rowsPerPartition = 30
		maxAttempts      = 10
		claimLimit       = 16
		// The claim may read a small multiple more rows at 8x depth (planner
		// noise, page boundaries) but MUST NOT track the backlog. 8x depth with a
		// linear claim would be ~8x the rows; 3x is far below that and far above
		// any legitimate constant-cost variance.
		maxRowsGrowthFactor = 3.0
	)

	ctx := context.Background()
	pool := setupClaimPlanPG(ctx, t)

	claimSQL := buildClaimQuery("outbox", "payload->>'object'")

	seedBacklog(ctx, t, pool, 0, shallowBacklog, rowsPerPartition)
	shallowRows, _ := explainClaim(ctx, t, pool, claimSQL, maxAttempts, claimLimit)

	seedBacklog(ctx, t, pool, shallowBacklog, deepBacklog, rowsPerPartition)
	deepRows, deepPlan := explainClaim(ctx, t, pool, claimSQL, maxAttempts, claimLimit)

	t.Logf("rows examined: shallow(%d backlog)=%d  deep(%d backlog)=%d",
		shallowBacklog, shallowRows, deepBacklog, deepRows)

	require.NotContains(t, deepPlan, "Seq Scan",
		"claim seq-scans the outbox at depth — the partition index cannot be used "+
			"without an index producing ORDER BY (attempt_count, id) over the pending "+
			"rows, so the claim degrades to O(backlog) and the drain rate falls as the "+
			"queue it must drain grows.\nplan:\n%s", deepPlan)

	require.Positive(t, shallowRows, "explain reported no rows examined")
	growth := float64(deepRows) / float64(shallowRows)
	require.LessOrEqual(t, growth, maxRowsGrowthFactor,
		"claim cost tracks backlog depth (%.1fx more rows examined for 8x deeper queue): "+
			"the drain slows down exactly as the backlog grows, so a producer marginally "+
			"faster than the consumer diverges without bound.\nplan:\n%s", growth, deepPlan)
}

// setupClaimPlanPG spins a Postgres testcontainer with the canonical outbox
// table. Own setup (not the drainer_test helper) because this file is an
// internal test — it needs buildClaimQuery, which is unexported.
func setupClaimPlanPG(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ctr, err := postgres.Run(startCtx, "postgres:16-alpine",
		postgres.WithDatabase("drainer_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		termCtx, termCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer termCancel()
		_ = ctr.Terminate(termCtx)
	})

	dsn, err := ctr.ConnectionString(startCtx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, claimPlanTable)
	require.NoError(t, err, "create outbox table")

	return pool
}

// seedBacklog inserts unsent rows [from, to) spread over partitions of
// rowsPerPartition rows each, then ANALYZEs so the planner has real statistics
// (a claim on a live service always runs against an analyzed table).
func seedBacklog(ctx context.Context, t *testing.T, pool *pgxpool.Pool, from, to, rowsPerPartition int) {
	t.Helper()

	_, err := pool.Exec(ctx, `
		INSERT INTO outbox (event_type, payload)
		SELECT CASE WHEN i % 5 = 0 THEN 'fga.tuple.delete' ELSE 'fga.tuple.write' END,
		       jsonb_build_object(
		           'user',     'user:usr' || (i % 20)::text,
		           'relation', 'v_get',
		           'object',   'vpc_network:net' || (i / $3)::text)
		FROM generate_series($1, $2 - 1) AS i`,
		from, to, rowsPerPartition)
	require.NoError(t, err, "seed backlog rows [%d,%d)", from, to)

	_, err = pool.Exec(ctx, `ANALYZE outbox`)
	require.NoError(t, err)
}

// explainClaim runs the real claim statement under EXPLAIN (ANALYZE) inside a
// rolled-back transaction — the claim is an UPDATE, so the rollback keeps
// attempt_count and row locks from leaking into the next measurement. Returns
// the total rows examined across all plan nodes plus the rendered plan.
func explainClaim(ctx context.Context, t *testing.T, pool *pgxpool.Pool, claimSQL string, maxAttempts, limit int) (int, string) {
	t.Helper()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	var raw []byte
	err = tx.QueryRow(ctx,
		`EXPLAIN (ANALYZE, FORMAT JSON) `+claimSQL, maxAttempts, limit,
	).Scan(&raw)
	require.NoError(t, err, "explain claim")

	var explained []struct {
		Plan map[string]any `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal(raw, &explained))
	require.NotEmpty(t, explained)

	var (
		rows  int
		nodes []string
	)
	walkPlan(explained[0].Plan, &rows, &nodes)
	return rows, strings.Join(nodes, "\n")
}

// walkPlan sums "Actual Rows" over every node of an EXPLAIN JSON plan tree and
// renders one line per node. Rows examined (not wall-clock) is the deterministic
// witness of "does this claim read the whole backlog".
func walkPlan(node map[string]any, rows *int, nodes *[]string) {
	if node == nil {
		return
	}
	nodeType, _ := node["Node Type"].(string)
	actual, _ := node["Actual Rows"].(float64)
	loops, _ := node["Actual Loops"].(float64)
	if loops < 1 {
		loops = 1
	}
	*rows += int(actual * loops)
	*nodes = append(*nodes, fmt.Sprintf("  %-20s actual_rows=%d loops=%d", nodeType, int(actual), int(loops)))

	children, _ := node["Plans"].([]any)
	for _, c := range children {
		child, ok := c.(map[string]any)
		if !ok {
			continue
		}
		walkPlan(child, rows, nodes)
	}
}
