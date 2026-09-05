// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Move OK (no attached LB).
func TestMove_Happy(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, nil)

	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)

	// Здесь утверждалась ПАРА `MOVED` + `UPDATED` — то есть проба закрепляла
	// дефект #1565: второе событие не добавляло получателей (подписки по роду
	// изменения не бывает) и было неотличимо от первого. Утверждение ЗАМЕНЕНО, а
	// не ослаблено: теперь оно про число строк, а не про их перечень.
	events := repo.outboxEvents()
	require.Len(t, events, 1,
		"переезд объявляет о переехавшей группе РОВНО ОДНУ строку")
	assert.Equal(t, kachorepo.OutboxActionMoved, events[0].Action,
		"слово хранилища обязано называть сделанное — словарь отдаёт его правкой")

	// project-rewrite = unregister(src) THEN register(dst) in the writer-tx.
	// The order is the contract, not the style: both intents are about the same
	// object, so they drain in emission order and the SURVIVING state must be
	// emitted LAST. The end-to-end consequence is locked in
	// move_mirror_projection_integration_test.go.
	require.Len(t, repo.fga, 2)
	assert.Equal(t, domain.FGAEventUnregister, repo.fga[0].EventType,
		"the source scope comes down FIRST")
	assert.Equal(t, "project:prj-src", repo.fga[0].Intent.Tuples[0].SubjectID)
	assert.Equal(t, domain.FGAEventRegister, repo.fga[1].EventType,
		"the destination scope goes up LAST — it is the state that must survive")
	assert.Equal(t, "project:prj-dst", repo.fga[1].Intent.Tuples[0].SubjectID)
}

// TestMove_RegisterDstCarriesLabelsAndParent — regression: the register(dst)
// FGA intent must mirror tgMirrorIntent semantics (Labels from the moved record +
// ParentProjectID=dst), NOT reuse the bare tgUnregisterIntent (which drops both).
// Previously Move emitted register(dst) with Labels=nil / ParentProjectID="",
// wiping the kaname resource_mirror row feeding the γ label/parent selector →
// label-based grants and parent-scoped queries silently excluded the moved TG in
// the destination project until an unrelated Update repaired the mirror.
func TestMove_RegisterDstCarriesLabelsAndParent(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	tg.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	repo.seedTG(tg)
	opsRepo := newFakeOpsRepo()
	uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, nil)

	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)
	require.Len(t, repo.fga, 2)

	// unregister(src) stays bare (IAM uses only object+source_version on unregister).
	unreg := repo.fga[0]
	require.Equal(t, domain.FGAEventUnregister, unreg.EventType)
	require.Equal(t, "project:prj-src", unreg.Intent.Tuples[0].SubjectID)
	require.Empty(t, unreg.Intent.ParentProjectID)
	require.Nil(t, unreg.Intent.Labels)

	// register(dst) must carry the mirror fields for the destination.
	reg := repo.fga[1]
	require.Equal(t, domain.FGAEventRegister, reg.EventType)
	require.Equal(t, "prj-dst", reg.Intent.ParentProjectID,
		"register(dst) must set ParentProjectID=dst for the γ parent selector")
	require.Equal(t, map[string]string{"env": "prod"}, reg.Intent.Labels,
		"register(dst) must carry the moved TG's labels for the γ label selector")
}

// Same-project destination → InvalidArgument с фиксированным текстом.
func TestMove_SameProject_InvalidArg(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-x", "same-proj")
	repo.seedTG(tg)
	uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)

	_, err := uc.Execute(context.Background(), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-x",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "destination project is the same as source")
}

// referenced by a listener → FailedPrecondition с фиксированным текстом (NLB
// CONTRACT: association LB↔TG derives from listeners.default_target_group_id;
// a referenced TG cannot be moved cross-project — repoint the listeners first).
func TestMove_ReferencedByListener(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-y", "referenced")
	repo.seedTG(tg)
	repo.seedReferencingListener(string(tg.ID), "lst-7h3k9m2x4q8w1t0y")
	uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)

	_, err := uc.Execute(context.Background(), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-z",
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(),
		"target group is referenced by 1 listener(s); repoint them before moving")
}

// Destination project peer NotFound → InvalidArgument with с фиксированным текстом.
func TestMove_DestProjectNotFound(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "peer-nf")
	repo.seedTG(tg)
	uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(),
		&fakeProjectClient{getFunc: func(_ context.Context, id string) (*iam.Project, error) {
			return nil, projectNotFound(id)
		}}, nil, nil)

	_, err := uc.Execute(context.Background(), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-doesnt-exist",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "Project prj-doesnt-exist not found")
}

