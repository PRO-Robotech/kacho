// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

// owner-tuple opgate — RouteTable.Create confirm-gate: OTG-03/04/05/05b. RouteTable —
// non-opgated ресурс до этого коммита. Контракт confirm-gate идентичен Network/SG/Subnet
// (P3). (makeNetwork — из usecase_test.go этого же пакета.)

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

func routeTableCreateUC(t *testing.T) (*CreateRouteTableUseCase, *repomock.OpsRepo, *kachomock.Repository, string) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	net := makeNetwork(t, kr)
	uc := NewCreateRouteTableUseCase(kr, &repomock.ProjectClient{OK: true}, or)
	return uc, or, kr, net.ID
}

func validRouteTable(netID string) domain.RouteTable {
	return domain.RouteTable{ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("rt-otg")}
}

func routeTableDurable(t *testing.T, kr *kachomock.Repository, rtID string) bool {
	t.Helper()
	rd, err := kr.Reader(context.Background())
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	rec, gerr := rd.RouteTables().Get(context.Background(), rtID)
	return gerr == nil && rec != nil
}

// OTG-03 — op done только после confirm ALLOW (ordering).
func TestCreateRouteTable_OTG03_DoneOnlyAfterConfirm(t *testing.T) {
	uc, or, _, netID := routeTableCreateUC(t)
	fc := &fakeConfirmer{}
	uc.WithConfirmer(fc)

	op, err := uc.Execute(context.Background(), validRouteTable(netID))
	require.NoError(t, err)

	end := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(end) {
		cur, _ := or.Get(context.Background(), op.ID)
		require.False(t, cur.Done, "RouteTable op done пока confirmer DENY — окно 403 не закрыто")
		time.Sleep(5 * time.Millisecond)
	}
	require.GreaterOrEqual(t, fc.callCount(), 1)

	fc.allow.Store(true)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)
}

// OTG-04 — gate ON: op не done пока DENY (нет окна). N итераций.
func TestCreateRouteTable_OTG04_NoWindowGateON(t *testing.T) {
	for i := 0; i < 5; i++ {
		uc, or, _, netID := routeTableCreateUC(t)
		fc := &fakeConfirmer{}
		uc.WithConfirmer(fc)
		op, err := uc.Execute(context.Background(), validRouteTable(netID))
		require.NoError(t, err)

		end := time.Now().Add(50 * time.Millisecond)
		for time.Now().Before(end) {
			cur, _ := or.Get(context.Background(), op.ID)
			require.False(t, cur.Done, "iteration %d: RouteTable op done пока DENY", i)
			time.Sleep(5 * time.Millisecond)
		}
		fc.allow.Store(true)
		saved := repomock.AwaitOpDone(t, or, op.ID)
		require.True(t, saved.Done)
		require.Nil(t, saved.Error)
	}
}

// OTG-05 / 05b — confirm timeout → op.error Unavailable, точный текст; resource-ref
// в op.metadata; ресурс durable.
func TestCreateRouteTable_OTG05_ConfirmTimeout_FailClosed(t *testing.T) {
	uc, or, kr, netID := routeTableCreateUC(t)
	fc := &fakeConfirmer{} // DENY forever
	uc.WithConfirmer(fc)
	uc.dispatch = shortDeadlineDispatch(t, 300*time.Millisecond)

	op, err := uc.Execute(context.Background(), validRouteTable(netID))
	require.NoError(t, err)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.NotNil(t, saved.Error, "confirm timeout → op.error")
	assert.Equal(t, int32(codes.Unavailable), saved.Error.Code)
	assert.NotEqual(t, int32(codes.DeadlineExceeded), saved.Error.Code)
	assert.Equal(t, "owner-tuple registration not confirmed", saved.Error.Message)
	assert.Nil(t, saved.Response)

	meta, merr := operations.MetadataFor[*vpcv1.CreateRouteTableMetadata](saved)
	require.NoError(t, merr)
	rtID := meta.GetRouteTableId()
	require.NotEmpty(t, rtID, "op.metadata несёт route_table_id на error-терминале")
	require.True(t, routeTableDurable(t, kr, rtID), "RouteTable durable на timeout-ветке")
}
