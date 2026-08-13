// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package fake_test

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend/contract"
	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend/fake"
)

// Дублёр обязан оставаться пригодным для отрицательных полос контрактной суиты:
// расхождение ловится компиляцией, а не прогоном, который свёл бы его к «не исполнено».
var _ contract.Faulty = (*fake.Backend)(nil)

// TestFakeSatisfiesBackendContract — та же суита, которой обязан пройти адаптер живого
// хранилища. Дублёр, снисходительнее настоящего, прячет ровно тот дефект, ради которого
// его подставляют, поэтому «не снисходительнее» здесь не обещание, а прогон.
func TestFakeSatisfiesBackendContract(t *testing.T) {
	rep := contract.Run(t, contract.Options{
		Name: "blockbackend/fake",
		New: func(_ *testing.T, caps blockbackend.Capabilities) blockbackend.Backend {
			return fake.New(caps)
		},
		Locator:      blockbackend.Locator{Pool: "kacho-fast", Namespace: "prj-aaaaaaaaaaaaaaaaa"},
		OtherLocator: blockbackend.Locator{Pool: "kacho-fast", Namespace: "prj-bbbbbbbbbbbbbbbbb"},
	})

	// Ноль исполненных случаев — это «прогон не дошёл», а не «нашлось ноль». Числа
	// утверждаются здесь, а не только печатаются переписью: печать сама по себе
	// прогон не роняет.
	if rep.Executed == 0 {
		t.Fatalf("суита не исполнила ни одного случая — зелёный прогон здесь означал бы, "+
			"что проверка не состоялась (объявлено %d)", rep.Declared)
	}
	if rep.Executed != rep.Declared {
		t.Errorf("исполнено %d случаев из %d объявленных; не исполненные не проверены, "+
			"а не чисты: %v", rep.Executed, rep.Declared, rep.Skipped)
	}
	if rep.Kind == "" {
		t.Errorf("реализация не назвала свой вид — перепись не смогла бы сказать, против чего шёл прогон")
	}
}

// TestInjectionRefusesUnknownVerb — инъекция в глагол, которого у порта нет, обязана
// быть слышной. Молчаливое принятие опечатки означало бы отрицательный случай, который
// НИЧЕГО не инъецировал и позеленел на неисполненном отказе.
func TestInjectionRefusesUnknownVerb(t *testing.T) {
	b := fake.New(blockbackend.Capabilities{})

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatalf("инъекция в неизвестный глагол принята молча — опечатка в имени глагола " +
				"дала бы случай, который ничего не проверяет")
		}
	}()
	b.FailVerb("CreateVolme", blockbackend.OutcomeDenied)
}

