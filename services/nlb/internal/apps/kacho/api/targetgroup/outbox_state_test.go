// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// TestEveryMutationEmitsTheWholeGroupUnderTheStateEnvelope — вид
// `nlb_target_group` несёт ПОЛНОЕ состояние на КАЖДОЙ точке эмиссии.
//
// # Предмет
//
// Контракт единой формы подписки разрешает подписчику читать непустое состояние
// события как ПОЛНОЕ состояние предмета. Значит обогащение вида —
// ВСЁ-ИЛИ-НИЧЕГО: одна точка эмиссии, положившая частичный снимок, делает ложным
// весь вид, и делает это тихо.
//
// Точек у группы семь: создание · правка · снятие · переезд (две) · добавление
// целей · снятие целей. Снятие состояния не несёт by construction — предмета
// больше нет, — остальные обязаны нести.
//
// # Почему цели названы ОТДЕЛЬНО
//
// Публичная проекция группы строится НЕ из её строки, а из набора целей С
// СОСТОЯНИЕМ (`TargetStates`). Путь записи, заполнивший одну лишь строку, отдаёт
// группу с ПУСТЫМ набором целей — и это худший вид неполноты: пустой массив
// читается подписчиком как «целей нет», а не как «это событие поле не
// заполняет», и клиент, ведущий состояние, предложит создать их заново.
func TestEveryMutationEmitsTheWholeGroupUnderTheStateEnvelope(t *testing.T) {
	type emission struct {
		name string
		slug string
		run  func(t *testing.T, repo *fakeRepo, tgID string)
	}
	cases := []emission{
		{"правка", "update", func(t *testing.T, repo *fakeRepo, tgID string) {
			opsRepo := newFakeOpsRepo()
			uc := NewUpdateTargetGroupUseCase(repo, opsRepo, nil)
			op, err := uc.Execute(context.Background(), &lbv1.UpdateTargetGroupRequest{
				TargetGroupId: tgID,
				UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
				Labels:        map[string]string{"tier": "critical"},
			})
			require.NoError(t, err)
			require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		}},
		{"переезд", "move", func(t *testing.T, repo *fakeRepo, tgID string) {
			opsRepo := newFakeOpsRepo()
			uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{},
				&fakeCheckClient{allowed: true}, nil)
			op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
				TargetGroupId:        tgID,
				DestinationProjectId: "prj-dst",
			})
			require.NoError(t, err)
			require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		}},
		{"добавление целей", "add", func(t *testing.T, repo *fakeRepo, tgID string) {
			opsRepo := newFakeOpsRepo()
			uc := mkAddUC(repo, opsRepo)
			op, err := uc.Execute(context.Background(), &lbv1.AddTargetsRequest{
				TargetGroupId: tgID,
				Targets: []*lbv1.Target{
					{Identity: &lbv1.Target_ExternalIp{ExternalIp: &lbv1.Target_ExternalIP{
						Address: "203.0.113.7",
					}}, Weight: 10},
				},
			})
			require.NoError(t, err)
			require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		}},
		{"снятие целей", "remove", func(t *testing.T, repo *fakeRepo, tgID string) {
			opsRepo := newFakeOpsRepo()
			uc := NewRemoveTargetsUseCase(repo, opsRepo, nil)
			op, err := uc.Execute(context.Background(), &lbv1.RemoveTargetsRequest{
				TargetGroupId: tgID,
				Targets: []*lbv1.Target{
					{Identity: &lbv1.Target_ExternalIp{ExternalIp: &lbv1.Target_ExternalIP{
						Address: "203.0.113.99",
					}}, Weight: 50},
				},
			})
			require.NoError(t, err)
			require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := newFakeRepo()
			tg := makeTG("prj-acme", "state-"+c.slug)
			tg.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
			repo.seedTG(tg)
			// Две цели, чтобы неполнота набора была отличима от его отсутствия.
			keep := kachoTarget(string(tg.ID), domain.Target{
				ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.10"}, Weight: 100,
			})
			drop := kachoTarget(string(tg.ID), domain.Target{
				ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.99"}, Weight: 50,
			})
			repo.seedTarget(string(tg.ID), &keep)
			repo.seedTarget(string(tg.ID), &drop)

			c.run(t, repo, string(tg.ID))

			var rows []fakeOutboxEvent
			for _, e := range repo.outboxEvents() {
				if e.ResourceType == "nlb_target_group" && e.Action != "DELETED" {
					rows = append(rows, e)
				}
			}
			require.NotEmpty(t, rows, "путь не эмитил ни одной строки своего вида")

			for _, ev := range rows {
				raw, err := json.Marshal(ev.Payload)
				require.NoError(t, err)
				var wire struct {
					State *kachorepo.TargetGroupRecord `json:"state"`
				}
				require.NoError(t, json.Unmarshal(raw, &wire))
				require.NotNil(t, wire.State,
					"род %q: нагрузка не несёт конверта полного состояния. Вид объявлен несущим "+
						"состояние, поэтому одна частичная строка делает ложным ВЕСЬ вид — и "+
						"делает это тихо", ev.Action)
				require.NotEmpty(t, string(wire.State.ProjectID),
					"род %q: у состояния нет якоря проекта — предмет разобрался и оказался "+
						"ложным ровно тем способом, ради которого полноту объявляет конверт",
					ev.Action)
				require.NotEmpty(t, wire.State.TargetStates,
					"род %q: состояние несёт ПУСТОЙ набор целей. Публичная проекция группы "+
						"строится из набора С СОСТОЯНИЕМ, поэтому подписчик прочтёт это как "+
						"«целей нет» и предложит создать их заново", ev.Action)
				for _, ts := range wire.State.TargetStates {
					require.NotEmpty(t, ts.Status,
						"род %q: у цели нет lifecycle-состояния — снятая цель неотличима от "+
							"обычной, а именно ради этого различия проекция и строится из "+
							"набора С состоянием", ev.Action)
				}
			}
		})
	}
}
