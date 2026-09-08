// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer_test

// Integration lock for the OBSERVABLE outcome of a cross-project Move: once the
// register-outbox has drained, the authorization projection of the moved
// NetworkLoadBalancer EXISTS and is parented to the DESTINATION project, and no
// projection remains parented to the SOURCE project.
//
// Everything on the producing side is real: the real handler drives the real
// Move use-case, which writes its intents through the real writer-tx emitter
// into the real `kacho_nlb.fga_register_outbox`; the real corelib drainer claims
// them with the production PartitionColumn ("resource_id") and applies them with
// the real applier (iam.NewRegisterApplier). Only kaname itself is a double —
// Go forbids importing services/iam/internal, so the mirror it keeps is modelled
// here, faithfully to services/iam/.../resource_mirror/emitter.go:
//
//	register(v, parent) → INSERT when absent (UNCONDITIONAL);
//	                      else UPDATE only if stored < v (source_version-LWW)
//	unregister(tomb)    → DELETE only if stored <= tomb; absent row ⇒ plain no-op
//
// The asymmetry is the point: two intents about the SAME object that carry the
// SAME version are not distinguishable by that model, so whichever is applied
// last decides — and the drainer's per-resource FIFO makes "last" deterministic.
//
// Run: go test ./services/nlb/internal/apps/kacho/api/loadbalancer/... -run MoveProjection

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	iampb "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// ---- fake kaname modelling the resource_mirror --------------------------

// mirrorRow — the modelled `kaname.resource_mirror` row: the parent scope the
// γ selector reads plus the source_version the LWW guard compares against.
type mirrorRow struct {
	parentProjectID string
	version         time.Time
}

type mirrorIAM struct {
	mu   sync.Mutex
	rows map[string]mirrorRow // object → row (absent key ⇒ no row)
	log  []string             // ordered apply log, for failure diagnostics
}

func newMirrorIAM() *mirrorIAM { return &mirrorIAM{rows: map[string]mirrorRow{}} }

func (m *mirrorIAM) RegisterResource(
	_ context.Context, in *iampb.RegisterResourceRequest, _ ...grpc.CallOption,
) (*iampb.RegisterResourceResponse, error) {
	v := in.GetSourceVersion().AsTime()
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, present := m.rows[in.GetObject()]
	if !present || stored.version.Before(v) {
		// INSERT branch (absent) is unconditional; UPDATE branch is LWW-gated.
		m.rows[in.GetObject()] = mirrorRow{parentProjectID: in.GetParentProjectId(), version: v}
	}
	m.log = append(m.log, fmt.Sprintf("register:%s parent=%s @%s",
		in.GetObject(), in.GetParentProjectId(), v.UTC().Format(time.RFC3339Nano)))
	return &iampb.RegisterResourceResponse{}, nil
}

func (m *mirrorIAM) UnregisterResource(
	_ context.Context, in *iampb.UnregisterResourceRequest, _ ...grpc.CallOption,
) (*iampb.UnregisterResourceResponse, error) {
	v := in.GetSourceVersion().AsTime()
	m.mu.Lock()
	defer m.mu.Unlock()
	if stored, present := m.rows[in.GetObject()]; present && !stored.version.After(v) {
		delete(m.rows, in.GetObject()) // hard delete — no tombstone retained
	}
	m.log = append(m.log, fmt.Sprintf("unregister:%s @%s",
		in.GetObject(), v.UTC().Format(time.RFC3339Nano)))
	return &iampb.UnregisterResourceResponse{}, nil
}

func (m *mirrorIAM) row(object string) (mirrorRow, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[object]
	return r, ok
}

func (m *mirrorIAM) snapshotLog() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.log...)
}

var _ iamclient.RegisterResourceClient = (*mirrorIAM)(nil)

// ---- drainer harness (production shape) ------------------------------------

// startMirrorDrainer runs the register-drainer with the SAME PartitionColumn the
// composition root wires (cmd/kacho-loadbalancer/wiring.go), so per-resource FIFO
// holds here exactly as in production. Returns a stop func.
func startMirrorDrainer(t *testing.T, pool *pgxpool.Pool, cli iamclient.RegisterResourceClient) func() {
	t.Helper()
	d, err := drainer.New[domain.FGARegisterIntent](
		pool,
		drainer.Config{
			Table:           "kacho_nlb.fga_register_outbox",
			Channel:         "kacho_nlb_fga_register_outbox",
			BatchSize:       32,
			PollFallback:    200 * time.Millisecond,
			MaxAttempts:     4,
			BackoffMin:      50 * time.Millisecond,
			BackoffMax:      200 * time.Millisecond,
			PartitionColumn: "resource_id",
		},
		iamclient.DecodeFGARegisterIntent,
		iamclient.NewRegisterApplier(cli),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

// waitOutboxDrained blocks until every register-outbox row is marked sent.
func waitOutboxDrained(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	require.Eventually(t, func() bool {
		var pending int
		require.NoError(t, pool.QueryRow(context.Background(),
			`SELECT count(*) FROM kacho_nlb.fga_register_outbox WHERE sent_at IS NULL`).Scan(&pending))
		return pending == 0
	}, 30*time.Second, 100*time.Millisecond, "register-outbox never fully drained")
}

// ---- test ------------------------------------------------------------------

// TestIntegration_MoveProjection_LandsOnDestination — Move rewrites the project
// scope of a NetworkLoadBalancer by emitting, in ONE writer-tx, an unregister of
// the source scope and a register of the destination scope. Both rows share a
// partition (the same resource_id), so the drainer applies them in id order.
//
// The outcome the tenant feels is the projection: after the queue drains the
// moved balancer must still HAVE one, parented to the destination. Losing it
// costs the balancer the access its owner is entitled to — silently, because
// nothing about the Move operation itself fails.
//
// Two intents about one subject emitted in one transaction must therefore be
// ORDERED — distinguishable in version and emitted newest-state-last — or the
// second one undoes the first.
func TestIntegration_MoveProjection_LandsOnDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := makeHandler(t, repo, opsRepo)

	const srcProject = "prj-move-src"
	const dstProject = "prj-move-dst"

	// Create in the SOURCE project — the real create-path emits the register
	// intent that establishes the projection (parent = source).
	createOp, err := h.Create(ctx, internalAutoReq(srcProject, "edge-move"))
	require.NoError(t, err)
	created := pollOpDone(t, opsRepo, createOp.GetId())
	require.Nilf(t, created.Error, "create operation error: %v", created.Error)

	var lbID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM kacho_nlb.load_balancers WHERE project_id = $1`, srcProject).Scan(&lbID))
	object := domain.FGAObjectTypeLoadBalancer + ":" + lbID

	// Move to the DESTINATION project.
	moveOp, err := h.Move(ctxNamedCaller(), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  dstProject,
	})
	require.NoError(t, err)
	moved := pollOpDone(t, opsRepo, moveOp.GetId())
	require.Nilf(t, moved.Error, "move operation error: %v", moved.Error)

	// Drain create-register + the two Move intents through the real applier.
	iam := newMirrorIAM()
	stop := startMirrorDrainer(t, pool, iam)
	defer stop()
	waitOutboxDrained(t, pool)

	row, present := iam.row(object)
	require.Truef(t, present,
		"moved balancer has NO authorization projection — its owner loses the access the Move was supposed to carry over; apply log=%v",
		iam.snapshotLog())
	assert.Equalf(t, dstProject, row.parentProjectID,
		"projection must be parented to the destination project; apply log=%v", iam.snapshotLog())
	assert.NotEqual(t, srcProject, row.parentProjectID,
		"no projection may remain parented to the source project after Move")
}
