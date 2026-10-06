// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// ntf3_module_flag_probe_test.go — общая часть проб полосы S1-A4 issue-2918
// (флаг модуля KACHO_<MODULE>_NOTIFICATIONS_ENABLED). Текст файла одинаков у
// compute, nlb, registry, storage, vpc; модульное — константы ntf3Module,
// ntf3Knob, ntf3ModulePkg, ntf3ModuleDir и пробы в ntf3_module_flag_test.go.
//
// Каждая проверка здесь сперва спрашивает фикстуру (близнец, перепись), потом
// испытуемого. У каждой — самопроверка на законном синтетическом предмете
// (TestNTF3ProbeSelfCheck_*): проба, не способная позеленеть на верной форме,
// вердикта не даёт.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/subscription"
)

type ntf3Holder struct {
	name string
	fn   any
}

var (
	ntf3OptionsType = reflect.TypeOf(journaltx.Options{})
	ntf3EnabledType = reflect.TypeOf(feed.Enabled{})
	ntf3BoolType    = reflect.TypeOf(false)
	ntf3ErrorType   = reflect.TypeOf((*error)(nil)).Elem()
)

// ntf3SetKnob ставит ручку флага (value == nil — снимает её вовсе).
func ntf3SetKnob(t *testing.T, value *string) {
	t.Helper()
	// t.Setenv регистрирует восстановление прежнего значения и тогда, когда
	// переменную тут же снимают.
	t.Setenv(ntf3Knob, "")
	if value == nil {
		if err := os.Unsetenv(ntf3Knob); err != nil {
			t.Fatalf("ФИКСТУРА: не снять %s: %v", ntf3Knob, err)
		}
		return
	}
	t.Setenv(ntf3Knob, *value)
}

// ntf3RequireKnobRefusal — NTF3-64: незаданная и неразбираемая ручка — отказ
// загрузки, называющий ручку и true | false; true и false принимаются.
func ntf3RequireKnobRefusal(t *testing.T, load func(*testing.T, *string) error) {
	t.Helper()
	// Вопрос: обе законные величины принимаются. Без этого отказ ниже мог бы
	// прийти от фикстуры, а не от ручки.
	for _, v := range []string{"true", "false"} {
		if err := load(t, &v); err != nil {
			t.Fatalf("ФИКСТУРА (близнец NTF3-64): %s=%s отвергнута — конфигурация пробы не годна, вердикта нет: %v",
				ntf3Knob, v, err)
		}
	}
	yes := "yes"
	for _, c := range []struct {
		name  string
		value *string
	}{
		{"не задана", nil},
		{"= yes", &yes},
	} {
		err := load(t, c.value)
		if err == nil {
			t.Errorf("NTF3-64: %s: загрузка и проверка конфигурации прошли при ручке %s %s — у ручки есть "+
				"умолчание, и процесс поднял бы слушатели (ожидался отказ старта, называющий ручку и true | false)",
				ntf3Module, ntf3Knob, c.name)
			continue
		}
		msg := err.Error()
		for _, want := range []string{ntf3Knob, "true", "false"} {
			if !strings.Contains(msg, want) {
				t.Errorf("NTF3-64: %s: отказ при ручке %s %s не называет %q: %q", ntf3Module, ntf3Knob, c.name, want, msg)
			}
		}
	}
}

// ntf3FlagArg — значение флага в форме параметра, которой его принимает
// потребитель: bool, journaltx.Options либо feed.Enabled (одно чтение ручки —
// одно значение у четырёх потребителей, З11). Иная форма — false.
func ntf3FlagArg(t *testing.T, typ reflect.Type, on bool) (reflect.Value, bool) {
	t.Helper()
	switch typ {
	case ntf3BoolType:
		return reflect.ValueOf(on), true
	case ntf3OptionsType:
		return reflect.ValueOf(journaltx.NewOptions(on)), true
	case ntf3EnabledType:
		en, err := feed.ParseEnabled(ntf3Knob, func(string) (string, bool) { return strconv.FormatBool(on), true })
		if err != nil {
			t.Fatalf("ФИКСТУРА: feed.ParseEnabled(%v): %v", on, err)
		}
		return reflect.ValueOf(en), true
	}
	return reflect.Value{}, false
}

