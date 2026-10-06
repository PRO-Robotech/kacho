// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/subscription"
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestDeleteListener_LBUpdatedOutbox — Delete эмитит nlb_load_balancer:<id>
// UPDATED для пересчёта LB.status. Листенер собственный VIP не освобождает —
// адрес принадлежит родительскому LB (release живёт в LoadBalancer.Delete /
// free_ip_runner).
func TestDeleteListener_LBUpdatedOutbox(t *testing.T) {
	t.Parallel()
	suite := newDeleteSuite(t)
	suite.repo.seedListener(suite.listener)
	op, err := suite.uc.Run(context.Background(), &lbv1.DeleteListenerRequest{
		ListenerId: string(suite.listener.ID),
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	events := suite.repo.pendingOutbox()
	hasLBUpd := false
	for _, e := range events {
		if e.ResourceType == "nlb_load_balancer" && e.Action == kachorepo.OutboxActionUpdated {
			hasLBUpd = true
			break
		}
	}
	require.True(t, hasLBUpd, "nlb_load_balancer:<id> UPDATED must be emitted on Listener.Delete")
}

// TestDeleteListener_RowAlreadyGone_NoSecondDeletion — повторный воркер снятия
// застаёт строку уже снятой: её снял единственный удаляющий путь (этот же
// use-case), и снятие объявлено строкой DELETED в ТОЙ ЖЕ транзакции, что
// удаление. Второй строки DELETED не пишется — имени предмета на момент снятия
// у проигравшего нет (RETURNING ничего не вернул), а выдуманное из снимка
// чтения уехало бы подписчику как факт; вид именованный, и функция фундамента
// снятие без имени отвергла бы, уронив повторный Delete кодом Internal.
func TestDeleteListener_RowAlreadyGone_NoSecondDeletion(t *testing.T) {
	t.Parallel()
	suite := newDeleteSuite(t)
	snap := *suite.listener
	snap.Status = domain.ListenerStatusDeleting
	suite.repo.mu.Lock()
	delete(suite.repo.listeners, string(snap.ID))
	suite.repo.mu.Unlock()

	_, err := suite.uc.doDelete(context.Background(), &snap)
	require.NoError(t, err, "повторное снятие идемпотентно")

	for _, e := range suite.repo.pendingOutbox() {
		require.Falsef(t, e.ResourceType == kachorepo.OutboxResourceListener && e.Action == kachorepo.OutboxActionDeleted,
			"строка DELETED слушателя на повторном снятии: %+v", e)
	}
}

// TestDeleteListener_DeletedRowCarriesTheNameFromTheDelete — строка DELETED
// несёт имя под ключом фундамента, взятое из удаляющего оператора, а не из
// снимка чтения до него.
func TestDeleteListener_DeletedRowCarriesTheNameFromTheDelete(t *testing.T) {
	t.Parallel()
	suite := newDeleteSuite(t)
	snap := *suite.listener
	suite.repo.mu.Lock()
	suite.repo.listeners[string(snap.ID)].Name = domain.LbName("renamed")
	suite.repo.mu.Unlock()

	_, err := suite.uc.doDelete(context.Background(), &snap)
	require.NoError(t, err)

	var got []any
	for _, e := range suite.repo.pendingOutbox() {
		if e.ResourceType == kachorepo.OutboxResourceListener && e.Action == kachorepo.OutboxActionDeleted {
			got = append(got, e.Payload[subscription.NamePayloadKey])
		}
	}
	require.Equal(t, []any{"renamed"}, got, "имя в строке DELETED")
}

// TestDeleteListener_NotFound — listener_id doesn't exist → sync NotFound.
func TestDeleteListener_NotFound(t *testing.T) {
	t.Parallel()
	suite := newDeleteSuite(t)
	_, err := suite.uc.Run(context.Background(), &lbv1.DeleteListenerRequest{
		ListenerId: "lstNOTEXISTS0000001",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}

// TestDeleteListener_EmptyID — sync InvalidArgument.
func TestDeleteListener_EmptyID(t *testing.T) {
	t.Parallel()
	suite := newDeleteSuite(t)
	_, err := suite.uc.Run(context.Background(), &lbv1.DeleteListenerRequest{ListenerId: ""})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// ---- shared helpers ----

type deleteSuite struct {
	t        *testing.T
	repo     *fakeRepo
	ops      *fakeOpsRepo
	listener *kachorepo.ListenerRecord
	uc       *DeleteUseCase
}

func newDeleteSuite(t *testing.T) *deleteSuite {
	t.Helper()
	repo := newFakeRepo()
	lb := newRecordLB(t, "prj01TESTPROJ0000001", "ru-central1", domain.LBTypeExternal, "parent-lb")
	repo.seedLB(lb)
	listener := &kachorepo.ListenerRecord{
		Listener: domain.Listener{
			ID:             domain.ResourceID(ids.NewID(ids.PrefixListener)),
			ProjectID:      lb.ProjectID,
			LoadBalancerID: lb.ID,
			RegionID:       lb.RegionID,
			Name:           domain.LbName("doomed"),
			Labels:         domain.LbLabels{},
			Protocol:       domain.ProtoTCP,
			Port:           443,
			Status:         domain.ListenerStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	repo.seedListener(listener)
	ops := newFakeOpsRepo()
	uc := NewDeleteUseCase(repo, ops, slog.Default())
	return &deleteSuite{
		t:        t,
		repo:     repo,
		ops:      ops,
		listener: listener,
		uc:       uc,
	}
}

// _ — sentinel for errors import (used indirectly elsewhere).
var _ = errors.Is
