// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup_test

// Sibling of the NetworkLoadBalancer lock in
// apps/kacho/api/loadbalancer/move_mirror_projection_integration_test.go — the
// same class, the other resource that supports a cross-project Move.
//
// Real handler → real Move use-case → real writer-tx emitter → real
// `kacho_nlb.fga_register_outbox` → real corelib drainer with the production
// PartitionColumn → real applier. kaname is modelled (Go forbids importing
// services/iam/internal), faithfully to services/iam/.../resource_mirror:
//
//	register(v, parent) → INSERT when absent (UNCONDITIONAL);
//	                      else UPDATE only if stored < v (source_version-LWW)
//	unregister(tomb)    → DELETE only if stored <= tomb; absent row ⇒ plain no-op
//
// Run: go test ./services/nlb/internal/apps/kacho/api/targetgroup/... -run MoveProjection

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
	"google.golang.org/protobuf/types/known/durationpb"

	iampb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// ---- fake kaname modelling the resource_mirror --------------------------

type mirrorRow struct {
	parentProjectID string
	version         time.Time
}

type mirrorIAM struct {
	mu   sync.Mutex
	rows map[string]mirrorRow
	log  []string
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
		delete(m.rows, in.GetObject())
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

// TestIntegration_MoveProjection_TargetGroupLandsOnDestination — after a
// cross-project Move the target group must still HAVE an authorization
// projection, parented to the destination. Two intents about one subject emitted
// in one transaction must be ORDERED, or the second one undoes the first.
func TestIntegration_MoveProjection_TargetGroupLandsOnDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, repo := setupDB(t)
	opsRepo := newOpsRepo(t, pool)
	h := mkHandler(t, repo, opsRepo)

	const srcProject = "prj-tgmove-src"
	const dstProject = "prj-tgmove-dst"

	createOp, err := h.Create(ctx, &lbv1.CreateTargetGroupRequest{
		ProjectId: srcProject,
		RegionId:  "ru-central1",
		Name:      "tg-move",
		Port:      8080,
		HealthCheck: &lbv1.HealthCheck{
			Interval:           durationpb.New(2 * time.Second),
			Timeout:            durationpb.New(1 * time.Second),
			UnhealthyThreshold: 2,
			HealthyThreshold:   2,
			Options: &lbv1.HealthCheck_Tcp{
				Tcp: &lbv1.HealthCheck_TcpOptions{Port: 8080},
			},
		},
		DeregistrationDelay: durationpb.New(300 * time.Second),
	})
	require.NoError(t, err)
	created := pollOpDone(t, opsRepo, createOp.GetId())
	require.Nilf(t, created.Error, "create operation error: %v", created.Error)

	var tgID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM kacho_nlb.target_groups WHERE project_id = $1`, srcProject).Scan(&tgID))
	object := domain.FGAObjectTypeTargetGroup + ":" + tgID

	moveOp, err := h.Move(ctxNamedCaller(), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        tgID,
		DestinationProjectId: dstProject,
	})
	require.NoError(t, err)
	moved := pollOpDone(t, opsRepo, moveOp.GetId())
	require.Nilf(t, moved.Error, "move operation error: %v", moved.Error)

	iam := newMirrorIAM()
	stop := startMirrorDrainer(t, pool, iam)
	defer stop()
	waitOutboxDrained(t, pool)

	row, present := iam.row(object)
	require.Truef(t, present,
		"moved target group has NO authorization projection — its owner loses the access the Move was supposed to carry over; apply log=%v",
		iam.snapshotLog())
	assert.Equalf(t, dstProject, row.parentProjectID,
		"projection must be parented to the destination project; apply log=%v", iam.snapshotLog())
	assert.NotEqual(t, srcProject, row.parentProjectID,
		"no projection may remain parented to the source project after Move")
}
