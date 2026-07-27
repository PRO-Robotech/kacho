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

// forwardersAssign — присваивание переменной, уезжающей в WithTrustedForwarders.
var forwardersAssign = regexp.MustCompile(`(?m)^\s*forwarders\s*:?=\s*(.+)$`)

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

	m := forwardersAssign.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("serve.go: не найдено присваивание `forwarders` — " +
			"страж потерял цель, обнови его вместе с проводкой")
	}
	rhs := strings.TrimSpace(m[1])

	if !strings.Contains(rhs, "cfg.") {
		t.Fatalf("serve.go: список доверенных отправителей задан литералом `%s`, "+
			"а не конфигурацией. Пустой литерал означает «принимаем переданную личность "+
			"от ЛЮБОГО пира с сертификатом» (pkg/grpcsrv principalIsTrusted сужает круг "+
			"только на непустом списке) — и настроить это невозможно ни одним способом", rhs)
	}
}

// TestServe_BothListenersGetTheSameAllowList — измерение нельзя закрыть на одном
// листенере: внутренний (:9091) несёт привязку/отвязку тома, публичный (:9090) —
// чтение и изменение. Оба обязаны получать один и тот же список.
func TestServe_BothListenersGetTheSameAllowList(t *testing.T) {
	src := readServeSrc(t)

	if n := strings.Count(src, "unaryChain(logger, forwarders,"); n != 2 {
		t.Fatalf("unaryChain с общим списком отправителей встречается %d раз(а), ожидается 2 "+
			"(публичный :9090 и внутренний :9091)", n)
	}
	if n := strings.Count(src, "streamChain(logger, forwarders,"); n != 2 {
		t.Fatalf("streamChain с общим списком отправителей встречается %d раз(а), ожидается 2", n)
	}
}
