// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package refusal_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/refusal"
)

// errPrecondition — sentinel предусловия, какой держит каждый сервис. Здесь он
// свой: пакет полос про СЕРВИСНЫЕ sentinel'ы ничего не знает и знать не должен.
var errPrecondition = errors.New("failed precondition")

// reasonOf достаёт машинный признак из деталей ответа — ровно тем способом,
// каким его прочтёт клиент.
func reasonOf(t *testing.T, err error) (*errdetails.ErrorInfo, bool) {
	t.Helper()
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info, true
		}
	}
	return nil, false
}

// Словарь — ровно четыре полосы, и токены именно эти.
//
// Проба утверждает СОСТАВ, а не длину: `require.Len` в одиночку зеленеет на
// словаре, где одну полосу переименовали, а другую дописали.
func TestDictionaryIsTwoTokensAndFourLanes(t *testing.T) {
	t.Parallel()

	tokens := map[string]bool{}
	pairs := map[string]bool{}
	for _, l := range refusal.AllLanes() {
		require.Truef(t, l.IsDeclared(), "полоса словаря без токена: %#v", l)
		pair := l.Token() + "/" + l.ReferenceKind()
		require.Falsef(t, pairs[pair], "полоса %s объявлена дважды", pair)
		pairs[pair] = true
		tokens[l.Token()] = true
	}
	// Токены — ВЗЯТЫЕ, а не придуманные: `REFERENCE_IN_USE` уже производит служба
	// доступа и уже разобран консолью. Утверждается СОСТАВ, а не длина:
	// `require.Len` в одиночку зеленеет на словаре, где одну полосу переименовали,
	// а другую дописали.
	for _, want := range []string{"DELETION_PROTECTED", "REFERENCE_IN_USE"} {
		require.Truef(t, tokens[want], "токен %s пропал из словаря", want)
	}
	require.Len(t, tokens, 2, "набор токенов вырос или сжался — это правка контракта, и её видит клиент")
	for _, want := range []string{
		"DELETION_PROTECTED/", "REFERENCE_IN_USE/children",
		"REFERENCE_IN_USE/referrers", "REFERENCE_IN_USE/unnamed",
	} {
		require.Truef(t, pairs[want], "полоса %s пропала из словаря", want)
	}
	require.Len(t, pairs, 4, "число полос изменилось — это правка контракта")
	t.Logf("осмотрено: токенов %d, полос %d", len(tokens), len(pairs))
}

// Нулевое значение полосой контракта не притворяется.
func TestZeroLaneIsNotAContractLane(t *testing.T) {
	t.Parallel()

	var zero refusal.Lane
	require.False(t, zero.IsDeclared(), "нулевое значение объявило себя полосой")
	require.Empty(t, zero.Token())

	// Пометка нулевой полосой не помечает: отказ возвращается как был.
	src := fmt.Errorf("%w: network is not empty", errPrecondition)
	require.Same(t, src, refusal.Wrap(zero, refusal.Ref{}, src),
		"нулевая полоса обязана вернуть отказ как есть, а не завернуть его в признак без токена")

	_, _, ok := refusal.LaneOf(src)
	require.False(t, ok, "полоса нашлась на отказе, который ею не помечали")
}

// Пометка полосой НЕ трогает ни текст отказа, ни его sentinel.
//
// Это несущее свойство, а не удобство: на нём стоит то, что признак ставится
// поверх работающего дерева, ничего в нём не сдвигая. Разойдись текст — сдвинулись
// бы разом все утверждения о нём, и сдвинулись бы молча.
func TestWrapChangesNeitherTheTextNorTheSentinel(t *testing.T) {
	t.Parallel()

	src := fmt.Errorf("%w: Network net-1 is not empty (subnets: 2)", errPrecondition)
	wrapped := refusal.Wrap(refusal.HoldsChildren, refusal.Ref{ResourceType: "network", ResourceID: "net-1"}, src)

	require.Equal(t, src.Error(), wrapped.Error(), "полоса дописала слово в контрактный текст")
	require.ErrorIs(t, wrapped, errPrecondition, "классификация сервиса перестала видеть sentinel под полосой")

	lane, ref, ok := refusal.LaneOf(wrapped)
	require.True(t, ok)
	require.Equal(t, "REFERENCE_IN_USE", lane.Token())
	require.Equal(t, "children", lane.ReferenceKind())
	require.Equal(t, "net-1", ref.ResourceID)
}