// ntf3CallJournal зовёт конструктор журнала модуля со значением флага на месте
// параметра флага; прочие параметры — строка "probe" либо нулевое значение.
func ntf3CallJournal(t *testing.T, fn reflect.Value, flagIdx int, on bool) subscription.Journal {
	t.Helper()
	typ := fn.Type()
	args := make([]reflect.Value, 0, typ.NumIn())
	for i := 0; i < typ.NumIn(); i++ {
		in := typ.In(i)
		switch {
		case i == flagIdx:
			v, _ := ntf3FlagArg(t, in, on)
			args = append(args, v)
		case in.Kind() == reflect.String:
			args = append(args, reflect.ValueOf("probe").Convert(in))
		default:
			args = append(args, reflect.Zero(in))
		}
	}
	out := fn.Call(args)
	if len(out) == 0 {
		t.Fatalf("ФИКСТУРА: конструктор журнала ничего не вернул")
	}
	j, ok := out[0].Interface().(subscription.Journal)
	if !ok {
		t.Fatalf("ФИКСТУРА: конструктор журнала вернул %s, а не subscription.Journal", out[0].Type())
	}
	if len(out) > 1 && out[len(out)-1].Type().Implements(ntf3ErrorType) && !out[len(out)-1].IsNil() {
		t.Fatalf("NTF3-65/67: %s: конструктор журнала отказал при флаге %v: %v", ntf3Module, on, out[len(out)-1].Interface())
	}
	return j
}

// ntf3RequireKindsFollowTheFlag — NTF3-65 / NTF3-67: ключ журнала ленты
// объявлен в Mapping.Kinds ровно при включённом флаге и едет на провод видом
// notification_feed; прочие виды от флага не зависят (близнец по одному факту).
func ntf3RequireKindsFollowTheFlag(t *testing.T, journalFn any) {
	t.Helper()
	fn := reflect.ValueOf(journalFn)
	typ := fn.Type()
	flagIdx := -1
	for i := 0; i < typ.NumIn(); i++ {
		if _, ok := ntf3FlagArg(t, typ.In(i), true); ok {
			if flagIdx >= 0 {
				t.Fatalf("NTF3-65/67: %s: у конструктора журнала %s два параметра флага", ntf3Module, typ)
			}
			flagIdx = i
		}
	}
	if flagIdx < 0 {
		t.Fatalf("NTF3-65/67: %s: конструктор журнала subscriptionjournal.Journal %s не принимает флаг модуля "+
			"(bool | journaltx.Options | feed.Enabled) — словарь видов Mapping.Kinds от флага не зависит, "+
			"и ключ %q не может быть объявлен ровно при включённом флаге", ntf3Module, typ, feed.JournalKey)
	}
	on := ntf3CallJournal(t, fn, flagIdx, true)
	off := ntf3CallJournal(t, fn, flagIdx, false)

	others := func(j subscription.Journal) []string {
		var ks []string
		for k, v := range j.Mapping.Kinds {
			if k != feed.JournalKey {
				ks = append(ks, k+"→"+v.ObjectType)
			}
		}
		sort.Strings(ks)
		return ks
	}
	a, b := others(on), others(off)
	if len(b) == 0 {
		t.Fatalf("ФИКСТУРА: %s: словарь видов без ключа ленты пуст — сравнивать нечего", ntf3Module)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("NTF3-65/67: %s: флаг меняет не только ключ %q: при true %v, при false %v", ntf3Module, feed.JournalKey, a, b)
	}
	kind, ok := on.Mapping.Kinds[feed.JournalKey]
	if !ok {
		t.Errorf("NTF3-67: %s: при флаге true в Mapping.Kinds нет ключа %q — подписка с kinds [\"notification_feed\"] "+
			"была бы отвергнута", ntf3Module, feed.JournalKey)
	} else if kind.ObjectType != "notification_feed" {
		t.Errorf("NTF3-67: %s: ключ %q едет на провод видом %q, ожидался notification_feed", ntf3Module, feed.JournalKey, kind.ObjectType)
	}
	if _, ok := off.Mapping.Kinds[feed.JournalKey]; ok {
		t.Errorf("NTF3-65: %s: при флаге false в Mapping.Kinds объявлен ключ %q — подписка с kinds "+
			"[\"notification_feed\"] была бы открыта при выключенной ленте", ntf3Module, feed.JournalKey)
	}
}

