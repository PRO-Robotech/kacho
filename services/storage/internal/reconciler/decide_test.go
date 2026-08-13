// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
)

// Решение сверщика — самая опасная часть системы: оно приводит к созданию и
// удалению данных. Поэтому оно вынесено в чистую функцию и проверяется таблицей, а
// не через живую базу, где такие случаи гонялись бы редко и дорого.
//
// Приёмка STOR-P-24, 25, 28, 31, 33, 35.

func obs(state blockbackend.ObservedState, size int64) blockbackend.Observed {
	return blockbackend.Observed{State: state, SizeBytes: size}
}

func TestDecide(t *testing.T) {
	t.Parallel()

	const want, got = 100, 50

	cases := map[string]struct {
		item reconciler.Item
		want reconciler.Action
		why  string
	}{
		"создаётся, объекта нет — создать": {
			item: reconciler.Item{Desired: domain.VolumeStatusCreating, Observed: obs(blockbackend.ObservedAbsent, 0)},
			want: reconciler.ActionProvision,
		},
		"создаётся, объект на месте — объявить готовым": {
			item: reconciler.Item{Desired: domain.VolumeStatusCreating, Observed: obs(blockbackend.ObservedReady, want)},
			want: reconciler.ActionConfirm,
			why:  "намерение исполнено — готовность объявляет ФАКТ, а не наша строка",
		},
		"создаётся, объект неисправен — не пересоздавать": {
			item: reconciler.Item{Desired: domain.VolumeStatusCreating, Observed: obs(blockbackend.ObservedError, 0)},
			want: reconciler.ActionReportVanished,
			why:  "под именем уже лежат чьи-то данные — «починка» стёрла бы их",
		},
		"удаляется, объект жив — снять объект": {
			item: reconciler.Item{Desired: domain.VolumeStatusDeleting, Observed: obs(blockbackend.ObservedReady, want)},
			want: reconciler.ActionRemove,
		},
		"удаляется, объекта уже нет — забыть строку": {
			item: reconciler.Item{Desired: domain.VolumeStatusDeleting, Observed: obs(blockbackend.ObservedAbsent, 0)},
			want: reconciler.ActionForget,
			why:  "строка живёт ДОЛЬШЕ объекта: иначе крах между шагами оставил бы ёмкость без записи",
		},
		"готов, объект пропал — объявить ошибкой, а не пересоздавать": {
			item: reconciler.Item{Desired: domain.VolumeStatusAvailable, Observed: obs(blockbackend.ObservedAbsent, 0)},
			want: reconciler.ActionReportVanished,
			why:  "данные не вернутся, а пустой объект того же имени выглядел бы здоровым ресурсом",
		},
		"привязан, объект пропал — то же самое": {
			item: reconciler.Item{Desired: domain.VolumeStatusInUse, Observed: obs(blockbackend.ObservedAbsent, 0)},
			want: reconciler.ActionReportVanished,
		},
		"готов, размер не доехал — довести": {
			item: reconciler.Item{
				Desired: domain.VolumeStatusAvailable, DesiredSizeBytes: want,
				Observed: obs(blockbackend.ObservedReady, got),
			},
			want: reconciler.ActionResize,
		},
		"готов, размер сошёлся — не трогать": {
			item: reconciler.Item{
				Desired: domain.VolumeStatusAvailable, DesiredSizeBytes: want,
				Observed: obs(blockbackend.ObservedReady, want),
			},
			want: reconciler.ActionNone,
		},
		"готов, объект БОЛЬШЕ желаемого — не трогать": {
			item: reconciler.Item{
				Desired: domain.VolumeStatusAvailable, DesiredSizeBytes: got,
				Observed: obs(blockbackend.ObservedReady, want),
			},
			want: reconciler.ActionNone,
			why:  "уменьшение контракт не принимает, и сверщик не вправе его изобретать",
		},
		"ошибочный, объект здоров — вернуть в строй": {
			item: reconciler.Item{Desired: domain.VolumeStatusError, Observed: obs(blockbackend.ObservedReady, want)},
			want: reconciler.ActionConfirm,
			why:  "ошибка была наблюдением, а не приговором",
		},
		"ошибочный, объекта нет — оставить как есть": {
			item: reconciler.Item{Desired: domain.VolumeStatusError, Observed: obs(blockbackend.ObservedAbsent, 0)},
			want: reconciler.ActionNone,
		},
	}

	for name, tc := range cases {
		if got := reconciler.Decide(tc.item); got != tc.want {
			t.Errorf("%s: решение %s, ожидалось %s. %s", name, got, tc.want, tc.why)
		}
	}
}

