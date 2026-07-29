// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler_test

// Reviving a poisoned intent must respect the order of its partition.
//
// A register-outbox carries BOTH the registration and the deregistration of one
// resource, and the target these intents drive — kacho-iam's resource_mirror — is
// only PARTIALLY versioned: the last-writer-wins guard covers the update branch,
// while deregistration is a hard delete that leaves no tombstone. So an intent to
// register, replayed AFTER the matching deregistration has already been delivered,
// finds nothing to compare against, takes the insert branch and RESURRECTS the
// mirror row of a resource that no longer exists. iam's reconciler is
// level-triggered off that mirror, so it then re-materialises the owner tuple
// forever: access that was revoked comes back and stays back.
//
// The claim path already prevents this while an intent is deliverable (see
// drainer Config.PartitionColumn). A revival that ignores the partition puts the
// intent back into the deliverable set with no regard for what has already been
// delivered past it — which is how the same over-grant returns through the
// backstop that exists to repair the queue.
//
// The observable asserted here is the target state, not the row state: the mirror
// of a deleted resource must stay absent.
//
// Run: go test ./pkg/outbox/... -race -p 1

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
)

// registerOutboxSchema mirrors a production register-outbox (compute migrations
// 0010 + 0018 + the claim-order index): the same columns, the same NOTIFY trigger,
// and the two partial indexes the partition-head claim needs.
const registerOutboxSchema = `
CREATE SCHEMA IF NOT EXISTS kacho_svc;

CREATE TABLE kacho_svc.fga_register_outbox (
    id            bigserial    PRIMARY KEY,
    event_type    text         NOT NULL,
    resource_kind text         NOT NULL DEFAULT '',
    resource_id   text         NOT NULL DEFAULT '',
    payload       jsonb        NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    sent_at       timestamptz,
    last_error    text,
    attempt_count integer      NOT NULL DEFAULT 0,
    CONSTRAINT fga_register_outbox_event_type_check
        CHECK (event_type IN ('fga.register', 'fga.unregister'))
);

CREATE INDEX fga_register_outbox_partition_head_idx
    ON kacho_svc.fga_register_outbox (resource_id, id) WHERE sent_at IS NULL;
CREATE INDEX fga_register_outbox_claim_order_idx
    ON kacho_svc.fga_register_outbox (attempt_count, id) WHERE sent_at IS NULL;

CREATE OR REPLACE FUNCTION kacho_svc.fga_register_outbox_notify() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
    PERFORM pg_notify('kacho_svc_fga_register_outbox', NEW.id::text);
    RETURN NEW;
END;
$fn$;

CREATE TRIGGER fga_register_outbox_notify_trg
    AFTER INSERT ON kacho_svc.fga_register_outbox
    FOR EACH ROW EXECUTE FUNCTION kacho_svc.fga_register_outbox_notify();
`

const (
	redriveTable   = "kacho_svc.fga_register_outbox"
	redriveChannel = "kacho_svc_fga_register_outbox"
	redriveMaxAtt  = 10
)

func setupRegisterOutboxPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() || os.Getenv("SKIP_INTEGRATION") == "1" {
		t.Skip("integration tests skipped (SKIP_INTEGRATION=1)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("kacho_svc_test"),
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
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, registerOutboxSchema)
	require.NoError(t, err)
	return pool
}

// mirror models kacho-iam's resource_mirror closely enough for the asymmetry that
// matters: registration inserts when the row is absent and is version-gated only
// when it is present; deregistration is a hard delete that keeps no tombstone.
type mirror struct {
	mu      sync.Mutex
	present map[string]struct{}
	applied []string
}

func newMirror() *mirror { return &mirror{present: map[string]struct{}{}} }

// intentPayload is what the drainer hands the applier: the drainer decodes the
// payload column only, so the resource the intent is about rides in there.
type intentPayload struct {
	ResourceID string `json:"resource_id"`
}

func (m *mirror) apply(_ context.Context, eventType string, raw json.RawMessage) error {
	var p intentPayload
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return m.applyResource(eventType, p.ResourceID)
}

// applyResource records one applied intent against the modelled mirror.
func (m *mirror) applyResource(eventType, resourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch eventType {
	case "fga.register":
		m.present[resourceID] = struct{}{}
	case "fga.unregister":
		delete(m.present, resourceID) // hard delete — no tombstone survives
	}
	m.applied = append(m.applied, eventType+":"+resourceID)
	return nil
}

func (m *mirror) isPresent(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.present[id]
	return ok
}

func (m *mirror) log() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.applied...)
}

// seedIntent inserts one intent row in a chosen delivery state. The resource id is
// carried in the payload too so the applier can key on it.
func seedIntent(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	eventType, resourceID string, attempt int, delivered bool,
) int64 {
	t.Helper()
	var id int64
	sentAt := "NULL"
	if delivered {
		sentAt = "now()"
	}
	err := pool.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO kacho_svc.fga_register_outbox
		    (event_type, resource_kind, resource_id, payload, attempt_count, sent_at)
		VALUES ($1, 'apps_application', $2, jsonb_build_object('resource_id', $2::text), $3, %s)
		RETURNING id`, sentAt),
		eventType, resourceID, attempt).Scan(&id)
	require.NoError(t, err)
	return id
}

func attemptCountOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rowID int64) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT attempt_count FROM kacho_svc.fga_register_outbox WHERE id=$1`, rowID).Scan(&n))
	return n
}

