// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

// owner-tuple opgate (P3, canonical VPC) — use-case-уровень OTG-03/04/05/05b/13/16
// для Network.Create. Confirm-gate: Create-op достигает `done=true, result=response`
// ТОЛЬКО после того, как read-after-register confirmer подтвердит эффективность
// owner-tuple в FGA — иначе fail-closed Unavailable по confirmation-deadline. Так
// закрыто окно 403 «no direct relations granted» на немедленной мутации создателя.
//
// Тесты — behavioral-lock (не только код): done-ordering, точный код+текст ошибки,
// отсутствие success-done без confirm, resource-ref в op.metadata на error-терминале,
// concurrent independent gating под -race, no-regression Update/Delete-flow.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// fakeConfirmer — управляемый OwnerTupleConfirmer. allow-флаг переключает тест;
// пока false — Confirm возвращает (false,nil) (owner-tuple ещё не виден → pending).
type fakeConfirmer struct {
	allow atomic.Bool
	err   atomic.Pointer[error]

	mu        sync.Mutex
	calls     int
	resources map[string]struct{}
}

func newFakeConfirmer() *fakeConfirmer {
	return &fakeConfirmer{resources: map[string]struct{}{}}
}

func (f *fakeConfirmer) Confirm(_ context.Context, _ operations.Principal, resourceID string) (bool, error) {
	f.mu.Lock()
	f.calls++
	f.resources[resourceID] = struct{}{}
	f.mu.Unlock()
	if ep := f.err.Load(); ep != nil {
		return false, *ep
	}
	return f.allow.Load(), nil
}

func (f *fakeConfirmer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeConfirmer) sawResource(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.resources[id]
	return ok
}

// shortDeadlineDispatch — confirmDispatcher поверх Worker с коротким
// confirmation-deadline (OTG-05: timeout-ветка не должна ждать дефолтные 30s) и
// быстрым terminal-write budget.
func shortDeadlineDispatch(t *testing.T, deadline time.Duration) confirmDispatcher {
	t.Helper()
	w := operations.NewWorker(
		operations.WithConfirmationDeadline(deadline),
		operations.WithTerminalWriteConfig(operations.TerminalWriteConfig{
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     20 * time.Millisecond,
			MaxElapsed:      200 * time.Millisecond,
		}),
	)
	w.Start()
	t.Cleanup(w.Stop)
	return func(ctx context.Context, or operations.Repo, opID string,
		fn func(context.Context) (*anypb.Any, error), confirm operations.ConfirmFunc) {
		operations.RunWithWorkerConfirm(w, ctx, or, opID, fn, confirm)
	}
}

// requireOpNotDoneWithin поллит op в течение d и падает, если она стала done —
// behavioral-lock «пока confirm DENY, op не завершена» (нет окна 403).
func requireOpNotDoneWithin(t *testing.T, or *repomock.OpsRepo, opID string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		op, err := or.Get(context.Background(), opID)
		require.NoError(t, err)
		require.False(t, op.Done,
			"op %s стала done, пока owner-tuple confirmer DENY — окно 403 не закрыто (регрессия OTG-04)", opID)
		time.Sleep(5 * time.Millisecond)
	}
}

func newNetworkForTest(name string) domain.Network {
	return domain.Network{ProjectID: "f1", Name: domain.RcNameVPC(name)}
}

// ---- OTG-03 — op.done(success) наступает ТОЛЬКО после confirm owner-tuple ----

func TestCreateNetwork_OTG03_DoneOnlyAfterConfirm_Ordering(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	fc := newFakeConfirmer() // DENY по умолчанию
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithConfirmer(fc)

	op, err := uc.Execute(context.Background(), newNetworkForTest("net-otg03"))
	require.NoError(t, err)

	// Пока confirmer DENY — worker-fn закоммитил ресурс, но op остаётся done=false
	// (PENDING). Ресурс уже durable (Insert+Commit до confirm-loop).
	requireOpNotDoneWithin(t, or, op.ID, 150*time.Millisecond)
	require.GreaterOrEqual(t, fc.callCount(), 1, "confirm-проба должна крутиться пока DENY")

	netID := networkIDFromOp(t, or, op.ID)
	require.True(t, networkDurable(t, kr, netID), "ресурс-строка durable до confirm (Insert+Commit в worker-fn)")

	// Регистрируем owner-tuple → confirmer начинает ALLOW → op становится done=success.
	fc.allow.Store(true)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error, "после ALLOW — success-done, не error")
	require.NotNil(t, saved.Response, "success-done несёт response")
	assert.True(t, fc.sawResource(netID), "confirm-проба гоняется по id созданного Network")
}

