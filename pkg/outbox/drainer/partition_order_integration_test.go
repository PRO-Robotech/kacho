// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package drainer_test

// Integration tests for Config.PartitionColumn — order-preserving concurrent
// drain that closes the cross-batch reorder LEAK.
//
// Background / root cause (CRIT-1)
//
//	iam's fga_outbox carries BOTH tuple WRITES (grant / label-register) AND
//	DELETES (revoke / delete-stale) of the SAME (user,relation,object). These are
//	NOT commutative. ApplyConcurrency>1 does not preserve apply order — and the
//	claim `ORDER BY (attempt_count,id)` splits a bumped WRITE (attempt≥1 after a
//	transient failure) and a fresh DELETE (attempt=0) into DIFFERENT claim batches.
//	The delete is then claimed and applied BEFORE its predecessor write →
//	delete-before-write → the tuple survives the revoke → authz OVER-GRANT leak.
//
//	Fix (Config.PartitionColumn = "payload->>'object'"): claim never takes a row
//	if a DELIVERABLE (sent_at IS NULL AND attempt_count < MaxAttempts) same-object
//	predecessor with a smaller id exists. A successor is therefore never claimed
//	ahead of an unsent predecessor — per-partition FIFO holds cross-batch (and
//	cross-replica), so a write always applies before a later delete of the same
//	object.
//
// Run: go test ./pkg/outbox/drainer/... -race -p 1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
)

// tupleState is an order-sensitive fake OpenFGA: it models per-object tuple
// PRESENCE, honouring event_type (write → present, delete → absent) in the exact
// order the drainer invokes Apply. A delete of an absent tuple is an idempotent
// no-op (FGA 404 → success); a write is idempotent-add. The final presence of an
// object thus reflects the LAST apply of that object — so a reordered
// delete-then-write leaves it PRESENT (the leak), while write-then-delete leaves
// it ABSENT (correct). Concurrency-safe (ApplyConcurrency>1 fans applies out).
type tupleState struct {
	mu      sync.Mutex
	present map[string]bool
	log     []string // ordered "write:<obj>" / "delete:<obj>"
	delay   time.Duration
}

func newTupleState() *tupleState { return &tupleState{present: map[string]bool{}} }

// apply is a drainer.Applier[rawPayload] over the modelled tuple set.
func (s *tupleState) apply(ctx context.Context, eventType string, payload rawPayload) error {
	var e struct {
		Object string `json:"object"`
	}
	if err := json.Unmarshal(payload, &e); err != nil {
		return errors.Join(drainer.ErrPermanent, err)
	}
	// Delay OUTSIDE the lock so applies of distinct objects genuinely overlap
	// under ApplyConcurrency>1 (models a slow peer).
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch eventType {
	case "fga.tuple.write":
		s.present[e.Object] = true
		s.log = append(s.log, "write:"+e.Object)
	case "fga.tuple.delete":
		delete(s.present, e.Object)
		s.log = append(s.log, "delete:"+e.Object)
	default:
		return errors.Join(drainer.ErrPermanent, fmt.Errorf("unknown event_type %q", eventType))
	}
	return nil
}

func (s *tupleState) isPresent(obj string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.present[obj]
}

// opsFor returns the ordered apply log restricted to one object.
func (s *tupleState) opsFor(obj string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.log {
		if e == "write:"+obj || e == "delete:"+obj {
			out = append(out, e)
		}
	}
	return out
}

func (s *tupleState) snapshotLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// insertOutboxRowAttempt inserts one fga_outbox row with an explicit
// attempt_count (models a row already bumped by prior transient claims).
func insertOutboxRowAttempt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType, payload string, attempt int) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO kacho_iam.fga_outbox (event_type, payload, attempt_count)
		 VALUES ($1, $2::jsonb, $3) RETURNING id`,
		eventType, payload, attempt,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// insertOutboxRowAged inserts a row with an explicit attempt_count AND a
// backdated created_at (now()-age) — models a partition head that has been unsent
// for a while (wedge attribution tests).
func insertOutboxRowAged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType, payload string, attempt int, age time.Duration) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO kacho_iam.fga_outbox (event_type, payload, attempt_count, created_at)
		 VALUES ($1, $2::jsonb, $3, now() - make_interval(secs => $4)) RETURNING id`,
		eventType, payload, attempt, age.Seconds(),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// startDrainerApplier launches one drainer over a caller-supplied
