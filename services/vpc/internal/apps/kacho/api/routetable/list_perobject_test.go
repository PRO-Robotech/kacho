// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Per-page фильтрованный List для RouteTableService: возвращаем ТОЛЬКО
// авторизованные subject'у route-table'ы (viewer ∪ v_list, FGA-тип
// vpc_route_table), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetRouteTableUseCase и pkg/authz/interceptor_test.go.

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

// seedRouteTablesLabeled вставляет route-table'ы с заданными id в проект.
func seedRouteTablesLabeled(t *testing.T, kr *kachomock.Repository, projectID, networkID string, rtIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range rtIDs {
		rt := &domain.RouteTable{
			ID:        id,
			ProjectID: projectID,
			NetworkID: networkID,
			Name:      domain.RcNameVPC("rt-" + id),
		}
		if _, ierr := w.RouteTables().Insert(context.Background(), rt); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestRouteTableListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_aaa", "rtb_bbb", "rtb_ccc")

	filter := &fakeListFilter{allowed: []string{"rtb_aaa", "rtb_bbb"}}
	uc := NewListRouteTablesUseCase(kr, filter)

	rts, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, rts, 2)
	got := map[string]bool{}
	for _, rt := range rts {
		got[rt.ID] = true
	}
	assert.True(t, got["rtb_aaa"])
	assert.True(t, got["rtb_bbb"])
	assert.False(t, got["rtb_ccc"], "rtb_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.routeTables.list,
	// FGA-тип vpc_route_table) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", filter.gotSubject)
	assert.Equal(t, "vpc_route_table", filter.gotResourceType)
	assert.Equal(t, "vpc.routeTables.list", filter.gotAction)
	assert.ElementsMatch(t, []string{"rtb_aaa", "rtb_bbb", "rtb_ccc"}, filter.gotIDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestRouteTableListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_visible", "rtb_secret")

	filter := &fakeListFilter{allowed: []string{"rtb_visible"}}
	uc := NewListRouteTablesUseCase(kr, filter)

	rts, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, rts, 1)
	assert.Equal(t, "rtb_visible", rts[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestRouteTableListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_a", "rtb_b")

	filter := &fakeListFilter{allowed: nil}
	uc := NewListRouteTablesUseCase(kr, filter)

	rts, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, rts)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestRouteTableListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_a", "rtb_b", "rtb_c")

	filter := &fakeListFilter{allowAll: true}
	uc := NewListRouteTablesUseCase(kr, filter)

	rts, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, rts, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestRouteTableListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_a")

	filter := &fakeListFilter{err: status.Error(codes.Unavailable, "iam down")}
	uc := NewListRouteTablesUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestRouteTableListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_a")

	filter := &fakeListFilter{err: errors.New("boom")}
	uc := NewListRouteTablesUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestRouteTableListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_a", "rtb_b")

	uc := NewListRouteTablesUseCase(kr, nil)
	rts, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, rts, 2)
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestRouteTableListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListRouteTablesUseCase(kr, &fakeListFilter{allowAll: true})

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", RouteTableFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestRouteTableListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedRouteTablesLabeled(t, kr, "prj_1", "enp_net1", "rtb_a", "rtb_b")

	filter := &fakeListFilter{allowed: []string{"rts_unused"}}
	uc := NewListRouteTablesUseCase(kr, filter)

	rts, _, err := uc.Execute(context.Background(), "", RouteTableFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, rts, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
