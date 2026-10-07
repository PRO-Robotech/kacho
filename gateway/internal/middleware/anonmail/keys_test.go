// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"strings"
	"testing"
)

// TestKeysFor_SourceAndSubnetsByFamily — ключи звена (приёмка NTF-2, Р5;
// замысел З8): источник — адрес целиком (IPv4 /32, IPv6 /64), подсети — /24
// (IPv4), /56 и /48 (IPv6). Адрес, отображённый в IPv6 (::ffff:a.b.c.d), — это
// IPv4: иначе один клиент получал бы два источника по форме записи.
func TestKeysFor_SourceAndSubnetsByFamily(t *testing.T) {
	cases := []struct {
		addr    string
		source  string
		subnets []string
		lens    []int
	}{
		{"192.0.2.17", "src/v4/192.0.2.17/32", []string{"net/v4/192.0.2.0/24"}, []int{24}},
		{"::ffff:192.0.2.17", "src/v4/192.0.2.17/32", []string{"net/v4/192.0.2.0/24"}, []int{24}},
		{"2001:db8:aa:bb01:1:2:3:4", "src/v6/2001:db8:aa:bb01::/64",
			[]string{"net/v6/2001:db8:aa:bb00::/56", "net/v6/2001:db8:aa::/48"}, []int{56, 48}},
	}
	for _, c := range cases {
		k, err := KeysFor(c.addr)
		if err != nil {
			t.Fatalf("%s: %v", c.addr, err)
		}
		if k.Source != c.source {
			t.Errorf("%s: источник %q, ожидался %q", c.addr, k.Source, c.source)
		}
		if len(k.Subnets) != len(c.subnets) {
			t.Fatalf("%s: подсетей %d, ожидалось %d", c.addr, len(k.Subnets), len(c.subnets))
		}
		for i, sn := range k.Subnets {
			if sn.Key != c.subnets[i] || sn.Len != c.lens[i] {
				t.Errorf("%s: подсеть %d = %+v, ожидалась %s длины %d", c.addr, i, sn, c.subnets[i], c.lens[i])
			}
		}
	}
	// Две машины одной /64 — один источник; соседняя /64 той же /56 — другой
	// источник, та же подсеть /56.
	a, _ := KeysFor("2001:db8:aa:bb01::1")
	b, _ := KeysFor("2001:db8:aa:bb01::2")
	c, _ := KeysFor("2001:db8:aa:bb02::1")
	if a.Source != b.Source {
		t.Errorf("адреса одной /64 дали разные источники: %s и %s", a.Source, b.Source)
	}
	if a.Source == c.Source || a.Subnets[0].Key != c.Subnets[0].Key {
		t.Errorf("соседние /64 одной /56: источники %s/%s, подсети %s/%s", a.Source, c.Source, a.Subnets[0].Key, c.Subnets[0].Key)
	}
}

// TestKeysFor_UnparsableAddressIsRefused — адрес, который не разбирается, ключом
// не становится: свести такие запросы к общему ключу значило бы счесть их одним
// источником по случайности.
func TestKeysFor_UnparsableAddressIsRefused(t *testing.T) {
	for _, addr := range []string{"", "not-an-ip", "192.0.2.1:443", "[2001:db8::1]"} {
		if _, err := KeysFor(addr); err == nil {
			t.Errorf("адрес %q принят ключом", addr)
		}
	}
	if _, err := KeysFor("192.0.2.1"); err != nil {
		t.Errorf("законный близнец отвергнут: %v", err)
	}
}

// TestKeysFor_AllKeysAreDistinctAcrossClasses — ключ источника и ключ подсети
// не совпадают ни при каком адресе: их строки различаются приставкой класса.
func TestKeysFor_AllKeysAreDistinctAcrossClasses(t *testing.T) {
	k, _ := KeysFor("10.0.0.0")
	all := k.All()
	seen := map[string]bool{}
	for _, s := range all {
		if seen[s] {
			t.Fatalf("ключ %q повторён: %v", s, all)
		}
		seen[s] = true
		if !strings.HasPrefix(s, "src/") && !strings.HasPrefix(s, "net/") {
			t.Errorf("ключ %q без приставки класса", s)
		}
	}
}
