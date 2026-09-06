// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// The read-TX is a BORROWED POOL CONNECTION, so the order in which List does its
// two jobs is a capacity property, not a matter of taste: asking iam whether the
// caller may see the page is a network wait on another service, and a connection
// held across it is a connection nobody else can use for the whole round-trip.
// Once as many Lists are in flight as the pool is wide, every following Reader —
// and every Writer, the pool is shared — waits for an answer from iam before it
// can even reach a healthy database.
//
// Asserted here at the level that survives a rewrite: the ledger of readers
// opened/closed AT THE MOMENT the filter is called. Both halves are needed —
// "no reader open" alone would also be true of a List that never opened one, so
// the probe requires that a reader was opened AND that it was already given back.
//
// The page is fully materialised by repo.List before it returns (rows are scanned
// into a slice, rows.Close deferred inside), and the proto mapping never touches
// the database again — so releasing the reader first loses nothing.

// releaseTrackingRepo hands out readers whose open/close it counts. It embeds the
// package's in-memory fake, so every other behaviour of List is unchanged.
type releaseTrackingRepo struct {
	*fakeRepo
	opened int
	closed int
}

func (r *releaseTrackingRepo) Reader(ctx context.Context) (kachorepo.RepositoryReader, error) {
	inner, err := r.fakeRepo.Reader(ctx)
	if err != nil {
		return nil, err
	}
	r.opened++
	return &releaseTrackingReader{inner: inner, owner: r}, nil
}

type releaseTrackingReader struct {
	inner kachorepo.RepositoryReader
	owner *releaseTrackingRepo
}

func (rd *releaseTrackingReader) LoadBalancers() kachorepo.LoadBalancerReaderIface {
	return rd.inner.LoadBalancers()
}
func (rd *releaseTrackingReader) Listeners() kachorepo.ListenerReaderIface {
	return rd.inner.Listeners()
}
func (rd *releaseTrackingReader) TargetGroups() kachorepo.TargetGroupReaderIface {
	return rd.inner.TargetGroups()
}

func (rd *releaseTrackingReader) Close() error {
	rd.owner.closed++
	return rd.inner.Close()
}

// readerStateProbe records the reader ledger at the instant rights are asked about.
//
// Наблюдение переехало на СОСЕДА: сужатель теперь один на дерево, и подставлять его
// целиком значило бы наблюдать не тот момент. Момент, ради которого проба написана,
// — сетевой вопрос к kaname, и он здесь и перехватывается.
type readerStateProbe struct {
	repo         *releaseTrackingRepo
	calls        int
	openedAtCall int
	closedAtCall int
}

var _ listnarrow.AuthorizeClient = (*readerStateProbe)(nil)

func (p *readerStateProbe) BatchCheck(_ context.Context, checks []listnarrow.Check) ([]bool, error) {
	p.calls++
	p.openedAtCall, p.closedAtCall = p.repo.opened, p.repo.closed
	out := make([]bool, 0, len(checks))
	for range checks {
		out = append(out, true)
	}
	return out, nil
}

// TestListLoadBalancers_ReleasesReaderBeforeAuthz — the pooled read-TX must be
// given back BEFORE iam is asked about the page.
func TestListLoadBalancers_ReleasesReaderBeforeAuthz(t *testing.T) {
	t.Parallel()
	repo := &releaseTrackingRepo{fakeRepo: newFakeRepo()}
	seedLB(t, repo.fakeRepo, "prj-a", "lb-a1")

	probe := &readerStateProbe{repo: repo}
	uc := NewListLoadBalancersUseCase(repo, narrowtest.New(probe))

	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListNetworkLoadBalancersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetNetworkLoadBalancers(), 1)

	require.Equal(t, 1, probe.calls,
		"the page must actually be run through the visibility filter")
	require.Equal(t, 1, probe.openedAtCall,
		"the page must be read through a reader — otherwise this test proves nothing")
	require.Equal(t, 1, probe.closedAtCall,
		"the read-TX was still open while iam was asked: a pooled connection is held "+
			"across a peer round-trip, so concurrent Lists starve every other reader and writer")
}

// Quotas — совещательная полоса учёта. В пробах этого пакета полоса НЕ
// провязана (`u.quota == nil`), поэтому дублёр обязан не «разрешать», а
// доказывать, что до него не доходят: разрешающий дублёр скрыл бы ровно тот
// вызов, ради которого его подставляют (`testing.md` §«дублёр, принимающий
// больше настоящего»).
func (r *releaseTrackingReader) Quotas() kachorepo.QuotaReaderIface {
	panic("quota band must not be reached: this package's probes do not wire it")
}
