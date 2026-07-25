// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Per-page фильтрованный List для NetworkService: возвращаем ТОЛЬКО
// авторизованные subject'у networks (viewer ∪ v_list, FGA-тип
// vpc_network), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetNetworkUseCase и pkg/authz/interceptor_test.go.

// fakeListFilter — in-memory ListFilter для unit-тестов. Запоминает аргументы, с
// которыми его позвали, и отвечает из заранее заданного видимого набора.
type fakeListFilter struct {
	allowed  []string
	allowAll bool
	err      error

	gotSubject      string
	gotResourceType string
	gotAction       string
	gotIDs          []string
	calls           int
}

func (f *fakeListFilter) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	f.calls++
	f.gotSubject = subject
	f.gotResourceType = resourceType
	f.gotAction = action
	f.gotIDs = append([]string(nil), ids...)
	if f.err != nil {
		return nil, f.err
	}
	if f.allowAll {
		return ids, nil
	}
	set := make(map[string]bool, len(f.allowed))
	for _, a := range f.allowed {
		set[a] = true
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if set[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// seedNetworksLabeled вставляет networks с заданными id в проект.
func seedNetworksLabeled(t *testing.T, kr *kachomock.Repository, projectID string, netIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range netIDs {
		n := &domain.Network{ID: id, ProjectID: projectID, Name: domain.RcNameVPC("net-" + id)}
		if _, ierr := w.Networks().Insert(context.Background(), n); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestNetworkListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_aaa", "net_bbb", "net_ccc")

	filter := &fakeListFilter{allowed: []string{"net_aaa", "net_bbb"}}
	uc := NewListNetworksUseCase(kr, filter)

	nets, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, nets, 2)
	got := map[string]bool{}
	for _, n := range nets {
		got[n.ID] = true
	}
	assert.True(t, got["net_aaa"])
	assert.True(t, got["net_bbb"])
	assert.False(t, got["net_ccc"], "net_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.networks.list,
	// FGA-тип vpc_network) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", filter.gotSubject)
	assert.Equal(t, "vpc_network", filter.gotResourceType)
	assert.Equal(t, "vpc.networks.list", filter.gotAction)
	assert.ElementsMatch(t, []string{"net_aaa", "net_bbb", "net_ccc"}, filter.gotIDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestNetworkListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_visible", "net_secret")

	filter := &fakeListFilter{allowed: []string{"net_visible"}}
	uc := NewListNetworksUseCase(kr, filter)

	nets, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, nets, 1)
	assert.Equal(t, "net_visible", nets[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestNetworkListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	filter := &fakeListFilter{allowed: nil}
	uc := NewListNetworksUseCase(kr, filter)

	nets, next, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, nets)
	assert.Empty(t, next)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestNetworkListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b", "net_c")

	filter := &fakeListFilter{allowAll: true}
	uc := NewListNetworksUseCase(kr, filter)

	nets, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, nets, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestNetworkListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a")

	filter := &fakeListFilter{err: status.Error(codes.Unavailable, "iam down")}
	uc := NewListNetworksUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestNetworkListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a")

	filter := &fakeListFilter{err: errors.New("boom")}
	uc := NewListNetworksUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestNetworkListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	uc := NewListNetworksUseCase(kr, nil)
	nets, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, nets, 2)
}

// явный system principal → нефильтрованный passthrough (без вызова FGA).
func TestNetworkListPerObject_SystemSubjectPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	filter := &fakeListFilter{allowed: []string{"net_a"}}
	uc := NewListNetworksUseCase(kr, filter)

	nets, _, err := uc.Execute(context.Background(), authzfilter.SystemSubject, NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, nets, 2)
	assert.Equal(t, 0, filter.calls, "explicit system principal → passthrough, no authz call")
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestNetworkListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListNetworksUseCase(kr, &fakeListFilter{allowAll: true})

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", NetworkFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestNetworkListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	filter := &fakeListFilter{allowed: []string{"unused_id"}}
	uc := NewListNetworksUseCase(kr, filter)

	nets, _, err := uc.Execute(context.Background(), "", NetworkFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, nets, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
