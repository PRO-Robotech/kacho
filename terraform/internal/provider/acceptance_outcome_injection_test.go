// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Инъекции разделения исходов (kacho#2771): «провайдер не поднялся» даёт «НЕ
// ВЫПОЛНИЛОСЬ», «поднялся и ответил не то» — «КРАСНОЕ», и ни одно не выдаётся за
// другое. Входы настоящие: провайдер, которому не дали слушателя; двоичный файл, не
// говорящий протоколом go-plugin, перед настоящим исполнителем цикла; сборка из
// несобираемого пакета; поднятый провайдер с заведомо неверным ожиданием.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	tfinterface "github.com/mitchellh/go-testing-interface"
)

// accRecorder — подставной исполнитель для terraform-plugin-testing: записывает
// отказы вместо того, чтобы ронять пробу, и исполняет уборку по окончании.
type accRecorder struct {
	name     string
	mu       sync.Mutex
	msgs     []string
	failed   bool
	skipped  bool
	cleanups []func()
}

var _ tfinterface.T = (*accRecorder)(nil)

func (r *accRecorder) note(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	r.failed = true
}
func (r *accRecorder) Cleanup(f func())             { r.cleanups = append(r.cleanups, f) }
func (r *accRecorder) Error(args ...any)            { r.note(fmt.Sprint(args...)) }
func (r *accRecorder) Errorf(f string, args ...any) { r.note(fmt.Sprintf(f, args...)) }
func (r *accRecorder) Fail()                        { r.failed = true }
func (r *accRecorder) FailNow()                     { r.failed = true; runtime.Goexit() }
func (r *accRecorder) Failed() bool                 { return r.failed }
func (r *accRecorder) Fatal(args ...any)            { r.note(fmt.Sprint(args...)); runtime.Goexit() }
func (r *accRecorder) Fatalf(f string, args ...any) {
	r.note(fmt.Sprintf(f, args...))
	runtime.Goexit()
}
func (r *accRecorder) Helper()                     {}
func (r *accRecorder) Log(args ...any)             {}
func (r *accRecorder) Logf(f string, args ...any)  {}
func (r *accRecorder) Name() string                { return r.name }
func (r *accRecorder) Parallel()                   {}
func (r *accRecorder) Skip(args ...any)            { r.skipped = true; runtime.Goexit() }
func (r *accRecorder) SkipNow()                    { r.skipped = true; runtime.Goexit() }
func (r *accRecorder) Skipf(f string, args ...any) { r.skipped = true; runtime.Goexit() }
func (r *accRecorder) Skipped() bool               { return r.skipped }
func (r *accRecorder) joined() string              { return strings.Join(r.msgs, "\n") }

// run исполняет f в своей горутине: Fatal подставного исполнителя обрывает её, а не
// пробу.
func (r *accRecorder) run(f func(tfinterface.T)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
}

// recorderFataler — подставной исполнитель для путей пометки, которым нужно accFataler.
type recorderFataler struct {
	name string
	msgs []string
}

func (f *recorderFataler) Helper()      {}
func (f *recorderFataler) Name() string { return f.name }
func (f *recorderFataler) Fatalf(format string, args ...any) {
	f.msgs = append(f.msgs, fmt.Sprintf(format, args...))
}

// accInjectionCase — минимальный цикл: создать сеть и сверить её имя с ожиданием.
func accInjectionCase(t *testing.T, e *fakeEdge, wantName string) resource.TestCase {
	return resource.TestCase{
		ProtoV6ProviderFactories: accProviderFactories(t),
		Steps: []resource.TestStep{{
			Config: accNetworkConfig(e, "acc-outcome", "инъекция", `["10.20.0.0/16"]`, `[]`),
			Check:  resource.TestCheckResourceAttr("kacho_vpc_network.t", "name", wantName),
		}},
	}
}

// Провайдер НЕ ПОДНЯЛСЯ: каталог сокетов длиннее предела ядра — ровно тот вход, что
// ронял группу на машинах с длинным TMPDIR. Исход — «не выполнилось», не красное.
func TestAcceptanceOutcome_ProviderThatDidNotStartIsNotExecuted(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	tc := accInjectionCase(t, e, "acc-outcome")

	long := filepath.Join(t.TempDir(), strings.Repeat("d", accSocketPathLimit()))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatalf("предпосылка не создана: длинный каталог сокетов не заведён: %v", err)
	}
	t.Setenv(accPluginSocketEnv, long)

	rec := &accRecorder{name: t.Name() + "/инъекция-не-поднялся"}
	rec.run(func(tt tfinterface.T) { accRunUnitTest(accT{T: tt, name: rec.name}, tc) })

	got := rec.joined()
	t.Logf("распознана подпись %q", accStartupSignatureIn(got))
	if !rec.failed || !strings.Contains(got, accNotExecutedLabel) {
		t.Fatalf("провайдер без слушателя не дал «%s»:\n%s", accNotExecutedLabel, got)
	}
	if strings.Contains(got, "КРАСНОЕ") {
		t.Fatalf("отказ запуска провайдера выдан за красное:\n%s", got)
	}
}

