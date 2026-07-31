// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// RBAC  per-object filtered List — Listener.
// Страница из БД сужается per-object проверкой (iam BatchCheck, viewer ∪ v_list);
// no-leak / fail-closed / passthrough — как в loadbalancer/list_filter_test.go.

type fakeListFilter struct {
	bypass  bool
	err     error
	allowed map[string][]string
	gotSubj string
	gotType string
	gotAct  string
	gotIDs  []string
}

// Compile-time guard: the fake must implement the real port — a drift in the
// filter's signature must break here, not silently skip the authz layer.
var _ authzfilter.Filter = (*fakeListFilter)(nil)

func (f *fakeListFilter) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	f.gotSubj, f.gotType, f.gotAct, f.gotIDs = subject, resourceType, action, ids
	if f.err != nil {
		return nil, f.err
	}
	if f.bypass {
		return ids, nil
	}
	allowed := make(map[string]struct{}, len(f.allowed[resourceType]))
	for _, id := range f.allowed[resourceType] {
		allowed[id] = struct{}{}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := allowed[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func ctxWithUser(id string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: id})
}

func seedListenerLF(t *testing.T, repo *fakeRepo, projectID, lbID, name string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixListener)
	repo.seedListener(&kachorepo.ListenerRecord{
		Listener: domain.Listener{
			ID:             domain.ResourceID(id),
			LoadBalancerID: domain.ResourceID(lbID),
			ProjectID:      domain.ProjectID(projectID),
			RegionID:       "ru-central1",
			Name:           domain.LbName(name),
			Labels:         domain.LbLabels{},
			Protocol:       domain.ProtoTCP,
			Port:           80,
			TargetPort:     80,
			Status:         domain.ListenerStatusActive,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	return id
}

// union analog: List отдаёт только доступные listener'ы.
func TestListListenersFilter_OnlyAccessible(t *testing.T) {
	repo := newFakeRepo()
	a := seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-a1")
	b := seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-a2")
	_ = seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-a3") // НЕ в гранте

	flt := &fakeListFilter{allowed: map[string][]string{
		"nlb_listener": {a, b},
	}}
	uc := NewListUseCase(repo, flt)

	resp, err := uc.Run(ctxWithUser("usr_alice"),
		&lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetListeners(), 2)
	got := map[string]bool{}
	for _, l := range resp.GetListeners() {
		got[l.GetId()] = true
	}
	assert.True(t, got[a])
	assert.True(t, got[b])

	assert.Equal(t, "user:usr_alice", flt.gotSubj)
	assert.Equal(t, "nlb_listener", flt.gotType)
	assert.Equal(t, "loadbalancer.listeners.list", flt.gotAct)
}

// no-leak: пустой грант → пустой List.
func TestListListenersFilter_EmptyGrantEmptyList(t *testing.T) {
	repo := newFakeRepo()
	seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-secret")

	flt := &fakeListFilter{allowed: map[string][]string{}}
	uc := NewListUseCase(repo, flt)

	resp, err := uc.Run(ctxWithUser("usr_bob"),
		&lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	assert.Empty(t, resp.GetListeners())
}

// fail-closed → Unavailable.
func TestListListenersFilter_FailClosed(t *testing.T) {
	repo := newFakeRepo()
	seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-a1")

	flt := &fakeListFilter{err: status.Error(codes.Unavailable, "iam down")}
	uc := NewListUseCase(repo, flt)

	_, err := uc.Run(ctxWithUser("usr_alice"),
		&lbv1.ListListenersRequest{ProjectId: "prj-a"})
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
func TestListListenersFilter_AbsentModelRefuses(t *testing.T) {
	repo := newFakeRepo()
	seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-a1")
	seedListenerLF(t, repo, "prj-a", "nlb_lb1", "l-a2")

	uc := NewListUseCase(repo, nil)
	resp, err := uc.Run(ctxWithUser("usr_alice"),
		&lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, resp.GetListeners())
}
