// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

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

// Per-page фильтрованный List для SecurityGroupService: возвращаем ТОЛЬКО
// авторизованные subject'у security group (viewer ∪ v_list, FGA-тип
// vpc_security_group), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetSecurityGroupUseCase и pkg/authz/interceptor_test.go.

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

// seedSecurityGroupsLabeled вставляет non-default SG с заданными id в
// project/network. Non-default — чтобы не упереться в инвариант
// one-default-SG-per-network.
func seedSecurityGroupsLabeled(t *testing.T, kr *kachomock.Repository, projectID, networkID string, sgIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range sgIDs {
		sg := &domain.SecurityGroup{
			ID:        id,
			ProjectID: projectID,
			NetworkID: networkID,
			Name:      domain.RcNameVPC("sg-" + id),
		}
		if _, ierr := w.SecurityGroups().Insert(context.Background(), sg); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestSecurityGroupListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_aaa", "sg_bbb", "sg_ccc")

	filter, peer := narrowtest.Recording("sg_aaa", "sg_bbb")
	uc := NewListSecurityGroupsUseCase(kr, filter)

	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, sgs, 2)
	got := map[string]bool{}
	for _, sg := range sgs {
		got[sg.ID] = true
	}
	assert.True(t, got["sg_aaa"])
	assert.True(t, got["sg_bbb"])
	assert.False(t, got["sg_ccc"], "sg_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.securityGroups.list,
	// FGA-тип vpc_security_group) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", peer.Subject)
	assert.Equal(t, "vpc_security_group", peer.ResourceType)
	assert.Equal(t, "vpc.securityGroups.list", peer.Action)
	assert.ElementsMatch(t, []string{"sg_aaa", "sg_bbb", "sg_ccc"}, peer.IDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestSecurityGroupListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_visible", "sg_secret")

	filter := narrowtest.Allowing("sg_visible")
	uc := NewListSecurityGroupsUseCase(kr, filter)

	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, sgs, 1)
	assert.Equal(t, "sg_visible", sgs[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestSecurityGroupListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_a", "sg_b")

	filter := narrowtest.DenyingAll()
	uc := NewListSecurityGroupsUseCase(kr, filter)

	sgs, next, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, sgs)
	assert.Empty(t, next)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestSecurityGroupListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_a", "sg_b", "sg_c")

	filter := narrowtest.AllowingAll()
	uc := NewListSecurityGroupsUseCase(kr, filter)

	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, sgs, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestSecurityGroupListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_a")

	filter := narrowtest.Failing(status.Error(codes.Unavailable, "iam down"))
	uc := NewListSecurityGroupsUseCase(kr, filter)

	_, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestSecurityGroupListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_a")

	filter := narrowtest.Failing(errors.New("boom"))
	uc := NewListSecurityGroupsUseCase(kr, filter)

	_, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestSecurityGroupListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_a", "sg_b")

	uc := NewListSecurityGroupsUseCase(kr, narrowtest.AllowingAll())
	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, sgs, 2)
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestSecurityGroupListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListSecurityGroupsUseCase(kr, narrowtest.AllowingAll())

	_, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestSecurityGroupListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedSecurityGroupsLabeled(t, kr, "prj_1", "enp_net1", "sg_a", "sg_b")

	filter := narrowtest.Allowing("unused_id")
	uc := NewListSecurityGroupsUseCase(kr, filter)

	sgs, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, sgs, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
