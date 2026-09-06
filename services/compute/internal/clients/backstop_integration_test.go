// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// backstop_integration_test.go — kacho-compute backstop: reconciler + metrics +
// fail-closed boot-gate over the register-outbox, WITHOUT changing co-commit
// atomicity (no migration).
//
//   - reconciler re-drives a poisoned row back to claimable → delivered
//   - fail-closed boot-gate: require-iam + no drainer → Create refused
//   - long-outage no-poison: IAM down > MaxAttempts (transient) → not poisoned →
//     delivered exactly once on recovery + metrics surface backlog/poisoned while
//     pending
//
// testcontainers Postgres 16; real corelib reconciler/drainer/metrics + fake IAM.
// Reuses the harness in register_drainer_integration_test.go (setupDrainerDB,
// fakeIAMRegister, newDrainer, insertIntent, countSent). Skipped under -short.
package clients_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
)

const computeOutboxTbl = "public.compute_fga_register_outbox"

// Test_1_4_30_ReconcilerRedrivesPoisoned — 1.4-30: a poisoned register-intent
// (attempt_count >= MaxAttempts, sent_at NULL) is re-driven to claimable by the
// reconciler → the drainer then delivers it (sent_at NOT NULL) with its ORIGINAL
// decoder-correct tuple payload. Atomicity untouched (no resource-writer change).
func Test_1_4_30_ReconcilerRedrivesPoisoned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := setupDrainerDB(t)

	// A poisoned intent with a valid tuple payload (the cause is now fixed).
	insertIntent(ctx, t, pool, fgaintent.EventRegister, "Instance", "epd-redrive",
		fgaintent.Tuple{SubjectID: "project:proj-x", Relation: "project", Object: "compute_instance:epd-redrive"})
	_, err := pool.Exec(ctx,
		`UPDATE compute_fga_register_outbox SET attempt_count = 10, last_error = 'was permanent'
		   WHERE resource_id = 'epd-redrive'`)
	require.NoError(t, err)

	rc, err := reconciler.NewRedriveOnly(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           computeOutboxTbl,
		Channel:         "compute_fga_register_outbox",
		MaxAttempts:     10,
	}, nil)
	require.NoError(t, err)

	n, err := rc.RedrivePoisoned(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "exactly one poisoned row re-driven")

	var attempt int
	var lastErr *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT attempt_count, last_error FROM compute_fga_register_outbox WHERE resource_id='epd-redrive'`).
		Scan(&attempt, &lastErr))
	assert.Less(t, attempt, 10, "attempt_count reset below MaxAttempts (claimable)")
	assert.Nil(t, lastErr, "last_error cleared")

	// The drainer now delivers the re-driven intent (IAM healthy).
	fake := &fakeIAMRegister{}
	applier := clients.NewIAMRegisterApplierWithClient(fake)
	d := newDrainer(t, pool, applier)
	go func() { _ = d.Run(ctx) }()
	require.Eventually(t, func() bool {
		return fake.registeredCount() == 1
	}, 5*time.Second, 50*time.Millisecond, "re-driven intent delivered exactly once")
}

// Test_1_4_31_FailClosedBootGate_RefusesCreate — 1.4-31: require-iam armed +
// register-drainer not connected → a mutating Create of THIS service is refused
// (UNAVAILABLE); read RPCs pass; Internal-admin Creates are not gated; connect →
// Create allowed.
//
// Since kacho-compute moved onto the shared carrier (`pkg/servicehost`), the
// interceptor that composes these two halves is the carrier's — and the carrier
// pins its own behaviour on the executing code
// (`TestBootGateRefusesCreateWhileTheDeliveryPathIsDown`,
// `TestBootGateIsSilentOnEverythingButTenantCreate`,
// `TestChainWithoutTheGateAcceptsCreateWhileTheDeliveryPathIsDown`). What is left
// here is the part that is OURS and that no carrier test can make: that the
// predicate classifies THIS service's own method names the way we expect. It is
// asked of `servicehost.IsGatedMutation` — the very predicate the gate executes —
// and not of a local copy, because a copy would diverge silently and exactly
// where the divergence is invisible.
func Test_1_4_31_FailClosedBootGate_RefusesCreate(t *testing.T) {
	const (
		createMethod = "/kacho.cloud.compute.v1.InstanceService/Create"
		getMethod    = "/kacho.cloud.compute.v1.InstanceService/Get"
		adminMethod  = "/kacho.cloud.compute.v1.InternalMachineTypeService/Create"
	)
	require.True(t, servicehost.IsGatedMutation(createMethod),
		"tenant Create of this service must fall under the gate — otherwise a machine is created "+
			"while its owner-registration intent has nowhere to go")
	require.False(t, servicehost.IsGatedMutation(getMethod),
		"a read must NOT be gated: refusing reads while the delivery path is down would take the "+
			"service down for everyone over a write-path dependency")
	require.False(t, servicehost.IsGatedMutation(adminMethod),
		"an Internal-admin Create records no owner-tuple intent, so gating it would close the admin "+
			"path over a reason that does not apply to it")

	gate := bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-compute"})
	assert.False(t, gate.Ready(), "require-iam + not connected → NotReady")

	err := gate.GuardMutation()
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err), "Create refused fail-closed (UNAVAILABLE)")

	gate.SetConnected(true)
	assert.True(t, gate.Ready(), "connected → Ready")
	require.NoError(t, gate.GuardMutation(), "Create allowed once the IAM-register path connected")
}

// Test_1_4_31_RequireIAMOff_NoOp — contrast: require-iam=false (dev) → no-op gate.
func Test_1_4_31_RequireIAMOff_NoOp(t *testing.T) {
	gate := bootgate.New(bootgate.Config{RequireIAM: false, Service: "kacho-compute"})
	assert.True(t, gate.Ready(), "require-iam off → always Ready (dev)")
	require.NoError(t, gate.GuardMutation(), "Create allowed in dev back-compat mode")
}

// controllableIAM — a fake IAM register client whose outage is flipped by the
// test (down → Unavailable on every call). Used for the deterministic long-outage
// no-poison scenario.
type controllableIAM struct {
	down     atomic.Bool
	attempts atomic.Int32
	applied  atomic.Int32
}

func (c *controllableIAM) RegisterResource(_ context.Context, _ *iamv1.RegisterResourceRequest, _ ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error) {
	if c.down.Load() {
		c.attempts.Add(1)
		return nil, status.Error(codes.Unavailable, "iam down")
	}
	c.applied.Add(1)
	return &iamv1.RegisterResourceResponse{}, nil
}

func (c *controllableIAM) UnregisterResource(_ context.Context, _ *iamv1.UnregisterResourceRequest, _ ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error) {
	return &iamv1.UnregisterResourceResponse{}, nil
}

// Test_1_4_32_LongOutageNoPoison_ThenMetricsSurface — 1.4-32 + 1.4-23: IAM
// Unavailable for MORE than MaxAttempts consecutive transient attempts (D-5) → the
// intent is NOT poisoned (stays pending) → delivered exactly once on recovery; the
// metrics Collector surfaces backlog/oldest while pending, poisoned stays 0.
func Test_1_4_32_LongOutageNoPoison_ThenMetricsSurface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := setupDrainerDB(t)

	const maxAttempts = 5
	iam := &controllableIAM{}
	iam.down.Store(true)
	applier := clients.NewIAMRegisterApplierWithClient(iam)
	d := newDrainer(t, pool, applier) // MaxAttempts=5 in the harness
	go func() { _ = d.Run(ctx) }()

	insertIntent(ctx, t, pool, fgaintent.EventRegister, "Instance", "epd-long",
		fgaintent.Tuple{SubjectID: "project:proj-x", Relation: "project", Object: "compute_instance:epd-long"})

	rec := metrics.NewMemRecorder()
	col := metrics.NewCollector(pool, rec, metrics.CollectorConfig{Table: computeOutboxTbl, MaxAttempts: maxAttempts})

	// While IAM is down: > maxAttempts transient attempts yet the intent is NOT
	// poisoned (still pending) — and metrics surface backlog + oldest age (D-5/D-7).
	require.Eventually(t, func() bool {
		_ = col.Scan(ctx)
		return iam.attempts.Load() > maxAttempts &&
			rec.BacklogDepth(computeOutboxTbl) >= 1 && rec.OldestPendingAgeSeconds(computeOutboxTbl) > 0
	}, 10*time.Second, 100*time.Millisecond, "> maxAttempts transient attempts, still pending, backlog surfaced")

	var sentNull bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT sent_at IS NULL FROM compute_fga_register_outbox WHERE resource_id='epd-long'`).Scan(&sentNull))
	assert.True(t, sentNull, "intent durable (pending) through a transient outage longer than MaxAttempts")

	// IAM recovers → the same durable intent is delivered exactly once.
	//
	// BOTH facts are waited for in ONE condition: the applier ran exactly once AND
	// the row is marked sent. Waiting on the call alone does not order the two —
	// the drainer sets sent_at in a SEPARATE statement (markSuccess), committed
	// AFTER the applier returns, so a read placed right behind the wait lands in
	// the gap between them. That gap is narrow and on a quiet machine almost
	// always closed; under contention for the host (parallel testcontainers
	// suites) it opens, and the probe reddens on a healthy product — reporting the
	// load on the machine rather than the behaviour of the drain. Ask the witness
	// you waited on: what ends this wait is the OUTCOME (the row is marked), not a
	// deadline. Tree-wide the shape is held by
	// internal/repohygiene.TestDurableStateNeverAssertedAfterInProcessWait.
	iam.down.Store(false)
	require.Eventually(t, func() bool {
		if iam.applied.Load() != 1 {
			return false
		}
		var sent bool
		if err := pool.QueryRow(ctx,
			`SELECT sent_at IS NOT NULL FROM compute_fga_register_outbox WHERE resource_id='epd-long'`).
			Scan(&sent); err != nil {
			return false
		}
		return sent
	}, 10*time.Second, 100*time.Millisecond,
		"tuple delivered exactly once after a long transient outage, and the intent marked sent "+
			"(no poison, not lost)")

	require.NoError(t, col.Scan(ctx))
	assert.Equal(t, float64(0), rec.PoisonedCount(computeOutboxTbl),
		"a transient (Unavailable) outage must NOT poison — outbox_poisoned stays 0")
}
