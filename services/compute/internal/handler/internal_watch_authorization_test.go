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
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
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

// stubVisibility — подставная ПРИЁМНАЯ СТОРОНА вопроса о видимости.
//
// Дублируется сосед, а не сужатель: тот теперь один на дерево, и подменять его
// целиком значило бы проверять чужую реализацию вместо боевой. allow перечисляет
// объекты, которые субъекту видны; err — то, чем отвечает модель (недоступная модель
// никогда не читается как «да»).
type stubVisibility struct {
	allow map[string]bool
	err   error
	asked []string
}

func (s *stubVisibility) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest,
	_ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	for _, c := range in.GetChecks() {
		s.asked = append(s.asked, c.GetResource().GetId())
	}
	if s.err != nil {
		return nil, s.err
	}
	if len(in.GetChecks()) > watchVisibilityBatchSize {
		return nil, status.Errorf(codes.InvalidArgument,
			"visibility question exceeded the batch contract: %d ids in one partition", len(in.GetChecks()))
	}
	out := make([]*iamv1.AuthorizeCheckResponse, 0, len(in.GetChecks()))
	for _, c := range in.GetChecks() {
		out = append(out, &iamv1.AuthorizeCheckResponse{Allowed: s.allow[c.GetResource().GetId()]})
	}
	return &iamv1.BatchAuthorizeCheckResponse{Responses: out}, nil
}

// narrowerOver — настоящий сужатель поверх подставного соседа.
func narrowerOver(s *stubVisibility) *listnarrow.Narrower {
	return listnarrow.New(s, listnarrow.Config{Relations: authzfilter.PageRelations})
}

// allowAllVisibility — видимость, которая ДЕЙСТВИТЕЛЬНО сужает, но разрешает всё, о
// чём спрошена. Нужна пробам, чей предмет — механика курсора, а не авторизация.
func allowAllVisibility(ids ...string) *listnarrow.Narrower {
	n, _ := recordingVisibility(ids...)
	return n
}

