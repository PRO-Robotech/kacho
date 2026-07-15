// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	"github.com/PRO-Robotech/kacho/services/storage/internal/clients"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// fgaOutboxRow — снимок строки kacho_storage.fga_register_outbox для assert'ов.
type fgaOutboxRow struct {
	eventType    string
	resourceKind string
	resourceID   string
	payload      []byte
	sentAtNull   bool
}

// selectFGARows читает все строки fga_register_outbox (по возрастанию id).
func selectFGARows(t *testing.T, pool *pgxpool.Pool) []fgaOutboxRow {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT event_type, resource_kind, resource_id, payload, (sent_at IS NULL)
		   FROM kacho_storage.fga_register_outbox ORDER BY id ASC`)
	require.NoError(t, err)
	defer rows.Close()
	var out []fgaOutboxRow
	for rows.Next() {
		var r fgaOutboxRow
		require.NoError(t, rows.Scan(&r.eventType, &r.resourceKind, &r.resourceID, &r.payload, &r.sentAtNull))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestVolumeInsert_EmitsFGARegisterIntent — Volume.Insert атомарно пишет
// fga.register-строку в fga_register_outbox (owner-tuple project#project@storage_volume)
// в той же writer-TX, что и доменный INSERT (SEC-D transactional-outbox, ban #10/#16).
func TestVolumeInsert_EmitsFGARegisterIntent(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	v := mkVolume(t, r, "prj-1", "vol-fga", 10<<30)

	rows := selectFGARows(t, pool)
	require.Len(t, rows, 1, "ровно одна register-строка на Create")
	require.Equal(t, "fga.register", rows[0].eventType)
	require.Equal(t, "storage_volume", rows[0].resourceKind)
	require.Equal(t, v.ID, rows[0].resourceID)
	require.True(t, rows[0].sentAtNull, "свежий intent ещё не применён (sent_at NULL)")

	p, err := fgaregister.Decode(rows[0].payload)
	require.NoError(t, err)
	require.Equal(t, "project:prj-1", p.SubjectID)
	require.Equal(t, "project", p.Relation)
	require.Equal(t, "storage_volume:"+v.ID, p.Object)
	require.False(t, p.SourceVersion.IsZero(), "source_version проштампован БД-часами (now())")
}

// TestVolumeDelete_EmitsFGAUnregisterIntent — Volume.Delete атомарно пишет
// fga.unregister-строку (снятие owner-tuple) в той же TX, что и DELETE строки тома.
func TestVolumeDelete_EmitsFGAUnregisterIntent(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()

	v := mkVolume(t, r, "prj-1", "vol-del", 10<<30)
	require.NoError(t, r.Delete(ctx, v.ID))

	rows := selectFGARows(t, pool)
	require.Len(t, rows, 2, "register (Create) + unregister (Delete)")
	require.Equal(t, "fga.register", rows[0].eventType)
	require.Equal(t, "fga.unregister", rows[1].eventType)
	require.Equal(t, "storage_volume", rows[1].resourceKind)
	require.Equal(t, v.ID, rows[1].resourceID)

	p, err := fgaregister.Decode(rows[1].payload)
	require.NoError(t, err)
	require.Equal(t, "project:prj-1", p.SubjectID)
	require.Equal(t, "storage_volume:"+v.ID, p.Object)
}

// TestSnapshotInsert_EmitsFGARegisterIntent — Snapshot.Insert пишет
// fga.register-строку owner-tuple storage_snapshot в writer-TX from-READY-CAS.
func TestSnapshotInsert_EmitsFGARegisterIntent(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	v := mkVolume(t, vr, "prj-1", "vol-src", 10<<30)
	s, err := sr.Insert(ctx, &domain.Snapshot{
		ID:             ids.NewID(domain.PrefixSnapshot),
		ProjectID:      "prj-1",
		Name:           "snap-fga",
		SourceVolumeID: v.ID,
	})
	require.NoError(t, err)

	rows := selectFGARows(t, pool)
	// vol register + snapshot register
	require.Len(t, rows, 2)
	snapRow := rows[1]
	require.Equal(t, "fga.register", snapRow.eventType)
	require.Equal(t, "storage_snapshot", snapRow.resourceKind)
	require.Equal(t, s.ID, snapRow.resourceID)

	p, err := fgaregister.Decode(snapRow.payload)
	require.NoError(t, err)
	require.Equal(t, "project:prj-1", p.SubjectID)
	require.Equal(t, "storage_snapshot:"+s.ID, p.Object)
}

// TestSnapshotDelete_EmitsFGAUnregisterIntent — Snapshot.Delete пишет fga.unregister.
func TestSnapshotDelete_EmitsFGAUnregisterIntent(t *testing.T) {
	pool := newTestPool(t)
	vr := pg.NewVolumeRepo(pool)
	sr := pg.NewSnapshotRepo(pool)
	ctx := context.Background()

	v := mkVolume(t, vr, "prj-1", "vol-src2", 10<<30)
	s, err := sr.Insert(ctx, &domain.Snapshot{
		ID:             ids.NewID(domain.PrefixSnapshot),
		ProjectID:      "prj-1",
		Name:           "snap-del",
		SourceVolumeID: v.ID,
	})
	require.NoError(t, err)
	require.NoError(t, sr.Delete(ctx, s.ID))

	rows := selectFGARows(t, pool)
	require.Len(t, rows, 3) // vol register, snap register, snap unregister
	last := rows[2]
	require.Equal(t, "fga.unregister", last.eventType)
	require.Equal(t, "storage_snapshot", last.resourceKind)
	require.Equal(t, s.ID, last.resourceID)
}

// TestVolumeInsert_FailedFK_NoFGAIntent — при откате writer-TX (FK RESTRICT на
// несуществующий disk_type) fga.register-строка НЕ остаётся: intent атомарен с DML
// (не dual-write) — orphan-tuple исключён by construction.
func TestVolumeInsert_FailedFK_NoFGAIntent(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	_, err := r.Insert(context.Background(), &domain.Volume{
		ID:         ids.NewID(domain.PrefixVolume),
		ProjectID:  "prj-1",
		Name:       "vol-orphan",
		ZoneID:     "region-1-a",
		DiskTypeID: "block-unicorn", // нет в каталоге → FK RESTRICT 23503 → rollback
		SizeBytes:  10 << 30,
	})
	require.Error(t, err)

	require.Empty(t, selectFGARows(t, pool), "откат TX не оставляет orphan register-intent")
}

// fakeIAMRegisterClient — потокобезопасный двойник clients.IAMRegisterRPC для
// drainer-теста (drainer применяет из горутины).
type fakeIAMRegisterClient struct {
	mu       sync.Mutex
	register []*iamv1.RegisterResourceRequest
}

func (f *fakeIAMRegisterClient) RegisterResource(_ context.Context, in *iamv1.RegisterResourceRequest, _ ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.register = append(f.register, in)
	return &iamv1.RegisterResourceResponse{}, nil
}

func (f *fakeIAMRegisterClient) UnregisterResource(_ context.Context, _ *iamv1.UnregisterResourceRequest, _ ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error) {
	return &iamv1.UnregisterResourceResponse{}, nil
}

func (f *fakeIAMRegisterClient) registerCalls() []*iamv1.RegisterResourceRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*iamv1.RegisterResourceRequest(nil), f.register...)
}

// TestFGARegisterDrainer_AppliesIntentToIAM — end-to-end: Volume.Create эмитит
// register-intent в fga_register_outbox → corelib outbox/drainer забирает строку и
// применяет через clients.NewIAMRegisterApplier (fake IAM) → RegisterResource вызван
// с owner-tuple тома, строка помечена sent (backlog очищен). Это анти-BOLA основа:
// без owner-tuple gateway scope_extractor не резолвит target→project.
func TestFGARegisterDrainer_AppliesIntentToIAM(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)

	v := mkVolume(t, r, "prj-1", "vol-drain", 10<<30) // эмитит register-intent

	fake := &fakeIAMRegisterClient{}
	d, err := drainer.New[clients.FGARegisterPayload](
		pool,
		drainer.Config{
			Table:        "kacho_storage.fga_register_outbox",
			Channel:      "kacho_storage_fga_register_outbox",
			PollFallback: 200 * time.Millisecond,
		},
		clients.DecodeFGARegisterPayload,
		clients.NewIAMRegisterApplier(fake),
		slog.Default(),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	require.Eventually(t, func() bool { return len(fake.registerCalls()) >= 1 },
		10*time.Second, 50*time.Millisecond, "drainer должен применить register-intent через IAM")

	call := fake.registerCalls()[0]
	require.Equal(t, "storage_volume:"+v.ID, call.Object)
	require.Equal(t, "project:prj-1", call.SubjectId)

	// backlog очищен: строка помечена sent (sent_at NOT NULL).
	require.Eventually(t, func() bool {
		var pending int
		if qerr := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM kacho_storage.fga_register_outbox WHERE sent_at IS NULL`).Scan(&pending); qerr != nil {
			return false
		}
		return pending == 0
	}, 10*time.Second, 50*time.Millisecond, "применённый intent помечается sent")
}
