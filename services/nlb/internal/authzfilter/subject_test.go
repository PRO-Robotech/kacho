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

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
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
// ScopeFiltered List RPCs have no per-RPC Check behind them, so a page handed out
// here is cross-tenant enumeration with nothing left to stop it.
//
// The refusal used to be delegated to the filter. It is now taken here, before the
// filter is consulted at all, so it no longer depends either on a filter BEING
// wired (it may be nil) or on the wired filter's own discipline. The assertions
// therefore pin the outcome the caller sees — refused, no page — and that the
// filter is NOT relied upon; they no longer pin "the filter was consulted", which
// was the mechanism, not the property.
func TestFilterPage_SystemSubjectDoesNotBypass(t *testing.T) {
	flt := &fakeFilter{visible: []string{"nlb-a"}} // would hand the page back if asked
	got, err := FilterPage(context.Background(), flt,
		ResourceTypeLoadBalancer, ActionLoadBalancerList, []string{"nlb-a"})
	require.Error(t, err, "an unnamed caller must be refused")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Empty(t, got, "system subject MUST NOT yield an unfiltered page (cross-tenant leak)")
	assert.Zero(t, flt.calls, "the refusal must not depend on the filter's cooperation")
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

// TestFilterPage_UnnamedCallerIsCutOffEvenWithoutAFilter — за ScopeFiltered-List
// нет per-RPC Check, на который можно откатиться: фильтр по данным — ЕДИНСТВЕННЫЙ
// слой авторизации. Поэтому вызывающий, которого никто не назвал, обязан получать
// отказ БЕЗУСЛОВНО, а не «когда фильтр подключён» — и ровно тот же отказ, что при
// подключённом фильтре: положение вызывающего одинаково, значит и ответ обязан
// быть одинаков.
//
// Привязка отсечки к наличию фильтра означает, что посадка без фильтра отдаёт всю
// страницу кому угодно, и держится это лишь на boot-guard'е — то есть контроль
// существует ровно до первой конфигурации, которая его не включила. vpc и storage
// эту отсечку уже сделали безусловной; nlb был последним.
func TestFilterPage_UnnamedCallerIsCutOffEvenWithoutAFilter(t *testing.T) {
	ids := []string{"nlb-1", "nlb-2"}

	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"no principal at all", context.Background()},
		{"edge anonymity marker", operations.WithPrincipal(context.Background(),
			operations.Principal{Type: "system", ID: operations.AnonymousPrincipalID})},
		{"marker declared as a tenant", operations.WithPrincipal(context.Background(),
			operations.Principal{Type: "user", ID: operations.AnonymousPrincipalID})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FilterPage(tc.ctx, nil, "nlb_load_balancer", "v_list", ids)
			require.Error(t, err, "unnamed caller must be refused, not handed a page")
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
			assert.Emptyf(t, got, "unnamed caller must not enumerate; got %v", got)
		})
	}
}

// TestFilterPage_NamedCallerStillPassesThroughWithoutAFilter — обратная сторона:
// сужение анонимности не должно ломать посадку без фильтра для НАЗВАННОГО
// вызывающего (dev/in-process фикстуры), иначе это не отсечка анонимности, а
// поломка списков.
func TestFilterPage_NamedCallerStillPassesThroughWithoutAFilter(t *testing.T) {
	ids := []string{"nlb-1", "nlb-2"}
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})

	got, err := FilterPage(ctx, nil, "nlb_load_balancer", "v_list", ids)
	require.NoError(t, err)
	assert.Equal(t, ids, got, "named caller keeps the documented no-filter passthrough")
}

// TestFGASubjectFromPrincipal_ReservedWordMirrorsOperations — доменное зеркало
// зарезервированного слова обязано совпадать с общим предикатом платформы.
// Разъехавшись, они превратили бы «неизвестно кто» в субъект.
func TestFGASubjectFromPrincipal_ReservedWordMirrorsOperations(t *testing.T) {
	assert.Equal(t, operations.AnonymousPrincipalID, domain.AnonymousPrincipalID,
		"domain mirror of the reserved anonymity word drifted from operations")
	for _, pType := range []string{"user", "service_account", "system", ""} {
		assert.Emptyf(t, domain.FGASubjectFromPrincipal(pType, operations.AnonymousPrincipalID),
			"the reserved word must never become a subject, declared type %q", pType)
	}
}
