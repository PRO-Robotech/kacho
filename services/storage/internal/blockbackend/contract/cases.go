// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package contract

import (
	"context"
	"slices"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// giB — единица размера в случаях. Числа здесь произвольны и значения не несут; важно
// лишь их отношение друг к другу.
const giB int64 = 1 << 30

// mkVolume создаёт обычный объект и возвращает его адрес.
func (r *runner) mkVolume(t *testing.T, b blockbackend.Backend, name string, size int64) blockbackend.ObjectRef {
	t.Helper()
	ref := r.ref(name)
	mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{Ref: ref, SizeBytes: size}),
		"CreateVolume("+name+") — подготовка случая")
	return ref
}

// mkSnapshot снимает снимок с тома и возвращает адрес снимка.
func (r *runner) mkSnapshot(t *testing.T, b blockbackend.Backend, vol blockbackend.ObjectRef, name string) blockbackend.ObjectRef {
	t.Helper()
	snap := r.ref(name)
	mustOK(t, b.CreateSnapshot(t.Context(), vol, snap), "CreateSnapshot("+name+") — подготовка случая")
	return snap
}

// cases — полный перечень случаев суиты.
//
// Каждое отрицание стоит в паре с положительным контролем В ТОМ ЖЕ случае: иначе
// реализация, отвергающая любой ввод, прошла бы весь отрицательный набор.
func cases() []kase {
	return []kase{
		// ---------------------------------------------------------------- форма
		{name: "Kind/непусто-и-постоянно", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			first := b.Kind()
			if first == "" {
				t.Fatalf("Kind: пусто — вид бэкенда попадает в журнал и счётчики, " +
					"где безымянный неотличим от любого другого")
			}
			if second := b.Kind(); second != first {
				t.Fatalf("Kind: %q, затем %q — вид обязан быть константой реализации", first, second)
			}
		}},

		{name: "Capabilities/постоянны-между-вызовами", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			first, second := b.Capabilities(), b.Capabilities()
			if first != second {
				t.Fatalf("Capabilities: %+v, затем %+v — способности объявляются константами, "+
					"а не опрашиваются: меняющийся ответ означает, что решение о допустимости "+
					"операции зависит от момента вопроса", first, second)
			}
		}},

		// -------------------------------------------------- идемпотентность создания
		{name: "CreateVolume/повтор-с-теми-же-аргументами-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			spec := blockbackend.VolumeSpec{Ref: r.ref("vol-repeat"), SizeBytes: 4 * giB}
			mustOK(t, b.CreateVolume(t.Context(), spec), "CreateVolume первый раз")
			mustOK(t, b.CreateVolume(t.Context(), spec), "CreateVolume повтор с теми же аргументами")
			obs := observeState(t, b, spec.Ref, "Observe после повтора")
			if obs.State != blockbackend.ObservedReady || obs.SizeBytes != 4*giB {
				t.Fatalf("после повтора получено %s size=%d, ожидалось READY size=%d — "+
					"повтор обязан попадать в ТОТ ЖЕ объект, а не заводить второй",
					obs.State, obs.SizeBytes, 4*giB)
			}
		}},

		{name: "CreateVolume/повтор-с-другим-размером-конфликт", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.ref("vol-divergent")
			same := blockbackend.VolumeSpec{Ref: ref, SizeBytes: 4 * giB}
			mustOK(t, b.CreateVolume(t.Context(), same), "CreateVolume первый раз")
			// Положительный контроль: расходится именно размер, а не сам факт повтора.
			mustOK(t, b.CreateVolume(t.Context(), same), "CreateVolume повтор с тем же размером")

			other := blockbackend.VolumeSpec{Ref: ref, SizeBytes: 8 * giB}
			mustOutcome(t, b.CreateVolume(t.Context(), other), blockbackend.OutcomeConflict,
				"CreateVolume повтор с ДРУГИМ размером")

			obs := observeState(t, b, ref, "Observe после конфликта")
			if obs.SizeBytes != 4*giB {
				t.Fatalf("после конфликта размер %d, ожидался прежний %d — расходящийся повтор "+
					"обязан отказать, а не молча принять новое значение", obs.SizeBytes, 4*giB)
			}
		}},

		// --------------------------------------------------------- строгость ввода
		{name: "CreateVolume/пустой-пул-отвергнут", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			good := blockbackend.VolumeSpec{Ref: r.ref("vol-pool-ok"), SizeBytes: giB}
			mustOK(t, b.CreateVolume(t.Context(), good), "CreateVolume с заполненным пулом")

			bad := good
			bad.Ref.Pool = ""
			bad.Ref.Name = "vol-pool-empty"
			mustOutcome(t, b.CreateVolume(t.Context(), bad), blockbackend.OutcomeRejected,
				"CreateVolume с пустым пулом")
		}},

		{name: "CreateVolume/пустое-пространство-арендатора-отвергнуто", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			good := blockbackend.VolumeSpec{Ref: r.ref("vol-ns-ok"), SizeBytes: giB}
			mustOK(t, b.CreateVolume(t.Context(), good), "CreateVolume с заполненным пространством")

			// Пустое пространство арендатора — не косметика: без него все арендаторы
			// класса делят одно пространство имён у бэкенда, и ошибка в правах на его
			// стороне становится межарендной.
			bad := good
			bad.Ref.Namespace = ""
			bad.Ref.Name = "vol-ns-empty"
			mustOutcome(t, b.CreateVolume(t.Context(), bad), blockbackend.OutcomeRejected,
				"CreateVolume с пустым пространством арендатора")
		}},

		{name: "CreateVolume/пустое-имя-объекта-отвергнуто", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-name-ok"), SizeBytes: giB}), "CreateVolume с именем")
			mustOutcome(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref(""), SizeBytes: giB}), blockbackend.OutcomeRejected,
				"CreateVolume с пустым именем")
		}},

		{name: "CreateVolume/неположительный-размер-отвергнут", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-size-ok"), SizeBytes: 1}), "CreateVolume с размером 1")
			mustOutcome(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-size-zero"), SizeBytes: 0}), blockbackend.OutcomeRejected,
				"CreateVolume с нулевым размером")
			mustOutcome(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-size-neg"), SizeBytes: -giB}), blockbackend.OutcomeRejected,
				"CreateVolume с отрицательным размером")
		}},

		{name: "CreateVolume/отрицательное-число-QoS-отвергнуто", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-qos-ok"), SizeBytes: giB,
				QoS: map[string]int64{"iops": 1000}}), "CreateVolume с положительным QoS")
			mustOutcome(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-qos-neg"), SizeBytes: giB,
				QoS: map[string]int64{"iops": -1}}), blockbackend.OutcomeRejected,
				"CreateVolume с отрицательным числом QoS")
		}},

		{name: "CreateVolume/отменённый-контекст-не-создаёт-объект", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.ref("vol-cancelled")
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// Отменённый вызов повторяем: он не утверждение о запросе, а отсутствие
			// ответа. Полоса обязана быть той, что означает «спроси позже».
			mustOutcome(t, b.CreateVolume(ctx, blockbackend.VolumeSpec{Ref: ref, SizeBytes: giB}),
				blockbackend.OutcomeUnavailable, "CreateVolume с отменённым контекстом")

			obs := observeState(t, b, ref, "Observe после отменённого создания")
			if obs.State != blockbackend.ObservedAbsent {
				t.Fatalf("после отменённого создания объект %s, ожидалось ABSENT — "+
					"вызов, отказавший вызывающему, не должен оставлять объект", obs.State)
			}

			// Положительный контроль: живой контекст создаёт.
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{Ref: ref, SizeBytes: giB}),
				"CreateVolume с живым контекстом")
		}},

		// ------------------------------------------------------- инъекция отказа
		{name: "CreateVolume/отказ-в-правах-не-повторяем", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			f := r.faulty(t, b)

			f.FailVerb(VerbCreateVolume, blockbackend.OutcomeDenied)
			err := b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-denied"), SizeBytes: giB})
			mustOutcome(t, err, blockbackend.OutcomeDenied, "CreateVolume при отказе в правах")
			if blockbackend.OutcomeOf(err).Retryable() {
				t.Fatalf("отказ в правах объявлен повторяемым — повтор идентичного запроса " +
					"пройти не может, а вечный повтор держит голову очереди на строке, " +
					"которая никогда не применится")
			}

			// Положительный контроль: снятая инъекция возвращает успех — значит
			// красное выше произвела именно она, а не сломанная подготовка случая.
			f.ClearFailures()
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-denied"), SizeBytes: giB}), "CreateVolume после снятия инъекции")
		}},

		{name: "CreateVolume/неклассифицированный-исход-терминален", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			f := r.faulty(t, b)

			// Две формы одного: полоса названа «не классифицировано» — и полосы нет
			// вовсе. Контракт обязан давать одинаковый терминальный исход в обеих,
			// иначе трактовка достаётся тому, кто первым прочитал.
			for _, form := range []struct {
				name   string
				inject func()
			}{
				{"полоса названа не-классифицированной", func() {
					f.FailVerb(VerbCreateVolume, blockbackend.OutcomeUnclassified)
				}},
				{"полосы нет вовсе", func() { f.FailVerbUnclassified(VerbCreateVolume) }},
			} {
				f.ClearFailures()
				form.inject()
				err := b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
					Ref: r.ref("vol-unclassified"), SizeBytes: giB})
				mustOutcome(t, err, blockbackend.OutcomeUnclassified, "CreateVolume: "+form.name)
				if blockbackend.OutcomeOf(err).Retryable() {
					t.Fatalf("%s: неклассифицированный исход объявлен повторяемым — "+
						"корзина «прочее» получила бы семантику того, кто её первым прочитал", form.name)
				}
			}

			f.ClearFailures()
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-unclassified"), SizeBytes: giB}), "CreateVolume после снятия инъекции")
		}},

		{name: "CreateVolume/исчерпание-ёмкости-названо-своей-полосой", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			f := r.faulty(t, b)

			f.FailVerb(VerbCreateVolume, blockbackend.OutcomeCapacityExhausted)
			err := b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-full"), SizeBytes: giB})
			mustOutcome(t, err, blockbackend.OutcomeCapacityExhausted, "CreateVolume при исчерпании ёмкости")
			if blockbackend.OutcomeOf(err).Retryable() {
				t.Fatalf("исчерпание ёмкости объявлено повторяемым — место само не появится, " +
					"а повтор превратит отказ в нагрузку на переполненный бэкенд")
			}

			f.ClearFailures()
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.ref("vol-full"), SizeBytes: giB}), "CreateVolume после снятия инъекции")
		}},

		// ----------------------------------------------------------- удаление тома
		{name: "DeleteVolume/отсутствующий-и-повтор-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.ref("vol-delete")

			// Отсутствующий — успех: удаление утверждает «этого объекта нет», и оно
			// уже верно.
			mustOK(t, b.DeleteVolume(t.Context(), ref), "DeleteVolume отсутствующего")

			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{Ref: ref, SizeBytes: giB}),
				"CreateVolume перед удалением")
			mustOK(t, b.DeleteVolume(t.Context(), ref), "DeleteVolume первый раз")
			mustOK(t, b.DeleteVolume(t.Context(), ref), "DeleteVolume повтор")

			obs := observeState(t, b, ref, "Observe после удаления")
			if obs.State != blockbackend.ObservedAbsent {
				t.Fatalf("после удаления объект %s, ожидалось ABSENT", obs.State)
			}
		}},

		{name: "DeleteVolume/отказывается-снимать-снимок", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			vol := r.mkVolume(t, b, "vol-kind", giB)
			snap := r.mkSnapshot(t, b, vol, "snap-kind")

			// Глагол тома не снимает снимки: перепутанный глагол обязан отказать, а не
			// снести объект другого рода — иначе ошибка вызывающего становится потерей
			// данных, которую никто не заметит.
			mustOutcome(t, b.DeleteVolume(t.Context(), snap), blockbackend.OutcomeRejected,
				"DeleteVolume по адресу снимка")
			if obs := observeState(t, b, snap, "Observe снимка после отказа"); obs.State != blockbackend.ObservedReady {
				t.Fatalf("снимок после отказавшего DeleteVolume %s, ожидалось READY", obs.State)
			}

			// Положительный контроль: свой род объекта тот же глагол снимает.
			mustOK(t, b.DeleteVolume(t.Context(), vol), "DeleteVolume по адресу тома")
		}},

		{name: "DeleteVolume/источник-с-живыми-клонами-конфликт", run: func(t *testing.T, r *runner) {
			caps := fullCaps()
			b := r.backend(t, caps, capCloneKeepsParent, capCloneFromImage)
			src := r.mkVolume(t, b, "img-parent", 2*giB)
			clone := r.ref("vol-from-img")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: src, Target: clone, SizeBytes: 2 * giB}), "CloneVolume из образа")

			mustOutcome(t, b.DeleteVolume(t.Context(), src), blockbackend.OutcomeConflict,
				"DeleteVolume источника при живом клоне")

			// Положительный контроль: клон снят — источник снимается.
			mustOK(t, b.DeleteVolume(t.Context(), clone), "DeleteVolume клона")
			mustOK(t, b.DeleteVolume(t.Context(), src), "DeleteVolume источника после снятия клона")
		}},

		// ------------------------------------------------------------ изменение размера
		{name: "ResizeVolume/рост-и-повтор-до-достигнутого-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.mkVolume(t, b, "vol-grow", 2*giB)

			mustOK(t, b.ResizeVolume(t.Context(), ref, 4*giB), "ResizeVolume рост")
			mustOK(t, b.ResizeVolume(t.Context(), ref, 4*giB), "ResizeVolume повтор до достигнутого")

			obs := observeState(t, b, ref, "Observe после роста")
			if obs.SizeBytes != 4*giB {
				t.Fatalf("после роста размер %d, ожидался %d", obs.SizeBytes, 4*giB)
			}
		}},

		{name: "ResizeVolume/уменьшение-отвергнуто", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.mkVolume(t, b, "vol-shrink", 4*giB)

			// Положительный контроль стоит первым: глагол рабочий, отвергается именно
			// направление.
			mustOK(t, b.ResizeVolume(t.Context(), ref, 8*giB), "ResizeVolume рост")
			mustOutcome(t, b.ResizeVolume(t.Context(), ref, 4*giB), blockbackend.OutcomeRejected,
				"ResizeVolume уменьшение")

			obs := observeState(t, b, ref, "Observe после отказа")
			if obs.SizeBytes != 8*giB {
				t.Fatalf("после отказавшего уменьшения размер %d, ожидался прежний %d",
					obs.SizeBytes, 8*giB)
			}
		}},

		{name: "ResizeVolume/отсутствующий-объект-не-найден", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			mustOutcome(t, b.ResizeVolume(t.Context(), r.ref("vol-absent"), giB),
				blockbackend.OutcomeNotFound, "ResizeVolume отсутствующего")

			// Положительный контроль: существующий растёт.
			ref := r.mkVolume(t, b, "vol-present", giB)
			mustOK(t, b.ResizeVolume(t.Context(), ref, 2*giB), "ResizeVolume существующего")
		}},

		// ----------------------------------------------------------------- снимки
		{name: "CreateSnapshot/повтор-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			vol := r.mkVolume(t, b, "vol-snap-src", 2*giB)
			snap := r.ref("snap-repeat")

			mustOK(t, b.CreateSnapshot(t.Context(), vol, snap), "CreateSnapshot первый раз")
			mustOK(t, b.CreateSnapshot(t.Context(), vol, snap), "CreateSnapshot повтор")

			obs := observeState(t, b, snap, "Observe снимка")
			if obs.State != blockbackend.ObservedReady {
				t.Fatalf("снимок после повтора %s, ожидалось READY", obs.State)
			}
		}},

		{name: "CreateSnapshot/повтор-с-другого-тома-конфликт", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			first := r.mkVolume(t, b, "vol-snap-a", 2*giB)
			second := r.mkVolume(t, b, "vol-snap-b", 2*giB)
			snap := r.ref("snap-divergent")

			mustOK(t, b.CreateSnapshot(t.Context(), first, snap), "CreateSnapshot с первого тома")
			mustOK(t, b.CreateSnapshot(t.Context(), first, snap), "CreateSnapshot повтор с того же тома")
			mustOutcome(t, b.CreateSnapshot(t.Context(), second, snap), blockbackend.OutcomeConflict,
				"CreateSnapshot того же имени с ДРУГОГО тома")
		}},

		{name: "CreateSnapshot/отсутствующий-том-не-найден", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			mustOutcome(t, b.CreateSnapshot(t.Context(), r.ref("vol-nope"), r.ref("snap-nope")),
				blockbackend.OutcomeNotFound, "CreateSnapshot с отсутствующего тома")

			vol := r.mkVolume(t, b, "vol-yes", giB)
			mustOK(t, b.CreateSnapshot(t.Context(), vol, r.ref("snap-yes")),
				"CreateSnapshot с существующего тома")
		}},

		{name: "CreateSnapshot/без-способности-отказ", run: func(t *testing.T, r *runner) {
			off := fullCaps()
			off.Snapshots = false
			b := r.backend(t, off, capSnapshots)
			vol := r.mkVolume(t, b, "vol-nosnap", giB)

			// Способность, которой бэкенд не объявил, обязана отказывать и на самом
			// бэкенде тоже. Проверка в use-case — первый рубеж, а не единственный:
			// реализация, делающая вид, что умеет, оставит систему без второго.
			mustOutcome(t, b.CreateSnapshot(t.Context(), vol, r.ref("snap-nosnap")),
				blockbackend.OutcomeRejected, "CreateSnapshot без объявленной способности")

			// Положительный контроль — та же операция на реализации, способность
			// объявившей: отказ выше произведён именно её отсутствием.
			on := r.backend(t, fullCaps(), capSnapshots)
			volOn := r.mkVolume(t, on, "vol-nosnap", giB)
			mustOK(t, on.CreateSnapshot(t.Context(), volOn, r.ref("snap-nosnap")),
				"CreateSnapshot при объявленной способности")
		}},

		{name: "DeleteSnapshot/отсутствующий-и-повтор-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			vol := r.mkVolume(t, b, "vol-dsnap", giB)
			snap := r.ref("snap-delete")

			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot отсутствующего")
			mustOK(t, b.CreateSnapshot(t.Context(), vol, snap), "CreateSnapshot перед удалением")
			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot первый раз")
			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot повтор")

			if obs := observeState(t, b, snap, "Observe после удаления"); obs.State != blockbackend.ObservedAbsent {
				t.Fatalf("снимок после удаления %s, ожидалось ABSENT", obs.State)
			}
		}},

		{name: "DeleteSnapshot/отказывается-снимать-обычный-объект", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			vol := r.mkVolume(t, b, "vol-not-a-snap", giB)
			snap := r.mkSnapshot(t, b, vol, "snap-real")

			mustOutcome(t, b.DeleteSnapshot(t.Context(), vol), blockbackend.OutcomeRejected,
				"DeleteSnapshot по адресу тома")
			if obs := observeState(t, b, vol, "Observe тома после отказа"); obs.State != blockbackend.ObservedReady {
				t.Fatalf("том после отказавшего DeleteSnapshot %s, ожидалось READY", obs.State)
			}

			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot по адресу снимка")
		}},

		{name: "DeleteSnapshot/с-живыми-клонами-конфликт-при-объявленной-зависимости", run: func(t *testing.T, r *runner) {
			caps := fullCaps()
			b := r.backend(t, caps, capSnapshots, capCloneFromSnapshot, capCloneKeepsParent)
			vol := r.mkVolume(t, b, "vol-parent", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-parent")
			clone := r.ref("vol-clone")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: clone, SizeBytes: 2 * giB}), "CloneVolume из снимка")

			mustOutcome(t, b.DeleteSnapshot(t.Context(), snap), blockbackend.OutcomeConflict,
				"DeleteSnapshot при живом зависимом клоне")

			// Положительный контроль: зависимость снята — снимок снимается.
			mustOK(t, b.DeleteVolume(t.Context(), clone), "DeleteVolume клона")
			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot после снятия клона")
		}},

		{name: "DeleteSnapshot/без-объявленной-зависимости-успех", run: func(t *testing.T, r *runner) {
			caps := fullCaps()
			caps.CloneKeepsParent = false
			b := r.backend(t, caps, capSnapshots, capCloneFromSnapshot, capCloneKeepsParent)
			vol := r.mkVolume(t, b, "vol-parent", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-parent")
			clone := r.ref("vol-clone")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: clone, SizeBytes: 2 * giB}), "CloneVolume из снимка")

			// Бэкенд зависимости не объявил — значит клон её не несёт, и удаление
			// источника ничего не ломает. Конфликт здесь означал бы, что реализация
			// отслеживает зависимость, о которой не сказала.
			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot при необъявленной зависимости")
			if obs := observeState(t, b, clone, "Observe клона"); obs.State != blockbackend.ObservedReady {
				t.Fatalf("клон после удаления источника %s, ожидалось READY", obs.State)
			}
		}},

		// ------------------------------------------------------------------ клоны
		{name: "CloneVolume/повтор-с-теми-же-аргументами-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot)
			vol := r.mkVolume(t, b, "vol-clone-src", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-clone-src")
			spec := blockbackend.CloneSpec{Source: snap, Target: r.ref("vol-clone-dst"), SizeBytes: 2 * giB}

			mustOK(t, b.CloneVolume(t.Context(), spec), "CloneVolume первый раз")
			mustOK(t, b.CloneVolume(t.Context(), spec), "CloneVolume повтор")
			if obs := observeState(t, b, spec.Target, "Observe клона"); obs.SizeBytes != 2*giB {
				t.Fatalf("клон после повтора размер %d, ожидался %d", obs.SizeBytes, 2*giB)
			}
		}},

		{name: "CloneVolume/повтор-с-другим-размером-конфликт", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot)
			vol := r.mkVolume(t, b, "vol-clone-src", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-clone-src")
			spec := blockbackend.CloneSpec{Source: snap, Target: r.ref("vol-clone-dst"), SizeBytes: 2 * giB}

			mustOK(t, b.CloneVolume(t.Context(), spec), "CloneVolume первый раз")
			mustOK(t, b.CloneVolume(t.Context(), spec), "CloneVolume повтор с тем же размером")

			bigger := spec
			bigger.SizeBytes = 4 * giB
			mustOutcome(t, b.CloneVolume(t.Context(), bigger), blockbackend.OutcomeConflict,
				"CloneVolume повтор с ДРУГИМ размером")
		}},

		{name: "CloneVolume/цель-меньше-источника-отвергнута", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot)
			vol := r.mkVolume(t, b, "vol-clone-src", 4*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-clone-src")

			mustOutcome(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: r.ref("vol-too-small"), SizeBytes: 2 * giB}),
				blockbackend.OutcomeRejected, "CloneVolume в цель меньше источника")

			// Положительный контроль: ровно размер источника и больше — проходят.
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: r.ref("vol-exact"), SizeBytes: 4 * giB}),
				"CloneVolume в цель размером с источник")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: r.ref("vol-bigger"), SizeBytes: 8 * giB}),
				"CloneVolume в цель больше источника")
		}},

		{name: "CloneVolume/отсутствующий-источник-не-найден", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot)
			mustOutcome(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: r.ref("snap-absent"), Target: r.ref("vol-orphan"), SizeBytes: giB}),
				blockbackend.OutcomeNotFound, "CloneVolume из отсутствующего источника")

			vol := r.mkVolume(t, b, "vol-src", giB)
			snap := r.mkSnapshot(t, b, vol, "snap-src")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: r.ref("vol-ok"), SizeBytes: giB}),
				"CloneVolume из существующего источника")
		}},

		{name: "CloneVolume/родитель-виден-в-Observed-при-объявленной-зависимости", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot, capCloneKeepsParent)
			vol := r.mkVolume(t, b, "vol-p", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-p")
			target := r.ref("vol-child")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: target, SizeBytes: 2 * giB}), "CloneVolume")

			obs := observeState(t, b, target, "Observe клона")
			if obs.Parent != snap.Name {
				t.Fatalf("родитель клона %q, ожидался %q — бэкенд объявил зависимость клона "+
					"от родителя, значит она обязана быть ВИДНА: на ней стоит решение об "+
					"удалении источника", obs.Parent, snap.Name)
			}
		}},

		{name: "CloneVolume/при-Detached-родителя-не-остаётся", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot, capCloneKeepsParent)
			vol := r.mkVolume(t, b, "vol-p", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-p")

			// Положительный контроль в том же случае: без Detached родитель виден —
			// значит пустота ниже произведена именно флагом, а не тем, что реализация
			// родителя не показывает никогда.
			kept := r.ref("vol-kept")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: kept, SizeBytes: 2 * giB}), "CloneVolume без Detached")
			if obs := observeState(t, b, kept, "Observe клона без Detached"); obs.Parent == "" {
				t.Fatalf("родитель клона пуст при объявленной зависимости — положительный " +
					"контроль не сработал, отрицание ниже беспредметно")
			}

			detached := r.ref("vol-detached")
			mustOK(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: detached, SizeBytes: 2 * giB, Detached: true}),
				"CloneVolume с Detached")
			if obs := observeState(t, b, detached, "Observe отвязанного клона"); obs.Parent != "" {
				t.Fatalf("родитель отвязанного клона %q, ожидалась пустота — Detached "+
					"требует независимости от источника по завершении", obs.Parent)
			}

			// И следствие независимости: источник снимается, пока отвязанный клон жив.
			mustOK(t, b.DeleteVolume(t.Context(), kept), "DeleteVolume зависимого клона")
			mustOK(t, b.DeleteSnapshot(t.Context(), snap), "DeleteSnapshot при живом отвязанном клоне")
		}},

		{name: "CloneVolume/без-способности-из-снимка-отказ", run: func(t *testing.T, r *runner) {
			off := fullCaps()
			off.CloneFromSnapshot = false
			b := r.backend(t, off, capSnapshots, capCloneFromSnapshot)
			vol := r.mkVolume(t, b, "vol-src", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-src")

			mustOutcome(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: r.ref("vol-dst"), SizeBytes: 2 * giB}),
				blockbackend.OutcomeRejected, "CloneVolume из снимка без способности")

			on := r.backend(t, fullCaps(), capSnapshots, capCloneFromSnapshot)
			volOn := r.mkVolume(t, on, "vol-src", 2*giB)
			snapOn := r.mkSnapshot(t, on, volOn, "snap-src")
			mustOK(t, on.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snapOn, Target: r.ref("vol-dst"), SizeBytes: 2 * giB}),
				"CloneVolume из снимка при объявленной способности")
		}},

		{name: "CloneVolume/без-способности-из-образа-отказ", run: func(t *testing.T, r *runner) {
			off := fullCaps()
			off.CloneFromImage = false
			b := r.backend(t, off, capCloneFromImage)
			img := r.mkVolume(t, b, "img-src", 2*giB)

			mustOutcome(t, b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: img, Target: r.ref("vol-dst"), SizeBytes: 2 * giB}),
				blockbackend.OutcomeRejected, "CloneVolume из образа без способности")

			on := r.backend(t, fullCaps(), capCloneFromImage)
			imgOn := r.mkVolume(t, on, "img-src", 2*giB)
			mustOK(t, on.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: imgOn, Target: r.ref("vol-dst"), SizeBytes: 2 * giB}),
				"CloneVolume из образа при объявленной способности")
		}},

		// ------------------------------------------------------------ перенос снимка
		{name: "CopySnapshot/повтор-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			vol := r.mkVolume(t, b, "vol-copy-src", 2*giB)
			snap := r.mkSnapshot(t, b, vol, "snap-copy-src")
			target := r.otherRef("snap-copy-dst")

			mustOK(t, b.CopySnapshot(t.Context(), snap, target), "CopySnapshot первый раз")
			mustOK(t, b.CopySnapshot(t.Context(), snap, target), "CopySnapshot повтор")

			if obs := observeState(t, b, target, "Observe копии"); obs.State != blockbackend.ObservedReady {
				t.Fatalf("копия снимка %s, ожидалось READY", obs.State)
			}
			// Источник переносом не тронут.
			if obs := observeState(t, b, snap, "Observe источника"); obs.State != blockbackend.ObservedReady {
				t.Fatalf("источник после переноса %s, ожидалось READY", obs.State)
			}
		}},

		{name: "CopySnapshot/цель-с-другим-размером-конфликт", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			small := r.mkVolume(t, b, "vol-small", 2*giB)
			snapSmall := r.mkSnapshot(t, b, small, "snap-small")

			// Цель занята снимком другого размера — перенос обязан отказать, а не
			// подменить чужой объект.
			bigRef := blockbackend.ObjectRef{Locator: r.opts.OtherLocator, Name: "vol-big"}
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{Ref: bigRef, SizeBytes: 8 * giB}),
				"CreateVolume во втором локаторе")
			target := r.otherRef("snap-target")
			mustOK(t, b.CreateSnapshot(t.Context(), bigRef, target), "CreateSnapshot во втором локаторе")

			mustOutcome(t, b.CopySnapshot(t.Context(), snapSmall, target), blockbackend.OutcomeConflict,
				"CopySnapshot в занятую цель другого размера")

			// Положительный контроль: свободная цель принимает.
			mustOK(t, b.CopySnapshot(t.Context(), snapSmall, r.otherRef("snap-free")),
				"CopySnapshot в свободную цель")
		}},

		{name: "CopySnapshot/без-способности-отказ", run: func(t *testing.T, r *runner) {
			on := r.backend(t, fullCaps(), capSnapshots)
			vol := r.mkVolume(t, on, "vol-copy-src", 2*giB)
			snap := r.mkSnapshot(t, on, vol, "snap-copy-src")
			mustOK(t, on.CopySnapshot(t.Context(), snap, r.otherRef("snap-copy-dst")),
				"CopySnapshot при объявленной способности")

			off := fullCaps()
			off.Snapshots = false
			b := r.backend(t, off, capSnapshots)
			mustOutcome(t, b.CopySnapshot(t.Context(), r.ref("snap-copy-src"), r.otherRef("snap-copy-dst")),
				blockbackend.OutcomeRejected, "CopySnapshot без объявленной способности")
		}},

		// ------------------------------------------------------------ перенос тома
		{name: "MigrateVolume/перенос-и-повтор-успех", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.mkVolume(t, b, "vol-migrate", 2*giB)

			mustOK(t, b.MigrateVolume(t.Context(), ref, r.opts.OtherLocator), "MigrateVolume первый раз")
			// Повтор идёт по ПРЕЖНЕМУ адресу: исполнитель операций переисполняет
			// функцию, не зная, доехал ли перенос, и обязан получить успех.
			mustOK(t, b.MigrateVolume(t.Context(), ref, r.opts.OtherLocator), "MigrateVolume повтор")

			moved := r.otherRef("vol-migrate")
			obs := observeState(t, b, moved, "Observe в целевом локаторе")
			if obs.State != blockbackend.ObservedReady || obs.SizeBytes != 2*giB {
				t.Fatalf("после переноса %s size=%d, ожидалось READY size=%d — перенос "+
					"обязан сохранить данные", obs.State, obs.SizeBytes, 2*giB)
			}
			if obs := observeState(t, b, ref, "Observe в исходном локаторе"); obs.State != blockbackend.ObservedAbsent {
				t.Fatalf("в исходном локаторе после переноса %s, ожидалось ABSENT", obs.State)
			}
		}},

		{name: "MigrateVolume/отсутствующий-объект-не-найден", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			mustOutcome(t, b.MigrateVolume(t.Context(), r.ref("vol-nowhere"), r.opts.OtherLocator),
				blockbackend.OutcomeNotFound, "MigrateVolume отсутствующего объекта")

			ref := r.mkVolume(t, b, "vol-here", giB)
			mustOK(t, b.MigrateVolume(t.Context(), ref, r.opts.OtherLocator),
				"MigrateVolume существующего объекта")
		}},

		// ----------------------------------------------------------------- чтение
		{name: "Observe/отсутствующий-объект-это-ABSENT-без-ошибки", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			obs, err := b.Observe(t.Context(), r.ref("vol-never-was"))
			mustOK(t, err, "Observe отсутствующего")
			if obs.State != blockbackend.ObservedAbsent {
				t.Fatalf("отсутствующий объект наблюдён как %s, ожидалось ABSENT", obs.State)
			}

			// Положительный контроль: существующий читается как READY со своим размером.
			ref := r.mkVolume(t, b, "vol-is", 3*giB)
			got := observeState(t, b, ref, "Observe существующего")
			if got.State != blockbackend.ObservedReady || got.SizeBytes != 3*giB {
				t.Fatalf("существующий объект наблюдён как %s size=%d, ожидалось READY size=%d",
					got.State, got.SizeBytes, 3*giB)
			}
		}},

		{name: "Observe/недоступность-это-ошибка-а-не-отсутствие", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.mkVolume(t, b, "vol-observed", giB)
			f := r.faulty(t, b)

			f.FailVerb(VerbObserve, blockbackend.OutcomeUnavailable)
			obs, err := b.Observe(t.Context(), ref)
			mustOutcome(t, err, blockbackend.OutcomeUnavailable, "Observe при недоступном бэкенде")
			if obs.State == blockbackend.ObservedAbsent {
				t.Fatalf("недоступный бэкенд наблюдён как ABSENT — молчание не является " +
					"утверждением об отсутствии объекта, а сверщик по такому ответу снёс бы " +
					"живую строку как утечку")
			}

			// Положительный контроль: инъекция снята — читается тот же объект.
			f.ClearFailures()
			if got := observeState(t, b, ref, "Observe после снятия инъекции"); got.State != blockbackend.ObservedReady {
				t.Fatalf("после снятия инъекции объект %s, ожидалось READY", got.State)
			}
		}},

		{name: "Observe/отменённый-контекст-это-ошибка-а-не-отсутствие", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			ref := r.mkVolume(t, b, "vol-cancel-observe", giB)

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			obs, err := b.Observe(ctx, ref)
			mustOutcome(t, err, blockbackend.OutcomeUnavailable, "Observe с отменённым контекстом")
			if obs.State == blockbackend.ObservedAbsent {
				t.Fatalf("отменённое чтение наблюдено как ABSENT — не дождавшийся ответа " +
					"вызывающий ничего не узнал об объекте")
			}

			if got := observeState(t, b, ref, "Observe с живым контекстом"); got.State != blockbackend.ObservedReady {
				t.Fatalf("при живом контексте объект %s, ожидалось READY", got.State)
			}
		}},

		// ------------------------------------------------------------ перечисление
		{name: "ListObjects/отдаёт-ровно-созданные-имена-и-завершается", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps(), capSnapshots)
			want := []string{"vol-l1", "vol-l2", "vol-l3"}
			for _, n := range want {
				r.mkVolume(t, b, n, giB)
			}
			// Снимок — тоже объект локатора: сверщик дрейфа обходит обе оси, и
			// перечисление, показывающее лишь тома, оставило бы снимки без сверки.
			snap := r.mkSnapshot(t, b, r.ref("vol-l1"), "snap-l1")
			want = append(want, snap.Name)

			// Страницами по одному: короткая страница обязана завершаться курсором, а
			// не перечислять вечно.
			got := listAll(t, b, r.opts.Locator, 1)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("перечислено %v, создано %v — сверка дрейфа считает лишнее утечкой, "+
					"а недостающее потерей: обе ошибки дороги", got, want)
			}
		}},

		{name: "ListObjects/объекты-другого-локатора-не-показываются", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			r.mkVolume(t, b, "vol-here", giB)
			mustOK(t, b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: r.otherRef("vol-there"), SizeBytes: giB}), "CreateVolume во втором локаторе")

			here := listAll(t, b, r.opts.Locator, 10)
			if slices.Contains(here, "vol-there") {
				t.Fatalf("перечисление локатора показало объект соседа: %v — пространство "+
					"арендатора и есть единица изоляции, и сверщик по такому ответу пришёл бы "+
					"чинить чужие строки", here)
			}
			if !slices.Contains(here, "vol-here") {
				t.Fatalf("перечисление локатора не показало свой объект: %v — положительный "+
					"контроль не сработал, отрицание выше беспредметно", here)
			}

			// И симметрично: сосед видит своё и не видит наше.
			there := listAll(t, b, r.opts.OtherLocator, 10)
			if !slices.Contains(there, "vol-there") || slices.Contains(there, "vol-here") {
				t.Fatalf("перечисление второго локатора: %v, ожидалось ровно [vol-there]", there)
			}
		}},

		{name: "ListObjects/отрицательный-предел-отвергнут", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			r.mkVolume(t, b, "vol-limit", giB)

			_, _, err := b.ListObjects(t.Context(), r.opts.Locator, "", -1)
			mustOutcome(t, err, blockbackend.OutcomeRejected, "ListObjects с отрицательным пределом")

			// Ноль — «предел не назван», и это законно: он отличается от мусора.
			names, _, err := b.ListObjects(t.Context(), r.opts.Locator, "", 0)
			mustOK(t, err, "ListObjects без названного предела")
			if !slices.Contains(names, "vol-limit") {
				t.Fatalf("перечисление без названного предела отдало %v, ожидался vol-limit", names)
			}
		}},

		{name: "ListObjects/недоступность-это-ошибка-а-не-пустая-страница", run: func(t *testing.T, r *runner) {
			b := r.backend(t, fullCaps())
			r.mkVolume(t, b, "vol-list-fault", giB)
			f := r.faulty(t, b)

			f.FailVerb(VerbListObjects, blockbackend.OutcomeUnavailable)
			names, next, err := b.ListObjects(t.Context(), r.opts.Locator, "", 10)
			mustOutcome(t, err, blockbackend.OutcomeUnavailable, "ListObjects при недоступном бэкенде")
			if len(names) != 0 || next != "" {
				t.Fatalf("недоступный бэкенд отдал имена %v и курсор %q — отказ обязан быть "+
					"пустым по данным, иначе сверщик примет обрывок за полную картину", names, next)
			}

			f.ClearFailures()
			after := listAll(t, b, r.opts.Locator, 10)
			if !slices.Contains(after, "vol-list-fault") {
				t.Fatalf("после снятия инъекции перечислено %v, ожидался vol-list-fault", after)
			}
		}},
	}
}