// Полоса достаётся из ЦЕПОЧКИ, а не только с верхнего уровня: производитель
// помечает отказ в репозитории, а до классификатора он доезжает завёрнутым ещё
// раз-другой.
func TestLaneSurvivesFurtherWrapping(t *testing.T) {
	t.Parallel()

	inner := refusal.Wrap(refusal.ReferredTo, refusal.Ref{ResourceType: "address"},
		fmt.Errorf("%w: address is in use", errPrecondition))
	outer := fmt.Errorf("delete address: %w", inner)

	lane, _, ok := refusal.LaneOf(outer)
	require.True(t, ok, "полоса потерялась под чужой обёрткой")
	require.Equal(t, "REFERENCE_IN_USE", lane.Token())
}

// Attach отдаёт КОД, который ему передали, и НЕ подменяет его кодом полосы.
//
// Отрицательная половина здесь и есть предмет: код принадлежит классификации
// сервиса, и второй владелец кода разошёлся бы с первым на отказе, который
// полосой помечен неверно.
func TestAttachKeepsTheClassifierCode(t *testing.T) {
	t.Parallel()

	src := refusal.Wrap(refusal.ReferredUnnamed, refusal.Ref{}, fmt.Errorf("%w: Subnet has dependent resources", errPrecondition))

	// ПОЛОЖИТЕЛЬНАЯ ПОЛОВИНА: штатный код предусловия доезжает вместе с признаком.
	got, ok := refusal.Attach(src, refusal.Ref{Service: "vpc"}, refusal.Code, "Subnet has dependent resources")
	require.True(t, ok)
	require.Equal(t, codes.FailedPrecondition, status.Code(got))
	info, has := reasonOf(t, got)
	require.True(t, has, "признак не доехал в детали ответа")
	require.Equal(t, "REFERENCE_IN_USE", info.GetReason())
	require.Equal(t, "unnamed", info.GetMetadata()["reference_kind"],
		"вид ссылки обязан доехать метаданными: три полосы делят токен, и по токену их не различить")
	require.Equal(t, "vpc.kacho.cloud", info.GetDomain())

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: передали чужой код — он и уедет, полоса его не
	// перебивает.
	other, ok := refusal.Attach(src, refusal.Ref{Service: "vpc"}, codes.Aborted, "retry")
	require.True(t, ok)
	require.Equal(t, codes.Aborted, status.Code(other), "полоса подменила код классификации")
}

// Отказ без полосы Attach не трогает: ok=false, разбор идёт общим путём.
func TestAttachIsSilentOnAnUnlanedRefusal(t *testing.T) {
	t.Parallel()

	_, ok := refusal.Attach(fmt.Errorf("%w: network is not empty", errPrecondition),
		refusal.Ref{Service: "vpc"}, refusal.Code, "network is not empty")
	require.False(t, ok, "Attach выдал признак там, где производитель полосы не называл")

	_, ok = refusal.Attach(nil, refusal.Ref{Service: "vpc"}, refusal.Code, "")
	require.False(t, ok, "Attach выдал признак на отсутствующем отказе")
}

// Координата достраивается классификатором ТОЛЬКО там, где производитель её не
// назвал: сервис знает свой домен, репозиторий знает вид и идентификатор.
func TestFallbackRefFillsOnlyWhatTheProducerLeftEmpty(t *testing.T) {
	t.Parallel()

	src := refusal.Wrap(refusal.ReferredTo,
		refusal.Ref{ResourceType: "address", ResourceID: "addr-1"},
		fmt.Errorf("%w: address addr-1 is in use", errPrecondition))

	got, ok := refusal.Attach(src,
		refusal.Ref{Service: "vpc", ResourceType: "подмена", ResourceID: "подмена"},
		refusal.Code, "address addr-1 is in use")
	require.True(t, ok)

	info, has := reasonOf(t, got)
	require.True(t, has)
	require.Equal(t, "vpc.kacho.cloud", info.GetDomain(), "домен не достроен там, где производитель его не знал")
	require.Equal(t, "address", info.GetMetadata()["resource_type"], "координата производителя подменена запасной")
	require.Equal(t, "addr-1", info.GetMetadata()["resource_id"], "координата производителя подменена запасной")
}

