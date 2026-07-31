// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
)

// RBAC  per-object filtered List — kacho-nlb consumer.
// Acceptance (docs/specs/rbac-rules-model-2026-acceptance.md):
//   - byName / global: List отдаёт только доступные объекты — страница из БД,
//     суженная per-object проверкой (iam BatchCheck по `nlb_*`, viewer ∪ v_list).
//   - no-leak: объект вне грантов отсутствует в List И Get→NotFound.
//   - read==enforce: List-видимость = Check-allow (одна tuple-база, relation viewer).
//   - fail-closed: IAM недоступен → Unavailable (НЕ нефильтрованный список).

// fakeListFilter — in-memory authzfilter.Filter для unit-тестов.
//
//   - bypass=true → страница отдаётся как есть (list-filter disabled / wildcard).
//   - err!=nil    → возвращается как есть (fail-closed Unavailable у use-case).
//   - allowed     → per (resourceType) explicit allow-list; nil-map → ничего не видно.
type fakeListFilter struct {
	bypass  bool
	err     error
	allowed map[string][]string // resourceType → ids
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

// ctxWithUser возвращает ctx с user-principal (FGA subject "user:<id>").
func ctxWithUser(id string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: id})
}

// global: subject с list-грантом видит все доступные LB; чужие отсутствуют.
func TestListLoadBalancersFilter_OnlyAccessible(t *testing.T) {
	repo := newFakeRepo()
	a := seedLB(t, repo, "prj-a", "lb-a1")
	b := seedLB(t, repo, "prj-a", "lb-a2")
	_ = seedLB(t, repo, "prj-a", "lb-a3") // НЕ в гранте → не должен попасть в List

	flt := &fakeListFilter{allowed: map[string][]string{
		"nlb_network_load_balancer": {a, b},
	}}
	uc := NewListLoadBalancersUseCase(repo, flt)

	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetNetworkLoadBalancers(), 2)
	got := map[string]bool{}
	for _, lb := range resp.GetNetworkLoadBalancers() {
		got[lb.GetId()] = true
	}
	assert.True(t, got[a])
	assert.True(t, got[b])

	// read==enforce: фильтр спрошен с relation viewer-action на правильном типе.
	assert.Equal(t, "user:usr_alice", flt.gotSubj)
	assert.Equal(t, "nlb_network_load_balancer", flt.gotType)
	assert.Equal(t, "loadbalancer.networkLoadBalancers.list", flt.gotAct)
}

// no-leak: пустой грант → пустой List (НЕ ошибка, НЕ leak).
func TestListLoadBalancersFilter_EmptyGrantEmptyList(t *testing.T) {
	repo := newFakeRepo()
	seedLB(t, repo, "prj-a", "lb-secret")

	flt := &fakeListFilter{allowed: map[string][]string{}} // нет грантов
	uc := NewListLoadBalancersUseCase(repo, flt)

	resp, err := uc.Execute(ctxWithUser("usr_bob"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNetworkLoadBalancers())
	assert.Empty(t, resp.GetNextPageToken())
}

// fail-closed: IAM BatchCheck error → Unavailable (НЕ нефильтрованный список).
func TestListLoadBalancersFilter_FailClosed(t *testing.T) {
	repo := newFakeRepo()
	seedLB(t, repo, "prj-a", "lb-a1")

	flt := &fakeListFilter{err: status.Error(codes.Unavailable, "iam down")}
	uc := NewListLoadBalancersUseCase(repo, flt)

	_, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// bypass: BypassAll (admin / wildcard) → нефильтрованный project-scoped список.
func TestListLoadBalancersFilter_BypassReturnsAll(t *testing.T) {
	repo := newFakeRepo()
	seedLB(t, repo, "prj-a", "lb-a1")
	seedLB(t, repo, "prj-a", "lb-a2")

	flt := &fakeListFilter{bypass: true}
	uc := NewListLoadBalancersUseCase(repo, flt)

	resp, err := uc.Execute(ctxWithUser("usr_admin"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetNetworkLoadBalancers(), 2)
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
func TestListLoadBalancersFilter_AbsentModelRefuses(t *testing.T) {
	repo := newFakeRepo()
	seedLB(t, repo, "prj-a", "lb-a1")
	seedLB(t, repo, "prj-a", "lb-a2")

	uc := NewListLoadBalancersUseCase(repo, nil)
	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.Error(t, err, "спросить негде — значит отказ, а не «да»")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, resp.GetNetworkLoadBalancers())
}

// SECURITY (CWE-862): a system/empty-subject request (a
// caller whose forwarded principal was dropped — anonymous peer, non-forwarder
// mTLS, missing x-kacho-principal-* headers) MUST NOT bypass the per-object
// filter on a List path. With an enabled filter it fails closed to an EMPTY
// result, never the victim project's rows — no cross-tenant enumeration.
func TestListLoadBalancersFilter_SystemSubjectNoLeak(t *testing.T) {
	repo := newFakeRepo()
	seedLB(t, repo, "prj-a", "lb-a1")

	// Фильтр НАМЕРЕННО отдал бы страницу, если бы его спросили — так проверяется,
	// что отказ не зависит от его сговорчивости.
	flt := &fakeListFilter{allowed: map[string][]string{"user:": {"*"}}}
	uc := NewListLoadBalancersUseCase(repo, flt)

	// ctx без принципала → никого не названо → отказ ДО обращения к фильтру.
	// Раньше отсечка жила внутри фильтра, поэтому при неподключённом фильтре не
	// исполнялась вовсе; теперь она принимается раньше и одинакова в обоих случаях.
	resp, err := uc.Execute(context.Background(),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.Error(t, err, "principal-less caller must be refused")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Empty(t, resp.GetNetworkLoadBalancers(),
		"principal-less caller must not enumerate the project's load balancers")
	assert.Empty(t, flt.gotSubj,
		"the refusal must not depend on the filter being consulted at all")
}

// errFromFilter — guard: фильтр возвращает не-status ошибку → всё равно Unavailable.
func TestListLoadBalancersFilter_GenericErrIsUnavailable(t *testing.T) {
	repo := newFakeRepo()
	seedLB(t, repo, "prj-a", "lb-a1")

	flt := &fakeListFilter{err: errors.New("boom")}
	uc := NewListLoadBalancersUseCase(repo, flt)

	_, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
