// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Subject-resolution + FilterPage normalisation tests. kacho-nlb derives the FGA
// subject from the operations.Principal in ctx (set by grpcsrv.UnaryPrincipalExtract),
// NOT from raw gRPC metadata — so these tests drive operations.WithPrincipal /
// SystemPrincipal through domain.FGASubjectFromPrincipal.

func ctxWithPrincipal(typ, id string) context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: typ, ID: id})
}

// user principal → "user:<id>".
func TestSubjectFromCtx_User(t *testing.T) {
	assert.Equal(t, "user:usr_alice", SubjectFromCtx(ctxWithPrincipal("user", "usr_alice")))
}

// service account principal → "service_account:<id>".
func TestSubjectFromCtx_ServiceAccount(t *testing.T) {
	assert.Equal(t, "service_account:sa_xyz", SubjectFromCtx(ctxWithPrincipal("service_account", "sa_xyz")))
}

// system principal (background / dev) → "" (fail-closed downstream, NOT bypass).
func TestSubjectFromCtx_SystemIsEmpty(t *testing.T) {
	assert.Empty(t, SubjectFromCtx(context.Background()), "system (bare ctx)")
	assert.Empty(t, SubjectFromCtx(ctxWithPrincipal("system", "bootstrap")), "system (explicit)")
}

// fakeFilter — minimal Filter for FilterPage normalisation tests.
type fakeFilter struct {
	visible []string
	err     error
	gotSubj string
	gotType string
	gotAct  string
	gotIDs  []string
	calls   int
}

func (f *fakeFilter) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	f.calls++
	f.gotSubj, f.gotType, f.gotAct, f.gotIDs = subject, resourceType, action, ids
	if f.err != nil {
		return nil, f.err
	}
	return f.visible, nil
}

// nil filter → passthrough without resolving a subject (list-filter disabled / dev).
func TestFilterPage_NilFilterPassthrough(t *testing.T) {
	got, err := FilterPage(ctxWithPrincipal("user", "usr_alice"), nil,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a", "nlb-b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"nlb-a", "nlb-b"}, got)
}

// empty page → no authz round-trip.
func TestFilterPage_EmptyPageNoCall(t *testing.T) {
	flt := &fakeFilter{}
	got, err := FilterPage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Zero(t, flt.calls)
}

// SECURITY (CWE-862): a system/empty subject on a List path (a request whose
// forwarded principal was dropped — anonymous peer, non-forwarder mTLS, missing
// x-kacho-principal-* headers) MUST NOT short-circuit to an unfiltered page.
// FilterPage delegates to the filter, which is the sole per-object authz layer for
// ScopeFiltered List RPCs; short-circuiting here re-opens cross-tenant enumeration.
func TestFilterPage_SystemSubjectDoesNotBypass(t *testing.T) {
	flt := &fakeFilter{visible: nil}
	got, err := FilterPage(context.Background(), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.NoError(t, err)
	assert.Empty(t, got, "system subject MUST NOT yield an unfiltered page (cross-tenant leak)")
	require.Equal(t, 1, flt.calls, "the filter must be consulted")
	assert.Empty(t, flt.gotSubj, "the (empty) subject must be passed through, not swapped for a bypass")
}

// SECURITY: with the real enabled FGAFilter an empty/system subject fails closed
// (Unauthenticated) and never queries iam — proving a principal-less caller cannot
// enumerate another project's resources.
func TestFilterPage_SystemSubjectFailClosed_RealFilter(t *testing.T) {
	cli := newFakeAuthorizeClient()
	flt := NewFGAFilter(cli, Config{
		Enabled: true, Timeout: time.Second, CacheTTL: time.Second, CacheMaxEntries: 10,
	})
	_, err := FilterPage(context.Background(), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err, "system subject must fail closed with the real filter (else unfiltered leak)")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, 0, cli.calls, "filter must not query iam for an empty subject")
}

// user subject → filter called with the resolved subject + given type/action/ids.
func TestFilterPage_UserSubjectCallsFilter(t *testing.T) {
	flt := &fakeFilter{visible: []string{"nlb-1"}}
	got, err := FilterPage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeListener, ActionListenerList, []string{"nlb-1", "nlb-2"})
	require.NoError(t, err)
	assert.Equal(t, "user:usr_alice", flt.gotSubj)
	assert.Equal(t, ResourceTypeListener, flt.gotType)
	assert.Equal(t, ActionListenerList, flt.gotAct)
	assert.Equal(t, []string{"nlb-1", "nlb-2"}, flt.gotIDs, "only the page's ids are asked about")
	assert.Equal(t, []string{"nlb-1"}, got)
}

// filter status error is passed through (fail-closed), never a page.
func TestFilterPage_FilterStatusErrPassthrough(t *testing.T) {
	flt := &fakeFilter{err: status.Error(codes.Unavailable, "iam down")}
	got, err := FilterPage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeTargetGroup, ActionTargetGroupList, []string{"nlb-a"})
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// non-status filter error is coerced to Unavailable (defensive fail-closed guard:
// a raw error would otherwise leak as codes.Unknown).
func TestFilterPage_NonStatusErrCoercedUnavailable(t *testing.T) {
	flt := &fakeFilter{err: errors.New("boom")}
	_, err := FilterPage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeTargetGroup, ActionTargetGroupList, []string{"nlb-a"})
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

// ---- FilterVisiblePage (records) -------------------------------------------

type rec struct {
	id string
}

func recID(r *rec) string { return r.id }

// Записи фильтруются по видимости и СОХРАНЯЮТ порядок курсора.
func TestFilterVisiblePage_KeepsCursorOrder(t *testing.T) {
	flt := &fakeFilter{visible: []string{"c", "a"}}
	page := []*rec{{id: "c"}, {id: "a"}, {id: "b"}}

	got, err := FilterVisiblePage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, page, recID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "c", got[0].id)
	assert.Equal(t, "a", got[1].id)
}

// nil-filter → страница как есть; пустая страница → без обращения к iam.
func TestFilterVisiblePage_PassthroughAndEmpty(t *testing.T) {
	page := []*rec{{id: "a"}}
	got, err := FilterVisiblePage(ctxWithPrincipal("user", "usr_alice"), nil,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, page, recID)
	require.NoError(t, err)
	assert.Equal(t, page, got)

	flt := &fakeFilter{}
	got, err = FilterVisiblePage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []*rec(nil), recID)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Zero(t, flt.calls)
}

// fail-closed: ошибка фильтра → ошибка наружу, НЕ нефильтрованная страница.
func TestFilterVisiblePage_FailClosed(t *testing.T) {
	flt := &fakeFilter{err: status.Error(codes.Unavailable, "iam down")}
	got, err := FilterVisiblePage(ctxWithPrincipal("user", "usr_alice"), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []*rec{{id: "a"}}, recID)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
