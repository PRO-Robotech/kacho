// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package contract — ОДНА суита, прогоняемая против ЛЮБОЙ реализации порта
// [blockbackend.Backend].
//
// # Зачем суита общая
//
// Дублёр заводится ради того, чтобы проверить отрицательные пути системы без живого
// хранилища. Дублёр, молча глотающий ввод, на котором настоящий адаптер отказывает,
// прячет ровно тот дефект, ради которого его подставляют: проба зеленеет, а на стенде
// тот же вызов отвергается. Поэтому «фейк не снисходительнее настоящего» выражено не
// обещанием в записке, а суитой, которую обязаны пройти ОБЕ реализации.
//
// Отсюда же требование к строгости самой суиты: она утверждает не только «умеет», но и
// «отвергает» — и каждое отрицание стоит рядом с положительным контролем. Реализация,
// отвергающая вообще всё, обязана краснеть на положительных случаях, иначе набор
// отрицаний зеленел бы на ней целиком.
//
// # Почему инъекция отказа обязательна
//
// Полосы [blockbackend.Outcome] — половина контракта порта: недоступность повторяема, а
// отказ в правах нет, и путать их дорого. Проверить эту половину можно только на
// реализации, которую МОЖНО заставить отказать. Реализация, такой возможности не
// давшая, проходит суиту с непроверенными отрицательными полосами — и прогон это
// НАЗЫВАЕТ и роняет, а не умалчивает: «не исполнено» никогда не засчитывается за
// «прошло».
//
// # Что печатает прогон
//
// Реализацию, вид бэкенда, число объявленных, исполненных, упавших и НЕ исполненных
// случаев с причиной по каждому. «Ноль находок» обязано быть отличимо от «ноль
// прогнанного» — иначе суита, не дошедшая ни до одного случая, читается как чистая.
package contract

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// Глаголы порта — словарь инъекции отказа. Значения совпадают с именами методов
// [blockbackend.Backend]: второго словаря, который мог бы с ним разойтись, здесь нет.
const (
	VerbCreateVolume   = "CreateVolume"
	VerbDeleteVolume   = "DeleteVolume"
	VerbResizeVolume   = "ResizeVolume"
	VerbCreateSnapshot = "CreateSnapshot"
	VerbDeleteSnapshot = "DeleteSnapshot"
	VerbCloneVolume    = "CloneVolume"
	VerbCopySnapshot   = "CopySnapshot"
	VerbMigrateVolume  = "MigrateVolume"
	VerbObserve        = "Observe"
	VerbListObjects    = "ListObjects"
)

// Faulty — управляемая инъекция отказа. Реализация порта, желающая быть проверенной по
// ОТРИЦАТЕЛЬНЫМ полосам, объявляет этот интерфейс; суита обнаруживает его приведением
// типа, поэтому сам порт остаётся чистым — тестовой ручки в производственном
// интерфейсе нет.
type Faulty interface {
	// FailVerb заставляет названный глагол возвращать отказ названной полосы, пока
	// инъекция не снята.
	FailVerb(verb string, outcome blockbackend.Outcome)

	// FailVerbUnclassified заставляет глагол вернуть отказ, полосы НЕ несущий.
	// Отличается от FailVerb(verb, OutcomeUnclassified) намеренно: там полоса
	// названа явно, здесь её нет вовсе — а контракт обязан давать одинаковый
	// терминальный исход в обоих случаях.
	FailVerbUnclassified(verb string)

	// ClearFailures снимает все инъекции.
	ClearFailures()
}

// Options — что нужно суите, чтобы прогнаться против реализации.
type Options struct {
	// Name — как называть реализацию в отчёте.
	Name string

	// New строит СВЕЖУЮ реализацию с запрошенными способностями. Свежую на каждый
	// случай: случаи, делящие состояние, зеленеют и краснеют по чужим следам.
	//
	// Реализация вправе объявить не те способности, что запрошены (у адаптера они
	// константы) — суита сверяет полученные с нужными ей и записывает случай как НЕ
	// исполненный, называя способность. Тихо трактовать его как пройденный нельзя.
	New func(t *testing.T, caps blockbackend.Capabilities) blockbackend.Backend

	// Locator — где суита работает.
	Locator blockbackend.Locator

	// OtherLocator — второе место размещения, отличное от [Options.Locator]. Нужно
	// переносу снимка и тома, а также проверке, что перечисление одного локатора не
	// показывает объекты другого: без второго локатора эта проверка не имеет
	// предмета, а изоляция арендаторов держится именно на ней.
	OtherLocator blockbackend.Locator
}

// Skipped — случай, который НЕ исполнялся, и почему.
type Skipped struct {
	Case   string
	Reason string
}

// Report — что прогон осмотрел и что нашёл.
type Report struct {
	Implementation string
	Kind           string
	Declared       int
	Executed       int
	Failed         int
	Skipped        []Skipped
}

