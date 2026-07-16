// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package access_binding_test

// create_owner_tuple_opgate_integration_test.go — owner-tuple op-gating (opgate P2)
// for iam AccessBinding.Create (testcontainers PG16 + a controllable in-process
// FGA read-port). Source acceptance (✅ APPROVED):
// docs/specs/sub-phase-owner-tuple-opgate-acceptance.md.
//
// Guarantee under test: AccessBinding.Create reaches Operation done=true,result=
// response ONLY after the owner-tuple (the per-object access on
// iam_access_binding:<id> that the gateway scope_extractor{iam_access_binding}
// resolves on an IMMEDIATE Update/Delete) is confirmed effective in FGA
// (read-after-register). Fail-closed: confirm not achieved within the
// confirmation-deadline → op.error(codes.Unavailable, "owner-tuple registration
// not confirmed"); a success-done is NEVER emitted without confirm; the created
// binding row + register-intent stay durable on every branch.
//
// Scenarios: OTG-03 (op done only after confirm; ordering + durability) and
// OTG-05b (confirm timeout → fail-closed op.error; resource-ref discoverable in
// op.metadata on the error terminal; resource durable — orphan-guard).
//
// The confirm probe is exercised through a FAKE clients.RelationStore whose Check
// the test flips DENY↔ALLOW — so no real OpenFGA is needed. The confirmation
// deadline is overridden per-test via an injected operations.Worker (WithWorker),
// keeping the shared package default-registry untouched.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	accessbindingapp "github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/api/access_binding"
	"github.com/PRO-Robotech/kacho/services/iam/internal/clients"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho/pg"
)

// gateConfirmStore — controllable fake clients.RelationStore for the owner-tuple
// op-gating tests. Check returns the atomically-settable `allow` ONLY for the
// confirm relation (v_update on iam_access_binding:<id>) — the exact per-object
// access the gateway scope_extractor{iam_access_binding} resolves on an immediate
// Update/Delete. Every OTHER Check (e.g. requireGrantAuthority's cluster/scope
// admin probe) returns false, so the Create still authorizes through the
// account-OWNER grant path (owner_user_id), never through this fake. Tuple writes
// are no-ops (op-gating confirm is read-only — OTG-07 read-only invariant).
type gateConfirmStore struct{ allow atomic.Bool }

func (g *gateConfirmStore) Check(_ context.Context, _, relation, object string) (bool, error) {
	if relation == "v_update" && strings.HasPrefix(object, "iam_access_binding:") {
		return g.allow.Load(), nil
	}
	return false, nil
}
func (*gateConfirmStore) WriteTuples(context.Context, []clients.RelationTuple) error  { return nil }
func (*gateConfirmStore) DeleteTuples(context.Context, []clients.RelationTuple) error { return nil }

var _ clients.RelationStore = (*gateConfirmStore)(nil)

// opgateWorker builds an isolated operations.Worker whose owner-tuple confirmation
// deadline is overridden for the test (acceptance §OTG-05: a small test value,
// e.g. 500ms, with normal propagation ≪ deadline). The terminal-write budget is
// shrunk so the fail-closed MarkError lands promptly. Injected via WithWorker so
// the shared package default-registry is untouched (per-test isolation + a
// deterministic deadline — no global mutation, no time.Sleep dependence).
func opgateWorker(t *testing.T, confirmDeadline time.Duration) *operations.Worker {
	t.Helper()
	w := operations.NewWorker(
		operations.WithConfirmationDeadline(confirmDeadline),
		operations.WithTerminalWriteConfig(operations.TerminalWriteConfig{
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     20 * time.Millisecond,
			MaxElapsed:      500 * time.Millisecond,
		}),
	)
	w.Start()
	t.Cleanup(w.Stop)
	return w
}

func bindingRowExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_bindings WHERE id = $1`, id).Scan(&n))
	return n == 1
}

// emittedTuplesCount reports how many FGA tuples were durably recorded for the
// binding (co-committed in the writer-tx, ban #10) — the iam analogue of the
// SEC-D register-intent durable record.
func emittedTuplesCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bindingID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_iam.access_binding_emitted_tuples WHERE binding_id = $1`,
		bindingID).Scan(&n))
	return n
}

// seedOpgateGrant seeds an account owner + member + an assignable account role and
// returns a valid owner-create AccessBinding request (owner grants the role to the
// member on the account it owns — the owner-create grant-authority path). suffix
// keeps ids/emails unique across the two tests sharing one binary.
func seedOpgateGrant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (callerCtx context.Context, binding domain.AccessBinding) {
	t.Helper()
	owner := mustSeedUser(t, ctx, pool, "otg"+suffix+"o")
	acc := seedAccountByOwner(t, ctx, pool, "acc-otg"+suffix, owner)
	member := mustSeedUser(t, ctx, pool, "otg"+suffix+"m")
	role := seedAccountCustomRole(t, ctx, pool, acc, "otg"+suffix+"role")
	return asUser(ctx, owner), domain.AccessBinding{
		SubjectType:  domain.SubjectTypeUser,
		SubjectID:    domain.SubjectID(member),
		RoleID:       role,
		ResourceType: "account",
		ResourceID:   string(acc),
		Scope:        domain.ScopeAccount,
	}
}