// ntf3RequireBootPostureReportsTheFlag — NTF3-67: строка самоотчёта посадки
// несёт ровно одно поле флага ленты со значением ручки (близнецы true, false).
//
// Значение — написание фундамента (`observability.NotificationsOn` /
// `NotificationsOff`), строкой, а не bool: состояний у поля три, и «ленты нет»
// (`n/a`, нулевое значение) обязано быть отличимо от «лента выключена» — у
// модуля с лентой `n/a` есть отказ этой пробы, а не совпадение с false.
func ntf3RequireBootPostureReportsTheFlag(t *testing.T, line func(*testing.T, string) map[string]any) {
	t.Helper()
	for _, c := range []struct {
		value string
		want  string
	}{{"true", observability.NotificationsOn}, {"false", observability.NotificationsOff}} {
		got := line(t, c.value)
		if len(got) == 0 {
			t.Fatalf("ФИКСТУРА: %s: строка самоотчёта пуста", ntf3Module)
		}
		var keys []string
		for k := range got {
			if strings.Contains(strings.ToLower(k), "notification") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) != 1 {
			t.Errorf("NTF3-67: %s: строка самоотчёта посадки при %s=%s несёт полей флага %d (%v), ожидалось одно "+
				"со значением %v; строка: %v", ntf3Module, ntf3Knob, c.value, len(keys), keys, c.want, got)
			continue
		}
		if got[keys[0]] != c.want {
			t.Errorf("NTF3-67: %s: самоотчёт посадки %s = %v (%T) при %s=%s, ожидалось %q",
				ntf3Module, keys[0], got[keys[0]], got[keys[0]], ntf3Knob, c.value, c.want)
		}
	}
}

// ntf3GaugeName — серия флага ленты модуля (NTF1-N09; NTF3-65, NTF3-67).
const ntf3GaugeName = "kacho_notifications_enabled"

// ntf3RequireGaugeFollowsTheFlag — NTF3-65 / NTF3-67: серия
// kacho_notifications_enabled{module="<модуль>"} заводится при ОБОИХ значениях
// флага — 1 при true, 0 при false («выключено» отличимо от «серии нет»).
// register — путь корня, которым он ставит серию, на чистом реестре.
func ntf3RequireGaugeFollowsTheFlag(t *testing.T, register func(*testing.T, string, prometheus.Registerer) error) {
	t.Helper()
	for _, c := range []struct {
		value string
		want  float64
	}{{"true", 1}, {"false", 0}} {
		reg := prometheus.NewRegistry()
		if err := register(t, c.value, reg); err != nil {
			t.Errorf("NTF3-67: %s: серия флага при %s=%s не заведена: %v", ntf3Module, ntf3Knob, c.value, err)
			continue
		}
		mfs, err := reg.Gather()
		if err != nil {
			t.Fatalf("ФИКСТУРА: %s: сбор реестра: %v", ntf3Module, err)
		}
		var values []float64
		for _, mf := range mfs {
			if mf.GetName() != ntf3GaugeName {
				continue
			}
			for _, m := range mf.GetMetric() {
				for _, l := range m.GetLabel() {
					if l.GetName() == "module" && l.GetValue() == ntf3Module {
						values = append(values, m.GetGauge().GetValue())
					}
				}
			}
		}
		if len(values) != 1 {
			t.Errorf("NTF3-67: %s: серий %s{module=%q} при %s=%s %d, ожидалась одна — "+
				"«выключено» обязано быть отличимо от «серии нет»", ntf3Module, ntf3GaugeName, ntf3Module, ntf3Knob, c.value, len(values))
			continue
		}
		if values[0] != c.want {
			t.Errorf("NTF3-67: %s: %s{module=%q} = %v при %s=%s, ожидалось %v",
				ntf3Module, ntf3GaugeName, ntf3Module, values[0], ntf3Knob, c.value, c.want)
		}
	}
}

