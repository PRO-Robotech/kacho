// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestMove_EmitsAListenerRowForEveryCascadedListener — переезд объявляет ТО, ЧТО ОН СДЕЛАЛ.
//
// # Предмет
//
// `LoadBalancer.Move` в одной writer-транзакции каскадом переписывает
// `project_id` у ВСЕХ слушателей балансировщика. В журнал при этом уходили две
// строки, и обе вида `nlb_load_balancer`; строк вида `nlb_listener` не уходило ни
// одной.
//
// Следствие у подписчика: якорь проекта у слушателя сменился, а событие об этом
// не пришло. Поток не замолкает и не отказывает — он просто не говорит, и
// отличить это от «изменений не было» нечем. С тех пор как событие вида
// `nlb_listener` несёт ПОЛНОЕ состояние, цена этого молчания стала прямой: в
// состоянии подписчика лежит слушатель с неверным проектом (#1549).
//
// # Что именно утверждается
//
// Строка на КАЖДЫЙ переехавший слушатель · в той же транзакции · с НОВЫМ якорем
// проекта в колонке · и с полным состоянием под конвертом, где проект тоже
// новый. Последнее — не педантизм: вид `nlb_listener` объявлен несущим состояние,
// поэтому строка с частичным снимком сделала бы ложным ВЕСЬ вид, а не только
// себя.
func TestMove_EmitsAListenerRowForEveryCascadedListener(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-src", "edge")
	first := domain.ResourceID(ids.NewID(ids.PrefixListener))
	second := domain.ResourceID(ids.NewID(ids.PrefixListener))
	repo.lists[lbID] = []*kachorepo.ListenerRecord{
		{Listener: domain.Listener{
			ID: first, LoadBalancerID: domain.ResourceID(lbID),
			ProjectID: domain.ProjectID("prj-src"), Name: domain.LbName("front"),
			Labels:   domain.LabelsFromMap(map[string]string{"env": "prod"}),
			Protocol: domain.ProtoTCP, Port: domain.LbPort(443),
			Status: domain.ListenerStatusActive,
		}},
		{Listener: domain.Listener{
			ID: second, LoadBalancerID: domain.ResourceID(lbID),
			ProjectID: domain.ProjectID("prj-src"), Name: domain.LbName("back"),
			Protocol: domain.ProtoTCP, Port: domain.LbPort(80),
			Status: domain.ListenerStatusActive,
		}},
	}

	opsRepo := newFakeOpsRepo()
	uc := NewMoveLoadBalancerUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, slog.Default())
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DestinationProjectId:  "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)

	byID := map[string]outboxEvent{}
	for _, e := range repo.outboxEvents() {
		if e.ResourceType == "nlb_listener" {
			byID[e.ResourceID] = e
		}
	}
	require.Len(t, byID, 2,
		"переезд каскадом переписал проект у обоих слушателей и не объявил этого ни одной "+
			"строкой: подписчик держит их в СТАРОМ проекте бессрочно, и отличить это от "+
			"«изменений не было» ему нечем")

	for _, id := range []domain.ResourceID{first, second} {
		ev, ok := byID[string(id)]
		require.True(t, ok, "слушатель %s переехал, а строки о нём нет", id)
		require.Equal(t, "prj-dst", ev.ProjectID,
			"строка встала в СТАРЫЙ проект — подписчик назначения её не получит вовсе")

		raw, mErr := json.Marshal(ev.Payload)
		require.NoError(t, mErr)
		var wire struct {
			State *kachorepo.ListenerRecord `json:"state"`
		}
		require.NoError(t, json.Unmarshal(raw, &wire))
		require.NotNil(t, wire.State,
			"строка не несёт конверта полного состояния. Вид `nlb_listener` объявлен несущим "+
				"состояние, поэтому одна частичная строка делает ложным ВЕСЬ вид — и делает "+
				"это тихо")
		require.Equal(t, "prj-dst", string(wire.State.ProjectID),
			"состояние несёт СТАРЫЙ проект: подписчик запишет как факт ровно то, ради чего "+
				"событие и посылалось")
	}
}
