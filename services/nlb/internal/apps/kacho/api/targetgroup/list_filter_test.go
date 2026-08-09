// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// RBAC  per-object filtered List — TargetGroup (byName / no-leak / fail-closed).
// Страница из БД сужается per-object проверкой (iam BatchCheck, viewer ∪ v_list);
// см. loadbalancer/list_filter_test.go.

func ctxWithUser(id string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: id})
}

// byName: List отдаёт ровно перечисленные id; остальные отсутствуют.
func TestListTargetGroupsFilter_OnlyAccessible(t *testing.T) {
	repo := newFakeRepo()
	a := makeTG("prj-a", "tg-a1")
	b := makeTG("prj-a", "tg-a2")
	c := makeTG("prj-a", "tg-a3") // НЕ в гранте
	repo.seedTG(a)
	repo.seedTG(b)
	repo.seedTG(c)

	flt, peer := narrowtest.Recording(string(a.ID), string(b.ID))
	uc := NewListTargetGroupsUseCase(repo, flt)

	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetTargetGroups(), 2)
	got := map[string]bool{}
	for _, tg := range resp.GetTargetGroups() {
		got[tg.GetId()] = true
	}
	assert.True(t, got[string(a.ID)])
	assert.True(t, got[string(b.ID)])
	assert.False(t, got[string(c.ID)])

	// read==enforce: правильный тип + list-action (viewer relation server-side).
	assert.Equal(t, "user:usr_alice", peer.Subject)
	assert.Equal(t, "nlb_target_group", peer.ResourceType)
	assert.Equal(t, "loadbalancer.targetGroups.list", peer.Action)
}

// no-leak: пустой грант → пустой List.
func TestListTargetGroupsFilter_EmptyGrantEmptyList(t *testing.T) {
	repo := newFakeRepo()
	repo.seedTG(makeTG("prj-a", "tg-secret"))

	flt := narrowtest.Allowing()
	uc := NewListTargetGroupsUseCase(repo, flt)

	resp, err := uc.Execute(ctxWithUser("usr_bob"),
		&lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	assert.Empty(t, resp.GetTargetGroups())
}

// fail-closed: ListObjects error → Unavailable.
func TestListTargetGroupsFilter_FailClosed(t *testing.T) {
	repo := newFakeRepo()
	repo.seedTG(makeTG("prj-a", "tg-a1"))

	flt := narrowtest.Failing(status.Error(codes.Unavailable, "iam down"))
	uc := NewListTargetGroupsUseCase(repo, flt)

	_, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// Отсутствующий фильтр — НЕ passthrough. За списочными RPC nlb per-RPC Check не
// задаётся вовсе (ScopeFiltered), поэтому «модели здесь нет» означает «авторизации
// здесь нет», и страница не отдаётся. Тест раньше закреплял противоположное: он
// утверждал как контракт ровно ту посадку, в которой RPC перечисляет чужой проект.
// Формулировка отказа взята у эталона, который уже стоял в дереве, — storage
// AllowedOnObject: «Это состояние посадки, а не ответ модели».
//
// Парный положительный к этому отказу — соседние тесты этого файла: при подключённой
// модели страница действительно возвращается и действительно сужается.
func TestListTargetGroupsFilter_AbsentModelRefuses(t *testing.T) {
	repo := newFakeRepo()
	repo.seedTG(makeTG("prj-a", "tg-a1"))
	repo.seedTG(makeTG("prj-a", "tg-a2"))

	uc := NewListTargetGroupsUseCase(repo, nil)
	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, resp.GetTargetGroups())
}

// bypass → all project-scoped.
func TestListTargetGroupsFilter_BypassReturnsAll(t *testing.T) {
	repo := newFakeRepo()
	repo.seedTG(makeTG("prj-a", "tg-a1"))
	repo.seedTG(makeTG("prj-a", "tg-a2"))

	flt := narrowtest.AllowingAll()
	uc := NewListTargetGroupsUseCase(repo, flt)
	resp, err := uc.Execute(ctxWithUser("usr_admin"),
		&lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetTargetGroups(), 2)
}