// Провайдер ПОДНЯЛСЯ и ответил не то: заведомо неверное ожидание имени. Исход —
// «красное», и за «не выполнилось» он не выдаётся.
func TestAcceptanceOutcome_RaisedProviderWithAWrongAnswerIsRed(t *testing.T) {
	e := newFakeEdge(t, edgeKindNetwork())
	tc := accInjectionCase(t, e, "заведомо-не-то-имя")

	rec := &accRecorder{name: t.Name() + "/инъекция-неверный-ответ"}
	rec.run(func(tt tfinterface.T) { accRunUnitTest(accT{T: tt, name: rec.name}, tc) })

	got := rec.joined()
	if !rec.failed || !strings.Contains(got, "КРАСНОЕ") {
		t.Fatalf("неверный ответ поднятого провайдера не дал красного:\n%s", got)
	}
	if strings.Contains(got, accNotExecutedLabel) {
		t.Fatalf("красное выдано за «%s»:\n%s", accNotExecutedLabel, got)
	}
	// Законный близнец: тот же цикл с верным ожиданием проходит — иначе красное
	// выше пришло бы не от ожидания, а от поломки оснастки.
	// Край — свой: состояние края после первого цикла не должно отвечать за близнеца.
	twinEdge := newFakeEdge(t, edgeKindNetwork())
	twin := &accRecorder{name: t.Name() + "/близнец"}
	twin.run(func(tt tfinterface.T) {
		accRunUnitTest(accT{T: tt, name: twin.name}, accInjectionCase(t, twinEdge, "acc-outcome"))
	})
	if twin.failed {
		t.Fatalf("законный близнец с верным ожиданием отказал:\n%s", twin.joined())
	}
}