// ntf3FeedName — локальное имя импорта corelib/notify/feed в файле ("" — не импортирован).
func ntf3FeedName(f *ast.File) string {
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if p != "github.com/PRO-Robotech/corelib/notify/feed" {
			continue
		}
		if im.Name != nil {
			return im.Name.Name
		}
		return "feed"
	}
	return ""
}

// ntf3GaugeHelper — функция корня, которой он ставит серию флага.
const ntf3GaugeHelper = "registerNotificationsGauge"

// ntf3RequireRootRegistersTheGauge — серию флага ставит КОРЕНЬ, путём фундамента:
// в не-тестовом дереве модуля ровно один вызов feed.RegisterEnabledGauge, он
// лежит в cmd/ внутри функции registerNotificationsGauge и несёт имя модуля
// литералом; сама функция зовётся из не-тестового кода корня. Проба поведения
// (ntf3RequireGaugeFollowsTheFlag) судит эту функцию — эта судит, что старт её
// зовёт.
func ntf3RequireRootRegistersTheGauge(t *testing.T, dir string) {
	t.Helper()
	files, fset := ntf3NonTestFiles(t, dir)
	var sites, misplaced, helperCalls []string
	for rel, f := range files {
		if !strings.HasPrefix(rel, "cmd/") {
			continue
		}
		name := ntf3FeedName(f)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				coord := fset.Position(call.Pos()).String()
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == ntf3GaugeHelper && fd.Name.Name != ntf3GaugeHelper {
					helperCalls = append(helperCalls, coord)
				}
				if name == "" || !ntf3IsSel(call.Fun, name, "RegisterEnabledGauge") {
					return true
				}
				sites = append(sites, coord)
				lit, ok := (ast.Expr)(nil), false
				if len(call.Args) == 3 {
					lit, ok = call.Args[1], true
				}
				bl, isLit := lit.(*ast.BasicLit)
				if fd.Name.Name != ntf3GaugeHelper || !ok || !isLit || bl.Value != strconv.Quote(ntf3Module) {
					misplaced = append(misplaced, coord)
				}
				return true
			})
		}
	}
	// Вызовы вне cmd/ — тоже места установки серии, и их место — не там.
	for rel, f := range files {
		if strings.HasPrefix(rel, "cmd/") {
			continue
		}
		name := ntf3FeedName(f)
		if name == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && ntf3IsSel(call.Fun, name, "RegisterEnabledGauge") {
				coord := fset.Position(call.Pos()).String()
				sites = append(sites, coord)
				misplaced = append(misplaced, coord)
			}
			return true
		})
	}
	sort.Strings(sites)
	sort.Strings(misplaced)
	sort.Strings(helperCalls)
	t.Logf("%s: не-тестовых файлов %d, вызовов feed.RegisterEnabledGauge %d, вызовов %s из корня %d",
		ntf3Module, len(files), len(sites), ntf3GaugeHelper, len(helperCalls))
	if len(sites) != 1 {
		t.Errorf("NTF3-67: %s: вызовов feed.RegisterEnabledGauge %d, ожидался один (корень): %v", ntf3Module, len(sites), sites)
	}
	if len(misplaced) > 0 {
		t.Errorf("NTF3-67: %s: серия флага ставится не функцией корня %s с именем модуля %q литералом:\n  %s",
			ntf3Module, ntf3GaugeHelper, ntf3Module, strings.Join(misplaced, "\n  "))
	}
	if len(helperCalls) == 0 {
		t.Errorf("NTF3-67: %s: %s не зовётся из не-тестового кода корня — серия на старте не ставится", ntf3Module, ntf3GaugeHelper)
	}
}

