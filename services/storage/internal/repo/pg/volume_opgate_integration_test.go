// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// Owner-tuple op-gating для Volume.Create (opgate P5) — integration (testcontainers
// Postgres + реальный pg.VolumeRepo + реальный operations.Repo + fake confirm-порт).
//
// Гарантия (acceptance sub-phase-owner-tuple-opgate, OTG-03/04/05/05b): Create-op
// достигает `done=true,result=response` ТОЛЬКО после read-after-register confirm
// owner-tuple; иначе fail-closed по confirmation-deadline — op.error
// (codes.Unavailable, "owner-tuple registration not confirmed"), НИКОГДА ложный
// success-done; resource-ref (CreateVolumeMetadata.volume_id) обнаружим на ВСЕХ
// терминалах, ресурс/register-intent durable во всех ветках.
//
// Behavioral-lock (не только код): проверяем НАБЛЮДАЕМОЕ — pending пока confirm DENY,
// отсутствие 403-окна (confirm-Check реплицирует gateway scope_extractor editor@
// storage_volume:<id>, FIX-2) на немедленной мутации, точный код+текст timeout,
// durability строки тома и fga_register_outbox-intent.

import (
	"context"
	stderrors "errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/config"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// ── fake confirm-порты (под контролем теста) ────────────────────────────────

// flipConfirmer — read-after-register проба, DENY пока не поднят allowed. Моделирует
// owner-tuple, ставший видимым в FGA (OTG-03: pending→ALLOW ordering).
type flipConfirmer struct {
	allowed atomic.Bool
	calls   atomic.Int64
	lastVol atomic.Value // string — последний volumeID, поданный в Confirm
}

func (c *flipConfirmer) Confirm(_ context.Context, _ operations.Principal, volumeID string) (bool, error) {
	c.calls.Add(1)
	c.lastVol.Store(volumeID)
	return c.allowed.Load(), nil
}

// lagConfirmer — read-after-register проба с искусственной пропагацией: ALLOW только
// спустя lag после reset (моделирует лаг видимости owner-tuple в FGA; OTG-04 403-окно).
type lagConfirmer struct {
	mu      sync.Mutex
	allowAt time.Time
	calls   atomic.Int64
}

func (c *lagConfirmer) reset(lag time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowAt = time.Now().Add(lag)
}

func (c *lagConfirmer) Confirm(_ context.Context, _ operations.Principal, _ string) (bool, error) {
	c.calls.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().After(c.allowAt), nil
}

// denyConfirmer — навсегда DENY + transient Unavailable-подобная ошибка (моделирует
// IAM/FGA outage дольше confirmation-deadline; OTG-05 fail-closed timeout). raw-текст
// НЕ должен течь наружу — worker маппит в фикс. "owner-tuple registration not confirmed".
type denyConfirmer struct{ calls atomic.Int64 }

func (c *denyConfirmer) Confirm(_ context.Context, _ operations.Principal, _ string) (bool, error) {
	c.calls.Add(1)
	return false, stderrors.New("iam unavailable: openfga read failed host=10.0.0.9:8081")
}

// ── харнесс ─────────────────────────────────────────────────────────────────

// opgateWorker — изолированный LRO-worker с override'нутым confirmation-deadline
// (не прод-30s) и быстрым terminal-write-budget (тестам не ждать секундами).
func opgateWorker(t *testing.T, deadline time.Duration, opts ...operations.WorkerOption) *operations.Worker {
	t.Helper()
	base := []operations.WorkerOption{
		operations.WithConfirmationDeadline(deadline),
		operations.WithTerminalWriteConfig(operations.TerminalWriteConfig{
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     20 * time.Millisecond,
			MaxElapsed:      500 * time.Millisecond,
		}),
	}
	w := operations.NewWorker(append(base, opts...)...)
	w.Start()
	t.Cleanup(w.Stop)
	return w
}

// okPeers — geo/iam peer-порты, всегда разрешающие (cross-domain ref не в фокусе opgate).
func okPeers() (*portmock.PeerClient, *portmock.PeerClient) {
	geo := &portmock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	return geo, iam
}

// newVolume — валидный вход Volume.Create (seededDiskType существует; FK RESTRICT).
func newVolume(project, name string) *domain.Volume {
	return &domain.Volume{
		ProjectID:  project,
		Name:       name,
		ZoneID:     "region-1-a",
		DiskTypeID: seededDiskType,
		SizeBytes:  10 << 30,
	}
}

// metaVolumeID — resource-ref из Operation.metadata (CreateVolumeMetadata.volume_id).
func metaVolumeID(t *testing.T, op *operations.Operation) string {
	t.Helper()
	m, err := operations.MetadataFor[*storagev1.CreateVolumeMetadata](op)
	require.NoError(t, err)
	require.NotEmpty(t, m.GetVolumeId(), "CreateVolumeMetadata.volume_id должен быть populated")
	return m.GetVolumeId()
}

// awaitOpTerminal поллит operations.Repo до done=true (детерминированно, не time.Sleep).
func awaitOpTerminal(t *testing.T, ops operations.Repo, id string) *operations.Operation {
	t.Helper()
	var out *operations.Operation
	require.Eventually(t, func() bool {
		got, err := ops.Get(context.Background(), id)
		if err != nil || !got.Done {
			return false
		}
		out = got
		return true
	}, 15*time.Second, 20*time.Millisecond, "op %s must reach terminal state", id)
	return out
}

// ── OTG-03 — op.done(success) наступает ТОЛЬКО после confirm ALLOW ────────────

// TestVolume_OTG03_OpDoneOnlyAfterConfirmAllow — пока confirm-проба DENY (pending),
// Create-op остаётся done=false, НО ресурс-строка + register-intent уже durable
// (writer-TX закоммичен). Как только проба ALLOW → op становится done=true,response.
// Момент success-done НЕ предшествует первому ALLOW (pending-окно наблюдаемо).
func TestVolume_OTG03_OpDoneOnlyAfterConfirmAllow(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	opsRepo := operations.NewRepo(pool, config.DBSchema)
	geo, iam := okPeers()

	confirmer := &flipConfirmer{} // старт DENY
	w := opgateWorker(t, 10*time.Second)
	uc := volume.New(repo, repo, geo, iam, opsRepo, serviceerr.ToStatus).
		WithOwnerConfirm(confirmer).
		WithConfirmWorker(w)

	op, err := uc.Create(context.Background(), newVolume("prj-1", "vol-otg03"))
	require.NoError(t, err, "sync-фаза Create не должна падать")
	volID := metaVolumeID(t, op)

	// confirm-loop крутится, но пока DENY — op не done. Ресурс/intent durable.
	require.Eventually(t, func() bool { return confirmer.calls.Load() >= 2 },
		5*time.Second, 20*time.Millisecond, "confirm-проба должна ретраиться")
	got, gerr := opsRepo.Get(context.Background(), op.ID)
	require.NoError(t, gerr)
	require.False(t, got.Done, "op НЕ должна быть done, пока confirm DENY (pending)")

	// Ресурс-строка durable во время pending (Insert закоммичен до confirm-loop).
	_, rerr := repo.Get(context.Background(), volID)
	require.NoError(t, rerr, "том durable во время pending confirm")
	// register-intent durable (fga_register_outbox owner-tuple).
	rows := selectFGARows(t, pool)
	require.Len(t, rows, 1)
	require.Equal(t, "fga.register", rows[0].eventType)
	require.Equal(t, volID, rows[0].resourceID)

	// Регистрируем tuple → confirm начинает ALLOW.
	confirmer.allowed.Store(true)

	done := awaitOpTerminal(t, opsRepo, op.ID)
	require.Nil(t, done.Error, "success-путь: op.error должен быть nil")
	require.NotNil(t, done.Response, "op.response должен нести маршалленный Volume")
	var v storagev1.Volume
	require.NoError(t, done.Response.UnmarshalTo(&v))
	require.Equal(t, volID, v.GetId(), "response несёт тот же созданный том")
}

// ── OTG-04 (КРИТ) — между op.done(success) и мутацией НЕТ 403-окна ────────────

// TestVolume_OTG04_NoDirectRelations403Window — gate OFF (confirmer не подключён) →
// op done раньше видимости owner-tuple → немедленный confirm-Check DENY = 403-окно
// «no direct relations granted» (RED воспроизводит баг). gate ON → op done только
// после confirm ALLOW → немедленный confirm-Check ALLOW во ВСЕХ N итерациях (окно
// закрыто; FIX-2: confirm-Check реплицирует gateway scope_extractor editor@storage_volume).
func TestVolume_OTG04_NoDirectRelations403Window(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	opsRepo := operations.NewRepo(pool, config.DBSchema)
	geo, iam := okPeers()

	const lag = 250 * time.Millisecond
	w := opgateWorker(t, 10*time.Second)

	// mutateAllowed — «выполнил бы gateway scope_extractor Check немедленную мутацию».
	mutateAllowed := func(t *testing.T, c *lagConfirmer, volID string) bool {
		t.Helper()
		ok, err := c.Confirm(context.Background(), operations.Principal{Type: "user", ID: "usr-creator"}, volID)
		require.NoError(t, err)
		return ok
	}

	// gate OFF — 403-окно ВОСПРОИЗВОДИТСЯ (baseline / RED-документирование).
	t.Run("gate_off_window_present", func(t *testing.T) {
		confirmer := &lagConfirmer{}
		confirmer.reset(lag)
		ucOff := volume.New(repo, repo, geo, iam, opsRepo, serviceerr.ToStatus).
			WithConfirmWorker(w) // confirmer НЕ подключён → без opgate
		op, err := ucOff.Create(context.Background(), newVolume("prj-1", "vol-otg04-off"))
		require.NoError(t, err)
		done := awaitOpTerminal(t, opsRepo, op.ID)
		require.Nil(t, done.Error, "gate off: op success немедленно (без confirm)")
		// Немедленно после success-done owner-tuple ещё не видим → мутация словила бы 403.
		require.False(t, mutateAllowed(t, confirmer, metaVolumeID(t, op)),
			"gate off: немедленная мутация ловит 403-окно «no direct relations granted»")
	})

	// gate ON — окна НЕТ ни в одной итерации (ЯДРО фикса).
	t.Run("gate_on_window_closed", func(t *testing.T) {
		const iters = 5
		for i := 0; i < iters; i++ {
			confirmer := &lagConfirmer{}
			confirmer.reset(lag)
			ucOn := volume.New(repo, repo, geo, iam, opsRepo, serviceerr.ToStatus).
				WithOwnerConfirm(confirmer).
				WithConfirmWorker(w)
			op, err := ucOn.Create(context.Background(), newVolume("prj-1", "vol-otg04-on-"+itoa(i)))
			require.NoError(t, err)
			done := awaitOpTerminal(t, opsRepo, op.ID)
			require.Nil(t, done.Error, "iter %d: gate on op success после confirm", i)
			require.True(t, mutateAllowed(t, confirmer, metaVolumeID(t, op)),
				"iter %d: НЕТ 403-окна — owner-tuple подтверждён до success-done (FIX-2)", i)
		}
	})
}

// ── OTG-05 (КРИТ) — confirm timeout → op.error(Unavailable), fail-closed ──────

// TestVolume_OTG05_ConfirmTimeoutFailClosed — confirm недоступен дольше deadline →
// op.error, code=Unavailable (НЕ DeadlineExceeded), точный текст; success-done НЕ
// выставляется НИКОГДА; ресурс-строка + register-intent остаются durable (не откачены).
func TestVolume_OTG05_ConfirmTimeoutFailClosed(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	opsRepo := operations.NewRepo(pool, config.DBSchema)
	geo, iam := okPeers()

	confirmer := &denyConfirmer{} // навсегда DENY+Unavailable (моделирует outage)
	w := opgateWorker(t, 400*time.Millisecond)
	uc := volume.New(repo, repo, geo, iam, opsRepo, serviceerr.ToStatus).
		WithOwnerConfirm(confirmer).
		WithConfirmWorker(w)

	op, err := uc.Create(context.Background(), newVolume("prj-1", "vol-otg05"))
	require.NoError(t, err)
	volID := metaVolumeID(t, op)

	done := awaitOpTerminal(t, opsRepo, op.ID)
	require.NotNil(t, done.Error, "fail-closed: op обязана завершиться error по timeout confirm")
	require.Nil(t, done.Response, "invariant: success-response без confirm НЕ выставляется")
	require.Equal(t, codes.Unavailable, codes.Code(done.Error.GetCode()),
		"timeout-код обязан быть Unavailable (retryable fail-closed; FIX-1)")
	require.NotEqual(t, codes.DeadlineExceeded, codes.Code(done.Error.GetCode()),
		"код НЕ должен быть DeadlineExceeded (FIX-1 явное отклонение)")
	require.Equal(t, "owner-tuple registration not confirmed", done.Error.GetMessage(),
		"стабильный текст — часть контракта (FIX-1); raw confirm-err не течёт наружу")
	require.NotContains(t, done.Error.GetMessage(), "openfga", "raw confirm-err (host/driver) НЕ течёт наружу")

	// Ресурс-строка durable (writer-TX закоммичен ДО confirm-gate; timeout не откатывает).
	_, rerr := repo.Get(context.Background(), volID)
	require.NoError(t, rerr, "том durable на timeout-ветке (не откачен)")
	rows := selectFGARows(t, pool)
	require.Len(t, rows, 1, "register-intent durable на timeout-ветке")
	require.Equal(t, volID, rows[0].resourceID)
	require.True(t, rows[0].sentAtNull, "intent ещё не применён (drainer добьёт at-least-once)")
}

// ── OTG-05b (КРИТ, orphan-guard) — resource-ref на error-терминале; row durable ──

// TestVolume_OTG05b_ResourceRefOnErrorTerminal — на timeout-`error` Operation.metadata
// (CreateVolumeMetadata) несёт resource-ref (volume_id) — обнаружим клиентом НА
// error-пути (не только на success); Get(ref)→200 (ресурс durable, не orphan-без-id).
// Закрывает fail-closed-дыру: error без обнаружимого id → пере-создание → orphan.
func TestVolume_OTG05b_ResourceRefOnErrorTerminal(t *testing.T) {
	pool := newTestPool(t)
	repo := pg.NewVolumeRepo(pool)
	opsRepo := operations.NewRepo(pool, config.DBSchema)
	geo, iam := okPeers()

	confirmer := &denyConfirmer{}
	w := opgateWorker(t, 400*time.Millisecond)
	uc := volume.New(repo, repo, geo, iam, opsRepo, serviceerr.ToStatus).
		WithOwnerConfirm(confirmer).
		WithConfirmWorker(w)

	op, err := uc.Create(context.Background(), newVolume("prj-1", "vol-otg05b"))
	require.NoError(t, err)

	done := awaitOpTerminal(t, opsRepo, op.ID)
	require.NotNil(t, done.Error, "op завершилась error(Unavailable) по timeout")

	// resource-ref обнаружим на error-терминале через RE-FETCH op (MarkError сохраняет metadata).
	refetched, gerr := opsRepo.Get(context.Background(), op.ID)
	require.NoError(t, gerr)
	require.True(t, refetched.Done)
	require.NotNil(t, refetched.Error, "re-fetched op остаётся error-терминалом")
	volID := metaVolumeID(t, refetched) // CreateVolumeMetadata.volume_id на error-пути

	// Get(<resource-ref>) → 200 (durable, не orphan-без-id) — клиент НЕ пере-создаёт.
	v, rerr := repo.Get(context.Background(), volID)
	require.NoError(t, rerr, "Get(resource-ref) на error-терминале → 200 durable")
	require.Equal(t, volID, v.ID)
	assert.Equal(t, "vol-otg05b", v.Name)
}

// itoa — маленький helper без зависимости от strconv в цикле (читаемость кейса).
func itoa(i int) string { return string(rune('a' + i)) }
