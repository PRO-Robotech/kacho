// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	quotaband "github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/quota"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Арендаторское чтение квот этого домена.
//
// Утверждается НАБЛЮДАЕМОЕ: что арендатор видит про СВОЙ проект, чего он не
// видит про чужой и что получает на вопрос, который не был корректным.

// quotaRowsStub — подставные строки учёта ДВУХ проектов.
//
// Двух намеренно: дублёр, знающий только один проект, отвечал бы верно на любой
// вопрос и сделал бы невидимым ровно тот дефект, ради которого стоит отрицание —
// чтение, отдающее чужие числа.
type quotaRowsStub struct {
	byProject map[string][]quotaread.State
	askedType string
	askedID   string
}

func (s *quotaRowsStub) Admit(context.Context, string, string, string) error { return nil }

func (s *quotaRowsStub) ListStates(
	_ context.Context, carrierType, carrierID string,
) ([]quotaread.State, error) {
	s.askedType, s.askedID = carrierType, carrierID
	return s.byProject[carrierID], nil
}

// readerStub — read-транзакция, из которой полоса берёт строки.
//
// Остальные глаголы читателя приходят ВСТРОЕННЫМ интерфейсом, а не заглушками:
// набранные вручную, они разошлись бы с настоящим составом молча, и проба
// перестала бы компилироваться только тогда, когда состав изменится в обратную
// сторону. Ни один из них здесь не вызывается.
type readerStub struct {
	kacho.RepositoryReader
	quotas kacho.QuotaReaderIface
	closed bool
}

func (r *readerStub) Quotas() kacho.QuotaReaderIface { return r.quotas }
func (r *readerStub) Close() error                   { r.closed = true; return nil }

// repoStub отдаёт полосе читателя; писатель ей на чтении не нужен и не даётся —
// чтение НЕ ЗАПИСЫВАЕТ, и попытка записи упала бы здесь паникой, а не прошла бы
// незамеченной.
type repoStub struct {
	reader *readerStub
	writes int
}

func (r *repoStub) Reader(context.Context) (kacho.RepositoryReader, error) { return r.reader, nil }

func (r *repoStub) Writer(context.Context) (kacho.RepositoryWriter, error) {
	r.writes++
	return nil, nil
}

type quotaLimitsStub struct{ calls int }

func (s *quotaLimitsStub) Resolve(
	context.Context, string, string,
) ([]quotaread.ResolvedLimit, error) {
	s.calls++
	return nil, nil
}

type quotaAccountsStub struct{}

func (quotaAccountsStub) AccountOf(context.Context, string) (string, error) { return "acc-1", nil }

func quotaFixture(t *testing.T) (*Handler, *quotaRowsStub, *repoStub, *quotaLimitsStub) {
	t.Helper()
	rows := &quotaRowsStub{byProject: map[string][]quotaread.State{
		"prj-mine": {
			{Kind: "loadbalancer.listeners", Limit: 64, Used: 5,
				SourceScope: "DEFAULT",
				CarrierType: quotaread.CarrierProject, CarrierID: "prj-mine"},
			{Kind: "loadbalancer.networkLoadBalancers", Limit: 16, Used: 2,
				SourceScope: "PROJECT", SourceScopeID: "prj-mine",
				CarrierType: quotaread.CarrierProject, CarrierID: "prj-mine"},
		},
		"prj-theirs": {
			{Kind: "loadbalancer.networkLoadBalancers", Limit: 999, Used: 900,
				SourceScope: "PROJECT", SourceScopeID: "prj-theirs",
				CarrierType: quotaread.CarrierProject, CarrierID: "prj-theirs"},
		},
	}}
	repo := &repoStub{reader: &readerStub{quotas: rows}}
	limits := &quotaLimitsStub{}
	guard := quotaband.NewGuard(repo, limits, quotaAccountsStub{}, "loadbalancer")
	return NewHandler(guard), rows, repo, limits
}

// Свои пределы и своё потребление видны — и в том виде, в каком их назвал учёт.
func TestHandler_ListShowsTheCallersOwnLimitAndUsage(t *testing.T) {
	h, rows, repo, limits := quotaFixture(t)

	resp, err := h.List(context.Background(), &lbv1.ListQuotasRequest{ProjectId: "prj-mine"})
	require.NoError(t, err)
	require.Len(t, resp.GetQuotas(), 2,
		"проект читает ПОЛНЫЙ набор своих видов: подмножество означало бы «этих пределов нет»")

	got := resp.GetQuotas()[1]
	require.Equal(t, "loadbalancer.networkLoadBalancers", got.GetKind())
	require.EqualValues(t, 16, got.GetLimit())
	require.EqualValues(t, 2, got.GetUsed(),
		"потребление — половина ответа: без него предел не говорит арендатору, сколько у него осталось")
	require.Equal(t, iamv1.Limit_PROJECT, got.GetSourceScope(),
		"источник величины отличает личное перекрытие от общего правила — иначе непонятно, кто её меняет")
	require.Equal(t, "prj-mine", got.GetSourceScopeId())
	require.Equal(t, quotaread.CarrierProject, got.GetCarrierType())
	require.Equal(t, "prj-mine", got.GetCarrierId())

	require.Equal(t, quotaread.CarrierProject, rows.askedType)
	require.Equal(t, "prj-mine", rows.askedID)
	require.Zero(t, limits.calls, "строки есть — спрашивать владельца величин не о чем")
	require.Zero(t, repo.writes, "чтение квот не записывает: «посмотреть» не может быть мутацией")
	require.True(t, repo.reader.closed, "read-транзакция обязана закрываться на каждом пути")
}

// Чужие числа не видны — вопрос задан ПРО ЗАПРОШЕННЫЙ проект.
func TestHandler_ListNeverShowsAnotherProjectsNumbers(t *testing.T) {
	h, rows, _, _ := quotaFixture(t)

	resp, err := h.List(context.Background(), &lbv1.ListQuotasRequest{ProjectId: "prj-mine"})
	require.NoError(t, err)

	require.Equal(t, "prj-mine", rows.askedID)
	for _, q := range resp.GetQuotas() {
		require.Equal(t, "prj-mine", q.GetCarrierId(),
			"в ответе оказалась строка чужого проекта: числа арендатора принадлежат ему одному")
		require.NotEqualValues(t, 900, q.GetUsed(), "потребление чужого проекта видно в ответе")
	}
}

// Пустой проект — отказ ПО ИМЕНИ ПОЛЯ, а не отказ в правах.
func TestHandler_ListRefusesAnUnanswerableQuestionByName(t *testing.T) {
	h, rows, _, limits := quotaFixture(t)

	_, err := h.List(context.Background(), &lbv1.ListQuotasRequest{})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "project_id: required", status.Convert(err).Message())

	require.Empty(t, rows.askedID, "запрос без проекта не должен доходить до учёта")
	require.Zero(t, limits.calls)
}

// Непровязанная полоса отвечает НАЗВАННЫМ отказом, а не пустым набором.
func TestHandler_ListWithNoBandRefusesInsteadOfClaimingNoQuotas(t *testing.T) {
	h := NewHandler(nil)

	_, err := h.List(context.Background(), &lbv1.ListQuotasRequest{ProjectId: "prj-mine"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}