// ntf3CallHolder зовёт конструктор держателя: opts — на месте параметра
// journaltx.Options, прочие параметры — нулевые значения. Паника — не вердикт.
func ntf3CallHolder(t *testing.T, h ntf3Holder, opts journaltx.Options) error {
	t.Helper()
	fn := reflect.ValueOf(h.fn)
	typ := fn.Type()
	n := typ.NumIn()
	if typ.IsVariadic() {
		n--
	}
	args := make([]reflect.Value, 0, n)
	for i := 0; i < n; i++ {
		if typ.In(i) == ntf3OptionsType {
			args = append(args, reflect.ValueOf(opts))
		} else {
			args = append(args, reflect.Zero(typ.In(i)))
		}
	}
	var out []reflect.Value
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: конструктор %s запаниковал на нулевых зависимостях — вердикта нет: %v", h.name, r)
			}
		}()
		out = fn.Call(args)
	}()
	last := out[len(out)-1]
	if last.Type().Implements(ntf3ErrorType) && !last.IsNil() {
		return last.Interface().(error)
	}
	return nil
}

// ntf3RequireHoldersCoverTheTree — перечень пробы покрывает каждого держателя
// journaltx.Options в не-тестовом дереве модуля.
func ntf3RequireHoldersCoverTheTree(t *testing.T, holders []ntf3Holder) {
	t.Helper()
	census := ntf3HolderCensus(t, ntf3ModuleDir)
	if len(holders) == 0 || len(census) == 0 {
		t.Fatalf("ФИКСТУРА: %s: перечень держателей %d, перепись дерева %d — пустой обход вердиктом не является",
			ntf3Module, len(holders), len(census))
	}
	covered := map[string]bool{}
	for _, h := range holders {
		rt := reflect.TypeOf(h.fn).Out(0)
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		covered[strings.TrimPrefix(rt.PkgPath(), ntf3ModulePkg+"/")+"."+rt.Name()] = true
	}
	var missing []string
	for key, coord := range census {
		if !covered[key] {
			missing = append(missing, key+" ("+coord+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("ФИКСТУРА: %s: держат journaltx.Options, а конструктора в перечне пробы нет:\n  %s",
			ntf3Module, strings.Join(missing, "\n  "))
	}
	t.Logf("%s: держателей journaltx.Options в дереве %d, конструкторов в перечне %d", ntf3Module, len(census), len(holders))
}

// ntf3RequireHolderRefusesZero — УК3-61 на одном конструкторе: Options —
// позиционный параметр, отказ — результат error; построенные собраны, нулевые
// отвергнуты journaltx.ErrOptionsUnset.
func ntf3RequireHolderRefusesZero(t *testing.T, h ntf3Holder) {
	t.Helper()
	typ := reflect.TypeOf(h.fn)
	nOpts := 0
	for i := 0; i < typ.NumIn(); i++ {
		if typ.In(i) == ntf3OptionsType {
			nOpts++
		}
	}
	if nOpts != 1 {
		t.Fatalf("УК3-61: %s: конструктор %s %s не принимает journaltx.Options позиционно (параметров Options: %d) — "+
			"писатель журнала берёт флаг не из корня", ntf3Module, h.name, typ, nOpts)
	}
	if !typ.Out(typ.NumOut() - 1).Implements(ntf3ErrorType) {
		t.Fatalf("УК3-61: %s: конструктор %s %s принимает Options, но не возвращает error — нулевые Options "+
			"при сборке корня не отвергаются", ntf3Module, h.name, typ)
	}
	// Близнец первым: построенные Options собраны.
	for _, on := range []bool{true, false} {
		if err := ntf3CallHolder(t, h, journaltx.NewOptions(on)); err != nil {
			t.Fatalf("УК3-61 (близнец): %s: %s отверг NewOptions(%v): %v", ntf3Module, h.name, on, err)
		}
	}
	err := ntf3CallHolder(t, h, journaltx.Options{})
	if err == nil {
		t.Fatalf("УК3-61: %s: %s собран на нулевых journaltx.Options — корень, не передавший флаг, стартовал бы", ntf3Module, h.name)
	}
	if !errors.Is(err, journaltx.ErrOptionsUnset) {
		t.Fatalf("УК3-61: %s: %s отверг нулевые Options не journaltx.ErrOptionsUnset: %v", ntf3Module, h.name, err)
	}
}

func ntf3RequireHoldersRefuseZeroOptions(t *testing.T, holders []ntf3Holder) {
	t.Helper()
	ntf3RequireHoldersCoverTheTree(t, holders)
	for _, h := range holders {
		t.Run(h.name, func(t *testing.T) { ntf3RequireHolderRefusesZero(t, h) })
	}
}

// ntf3NonTestFiles разбирает не-тестовые .go файлы дерева dir.
func ntf3NonTestFiles(t *testing.T, dir string) (map[string]*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = f
		return nil
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: обход дерева %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("ФИКСТУРА: обход дерева %s дал 0 файлов", dir)
	}
	return files, fset
}

// ntf3JournaltxName — локальное имя импорта corelib/journaltx в файле ("" — не импортирован).
func ntf3JournaltxName(f *ast.File) string {
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if p != "github.com/PRO-Robotech/corelib/journaltx" {
			continue
		}
		if im.Name != nil {
			return im.Name.Name
		}
		return "journaltx"
	}
	return ""
}