// drainer.Applier[rawPayload] (the order-sensitive tupleState.apply), rather than
// the shared record-only fakeApplier.
func startDrainerApplier(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg drainer.Config,
	applier drainer.Applier[rawPayload],
	opts ...drainer.Option[rawPayload],
) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	d, err := drainer.New[rawPayload](pool, cfg, rawDecoder, applier, testLogger(), opts...)
	require.NoError(t, err)
	dCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = d.Run(dCtx)
	}()
	return cancel, done
}

// Test_1_4_40_PartitionOrder_CrossBatch_NoDeleteBeforeWrite — the CRIT-1 RED→GREEN
// lock. A bumped WRITE (attempt_count=5) and a fresh DELETE (attempt_count=0) of
// the SAME object, plus fresh rows of other objects that fill the small claim
// batches so the write and delete land in different batches under
// ORDER BY (attempt_count,id). Without partition-head-only claim the delete is
// applied first and the write re-materialises the tuple → PRESENT (leak). With
// the fix the write applies before the delete → ABSENT.
func Test_1_4_40_PartitionOrder_CrossBatch_NoDeleteBeforeWrite(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, _ := setupDrainerPG(t)
	st := newTupleState()

	const obj = "vpc_network:leak0000000000000001"

	// W: write(obj), already bumped to attempt_count=5 (prior transient history).
	// Inserted FIRST → smallest id (grant precedes revoke).
	wID := insertOutboxRowAttempt(t, ctx, pool, "fga.tuple.write",
		fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":%q}`, obj), 5)
	// D: delete(obj), fresh attempt_count=0. Inserted SECOND → id > wID (revoke).
	dID := insertOutboxRowAttempt(t, ctx, pool, "fga.tuple.delete",
		fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":%q}`, obj), 0)
	// ≥2 fresh rows of OTHER objects (attempt_count=0) → fill claim batches so the
	// attempt=5 write and the attempt=0 delete never share one 2-row batch.
	fresh := make([]int64, 0, 4)
	for i := 0; i < 4; i++ {
		fresh = append(fresh, insertOutboxRowAttempt(t, ctx, pool, "fga.tuple.write",
			fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":"other:obj%02d"}`, i), 0))
	}

	cfg := testCfg()
	cfg.ApplyConcurrency = 2
	cfg.PartitionColumn = "payload->>'object'" // fix under test
	cfg.MaxAttempts = 10
	cfg.PollFallback = 300 * time.Millisecond

	dCancel, done := startDrainerApplier(t, ctx, pool, cfg, st.apply)
	defer func() { dCancel(); <-done }()

	waitForListenerReady(t, ctx, pool, cfg.Channel)

	// All rows must reach sent_at (the drain completes for both paths).
	all := append([]int64{wID, dID}, fresh...)
	for _, id := range all {
		waitForRowSent(t, ctx, pool, id, 30*time.Second)
	}

	// The delete is the LAST op on obj (by id order) → correct final state = ABSENT.
	require.Falsef(t, st.isPresent(obj),
		"cross-batch reorder leak: obj %q tuple PRESENT after revoke — apply log=%v",
		obj, st.snapshotLog())
	// And the two ops must have applied in id order (write then delete).
	assert.Equalf(t, []string{"write:" + obj, "delete:" + obj}, st.opsFor(obj),
		"same-object ops must apply in id order (write→delete); log=%v", st.snapshotLog())
}

// Test_1_4_41_PartitionOrder_MultiOp_StrictFIFO — a 6-op interleaved
// write/delete sequence on ONE object, with scattered attempt_counts (forcing
// cross-batch splits under ORDER BY (attempt_count,id)) and filler rows, must
// apply in strict id order under ApplyConcurrency>1. The final state equals the
// LAST op (delete → absent); the ops of the object appear in exact id order.
func Test_1_4_41_PartitionOrder_MultiOp_StrictFIFO(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, _ := setupDrainerPG(t)
	st := newTupleState()
	st.delay = 10 * time.Millisecond // overlap distinct-object applies

	const obj = "vpc_subnet:fifo0000000000000001"

	// Alternating write/delete on obj, scattered attempt_counts. Insert order =
	// id order = the intended apply order (grant,revoke,grant,revoke,grant,revoke).
	type op struct {
		et      string
		attempt int
	}
	seq := []op{
		{"fga.tuple.write", 4},
		{"fga.tuple.delete", 0},
		{"fga.tuple.write", 7},
		{"fga.tuple.delete", 1},
		{"fga.tuple.write", 2},
		{"fga.tuple.delete", 0},
	}
	ids := make([]int64, 0, len(seq))
	for _, o := range seq {
		ids = append(ids, insertOutboxRowAttempt(t, ctx, pool, o.et,
			fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":%q}`, obj), o.attempt))
	}
	// Filler rows of other objects so the obj-ops span several claim batches.
	filler := make([]int64, 0, 8)
	for i := 0; i < 8; i++ {
		filler = append(filler, insertOutboxRowAttempt(t, ctx, pool, "fga.tuple.write",
			fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":"noise:obj%02d"}`, i), 0))
	}

	cfg := testCfg()
	cfg.ApplyConcurrency = 4
	cfg.PartitionColumn = "payload->>'object'"
	cfg.MaxAttempts = 10
	cfg.PollFallback = 300 * time.Millisecond

	dCancel, done := startDrainerApplier(t, ctx, pool, cfg, st.apply)
	defer func() { dCancel(); <-done }()

	waitForListenerReady(t, ctx, pool, cfg.Channel)

	for _, id := range append(append([]int64{}, ids...), filler...) {
		waitForRowSent(t, ctx, pool, id, 30*time.Second)
	}

	want := []string{
		"write:" + obj, "delete:" + obj,
		"write:" + obj, "delete:" + obj,
		"write:" + obj, "delete:" + obj,
	}
	assert.Equalf(t, want, st.opsFor(obj),
		"strict per-partition FIFO under ApplyConcurrency; full log=%v", st.snapshotLog())
	assert.Falsef(t, st.isPresent(obj),
		"final state = last op (delete) → absent; log=%v", st.snapshotLog())
}

// Test_1_4_42_PartitionColumn_DistinctObjects_StayConcurrent — the partition
// predicate must NOT serialise UNRELATED partitions. K rows of K DISTINCT objects
// (each its own partition, each a partition head) with a fixed apply latency L
// must drain concurrently (~ceil(K/N)*L), not serially (~K*L). Guards against the
// NOT EXISTS accidentally throttling cross-partition throughput (the whole point
// of raising ApplyConcurrency for iam).
func Test_1_4_42_PartitionColumn_DistinctObjects_StayConcurrent(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, _ := setupDrainerPG(t)
	fa := newFakeApplier()

	const (
		k = 24
		l = 200 * time.Millisecond
		n = 8
	)
	fa.setDelay("fga.tuple.write", l)

	cfg := testCfg()
	cfg.ApplyConcurrency = n
	cfg.PartitionColumn = "payload->>'object'"
	cfg.ApplyTimeout = 5 * time.Second
	cfg.PollFallback = 500 * time.Millisecond

	dCancel, done, _ := startDrainer(t, ctx, pool, cfg, fa)
	defer func() { dCancel(); <-done }()

	waitForListenerReady(t, ctx, pool, cfg.Channel)

	ids := make([]int64, k)
	for i := 0; i < k; i++ {
		// DISTINCT object per row → each is its own partition head.
		ids[i] = insertOutboxRow(t, ctx, pool, "fga.tuple.write",
			fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":"obj:%03d"}`, i))
	}

	// ceil(24/8)*200ms = 600ms concurrent vs 4.8s sequential. 3s sits between.
	const deadline = 3 * time.Second
	require.Truef(t, fa.waitForCalls(k, deadline),
		"distinct-partition rows must drain concurrently within %s (sequential ~%s); got %d in flight",
		deadline, time.Duration(k)*l, fa.countCalls())

	for _, id := range ids {
		waitForRowSent(t, ctx, pool, id, 3*time.Second)
	}
}

// Test_1_4_43_PoisonedPredecessor_DoesNotWedgeSuccessor — a POISONED predecessor
// (attempt_count = MaxAttempts, never claimable) of a partition must NOT block a
// deliverable successor of the same partition forever. The blocking predicate
// excludes poisoned rows (`p.attempt_count < MaxAttempts`) precisely so a
// permanently-dead head does not wedge its partition. (Without that exclusion the
// successor would never be claimed → sent_at stays NULL → this times out.)
func Test_1_4_43_PoisonedPredecessor_DoesNotWedgeSuccessor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, _ := setupDrainerPG(t)
	fa := newFakeApplier() // default success; poisoned row is never claimed/applied

	const obj = "vpc_network:wedge000000000000001"
	const maxAttempts = 10

	// P: poisoned predecessor of obj — attempt_count already at MaxAttempts, so the
	// claim's poison-gate never selects it. Smaller id than the successor.
	_ = insertOutboxRowAttempt(t, ctx, pool, "fga.tuple.write",
		fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":%q}`, obj), maxAttempts)
	// S: deliverable successor of the SAME object (partition), fresh. id > P.
	sID := insertOutboxRowAttempt(t, ctx, pool, "fga.tuple.write",
		fmt.Sprintf(`{"user":"user:u","relation":"v_delete","object":%q}`, obj), 0)

	cfg := testCfg()
	cfg.ApplyConcurrency = 4
	cfg.PartitionColumn = "payload->>'object'"
	cfg.MaxAttempts = maxAttempts
	cfg.PollFallback = 300 * time.Millisecond

	dCancel, done, _ := startDrainer(t, ctx, pool, cfg, fa)
	defer func() { dCancel(); <-done }()

	waitForListenerReady(t, ctx, pool, cfg.Channel)

	// S must be delivered despite the poisoned predecessor sharing its partition.
	row := waitForRowSent(t, ctx, pool, sID, 10*time.Second)
	assert.NotNil(t, row.sentAt,
		"deliverable successor must not be wedged by a poisoned same-partition predecessor")
}

// Test_1_4_44_WedgeObserver_ReportsStuckPartition — the opt-in wedge observer
// reports a partition whose head has been unsent longer than WedgeWarnAfter (a
// stuck head that blocks its successors), and does NOT report a fresh partition.
func Test_1_4_44_WedgeObserver_ReportsStuckPartition(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, _ := setupDrainerPG(t)
	fa := newFakeApplier() // fresh rows apply+clear; poisoned head is never claimed

	const stuck = "vpc_network:wedgeobs0000000001"
	const fresh = "vpc_network:wedgeobsfresh000001"
	const maxAttempts = 10

	// A poisoned (never-claimed) head, backdated 8s → an old unsent partition head.
	insertOutboxRowAged(t, ctx, pool, "fga.tuple.write",
		fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":%q}`, stuck), maxAttempts, 8*time.Second)
	// A fresh deliverable row of another object — applied quickly, never wedged.
	insertOutboxRow(t, ctx, pool, "fga.tuple.write",
		fmt.Sprintf(`{"user":"user:u","relation":"v_get","object":%q}`, fresh))

	var mu sync.Mutex
	seen := map[string]time.Duration{}
	obs := func(part string, age time.Duration) {
		mu.Lock()
		seen[part] = age
		mu.Unlock()
	}

	cfg := testCfg()
	cfg.PartitionColumn = "payload->>'object'"
	cfg.WedgeWarnAfter = 200 * time.Millisecond
	cfg.MaxAttempts = maxAttempts
	cfg.PollFallback = 100 * time.Millisecond

	dCancel, done := startDrainerApplier(t, ctx, pool, cfg, applierFromFake(fa),
		drainer.WithWedgeObserver[rawPayload](obs))
	defer func() { dCancel(); <-done }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		age, ok := seen[stuck]
		_, reportedFresh := seen[fresh]
		mu.Unlock()
		if ok {
			assert.GreaterOrEqualf(t, age, cfg.WedgeWarnAfter,
				"reported wedge age must exceed WedgeWarnAfter (head backdated 8s)")
			assert.Falsef(t, reportedFresh,
				"fresh (non-wedged) partition %q must NOT be reported as wedged", fresh)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("wedge observer never reported stuck partition %q; seen=%v", stuck, seen)
}
