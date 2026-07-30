// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
)

// internal_watch_authorization_test.go — the OBSERVABLE contract of the outbox
// stream: who receives which rows.
//
// The stream carries a full snapshot of every changed resource — project id, name,
// labels, metadata, host name, boot source and user-data (a routine carrier of
// secrets). A subscriber therefore reads, in one call, the change journal of every
// tenant in the deployment. What narrows it must be asserted on what the caller
// actually RECEIVES: a test that only checks "an interceptor ran" or "an entry
// exists in the map" stays green while every row still goes out.
//
// Two things are asserted here, and neither can be satisfied by a method-level
// gate:
//
//  1. a caller the request does not identify receives NOT ONE row;
//  2. a caller identified and entitled to one project receives ONLY that
//     project's rows.
//
// (2) is why a single per-RPC Check cannot gate this RPC at all: the rows belong
// to individually-owned objects and the caller names none of them, so there is no
// one object to ask a single question about (project-rule: authorise at the DATA
// level, in batches of ≤100).

// watchTestSubject — an identified caller, in the shape the model is asked about.
// Tests whose subject is cursor mechanics rather than authorisation pass this so
// that a refusal cannot be mistaken for the behaviour they assert.
const watchTestSubject = "user:usr_alice"

// stubVisibility — an in-memory stand-in for the per-object visibility question.
// allow lists the object ids the subject may see; err, when set, is what the
// model answers (an unreachable model must never read as "yes").
type stubVisibility struct {
	allow   map[string]bool
	err     error
	narrows bool
	asked   []string
}

