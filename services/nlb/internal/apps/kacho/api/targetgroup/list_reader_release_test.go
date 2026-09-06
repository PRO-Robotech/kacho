// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	"google.golang.org/grpc"
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
//
// Наблюдение переехало на СОСЕДА: сужатель теперь один на дерево, и подставлять
// его целиком значило бы наблюдать не тот момент. Момент, ради которого проба
// написана, — сетевой вопрос к kaname, и он здесь и перехватывается.
type readerStateProbe struct {
	repo         *releaseTrackingRepo
	calls        int
	openedAtCall int
	closedAtCall int
}

var _ listnarrow.AuthorizeClient = (*readerStateProbe)(nil)

func (p *readerStateProbe) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest,
	_ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	p.calls++
	p.openedAtCall, p.closedAtCall = p.repo.opened, p.repo.closed
	out := make([]*iamv1.AuthorizeCheckResponse, 0, len(in.GetChecks()))
	for range in.GetChecks() {
		out = append(out, &iamv1.AuthorizeCheckResponse{Allowed: true})
	}
	return &iamv1.BatchAuthorizeCheckResponse{Responses: out}, nil
}

// TestListTargetGroups_ReleasesReaderBeforeAuthz — pooled read-TX released first.
func TestListTargetGroups_ReleasesReaderBeforeAuthz(t *testing.T) {
	t.Parallel()
	repo := &releaseTrackingRepo{fakeRepo: newFakeRepo()}
	repo.fakeRepo.seedTG(makeTG("prj-a", "tg-a1"))

	probe := &readerStateProbe{repo: repo}
	uc := NewListTargetGroupsUseCase(repo, narrowtest.New(probe))

	resp, err := uc.Execute(ctxWithUser("usr_alice"),
		&lbv1.ListTargetGroupsRequest{ProjectId: "prj-a"})
	require.NoError(t, err)
	require.Len(t, resp.GetTargetGroups(), 1)

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
