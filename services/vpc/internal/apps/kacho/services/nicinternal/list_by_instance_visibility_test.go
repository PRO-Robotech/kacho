// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package nicinternal

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// ListByInstance answers "which interfaces are bound to these instances" for the
// compute-side mirror. The instance ids come from the caller, and the reply carries
// the interface id, its subnet, its MAC and its resolved primary addresses.
//
// The per-RPC question that used to guard it was `viewer` on the singleton
// `cluster:cluster_kacho_root`. That relation exists for the global reference
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

	filter := &fakeNICFilter{allowed: []string{"nic_mine"}}
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(context.Background(), "user:usr_alice",
		[]string{"ins_mine", "ins_theirs"})
	require.NoError(t, err)

	assert.Equal(t, []string{"nic_mine"}, nicIDsOf(att),
		"an interface the subject may not see must not come back merely because the "+
			"caller named the instance it is bound to")

	// The model is asked about the ids of the page, never about the whole population.
	assert.Equal(t, "user:usr_alice", filter.gotSubject)
	assert.Equal(t, authzfilter.ResourceTypeNetworkInterface, filter.gotResourceType)
	assert.Equal(t, authzfilter.ActionNetworkInterfaceList, filter.gotAction)
	assert.ElementsMatch(t, []string{"nic_mine", "nic_theirs"}, filter.gotIDs)
}

// No grant at all → nothing comes back, and nothing leaks through the shape of the
// answer either.
func TestListByInstance_NoGrantReturnsNothing(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	filter := &fakeNICFilter{}
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(context.Background(), "user:usr_stranger", []string{"ins_theirs"})
	require.NoError(t, err)
	assert.Empty(t, att)
}

// An unextracted identity is "I do not know who you are", not "trusted caller". It
// must not be served the unfiltered page.
func TestListByInstance_EmptySubjectFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	filter := &fakeNICFilter{allowAll: true}
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(context.Background(), "", []string{"ins_theirs"})
	require.NoError(t, err)
	assert.Empty(t, att, "empty subject must fail closed, never pass through unfiltered")
	assert.Zero(t, filter.calls, "no identity means there is nothing to ask the model about")
}

// A trusted system caller carries the explicit sentinel and is passed through — that
// is a different case from "identity missing" and must stay distinguishable.
func TestListByInstance_SystemSubjectPassesThrough(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_a", "e9b_sub1", "nic_a", "ins_a")

	filter := &fakeNICFilter{}
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(context.Background(), authzfilter.SystemSubject, []string{"ins_a"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nic_a"}, nicIDsOf(att))
	assert.Zero(t, filter.calls)
}

// The model being unreachable must not turn into "show everything".
func TestListByInstance_FilterErrorFailsClosed(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_a", "e9b_sub1", "nic_a", "ins_a")

	sentinel := errors.New("iam unavailable")
	filter := &fakeNICFilter{err: sentinel}
	svc := NewService(kr).WithListFilter(filter)

	att, err := svc.ListByInstance(context.Background(), "user:usr_alice", []string{"ins_a"})
	require.Error(t, err, "a visibility answer we could not obtain is not an answer of yes")
	assert.Empty(t, att)
}

// Незаданный фильтр не должен превращаться в «покажи всё кому угодно».
//
// Per-RPC Check за этот RPC не задаётся (ScopeFiltered), поэтому вызывающий без
// извлечённой identity доходит сюда — и обязан не получить ничего. Привязка
// fail-closed к наличию фильтра оставила бы конфигурацию, в которой RPC отдаёт
// привязки всего кластера кому угодно.
func TestListByInstance_EmptySubjectFailsClosedEvenWithoutFilter(t *testing.T) {
	kr := kachomock.NewRepository()
	seedAttachedNIC(t, kr, "prj_theirs", "e9b_sub2", "nic_theirs", "ins_theirs")

	svc := NewService(kr) // фильтр не подключён

	att, err := svc.ListByInstance(context.Background(), "", []string{"ins_theirs"})
	require.NoError(t, err)
	assert.Empty(t, att,
		"без identity ответ пуст независимо от того, подключён ли фильтр — "+
			"единственный гейт этого RPC живёт здесь")
}
