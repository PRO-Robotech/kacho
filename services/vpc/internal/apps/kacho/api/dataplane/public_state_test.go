// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aRow — пара «намерение ↔ подтверждение» в форме, которую отдаёт база.
func aRow() ApplyReportRow {
	return ApplyReportRow{
		IntentRevision:  7,
		Reported:        true,
		AppliedRevision: 7,
		Outcome:         OutcomeApplied,
	}
}

// Арендатор ВИДИТ отказ исполнителя — вместе с классом причины.
//
// Это предмет всей проекции: подтверждение отказа лежало в базе и не доходило до
// того, кого касается. Проба утверждает ОБА поля: без класса «не применено»
// неотличимо от «ещё не дошли руки», и арендатору нечего делать с таким ответом.
func TestRefusalOnTheCurrentRevisionIsVisibleWithItsClass(t *testing.T) {
	for _, reason := range []FailureReason{
		ReasonCapacity, ReasonConflict, ReasonUnsupported,
		ReasonDependencyNotReady, ReasonTransient, ReasonExecutorInternal,
	} {
		t.Run(string(reason), func(t *testing.T) {
			row := aRow()
			row.Outcome = OutcomeFailed
			row.Reason = reason

			got, err := row.PublicState()
			require.NoError(t, err)
			assert.False(t, got.Applied, "отказ прочитан как применённое намерение")
			assert.Equal(t, reason, got.Reason, "класс причины до арендатора не доехал")
		})
	}
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к пробе выше: применённое намерение не несёт ничего
// лишнего.
//
// Без него отрицание зеленело бы на проекции, которая ВСЕГДА говорит «не
// применено с классом»: она отвечала бы «отказ виден» на каждом входе.
func TestAppliedCurrentRevisionShowsNothingBeyondTheFact(t *testing.T) {
	got, err := aRow().PublicState()
	require.NoError(t, err)
	assert.True(t, got.Applied, "подтверждённое применение прочитано как неприменённое")
	assert.Equal(t, ReasonNone, got.Reason, "у успеха появилась причина неуспеха")
}

// Подтверждение о ПРЕЖНЕЙ ревизии не выдаётся за состояние текущей — ни как
// успех, ни как отказ.
//
// Оба направления существенны, и второе тоньше первого:
//
//   - устаревший УСПЕХ, показанный как «применено», обещает арендатору, что его
//     правка доехала, тогда как она даже не бралась в работу;
//   - устаревший ОТКАЗ, показанный с классом, приписывает НОВОМУ намерению
//     причину, которой у него нет: арендатор чинил бы то, что уже починил.
func TestReportAboutAnEarlierRevisionIsNotTheStateOfTheCurrentOne(t *testing.T) {
	t.Run("устаревший успех", func(t *testing.T) {
		row := aRow()
		row.IntentRevision = 9
		row.AppliedRevision = 7

		got, err := row.PublicState()
		require.NoError(t, err)
		assert.False(t, got.Applied, "успех о прежней ревизии выдан за состояние текущей")
		assert.Equal(t, ReasonNone, got.Reason)
	})

	t.Run("устаревший отказ", func(t *testing.T) {
		row := aRow()
		row.IntentRevision = 9
		row.AppliedRevision = 7
		row.Outcome = OutcomeFailed
		row.Reason = ReasonCapacity

		got, err := row.PublicState()
		require.NoError(t, err)
		assert.False(t, got.Applied)
		assert.Equal(t, ReasonNone, got.Reason,
			"класс прежнего отказа приписан намерению, о котором исполнитель ещё не отчитывался")
	})
}

// Намерение, о котором исполнитель не отчитывался вовсе, — «не применено» БЕЗ
// класса: причины у него нет, и придумывать её нельзя.
func TestIntentWithoutAnyReportIsNotAppliedAndHasNoClass(t *testing.T) {
	row := aRow()
	row.Reported = false
	row.AppliedRevision = 0
	row.Outcome = ""

	got, err := row.PublicState()
	require.NoError(t, err)
	assert.False(t, got.Applied)
	assert.Equal(t, ReasonNone, got.Reason)
}

// Значение вне закрытого словаря — ОТКАЗ, а не корзина «прочее».
//
// Словарь закрыт CHECK-ограничением базы, поэтому попасть сюда такое значение
// может ровно одним способом: словарь базы разошёлся с этим кодом. Молча
// свернув расхождение в «не применено», проекция сообщала бы арендатору
// правдоподобную неправду и пережила бы собственный предмет.
func TestValueOutsideTheClosedDictionaryIsRefusedNotBucketed(t *testing.T) {
	cases := map[string]ApplyReportRow{
		"исход вне словаря": {
			IntentRevision: 7, Reported: true, AppliedRevision: 7,
			Outcome: ApplyOutcome("REBOOTED"),
		},
		"класс вне словаря": {
			IntentRevision: 7, Reported: true, AppliedRevision: 7,
			Outcome: OutcomeFailed, Reason: FailureReason("CABLE_UNPLUGGED"),
		},
		"отказ без класса": {
			IntentRevision: 7, Reported: true, AppliedRevision: 7,
			Outcome: OutcomeFailed, Reason: ReasonNone,
		},
		"успех с классом": {
			IntentRevision: 7, Reported: true, AppliedRevision: 7,
			Outcome: OutcomeApplied, Reason: ReasonCapacity,
		},
		"ревизия намерения неположительна": {
			IntentRevision: 0, Reported: true, AppliedRevision: 7,
			Outcome: OutcomeApplied,
		},
		"ревизия подтверждения неположительна": {
			IntentRevision: 7, Reported: true, AppliedRevision: 0,
			Outcome: OutcomeApplied,
		},
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := row.PublicState()
			require.Error(t, err, "негодная строка прошла как состояние")
			assert.ErrorIs(t, err, ErrApplyRowShape)
		})
	}
}

