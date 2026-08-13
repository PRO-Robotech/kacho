// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package realization

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

const someInstance = "ins-abcdefghjkmnpqrst"

type fakeObserved struct {
	got     repo.ObservedReport
	called  bool
	applied bool
	current int64
	err     error
}

func (f *fakeObserved) ApplyObservedState(_ context.Context, rep repo.ObservedReport) (bool, int64, error) {
	f.got, f.called = rep, true
	return f.applied, f.current, f.err
}

func report() Report {
	return Report{
		InstanceID: someInstance,
		State:      "OBSERVED_RUNNING",
		SequenceNo: 7,
		ObservedAt: time.Now().Add(-time.Minute),
	}
}

// TestApply_RejectsWhatWouldBecomeAStorageError — значения, которых база не
// примет, отвергаются здесь и по имени поля.
//
// Каждое отрицание идёт в паре с положительным контролем в конце: без него
// «отвергнуто» неотличимо от «отвергается всё», и проба зеленела бы на приёме,
// который не принимает ни одного законного отчёта.
func TestApply_RejectsWhatWouldBecomeAStorageError(t *testing.T) {
	cases := []struct {
		name  string
		mutFn func(*Report)
		field string
	}{
		{"состояние вне закрытого набора", func(r *Report) { r.State = "OBSERVED_ЧТО_ТО" }, "observedState"},
		{"состояние не названо вовсе", func(r *Report) { r.State = "" }, "observedState"},
		{"номер события нулевой", func(r *Report) { r.SequenceNo = 0 }, "deliverySequenceNo"},
		{"номер события отрицательный", func(r *Report) { r.SequenceNo = -1 }, "deliverySequenceNo"},
		{"время наблюдения не названо", func(r *Report) { r.ObservedAt = time.Time{} }, "observedAt"},
		{"время наблюдения из будущего", func(r *Report) { r.ObservedAt = time.Now().Add(time.Hour) }, "observedAt"},
		{"пояснение сверх предела", func(r *Report) { r.Reason = strings.Repeat("я", maxReasonLen+1) }, "reason"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := report()
			tc.mutFn(&rep)
			fake := &fakeObserved{}
			_, err := NewService(fake).Apply(context.Background(), rep)
			if err == nil {
				t.Fatal("отчёт обязан быть отвергнут")
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Errorf("код = %v, ожидался InvalidArgument", status.Code(err))
			}
			if !strings.Contains(status.Convert(err).Message(), tc.field) {
				t.Errorf("сообщение %q не называет поле %q — узлу нечего исправлять",
					status.Convert(err).Message(), tc.field)
			}
			if fake.called {
				t.Error("отвергнутый отчёт не должен доезжать до хранилища")
			}
		})
	}

	// Положительный контроль: законный отчёт доезжает целиком и без подмен.
	t.Run("законный отчёт доезжает как прислан", func(t *testing.T) {
		fake := &fakeObserved{applied: true, current: 7}
		rep := report()
		res, err := NewService(fake).Apply(context.Background(), rep)
		if err != nil {
			t.Fatalf("законный отчёт обязан пройти, получено %v", err)
		}
		if !fake.called {
			t.Fatal("законный отчёт обязан доехать до хранилища")
		}
		if !fake.got.ObservedAt.Equal(rep.ObservedAt) {
			t.Errorf("время наблюдения подменено: было %v, доехало %v — подмена стирает "+
				"единственный след потери связи", rep.ObservedAt, fake.got.ObservedAt)
		}
		if !res.Applied || res.CurrentSeq != 7 {
			t.Errorf("исход приёма искажён: %+v", res)
		}
	})
}

// TestApply_UnknownDeliveryIsStateNotInput — отчёт о неотправленном намерении
// отвечает состоянием, а не разбором ввода.
//
// То же самое поле в другой момент было бы законным, поэтому это не
// InvalidArgument: вызывающему нечего исправлять в запросе.
func TestApply_UnknownDeliveryIsStateNotInput(t *testing.T) {
	fake := &fakeObserved{err: repo.ErrUnknownDelivery}
	_, err := NewService(fake).Apply(context.Background(), report())
	if err == nil {
		t.Fatal("отчёт о неотправленном намерении обязан быть отвергнут")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("код = %v, ожидался FailedPrecondition", status.Code(err))
	}

	// Положительный контроль: без этой ошибки хранилища тот же отчёт проходит.
	if _, err := NewService(&fakeObserved{applied: true}).Apply(context.Background(), report()); err != nil {
		t.Fatalf("тот же отчёт без расхождения обязан пройти, получено %v", err)
	}
}

// TestApply_StaleIsAcceptedNotRefused — устаревший отчёт не является отказом.
//
// Узел не обязан знать, что его обогнал более свежий отчёт, и повторять ему
// нечего. Отказ здесь заставил бы агента повторять то, что уже неактуально.
func TestApply_StaleIsAcceptedNotRefused(t *testing.T) {
	fake := &fakeObserved{applied: false, current: 9}
	res, err := NewService(fake).Apply(context.Background(), report())
	if err != nil {
		t.Fatalf("устаревший отчёт — принят и отброшен, а не отказ; получено %v", err)
	}
	if res.Applied {
		t.Error("устаревший отчёт не может отчитаться применением")
	}
	if res.CurrentSeq != 9 {
		t.Errorf("действующий номер = %d, ожидался 9 — по нему агент узнаёт, что отстал", res.CurrentSeq)
	}
}

// TestApply_MalformedInstanceIDRejectedFirst — непригодный идентификатор машины
// отвергается разбором, а не превращается в «не найдено».
func TestApply_MalformedInstanceIDRejectedFirst(t *testing.T) {
	rep := report()
	rep.InstanceID = "не идентификатор"
	fake := &fakeObserved{}
	if _, err := NewService(fake).Apply(context.Background(), rep); status.Code(err) != codes.InvalidArgument {
		t.Errorf("код = %v, ожидался InvalidArgument", status.Code(err))
	}
	if fake.called {
		t.Error("непригодный идентификатор не должен доезжать до хранилища")
	}
}
