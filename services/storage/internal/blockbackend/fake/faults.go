// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package fake

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// Имена глаголов. Совпадают с именами методов порта — и это не совпадение, а
// требование: по этой же строке вызывающий заказывает инъекцию, и разойдись она с
// именем метода, инъекция молча не срабатывала бы, а отрицательный случай зеленел бы на
// НЕисполненном отказе. Расхождение ловится прогоном: fake_test инъецирует каждый
// глагол и требует, чтобы отказ пришёл.
const (
	verbCreateVolume   = "CreateVolume"
	verbDeleteVolume   = "DeleteVolume"
	verbResizeVolume   = "ResizeVolume"
	verbCreateSnapshot = "CreateSnapshot"
	verbDeleteSnapshot = "DeleteSnapshot"
	verbCloneVolume    = "CloneVolume"
	verbCopySnapshot   = "CopySnapshot"
	verbMigrateVolume  = "MigrateVolume"
	verbObserve        = "Observe"
	verbListObjects    = "ListObjects"
)

// injectableVerbs — глаголы порта, СПОСОБНЫЕ вернуть отказ.
//
// Набор выводится отражением по самому интерфейсу, а не выписывается рядом: глагол,
// добавленный в порт, становится инъецируемым сам, и второго словаря, который разошёлся
// бы с портом ровно тогда, когда порт вырос, здесь нет.
//
// Признак — последнее возвращаемое значение типа error. Он же отсекает Kind и
// Capabilities: инъекция в метод, отказ не возвращающий, была бы принята и не сделала бы
// ничего — то есть тихо отменила бы отрицательный случай, который её заказал.
var injectableVerbs = computeInjectableVerbs()

func computeInjectableVerbs() map[string]struct{} {
	iface := reflect.TypeOf((*blockbackend.Backend)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	out := make(map[string]struct{}, iface.NumMethod())
	for i := range iface.NumMethod() {
		m := iface.Method(i)
		if n := m.Type.NumOut(); n > 0 && m.Type.Out(n-1) == errType {
			out[m.Name] = struct{}{}
		}
	}
	return out
}

// fault — заказанный отказ одного глагола.
type fault struct {
	outcome blockbackend.Outcome
	// bandless различает две формы одного: полоса, названная «не классифицировано»,
	// и отказ, полосы не несущий ВОВСЕ. Контракт обязан давать одинаковый терминальный
	// исход в обеих, и проверить это можно, только умея произвести обе.
	bandless bool
}

// FailVerb заставляет названный глагол возвращать отказ названной полосы, пока инъекция
// не снята.
//
// Неизвестный глагол и полоса вне объявленного набора — ПАНИКА, а не тихое принятие:
// заказ инъекции, которая ничего не делает, оставил бы отрицательный случай зелёным на
// неисполненном отказе, и это тот самый класс «форма проверки без содержания», ради
// которого дублёр и заводится.
func (b *Backend) FailVerb(verb string, outcome blockbackend.Outcome) {
	assertInjectable(verb)
	if !outcome.Known() {
		panic(fmt.Sprintf("blockbackend/fake: полоса %d вне объявленного набора Outcome — "+
			"инъекция исхода, которого контракт не знает, проверяла бы выдумку", int(outcome)))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.faults[verb] = fault{outcome: outcome}
}

// FailVerbUnclassified заставляет глагол вернуть отказ, полосы НЕ несущий.
//
// Отличается от FailVerb(verb, OutcomeUnclassified) намеренно: там полоса названа явно,
// здесь её нет вовсе — ровно то, что приходит от бэкенда, чей ответ классифицировать не
// удалось. Оба обязаны давать один терминальный исход.
func (b *Backend) FailVerbUnclassified(verb string) {
	assertInjectable(verb)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.faults[verb] = fault{bandless: true}
}

// ClearFailures снимает все инъекции.
func (b *Backend) ClearFailures() {
	b.mu.Lock()
	defer b.mu.Unlock()
	clear(b.faults)
}

func assertInjectable(verb string) {
	if _, ok := injectableVerbs[verb]; ok {
		return
	}
	// Порядок устойчив: сообщение о невозможном вызове, меняющееся от прогона к
	// прогону, труднее и прочитать, и сравнить с ожидаемым.
	panic(fmt.Sprintf("blockbackend/fake: глагол %q порту неизвестен; инъецируемы %v",
		verb, slices.Sorted(maps.Keys(injectableVerbs))))
}

// errInjected — исходная ошибка инъекции. Живёт в журнале оператора и наружу арендатору
// не выходит: край выдаёт текст по полосе, из фиксированной таблицы.
var errInjected = errors.New("fake: injected failure")

// begin — общее начало каждого глагола: мёртвый контекст и заказанный отказ.
//
// Контекст спрашивается ПЕРВЫМ. Отменённый вызов до бэкенда не доехал, значит и
// заказанный на бэкенде отказ к нему отношения не имеет; полоса — недоступность,
// единственная, означающая «спроси позже»: вызывающий, не дождавшийся ответа, ничего об
// объекте не узнал.
func (b *Backend) begin(ctx context.Context, verb, object string) error {
	if err := ctx.Err(); err != nil {
		return blockbackend.Errorf(blockbackend.OutcomeUnavailable, verb, object, err)
	}

	b.mu.Lock()
	f, ok := b.faults[verb]
	b.mu.Unlock()
	if !ok {
		return nil
	}
	if f.bandless {
		return fmt.Errorf("fake %s %s: %w", verb, object, errInjected)
	}
	return blockbackend.Errorf(f.outcome, verb, object, errInjected)
}
