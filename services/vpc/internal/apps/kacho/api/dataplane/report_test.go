// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeRecorder — запись подтверждения в памяти.
type fakeRecorder struct {
	calls  []ApplyReport
	result ApplyRecord
	err    error
}

func (f *fakeRecorder) Record(_ context.Context, rep ApplyReport) (ApplyRecord, error) {
	f.calls = append(f.calls, rep)
	return f.result, f.err
}

func aReport() ApplyReport {
	return ApplyReport{ResourceID: "net12345678901234567", Revision: 7, Outcome: OutcomeApplied}
}

// Подтверждение, чья форма невыразима, отвергается СИНХРОННО и ДО обращения к
// хранилищу.
//
// Проверяются оба свойства: код с текстом (тексты отказов — часть контракта) И
// то, что хранилище не спрашивали. Второе не косметика: дойдя до записи, отказ
// стал бы зависеть от доступности базы, а вызывающий получил бы `UNAVAILABLE`
// на ввод, который валидным не станет никогда.
func TestMalformedReportIsRefusedBeforeTheStore(t *testing.T) {
	cases := []struct {
		name string
		rep  ApplyReport
		want string
	}{
		{
			name: "без идентификатора",
			rep:  ApplyReport{Revision: 7, Outcome: OutcomeApplied},
			want: "resource_id: required",
		},
		{
			name: "идентификатор не той формы",
			rep:  ApplyReport{ResourceID: "не-идентификатор", Revision: 7, Outcome: OutcomeApplied},
			want: "invalid dataplane intent id 'не-идентификатор'",
		},
		{
			name: "ревизия не положительна",
			rep:  ApplyReport{ResourceID: "net12345678901234567", Revision: 0, Outcome: OutcomeApplied},
			want: "Illegal argument revision",
		},
		{
			name: "исход не назван",
			rep:  ApplyReport{ResourceID: "net12345678901234567", Revision: 7},
			want: "Illegal argument outcome",
		},
		{
			name: "успех несёт причину неуспеха",
			rep: ApplyReport{ResourceID: "net12345678901234567", Revision: 7,
				Outcome: OutcomeApplied, Reason: ReasonCapacity},
			want: "Illegal argument reason",
		},
		{
			name: "отказ без класса причины",
			rep: ApplyReport{ResourceID: "net12345678901234567", Revision: 7,
				Outcome: OutcomeFailed},
			want: "Illegal argument reason",
		},
		{
			name: "класс причины вне словаря",
			rep: ApplyReport{ResourceID: "net12345678901234567", Revision: 7,
				Outcome: OutcomeFailed, Reason: FailureReason("host eth0 is down")},
			want: "Illegal argument reason",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &fakeRecorder{}
			uc := NewReportAppliedUseCase(rec, quietObserver())

			_, err := uc.Record(context.Background(), c.rep)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok, "отказ не является gRPC-статусом")
			assert.Equal(t, codes.InvalidArgument, st.Code())
			assert.Equal(t, c.want, st.Message())
			assert.Empty(t, rec.calls, "хранилище спрошено до того, как форма запроса признана выразимой")
		})
	}
}

// Положительный контроль: выразимое подтверждение доезжает до хранилища без
// изменений.
//
// Без него проба выше зеленела бы на реализации, отвергающей вообще всё.
func TestWellFormedReportReachesTheStoreUnchanged(t *testing.T) {
	rec := &fakeRecorder{result: ApplyRecord{Recorded: true, CurrentRevision: 7}}
	uc := NewReportAppliedUseCase(rec, quietObserver())

	got, err := uc.Record(context.Background(), aReport())

	require.NoError(t, err)
	assert.True(t, got.Recorded)
	assert.Equal(t, int64(7), got.CurrentRevision)
	require.Len(t, rec.calls, 1)
	assert.Equal(t, aReport(), rec.calls[0])
}

// Отказ с законным классом причины проходит — все шесть классов словаря.
func TestEveryFailureClassIsAccepted(t *testing.T) {
	for _, reason := range []FailureReason{
		ReasonCapacity, ReasonConflict, ReasonUnsupported,
		ReasonDependencyNotReady, ReasonTransient, ReasonExecutorInternal,
	} {
		t.Run(string(reason), func(t *testing.T) {
			rec := &fakeRecorder{result: ApplyRecord{Recorded: true, CurrentRevision: 7}}
			uc := NewReportAppliedUseCase(rec, quietObserver())
			rep := ApplyReport{ResourceID: "net12345678901234567", Revision: 7,
				Outcome: OutcomeFailed, Reason: reason}

			_, err := uc.Record(context.Background(), rep)

			require.NoError(t, err)
			require.Len(t, rec.calls, 1)
		})
	}
}

// Намерения нет — своё отсутствие называется своим кодом и контрактным тоном.
func TestUnknownIntentIsNotFound(t *testing.T) {
	rec := &fakeRecorder{err: ErrIntentUnknown}
	uc := NewReportAppliedUseCase(rec, quietObserver())

	_, err := uc.Record(context.Background(), aReport())

	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "Dataplane intent net12345678901234567 not found", st.Message())
}

// Ревизия, которой платформа не выдавала, — состояние, а не формат.
func TestRevisionNeverIssuedIsAFailedPrecondition(t *testing.T) {
	rec := &fakeRecorder{err: ErrRevisionNotIssued}
	uc := NewReportAppliedUseCase(rec, quietObserver())

	_, err := uc.Record(context.Background(), aReport())

	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Equal(t, "Dataplane intent net12345678901234567 was never issued revision 7", st.Message())
}

// Устаревшее подтверждение — НЕ отказ, но и не тишина: оно объявлено
// незасчитанным и попадает в наблюдаемость.
//
// Отказом это быть не может: гонка «применил N, платформа успела выдать N+1»
// штатна, и называть отказом нормальный ход работы значит приучить исполнителя
// не читать отказы.
func TestStaleReportIsCountedAsStaleNotAsFresh(t *testing.T) {
	obs := quietObserver()
	rec := &fakeRecorder{result: ApplyRecord{Recorded: false, CurrentRevision: 9}}
	uc := NewReportAppliedUseCase(rec, obs)

	got, err := uc.Record(context.Background(), aReport())

	require.NoError(t, err)
	assert.False(t, got.Recorded, "устаревшее подтверждение засчитано за свежее")
	assert.Equal(t, int64(9), got.CurrentRevision)
	assert.Equal(t, int64(1), obs.Totals().StaleReports,
		"устаревшее подтверждение не попало в наблюдаемость — отличить его от свежего нечем")
}
