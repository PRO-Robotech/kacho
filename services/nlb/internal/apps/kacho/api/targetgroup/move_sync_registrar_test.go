// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Тот же предмет, что и у балансировщика (см. loadbalancer/move_sync_registrar_test.go):
// Move — единственная мутация, которая СНОСИТ действующую проекцию прав и
// ставит новую, поэтому в окне материализации у ресурса нет проекции вообще, и
// владелец получает на собственный ресурс hide-existence `NotFound`. Ускоритель
// был провязан в Create и Update и отсутствовал ровно там, где окно опаснее
// всего. Радиус класса — ДВА глагола Move во всём дереве контрактов
// (`grep -n "rpc Move" proto/kacho` → balancer + target group), поэтому проба
// заводится у обоих: починка одного экземпляра оставила бы класс открытым.

func moveTGUCWithRegistrar(t *testing.T, reg Registrar) (*MoveTargetGroupUseCase, *fakeRepo, *fakeOpsRepo, string) {
	t.Helper()
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, nil)
	if reg != nil {
		uc = uc.WithRegistrar(reg)
	}
	return uc, repo, opsRepo, string(tg.ID)
}

func runTGMove(t *testing.T, uc *MoveTargetGroupUseCase, opsRepo *fakeOpsRepo, tgID string) {
	t.Helper()
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        tgID,
		DestinationProjectId: "prj-dst",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}

// TestMoveTG_SyncRegistrar_RestoresDestinationProjectionPostCommit — после
// durable commit'а Move синхронно доставляет проекцию НАЗНАЧЕНИЯ, и её версия
// строго больше версии снятия (иначе отзыв, доехавший дренажем позже, снимет
// её: отзыв в IAM гейтован `source_version <= tombstone`).
func TestMoveTG_SyncRegistrar_RestoresDestinationProjectionPostCommit(t *testing.T) {
	reg := &fakeSyncRegistrar{}
	uc, repo, opsRepo, tgID := moveTGUCWithRegistrar(t, reg)

	runTGMove(t, uc, opsRepo, tgID)

	calls := reg.calls()
	require.Len(t, calls, 1, "Move обязан синхронно доставить проекцию назначения ровно один раз")
	require.Equal(t, tgID, calls[0].ResourceID)
	require.Equal(t, "project:prj-dst", calls[0].Tuples[0].SubjectID,
		"доставляется проекция НАЗНАЧЕНИЯ, а не снятая проекция источника")

	versions := reg.versionsSeen()
	require.Len(t, versions, 1)
	require.False(t, versions[0].IsZero(), "версия обязана приехать из эмиттера, а не с часов доставки")

	require.Len(t, repo.fga, 2)
	require.Equal(t, domain.FGAEventUnregister, repo.fga[0].EventType)
	require.Equal(t, domain.FGAEventRegister, repo.fga[1].EventType)
	require.True(t, versions[0].After(repo.fga[0].StampedAt),
		"версия синхронно доставленной регистрации обязана быть строго больше версии снятия")
}

// TestMoveTG_SyncRegistrar_FailureDoesNotFailOperation — доставка BEST-EFFORT
// (ban #9): её отказ не роняет Operation, durable-intent остаётся backstop'ом.
func TestMoveTG_SyncRegistrar_FailureDoesNotFailOperation(t *testing.T) {
	reg := &fakeSyncRegistrar{err: errors.New("iam unavailable")}
	uc, repo, opsRepo, tgID := moveTGUCWithRegistrar(t, reg)

	runTGMove(t, uc, opsRepo, tgID)

	require.Equal(t, domain.ProjectID("prj-dst"), repo.tgs[tgID].ProjectID)
	require.Len(t, repo.fga, 2, "durable-intent'ы остаются backstop'ом дренажа")
	require.Len(t, reg.calls(), 1)
}

// TestMoveTG_NilRegistrar_StillMoves — положительный контроль: без ускорителя
// Move обязан работать по-прежнему. Без него «доставка не вызвана» было бы
// неотличимо от «сломан весь Move».
func TestMoveTG_NilRegistrar_StillMoves(t *testing.T) {
	uc, repo, opsRepo, tgID := moveTGUCWithRegistrar(t, nil)

	runTGMove(t, uc, opsRepo, tgID)

	require.Equal(t, domain.ProjectID("prj-dst"), repo.tgs[tgID].ProjectID)
	require.Len(t, repo.fga, 2)
}