func (s *stubVisibility) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	s.asked = append(s.asked, ids...)
	if s.err != nil {
		return nil, s.err
	}
	if len(ids) > watchVisibilityBatchSize {
		return nil, status.Errorf(codes.InvalidArgument,
			"visibility question exceeded the batch contract: %d ids in one partition", len(ids))
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if s.allow[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *stubVisibility) Narrows() bool { return s.narrows }

// allowAll — visibility that really narrows but happens to allow everything it is
// asked about. Used by the pre-existing streaming tests, whose subject is cursor
// mechanics rather than authorisation.
func allowAllVisibility(ids ...string) *stubVisibility {
	allow := make(map[string]bool, len(ids))
	for _, id := range ids {
		allow[id] = true
	}
	return &stubVisibility{allow: allow, narrows: true}
}

// ctxWithUser — a request that names a caller, the way the trust-aware
// principal-extract pair leaves it for the handler.
func ctxWithUser(parent context.Context, id string) context.Context {
	return operations.WithPrincipal(parent, operations.Principal{Type: "user", ID: id, DisplayName: id})
}

// TestIntegration_Watch_UnidentifiedCallerReceivesNoEvent — a request that names
// nobody must receive NOT ONE row, and must be refused as a permission problem.
//
// This is the observable form of the rule "an empty subject is cut
// unconditionally". Asserting only the status code would not be enough: the point
// of the RPC is the rows, so the row count is the assertion that matters.
func TestIntegration_Watch_UnidentifiedCallerReceivesNoEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	base := context.Background()
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(base, dsn)
	require.NoError(t, err)
	defer pool.Close()

	for _, id := range []string{"epi-a01", "epi-a02", "epi-b01"} {
		_, err := pool.Exec(base,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
			 VALUES ('Instance',$1,'CREATED','{"projectId":"prj-x","userData":"AWS_SECRET=nope"}'::jsonb)`, id)
		require.NoError(t, err)
	}

	// The stream blocks on notifications once caught up; the deadline is what ends
	// the call in the leaking case. It must be long enough that the catch-up pass
	// certainly completed, otherwise "no rows" could mean "no time".
	ctx, cancel := context.WithTimeout(base, 3*time.Second)
	defer cancel()

	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, allowAllVisibility("epi-a01", "epi-a02", "epi-b01"))
	fs := &fakeWatchStream{ctx: ctx} // ctx names nobody

	err = h.Watch(&computev1.WatchRequest{}, fs)

	assert.Empty(t, fs.sent,
		"a request that names no caller must receive NOT ONE outbox row; each row is a full "+
			"resource snapshot (project, labels, metadata, boot source, user-data)")
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"an unidentified caller is a permission failure, not a backend failure")
}

// TestIntegration_Watch_EntitledToOneProjectSeesOnlyItsOwnRows — the caller is
// identified and entitled to one project's instances; every other tenant's rows
// must be absent from what it receives.
//
// The narrowing is asked per row in partitions of at most watchVisibilityBatchSize
// — the stub fails the call if a bigger partition ever arrives, so the batch
// contract is asserted rather than assumed.
func TestIntegration_Watch_EntitledToOneProjectSeesOnlyItsOwnRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	base := context.Background()
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(base, dsn)
	require.NoError(t, err)
	defer pool.Close()

	mine := []string{"epi-mine01", "epi-mine02"}
	theirs := []string{"epi-other1", "epi-other2", "epi-other3"}
	for _, id := range append(append([]string{}, mine...), theirs...) {
		_, err := pool.Exec(base,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
			 VALUES ('Instance',$1,'CREATED','{}'::jsonb)`, id)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(ctxWithUser(base, "usr_alice"), 3*time.Second)
	defer cancel()

	vis := &stubVisibility{narrows: true, allow: map[string]bool{mine[0]: true, mine[1]: true}}
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	_ = h.Watch(&computev1.WatchRequest{}, fs)

	got := make([]string, 0, len(fs.sent))
	for _, ev := range fs.sent {
		got = append(got, ev.GetResourceId())
	}
	assert.ElementsMatch(t, mine, got,
		"the stream must carry only rows the caller is entitled to; rows of other tenants must be absent")
	assert.NotEmpty(t, vis.asked, "the model must actually be asked about the rows, not bypassed")
}

// TestIntegration_Watch_ModelErrorDeliversNothing — an unreachable or erroring
// permission model is not a "yes". The rows already read from the journal must not
// be sent when the question about them could not be answered.
func TestIntegration_Watch_ModelErrorDeliversNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	base := context.Background()
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(base, dsn)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(base,
		`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
		 VALUES ('Instance','epi-x01','CREATED','{}'::jsonb)`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(ctxWithUser(base, "usr_alice"), 3*time.Second)
	defer cancel()

	vis := &stubVisibility{narrows: true, err: status.Error(codes.Unavailable, "authorize peer down")}
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	err = h.Watch(&computev1.WatchRequest{}, fs)

	assert.Empty(t, fs.sent, "rows must not be delivered when the permission model could not answer")
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// TestIntegration_Watch_KindWithNoObjectTypeIsNotDelivered — a row whose kind has
// no live object type in the permission model cannot be authorised, so it must not
// be delivered. Block storage left compute (migration 0021), so `Disk` rows still
// sitting in the journal are exactly this case.
//
// Dropping them is the fail-closed choice: "we cannot ask whether you may see
// this" must never resolve to "you may".
func TestIntegration_Watch_KindWithNoObjectTypeIsNotDelivered(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	base := context.Background()
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(base, dsn)
	require.NoError(t, err)
	defer pool.Close()

	for _, row := range []struct{ kind, id string }{
		{"Instance", "epi-live1"},
		{"Disk", "epd-retired"},
		{"Image", "epm-retired"},
	} {
		_, err := pool.Exec(base,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
			 VALUES ($1,$2,'CREATED','{}'::jsonb)`, row.kind, row.id)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(ctxWithUser(base, "usr_alice"), 3*time.Second)
	defer cancel()

	// Deliberately permissive about every id — the drop must come from the kind
	// having no object type, not from the verdict.
	vis := &stubVisibility{narrows: true, allow: map[string]bool{
		"epi-live1": true, "epd-retired": true, "epm-retired": true,
	}}
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	_ = h.Watch(&computev1.WatchRequest{}, fs)

	got := make([]string, 0, len(fs.sent))
	for _, ev := range fs.sent {
		got = append(got, ev.GetResourceId())
	}
	assert.Equal(t, []string{"epi-live1"}, got,
		"only rows whose kind maps to a live object type may be delivered")
}

// TestIntegration_WatchStreamSince_CursorAdvancesPastDroppedRows — the cursor must
// advance over rows the caller may NOT see, not only over rows it received.
//
// This is the trap the narrowing introduces. The read loop continues while a batch
// came back full, so if the cursor only followed delivered rows, a full batch of
// rows the caller cannot see would be re-read forever: same query, same hundred
// rows, no progress, one core spinning inside a held stream slot. The cursor
// therefore follows the last SCANNED row — the same rule the paged list handlers
// use for `next_page_token`, and for the same reason: a full traversal must skip
// nothing while a page may legitimately come back partial.
//
// A whole batch is filled with invisible rows plus one visible row after it, so a
// stalled cursor cannot reach the visible row: delivering it proves progress past
// the invisible ones. The test is bounded by its own deadline, so a regression
// shows up as a failure rather than a hang.
func TestIntegration_WatchStreamSince_CursorAdvancesPastDroppedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	base := context.Background()
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(base, dsn)
	require.NoError(t, err)
	defer pool.Close()

	// A full batch of rows the caller may not see, then one it may.
	for i := 0; i < catchupBatchSize; i++ {
		_, err := pool.Exec(base,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
			 VALUES ('Instance',$1,'CREATED','{}'::jsonb)`, "epi-hidden"+padID(i))
		require.NoError(t, err)
	}
	_, err = pool.Exec(base,
		`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
		 VALUES ('Instance','epi-visible','CREATED','{}'::jsonb)`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(base, 20*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(context.Background()) }()

	vis := &stubVisibility{narrows: true, allow: map[string]bool{"epi-visible": true}}
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	newCursor, err := h.streamSince(ctx, conn, 0, nil, watchTestSubject, fs)
	require.NoError(t, err, "a batch of invisible rows must not stall the read loop")

	got := make([]string, 0, len(fs.sent))
	for _, ev := range fs.sent {
		got = append(got, ev.GetResourceId())
	}
	assert.Equal(t, []string{"epi-visible"}, got,
		"the row after a full invisible batch must be reached — proof the cursor moved past them")
	assert.EqualValues(t, catchupBatchSize+1, newCursor,
		"the cursor must equal the last SCANNED sequence_no, not the last delivered one")
}

// TestWatch_RefusedWhenNothingNarrowsTheStream — the stream must not open when the
// per-row filter is absent or does not actually narrow.
//
// This is the sibling trap of the original defect: a filter that is wired but
// switched off (or configured to pass the page through on error) returns every id
// unchanged, so the RPC would look filtered and leak everything. A `nil` filter is
// the same case. The production boot guard refuses to start in that configuration;
// this refusal is what holds on any other stand.
func TestWatch_RefusedWhenNothingNarrowsTheStream(t *testing.T) {
	cases := []struct {
		name string
		vis  EventVisibility
	}{
		{"no filter wired at all", nil},
		{"filter wired but does not narrow", &stubVisibility{narrows: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewInternalWatchHandler(nil, "", slog.Default(), 0, tc.vis)
			fs := &fakeWatchStream{ctx: ctxWithUser(context.Background(), "usr_alice")}

			err := h.Watch(&computev1.WatchRequest{}, fs)

			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.Empty(t, fs.sent)
		})
	}
}

// TestWatch_UnidentifiedCallerIsRefusedBeforeTouchingTheBackend — the refusal must
// happen before any backend contact.
//
// The dsn here cannot connect. If the identity gate sat AFTER the connection (as it
// did), the caller would be told `Unavailable` — a retryable answer to a request
// that will never become valid — and the refusal would depend on the backend being
// reachable. So the code is the observable, and it distinguishes the two orders:
// PermissionDenied can only come from a gate that ran first.
//
// This replaces an earlier assertion that a refused caller leaves the concurrency
// slot free. That assertion could not fail: the slot is released by a deferred
// send-back on every exit path, so it read as green whether or not the gate ran at
// all. A test that cannot fail is worse than no test — it occupies the slot and
// reports success.
func TestWatch_UnidentifiedCallerIsRefusedBeforeTouchingTheBackend(t *testing.T) {
	h := NewInternalWatchHandler(nil, "postgres://nobody@127.0.0.1:1/none", slog.Default(), 1, allowAllVisibility())

	err := h.Watch(&computev1.WatchRequest{}, &fakeWatchStream{ctx: context.Background()})

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"an unidentified caller must be refused before the backend is dialled; Unavailable here "+
			"would mean the identity gate runs after the connection")
}
