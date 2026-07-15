// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
)

// mkAttach строит self-describing attach-payload в зоне region-1-a / проекте prj-1
// (когерентно с mkVolume). ProjectID/ZoneID — placement инстанса для CAS-сверки.
func mkAttach(volumeID, instanceID, device string, boot bool) *domain.VolumeAttachment {
	return &domain.VolumeAttachment{
		VolumeID:     volumeID,
		InstanceID:   instanceID,
		InstanceName: "web-1",
		ProjectID:    "prj-1",
		ZoneID:       "region-1-a",
		DeviceName:   device,
		IsBoot:       boot,
		Mode:         domain.AttachmentModeReadWrite,
	}
}

// attachRowCount — число строк volume_attachments для тома (проверка «ровно одна» /
// «удалена»).
func attachRowCount(t *testing.T, pool *pgxpool.Pool, volumeID string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM volume_attachments WHERE volume_id=$1`, volumeID).Scan(&n))
	return n
}

// TestAttachHappyDerivedInUse — CAS-insert прошёл (1 row); Get → IN_USE (derived),
// attachments[0] с device/instance (S2-01, §3.2).
func TestAttachHappyDerivedInUse(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-attach-1", 10<<30)

	require.NoError(t, r.Attach(ctx, mkAttach(v.ID, "epd00000000000000001", "sdb", false)))

	got, err := r.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VolumeStatusInUse, got.Status, "READY + attachment → IN_USE")
	require.Len(t, got.Attachments, 1)
	require.Equal(t, "epd00000000000000001", got.Attachments[0].InstanceID)
	require.Equal(t, "sdb", got.Attachments[0].DeviceName)
	require.Equal(t, domain.AttachmentModeReadWrite, got.Attachments[0].Mode)
}

// TestAttachIdempotentReplay — повтор того же инстанса → OK, ровно одна строка (S2-02).
func TestAttachIdempotentReplay(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-replay", 10<<30)

	a := mkAttach(v.ID, "epd00000000000000002", "sdb", false)
	require.NoError(t, r.Attach(ctx, a))
	require.NoError(t, r.Attach(ctx, a), "idempotent replay (same instance) → OK")
	require.Equal(t, 1, attachRowCount(t, pool, v.ID), "no duplicate attachment row")
}

// TestAttachVolumeNotReady — том не READY → FailedPrecondition
// "Volume is not available for attachment" (S2-03, CAS WHERE state='READY' не сматчил).
func TestAttachVolumeNotReady(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-creating", 10<<30)
	_, err := pool.Exec(ctx, `UPDATE volumes SET state='CREATING' WHERE id=$1`, v.ID)
	require.NoError(t, err)

	err = r.Attach(ctx, mkAttach(v.ID, "epd00000000000000003", "sdb", false))
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "Volume is not available for attachment", err.Error()[len("failed precondition: "):])
	require.Equal(t, 0, attachRowCount(t, pool, v.ID))
}

// TestAttachZoneProjectMismatch — CS1-S4-05 / INV-4: zone и project — ДВА разных
// CAS-предиката, каждый со СВОИМ нормативным текстом (не переиспользуется zone-текст
// на project-mismatch — исправление относительно companion S2-04). Disambiguation
// после 0-row CAS различает, какой предикат не сматчил.
func TestAttachZoneProjectMismatch(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-zone", 10<<30) // zone region-1-a, project prj-1

	// расходится ТОЛЬКО зона → zone-текст.
	zoneBad := mkAttach(v.ID, "epd00000000000000004", "sdb", false)
	zoneBad.ZoneID = "region-1-b"
	err := r.Attach(ctx, zoneBad)
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "zone mismatch: %v", err)
	require.Equal(t, "Volume and Instance must be in the same zone", err.Error()[len("failed precondition: "):])

	// расходится ТОЛЬКО проект → ОТДЕЛЬНЫЙ project-текст (не zone-строка).
	projBad := mkAttach(v.ID, "epd00000000000000004", "sdb", false)
	projBad.ProjectID = "prj-other"
	err = r.Attach(ctx, projBad)
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "project mismatch: %v", err)
	require.Equal(t, "Volume and Instance must be in the same project", err.Error()[len("failed precondition: "):])
	require.Equal(t, 0, attachRowCount(t, pool, v.ID))
}

// TestAttachDoubleRace — два инстанса конкурентно attach'ат один том → ровно один OK,
// остальные FailedPrecondition "Volume <id> is in use" (S2-05/A2, CAS PK row-lock).
// Детерминизм: старт-гейт освобождает все горутины разом (не time.Sleep). Под -race.
func TestAttachDoubleRace(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	v := mkVolume(t, r, "prj-1", "vol-race", 10<<30)

	const n = 6
	var ok, inUse atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			// разные инстансы, одинаковое device (конфликт — только PK volume_id).
			a := mkAttach(v.ID, fmt.Sprintf("epd0000000000000010%d", idx), "sdb", false)
			err := r.Attach(context.Background(), a)
			switch {
			case err == nil:
				ok.Add(1)
			case stderrors.Is(err, ports.ErrFailedPrecondition):
				inUse.Add(1)
				require.Equal(t, fmt.Sprintf("Volume %s is in use", v.ID), err.Error()[len("failed precondition: "):])
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, ok.Load(), "exactly one attach wins")
	require.EqualValues(t, n-1, inUse.Load())
	require.Equal(t, 1, attachRowCount(t, pool, v.ID))
}

// TestAttachDeviceCollision — то же device_name на том же инстансе → FailedPrecondition
// "device <name> is already in use on Instance <id>" (S2-06, UNIQUE(instance_id,device_name)).
func TestAttachDeviceCollision(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v1 := mkVolume(t, r, "prj-1", "vol-dev-1", 10<<30)
	v2 := mkVolume(t, r, "prj-1", "vol-dev-2", 10<<30)

	require.NoError(t, r.Attach(ctx, mkAttach(v1.ID, "epd00000000000000005", "sdb", false)))
	err := r.Attach(ctx, mkAttach(v2.ID, "epd00000000000000005", "sdb", false))
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, "device sdb is already in use on Instance epd00000000000000005",
		err.Error()[len("failed precondition: "):])
}

// TestAttachSecondBoot — второй boot-том на инстанс → FailedPrecondition
// "Instance <id> already has a boot volume" (S2-07, EXCLUDE WHERE is_boot 23P01).
func TestAttachSecondBoot(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v1 := mkVolume(t, r, "prj-1", "vol-boot-1", 10<<30)
	v2 := mkVolume(t, r, "prj-1", "vol-boot-2", 10<<30)
	const ins = "epd00000000000000006"

	require.NoError(t, r.Attach(ctx, mkAttach(v1.ID, ins, "sda", true)))
	err := r.Attach(ctx, mkAttach(v2.ID, ins, "sdc", true))
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("Instance %s already has a boot volume", ins),
		err.Error()[len("failed precondition: "):])
}

// TestDetachIdempotent — detach удаляет строку (derived AVAILABLE); повтор → no-op OK (S2-08).
func TestDetachIdempotent(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v := mkVolume(t, r, "prj-1", "vol-detach", 10<<30)
	const ins = "epd00000000000000008"
	require.NoError(t, r.Attach(ctx, mkAttach(v.ID, ins, "sdb", false)))

	require.NoError(t, r.Detach(ctx, v.ID, ins))
	require.Equal(t, 0, attachRowCount(t, pool, v.ID))
	got, err := r.Get(ctx, v.ID)
	require.NoError(t, err)
	require.Equal(t, domain.VolumeStatusAvailable, got.Status, "detach → derived AVAILABLE")

	require.NoError(t, r.Detach(ctx, v.ID, ins), "idempotent detach (0 rows) → OK")
}

// TestListAttachmentsBatched — батч по instance_ids[] (не N+1): группировка по инстансу (S2-09).
func TestListAttachmentsBatched(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v1 := mkVolume(t, r, "prj-1", "vol-la-1", 10<<30)
	v2 := mkVolume(t, r, "prj-1", "vol-la-2", 10<<30)
	v3 := mkVolume(t, r, "prj-1", "vol-la-3", 10<<30)
	const ins1, ins2 = "epd00000000000000101", "epd00000000000000102"
	require.NoError(t, r.Attach(ctx, mkAttach(v1.ID, ins1, "sdb", false)))
	require.NoError(t, r.Attach(ctx, mkAttach(v2.ID, ins1, "sdc", false)))
	require.NoError(t, r.Attach(ctx, mkAttach(v3.ID, ins2, "sdb", false)))

	atts, err := r.ListAttachments(ctx, []string{ins1, ins2})
	require.NoError(t, err)
	require.Len(t, atts, 3)
	byInstance := map[string]int{}
	for _, a := range atts {
		byInstance[a.InstanceID]++
	}
	require.Equal(t, 2, byInstance[ins1])
	require.Equal(t, 1, byInstance[ins2])
}

// TestAttachAutoDeviceNameRace — CS1-S4-08 (ban #12, -race): два конкурентных auto-device
// attach РАЗНЫХ томов на ОДИН инстанс → разные имена; проигравший 23505 на
// UNIQUE(instance_id,device_name) РЕТРАИТСЯ до свободного (retry-until-free), 23505 наружу
// НЕ вытекает. Старт-гейт освобождает горутины разом (не time.Sleep).
func TestAttachAutoDeviceNameRace(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	// vol-A занимает sdb; N томов конкурентно гонятся за первыми свободными именами.
	// N высок, чтобы (без retry) большинство горутин прочитали один committed-снимок
	// used-set, выбрали одно имя и столкнулись на 23505 → RED; с retry — все различны.
	const n = 8
	const ins = "epd00000000000000210"
	vA := mkVolume(t, r, "prj-1", "vol-arace-a", 10<<30)
	require.NoError(t, r.Attach(ctx, mkAttach(vA.ID, ins, "sdb", false)))
	vids := make([]string, n)
	for i := range vids {
		vids[i] = mkVolume(t, r, "prj-1", fmt.Sprintf("vol-arace-%d", i), 10<<30).ID
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i, vid := range vids {
		wg.Add(1)
		go func(idx int, volumeID string) {
			defer wg.Done()
			<-start
			errs[idx] = r.Attach(context.Background(), mkAttach(volumeID, ins, "", false)) // пустой device → auto
		}(i, vid)
	}
	close(start)
	wg.Wait()
	for i, e := range errs {
		require.NoError(t, e, "auto-device attach %d must retry-until-free, never leak 23505", i)
	}
	// ровно n+1 строк на инстансе (sdb + n авто), все device_name различны.
	var names []string
	rows, err := pool.Query(ctx, `SELECT device_name FROM volume_attachments WHERE instance_id=$1 ORDER BY device_name`, ins)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var dn string
		require.NoError(t, rows.Scan(&dn))
		names = append(names, dn)
	}
	require.Len(t, names, n+1)
	require.Equal(t, len(names), len(uniqueStrings(names)), "all device names distinct: %v", names)
}

// TestAttachNoFreeDevice — CS1-S4-07: пространство имён sdb..sdz (25) исчерпано → auto-attach
// возвращает FailedPrecondition "no free device name on Instance <id>" (не ErrInternal, не 23505).
func TestAttachNoFreeDevice(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	const ins = "epd00000000000000220"
	// занять все 25 имён sdb..sdz явными attach разных томов.
	for c := byte('b'); c <= 'z'; c++ {
		v := mkVolume(t, r, "prj-1", fmt.Sprintf("vol-fill-%c", c), 10<<30)
		require.NoError(t, r.Attach(ctx, mkAttach(v.ID, ins, "sd"+string(c), false)))
	}
	vOver := mkVolume(t, r, "prj-1", "vol-overflow", 10<<30)
	err := r.Attach(ctx, mkAttach(vOver.ID, ins, "", false)) // auto → нет свободного
	require.True(t, stderrors.Is(err, ports.ErrFailedPrecondition), "got %v", err)
	require.Equal(t, fmt.Sprintf("no free device name on Instance %s", ins), err.Error()[len("failed precondition: "):])
}

// uniqueStrings — helper: множество различных строк.
func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// TestAttachAutoDeviceName — пустой device → авто-назначение уникального; второй том
// без device → другое имя (S3-11, UNIQUE(instance_id,device_name) не нарушен).
func TestAttachAutoDeviceName(t *testing.T) {
	pool := newTestPool(t)
	r := pg.NewVolumeRepo(pool)
	ctx := context.Background()
	v1 := mkVolume(t, r, "prj-1", "vol-auto-1", 10<<30)
	v2 := mkVolume(t, r, "prj-1", "vol-auto-2", 10<<30)
	const ins = "epd00000000000000201"

	require.NoError(t, r.Attach(ctx, mkAttach(v1.ID, ins, "", false)))
	require.NoError(t, r.Attach(ctx, mkAttach(v2.ID, ins, "", false)))

	g1, err := r.Get(ctx, v1.ID)
	require.NoError(t, err)
	g2, err := r.Get(ctx, v2.ID)
	require.NoError(t, err)
	d1 := g1.Attachments[0].DeviceName
	d2 := g2.Attachments[0].DeviceName
	require.NotEmpty(t, d1, "auto-assigned device name")
	require.NotEmpty(t, d2)
	require.NotEqual(t, d1, d2, "distinct auto-assigned device names on same instance")
}
