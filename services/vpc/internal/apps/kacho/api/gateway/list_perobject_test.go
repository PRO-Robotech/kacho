// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

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

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// Per-page фильтрованный List для GatewayService: возвращаем ТОЛЬКО
// авторизованные subject'у gateway'и (viewer ∪ v_list, FGA-тип
// vpc_gateway), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetGatewayUseCase и pkg/authz/interceptor_test.go.

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

// seedGatewaysLabeled вставляет в проект gateway'и с указанными id.
func seedGatewaysLabeled(t *testing.T, kr *kachomock.Repository, projectID string, gwIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range gwIDs {
		g := &domain.Gateway{
			ID:          id,
			ProjectID:   projectID,
			Name:        domain.RcNameVPC("gw-" + id),
			GatewayType: domain.GatewayTypeNat,
		}
		if _, ierr := w.Gateways().Insert(context.Background(), g); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestGatewayListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_aaa", "gw_bbb", "gw_ccc")

	filter, peer := narrowtest.Recording("gw_aaa", "gw_bbb")
	uc := NewListGatewaysUseCase(kr, filter)

	gws, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, gws, 2)
	got := map[string]bool{}
	for _, g := range gws {
		got[g.ID] = true
	}
	assert.True(t, got["gw_aaa"])
	assert.True(t, got["gw_bbb"])
	assert.False(t, got["gw_ccc"], "gw_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.gateways.list,
	// FGA-тип vpc_gateway) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", peer.Subject)
	assert.Equal(t, "vpc_gateway", peer.ResourceType)
	assert.Equal(t, "vpc.gateways.list", peer.Action)
	assert.ElementsMatch(t, []string{"gw_aaa", "gw_bbb", "gw_ccc"}, peer.IDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestGatewayListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_visible", "gw_secret")

	filter := narrowtest.Allowing("gw_visible")
	uc := NewListGatewaysUseCase(kr, filter)

	gws, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, gws, 1)
	assert.Equal(t, "gw_visible", gws[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestGatewayListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_a", "gw_b")

	filter := narrowtest.DenyingAll()
	uc := NewListGatewaysUseCase(kr, filter)

	gws, next, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, gws)
	assert.Empty(t, next)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestGatewayListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_a", "gw_b", "gw_c")

	filter := narrowtest.AllowingAll()
	uc := NewListGatewaysUseCase(kr, filter)

	gws, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, gws, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestGatewayListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_a")

	filter := narrowtest.Failing(status.Error(codes.Unavailable, "iam down"))
	uc := NewListGatewaysUseCase(kr, filter)

	_, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestGatewayListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_a")

	filter := narrowtest.Failing(errors.New("boom"))
	uc := NewListGatewaysUseCase(kr, filter)

	_, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestGatewayListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_a", "gw_b")

	uc := NewListGatewaysUseCase(kr, narrowtest.AllowingAll())
	gws, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, gws, 2)
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestGatewayListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListGatewaysUseCase(kr, narrowtest.AllowingAll())

	_, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestGatewayListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedGatewaysLabeled(t, kr, "prj_1", "gw_a", "gw_b")

	filter := narrowtest.Allowing("unused_id")
	uc := NewListGatewaysUseCase(kr, filter)

	gws, _, err := uc.Execute(narrowtest.Caller(), GatewayFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, gws, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