// WriteCensus печатает объём осмотренного. Пишется ВСЕГДА — и на зелёном прогоне тоже:
// сколько случаев исполнено, есть такая же часть результата, как и сколько упало.
func (r Report) WriteCensus(w io.Writer) {
	_, _ = fmt.Fprintf(w, "blockbackend contract: реализация %q, вид %q\n", r.Implementation, r.Kind)
	_, _ = fmt.Fprintf(w, "  объявлено случаев: %d\n", r.Declared)
	_, _ = fmt.Fprintf(w, "  исполнено        : %d\n", r.Executed)
	_, _ = fmt.Fprintf(w, "  упало            : %d\n", r.Failed)
	_, _ = fmt.Fprintf(w, "  не исполнено     : %d\n", len(r.Skipped))
	for _, s := range r.Skipped {
		_, _ = fmt.Fprintf(w, "    - %s: %s\n", s.Case, s.Reason)
	}
}

// reasonNoFaults — причина, по которой отрицательные полосы остаются непроверенными.
// Вынесена константой, потому что по ней же считается итоговый отказ прогона.
const reasonNoFaults = "реализация не предоставила управляемую инъекцию отказа (contract.Faulty)"

// Run прогоняет суиту и роняет t на каждом расхождении.
//
// Возвращает отчёт, чтобы вызывающий мог утверждать что-то более узкое — например, что
// число исполненных случаев не упало после правки суиты.
func Run(t *testing.T, opts Options) Report {
	t.Helper()

	switch {
	case opts.Name == "":
		t.Fatalf("contract: не названа реализация — отчёт «прогнано против чего» был бы пуст")
	case opts.New == nil:
		t.Fatalf("contract: не задан конструктор реализации — прогонять нечего")
	case opts.Locator.Pool == "" || opts.Locator.Namespace == "":
		t.Fatalf("contract: локатор обязан нести пул и пространство арендатора")
	case opts.OtherLocator.Pool == "" || opts.OtherLocator.Namespace == "":
		t.Fatalf("contract: второй локатор обязателен — без него перенос и изоляция перечисления беспредметны")
	case opts.Locator == opts.OtherLocator:
		t.Fatalf("contract: второй локатор совпадает с первым — проверка изоляции стала бы тождественно истинной")
	}

	all := cases()
	assertEveryVerbCovered(t, all)

	rep := Report{Implementation: opts.Name, Declared: len(all)}
	r := &runner{opts: opts, rep: &rep}

	// Вид спрашивается у отдельного экземпляра: он константа реализации, и знать его
	// нужно до первого случая, чтобы перепись назвала, против чего шёл прогон даже
	// если первый же случай упадёт.
	rep.Kind = opts.New(t, fullCaps()).Kind()

	for _, c := range all {
		before := len(rep.Skipped)
		ok := t.Run(c.name, func(t *testing.T) { c.run(t, r) })
		switch {
		case len(rep.Skipped) > before:
			// случай не исполнялся — ни в исполненные, ни в упавшие
		case !ok:
			rep.Failed++
			rep.Executed++
		default:
			rep.Executed++
		}
	}

	rep.WriteCensus(os.Stdout)

	if n := countReason(rep.Skipped, reasonNoFaults); n > 0 {
		t.Errorf("contract %s: %d случай(ев) отрицательных полос не исполнены — %s. "+
			"Непроверенная классификация отказов не является проверенной",
			opts.Name, n, reasonNoFaults)
	}
	return rep
}

func countReason(ss []Skipped, reason string) int {
	n := 0
	for _, s := range ss {
		if s.Reason == reason {
			n++
		}
	}
	return n
}

// assertEveryVerbCovered требует, чтобы КАЖДЫЙ метод порта был назван хотя бы одним
// случаем.
//
// Перечень методов берётся отражением по самому интерфейсу, а не выписывается рядом:
// одиннадцатый глагол, добавленный в порт без случая в суите, обязан ронять прогон, а
// не проезжать молча. Рукописный перечень этого не даёт — он разошёлся бы с портом
// ровно тогда, когда порт вырос.
func assertEveryVerbCovered(t *testing.T, all []kase) {
	t.Helper()
	iface := reflect.TypeOf((*blockbackend.Backend)(nil)).Elem()
	for i := range iface.NumMethod() {
		name := iface.Method(i).Name
		covered := false
		for _, c := range all {
			if strings.Contains(c.name, name) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("contract: глагол порта %s не назван ни одним случаем суиты — "+
				"он остался бы непроверенным, а прогон зелёным", name)
		}
	}
}

// kase — один случай суиты. Имя обязано содержать имя проверяемого глагола: на этом
// стоит перепись покрытия выше.
type kase struct {
	name string
	run  func(t *testing.T, r *runner)
}

// runner несёт настройки прогона и копит перепись.
type runner struct {
	opts Options
	rep  *Report
}

// capName — имя способности, которую случай ТРЕБУЕТ от реализации.
type capName string

const (
	capSnapshots         capName = "snapshots"
	capCloneFromSnapshot capName = "cloneFromSnapshot"
	capCloneFromImage    capName = "cloneFromImage"
	capCloneKeepsParent  capName = "cloneKeepsParent"
)