// Пустая координата НЕ едет пустым значением: ключ с пустой строкой читается как
// «идентификатор известен и пуст».
func TestEmptyCoordinateDoesNotTravelAsAnEmptyValue(t *testing.T) {
	t.Parallel()

	err := refusal.Protected.Errf(refusal.Ref{Service: "nlb"}, "load balancer has deletion protection enabled")
	info, has := reasonOf(t, err)
	require.True(t, has)
	require.NotContains(t, info.GetMetadata(), "resource_id")
	require.NotContains(t, info.GetMetadata(), "resource_type")
	require.Equal(t, "DELETION_PROTECTED", info.GetReason())
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "load balancer has deletion protection enabled", status.Convert(err).Message())
}

// Errf и Attach кладут признак ОДИНАКОВО: один и тот же факт, отданный
// предпроверкой и запасным путём БД, обязан приходить клиенту одной формой.
func TestBothEntryPointsProduceTheSameShape(t *testing.T) {
	t.Parallel()

	const msg = "NetworkLoadBalancer lb-1 has listener(s); delete first"
	ref := refusal.Ref{Service: "nlb", ResourceType: "load_balancer", ResourceID: "lb-1"}

	sync := refusal.HoldsChildren.Errf(ref, "%s", msg)
	async, ok := refusal.Attach(
		refusal.Wrap(refusal.HoldsChildren, refusal.Ref{ResourceType: "load_balancer", ResourceID: "lb-1"},
			fmt.Errorf("%w: "+msg, errPrecondition)),
		refusal.Ref{Service: "nlb"}, refusal.Code, msg)
	require.True(t, ok)

	si, sok := reasonOf(t, sync)
	ai, aok := reasonOf(t, async)
	require.True(t, sok)
	require.True(t, aok)
	require.Equal(t, si.GetReason(), ai.GetReason())
	require.Equal(t, si.GetDomain(), ai.GetDomain())
	require.Equal(t, si.GetMetadata(), ai.GetMetadata())
	require.Equal(t, status.Code(sync), status.Code(async))
	require.Equal(t, status.Convert(sync).Message(), status.Convert(async).Message())
}

// Три полосы делят ОДИН токен — и обязаны различаться метаданными, иначе
// объединение токенов уничтожило бы то самое различие, ради которого словарь и
// заводится.
//
// Проба парная: положительная половина требует, чтобы вид ссылки доехал и был
// РАЗНЫМ у трёх полос; отрицательная — чтобы у защиты от удаления его не было
// вовсе. Без второй «вид ссылки есть» зеленело бы и на полосе, где ссылок нет ни
// одной, то есть признак сообщал бы о ссылке, которой не существует.
func TestLanesSharingATokenAreToldApartByMetadata(t *testing.T) {
	t.Parallel()

	kinds := map[string]string{}
	for _, l := range []refusal.Lane{refusal.HoldsChildren, refusal.ReferredTo, refusal.ReferredUnnamed} {
		err := l.Errf(refusal.Ref{Service: "vpc"}, "x")
		info, ok := reasonOf(t, err)
		require.True(t, ok)
		require.Equal(t, "REFERENCE_IN_USE", info.GetReason(),
			"полоса ссылки обязана называться токеном службы доступа, а не своим")
		kind := info.GetMetadata()["reference_kind"]
		require.NotEmptyf(t, kind, "полоса %s не назвала вид ссылки", l)
		require.Emptyf(t, kinds[kind], "вид ссылки %q объявлен двумя полосами: %s и %s",
			kind, kinds[kind], l)
		kinds[kind] = l.String()
	}
	require.Len(t, kinds, 3, "три полосы одного токена обязаны дать три разных вида ссылки")

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: у защиты от удаления зависимых нет ни одного.
	protected := refusal.Protected.Errf(refusal.Ref{Service: "nlb"}, "x")
	info, ok := reasonOf(t, protected)
	require.True(t, ok)
	require.Equal(t, "DELETION_PROTECTED", info.GetReason())
	require.NotContains(t, info.GetMetadata(), "reference_kind",
		"признак назвал вид ссылки там, где ссылок нет вовсе")
	require.Empty(t, refusal.Protected.ReferenceKind())

	t.Logf("осмотрено: полос с общим токеном 3, видов ссылки %d", len(kinds))
}