// recordingVisibility — то же, вместе с соседом: пробе, утверждающей, О ЧЁМ спросили
// (или что не спросили вовсе), нужен сам наблюдатель, а не только исход.
func recordingVisibility(ids ...string) (*listnarrow.Narrower, *stubVisibility) {
	allow := make(map[string]bool, len(ids))
	for _, id := range ids {
		allow[id] = true
	}
	stub := &stubVisibility{allow: allow}
	return narrowerOver(stub), stub
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
	pgtest.ClosePoolAtEnd(t, pool)

	for _, id := range []string{"epi-a01", "epi-a02", "epi-b01"} {
		_, err := pool.Exec(base,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
			 VALUES ('Instance',$1,'CREATED','{"projectId":"prj-x","userData":"TENANT_SECRET=nope"}'::jsonb)`, id)
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
	pgtest.ClosePoolAtEnd(t, pool)

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

	stub := &stubVisibility{allow: map[string]bool{mine[0]: true, mine[1]: true}}
	vis := narrowerOver(stub)
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	_ = h.Watch(&computev1.WatchRequest{}, fs)

	got := make([]string, 0, len(fs.sent))
	for _, ev := range fs.sent {
		got = append(got, ev.GetResourceId())
	}
	assert.ElementsMatch(t, mine, got,
		"the stream must carry only rows the caller is entitled to; rows of other tenants must be absent")
	assert.NotEmpty(t, stub.asked, "the model must actually be asked about the rows, not bypassed")
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
	pgtest.ClosePoolAtEnd(t, pool)

	_, err = pool.Exec(base,
		`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
		 VALUES ('Instance','epi-x01','CREATED','{}'::jsonb)`)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(ctxWithUser(base, "usr_alice"), 3*time.Second)
	defer cancel()

	stub := &stubVisibility{err: status.Error(codes.Unavailable, "authorize peer down")}
	vis := narrowerOver(stub)
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
	pgtest.ClosePoolAtEnd(t, pool)

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
	stub := &stubVisibility{allow: map[string]bool{
		"epi-live1": true, "epd-retired": true, "epm-retired": true,
	}}
	vis := narrowerOver(stub)
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
	pgtest.ClosePoolAtEnd(t, pool)

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

	ctx, cancel := context.WithTimeout(ctxWithUser(base, "usr_alice"), 20*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(context.Background()) }()

	stub := &stubVisibility{allow: map[string]bool{"epi-visible": true}}
	vis := narrowerOver(stub)
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	newCursor, err := h.streamSince(ctx, conn, 0, nil, fs)
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

// TestIntegration_Watch_DeletionEventIsNotDeliverableOnceTheObjectIsGone — locks a
// CONSEQUENCE of per-row narrowing that is not obviously desirable, so that it cannot
// change silently in either direction.
//
// Mechanism. `Instance.Delete` hard-deletes the row and, in the SAME transaction,
// emits both the `DELETED` journal row and the intent to withdraw the object's
// registration from the permission model (`internal/repo/instance_repo.go`). Once the
// drainer applies that intent, no subject — including the one who performed the
// deletion — can be granted anything on the object, so the per-row question about the
// `DELETED` row answers "no" and the row is dropped. The payload of a `DELETED` row is
// `{"id": …}`, carrying no project, so there is no second predicate to fall back on.
//
// Why it is locked rather than "fixed" here. Making deletions visible requires
// deciding WHOSE deletions a caller may see, and the only available answer would be
// the parent project — i.e. granting project-tier viewers knowledge of objects they
// were never entitled to see individually. That is an existence oracle and a widening
// of access, and it is a product decision about deletion-event visibility, not a
// detail of this fix. Guessing it would be worse than naming it. Recorded as item 14 of
// docs/engineering/architecture/07-known-divergences.md.
//
// Delivering the row unconditionally is NOT an option: that is the leak this whole
// file exists to close.
//
// The test also pins the narrower unpleasant part: before the intent is drained the
// answer may be "yes", so the pre-drain behaviour is timing-dependent. It asserts the
// SETTLED state (registration withdrawn), which is the state a real deployment reaches.
func TestIntegration_Watch_DeletionEventIsNotDeliverableOnceTheObjectIsGone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	base := context.Background()
	dsn := setupWatchDB(t)
	pool, err := coredb.NewPool(base, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	for _, row := range []struct{ id, event string }{
		{"epi-alive", "CREATED"},
		{"epi-gone", "DELETED"},
	} {
		_, err := pool.Exec(base,
			`INSERT INTO compute_outbox (resource_kind, resource_id, event_type, payload)
			 VALUES ('Instance',$1,$2,'{"id":"x"}'::jsonb)`, row.id, row.event)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(ctxWithUser(base, "usr_alice"), 3*time.Second)
	defer cancel()

	// The settled state: the model holds nothing about the deleted object, exactly as
	// after the withdrawal intent drains. It still answers about the live one.
	stub := &stubVisibility{allow: map[string]bool{"epi-alive": true}}
	vis := narrowerOver(stub)
	h := NewInternalWatchHandler(pool, dsn, slog.Default(), 0, vis)
	fs := &fakeWatchStream{ctx: ctx}

	_ = h.Watch(&computev1.WatchRequest{}, fs)

	got := make([]string, 0, len(fs.sent))
	for _, ev := range fs.sent {
		got = append(got, ev.GetResourceId())
	}
	assert.Equal(t, []string{"epi-alive"}, got,
		"known consequence: a DELETED row is undeliverable once the object's registration is "+
			"withdrawn — see the comment above and known-divergences item 14; it must NOT be 'fixed' by "+
			"delivering the row unconditionally")
	assert.Contains(t, stub.asked, "epi-gone",
		"the row must be REFUSED by the model, not skipped before asking — otherwise a later "+
			"policy decision about deletion visibility would have nowhere to take effect")
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
		vis  *listnarrow.Narrower
	}{
		{"no filter wired at all", nil},
		{"filter wired but does not narrow", narrowtest.Breakglass()},
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

// TestNarrowToSubject_SplitsIntoPartitionsTheModelAccepts — the ≤100 partition
// contract, exercised directly.
//
// It is not reachable through the read loop today, because a read batch is itself
// capped at catchupBatchSize (100), so every group already fits one question. That is
// exactly why it is asserted here rather than left to a full-stream test: the
// splitting is what keeps the contract if the read batch is ever raised, and an
// enforcement path nobody exercises is indistinguishable from one that does not work.
//
// The stub refuses any partition larger than the contract, so a broken split fails the
// call instead of quietly sending an over-sized question the model would reject.
func TestNarrowToSubject_SplitsIntoPartitionsTheModelAccepts(t *testing.T) {
	const rows = watchVisibilityBatchSize*2 + 37 // two full partitions and a remainder

	batch := make([]*computev1.Event, 0, rows)
	ids := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		id := "epi-" + padID(i)
		ids = append(ids, id)
		batch = append(batch, &computev1.Event{SequenceNo: int64(i + 1), ResourceKind: "Instance", ResourceId: id})
	}

	allow := make(map[string]bool, len(ids))
	for _, id := range ids {
		allow[id] = true
	}
	stub := &stubVisibility{allow: allow}
	h := NewInternalWatchHandler(nil, "", slog.Default(), 0, narrowerOver(stub))

	visible, err := h.narrowToSubject(ctxWithUser(context.Background(), "usr_alice"), batch)

	require.NoError(t, err, "each partition must be within the size the model accepts")
	require.Len(t, visible, rows, "splitting must not lose or duplicate rows")
	assert.Equal(t, ids, func() []string {
		got := make([]string, 0, len(visible))
		for _, ev := range visible {
			got = append(got, ev.GetResourceId())
		}
		return got
	}(), "input order must survive the split — the journal is ordered by sequence_no")
	assert.Len(t, stub.asked, rows, "every row must be asked about exactly once")
}

// passthroughNarrower — сужатель, который ПРОВЯЗАН и отвечает, но пропускает всё, о
// чём спрошен. Это ровно то, что делает настоящий сужатель в аварийном режиме, и
// единственное, чем он отличается от работающего, — `Narrows() == false`.
//
// Дублёра здесь больше нет: аварийный режим — БОЕВАЯ посадка, и проба берёт её как
// есть, вместе с записывающим соседом, чтобы утверждать, что его даже не спросили.
func passthroughNarrower() (*listnarrow.Narrower, *narrowtest.Peer) {
	peer := &narrowtest.Peer{AllowAll: true}
	return listnarrow.New(peer, listnarrow.Config{
		Relations:  authzfilter.PageRelations,
		Breakglass: true,
	}), peer
}

// TestNarrowToSubject_RefusesAFilterThatDoesNotNarrow — the read loop must refuse a
// non-narrowing filter, not merely a missing one.
//
// The entry to the RPC already checks both. The read loop checked only `nil`, and that
// asymmetry is worse than it looks: a nil filter would panic — loud, recovered into
// INTERNAL, nothing delivered — whereas a wired-but-passthrough filter returns every id
// unchanged, so a second caller of the read loop would emit the whole journal SILENTLY,
// under code that looks filtered. The quiet case was the one left open.
//
// Asserted on the observable: an error and no rows, for a filter that would otherwise
// have said yes to everything.
func TestNarrowToSubject_RefusesAFilterThatDoesNotNarrow(t *testing.T) {
	vis, peer := passthroughNarrower()
	h := NewInternalWatchHandler(nil, "", slog.Default(), 0, vis)

	visible, err := h.narrowToSubject(ctxWithUser(context.Background(), "usr_alice"),
		[]*computev1.Event{
			{SequenceNo: 1, ResourceKind: "Instance", ResourceId: "epi-a"},
			{SequenceNo: 2, ResourceKind: "Instance", ResourceId: "epi-b"},
		})

	require.Error(t, err, "a filter that passes ids through is not narrowing, so it cannot authorise the rows")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, visible, "not one row may be returned by a filter that narrows nothing")
	assert.Zero(t, peer.Calls, "such a filter must not even be consulted — its answer means nothing")
}

// TestWatchStreamSince_WithoutAFilterFailsClosedRatherThanPanics — the narrowing must
// be refused where the rows are USED, not only at the entry to the RPC.
//
// Watch checks for a working filter before it opens anything, so no current path
// reaches the read loop without one. That is exactly the shape in which such holes
// come back: the guard sits at one door, someone later adds a second caller of the
// read loop, and the missing filter surfaces as a nil dereference — recovered into an
// opaque INTERNAL, indistinguishable from a database problem, with no statement about
// authorisation anywhere.
//
// So the read loop states the requirement itself. Asserted on the observable: an
// error, no rows, and no panic.
func TestWatchStreamSince_WithoutAFilterFailsClosedRatherThanPanics(t *testing.T) {
	h := NewInternalWatchHandler(nil, "", slog.Default(), 0, nil)

	// A non-empty batch is what makes the question unavoidable; an empty one has
	// nothing to ask about and would pass regardless.
	visible, err := h.narrowToSubject(ctxWithUser(context.Background(), "usr_alice"),
		[]*computev1.Event{{SequenceNo: 1, ResourceKind: "Instance", ResourceId: "epi-x"}})

	require.Error(t, err, "no filter means no answer about these rows, which is not a yes")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, visible)
	// NB: no assertion about a stream here. narrowToSubject takes no stream, so an
	// `Empty(fs.sent)` on a locally-built fake — as this test first carried — could not
	// fail for any defect: nothing ever writes to it. The delivered-row observable
	// belongs to the tests that call Watch.
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