func capValue(c blockbackend.Capabilities, n capName) bool {
	switch n {
	case capSnapshots:
		return c.Snapshots
	case capCloneFromSnapshot:
		return c.CloneFromSnapshot
	case capCloneFromImage:
		return c.CloneFromImage
	case capCloneKeepsParent:
		return c.CloneKeepsParent
	default:
		return false
	}
}

// fullCaps — реализация умеет всё. Посадка по умолчанию для случаев, которым
// способности безразличны.
func fullCaps() blockbackend.Capabilities {
	return blockbackend.Capabilities{
		Snapshots:         true,
		CloneFromSnapshot: true,
		CloneFromImage:    true,
		CloneKeepsParent:  true,
		OnlineGrow:        true,
		MultiAttach:       true,
		EncryptionAtRest:  true,
	}
}

// backend строит свежую реализацию и сверяет НАЗВАННЫЕ случаем способности с теми, что
// реализация объявила. Расхождение — не провал реализации: у адаптера способности
// константы, и он вправе не уметь того, что случай проверяет. Но и пройденным такой
// случай не считается — он уходит в перепись с причиной.
func (r *runner) backend(t *testing.T, caps blockbackend.Capabilities, need ...capName) blockbackend.Backend {
	t.Helper()
	b := r.opts.New(t, caps)
	got := b.Capabilities()
	for _, n := range need {
		if capValue(got, n) != capValue(caps, n) {
			r.skip(t, fmt.Sprintf("реализация объявила способность %s = %v, случаю нужна %v",
				n, capValue(got, n), capValue(caps, n)))
		}
	}
	return b
}

// faulty достаёт инъекцию отказа либо уводит случай в перепись.
func (r *runner) faulty(t *testing.T, b blockbackend.Backend) Faulty {
	t.Helper()
	f, ok := b.(Faulty)
	if !ok {
		r.skip(t, reasonNoFaults)
	}
	return f
}

// skip записывает случай как НЕ исполненный и прекращает его.
func (r *runner) skip(t *testing.T, reason string) {
	t.Helper()
	r.rep.Skipped = append(r.rep.Skipped, Skipped{Case: t.Name(), Reason: reason})
	t.Skipf("не исполнено: %s", reason)
}

// ref — объект в основном локаторе прогона.
func (r *runner) ref(name string) blockbackend.ObjectRef {
	return blockbackend.ObjectRef{Locator: r.opts.Locator, Name: name}
}

// otherRef — объект во втором локаторе прогона.
func (r *runner) otherRef(name string) blockbackend.ObjectRef {
	return blockbackend.ObjectRef{Locator: r.opts.OtherLocator, Name: name}
}

// mustOK — положительный контроль. Стоит рядом с каждым отрицанием: реализация,
// отвергающая вообще всё, обязана краснеть здесь, иначе набор отрицаний зеленел бы на
// ней целиком.
func mustOK(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: ожидался успех, получен отказ полосы %s: %v",
			what, blockbackend.OutcomeOf(err), err)
	}
}

// mustOutcome требует отказа НАЗВАННОЙ полосы. Утверждается полоса, а не только факт
// отказа: «отказал чем-нибудь» не отличает недоступность от отказа в правах, а от этого
// различия зависит, повторять ли операцию.
func mustOutcome(t *testing.T, err error, want blockbackend.Outcome, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: ожидался отказ полосы %s, получен успех — реализация приняла ввод, "+
			"который обязана отвергнуть", what, want)
	}
	if got := blockbackend.OutcomeOf(err); got != want {
		t.Fatalf("%s: ожидалась полоса %s, получена %s: %v", what, want, got, err)
	}
}

// observeState — читает состояние объекта, требуя, чтобы само чтение прошло.
func observeState(t *testing.T, b blockbackend.Backend, ref blockbackend.ObjectRef, what string) blockbackend.Observed {
	t.Helper()
	obs, err := b.Observe(t.Context(), ref)
	mustOK(t, err, what)
	return obs
}

// listAll перечисляет локатор до конца, страницами по limit, и возвращает имена.
// Заодно стережёт незавершаемый курсор: перечисление, у которого next не пустеет,
// повесило бы сверщик дрейфа, а не упало.
func listAll(t *testing.T, b blockbackend.Backend, loc blockbackend.Locator, limit int) []string {
	t.Helper()
	var (
		names  []string
		cursor string
	)
	for page := 0; ; page++ {
		if page > 100 {
			t.Fatalf("ListObjects: курсор не завершился за %d страниц — перечисление зациклено", page)
		}
		got, next, err := b.ListObjects(t.Context(), loc, cursor, limit)
		mustOK(t, err, "ListObjects")
		names = append(names, got...)
		if next == "" {
			return names
		}
		if next == cursor {
			t.Fatalf("ListObjects: курсор не двигается (%q) — перечисление зациклено", next)
		}
		cursor = next
	}
}
