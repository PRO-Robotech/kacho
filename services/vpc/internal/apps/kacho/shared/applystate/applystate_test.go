// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package applystate

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
)

// applystate_test.go — перевод проекции в поле контракта и поведение на
// отсутствии строки намерения.

// stubReader — подставной читатель проекции. Считает обращения: предмет
// отдельного утверждения — что страница стоит ОДНО обращение, а не по одному на
// строку.
type stubReader struct {
	states map[string]dataplane.PublicApplyState
	err    error
	calls  int
	sawIDs []string
}

func (s *stubReader) PublicApplyStates(_ context.Context, ids []string) (map[string]dataplane.PublicApplyState, error) {
	s.calls++
	s.sawIDs = append(s.sawIDs, ids...)
	if s.err != nil {
		return nil, s.err
	}
	return s.states, nil
}

// TestReasonMappingCoversTheWholeClosedDictionary — каждый класс закрытого
// словаря доезжает до арендатора НАЗВАННЫМ.
//
// Ветвь, потерянная в переводе, превращает названный класс в «класса нет»:
// арендатор видит «не применено» без причины там, где причина есть, и ждёт
// вместо того чтобы чинить. Обход идёт по единому источнику
// (`dataplane.KnownFailureReasons`), поэтому новый класс словаря роняет эту
// пробу сам — перечень здесь не выписан вторым списком.
func TestReasonMappingCoversTheWholeClosedDictionary(t *testing.T) {
	seen := map[vpcv1.ApplyFailureReason]bool{}
	for _, r := range dataplane.KnownFailureReasons {
		got := reasonToProto(r)
		assert.NotEqual(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED, got,
			"класс %q не имеет ветви перевода — он доедет до арендатора как «класса нет»", r)
		assert.False(t, seen[got], "класс %q переведён в уже занятое значение %s — два класса схлопнулись в один", r, got)
		seen[got] = true
	}
	assert.Len(t, seen, len(dataplane.KnownFailureReasons),
		"переведённых значений меньше, чем классов словаря")

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: «класса нет» остаётся «класса нет». Без неё
	// проверка выше зеленела бы и на переводе, возвращающем что угодно ненулевое.
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED,
		reasonToProto(dataplane.ReasonNone),
		"отсутствие класса обязано оставаться отсутствием, а не становиться классом")
}

// TestOneFillsWhatTheProjectionSaidAndNothingElse — единичное чтение.
func TestOneFillsWhatTheProjectionSaidAndNothingElse(t *testing.T) {
	r := &stubReader{states: map[string]dataplane.PublicApplyState{
		"net-live": {Applied: false, Reason: dataplane.ReasonCapacity},
	}}
	missing := 0
	f := NewFiller(r, func() { missing++ })

	got, err := f.One(context.Background(), "net-live")
	require.NoError(t, err)
	require.NotNil(t, got, "о живом объекте проекция сказала — поле обязано быть заполнено")
	assert.False(t, got.GetApplied())
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CAPACITY, got.GetReason())
	assert.Zero(t, missing, "счётчик отсутствия сработал на объекте, о котором сказано")

	// Объект, о котором проекция ничего не сказала: поле НЕ заполняется, чтение
	// успешно, факт наблюдаем.
	got, err = f.One(context.Background(), "net-gone")
	require.NoError(t, err, "снятое намерение — штатная гонка удаления, а не отказ чтения")
	assert.Nil(t, got, "«утверждения нет» выражается отсутствием, а не правдоподобным «не применено»")
	assert.Equal(t, 1, missing, "отсутствие строки намерения обязано быть наблюдаемым")
}

// TestPageCostsOneReadRegardlessOfPageSize — стоимость страницы принадлежит
// запросу.
func TestPageCostsOneReadRegardlessOfPageSize(t *testing.T) {
	r := &stubReader{states: map[string]dataplane.PublicApplyState{
		"a": {Applied: true},
		"b": {Applied: false, Reason: dataplane.ReasonTransient},
	}}
	missing := 0
	f := NewFiller(r, func() { missing++ })

	got, err := f.Page(context.Background(), []string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, 1, r.calls, "страница обязана стоить ОДНО обращение, а не по одному на строку")
	assert.Len(t, got, 2, "в карту попадают только объекты, о которых проекция сказала")
	assert.True(t, got["a"].GetApplied())
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_TRANSIENT, got["b"].GetReason())
	assert.NotContains(t, got, "c")
	assert.Equal(t, 1, missing)

	// Пустая страница обращения не делает вовсе.
	r.calls = 0
	got, err = f.Page(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Zero(t, r.calls, "пустая страница — не «все», а «ни одного»: спрашивать нечего")
}

// TestReadFailureIsNotSwallowedAndDoesNotLeak — отказ проекции доезжает до
// вызывающего, и доезжает НЕПРОЗРАЧНЫМ.
//
// Две половины, и обе обязательны. Проглотив отказ, заполнитель отдал бы
// страницу с пустыми полями — то есть «утверждения нет» о каждом объекте, — и
// сломанная проекция выглядела бы как штатная гонка удаления на всём проекте
// разом. Пропустив текст отказа наружу, он отдал бы арендатору внутренности
// хранилища: утверждается ИМЕННО СООБЩЕНИЕ, а не только код, иначе рефактор
// вернул бы утечку при зелёной пробе.
func TestReadFailureIsNotSwallowedAndDoesNotLeak(t *testing.T) {
	const leaky = "dial tcp 10.0.0.7:5432: connect: connection refused"
	f := NewFiller(&stubReader{err: errors.New(leaky)}, nil)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"единичное чтение", func() error { _, e := f.One(context.Background(), "net-x"); return e }},
		{"страница", func() error { _, e := f.Page(context.Background(), []string{"net-x"}); return e }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err, "отказ проекции проглочен — сломанная проекция читалась бы как гонка удаления")
			assert.Equal(t, codes.Internal, status.Code(err))
			assert.NotContains(t, status.Convert(err).Message(), leaky,
				"текст хранилища уехал арендатору")
			assert.Equal(t, "internal database error", status.Convert(err).Message(),
				"текст отказа обязан быть фиксированным: переменный текст сам по себе есть канал наружу")
		})
	}
}

// TestNilFillerIsALegalInputAndSaysNothing — сборка без провязанной проекции.
//
// Утверждение здесь именно про МОЛЧАНИЕ: нулевой заполнитель отдаёт «утверждения
// нет», а не «не применено», и к базе не ходит. Провязку боевой сборки держит
// гейт по дереву, а не готовность этого типа падать.
func TestNilFillerIsALegalInputAndSaysNothing(t *testing.T) {
	var f *Filler
	got, err := f.One(context.Background(), "net-x")
	require.NoError(t, err)
	assert.Nil(t, got)

	page, err := f.Page(context.Background(), []string{"net-x"})
	require.NoError(t, err)
	assert.Empty(t, page)
}
