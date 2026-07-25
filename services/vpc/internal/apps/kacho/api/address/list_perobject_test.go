// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

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

// Per-page фильтрованный List для AddressService: возвращаем ТОЛЬКО
// авторизованные subject'у адреса (viewer ∪ v_list, FGA-тип
// vpc_address), read==enforce, fail-closed при недоступном iam, пустой grant
// → пусто (no-leak).
//
// Единичный Get здесь НЕ тестируется: его видимость энфорсит per-RPC
// authz-interceptor прямым per-object Check'ом (existence-hiding на deny), а не
// use-case — см. GetAddressUseCase и pkg/authz/interceptor_test.go.

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

// seedAddressesLabeled вставляет external-адреса с заданными id в проект.
func seedAddressesLabeled(t *testing.T, kr *kachomock.Repository, projectID string, addrIDs ...string) {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	for _, id := range addrIDs {
		a := &domain.Address{
			ID:        id,
			ProjectID: projectID,
			Name:      domain.RcNameVPC("addr-" + id),
			Type:      domain.AddressTypeExternal,
			IpVersion: domain.IpVersionIPv4,
		}
		if _, ierr := w.Addresses().Insert(context.Background(), a); ierr != nil {
			require.NoError(t, ierr)
		}
	}
	require.NoError(t, w.Commit())
}

// List возвращает ровно per-object разрешенный набор.
func TestAddressListPerObject_ReturnsOnlyAllowed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_aaa", "adr_bbb", "adr_ccc")

	filter := &fakeListFilter{allowed: []string{"adr_aaa", "adr_bbb"}}
	uc := NewListAddressesUseCase(kr, filter)

	addrs, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, addrs, 2)
	got := map[string]bool{}
	for _, a := range addrs {
		got[a.ID] = true
	}
	assert.True(t, got["adr_aaa"])
	assert.True(t, got["adr_bbb"])
	assert.False(t, got["adr_ccc"], "adr_ccc not in the allowed set → must not appear")

	// read==enforce: фильтр зовется с read-verb (action vpc.addresses.list,
	// FGA-тип vpc_address) и получает РОВНО идентификаторы страницы.
	assert.Equal(t, "user:usr_alice", filter.gotSubject)
	assert.Equal(t, "vpc_address", filter.gotResourceType)
	assert.Equal(t, "vpc.addresses.list", filter.gotAction)
	assert.ElementsMatch(t, []string{"adr_aaa", "adr_bbb", "adr_ccc"}, filter.gotIDs,
		"visibility must be asked for the page's ids, never for the whole universe")
}

// no-leak: объект вне всех grant'ов отсутствует в List.
func TestAddressListPerObject_NoLeak(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_visible", "adr_secret")

	filter := &fakeListFilter{allowed: []string{"adr_visible"}}
	uc := NewListAddressesUseCase(kr, filter)

	addrs, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, "adr_visible", addrs[0].ID)
}

// Пустой grant: subject без grant'а → пустой список (НЕ нефильтрованный).
func TestAddressListPerObject_EmptyGrantEmptyList(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a", "adr_b")

	filter := &fakeListFilter{allowed: nil}
	uc := NewListAddressesUseCase(kr, filter)

	addrs, next, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, addrs)
	assert.Empty(t, next)
}

// Полный grant (subject видит всё в скоупе) → отдаются все строки страницы.
func TestAddressListPerObject_AllVisibleReturnsAll(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a", "adr_b", "adr_c")

	filter := &fakeListFilter{allowAll: true}
	uc := NewListAddressesUseCase(kr, filter)

	addrs, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, addrs, 3)
}

// fail-closed: iam недоступен → Unavailable (НЕ нефильтрованный, НЕ молча пустой).
func TestAddressListPerObject_FailClosedUnavailable(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a")

	filter := &fakeListFilter{err: status.Error(codes.Unavailable, "iam down")}
	uc := NewListAddressesUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// fail-closed (plain error): не-status ошибка тоже маппится в non-OK код, никогда
// не проходит молча как нефильтрованная.
func TestAddressListPerObject_FailClosedPlainError(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a")

	filter := &fakeListFilter{err: errors.New("boom")}
	uc := NewListAddressesUseCase(kr, filter)

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.NotEqual(t, codes.OK, st.Code())
}

// nil-фильтр → нефильтрованный passthrough (list-filter выключен).
func TestAddressListPerObject_NilFilterPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a", "adr_b")

	uc := NewListAddressesUseCase(kr, nil)
	addrs, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, addrs, 2)
}

// явный system principal → нефильтрованный passthrough (без вызова FGA).
func TestAddressListPerObject_SystemSubjectPassthrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a", "adr_b")

	filter := &fakeListFilter{allowed: []string{"adr_a"}}
	uc := NewListAddressesUseCase(kr, filter)

	addrs, _, err := uc.Execute(context.Background(), authzfilter.SystemSubject, AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Len(t, addrs, 2)
	assert.Equal(t, 0, filter.calls, "explicit system principal → passthrough, no authz call")
}

// project_id по-прежнему обязателен (контракт не меняется).
func TestAddressListPerObject_ProjectIDRequired(t *testing.T) {
	kr := kachomock.NewRepository()
	uc := NewListAddressesUseCase(kr, &fakeListFilter{allowAll: true})

	_, _, err := uc.Execute(context.Background(), "user:usr_alice", AddressFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// No-leak (defense-in-depth): пустой subject (principal не извлечен — anon /
// gateway не проставил identity) при ВКЛЮЧЕННОМ фильтре → fail-closed (пустой
// список), НЕ unfiltered passthrough. «Не знаю, кто ты» != «доверенный system».
func TestAddressListPerObject_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAddressesLabeled(t, kr, "prj_1", "adr_a", "adr_b")

	filter := &fakeListFilter{allowed: []string{"unused_id"}}
	uc := NewListAddressesUseCase(kr, filter)

	addrs, _, err := uc.Execute(context.Background(), "", AddressFilter{ProjectID: "prj_1"}, Pagination{})
	require.NoError(t, err)
	assert.Empty(t, addrs, "empty subject + filter enabled -> fail-closed empty, NOT leak")
}
