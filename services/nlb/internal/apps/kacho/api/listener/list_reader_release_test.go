// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Same property as loadbalancer/list_reader_release_test.go: the pooled read-TX
// must be given back BEFORE the caller's rights are asked about, because that ask
// is a network wait on iam and the connection is shared with every other reader
// and writer of this service.

// releaseTrackingRepo hands out readers whose open/close it counts.
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
type readerStateProbe struct {
	repo         *releaseTrackingRepo
	calls        int
	openedAtCall int
	closedAtCall int
}

var _ authzfilter.Filter = (*readerStateProbe)(nil)

func (p *readerStateProbe) FilterVisibleIDs(_ context.Context, _, _, _ string, ids []string) ([]string, error) {
	p.calls++
	p.openedAtCall, p.closedAtCall = p.repo.opened, p.repo.closed
	return ids, nil
}

// TestListListeners_ReleasesReaderBeforeAuthz — pooled read-TX released first.
func TestListListeners_ReleasesReaderBeforeAuthz(t *testing.T) {
	t.Parallel()
	repo := &releaseTrackingRepo{fakeRepo: newFakeRepo()}
	seedListenerLF(t, repo.fakeRepo, "prj-a", "nlb_lb1", "l-a1")

	probe := &readerStateProbe{repo: repo}
	uc := NewListUseCase(repo, probe)

	resp, err := uc.Run(ctxWithUser("usr_alice"),
		&lbv1.ListListenersRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetListeners(), 1)

	require.Equal(t, 1, probe.calls,
		"the page must actually be run through the visibility filter")
	require.Equal(t, 1, probe.openedAtCall,
		"the page must be read through a reader — otherwise this test proves nothing")
	require.Equal(t, 1, probe.closedAtCall,
		"the read-TX was still open while iam was asked: a pooled connection is held "+
			"across a peer round-trip, so concurrent Lists starve every other reader and writer")
}
