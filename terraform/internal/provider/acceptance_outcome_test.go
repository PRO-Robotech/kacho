// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Исход приёмочной пробы: «провайдер не поднялся» и «провайдер поднялся и повёл себя
// не так» — РАЗНЫЕ категории, и группа их различает (kacho#2771).
//
// # Что было
//
// Приёмочные пробы поднимают провайдера через go-plugin: либо в том же процессе с
// перецеплением (resource.UnitTest), либо собранным двоичным файлом, который запускает
// исполнитель цикла (прямой цикл переезда типов). В обоих случаях провайдер слушает
// unix-сокет, и путь к нему собирается из временного каталога: `$TMPDIR/pluginNNNNNNNNNN`.
// Путь unix-сокета ограничен ядром (108 байт вместе с завершающим нулём на Linux, 104
// на darwin); при длинном TMPDIR слушатель не создаётся, и провайдер не поднимается
// вовсе. Замер 2026-09-27 на 1d42a6728bf: TMPDIR в 91 знак — 28 приёмочных проб
// красные текстами «timeout waiting on reattach config» и «Failed to read any lines
// from plugin's stdout»; тот же прогон с коротким TMPDIR при нагрузке 65 — зелёный,
// 37 проб. Корень — не нагрузка и не поведение провайдера, а длина пути сокета.
//
// И группа печатала это ОТКАЗОМ: «провайдер не поднялся» было неотличимо от «провайдер
// ответил не то», и чинящий шёл искать дефект поведения там, где провайдер не запускался.
//
// # Что стало
//
//  1. Каталог сокетов выбирается так, чтобы путь поместился (accSocketDirFor): заданный
//     оператором PLUGIN_UNIX_SOCKET_DIR — как есть, иначе временный каталог процесса,
//     иначе короткий общесистемный. Не поместился ни один — это «не выполнилось» с
//     названной причиной, а не красное.
//  2. Каждый отказ пробы, чей текст — отказ ЗАПУСКА провайдера (рукопожатие go-plugin,
//     перецепление, сборка двоичного файла), помечается «НЕ ВЫПОЛНИЛОСЬ»; прочие отказы
//     — «КРАСНОЕ». Метка стоит в тексте отказа самой пробы.
//  3. После прогона пакет печатает строку итога с числом каждой категории, а двоичный
//     файл пробы выходит кодом 3, если все отказы — «не выполнилось» (и только тогда, когда
//     это можно утверждать: каждая выбранная проба учтена). `go test` сводит любой
//     ненулевой код к 1, поэтому группе, которая хочет различать категории по коду, нужен
//     запуск собранного двоичного файла либо чтение строки итога.

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfinterface "github.com/mitchellh/go-testing-interface"
)

// ---- категории исхода --------------------------------------------------------------------

type accCategory int

const (
	accPassed accCategory = iota + 1
	accRed
	accNotExecuted
)

// accNotExecutedLabel — метка третьей категории в тексте отказа и в строке итога.
const accNotExecutedLabel = "НЕ ВЫПОЛНИЛОСЬ"

// accStartupSignatures — тексты, которыми go-plugin, terraform-plugin-testing и
// исполнитель цикла сообщают, что ПРОВАЙДЕР НЕ ПОДНЯЛСЯ. Каждый снят с настоящего отказа
// (захват — в шапке файла и в пробах-инъекциях ниже), а не выписан из документации.
var accStartupSignatures = []string{
	// terraform-plugin-testing: провайдер в процессе не отдал конфигурацию перецепления.
	"unable to serve provider",
	"timeout waiting on reattach config",
	"nil reattach config",
	// Исполнитель цикла: двоичный файл провайдера не прошёл рукопожатие go-plugin.
	"Failed to read any lines from plugin's stdout",
	"failed to negotiate the initial go-plugin protocol handshake",
	"Unrecognized remote plugin message",
	"plugin exited before we could connect",
}