// OTG-03 — op.done(success) is reached ONLY after the owner-tuple confirm; while
// the confirm probe DENYs the Operation stays done=false (PENDING) even though the
// binding row + register-intent are already durable, and it flips to done=true,
// result=response only after the probe starts to ALLOW (ordering).
func TestCreate_OwnerTupleOpgate_OTG03_OpDoneOnlyAfterConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := poolFromDSN(t, setupTestDB(t))
	repo := kachopg.New(pool, nil)
	opsRepo := operations.NewRepo(pool, "kacho_iam")

	gate := &gateConfirmStore{}         // allow=false → confirm DENY (owner-tuple not yet effective)
	w := opgateWorker(t, 5*time.Second) // large deadline — the timeout must NOT fire during the DENY window
	create := accessbindingapp.NewCreateAccessBindingUseCase(repo, opsRepo).
		WithRelationStore(gate, nil).
		WithWorker(w)

	callerCtx, binding := seedOpgateGrant(t, ctx, pool, "03")
	op, err := create.Execute(callerCtx, binding)
	require.NoError(t, err)

	// resource-ref (binding id) is stamped in op.metadata at op-creation.
	md, err := operations.MetadataFor[*iamv1.CreateAccessBindingMetadata](op)
	require.NoError(t, err)
	bindingID := md.GetAccessBindingId()
	require.NotEmpty(t, bindingID)

	// The writer-tx (binding INSERT + register-intent emit) commits inside the
	// worker's doCreate BEFORE the confirm-loop → the row + intent are durable
	// while the Operation is still PENDING (not yet done).
	require.Eventually(t, func() bool { return bindingRowExists(t, ctx, pool, bindingID) },
		5*time.Second, 20*time.Millisecond, "binding row durable before confirm (writer-tx committed)")
	require.NotZero(t, emittedTuplesCount(t, ctx, pool, bindingID),
		"register-intent (emitted tuples) durable in the writer-tx")

	// Hold DENY for a window: the Operation must never flip to done while the
	// owner-tuple confirm is not achieved (no premature success-done).
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		cur, gerr := opsRepo.Get(ctx, op.ID)
		require.NoError(t, gerr)
		require.False(t, cur.Done,
			"op must stay done=false while owner-tuple confirm DENY (PENDING, not success-done)")
		time.Sleep(20 * time.Millisecond)
	}

	// Register the owner-tuple → confirm flips to ALLOW → op reaches done=true,
	// result=response (ordering: done follows the first ALLOW, never precedes it).
	gate.allow.Store(true)
	done := awaitOp(t, ctx, opsRepo, op.ID)
	require.Nil(t, done.Error, "op.done(success) after confirm ALLOW carries no error")
	require.NotNil(t, done.Response, "success terminal carries the binding response")
}

// OTG-05b (CRITICAL orphan-guard) — confirm never achieved within the (500ms)
// deadline → the Operation fails closed: done=true, error(Unavailable, exact
// text), code != DeadlineExceeded, no success-done. The resource-ref is
// discoverable in op.metadata ON the error terminal (FIX-3) and the binding row
// stayed durable (not rolled back by the timeout).
func TestCreate_OwnerTupleOpgate_OTG05b_ConfirmTimeout_FailClosed_ResourceRefDurable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := poolFromDSN(t, setupTestDB(t))
	repo := kachopg.New(pool, nil)
	opsRepo := operations.NewRepo(pool, "kacho_iam")

	gate := &gateConfirmStore{}                // stays DENY forever → models an FGA/IAM outage > deadline
	w := opgateWorker(t, 500*time.Millisecond) // small confirmation deadline (acceptance §OTG-05 override)
	create := accessbindingapp.NewCreateAccessBindingUseCase(repo, opsRepo).
		WithRelationStore(gate, nil).
		WithWorker(w)

	callerCtx, binding := seedOpgateGrant(t, ctx, pool, "05b")
	op, err := create.Execute(callerCtx, binding)
	require.NoError(t, err)

	md0, err := operations.MetadataFor[*iamv1.CreateAccessBindingMetadata](op)
	require.NoError(t, err)
	bindingID := md0.GetAccessBindingId()
	require.NotEmpty(t, bindingID)

	// Confirm never ALLOWs → after the deadline the op terminates fail-closed.
	done := awaitOp(t, ctx, opsRepo, op.ID)

	// (1) fail-closed terminal: op.error(Unavailable, exact text), NOT success.
	require.NotNil(t, done.Error, "confirm-timeout must fail closed (op.error), never success-done")
	assert.Equal(t, int32(codes.Unavailable), done.Error.Code,
		"FIX-1: confirm-timeout code is retryable Unavailable (fail-closed for mutations)")
	assert.NotEqual(t, int32(codes.DeadlineExceeded), done.Error.Code,
		"FIX-1: code is NOT DeadlineExceeded (explicit alternative rejected)")
	assert.Equal(t, "owner-tuple registration not confirmed", done.Error.Message,
		"FIX-1: stable contract text on the confirm-timeout terminal")
	assert.Nil(t, done.Response, "no success response on the fail-closed terminal (no success-done)")

	// (2) FIX-3 orphan-guard: the resource-ref is discoverable in op.metadata on
	// the ERROR terminal (not only on success) → the client can Get(ref) / retry
	// the mutation rather than re-create (which would orphan a duplicate).
	mdErr, err := operations.MetadataFor[*iamv1.CreateAccessBindingMetadata](done)
	require.NoError(t, err, "Create metadata preserved on the error terminal")
	assert.Equal(t, bindingID, mdErr.GetAccessBindingId(),
		"CreateAccessBindingMetadata.access_binding_id present on the error terminal (FIX-3)")

	// (3) resource durable — the binding row survived the fail-closed timeout
	// (the writer-tx committed inside doCreate; the timeout does not roll it back).
	assert.True(t, bindingRowExists(t, ctx, pool, bindingID),
		"binding row durable despite confirm-timeout (writer-tx committed, not rolled back)")
}