// Двоичный файл провайдера, НЕ ГОВОРЯЩИЙ протоколом go-plugin, перед настоящим
// исполнителем цикла: его текст отказа распознаётся как запуск, а не поведение.
func TestAcceptanceOutcome_BinaryWithoutHandshakeIsNotExecuted(t *testing.T) {
	accRequireCLI(t)
	plugins := t.TempDir()
	fake := filepath.Join(plugins, "terraform-provider-kacho")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("предпосылка не создана: подставной двоичный файл не записан: %v", err)
	}
	dir := t.TempDir()
	rc := fmt.Sprintf("provider_installation {\n  dev_overrides { %q = %q }\n  direct {}\n}\n", providerSourceAddress, plugins)
	if err := os.WriteFile(filepath.Join(dir, "tofu.tfrc"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newFakeEdge(t, edgeKindNetwork())
	// Настройка прямого цикла: провайдер приезжает подменой адреса источника (как в
	// пробах переезда типов), без реестра и без `init`.
	accWriteConfig(t, dir, fmt.Sprintf(`terraform {
  required_providers {
    kacho = {
      source = %q
    }
  }
}

provider "kacho" {
  endpoint = %q
  token    = "acceptance-token"
}

resource "kacho_vpc_network" "t" {
  project_id = %q
  name       = "acc-outcome"
}
`, providerSourceAddress, e.URL(), accNetworkProject))

	out, code := accTofuRun(t, dir, "plan", "-no-color")
	if code == 0 {
		t.Fatalf("план с провайдером без рукопожатия прошёл — предпосылка не создана:\n%s", out)
	}
	if !accIsStartupFailure(out) {
		t.Fatalf("отказ исполнителя на провайдере без рукопожатия не распознан как отказ запуска — "+
			"проба выдала бы его за красное:\n%s", out)
	}
	t.Logf("исполнитель отказал кодом %d, распознана подпись %q", code, accStartupSignatureIn(out))
	f := &recorderFataler{name: t.Name() + "/инъекция-рукопожатие"}
	accStartupFailed(f, "исполнитель цикла не поднял провайдера", out)
	if len(f.msgs) != 1 || !strings.HasPrefix(f.msgs[0], accNotExecutedLabel) {
		t.Fatalf("путь пометки не дал «%s»: %q", accNotExecutedLabel, f.msgs)
	}
}

// Несобираемый провайдер: сборка из пакета с ошибкой компиляции — «не выполнилось».
func TestAcceptanceOutcome_UnbuildableProviderIsNotExecuted(t *testing.T) {
	accTrack(t)
	pkg := filepath.Join(t.TempDir(), "broken")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "main.go"), []byte("package main\n\nfunc main() { несобираемо }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Отдельный модуль: сборка не должна зависеть от того, что лежит рядом.
	if err := os.WriteFile(filepath.Join(pkg, "go.mod"), []byte("module broken\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// GOWORK=off: синтетический модуль не должен подхватить рабочее пространство
	// выше по дереву каталогов.
	buildErr := accBuildProviderAt(t.TempDir(), pkg, ".", []string{"GOWORK=off"})
	if buildErr == nil {
		t.Fatal("предпосылка не создана: несобираемый пакет собрался")
	}
	if !strings.Contains(buildErr.Error(), "несобираемо") {
		t.Fatalf("сборка отказала не компиляцией, а чем-то другим — инъекция не о том:\n%v", buildErr)
	}
	f := &recorderFataler{name: t.Name() + "/инъекция-сборка"}
	accPluginBuilt(f, buildErr)
	if len(f.msgs) != 1 || !strings.HasPrefix(f.msgs[0], accNotExecutedLabel) {
		t.Fatalf("несобираемый провайдер не дал «%s»: %q", accNotExecutedLabel, f.msgs)
	}
	// Законный близнец: собранный провайдер пометки не получает.
	ok := &recorderFataler{name: t.Name() + "/близнец"}
	accPluginBuilt(ok, nil)
	if len(ok.msgs) != 0 {
		t.Fatalf("собранный провайдер помечен: %q", ok.msgs)
	}
}

// Выбор каталога сокетов: заданный оператором — как есть либо отказ; иначе временный
// каталог, иначе короткий общесистемный; не поместился никто — отказ с причиной.
func TestAcceptanceOutcome_SocketDirIsChosenToFit(t *testing.T) {
	accTrack(t)
	short := "/s"
	long := "/" + strings.Repeat("x", accSocketPathLimit())
	var made []string
	mkdir := func(base string) (string, error) {
		d := base + "/kp-1"
		made = append(made, d)
		return d, nil
	}
	for _, tc := range []struct {
		name, explicit, temp string
		wantSet, wantErr     bool
	}{
		{"короткий временный — умолчание", "", short, false, false},
		{"длинный временный — короткий общесистемный", "", long, true, false},
		{"оператор задал короткий", "/op", long, true, false},
		{"оператор задал длинный — отказ, а не подмена", long, short, false, true},
	} {
		set, _, err := accSocketDirFor(tc.explicit, tc.temp, mkdir)
		if (err != nil) != tc.wantErr || (set != "") != tc.wantSet {
			t.Errorf("%s: set=%q err=%v", tc.name, set, err)
		}
	}
	if len(made) != 1 {
		t.Errorf("короткий каталог заведён %d раз, ждали один (только при длинном временном)", len(made))
	}
	// Граница предела: ровно на пределе помещается, на знак больше — нет.
	edge := "/" + strings.Repeat("e", accSocketPathLimit()-accSocketSuffixLen-1)
	if !accSocketFits(edge) || accSocketFits(edge+"e") {
		t.Errorf("граница предела неверна: %d знаков помещается=%v, %d — %v",
			len(edge), accSocketFits(edge), len(edge)+1, accSocketFits(edge+"e"))
	}
}

// Код выхода 3 — только когда все отказы «не выполнилось» и каждая выбранная проба
// учтена.
func TestAcceptanceOutcome_ExitCodeThreeOnlyWhenEveryFailureIsNotExecuted(t *testing.T) {
	accTrack(t)
	accOutcomes.Lock()
	saved := accOutcomes.category
	accOutcomes.category = map[string]accCategory{"a": accPassed, "b": accNotExecuted}
	accOutcomes.Unlock()
	defer func() {
		accOutcomes.Lock()
		accOutcomes.category = saved
		accOutcomes.Unlock()
	}()

	if got := accExitCode(1, true); got != 3 {
		t.Errorf("все отказы «не выполнилось», выбранные учтены: код %d, ждали 3", got)
	}
	if got := accExitCode(1, false); got != 1 {
		t.Errorf("выбрана неучтённая проба: код %d, ждали 1 — неучтённая красная спряталась бы", got)
	}
	accOutcomes.Lock()
	accOutcomes.category["c"] = accRed
	accOutcomes.Unlock()
	if got := accExitCode(1, true); got != 1 {
		t.Errorf("есть красное: код %d, ждали 1", got)
	}
	if got := accExitCode(0, true); got != 0 {
		t.Errorf("зелёный прогон: код %d, ждали 0", got)
	}
}
