// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestMove_EmitsExactlyOneRowAboutTheMovedBalancer — НА ОДИН ПЕРЕЕЗД ОДНА СТРОКА
// О ПЕРЕЕХАВШЕМ ПРЕДМЕТЕ.
//
// # Предмет
//
// Переезд эмитил о самом балансировщике ДВЕ строки подряд, в одной
// writer-транзакции: род `MOVED`, следом род `UPDATED`. Словарь родов журнала
// отображает ОБА слова в один род контракта (`SubscriptionEvent_UPDATED`), а
// нагрузку обе строки собирают ОДНИМ И ТЕМ ЖЕ строителем на ОДНОЙ И ТОЙ ЖЕ
// записи. То есть подписчик получал два события, различимых только позицией.
//
// # Почему второе не «безвредный повтор», а находка
//
// Довод в пользу пары был один: «для downstream watchers, не подписанных на
// MOVED». Подписки по роду изменения НЕ БЫВАЕТ — фильтр единой формы сужается по
// видам, проекту и идентификаторам, и только по ним. Значит второй род не
// добавляет ни одного получателя; он добавляет объём журнала, который не
// чистится, и второе событие, про которое подписчик обязан догадаться, что оно
// то же самое.
//
// Тот же вывод уже сделан ТРЕМЯ строками ниже в этом же файле — на слушателях,
// которых переезд объявляет каскадом: там парного `UPDATED` не шлётся намеренно,
// и причина названа дословно та же. Продукт противоречил сам себе в пределах
// одной функции, и различие никем не решалось.
//
// # Что именно утверждается
//
// Строка о переехавшем балансировщике РОВНО ОДНА · род её `MOVED` (слово
// хранилища, которое словарь отдаёт как правку — форма на проводе не меняется) ·
// она несёт конверт полного состояния · и якорь проекта в ней новый.
//
// Утверждение о числе стоит рядом с утверждением о содержимом намеренно: проба,
// требующая только «одна», зеленела бы на переезде, не объявившем НИЧЕГО, —
// ноль строк тоже не два.
func TestMove_EmitsExactlyOneRowAboutTheMovedBalancer(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")

	opsRepo := newFakeOpsRepo()
	uc := NewMoveLoadBalancerUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, slog.Default())
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	var own []outboxEvent
	for _, e := range repo.outboxEvents() {
		if e.ResourceType == kachorepo.OutboxResourceLoadBalancer {
			own = append(own, e)
		}
	}

	actions := make([]string, 0, len(own))
	for _, e := range own {
		actions = append(actions, e.Action)
	}
	require.Len(t, own, 1,
		"о переехавшем балансировщике объявлено %d строк (%v). Подписка сужается по видам, "+
			"проекту и идентификаторам, но НЕ по роду изменения, и словарь отдаёт оба слова "+
			"одним родом контракта — значит вторая строка не добавляет ни одного получателя, "+
			"а добавляет второе неотличимое событие и объём журнала, который не чистится",
		len(own), actions)

	require.Equal(t, kachorepo.OutboxActionMoved, own[0].Action,
		"оставлено слово, которое НЕ называет сделанного: переезд обязан читаться в журнале "+
			"как переезд, даже если словарь отдаёт его правкой")
	require.Equal(t, "prj-dst", own[0].ProjectID,
		"строка встала в СТАРЫЙ проект — подписчик назначения её не получит вовсе")

	// Состояние читается ЧИТАТЕЛЕМ ПРОИЗВОДИТЕЛЯ, а не своей копией разбора: у
	// строки журнала ключи имён КОЛОНОК, и вторая копия соответствия разошлась бы
	// с первой молча — на верном входе обе отвечают одинаково.
	raw, mErr := json.Marshal(own[0].Payload)
	require.NoError(t, mErr)
	state, sErr := kachorepo.LoadBalancerStateFromPayload(raw)
	require.NoError(t, sErr)
	require.NotNil(t, state,
		"единственная строка переезда не несёт конверта полного состояния. Вид "+
			"`nlb_load_balancer` объявлен несущим состояние, поэтому частичный снимок делает "+
			"ложным ВЕСЬ вид")
	require.Equal(t, "prj-dst", string(state.ProjectID),
		"состояние несёт СТАРЫЙ проект: подписчик запишет как факт ровно то, ради чего "+
			"событие и посылалось")
}