// accStartupSignatureIn — первая подпись отказа запуска в тексте; "" — её нет.
func accStartupSignatureIn(text string) string {
	for _, s := range accStartupSignatures {
		if strings.Contains(text, s) {
			return s
		}
	}
	return ""
}

// accIsStartupFailure — текст отказа говорит о том, что провайдер не поднялся.
func accIsStartupFailure(text string) bool { return accStartupSignatureIn(text) != "" }

// accOutcomes — учёт исходов проб, исполнивших цикл terraform.
var accOutcomes = struct {
	sync.Mutex
	notRun   map[string]bool // проба отказала запуском провайдера
	category map[string]accCategory
}{notRun: map[string]bool{}, category: map[string]accCategory{}}

// accMarkNotExecuted — проба по имени отказала тем, что провайдер не поднялся.
func accMarkNotExecuted(name string) {
	accOutcomes.Lock()
	defer accOutcomes.Unlock()
	accOutcomes.notRun[name] = true
}

// accTrack учитывает пробу: по её окончании записывается категория исхода. Отказ пробы,
// не помеченный «не выполнилось», — красное, чем бы он ни был вызван.
func accTrack(t *testing.T) {
	t.Helper()
	name := t.Name()
	t.Cleanup(func() {
		if t.Skipped() {
			return
		}
		accOutcomes.Lock()
		defer accOutcomes.Unlock()
		switch {
		case !t.Failed():
			accOutcomes.category[name] = accPassed
		case accOutcomes.notRun[name]:
			accOutcomes.category[name] = accNotExecuted
		default:
			accOutcomes.category[name] = accRed
		}
	})
}

// accCounts — число проб каждой категории.
func accCounts() (passed, red, notExecuted int, names map[string]bool) {
	accOutcomes.Lock()
	defer accOutcomes.Unlock()
	names = map[string]bool{}
	for n, c := range accOutcomes.category {
		names[n] = true
		switch c {
		case accPassed:
			passed++
		case accRed:
			red++
		case accNotExecuted:
			notExecuted++
		}
	}
	return passed, red, notExecuted, names
}

// ---- классифицирующий исполнитель ---------------------------------------------------------

// accT — исполнитель, которого получает terraform-plugin-testing. Отказы библиотеки
// проходят через него и получают метку категории: провайдер не поднялся — «НЕ
// ВЫПОЛНИЛОСЬ», прочее — «КРАСНОЕ».
type accT struct {
	tfinterface.T
	name string
}

func (a accT) label(msg string) string {
	if accIsStartupFailure(msg) {
		accMarkNotExecuted(a.name)
		return accNotExecutedLabel + " (провайдер не поднялся — о его поведении прогон не сказал ничего): " + msg
	}
	return "КРАСНОЕ (провайдер поднялся и повёл себя не так): " + msg
}

func (a accT) Fatal(args ...any) { a.T.Helper(); a.T.Fatal(a.label(fmt.Sprint(args...))) }
func (a accT) Fatalf(format string, args ...any) {
	a.T.Helper()
	a.T.Fatal(a.label(fmt.Sprintf(format, args...)))
}
func (a accT) Error(args ...any) { a.T.Helper(); a.T.Error(a.label(fmt.Sprint(args...))) }
func (a accT) Errorf(format string, args ...any) {
	a.T.Helper()
	a.T.Error(a.label(fmt.Sprintf(format, args...)))
}

// accUnitTest — ЕДИНСТВЕННАЯ дверь приёмочных проб к resource.UnitTest: исход каждого
// цикла классифицируется. Что её обходят ноль проб, держит TestAcceptanceHarnessIsTheOnlyDoor.
func accUnitTest(t *testing.T, tc resource.TestCase) {
	t.Helper()
	accRunUnitTest(accT{T: t, name: t.Name()}, tc)
}

// accRunUnitTest — то же для подставного исполнителя самопроверки.
func accRunUnitTest(t tfinterface.T, tc resource.TestCase) {
	t.Helper()
	resource.UnitTest(t, tc)
}