// ---- OTG-04 (КРИТ) — между op.done(success) и мутацией НЕТ окна 403 ----

// gate ON: в N итерациях op НИКОГДА не done пока confirmer DENY (окна нет). RED
// (до фикса / gate OFF) — op done немедленно, до ALLOW (окно). GREEN — залочено.
func TestCreateNetwork_OTG04_NoDirectRelations403Window_GateON(t *testing.T) {
	const iterations = 8
	for i := 0; i < iterations; i++ {
		kr := kachomock.NewRepository()
		or := repomock.NewOpsRepo()
		fc := newFakeConfirmer() // DENY
		uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithConfirmer(fc)

		op, err := uc.Execute(context.Background(), newNetworkForTest("net-otg04"))
		require.NoError(t, err)

		// Пока owner-tuple не подтверждён — op НЕ done: клиент не начнёт немедленную
		// мутацию → 403-окна нет. Это behavioral-lock отсутствия окна.
		requireOpNotDoneWithin(t, or, op.ID, 60*time.Millisecond)

		// owner-tuple зарегистрирован/виден → op done(success). С этого момента
		// немедленная мутация создателя резолвится (ALLOW), не 403.
		fc.allow.Store(true)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.True(t, saved.Done)
		require.Nil(t, saved.Error, "iteration %d: success-done после confirm ALLOW", i)
	}
}

// gate OFF (nil confirmer) — воспроизводит окно: op done НЕМЕДЛЕННО, до всякого
// confirm owner-tuple (прежнее поведение). Документирует, что закрывает gate ON.
func TestCreateNetwork_OTG04_GateOFF_WindowPresent(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false) // без WithConfirmer

	op, err := uc.Execute(context.Background(), newNetworkForTest("net-otg04-off"))
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done, "без gate op done немедленно — owner-tuple мог быть ещё не виден (окно 403)")
	require.Nil(t, saved.Error)
}

// ---- OTG-05 (КРИТ) — confirm timeout → op.error Unavailable (fail-closed) ----

func TestCreateNetwork_OTG05_ConfirmTimeout_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	fc := newFakeConfirmer() // навсегда DENY (моделирует IAM/FGA outage)
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithConfirmer(fc)
	uc.dispatch = shortDeadlineDispatch(t, 300*time.Millisecond)

	op, err := uc.Execute(context.Background(), newNetworkForTest("net-otg05"))
	require.NoError(t, err) // sync-валидация прошла; ошибка приходит через Operation

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.NotNil(t, saved.Error, "confirm не достигнут за deadline → op.error (fail-closed)")
	assert.Equal(t, int32(codes.Unavailable), saved.Error.Code,
		"timeout-код = Unavailable (retryable, fail-closed; FIX-1)")
	assert.NotEqual(t, int32(codes.DeadlineExceeded), saved.Error.Code,
		"код НЕ DeadlineExceeded (FIX-1 — явное отклонение альтернативы)")
	assert.Equal(t, "owner-tuple registration not confirmed", saved.Error.Message,
		"стабильный текст — часть контракта (FIX-1)")
	assert.Nil(t, saved.Response, "invariant: success-response без confirm не выставляется НИКОГДА")

	// Ресурс-строка durable (не откачена timeout'ом) — worker-fn закоммитил ДО confirm.
	netID := networkIDFromOp(t, or, op.ID)
	require.True(t, networkDurable(t, kr, netID), "ресурс durable даже на timeout-ветке")
	require.GreaterOrEqual(t, len(kr.FGARegisterEvents()), 1, "register-intent durable (backstop drainer)")
}

// ---- OTG-05b (КРИТ, orphan-guard) — resource-ref обнаружим в op.metadata на error ----

