// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Move — ЕДИНСТВЕННАЯ мутация, которая СНОСИТ действующую проекцию прав и
// ставит новую. Create только добавляет (терять нечего), Update обновляет метки
// у живой проекции. У Move между unregister(src) и register(dst) проекции нет
// ВООБЩЕ, и всё это окно край не резолвит цель проверки прав в проект: владелец
// получает hide-existence `NotFound` — побайтово тот же текст, что у настоящего
// «не найдено» (`security.md` §6), то есть собственный ресурс выглядит
// исчезнувшим.
//
// Ускоритель (sync-registrar) был провязан в Create и Update и НЕ был провязан
// в Move — то есть ровно там, где окно самое опасное, его закрывал только
// дренаж. Наблюдалось на стволе: после Move и Move-назад уборка получала
// `404 "NetworkLoadBalancer <id> not found"`, а опрос операции — 403
// `no authorization path to the resource`.
//
// Пробы ниже держат ТРИ разных утверждения, и ни одно не выводится из
// остальных: что доставка вообще происходит; что она несёт ВЫЖИВАЮЩЕЕ состояние
// (назначение), а не снятое; и что её версия строго больше версии снятия —
// именно это делает ранний синхронный register безопасным, потому что отзыв в
// IAM гейтован `source_version <= tombstone` и более старым снятием более новую
// запись не убрать.

func moveUCWithRegistrar(t *testing.T, reg Registrar) (*MoveLoadBalancerUseCase, *fakeRepo, *fakeOpsRepo, string) {
	t.Helper()
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	lbID := seedLB(t, repo, "prj-src", "movable")
	uc := NewMoveLoadBalancerUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, slog.Default())
	if reg != nil {
		uc = uc.WithRegistrar(reg)
	}
	return uc, repo, opsRepo, lbID
}

func runMove(t *testing.T, uc *MoveLoadBalancerUseCase, opsRepo *fakeOpsRepo, lbID string) {
	t.Helper()
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}

// TestMove_SyncRegistrar_RestoresDestinationProjectionPostCommit — после
// durable commit'а Move синхронно доставляет проекцию НАЗНАЧЕНИЯ.
func TestMove_SyncRegistrar_RestoresDestinationProjectionPostCommit(t *testing.T) {
	t.Parallel()
	reg := &fakeSyncRegistrar{}
	uc, repo, opsRepo, lbID := moveUCWithRegistrar(t, reg)

	runMove(t, uc, opsRepo, lbID)
	require.Equal(t, domain.ProjectID("prj-dst"), repo.lbs[lbID].ProjectID)

	calls := reg.calls()
	require.Len(t, calls, 1,
		"Move обязан синхронно доставить проекцию назначения ровно один раз")
	require.Equal(t, lbID, calls[0].ResourceID)

	// Доставляется ВЫЖИВАЮЩЕЕ состояние — назначение. Доставка снятия здесь
	// была бы не «лишним вызовом», а обратной по смыслу: она бы гасила
	// проекцию вместо того, чтобы её восстанавливать.
	require.Equal(t, "project:prj-dst", calls[0].Tuples[0].SubjectID,
		"синхронно доставляется проекция НАЗНАЧЕНИЯ, а не снятая проекция источника")

	// Версия — та, что ушла в outbox, и она НЕ нулевая: общая форма доставки
	// регистрацию без маркера версии отвергает, поэтому нулевая версия означала
	// бы зелёное утверждение на мёртвом пути.
	versions := reg.versionsSeen()
	require.Len(t, versions, 1)
	require.False(t, versions[0].IsZero(), "версия обязана приехать из эмиттера, а не с часов доставки")

	// Ранняя синхронная доставка безопасна ТОЛЬКО потому, что её версия строго
	// больше версии снятия: доехавший позже дренажем unregister(src) гейтован
	// `source_version <= tombstone` и снять её не может. Сравниваем с версией,
	// которую эмиттер выдал снятию в этой же транзакции.
	require.Len(t, repo.fga, 2, "в транзакции обязаны стоять unregister(src) и register(dst)")
	require.Equal(t, domain.FGAEventUnregister, repo.fga[0].EventType)
	require.Equal(t, domain.FGAEventRegister, repo.fga[1].EventType)
	require.True(t, versions[0].After(repo.fga[0].StampedAt),
		"версия синхронно доставленной регистрации обязана быть строго больше версии снятия, "+
			"иначе отзыв, доехавший позже, снимет её")
}

// TestMove_SyncRegistrar_FailureDoesNotFailOperation — доставка BEST-EFFORT:
// её отказ логируется и глотается, потому что durable-intent в
// `fga_register_outbox` + дренаж остаются at-least-once backstop'ом, а
// `Operation.done` не гейтится на видимость (ban #9 — иначе phantom-ресурс).
func TestMove_SyncRegistrar_FailureDoesNotFailOperation(t *testing.T) {
	t.Parallel()
	reg := &fakeSyncRegistrar{err: errors.New("iam unavailable")}
	uc, repo, opsRepo, lbID := moveUCWithRegistrar(t, reg)

	runMove(t, uc, opsRepo, lbID)

	require.Equal(t, domain.ProjectID("prj-dst"), repo.lbs[lbID].ProjectID,
		"перенос durable независимо от исхода синхронной доставки")
	require.Len(t, repo.fga, 2, "durable-intent'ы остаются backstop'ом дренажа")
	require.Len(t, reg.calls(), 1)
}

// TestMove_NilRegistrar_StillMoves — положительный контроль к обеим пробам
// выше: без ускорителя Move обязан работать по-прежнему (дренаж — единственный
// путь). Без этого контроля «доставка не вызвана» было бы неотличимо от
// «сломан весь Move».
func TestMove_NilRegistrar_StillMoves(t *testing.T) {
	t.Parallel()
	uc, repo, opsRepo, lbID := moveUCWithRegistrar(t, nil)

	runMove(t, uc, opsRepo, lbID)

	require.Equal(t, domain.ProjectID("prj-dst"), repo.lbs[lbID].ProjectID)
	require.Len(t, repo.fga, 2)
}