// Строка, годная по форме, отказом не отвечает — положительный контроль к пробе
// выше. Без него отказ на КАЖДОМ входе выглядел бы исполнением правила.
func TestWellFormedRowIsNotRefused(t *testing.T) {
	_, err := aRow().PublicState()
	assert.NoError(t, err)
}

// Каждый класс закрытого словаря доезжает до арендатора — ни один не выпадает
// молча.
//
// Перечень берётся из ЕДИНОГО источника (`KnownFailureReasons`), а не
// выписывается здесь: выписанный, он пережил бы добавление класса и проба
// осталась бы зелёной, ничего о новом классе не утверждая.
func TestEveryClassOfTheClosedDictionaryReachesTheTenant(t *testing.T) {
	require.NotEmpty(t, KnownFailureReasons, "словарь классов пуст — проверять нечего")
	for _, reason := range KnownFailureReasons {
		row := aRow()
		row.Outcome = OutcomeFailed
		row.Reason = reason

		got, err := row.PublicState()
		require.NoError(t, err, "класс %q не принят", reason)
		assert.Equal(t, reason, got.Reason, "класс %q подменён при проекции", reason)
	}
}

// Публичная проекция несёт РОВНО две вещи и ни одной больше.
//
// Гейт против расширения по невнимательности: имя узла, интерфейса, туннеля или
// код ядра сюда добавляются одной строкой и не ловятся ничем — публичное поле, по
// которому опознаётся конкретная реализация фабрики, есть дефект дизайна, а не
// подробность. Проба падает на ЛЮБОМ третьем поле и называет его.
func TestPublicProjectionCarriesExactlyTwoFacts(t *testing.T) {
	typ := reflect.TypeOf(PublicApplyState{})
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
	}
	assert.Equal(t, []string{"Applied", "Reason"}, names,
		"состав публичной проекции изменился — см. требование про инфра-чувствительные данные")
}
