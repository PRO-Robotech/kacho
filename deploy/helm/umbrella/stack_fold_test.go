// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stack_fold_test.go — как пробы этого пакета, спрашивающие «что получает
// РЕЛИЗ», складывают цепочку `-f` стенда.
//
// Приехали вместе со своими пробами из gateway/deploy (#2734): пробы судят ярус
// поставщика в значениях зонта и дорогу службы доступа к нему, а не край, и
// живут рядом с тем, что судят. Пакет края оставил свои такие же читатели для
// своих проб — у тестовых пакетов общего кода нет.
//
// ЦЕПОЧКИ ЧИТАЕТ ОДИН ЧИТАТЕЛЬ. Таблицу `deploy/stacks.txt` в этом пакете уже
// разбирает deployedStacks (peer_transport_profiles_test.go); здесь она второго
// разбора не получает — deployableStacks только перекладывает прочитанное в
// карту по имени, в которой её ждут переехавшие пробы.
package umbrella_test

import (
	"sort"
	"strings"
	"testing"
)

// umbrellaValues — один профиль зонта деревом. Каталог чарта — каталог этого
// пакета, поэтому путь профиля относителен ему же.
func umbrellaValues(t *testing.T, profile string) map[string]any {
	t.Helper()
	return loadTree(t, profile)
}

// deployableStacks — цепочки `-f`, которыми helm на самом деле зовётся, по
// имени стенда.
//
// Пустая таблица — отказ, а не «стендов не осталось»: всякая проба ниже иначе
// объявила бы ноль находок, ничего не осмотрев.
func deployableStacks(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, s := range deployedStacks(t) {
		out[s.name] = s.files
	}
	if len(out) == 0 {
		t.Fatalf("%s не объявляет ни одного стенда — пакет не вправе заключить, что "+
			"их не осталось", stacksTable)
	}
	return out
}

// sortedStackNames — имена цепочек таблицы в устойчивом порядке. Обход карты
// давал бы подпробы и находки в порядке, разном от прогона к прогону, и два
// прогона одного дерева нельзя было бы сравнить построчно.
func sortedStackNames(stacks map[string][]string) []string {
	names := make([]string, 0, len(stacks))
	for name := range stacks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// mergeInto overlays src onto dst the way helm merges values files: maps merge
// key by key, anything else replaces wholesale.
//
// A map taken from src is COPIED into dst, never shared: a shared one would let
// the next overlay edit src in place (TestMergeInto_LeavesItsSourceIntact).
func mergeInto(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			cur, _ := dst[k].(map[string]any)
			dst[k] = mergeInto(cur, sub)
			continue
		}
		dst[k] = v
	}
	return dst
}

// TestMergeInto_LeavesItsSourceIntact — наложение не пишет СКВОЗЬ себя в
// дерево-источник. Результат несёт и слой, и накладку (законный близнец
// свойства), а слой, который вызывающий держит у себя, остаётся ровно тем, что
// прочитано из его файла.
//
// Дефект найден и снят у копии края (#2735): общая с источником карта давала
// следующей накладке править источник на месте, и дерево одного профиля
// начинало нести объявления другого. Копия здесь заведена переездом проб
// (#2734) и несёт ту же поправку с той же пробой — иначе переезд перенёс бы
// снятый дефект.
func TestMergeInto_LeavesItsSourceIntact(t *testing.T) {
	base := map[string]any{"kaname": map[string]any{"ports": map[string]any{"loginLane": 9100}}}
	late := map[string]any{"kaname": map[string]any{"ports": map[string]any{"public": 9090}}}

	merged := mergeInto(mergeInto(map[string]any{}, base), late)

	if v, ok := lookup(merged, "kaname", "ports", "public"); !ok || v != any(9090) {
		t.Fatalf("накладка не дошла до результата (kaname.ports.public = %v): %v", v, merged)
	}
	if v, ok := lookup(merged, "kaname", "ports", "loginLane"); !ok || v != any(9100) {
		t.Fatalf("накладка стёрла слой под собой (kaname.ports.loginLane = %v): %v", v, merged)
	}
	if v, leaked := lookup(base, "kaname", "ports", "public"); leaked {
		t.Fatalf("накладка записала свой ключ В ИСТОЧНИК: base несёт kaname.ports.public = %v, "+
			"объявление, которого его файл не делал: %v", v, base)
	}
}

// resolveStack merges a stack's profiles in order and returns the gateway value
// at the given path, or ("", false) when the stack never declares it.
func resolveStack(t *testing.T, stack []string, path ...string) (string, bool) {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range stack {
		merged = mergeInto(merged, umbrellaValues(t, profile))
	}
	var cur any = merged
	for _, key := range append([]string{"api-gateway"}, path...) {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = m[key]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok && strings.TrimSpace(s) != ""
}