func TestMove_MissingFields(t *testing.T) {
	uc := NewMoveTargetGroupUseCase(newFakeRepo(), newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)
	for _, tc := range []struct {
		name string
		req  *lbv1.MoveTargetGroupRequest
	}{
		{"no id", &lbv1.MoveTargetGroupRequest{DestinationProjectId: "p"}},
		{"no dst", &lbv1.MoveTargetGroupRequest{TargetGroupId: "tgr-x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Execute(context.Background(), tc.req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestMove_NotFound(t *testing.T) {
	uc := NewMoveTargetGroupUseCase(newFakeRepo(), newFakeOpsRepo(), &fakeProjectClient{}, nil, nil)
	_, err := uc.Execute(context.Background(), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        "tgr-missing",
		DestinationProjectId: "prj-dst",
	})
	require.Equal(t, codes.NotFound, status.Code(err))
}

// SECURITY (CWE-862/863): the caller must be authorized on
// the DESTINATION project (editor on project:<dst>). A caller with editor on the
// source TG but NO grant on the destination must be denied, else it injects its
// TG into a victim's project. Deny → PermissionDenied and the TG must NOT move.
func TestMove_DeniesUnauthorizedDestination(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	repo.seedTG(tg)
	chk := &fakeCheckClient{allowed: false}
	uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, chk, nil)

	_, err := uc.Execute(ctxWithUser("usr_attacker"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-victim",
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, domain.ProjectID("prj-src"), repo.tgs[string(tg.ID)].ProjectID,
		"TG must not be re-parented when dst authz is denied")
	require.Equal(t, 1, chk.calls)
	require.Equal(t, "user:usr_attacker", chk.gotSubject)
	require.Equal(t, domain.FGARelationEditor, chk.gotRelation)
	require.Equal(t, "project:prj-victim", chk.gotObject)
}

// Authorized (editor) on the destination → Move proceeds.
func TestMove_AllowsAuthorizedDestination(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	repo.seedTG(tg)
	chk := &fakeCheckClient{allowed: true}
	opsRepo := newFakeOpsRepo()
	uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{}, chk, nil)

	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-dst",
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)
	require.Equal(t, domain.ProjectID("prj-dst"), repo.tgs[string(tg.ID)].ProjectID)
	require.Equal(t, 1, chk.calls)
}

// IAM unavailable during the dst-authz check → fail-closed Unavailable.
func TestMove_DestCheckUnavailableFailsClosed(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "movable")
	repo.seedTG(tg)
	// CheckClient contract: transport-unavailable surfaces as domain.ErrUnavailable.
	chk := &fakeCheckClient{err: domain.ErrUnavailable}
	uc := NewMoveTargetGroupUseCase(repo, newFakeOpsRepo(), &fakeProjectClient{}, chk, nil)

	_, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-dst",
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, domain.ProjectID("prj-src"), repo.tgs[string(tg.ID)].ProjectID)
}

// TestMovePayloadIsTheWholeGroupOnTheWire — нагрузка переезда несёт КОНВЕРТ
// полного состояния и не несёт ключей прежнего минимального снимка.
//
// Здесь стояла проба `TestTgMovedPayload_KeysOnTheWire`: она сверяла, что
// исходный и целевой проекты лежат под именами `old_project_id`/`new_project_id`.
// Предмет её исчез вместе со строителем — форма нагрузки вида ЗАМЕНЕНА, а не
// дополнена: вид `nlb_target_group` несёт теперь полное состояние, и строитель у
// него один на все точки эмиссии. Ослабить пробу было нельзя, поэтому она
// ЗАМЕНЕНА утверждением о новой форме.
//
// Утверждение сделано ПО ПРОВОДУ — через настоящий JSON, а не через разборщик,
// собранный из тех же констант: круговой ход был бы истинен при любом их
// значении.
//
// Отрицание («прежних ключей нет») стоит В ПАРЕ с положительным («состояние
// есть»): одно без другого зеленело бы на пустой нагрузке.
func TestMovePayloadIsTheWholeGroupOnTheWire(t *testing.T) {
	repo := newFakeRepo()
	tg := makeTG("prj-src", "moved-wire")
	repo.seedTG(tg)
	tgt := kachoTarget(string(tg.ID), domain.Target{
		ExternalIP: &domain.TargetExternalIP{Address: "203.0.113.10"}, Weight: 100,
	})
	repo.seedTarget(string(tg.ID), &tgt)

	opsRepo := newFakeOpsRepo()
	uc := NewMoveTargetGroupUseCase(repo, opsRepo, &fakeProjectClient{},
		&fakeCheckClient{allowed: true}, nil)
	op, err := uc.Execute(ctxWithUser("usr_owner"), &lbv1.MoveTargetGroupRequest{
		TargetGroupId:        string(tg.ID),
		DestinationProjectId: "prj-dst",
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	var movedRow *fakeOutboxEvent
	for i, e := range repo.outboxEvents() {
		if e.ResourceType == "nlb_target_group" && e.Action == "MOVED" {
			ev := repo.outboxEvents()[i]
			movedRow = &ev
		}
	}
	require.NotNil(t, movedRow, "переезд не эмитил строки рода MOVED")

	raw, err := json.Marshal(movedRow.Payload)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &wire))

	require.Contains(t, wire, "state",
		"нагрузка переезда не несёт конверта полного состояния — одна частичная точка "+
			"делает ложным ВЕСЬ вид, и делает это тихо")
	require.NotContains(t, wire, "old_project_id", "ключ прежнего минимального снимка вернулся")
	require.NotContains(t, wire, "new_project_id", "ключ прежнего минимального снимка вернулся")
}
