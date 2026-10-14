// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// verifier_capacity_floor_injection_test.go — инъекции пробы пола ёмкости
// проверяющего (kacho#3022): каждая подаёт НАСТОЯЩИЙ вход из дерева или пина,
// изменённый ровно в одном факте, и сверяет, что вердикт изменился; рядом —
// законный близнец, на котором он не меняется.
package deploy_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pinFloorSource — исходник правила пола у пина, как он лежит в кэше модулей.
func pinFloorSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(kanameModuleDir(t, ".."), filepath.FromSlash(floorRuleFile))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(raw)
}

func floorOfSource(t *testing.T, src string) (capacityFloorRule, error) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "client_secrets.go", src, 0)
	if err != nil {
		t.Fatalf("синтетический исходник не разбирается: %v", err)
	}
	return readCapacityFloorRule(fset, file)
}

// replaceOnce — ровно одна замена: инъекция, не нашедшая образца, проверяла бы
// неизменённый вход и была бы зелёной впустую.
func replaceOnce(t *testing.T, src, old, repl string) string {
	t.Helper()
	if n := strings.Count(src, old); n != 1 {
		t.Fatalf("образец инъекции %q встречается в исходнике пина %d раз, а не один — инъекция беспредметна", old, n)
	}
	return strings.Replace(src, old, repl, 1)
}

// Правило пола в пине сменилось (доля — треть, а не половина): вердикт по
// НАСТОЯЩИМ стендам меняется без правки пробы — стенды с ёмкостью 2 становятся
// находкой, и пол в тексте находки — новый.
func TestVerifierCapacityFloorInjection_PinRuleChangeMovesTheVerdict(t *testing.T) {
	src := pinFloorSource(t)
	pinned, err := floorOfSource(t, src)
	if err != nil {
		t.Fatalf("пин не прочитан: %v", err)
	}
	mutated, err := floorOfSource(t, replaceOnce(t, src, "checker.Capacity()/2 < 1", "checker.Capacity()/3 < 1"))
	if err != nil {
		t.Fatalf("изменённое правило не прочитано: %v", err)
	}
	if mutated.Floor != 3 || pinned.Floor == mutated.Floor {
		t.Fatalf("пол изменённого правила %d (у пина %d) — разбор не следует за правилом", mutated.Floor, pinned.Floor)
	}

	facts := readCapacityFloorFacts(t)
	before, _ := judgeVerifierCapacityFloor(facts, pinned.Floor)
	after, census := judgeVerifierCapacityFloor(facts, mutated.Floor)
	if len(after) <= len(before) {
		t.Fatalf("пол поднят с %d до %d, а находок было %d и стало %d — вердикт не сдвинулся (%s)",
			pinned.Floor, mutated.Floor, len(before), len(after), census)
	}
	joined := strings.Join(after, "\n")
	if !strings.Contains(joined, "НИЖЕ пола 3") {
		t.Fatalf("находки не называют новый пол:\n%s", joined)
	}
}

// Сравнение «не больше» вместо «меньше» — пол на шаг доли выше: разбор считает
// пол, а не узнаёт число.
func TestVerifierCapacityFloorInjection_LessOrEqualRaisesTheFloorByOneShare(t *testing.T) {
	got, err := floorOfSource(t, replaceOnce(t, pinFloorSource(t), "checker.Capacity()/2 < 1", "checker.Capacity()/2 <= 1"))
	if err != nil {
		t.Fatalf("правило не прочитано: %v", err)
	}
	if got.Floor != 4 {
		t.Fatalf("`Capacity()/2 <= 1` пропускает ёмкость от 4, а разбор вывел %d", got.Floor)
	}
}

// Форма правила, которой разбор не знает, и снятое правило — отказ разбора, а
// не «пола нет».
func TestVerifierCapacityFloorInjection_UnreadableRuleIsARefusalNotAZeroFloor(t *testing.T) {
	src := pinFloorSource(t)
	cases := map[string]string{
		"правило снято":         replaceOnce(t, src, "checker.Capacity()/2 < 1", "false"),
		"делитель не литерал":   replaceOnce(t, src, "checker.Capacity()/2 < 1", "checker.Capacity()/shareDivisor < 1"),
		"граница не литерал":    replaceOnce(t, src, "checker.Capacity()/2 < 1", "checker.Capacity()/2 < minShare"),
		"ёмкость в чужой форме": replaceOnce(t, src, "checker.Capacity()/2 < 1", "checker.Capacity()-1 < 1"),
		"два сравнения ёмкости": replaceOnce(t, src, "checker.Capacity()/2 < 1", "checker.Capacity()/2 < 1 || checker.Capacity() < 4"),
	}
	for name, mutated := range cases {
		if rule, err := floorOfSource(t, mutated); err == nil {
			t.Errorf("%s: разбор вывел пол %d вместо отказа", name, rule.Floor)
		}
	}
}

