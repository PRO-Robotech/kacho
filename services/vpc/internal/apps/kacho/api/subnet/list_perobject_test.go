// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

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

// Per-page фильтрованный List для SubnetService: возвращаем ТОЛЬКО
// авторизованные subject'у подсети (viewer ∪ v_list, FGA-тип
// vpc_subnet), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetSubnetUseCase и pkg/authz/interceptor_test.go.

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

// seedSubnetsLabeled вставляет подсети с заданными id в project/network.
func seedSubnetsLabeled(t *testing.T, kr *kachomock.Repository, projectID, networkID string, subnetIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range subnetIDs {
		s := &domain.Subnet{ID: id, ProjectID: projectID, NetworkID: networkID, Name: domain.RcNameVPC("sub-" + id)}
		if _, ierr := w.Subnets().Insert(context.Background(), s); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestSubnetListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_aaa", "e9b_bbb", "e9b_ccc")

	filter := &fakeListFilter{allowed: []string{"e9b_aaa", "e9b_bbb"}}
	uc := NewListSubnetsUseCase(kr, filter)

	subs, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, subs, 2)
	got := map[string]bool{}
	for _, s := range subs {
		got[s.ID] = true
	}
	assert.True(t, got["e9b_aaa"])
	assert.True(t, got["e9b_bbb"])
	assert.False(t, got["e9b_ccc"], "e9b_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.subnets.list,
	// FGA-тип vpc_subnet) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", filter.gotSubject)
	assert.Equal(t, "vpc_subnet", filter.gotResourceType)
	assert.Equal(t, "vpc.subnets.list", filter.gotAction)
	assert.ElementsMatch(t, []string{"e9b_aaa", "e9b_bbb", "e9b_ccc"}, filter.gotIDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestSubnetListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_visible", "e9b_secret")

	filter := &fakeListFilter{allowed: []string{"e9b_visible"}}
	uc := NewListSubnetsUseCase(kr, filter)

	subs, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "e9b_visible", subs[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestSubnetListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a", "e9b_b")

	filter := &fakeListFilter{allowed: nil}
	uc := NewListSubnetsUseCase(kr, filter)

	subs, next, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, subs)
	assert.Empty(t, next)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestSubnetListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a", "e9b_b", "e9b_c")

	filter := &fakeListFilter{allowAll: true}
	uc := NewListSubnetsUseCase(kr, filter)

	subs, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, subs, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestSubnetListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a")

	filter := &fakeListFilter{err: status.Error(codes.Unavailable, "iam down")}
	uc := NewListSubnetsUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestSubnetListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a")

	filter := &fakeListFilter{err: errors.New("boom")}
	uc := NewListSubnetsUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestSubnetListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a", "e9b_b")

	uc := NewListSubnetsUseCase(kr, nil)
	subs, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, subs, 2)
}

// явный system principal → нефильтрованный passthrough (без вызова FGA).
func TestSubnetListPerObject_SystemSubjectPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a", "e9b_b")

	filter := &fakeListFilter{allowed: []string{"e9b_a"}}
	uc := NewListSubnetsUseCase(kr, filter)

	subs, _, err := uc.Execute(context.Background(), authzfilter.SystemSubject, SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, subs, 2)
	assert.Equal(t, 0, filter.calls, "explicit system principal → passthrough, no authz call")
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestSubnetListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListSubnetsUseCase(kr, &fakeListFilter{allowAll: true})

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", SubnetFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestSubnetListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSubnetsLabeled(t, kr, "prj_1", "enp_net1", "e9b_a", "e9b_b")

	filter := &fakeListFilter{allowed: []string{"unused_id"}}
	uc := NewListSubnetsUseCase(kr, filter)

	subs, _, err := uc.Execute(context.Background(), "", SubnetFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, subs, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
