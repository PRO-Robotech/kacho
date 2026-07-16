// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

// owner-tuple opgate (P3) — SecurityGroup.Create confirm-gate: OTG-03/04/05/05b.
// SecurityGroup — второй owner-ресурс матрицы vpc; confirm-gate идентичен Network
// (общий confirmDispatcher поверх RunWithConfirm). Здесь фиксируем core-контракт:
// op done только после confirm, fail-closed Unavailable по deadline, resource-ref
// в op.metadata на error.

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
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

type fakeConfirmer struct {
	allow atomic.Bool
	mu    sync.Mutex
	calls int
}

func (f *fakeConfirmer) Confirm(_ context.Context, _ operations.Principal, _ string) (bool, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.allow.Load(), nil
}

func (f *fakeConfirmer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

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

func sgCreateUC(t *testing.T) (*CreateSecurityGroupUseCase, *repomock.OpsRepo, *kachomock.Repository, string) {
	t.Helper()
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netID, ProjectID: "f1", Name: domain.RcNameVPC("net")})
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)
	return uc, or, sgr, netID
}

func validSG(netID string) domain.SecurityGroup {
	return domain.SecurityGroup{ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("sg-otg")}
}

func sgDurable(t *testing.T, sgr *kachomock.Repository, sgID string) bool {
	t.Helper()
	rd, err := sgr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	rec, gerr := rd.SecurityGroups().Get(context.Background(), sgID)
	return gerr == nil && rec != nil
}

// OTG-03 — op done только после confirm ALLOW (ordering).
func TestCreateSG_OTG03_DoneOnlyAfterConfirm(t *testing.T) {
	uc, or, _, netID := sgCreateUC(t)
	fc := &fakeConfirmer{}
	uc.WithConfirmer(fc)

	op, err := uc.Execute(context.Background(), validSG(netID))
	require.NoError(t, err)

	// Пока confirmer DENY — op не done.
	deadline := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(deadline) {
		cur, _ := or.Get(context.Background(), op.ID)
		require.False(t, cur.Done, "SG op done пока confirmer DENY — окно 403 не закрыто")
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, fc.callCount(), 1)

	fc.allow.Store(true)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)
}

// OTG-04 — gate ON: op не done пока DENY (нет окна). N итераций.
func TestCreateSG_OTG04_NoWindowGateON(t *testing.T) {
	for i := 0; i < 5; i++ {
		uc, or, _, netID := sgCreateUC(t)
		fc := &fakeConfirmer{}
		uc.WithConfirmer(fc)
		op, err := uc.Execute(context.Background(), validSG(netID))
		require.NoError(t, err)

		end := time.Now().Add(50 * time.Millisecond)
		for time.Now().Before(end) {
			cur, _ := or.Get(context.Background(), op.ID)
			require.False(t, cur.Done, "iteration %d: SG op done пока DENY", i)
			time.Sleep(5 * time.Millisecond)
		}
		fc.allow.Store(true)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.True(t, saved.Done)
		require.Nil(t, saved.Error)
	}
}

// OTG-05 — confirm timeout → op.error Unavailable, точный текст, ресурс durable.
func TestCreateSG_OTG05_ConfirmTimeout_FailClosed(t *testing.T) {
	uc, or, sgr, netID := sgCreateUC(t)
	fc := &fakeConfirmer{} // DENY forever
	uc.WithConfirmer(fc)
	uc.dispatch = shortDeadlineDispatch(t, 300*time.Millisecond)

	op, err := uc.Execute(context.Background(), validSG(netID))
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.NotNil(t, saved.Error, "confirm timeout → op.error")
	assert.Equal(t, int32(codes.Unavailable), saved.Error.Code)
	assert.NotEqual(t, int32(codes.DeadlineExceeded), saved.Error.Code)
	assert.Equal(t, "owner-tuple registration not confirmed", saved.Error.Message)
	assert.Nil(t, saved.Response, "success-response без confirm не выставляется")

	// OTG-05b — resource-ref в op.metadata на error; ресурс durable.
	meta, merr := operations.MetadataFor[*vpcv1.CreateSecurityGroupMetadata](saved)
	require.NoError(t, merr)
	sgID := meta.GetSecurityGroupId()
	require.NotEmpty(t, sgID, "op.metadata несёт security_group_id на error-терминале (FIX-3)")
	require.True(t, sgDurable(t, sgr, sgID), "SG durable на timeout-ветке")
}