// Законный близнец: стенд без церемонии с ёмкостью ниже пола — не находка;
// тот же стенд с включённой церемонией — находка. Различие ровно в одном факте.
func TestVerifierCapacityFloorInjection_CeremonyOffIsTheLawfulTwin(t *testing.T) {
	off := capacityFloorFacts{Stack: "twin", Ceremony: false, Declared: true, Capacity: 1, Coordinate: "x.yaml:1"}
	on := off
	on.Ceremony = true
	if f, c := judgeVerifierCapacityFloor([]capacityFloorFacts{off}, 2); len(f) != 0 || c.Judged != 0 {
		t.Fatalf("стенд без церемонии судим (%s): %v", c, f)
	}
	f, _ := judgeVerifierCapacityFloor([]capacityFloorFacts{on}, 2)
	if len(f) != 1 || !strings.Contains(f[0], "x.yaml:1") || !strings.Contains(f[0], "пола 2") {
		t.Fatalf("стенд с церемонией и ёмкостью 1 — ожидалась одна находка с координатой и полом, а не %v", f)
	}
	undeclared := on
	undeclared.Declared, undeclared.Capacity = false, 0
	if f, _ := judgeVerifierCapacityFloor([]capacityFloorFacts{undeclared}, 2); len(f) != 1 {
		t.Fatalf("стенд с церемонией без объявленной ёмкости — ожидалась находка, а не %v", f)
	}
}

// Предпосылки сверяются с телом у пина: предикат церемонии, сменивший смысл,
// и сборка, переставшая звать правило, — отказ модели.
func TestVerifierCapacityFloorInjection_PremisesFollowThePin(t *testing.T) {
	moduleDir := kanameModuleDir(t, "..")
	read := func(rel string) string {
		raw, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		return string(raw)
	}
	parse := func(src string) error {
		file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		return ceremonyPredicateIsTheEnabledKnob(file)
	}
	pred := read(ceremonyPredicateFile)
	if err := parse(pred); err != nil {
		t.Fatalf("предпосылка у настоящего пина не сошлась: %v", err)
	}
	if err := parse(replaceOnce(t, pred, "return a.ClientToken.Enabled\n", "return a.ClientToken.Enabled && a.Login.Own\n")); err == nil {
		t.Error("предикат церемонии сменил смысл, а модель пробы его приняла")
	}

	assembly := read(ceremonyAssemblyFile)
	asm := func(src string) error {
		file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		return ceremonyAssemblyCallsTheFloor(file)
	}
	if err := asm(assembly); err != nil {
		t.Fatalf("предпосылка сборки у настоящего пина не сошлась: %v", err)
	}
	if err := asm(replaceOnce(t, assembly, "ceremonyport.NewClientSecrets(", "ceremonyport.NewClientSecretsUnbounded(")); err == nil {
		t.Error("сборка церемонии перестала звать правило пола, а модель пробы это приняла")
	}
}

// Пустой обход: ноль стендов либо ноль судимых — перепись это различает, и
// проба по дереву обязана краснеть на нуле судимых.
func TestVerifierCapacityFloorInjection_EmptyWalkIsVisibleInTheCensus(t *testing.T) {
	if _, c := judgeVerifierCapacityFloor(nil, 2); c.Stacks != 0 || c.Judged != 0 {
		t.Fatalf("пустой вход дал перепись %s", c)
	}
	off := []capacityFloorFacts{{Stack: "a", Declared: true, Capacity: 8}}
	if f, c := judgeVerifierCapacityFloor(off, 2); len(f) != 0 || c.Stacks != 1 || c.Judged != 0 {
		t.Fatalf("стенд без церемонии: находки %v, перепись %s — «судимо 0» обязано быть видно", f, c)
	}
}