// Неустановленное состояние обязано останавливать сверщика ВСЕГДА, при любом
// намерении. Это отдельная проба, а не строка таблицы: перебор идёт по всем
// намерениям, и слив её в общую таблицу оставил бы дыру именно там, где она опаснее
// всего — при намерении удалить.
func TestDecide_UnknownNeverActs(t *testing.T) {
	t.Parallel()

	for _, desired := range []domain.VolumeStatus{
		domain.VolumeStatusCreating, domain.VolumeStatusAvailable,
		domain.VolumeStatusInUse, domain.VolumeStatusDeleting, domain.VolumeStatusError,
	} {
		it := reconciler.Item{Desired: desired, Observed: obs(blockbackend.ObservedUnknown, 0)}
		if got := reconciler.Decide(it); got != reconciler.ActionWait {
			t.Errorf("намерение %v при неустановленном состоянии дало %s: молчание бэкенда не является "+
				"утверждением ни об отсутствии объекта, ни о его наличии", desired, got)
		}
	}

	// Положительный контроль: то же намерение при УСТАНОВЛЕННОМ состоянии
	// действие производит — иначе проба зеленела бы на сверщике, не делающем
	// вообще ничего.
	live := reconciler.Item{Desired: domain.VolumeStatusCreating, Observed: obs(blockbackend.ObservedAbsent, 0)}
	if got := reconciler.Decide(live); got != reconciler.ActionProvision {
		t.Fatalf("контроль: при установленном состоянии сверщик обязан действовать, получено %s", got)
	}
}

func TestReasonFor_ClosedVocabulary(t *testing.T) {
	t.Parallel()

	cases := map[blockbackend.Outcome]domain.StatusReason{
		blockbackend.OutcomeUnavailable:       domain.ReasonBackendUnavailable,
		blockbackend.OutcomeCapacityExhausted: domain.ReasonBackendCapacityExhausted,
		blockbackend.OutcomeRejected:          domain.ReasonBackendRejected,
		blockbackend.OutcomeConflict:          domain.ReasonBackendRejected,
		blockbackend.OutcomeDenied:            domain.ReasonBackendRejected,
		blockbackend.OutcomeMisconfigured:     domain.ReasonBackendRejected,
		blockbackend.OutcomeNotFound:          domain.ReasonPreconditionFailed,
		blockbackend.OutcomeUnclassified:      domain.ReasonInternalError,
		blockbackend.Outcome(99):              domain.ReasonInternalError,
	}
	for outcome, wantReason := range cases {
		got := reconciler.ReasonFor(outcome)
		if got != wantReason {
			t.Errorf("исход %s дал причину %q, ожидалась %q", outcome, got, wantReason)
		}
		if !got.Valid() {
			t.Errorf("исход %s дал причину вне словаря: %q", outcome, got)
		}
	}
}

func TestTerminal_OnlyUnavailableRepeats(t *testing.T) {
	t.Parallel()

	if reconciler.Terminal(blockbackend.OutcomeUnavailable) {
		t.Error("недоступность обязана повторяться: она проходит сама")
	}
	for _, o := range []blockbackend.Outcome{
		blockbackend.OutcomeRejected, blockbackend.OutcomeCapacityExhausted,
		blockbackend.OutcomeConflict, blockbackend.OutcomeDenied,
		blockbackend.OutcomeMisconfigured, blockbackend.OutcomeUnclassified,
	} {
		if !reconciler.Terminal(o) {
			t.Errorf("исход %s обязан быть терминальным: бесконечный повтор терминального отказа — "+
				"очередь, у которой голова не проходит никогда", o)
		}
	}
}
