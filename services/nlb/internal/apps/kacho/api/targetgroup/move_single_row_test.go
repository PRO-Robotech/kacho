// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestMove_EmitsExactlyOneRowAboutTheMovedGroup — НА ОДИН ПЕРЕЕЗД ОДНА СТРОКА О
// ПЕРЕЕХАВШЕМ ПРЕДМЕТЕ.
//
// # Предмет
//
// Тот же, что у балансировщика (см. одноимённую пробу в пакете `loadbalancer`), и
// здесь он ХУЖЕ: у этого вида состояние есть давно, поэтому подписчик получал два
// события с одинаковым родом контракта и ОДИНАКОВЫМ полным состоянием — то есть
// обязан был догадаться, что второе не несёт новости.
//
// Довод в пользу пары — «для downstream watchers, не подписанных на MOVED» —
// неисполним by construction: подписки по роду изменения не бывает, фильтр единой
// формы сужается по видам, проекту и идентификаторам.
//
// # Что именно утверждается
//
// Строка о переехавшей группе РОВНО ОДНА · род её `MOVED` · она несёт конверт
// полного состояния · якорь проекта в ней новый. Число утверждается ВМЕСТЕ с
// содержимым: проба, требующая только «одна», зеленела бы на переезде, не
// объявившем ничего.
func TestMove_EmitsExactlyOneRowAboutTheMovedGroup(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable-once")
	repo.seedTG(tg)

	opsRepo := newFakeOpsRepo()
	uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, nil)
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-dst",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	var own []fakeOutboxEvent
	for _, e := range repo.outboxEvents() {
		if e.ResourceType == kachorepo.OutboxResourceTargetGroup {
			own = append(own, e)
		}
	}

	actions := make([]string, 0, len(own))
	for _, e := range own {
		actions = append(actions, e.Action)
	}
	require.Len(t, own, 1,
		"о переехавшей группе объявлено %d строк (%v). Словарь отдаёт оба слова одним родом "+
			"контракта, нагрузку обе строки собирают одним строителем на одной записи — "+
			"значит вторая строка неотличима от первой ничем, кроме позиции",
		len(own), actions)

	require.Equal(t, kachorepo.OutboxActionMoved, own[0].Action,
		"оставлено слово, которое НЕ называет сделанного")
	require.Equal(t, "prj-dst", own[0].ProjectID,
		"строка встала в СТАРЫЙ проект — подписчик назначения её не получит вовсе")

	// Состояние читается ЧИТАТЕЛЕМ ПРОИЗВОДИТЕЛЯ, а не своей копией разбора.
	raw, mErr := json.Marshal(own[0].Payload)
	require.NoError(t, mErr)
	state, sErr := kachorepo.TargetGroupStateFromPayload(raw)
	require.NoError(t, sErr)
	require.NotNil(t, state,
		"единственная строка переезда не несёт конверта полного состояния — частичный снимок "+
			"делает ложным ВЕСЬ вид, и делает это тихо")
	require.Equal(t, "prj-dst", string(state.ProjectID),
		"состояние несёт СТАРЫЙ проект: подписчик запишет как факт ровно то, ради чего "+
			"событие и посылалось")
}
