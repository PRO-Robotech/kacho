// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Ordering of the operation row against the mutation it describes.
//
// Every assertion here reads POSTGRES, never the returned value. The returned
// *operations.Operation is built in memory by the use-case a line after it decides
// what to report, so `op.Done`/`op.Error` are true of the struct whatever did or did
// not reach the database — the very property under test would be invisible to them.
// The failure mode is a crash BETWEEN two statements, and only the rows survive a
// crash, so only the rows can testify.
//
// What has to hold: the operation row exists, done=false, BEFORE the mutation runs.
// If it is written after, a process that dies in between leaves the mutation
// committed and its operation reported as a refusal — the caller is told the region
// was not created while the row sits in the catalog. The same shape was fixed
// deliberately in services/iam/.../cluster/ (row first, terminal state by atomic CAS)
// and is the rule pkg/operations.MetadataFinalizer states in its own doc comment.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/pg"
)

// opRowsFor — the operations rows a poller would find for this description, and
// whether any of them is already terminal. Reads Postgres, not the use-case's
// return value.
func opRowsFor(t *testing.T, pool *pgxpool.Pool, description string) (count int, anyDone bool) {
	t.Helper()
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*), coalesce(bool_or(done), false)
		   FROM kacho_geo.operations WHERE description = $1`,
		description).Scan(&count, &anyDone))
	return count, anyDone
}

func rowCount(t *testing.T, pool *pgxpool.Pool, q, id string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), q, id).Scan(&n))
	return n
}

// regionWriterSpy runs a probe at the instant the mutation is applied, so the test
// can look at the operations table from INSIDE the window the ordering is about.
type regionWriterSpy struct {
	region.Writer
	onInsert func()
	onUpdate func()
	onDelete func()
}

func (w *regionWriterSpy) Insert(ctx context.Context, r *domain.Region) (*domain.Region, error) {
	if w.onInsert != nil {
		w.onInsert()
	}
	return w.Writer.Insert(ctx, r)
}

func (w *regionWriterSpy) Update(ctx context.Context, id string, p region.UpdateParams) (*domain.Region, error) {
	if w.onUpdate != nil {
		w.onUpdate()
	}
	return w.Writer.Update(ctx, id, p)
}

func (w *regionWriterSpy) Delete(ctx context.Context, id string) error {
	if w.onDelete != nil {
		w.onDelete()
	}
	return w.Writer.Delete(ctx, id)
}

type zoneWriterSpy struct {
	zone.Writer
	onInsert func()
}

func (w *zoneWriterSpy) Insert(ctx context.Context, z *domain.Zone) (*domain.Zone, error) {
	if w.onInsert != nil {
		w.onInsert()
	}
	return w.Writer.Insert(ctx, z)
}

// errOpsRepo — an operations repo whose Create refuses. It stands in for the crash
// between the two statements: whatever the use-case does after the mutation cannot
// happen. If the mutation already ran, the catalog row is committed and there is no
// operation to poll — which is exactly the state that must be unreachable.
type errOpsRepo struct {
	operations.Repo
	createErr error
	created   bool
}

func (r *errOpsRepo) Create(ctx context.Context, op operations.Operation) error {
	r.created = true
	if r.createErr != nil {
		return r.createErr
	}
	return r.Repo.Create(ctx, op)
}

func (r *errOpsRepo) MarkDoneWithMetadata(ctx context.Context, id string, metadata, response *anypb.Any) error {
	if f, ok := r.Repo.(operations.MetadataFinalizer); ok {
		return f.MarkDoneWithMetadata(ctx, id, metadata, response)
	}
	return r.Repo.MarkDone(ctx, id, response)
}

// --- Region ---------------------------------------------------------------

func TestRegionCreate_OperationRowPrecedesTheMutation(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	repo := pg.NewRegionRepo(pool)

	const id = "ru-ord-a"
	var seenRows int
	var seenDone bool
	spy := &regionWriterSpy{Writer: repo, onInsert: func() {
		seenRows, seenDone = opRowsFor(t, pool, "Create region "+id)
	}}

	uc := region.New(repo, spy, ops, serviceerr.ToStatus)
	op, err := uc.Create(context.Background(), region.CreateInput{ID: id, Status: domain.GeoStatusUp})
	require.NoError(t, err)
	require.Nil(t, op.Error)

	require.Equal(t, 1, seenRows,
		"the operation row must already be in Postgres when the mutation runs; "+
			"written afterwards, a crash in between commits the region and leaves nothing to poll")
	require.False(t, seenDone, "the operation must still be in flight at that moment, not already terminal")

	// And it ends terminal, with the response a caller polls for.
	after, err := ops.Get(context.Background(), op.ID)
	require.NoError(t, err)
	require.True(t, after.Done)
	require.Nil(t, after.Error)
	require.NotNil(t, after.Response, "the finished row must carry the response, not only the in-memory copy")
}

func TestRegionCreate_OperationPersistFailureLeavesNoCommittedMutation(t *testing.T) {
	pool := newTestPool(t)
	base := operations.NewRepo(pool, "kacho_geo")
	repo := pg.NewRegionRepo(pool)

	const id = "ru-ord-b"
	ops := &errOpsRepo{Repo: base, createErr: errors.New("operations table unavailable")}
	uc := region.New(repo, repo, ops, serviceerr.ToStatus)

	_, err := uc.Create(context.Background(), region.CreateInput{ID: id, Status: domain.GeoStatusUp})
	require.Error(t, err, "a region whose operation cannot be persisted must not be reported as created")
	require.True(t, ops.created, "the use-case must have tried to persist the operation")

	require.Equal(t, 0, rowCount(t, pool, `SELECT count(*) FROM kacho_geo.regions WHERE id = $1`, id),
		"the region must NOT be committed when its operation could not be persisted: "+
			"otherwise the caller is told the create failed while the row is in the catalog")
}

func TestRegionUpdate_OperationRowPrecedesTheMutation(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	repo := pg.NewRegionRepo(pool)

	const id = "ru-ord-c"
	seedRegion(t, ops, region.New(repo, repo, ops, serviceerr.ToStatus), id)

	var seenRows int
	var seenDone bool
	spy := &regionWriterSpy{Writer: repo, onUpdate: func() {
		seenRows, seenDone = opRowsFor(t, pool, "Update region "+id)
	}}
	uc := region.New(repo, spy, ops, serviceerr.ToStatus)
	op, err := uc.Update(context.Background(), region.UpdateInput{ID: id, Mask: []string{"countryCode"}, CountryCode: "NL"})
	require.NoError(t, err)
	require.Nil(t, op.Error)

	require.Equal(t, 1, seenRows, "Update must persist its operation row before it changes the region")
	require.False(t, seenDone)
}

func TestRegionDelete_OperationRowPrecedesTheMutation(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	repo := pg.NewRegionRepo(pool)

	const id = "ru-ord-d"
	seedRegion(t, ops, region.New(repo, repo, ops, serviceerr.ToStatus), id)

	var seenRows int
	var seenDone bool
	spy := &regionWriterSpy{Writer: repo, onDelete: func() {
		seenRows, seenDone = opRowsFor(t, pool, "Delete region "+id)
	}}
	uc := region.New(repo, spy, ops, serviceerr.ToStatus)
	op, err := uc.Delete(context.Background(), id)
	require.NoError(t, err)
	require.Nil(t, op.Error)

	require.Equal(t, 1, seenRows, "Delete must persist its operation row before it removes the region")
	require.False(t, seenDone)
}

// A DB-refused mutation still has to land as a terminal ERROR row, not as a missing
// row: the caller was handed an operation id and must be able to poll it.
func TestRegionCreate_DbRefusalLandsAsTerminalErrorRow(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	repo := pg.NewRegionRepo(pool)
	uc := region.New(repo, repo, ops, serviceerr.ToStatus)

	const id = "ru-ord-e"
	seedRegion(t, ops, uc, id)

	op, err := uc.Create(context.Background(), region.CreateInput{ID: id, Status: domain.GeoStatusUp})
	require.NoError(t, err, "a duplicate lands on the operation, not as a sync gRPC error")
	require.NotNil(t, op.Error)

	row, gerr := ops.Get(context.Background(), op.ID)
	require.NoError(t, gerr, "the failed operation must be pollable")
	require.True(t, row.Done)
	require.NotNil(t, row.Error, "the error must be in Postgres, not only on the returned struct")
	require.NotEqual(t, int32(0), row.Error.GetCode())
	_ = status.FromProto(row.Error)
}

// --- Zone -----------------------------------------------------------------

func TestZoneCreate_OperationRowPrecedesTheMutation(t *testing.T) {
	pool := newTestPool(t)
	ops := operations.NewRepo(pool, "kacho_geo")
	rrepo := pg.NewRegionRepo(pool)
	zrepo := pg.NewZoneRepo(pool)

	const rid, zid = "ru-ord-z", "ru-ord-z-a"
	seedRegion(t, ops, region.New(rrepo, rrepo, ops, serviceerr.ToStatus), rid)

	var seenRows int
	var seenDone bool
	spy := &zoneWriterSpy{Writer: zrepo, onInsert: func() {
		seenRows, seenDone = opRowsFor(t, pool, "Create zone "+zid)
	}}
	uc := zone.New(zrepo, spy, ops, serviceerr.ToStatus)
	op, err := uc.Create(context.Background(), zone.CreateInput{ID: zid, RegionID: rid, Status: domain.GeoStatusUp})
	require.NoError(t, err)
	require.Nil(t, op.Error)

	require.Equal(t, 1, seenRows, "Zone.Create must persist its operation row before it writes the zone")
	require.False(t, seenDone)
}

func TestZoneCreate_OperationPersistFailureLeavesNoCommittedMutation(t *testing.T) {
	pool := newTestPool(t)
	base := operations.NewRepo(pool, "kacho_geo")
	rrepo := pg.NewRegionRepo(pool)
	zrepo := pg.NewZoneRepo(pool)

	const rid, zid = "ru-ord-y", "ru-ord-y-a"
	seedRegion(t, base, region.New(rrepo, rrepo, base, serviceerr.ToStatus), rid)

	ops := &errOpsRepo{Repo: base, createErr: errors.New("operations table unavailable")}
	uc := zone.New(zrepo, zrepo, ops, serviceerr.ToStatus)

	_, err := uc.Create(context.Background(), zone.CreateInput{ID: zid, RegionID: rid, Status: domain.GeoStatusUp})
	require.Error(t, err)
	require.Equal(t, 0, rowCount(t, pool, `SELECT count(*) FROM kacho_geo.zones WHERE id = $1`, zid),
		"the zone must NOT be committed when its operation could not be persisted")
}