func ntf3IsSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg && s.Sel.Name == name
}

// ntf3HolderCensus — типы-структуры не-тестового дерева с полем
// journaltx.Options: "<каталог>.<Тип>" → координата.
func ntf3HolderCensus(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, fset := ntf3NonTestFiles(t, dir)
	out := map[string]string{}
	for rel, f := range files {
		name := ntf3JournaltxName(f)
		if name == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fld := range st.Fields.List {
				if ntf3IsSel(fld.Type, name, "Options") {
					out[filepath.ToSlash(filepath.Dir(rel))+"."+ts.Name.Name] = fset.Position(ts.Pos()).String()
				}
			}
			return true
		})
	}
	return out
}

// ntf3RequireOptionsBuiltOnce — И6: journaltx.NewOptions в не-тестовом дереве
// модуля зовётся ровно в одном месте, и это место — корень (cmd/) либо пакет
// загрузчика конфигурации. Писатель, строящий Options сам, читает флаг вторым
// чтением — либо не читает его вовсе.
func ntf3RequireOptionsBuiltOnce(t *testing.T, dir string) {
	t.Helper()
	files, fset := ntf3NonTestFiles(t, dir)
	var sites, misplaced []string
	importers := 0
	for rel, f := range files {
		name := ntf3JournaltxName(f)
		if name == "" {
			continue
		}
		importers++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !ntf3IsSel(call.Fun, name, "NewOptions") {
				return true
			}
			coord := fset.Position(call.Pos()).String()
			sites = append(sites, coord)
			if !strings.HasPrefix(rel, "cmd/") && !strings.Contains("/"+filepath.ToSlash(filepath.Dir(rel))+"/", "/config/") {
				misplaced = append(misplaced, coord)
			}
			return true
		})
	}
	sort.Strings(sites)
	sort.Strings(misplaced)
	t.Logf("%s: не-тестовых файлов %d, импортирующих journaltx %d, вызовов NewOptions %d", ntf3Module, len(files), importers, len(sites))
	if importers == 0 {
		t.Fatalf("ФИКСТУРА: %s: ни один файл не импортирует corelib/journaltx — перепись не видит предмета", ntf3Module)
	}
	if len(misplaced) > 0 {
		t.Errorf("И6: %s: journaltx.NewOptions вне корня и загрузчика — флаг писателя не из чтения ручки:\n  %s",
			ntf3Module, strings.Join(misplaced, "\n  "))
	}
	if len(sites) != 1 {
		t.Errorf("И6: %s: мест построения journaltx.Options %d, ожидалось ровно одно (корень строит из одного чтения ручки): %s",
			ntf3Module, len(sites), fmt.Sprint(sites))
	}
}

