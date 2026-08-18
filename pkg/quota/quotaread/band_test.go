// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quotaread_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
)

// Полоса чтения квот: одно тело на всех владельцев.
//
// Утверждается ПОВЕДЕНИЕ, а не форма вызова: что арендатор видит на живом
// проекте, что он видит на пустом, и чего он не видит никогда.

type stubRows struct {
	states []quotaread.State
	err    error

	gotCarrierType string
	gotCarrierID   string
	calls          int
}

func (s *stubRows) ListStates(_ context.Context, carrierType, carrierID string) ([]quotaread.State, error) {
	s.calls++
	s.gotCarrierType, s.gotCarrierID = carrierType, carrierID
	return s.states, s.err
}

type stubLimits struct {
	limits []quotaread.ResolvedLimit
	err    error

	gotScopeID string
	gotService string
	calls      int
}

func (s *stubLimits) Resolve(_ context.Context, scopeID, service string) ([]quotaread.ResolvedLimit, error) {
	s.calls++
	s.gotScopeID, s.gotService = scopeID, service
	return s.limits, s.err
}

func newBand(t *testing.T, rows quotaread.Rows, limits quotaread.Limits) *quotaread.Band {
	t.Helper()
	b, err := quotaread.NewBand(rows, limits, "loadbalancer", "nlb")
	require.NoError(t, err)
	return b
}

// Живой проект: отдаются ЕГО строки, у соседа ничего не спрашивается.
func TestStatesOfALiveProjectComeFromItsOwnRows(t *testing.T) {
	t.Parallel()

	rows := &stubRows{states: []quotaread.State{
		{Kind: "loadbalancer.listeners", Limit: 64, Used: 3,
			SourceScope: "PROJECT", SourceScopeID: "prj-1",
			CarrierType: quotaread.CarrierProject, CarrierID: "prj-1"},
	}}
	limits := &stubLimits{}

	got, err := newBand(t, rows, limits).States(context.Background(), "prj-1")
	require.NoError(t, err)

	require.Equal(t, rows.states, got)
	require.Equal(t, quotaread.CarrierProject, rows.gotCarrierType)
	require.Equal(t, "prj-1", rows.gotCarrierID)
	require.Zero(t, limits.calls,
		"строки есть — спрашивать соседа не о чем; лишний вызов делает чтение своих квот "+
			"заложником доступности владельца величин на КАЖДОМ обращении, а не только на первом")
}

// Пустой проект: ответ собирается резолвом и НЕ бывает пустым.
//
// Это положительный контроль к отрицанию ниже: без него «ноль строк» зеленело бы
// и на исправной полосе, и на полосе, которая просто ничего не отдаёт.
func TestStatesOfAFreshProjectAreResolvedAndNeverEmpty(t *testing.T) {
	t.Parallel()

	rows := &stubRows{states: nil}
	limits := &stubLimits{limits: []quotaread.ResolvedLimit{
		{Kind: "loadbalancer.targetGroups", Value: 64, Carrier: quotaread.CarrierProject,
			SourceScope: "DEFAULT"},
		{Kind: "loadbalancer.networkLoadBalancers", Value: 16, Carrier: quotaread.CarrierProject,
			SourceScope: "ACCOUNT", SourceScopeID: "acc-7"},
	}}

	got, err := newBand(t, rows, limits).States(context.Background(), "prj-2")
	require.NoError(t, err)

	require.Len(t, got, 2, "проект, ещё ничего не создавший, читает ПОЛНЫЙ набор своих видов: "+
		"пустой ответ был бы прочитан как «предела нет»")
	// Порядок — по виду, а не по порядку ответа соседа: иначе набор свежего
	// проекта шёл бы иначе, чем набор того же проекта после первой мутации.
	require.Equal(t, "loadbalancer.networkLoadBalancers", got[0].Kind)
	require.Equal(t, "loadbalancer.targetGroups", got[1].Kind)

	require.EqualValues(t, 0, got[0].Used, "строк учёта нет — значит ни одна вставка ещё не списывала место")
	require.EqualValues(t, 16, got[0].Limit)
	require.Equal(t, "ACCOUNT", got[0].SourceScope)
	require.Equal(t, "acc-7", got[0].SourceScopeID)
	require.Equal(t, quotaread.CarrierProject, got[0].CarrierType)
	require.Equal(t, "prj-2", got[0].CarrierID)

	require.Equal(t, "prj-2", limits.gotScopeID)
	require.Equal(t, "loadbalancer", limits.gotService,
		"у соседа спрашивается токен КАТАЛОГА, а не имя сервиса: каталог знает виды nlb как loadbalancer")
}

