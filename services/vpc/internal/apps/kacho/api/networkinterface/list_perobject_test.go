// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

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

// Per-page фильтрованный List для NetworkInterfaceService: возвращаем ТОЛЬКО
// авторизованные subject'у NIC'и (viewer ∪ v_list, FGA-тип
// vpc_network_interface), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetNetworkInterfaceUseCase и pkg/authz/interceptor_test.go.

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

// seedNICsLabeled — вставляет NIC'и с заданными id в project/subnet.
func seedNICsLabeled(t *testing.T, kr *kachomock.Repository, projectID, subnetID string, nicIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range nicIDs {
		n := &domain.NetworkInterface{
			ID:        id,
			ProjectID: projectID,
			Name:      domain.RcNameVPC("nic-" + id),
			SubnetID:  subnetID,
			Status:    domain.NIStatusAvailable,
		}
		if _, ierr := w.NetworkInterfaces().Insert(context.Background(), n); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestNetworkInterfaceListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_aaa", "nic_bbb", "nic_ccc")

	filter, peer := narrowtest.Recording("nic_aaa", "nic_bbb")
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	nics, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, nics, 2)
	got := map[string]bool{}
	for _, n := range nics {
		got[n.ID] = true
	}
	assert.True(t, got["nic_aaa"])
	assert.True(t, got["nic_bbb"])
	assert.False(t, got["nic_ccc"], "nic_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.networkInterfaces.list,
	// FGA-тип vpc_network_interface) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", peer.Subject)
	assert.Equal(t, "vpc_network_interface", peer.ResourceType)
	assert.Equal(t, "vpc.networkInterfaces.list", peer.Action)
	assert.ElementsMatch(t, []string{"nic_aaa", "nic_bbb", "nic_ccc"}, peer.IDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestNetworkInterfaceListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_visible", "nic_secret")

	filter := narrowtest.Allowing("nic_visible")
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	nics, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, nics, 1)
	assert.Equal(t, "nic_visible", nics[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestNetworkInterfaceListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_a", "nic_b")

	filter := narrowtest.DenyingAll()
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	nics, next, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, nics)
	assert.Empty(t, next)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestNetworkInterfaceListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_a", "nic_b", "nic_c")

	filter := narrowtest.AllowingAll()
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	nics, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, nics, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestNetworkInterfaceListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_a")

	filter := narrowtest.Failing(status.Error(codes.Unavailable, "iam down"))
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	_, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestNetworkInterfaceListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_a")

	filter := narrowtest.Failing(errors.New("boom"))
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	_, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestNetworkInterfaceListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_a", "nic_b")

	uc := NewListNetworkInterfacesUseCase(kr, narrowtest.AllowingAll())
	nics, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, nics, 2)
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestNetworkInterfaceListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListNetworkInterfacesUseCase(kr, narrowtest.AllowingAll())

	_, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestNetworkInterfaceListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNICsLabeled(t, kr, "prj_1", "e9b_sub1", "nic_a", "nic_b")

	filter := narrowtest.Allowing("unused_id")
	uc := NewListNetworkInterfacesUseCase(kr, filter)

	nics, _, err := uc.Execute(narrowtest.Caller(), NetworkInterfaceFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, nics, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