// ─── самопроверка проб на законном синтетическом предмете ───────────────────

type ntf3LawfulHolder struct{ journal journaltx.Options }

func ntf3NewLawfulHolder(_ *int, opts journaltx.Options, _ ...string) (*ntf3LawfulHolder, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", ntf3Module, err)
	}
	return &ntf3LawfulHolder{journal: opts}, nil
}

func ntf3LawfulJournal(_ string, on bool) subscription.Journal {
	kinds := map[string]subscription.Kind{"Probe": {ObjectType: "probe_object"}}
	if on {
		kinds[feed.JournalKey] = subscription.Kind{ObjectType: "notification_feed"}
	}
	return subscription.Journal{Mapping: subscription.Mapping{Kinds: kinds}}
}

// TestNTF3ProbeSelfCheck_LawfulSubjectsAreGreen — пробы полосы зелёны на
// законной форме предмета: конструктор с позиционными Options и отказом
// нулевых; журнал, чей словарь видов следует флагу; самоотчёт с полем флага;
// серия флага фундамента при обоих значениях и её установка функцией корня;
// загрузчик, отвергающий незаданную ручку; дерево с одним местом построения
// Options в корне.
func TestNTF3ProbeSelfCheck_LawfulSubjectsAreGreen(t *testing.T) {
	ntf3RequireHolderRefusesZero(t, ntf3Holder{"ntf3NewLawfulHolder", ntf3NewLawfulHolder})
	ntf3RequireKindsFollowTheFlag(t, ntf3LawfulJournal)
	ntf3RequireBootPostureReportsTheFlag(t, func(_ *testing.T, v string) map[string]any {
		return map[string]any{"service": ntf3Module, "notifications_enabled": v}
	})
	ntf3RequireGaugeFollowsTheFlag(t, func(t *testing.T, v string, reg prometheus.Registerer) error {
		en, err := feed.ParseEnabled(ntf3Knob, func(string) (string, bool) { return v, true })
		if err != nil {
			t.Fatalf("ФИКСТУРА: разбор %s=%s: %v", ntf3Knob, v, err)
		}
		_, err = feed.RegisterEnabledGauge(reg, ntf3Module, en)
		return err
	})
	ntf3RequireKnobRefusal(t, func(t *testing.T, v *string) error {
		ntf3SetKnob(t, v)
		_, err := feed.ParseEnabled(ntf3Knob, os.LookupEnv)
		return err
	})

	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("cmd/probe/main.go", "package main\n\nimport jt \"github.com/PRO-Robotech/corelib/journaltx\"\n\nvar opts = jt.NewOptions(true)\n")
	write("internal/repo/repo.go", "package repo\n\nimport \"github.com/PRO-Robotech/corelib/journaltx\"\n\n"+
		"type Repo struct{ journal journaltx.Options }\n")
	write("cmd/probe/gauge.go", "package main\n\nimport (\n\t\"github.com/PRO-Robotech/corelib/notify/feed\"\n"+
		"\t\"github.com/prometheus/client_golang/prometheus\"\n)\n\n"+
		"func "+ntf3GaugeHelper+"(reg prometheus.Registerer, en feed.Enabled) error {\n"+
		"\t_, err := feed.RegisterEnabledGauge(reg, "+strconv.Quote(ntf3Module)+", en)\n\treturn err\n}\n\n"+
		"func boot(reg prometheus.Registerer, en feed.Enabled) error { return "+ntf3GaugeHelper+"(reg, en) }\n")
	ntf3RequireOptionsBuiltOnce(t, dir)
	ntf3RequireRootRegistersTheGauge(t, dir)
	if got := ntf3HolderCensus(t, dir); len(got) != 1 || got["internal/repo.Repo"] == "" {
		t.Fatalf("перепись держателей синтетического дерева: %v, ожидался internal/repo.Repo", got)
	}
}