func newRedriveReconciler(t *testing.T, pool *pgxpool.Pool) *reconciler.Reconciler {
	t.Helper()
	r, err := reconciler.NewRedriveOnly(pool, reconciler.Config{
		Table:       redriveTable,
		Channel:     redriveChannel,
		MaxAttempts: redriveMaxAtt,
	}, nil)
	require.NoError(t, err)
	return r
}

// runDrainerUntilQuiet starts a real drainer over the table (production wiring:
// PartitionColumn = resource_id) and stops it once nothing is left deliverable.
func runDrainerUntilQuiet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, m *mirror) {
	t.Helper()
	cfg := drainer.Config{
		Table:           redriveTable,
		Channel:         redriveChannel,
		BatchSize:       16,
		PollFallback:    200 * time.Millisecond,
		MaxAttempts:     redriveMaxAtt,
		BackoffMin:      10 * time.Millisecond,
		BackoffMax:      50 * time.Millisecond,
		ApplyTimeout:    5 * time.Second,
		PartitionColumn: "resource_id",
	}
	// The applier needs the resource the intent is about; the payload carries it,
	// so the decoder simply passes the payload through.
	d, err := drainer.New[json.RawMessage](pool, cfg,
		func(payload []byte) (json.RawMessage, error) { return json.RawMessage(payload), nil },
		m.apply, nil)
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(runCtx) }()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var pending int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT count(*) FROM kacho_svc.fga_register_outbox
			 WHERE sent_at IS NULL AND attempt_count < $1`, redriveMaxAtt).Scan(&pending))
		if pending == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	<-done
}

// Test_Redrive_DoesNotRevivePastADeliveredSuccessor — the over-grant scenario.
//
// Order of events for one resource: it was created (register), the registration
// was refused until it poisoned, then the resource was deleted (unregister) and
// that WAS delivered — so the target correctly holds nothing for it. Reviving the
// registration now replays a create past a delivered delete, and the mirror of a
// deleted resource comes back.
func Test_Redrive_DoesNotRevivePastADeliveredSuccessor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := setupRegisterOutboxPG(t)
	const gone = "app-deleted"

	// register(gone) poisoned; unregister(gone) already delivered.
	regID := seedIntent(t, ctx, pool, "fga.register", gone, redriveMaxAtt, false)
	seedIntent(t, ctx, pool, "fga.unregister", gone, 0, true)

	m := newMirror()
	// The target reflects the delivered deregistration: nothing for this resource.
	require.False(t, m.isPresent(gone), "precondition: the deleted resource has no mirror row")

	r := newRedriveReconciler(t, pool)
	_, err := r.RedrivePoisoned(ctx)
	require.NoError(t, err)

	runDrainerUntilQuiet(t, ctx, pool, m)

	require.Falsef(t, m.isPresent(gone),
		"the mirror row of a DELETED resource was resurrected by the revival: applied=%v", m.log())
	require.GreaterOrEqualf(t, attemptCountOf(t, ctx, pool, regID), redriveMaxAtt,
		"an intent superseded by a delivered deregistration must not be made deliverable again")
}

// Test_Redrive_RevivesWhenNothingHasOvertakenIt — the other direction. The
// backstop must keep doing its job: an intent with no delivered successor in its
// partition is revived and reaches the target. Without this the fix above would be
// indistinguishable from disabling the revival altogether.
func Test_Redrive_RevivesWhenNothingHasOvertakenIt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := setupRegisterOutboxPG(t)

	const (
		lonely    = "app-lonely"  // poisoned register, nothing after it
		pendingOK = "app-pending" // poisoned register, a PENDING successor after it
		earlier   = "app-earlier" // delivered register, then poisoned unregister
	)

	lonelyID := seedIntent(t, ctx, pool, "fga.register", lonely, redriveMaxAtt, false)

	pendingID := seedIntent(t, ctx, pool, "fga.register", pendingOK, redriveMaxAtt, false)
	seedIntent(t, ctx, pool, "fga.unregister", pendingOK, 0, false) // not delivered yet

	seedIntent(t, ctx, pool, "fga.register", earlier, 0, true) // delivered predecessor
	earlierID := seedIntent(t, ctx, pool, "fga.unregister", earlier, redriveMaxAtt, false)

	m := newMirror()
	_ = m.applyResource("fga.register", earlier) // target state after the delivered row

	r := newRedriveReconciler(t, pool)
	n, err := r.RedrivePoisoned(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, n, "all three poisoned intents are still eligible for revival")

	for _, id := range []int64{lonelyID, pendingID, earlierID} {
		require.Lessf(t, attemptCountOf(t, ctx, pool, id), redriveMaxAtt,
			"row %d must be claimable again after revival", id)
	}

	runDrainerUntilQuiet(t, ctx, pool, m)

	require.Truef(t, m.isPresent(lonely), "revived registration must reach the target: applied=%v", m.log())
	require.Falsef(t, m.isPresent(pendingOK),
		"partition order must hold after revival — register then unregister: applied=%v", m.log())
	require.Falsef(t, m.isPresent(earlier),
		"revived deregistration must reach the target: applied=%v", m.log())
}