// TestInjectionReachesEveryPortVerb — положительный контроль к предыдущему, и он же
// перепись: каждый глагол порта инъецируем И отказ до него ДОХОДИТ.
//
// Слабая форма («имя принято») здесь не годится: имя глагола внутри реализации и имя, по
// которому заказывают инъекцию, — две строки, и разойдись они, инъекция была бы принята
// и не сработала. Отрицательный случай, ничего не инъецировавший, зеленеет на
// неисполненном отказе — поэтому утверждается ИСХОД вызова, а не факт заказа.
func TestInjectionReachesEveryPortVerb(t *testing.T) {
	loc := blockbackend.Locator{Pool: "kacho-fast", Namespace: "prj-ddddddddddddddddd"}
	ref := blockbackend.ObjectRef{Locator: loc, Name: "vol-inject"}
	snap := blockbackend.ObjectRef{Locator: loc, Name: "snap-inject"}

	// Вызовы держатся в одном месте с именами: перечень обязан покрыть весь порт, и
	// перепись ниже сверяет его с самим интерфейсом, а не с памятью автора.
	calls := map[string]func(b *fake.Backend) error{
		contract.VerbCreateVolume: func(b *fake.Backend) error {
			return b.CreateVolume(t.Context(), blockbackend.VolumeSpec{Ref: ref, SizeBytes: 1 << 30})
		},
		contract.VerbDeleteVolume: func(b *fake.Backend) error { return b.DeleteVolume(t.Context(), ref) },
		contract.VerbResizeVolume: func(b *fake.Backend) error {
			return b.ResizeVolume(t.Context(), ref, 2<<30)
		},
		contract.VerbCreateSnapshot: func(b *fake.Backend) error {
			return b.CreateSnapshot(t.Context(), ref, snap)
		},
		contract.VerbDeleteSnapshot: func(b *fake.Backend) error { return b.DeleteSnapshot(t.Context(), snap) },
		contract.VerbCloneVolume: func(b *fake.Backend) error {
			return b.CloneVolume(t.Context(), blockbackend.CloneSpec{
				Source: snap, Target: blockbackend.ObjectRef{Locator: loc, Name: "vol-clone"}, SizeBytes: 1 << 30})
		},
		contract.VerbCopySnapshot: func(b *fake.Backend) error {
			return b.CopySnapshot(t.Context(), snap, blockbackend.ObjectRef{
				Locator: blockbackend.Locator{Pool: loc.Pool, Namespace: "prj-eeeeeeeeeeeeeeeee"}, Name: snap.Name})
		},
		contract.VerbMigrateVolume: func(b *fake.Backend) error {
			return b.MigrateVolume(t.Context(), ref, blockbackend.Locator{
				Pool: loc.Pool, Namespace: "prj-eeeeeeeeeeeeeeeee"})
		},
		contract.VerbObserve: func(b *fake.Backend) error {
			_, err := b.Observe(t.Context(), ref)
			return err
		},
		contract.VerbListObjects: func(b *fake.Backend) error {
			_, _, err := b.ListObjects(t.Context(), loc, "", 10)
			return err
		},
	}

	// Перепись: глаголы порта, способные вернуть отказ, берутся отражением по самому
	// интерфейсу. Одиннадцатый глагол, добавленный без вызова здесь, роняет прогон, а
	// не проезжает молча.
	iface := reflect.TypeOf((*blockbackend.Backend)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	var covered int
	for i := range iface.NumMethod() {
		m := iface.Method(i)
		if n := m.Type.NumOut(); n == 0 || m.Type.Out(n-1) != errType {
			continue
		}
		covered++
		call, ok := calls[m.Name]
		if !ok {
			t.Errorf("глагол порта %s не вызывается этой пробой — его инъекция осталась бы "+
				"непроверенной, а прогон зелёным", m.Name)
			continue
		}

		b := fake.New(blockbackend.Capabilities{Snapshots: true, CloneFromSnapshot: true, CloneFromImage: true})
		b.FailVerb(m.Name, blockbackend.OutcomeDenied)
		if got := blockbackend.OutcomeOf(call(b)); got != blockbackend.OutcomeDenied {
			t.Errorf("инъекция в %s дала полосу %s, ожидалась denied — заказанный отказ до "+
				"глагола не доехал", m.Name, got)
		}
	}
	if covered != len(calls) {
		t.Errorf("перечень вызовов несёт %d глаголов, порт — %d способных отказать: "+
			"лишняя запись проверяет то, чего в порту нет", len(calls), covered)
	}
	if covered == 0 {
		t.Fatalf("перепись не нашла ни одного отказывающего глагола — проба не проверила ничего")
	}
}

// TestConcurrentCreateIsSafe — дублёр живёт в пробах, которые гоняются под -race, и в
// пробах гонок use-case: небезопасный по потокам дублёр красит их своим дефектом, и
// разбирать потом приходится не тот код.
//
// Утверждается не только отсутствие гонки, но и ИСХОД: одинаковое создание проходит у
// всех, расходящееся — ни у кого, кроме первого.
func TestConcurrentCreateIsSafe(t *testing.T) {
	b := fake.New(blockbackend.Capabilities{})
	loc := blockbackend.Locator{Pool: "kacho-fast", Namespace: "prj-ccccccccccccccccc"}
	ref := blockbackend.ObjectRef{Locator: loc, Name: "vol-shared"}

	const n = 32
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Половина создаёт общий объект с тем же размером, половина — свой.
			spec := blockbackend.VolumeSpec{Ref: ref, SizeBytes: 1 << 30}
			if i%2 == 1 {
				spec.Ref = blockbackend.ObjectRef{Locator: loc, Name: fmt.Sprintf("vol-%02d", i)}
			}
			if err := b.CreateVolume(t.Context(), spec); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(errs) != 0 {
		t.Fatalf("конкурентное создание с одинаковыми аргументами дало отказы %v — "+
			"идемпотентность обязана держаться и под параллелью", errs)
	}

	names, next, err := b.ListObjects(t.Context(), loc, "", 1000)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if next != "" {
		t.Fatalf("курсор %q не пуст при пределе, превышающем число объектов", next)
	}
	if want := 1 + n/2; len(names) != want {
		t.Fatalf("создано %d объектов, ожидалось %d — конкурентный повтор обязан попадать "+
			"в тот же объект, а не заводить второй", len(names), want)
	}

	// Отрицательная половина: расходящийся размер отвергается и под параллелью.
	var conflicts int
	wg = sync.WaitGroup{}
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := b.CreateVolume(t.Context(), blockbackend.VolumeSpec{
				Ref: ref, SizeBytes: int64(2+i) << 30})
			mu.Lock()
			defer mu.Unlock()
			if blockbackend.OutcomeOf(err) == blockbackend.OutcomeConflict {
				conflicts++
			}
		}(i)
	}
	wg.Wait()
	if conflicts != n {
		t.Fatalf("расходящихся созданий отвергнуто %d из %d — молча принятое расхождение "+
			"переписало бы размер уже созданного объекта", conflicts, n)
	}
}