// Вид, считаемый в РОДИТЕЛЕ, в проектный ответ не попадает.
func TestStatesOmitKindsCountedInAParent(t *testing.T) {
	t.Parallel()

	rows := &stubRows{}
	limits := &stubLimits{limits: []quotaread.ResolvedLimit{
		{Kind: "loadbalancer.networkLoadBalancers", Value: 16, Carrier: quotaread.CarrierProject},
		{Kind: "loadbalancer.networkLoadBalancers.listeners", Value: 16,
			Carrier: "loadbalancer.networkLoadBalancers"},
		{Kind: "loadbalancer.orphan", Value: 1, Carrier: ""},
	}}

	got, err := newBand(t, rows, limits).States(context.Background(), "prj-3")
	require.NoError(t, err)

	require.Len(t, got, 1)
	require.Equal(t, "loadbalancer.networkLoadBalancers", got[0].Kind,
		"вложенный вид единственного значения на уровне проекта не имеет, а вид без "+
			"названного носителя не раскладывается наугад: обе строки показывали бы "+
			"потребление, которое не наполнится никогда")
}

// Недоступный владелец величин — ОТКАЗ, а не пустой набор.
func TestStatesRefuseWhenTheLimitOwnerCannotBeReached(t *testing.T) {
	t.Parallel()

	rows := &stubRows{}
	limits := &stubLimits{err: status.Error(codes.Unavailable, "iam is down")}

	_, err := newBand(t, rows, limits).States(context.Background(), "prj-4")
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err),
		"полоса недоступности повторяема и обязана быть отличима от промаха")
	require.NotContains(t, status.Convert(err).Message(), "iam is down",
		"проза соседа наружу не идёт — она может нести имя хоста и текст драйвера")
}

// Отказ СВОЕЙ базы не переодевается в отказ соседа.
func TestStatesPropagateOwnStorageFailureUnchanged(t *testing.T) {
	t.Parallel()

	own := errors.New("read quota rows: connection reset")
	rows := &stubRows{err: own}
	limits := &stubLimits{}

	_, err := newBand(t, rows, limits).States(context.Background(), "prj-5")
	require.ErrorIs(t, err, own,
		"своя беда, названная чужой полосой, уводит отладку к соседу, у которого всё в порядке")
	require.Zero(t, limits.calls)
}

// Несобранная полоса отказывает В МОМЕНТ СБОРКИ, а не на первом чтении.
func TestNewBandRefusesToAssembleWithoutItsParts(t *testing.T) {
	t.Parallel()

	rows, limits := &stubRows{}, &stubLimits{}

	// Положительный контроль: полная сборка проходит — иначе отрицания ниже
	// зеленели бы на конструкторе, который отвергает вообще всё.
	ok, err := quotaread.NewBand(rows, limits, "vpc", "vpc")
	require.NoError(t, err)
	require.NotNil(t, ok)

	for name, call := range map[string]func() (*quotaread.Band, error){
		"без строк":           func() (*quotaread.Band, error) { return quotaread.NewBand(nil, limits, "vpc", "vpc") },
		"без владельца":       func() (*quotaread.Band, error) { return quotaread.NewBand(rows, nil, "vpc", "vpc") },
		"без токена каталога": func() (*quotaread.Band, error) { return quotaread.NewBand(rows, limits, "", "vpc") },
		"без имени домена":    func() (*quotaread.Band, error) { return quotaread.NewBand(rows, limits, "vpc", "") },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := call()
			require.Error(t, err)
		})
	}
}