func TestCreateNetwork_OTG05b_TimeoutResourceRefInMetadata(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	fc := newFakeConfirmer() // DENY forever
	uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithConfirmer(fc)
	uc.dispatch = shortDeadlineDispatch(t, 300*time.Millisecond)

	op, err := uc.Execute(context.Background(), newNetworkForTest("net-otg05b"))
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.NotNil(t, saved.Error, "timeout → op.error")
	assert.Equal(t, int32(codes.Unavailable), saved.Error.Code)

	// resource-ref доступен НА error-пути (metadata populated at op-creation).
	meta, merr := operations.MetadataFor[*vpcv1.CreateNetworkMetadata](saved)
	require.NoError(t, merr, "Create<Resource>Metadata читается на error-терминале")
	netID := meta.GetNetworkId()
	require.NotEmpty(t, netID, "op.metadata несёт network_id на всех терминалах, вкл. error (FIX-3)")

	// Get(<resource-ref>) → durable (ресурс не orphan-без-id): клиент повторяет
	// мутацию по этому id, НЕ пере-создаёт → orphan-дубль не возникает.
	require.True(t, networkDurable(t, kr, netID), "Get(resource-ref) → ресурс durable (200)")
}

// ---- OTG-13 (-race) — N конкурентных Create независимо gated, 0 ложных 403 ----

func TestCreateNetwork_OTG13_ConcurrentIndependentGating(t *testing.T) {
	const n = 20
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	confirmers := make([]*fakeConfirmer, n)
	ops := make([]*operations.Operation, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			fc := newFakeConfirmer() // индивидуальный DENY
			confirmers[i] = fc
			uc := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithConfirmer(fc)
			op, err := uc.Execute(context.Background(), domain.Network{
				ProjectID: "f1", Name: domain.RcNameVPC("net-otg13-" + string(rune('a'+i))),
			})
			require.NoError(t, err)
			ops[i] = op
		}(i)
	}
	wg.Wait()

	// Пока все confirmer'ы DENY — ни одна op не должна быть done (независимый gate).
	time.Sleep(120 * time.Millisecond)
	for i := 0; i < n; i++ {
		cur, _ := or.Get(context.Background(), ops[i].ID)
		require.False(t, cur.Done, "op %d done пока её confirmer DENY — ложное завершение", i)
	}

	// Поднимаем все флаги — каждая op независимо проходит confirm → done(success).
	for _, fc := range confirmers {
		fc.allow.Store(true)
	}
	for i := 0; i < n; i++ {
		saved := repomock.AwaitOpDone(t, or, ops[i].ID)
		require.True(t, saved.Done)
		require.Nil(t, saved.Error, "op %d: confirmed → success, ни одной ложной 403", i)
	}
}

// ---- OTG-16 — Update/Delete СУЩЕСТВУЮЩЕГО ресурса НЕ получают confirm-gate ----

func TestCreateNetwork_OTG16_UpdateExistingNotGated(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	fc := newFakeConfirmer()
	fc.allow.Store(true) // confirm сразу ALLOW — Create проходит быстро
	createUC := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or, false).WithConfirmer(fc)

	op, err := createUC.Execute(context.Background(), newNetworkForTest("net-otg16"))
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)

	netID := networkIDFromOp(t, or, op.ID)
	callsAfterCreate := fc.callCount()
	require.Positive(t, callsAfterCreate, "Create — gated (confirmer вызван)")

	// Update СУЩЕСТВУЮЩЕГО Network — отдельный use-case БЕЗ confirmer. confirm-порт
	// на Update-flow не вызывается (нового owner-tuple не создаётся, латентность не растёт).
	updateUC := NewUpdateNetworkUseCase(kr, or)
	upOp, err := updateUC.Execute(context.Background(), UpdateInput{
		NetworkID:  netID,
		Network:    domain.Network{Description: domain.RcDescription("upd")},
		UpdateMask: []string{"description"},
	})
	require.NoError(t, err)
	upSaved := repomock.AwaitOpDone(t, or, upOp.ID)
	require.True(t, upSaved.Done)
	require.Nil(t, upSaved.Error, "Update существующего ресурса завершается как сегодня, без gate")

	assert.Equal(t, callsAfterCreate, fc.callCount(),
		"на Update-flow read-after-register confirm-порт НЕ вызывается (OTG-16)")
}

// ---- helpers ----

func networkIDFromOp(t *testing.T, or *repomock.OpsRepo, opID string) string {
	t.Helper()
	op, err := or.Get(context.Background(), opID)
	require.NoError(t, err)
	meta, merr := operations.MetadataFor[*vpcv1.CreateNetworkMetadata](op)
	require.NoError(t, merr)
	return meta.GetNetworkId()
}

func networkDurable(t *testing.T, kr *kachomock.Repository, netID string) bool {
	t.Helper()
	rd, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	rec, gerr := rd.Networks().Get(context.Background(), netID)
	return gerr == nil && rec != nil
}
