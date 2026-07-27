// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// trusted_forwarders_wiring_test.go — страж РАЗМЕЩЕНИЯ: список доверенных
// отправителей обязан приходить из конфигурации, а не из литерала в коде.
//
// Поведенческие замки (кто именно принимается, а кто отвергается) живут в
// trusted_forwarders_test.go. Этот файл закрывает другой класс: поведенческий тест
// можно прогнать с любым списком и получить зелёное, а живой процесс при этом будет
// подниматься с зашитым пустым. Разрыв между «проверено» и «развёрнуто» — ровно та
// «форма без содержания», из-за которой дыра и прожила до сих пор: цепочка
// интерсепторов была написана правильно и получала пустой аргумент.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// forwardersAssign — ЛЮБОЕ присваивание переменной, уезжающей в
// WithTrustedForwarders. Ищем все вхождения, а не первое: `forwarders :=
// cfg.TrustedForwarders()` с последующим `forwarders = nil` прошёл бы проверку
// первого совпадения и вернул бы дыру.
var forwardersAssign = regexp.MustCompile(`(?m)^\s*forwarders\s*:?=\s*(.+?)\s*$`)

// chainCall — вызов боевой цепочки со списком отправителей. Пробелы внутри списка
// аргументов не фиксируем: перенос строки при форматировании не должен превращать
// стража в ложное падение.
var chainCall = regexp.MustCompile(`(?s)(unary|stream)Chain\(\s*logger\s*,\s*forwarders\s*,`)

func readServeSrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	return string(b)
}

// TestServe_ForwarderAllowListComesFromConfig — список обязан выводиться из cfg.
//
// RED до правки: `forwarders := []string{}` — литерал, поля в конфиге нет вовсе,
// поэтому ни один способ настройки не может сузить круг отправителей.
func TestServe_ForwarderAllowListComesFromConfig(t *testing.T) {
	src := readServeSrc(t)

	all := forwardersAssign.FindAllStringSubmatch(src, -1)
	if len(all) == 0 {
		t.Fatal("serve.go: не найдено присваивание `forwarders` — " +
			"страж потерял цель, обнови его вместе с проводкой")
	}
	// КАЖДОЕ присваивание обязано приходить из конфигурации: достаточно одного
	// переприсваивания литералом ниже по функции, чтобы круг снова не сужался.
	for _, m := range all {
		rhs := strings.TrimSpace(m[1])
		if !strings.Contains(rhs, "cfg.") {
			t.Fatalf("serve.go: список доверенных отправителей присваивается как `%s` — "+
				"литералом, а не конфигурацией. Пустой литерал означает «принимаем переданную "+
				"личность от ЛЮБОГО пира с сертификатом» (pkg/grpcsrv principalIsTrusted сужает "+
				"круг только на непустом списке) — и настроить это невозможно ни одним способом", rhs)
		}
	}
}

// TestServe_BothListenersGetTheSameAllowList — измерение нельзя закрыть на одном
// листенере: внутренний (:9091) несёт привязку/отвязку тома, публичный (:9090) —
// чтение и изменение. Оба обязаны получать один и тот же список.
func TestServe_BothListenersGetTheSameAllowList(t *testing.T) {
	src := readServeSrc(t)

	var unary, stream int
	for _, m := range chainCall.FindAllStringSubmatch(src, -1) {
		if m[1] == "unary" {
			unary++
		} else {
			stream++
		}
	}
	if unary != 2 {
		t.Fatalf("unaryChain с общим списком отправителей встречается %d раз(а), ожидается 2 "+
			"(публичный :9090 и внутренний :9091)", unary)
	}
	if stream != 2 {
		t.Fatalf("streamChain с общим списком отправителей встречается %d раз(а), ожидается 2", stream)
	}
}