// accFataler — то, что пометке «не выполнилось» нужно от пробы. Интерфейс, а не
// *testing.T: путь пометки проверяется подставным исполнителем.
type accFataler interface {
	Helper()
	Name() string
	Fatalf(format string, args ...any)
}

// accStartupFailed — отказ запуска провайдера, найденный в выводе исполнителя цикла.
func accStartupFailed(t accFataler, what, out string) {
	t.Helper()
	accMarkNotExecuted(t.Name())
	t.Fatalf("%s (провайдер не поднялся — о его поведении прогон не сказал ничего): %s\n%s",
		accNotExecutedLabel, what, out)
}

// ---- каталог сокетов go-plugin ------------------------------------------------------------

// accPluginSocketEnv — переменная, которой go-plugin назначает каталог unix-сокетов;
// её читает и провайдер в процессе, и двоичный файл, запущенный исполнителем цикла.
const accPluginSocketEnv = "PLUGIN_UNIX_SOCKET_DIR"

// accSocketSuffixLen — сколько знаков go-plugin добавляет к каталогу: "/plugin" и
// случайный суффикс os.CreateTemp (десятичное uint32, до 10 знаков).
const accSocketSuffixLen = len("/plugin") + 10

// accSocketPathLimit — предел длины пути unix-сокета без завершающего нуля.
func accSocketPathLimit() int {
	if runtime.GOOS == "linux" {
		return 107
	}
	return 103
}

// accSocketFits — поместится ли путь сокета, собранный в каталоге dir.
func accSocketFits(dir string) bool {
	return len(dir)+accSocketSuffixLen <= accSocketPathLimit()
}

// accShortSocketBase — общесистемный короткий каталог, куда уходит сокет, если
// временный каталог процесса длинен. Короткий путь здесь не удобство, а условие: путь
// длиннее предела ядро не принимает.
const accShortSocketBase = "/tmp"

// accSocketDirFor выбирает каталог сокетов. Возвращает:
//   - set — значение для PLUGIN_UNIX_SOCKET_DIR ("" — оставить умолчание go-plugin);
//   - made — каталог, заведённый здесь и подлежащий уборке;
//   - err — ни один кандидат не помещается: условие проб не создано.
//
// Заданный оператором каталог сильнее выбора, но и он обязан помещаться: подменять его
// молча значило бы спорить с оператором, а принять длинный — отдать пробы в отказ.
func accSocketDirFor(explicit, tempDir string, mkdir func(base string) (string, error)) (set, made string, err error) {
	if explicit != "" {
		if accSocketFits(explicit) {
			return explicit, "", nil
		}
		return "", "", fmt.Errorf("%s=%q: путь сокета займёт %d знаков при пределе %d",
			accPluginSocketEnv, explicit, len(explicit)+accSocketSuffixLen, accSocketPathLimit())
	}
	if accSocketFits(tempDir) {
		return "", "", nil
	}
	if accSocketFits(accShortSocketBase) {
		d, err := mkdir(accShortSocketBase)
		if err == nil && accSocketFits(d) {
			return d, d, nil
		}
		if err == nil {
			_ = os.Remove(d)
			err = fmt.Errorf("заведённый каталог %q длиннее предела", d)
		}
		return "", "", fmt.Errorf("временный каталог %q длинен для сокета (%d знаков при пределе %d), "+
			"а короткий каталог сокетов не заведён: %w", tempDir, len(tempDir)+accSocketSuffixLen,
			accSocketPathLimit(), err)
	}
	return "", "", fmt.Errorf("ни временный каталог %q, ни %q не вмещают путь сокета", tempDir, accShortSocketBase)
}

// accSocketErr — причина, по которой каталог сокетов не выбран; её получает КАЖДАЯ проба
// цикла как «не выполнилось».
var accSocketErr error

