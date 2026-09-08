// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package nicinternal

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
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// ListByInstance answers "which interfaces are bound to these instances" for the
// compute-side mirror. The instance ids come from the caller, and the reply carries
// the interface id, its subnet, its MAC and its resolved primary addresses.
//
// The per-RPC question that used to guard it was `viewer` on the singleton
// `cluster:cluster_root`. That relation exists for the global reference
// catalogue — regions, zones, disk types — and the cluster bootstrap writes
// `cluster:<root>#viewer@user:*` precisely so that every authenticated subject can
// read it. Asking it here therefore admitted every authenticated subject in the
// cluster and then returned whatever instance ids they had named, from any project
// and any account. Interface bindings are not reference data.
//
// The answer is decided per returned interface instead: the page comes out of our own
// database, and the model is asked which of that page's ids the subject may see
// (`viewer ∪ v_list` on `vpc_network_interface:<id>`, batched). Same discipline as
// every public List of this service — the visible set equals the check-allowed set,
// and the cost follows the page rather than the population.

// fakeNICFilter — in-memory ListFilter recording how it was asked.
type fakeNICFilter struct {
	allowed  []string
	allowAll bool
	err      error

	gotSubject      string
	gotResourceType string
	gotAction       string
	gotIDs          []string
	calls           int
}

func (f *fakeNICFilter) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
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

// seedAttachedNIC inserts a NIC and binds it to an instance, so ListByInstanceIDs
// returns it.
func seedAttachedNIC(t *testing.T, kr *kachomock.Repository, projectID, subnetID, nicID, instanceID string) {
	t.Helper()
	ctx := context.Background()
	w, err := kr.Writer(ctx)
	require.NoError(t, err)
	_, err = w.NetworkInterfaces().Insert(ctx, &domain.NetworkInterface{
		ID:        nicID,
		ProjectID: projectID,
		Name:      domain.RcNameVPC("nic-" + nicID),
		SubnetID:  subnetID,
		Status:    domain.NIStatusAvailable,
	})
	require.NoError(t, err)
	_, err = w.NetworkInterfaces().AttachToInstance(ctx, kachorepo.AttachNICParams{
		NICID:      nicID,
		InstanceID: instanceID,
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
}

func nicIDsOf(att []*kachorepo.NetworkInterfaceAttachment) []string {
	out := make([]string, 0, len(att))
	for _, a := range att {
		out = append(out, a.NICID)
	}
	return out
}

// A caller who may see one interface must not receive the other one just because they
// named its instance. This is the whole point: the instance ids are caller-supplied.
func TestListByInstance_ReturnsOnlyInterfacesTheSubjectMaySee(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_mine", "e9b_sub1", "nic_mine", "ins_mine")
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	filter, peer := narrowtest.Recording("nic_mine")
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(narrowtest.Caller(), []string{"ins_mine", "ins_theirs"})
	require.NoError(t, err)

	assert.Equal(t, []string{"nic_mine"}, nicIDsOf(att),
		"an interface the subject may not see must not come back merely because the "+
			"caller named the instance it is bound to")

	// The model is asked about the ids of the page, never about the whole population.
	assert.Equal(t, "user:usr_alice", peer.Subject)
	assert.Equal(t, authzfilter.ResourceTypeNetworkInterface, peer.ResourceType)
	assert.Equal(t, authzfilter.ActionNetworkInterfaceList, peer.Action)
	assert.ElementsMatch(t, []string{"nic_mine", "nic_theirs"}, peer.IDs)
}

// No grant at all → nothing comes back, and nothing leaks through the shape of the
// answer either.
func TestListByInstance_NoGrantReturnsNothing(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	filter := narrowtest.DenyingAll()
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(narrowtest.Caller(), []string{"ins_theirs"})
	require.NoError(t, err)
	assert.Empty(t, att)
}

// Безымянный вызывающий — «не знаю, кто ты», а не «доверенный». Полярность теперь
// ОТКАЗ, а не пустая страница: «пусто» неотличимо от «личность потеряна по дороге».
// Сужатель здесь разрешает всё — значит отказ приходит именно с линии личности.
func TestListByInstance_UnnamedCallerIsRefused(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	filter, peer := narrowtest.Recording()
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(context.Background(), []string{"ins_theirs"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Empty(t, att, "безымянный запрос не получает нефильтрованной страницы")
	assert.Zero(t, peer.Calls, "спрашивать не о ком — модель не тревожат")
}

// There is no subject value that skips the filter any more, and that is the point:
// the only thing that ever produced one was a principal TYPE the caller writes into
// its own request headers — the same token the edge stamps on an unauthenticated
// request. A caller declaring it is nobody, and nobody sees nothing.
// The end-to-end lock over the handler seam lives in
// internal/handler/forged_system_principal_test.go.

// The model being unreachable must not turn into "show everything".
func TestListByInstance_FilterErrorFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_a", "e9b_sub1", "nic_a", "ins_a")

	sentinel := errors.New("iam unavailable")
	filter := narrowtest.Failing(sentinel)
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(narrowtest.Caller(), []string{"ins_a"})
	require.Error(t, err, "a visibility answer we could not obtain is not an answer of yes")
	assert.Empty(t, att)
}

// Оба отказа сохраняют СВОЮ линию, даже когда сходятся в одном запросе.
//
// Безымянный вызывающий при неподключённом сужателе получает ответ ПРО ЛИЧНОСТЬ:
// личность проверяется первой, и это порядок, а не вкус. Пока отсечка стояла за
// веткой посадки, посадка без модели отдавала всё кому угодно; схлопни их обратно —
// и фикс одного спрячет регрессию другого.
func TestListByInstance_UnnamedCallerWithoutModelIsRefusedByIdentity(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr) // сужатель не подключён

	att, err := svc.ListByInstance(context.Background(), []string{"ins_theirs"})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err),
		"личность проверяется ПЕРВОЙ — ответ не зависит от того, что прописал оператор")
	assert.Empty(t, att)
}
