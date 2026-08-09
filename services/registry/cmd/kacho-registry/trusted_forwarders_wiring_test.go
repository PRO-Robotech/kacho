// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// trusted_forwarders_wiring_test.go — страж РАЗМЕЩЕНИЯ: доверенную пару обязаны
// получать ОБА листенера, а список отправителей — приходить из конфигурации.
//
// Поведенческие замки (кого цепочка принимает, а кого отвергает) живут в
// trusted_forwarders_test.go и утверждают на ОДНОЙ цепочке. Что эту цепочку
// получает каждый из двух листенеров, а безусловный извлекатель заголовков не
// остался нигде — предмет этого файла. Разрыв между «проверено» и «смонтировано»
// и есть та «форма без содержания», из-за которой дыра прожила до сих пор:
// внутренний листенер был написан правильно, а публичный рядом читал заголовки
// личности без единой проверки.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// unconditionalExtract — безусловный извлекатель заголовков личности. Его godoc
// прямо запрещает монтировать его туда, куда дозванивается неконтролируемый пир;
// у registry дозванивается любой под пространства имён.
var unconditionalExtract = regexp.MustCompile(`grpcsrv\.(Unary|Stream)PrincipalExtract\(`)

// chainCall — вызов боевого сборщика цепочки. Пробелы не фиксируем: перенос
// строки при форматировании не должен превращать стража в ложное падение.
var chainCall = regexp.MustCompile(`(?s)\bidentity(Unary|Stream)\(\s*cfg\s*\)`)

// forwardersArg — то, ЧТО уезжает в общий конструктор пары как круг отправителей.
// Ищем все вхождения, а не первое: одного вызова с литералом достаточно, чтобы
// круг снова не сужался. Аргумент сам содержит скобки
// (`cfg.TrustedForwarders()`), поэтому класс «что угодно, кроме скобки» здесь не
// годится — берём нежадное совпадение до закрывающей скобки строки.
var forwardersArg = regexp.MustCompile(`(?m)grpcsrv\.PrincipalExtract(?:Unary|Stream)\(\s*(.*?)\s*\)\s*$`)

// chainAssign — ЛЮБОЕ присваивание переменной, которая уезжает в листенер.
// Считать одни только вызовы сборщика недостаточно: переприсваивание литералом
// ниже по функции оставило бы вызов на месте, а в листенер уехала бы другая
// цепочка.
var chainAssign = regexp.MustCompile(`(?m)^\s*(publicUnary|publicStream|internalUnary|internalStream)\s*:?=\s*(.+?)\s*$`)

func readServeSrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read composition root: %v", err)
	}
	return string(b)
}

// TestServe_NoUnconditionalPrincipalExtract — дыра в чистом виде: публичный
// листенер монтировал grpcsrv.UnaryPrincipalExtract / StreamPrincipalExtract,
// которые читают x-kacho-principal-* без всякой проверки транспорта.
//
// RED до правки: оба вхождения на месте.
func TestServe_NoUnconditionalPrincipalExtract(t *testing.T) {
	if hits := unconditionalExtract.FindAllString(readServeSrc(t), -1); len(hits) > 0 {
		t.Fatalf("serve.go всё ещё монтирует безусловный извлекатель личности: %v. "+
			"Он читает x-kacho-principal-* не глядя ни на транспорт, ни на личность сертификата "+
			"пира — любой под кластера присылает заголовки жертвы, и решение о правах принимается "+
			"от её имени. Нужна доверенная пара CertIdentityExtract → "+
			"TrustedPrincipalExtract(WithTrustedForwarders(...))", hits)
	}
}

// TestServe_BothListenersGetTheTrustAwareChain — измерение нельзя закрыть на
// одном листенере: публичный (:9090) несёт пользовательский CRUD реестров и
// репозиториев, внутренний (:9091) — административные RPC. Оба обязаны собирать
// цепочку одним и тем же боевым сборщиком.
func TestServe_BothListenersGetTheTrustAwareChain(t *testing.T) {
	src := readServeSrc(t)

	var unary, stream int
	for _, m := range chainCall.FindAllStringSubmatch(src, -1) {
		if m[1] == "Unary" {
			unary++
		} else {
			stream++
		}
	}
	if unary != 2 {
		t.Fatalf("identityUnary(cfg) встречается %d раз(а), ожидается 2 "+
			"(публичный :9090 и внутренний :9091)", unary)
	}
	if stream != 2 {
		t.Fatalf("identityStream(cfg) встречается %d раз(а), ожидается 2", stream)
	}
}

// TestServe_ForwarderAllowListComesFromConfig — список обязан выводиться из
// конфигурации. Пустой литерал означает «принимаем переданную личность от ЛЮБОГО
// пира с сертификатом» (corelib сужает круг только на непустом списке) — и
// настроить это было бы невозможно ни одним способом.
func TestServe_ForwarderAllowListComesFromConfig(t *testing.T) {
	src := readServeSrc(t)

	all := forwardersArg.FindAllStringSubmatch(src, -1)
	if len(all) == 0 {
		t.Fatal("serve.go: пара извлечения личности не собирается общим конструктором " +
			"grpcsrv.PrincipalExtract* — круг отправителей чужой личности ничем не сужается " +
			"(либо страж перестал читать проводку и его надо обновить вместе с ней)")
	}
	for _, m := range all {
		arg := strings.TrimSpace(m[1])
		if !strings.Contains(arg, "cfg.") {
			t.Fatalf("serve.go: allow-list передаётся как `%s` — литералом, а не конфигурацией", arg)
		}
	}
}

// TestServe_ChainVariablesAreNeverReassignedToALiteral — КАЖДОЕ присваивание
// переменной листенера обязано быть либо вызовом боевого сборщика, либо
// дополнением её же (append — так в цепочку добавляется authz-звено). Иначе
// переприсваивание литералом ниже по функции вернуло бы дыру, оставив все
// остальные проверки зелёными: вызовы сборщика на месте, а в листенер уехало
// другое.
func TestServe_ChainVariablesAreNeverReassignedToALiteral(t *testing.T) {
	src := readServeSrc(t)

	all := chainAssign.FindAllStringSubmatch(src, -1)
	if len(all) == 0 {
		t.Fatal("serve.go: не найдено ни одного присваивания цепочек листенеров — " +
			"страж потерял цель, обнови его вместе с проводкой")
	}
	for _, m := range all {
		name, rhs := m[1], strings.TrimSpace(m[2])
		switch {
		case strings.HasPrefix(rhs, "identityUnary(cfg)"), strings.HasPrefix(rhs, "identityStream(cfg)"):
		case strings.HasPrefix(rhs, "append("+name+","):
		default:
			t.Fatalf("serve.go: %s присваивается как `%s` — это не боевой сборщик цепочки и не "+
				"дополнение её же. Переданная личность принимается тем, что реально попало в "+
				"листенер, а не тем, что рядом вызвано", name, rhs)
		}
	}
}