// accPrepareSockets — зовётся из TestMain до прогона. Возвращает уборку.
func accPrepareSockets() func() {
	set, made, err := accSocketDirFor(os.Getenv(accPluginSocketEnv), os.TempDir(),
		func(base string) (string, error) { return os.MkdirTemp(base, "kp-") })
	if err != nil {
		accSocketErr = err
		return func() {}
	}
	if set != "" {
		if err := os.Setenv(accPluginSocketEnv, set); err != nil {
			accSocketErr = err
		}
	}
	return func() {
		if made != "" {
			_ = os.RemoveAll(made)
		}
	}
}

// ---- строка итога и код выхода -------------------------------------------------------------

// accSummary — строка итога прогона пакета. Печатается, только если хоть одна
// приёмочная проба исполнилась (пропущенные под -short не учитываются): иначе «ноль»
// значил бы «не спрашивали», а не «всё хорошо».
func accSummary() (string, bool) {
	passed, red, notExecuted, _ := accCounts()
	total := passed + red + notExecuted
	if total == 0 {
		return "", false
	}
	return fmt.Sprintf("ИТОГ ПРИЁМКИ ПРОВАЙДЕРА: учтено приёмочных проб %d · зелёных %d · красных %d · "+
		"%s %d (провайдер не поднялся)", total, passed, red, accNotExecutedLabel, notExecuted), true
}

// accExitCode — код выхода двоичного файла пробы. Код 3 («не выполнилось») — только
// если все отказы помечены так И каждая выбранная проба учтена: иначе неучтённая
// красная проба спряталась бы за третью категорию.
func accExitCode(code int, selectedAllTracked bool) int {
	if code == 0 || !selectedAllTracked {
		return code
	}
	_, red, notExecuted, _ := accCounts()
	if notExecuted > 0 && red == 0 {
		return 3
	}
	return code
}

// accTrackerRoots — функции оснастки, вызов которых учитывает пробу.
var accTrackerRoots = []string{"accTrack"}

// accPackageTests — имена проб пакета и множество функций, чей вызов учитывает пробу
// (транзитивно внутри пакета).
func accPackageTests(dir string) (tests []string, tracked map[string]bool, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, nil, err
	}
	calls := map[string]map[string]bool{}
	for _, p := range paths {
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, err
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			name := fd.Name.Name
			if strings.HasPrefix(name, "Test") && name != "TestMain" {
				tests = append(tests, name)
			}
			callees := map[string]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok {
						callees[id.Name] = true
					}
				}
				return true
			})
			calls[name] = callees
		}
	}
	tracked = map[string]bool{}
	for _, r := range accTrackerRoots {
		tracked[r] = true
	}
	for changed := true; changed; {
		changed = false
		for fn, callees := range calls {
			if tracked[fn] {
				continue
			}
			for c := range callees {
				if tracked[c] {
					tracked[fn] = true
					changed = true
					break
				}
			}
		}
	}
	sort.Strings(tests)
	return tests, tracked, nil
}

// accSelectedAllTracked — каждая проба, выбранная флагами прогона, учитывается.
func accSelectedAllTracked() bool {
	tests, tracked, err := accPackageTests(".")
	if err != nil || len(tests) == 0 {
		return false
	}
	runRe, skipRe, err := accSelectionFlags()
	if err != nil {
		return false
	}
	for _, name := range tests {
		if runRe != nil && !runRe.MatchString(name) {
			continue
		}
		if skipRe != nil && skipRe.MatchString(name) {
			continue
		}
		if !tracked[name] {
			return false
		}
	}
	return true
}

// accSelectionFlags — выражения -test.run и -test.skip (только верхний уровень имени).
func accSelectionFlags() (run, skip *regexp.Regexp, err error) {
	compile := func(flagName string) (*regexp.Regexp, error) {
		f := flag.Lookup(flagName)
		if f == nil || f.Value.String() == "" {
			return nil, nil
		}
		top := strings.SplitN(f.Value.String(), "/", 2)[0]
		return regexp.Compile(top)
	}
	if run, err = compile("test.run"); err != nil {
		return nil, nil, err
	}
	if skip, err = compile("test.skip"); err != nil {
		return nil, nil, err
	}
	return run, skip, nil
}
