// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Непрерывный fuzzing — CIDR-арифметика VPC.
//
// CIDR-блоки приходят строками из API (Network, Subnet, AddressPool) и уходят в
// расчёт ёмкости пула и в аллокатор адресов. Цель гоняет именно этот код —
// `domain.UsableIPv4Count` (ёмкость), `domain.PickRandomIPv4` /
// `domain.PickRandomIPv6` (выбор адреса) и `domain.UsableIPv4Sweep`
// (детерминированный перебор, к которому аллокатор откатывается, когда
// случайный выбор не сходится).
//
// Прежняя редакция этой цели фаззила `netip.ParsePrefix` — то есть стандартную
// библиотеку Go. Ни одной строки продукта на её пути не было.
//
// Утверждения — про то, чем аллокатор опасен, а не про отсутствие паники:
//
//   - выданный адрес обязан лежать ВНУТРИ префикса, из которого его просили.
//     Адрес за границей блока уезжает арендой в чужую подсеть, и заметно это
//     станет по маршрутизации, а не по ошибке;
//   - ёмкость неотрицательна и не превышает размер блока — на ней считается
//     утилизация пула, а завышение читается как «свободные адреса ещё есть» на
//     исчерпанном пуле;
//   - перебор выдаёт только адреса внутри блока и не длиннее запрошенного.
//
// Граница контракта: адрес выдаётся ТОЛЬКО из блока с нулевыми host-битами —
// API это требует (`validateAddressPoolCIDRs`, текст «host bits must be zero»),
// поэтому containment проверяется на `Masked()`. Неканоническая форма
// (`10.0.0.5/24`) до аллокатора не доходит и проверяется здесь лишь на то, что
// не роняет процесс.
package fuzz_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

func FuzzCIDRParse(f *testing.F) {
	seeds := []string{
		"10.0.0.0/16",
		"192.168.0.0/24",
		"172.16.0.0/12",
		"2001:db8::/32",
		"fd00::/8",
		"0.0.0.0/0",
		"::/0",
		// Границы, на которых ломается арифметика host-битов.
		"10.0.0.1/32",
		"10.0.0.0/31",
		"10.0.0.0/30",
		"10.0.0.0/1",
		"2001:db8::/127",
		"2001:db8::1/128",
		// Неканоническая форма — API её отвергает, аллокатор её не увидит.
		"10.0.0.5/24",
		// Мусор.
		"",
		"abc",
		"10.0.0.0/33",
		"256.256.256.256/24",
		"10.0.0.0",
		"/16",
		strings.Repeat("a", 1000),
		"10.0.0.0/16\x00",
		"10.0.0.0 /16",
		"::ffff:10.0.0.0/120",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ПАНИКА на CIDR %q: %v", input, r)
			}
		}()

		// Ёмкость считается прямо по строке из API — она обязана быть устойчива
		// к любому входу, включая тот, что не разбирается вовсе.
		if n := domain.UsableIPv4Count(input); n < 0 {
			t.Fatalf("отрицательная ёмкость %d для %q — по этому числу считается "+
				"утилизация пула", n, input)
		}

		prefix, err := netip.ParsePrefix(strings.TrimSpace(input))
		if err != nil {
			return // не CIDR — дальше идти некуда
		}

		if prefix.Addr().Is4() {
			assertIPv4Capacity(t, input, prefix)
			assertIPv4Allocation(t, input, prefix.Masked())
			assertIPv4Sweep(t, input, prefix.Masked())
			return
		}
		assertIPv6Allocation(t, input, prefix)
	})
}

// assertIPv4Capacity — ёмкость не превышает размер блока. По этому числу
// считается «свободно» в пуле: завышение читается как свободные адреса,
// которых нет.
func assertIPv4Capacity(t *testing.T, input string, prefix netip.Prefix) {
	t.Helper()
	n := domain.UsableIPv4Count(prefix.String())
	hostBits := 32 - prefix.Bits()
	if hostBits >= 32 {
		return // /0: код намеренно отвечает 0, размер блока здесь не ограничение
	}
	if maxAddrs := int64(1) << hostBits; n > maxAddrs {
		t.Fatalf("ёмкость %d больше самого блока %s (%d адресов), вход %q",
			n, prefix, maxAddrs, input)
	}
}

// assertIPv4Allocation — выданный адрес лежит внутри блока.
func assertIPv4Allocation(t *testing.T, input string, prefix netip.Prefix) {
	t.Helper()
	got, err := domain.PickRandomIPv4(prefix)
	if err != nil {
		return // не-IPv4 префикс отвергается по контракту (ErrNotIPv4)
	}
	assertInsidePrefix(t, input, prefix, got, "случайный выбор")
}

// assertIPv4Sweep — перебор не выходит ни за границы блока, ни за запрошенный
// предел. Аллокатор откатывается на него, когда случайный выбор не сошёлся.
func assertIPv4Sweep(t *testing.T, input string, prefix netip.Prefix) {
	t.Helper()
	const want = 24
	got := domain.UsableIPv4Sweep(prefix, want)
	if len(got) > want+2 {
		t.Fatalf("перебор вернул %d адресов при запрошенных %d (вход %q) — аллокатор "+
			"держит их в памяти целиком", len(got), want, input)
	}
	for _, s := range got {
		assertInsidePrefix(t, input, prefix, s, "перебор")
	}
}

// assertIPv6Allocation — то же для IPv6-ветви аллокатора.
func assertIPv6Allocation(t *testing.T, input string, prefix netip.Prefix) {
	t.Helper()
	// Неканоническая форма роняет процесс так же охотно, как каноническая, —
	// прогоняем обе, но containment требуем там, где он обещан.
	if _, err := domain.PickRandomIPv6(prefix); err != nil {
		return
	}
	masked := prefix.Masked()
	got, err := domain.PickRandomIPv6(masked)
	if err != nil {
		return
	}
	assertInsidePrefix(t, input, masked, got, "случайный выбор")
}

func assertInsidePrefix(t *testing.T, input string, prefix netip.Prefix, got, who string) {
	t.Helper()
	addr, err := netip.ParseAddr(got)
	if err != nil {
		t.Fatalf("%s вернул неразбираемый адрес %q для %q: %v", who, got, input, err)
	}
	if !prefix.Contains(addr) {
		t.Fatalf("%s вернул адрес %s ВНЕ префикса %s (вход %q) — аренда уходит в чужую "+
			"подсеть, и видно это станет по маршрутизации, а не по ошибке",
			who, addr, prefix, input)
	}
}
