// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// circle_test.go — КРУГ ДОВЕРЕННЫХ ЗВЕНЬЕВ АДРЕСА КЛИЕНТА: что в нём законно
// (kacho#3028).
//
// Одно суждение на двух читателей: край разбирает им ручку на старте, гейт
// рендера — значение, которое ручке выписывает каждая цепочка. Каждое
// отрицательное утверждение — в паре с законным близнецом, отличным в один
// факт.
package proxycircle_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/proxycircle"
)

func TestParse_EmptyIsLawfulAndTrustsNobody(t *testing.T) {
	for _, raw := range []string{"", " ", " , ,"} {
		got, err := proxycircle.Parse(raw)
		if err != nil || len(got) != 0 {
			t.Errorf("Parse(%q) = %v, %v; ожидался пустой круг без отказа — «никому»", raw, got, err)
		}
	}
}

func TestParse_ReadsTheDeclaredNetworks(t *testing.T) {
	got, err := proxycircle.Parse(" 10.244.0.0/16, fd00::/8 ,100.64.0.0/10")
	if err != nil {
		t.Fatalf("законный круг отвергнут: %v", err)
	}
	want := []string{"10.244.0.0/16", "fd00::/8", "100.64.0.0/10"}
	if len(got) != len(want) {
		t.Fatalf("круг %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("сеть %d = %s, ожидалась %s", i, got[i], want[i])
		}
	}
}

func TestParse_RefusesWhatTheCircleMustNotHold(t *testing.T) {
	for _, c := range []struct{ name, raw, mustSay string }{
		{"неразборная запись", "10.244.0.0/16,10.0.0.300/8", "10.0.0.300/8"},
		{"адрес без длины префикса", "10.244.1.17", "10.244.1.17"},
		{"весь адресный простор", "0.0.0.0/0", "0.0.0.0/0"},
		{"весь простор IPv6", "::/0", "::/0"},
		{"частная сеть, расширенная за свою границу", "10.0.0.0/7", "10.0.0.0/7"},
		{"публичная сеть", "198.51.100.0/24", "198.51.100.0/24"},
		{"петля", "127.0.0.0/8", "127.0.0.0/8"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := proxycircle.Parse(c.raw)
			if err == nil {
				t.Fatalf("Parse(%q) принял %v — звено вне сети подов получило бы доверие заголовку", c.raw, got)
			}
			if !strings.Contains(err.Error(), c.mustSay) {
				t.Fatalf("отказ не называет запись %q: %v", c.mustSay, err)
			}
		})
	}
}

// Законный близнец каждого диапазона: ровно граница диапазона и сеть внутри
// него принимаются.
func TestParse_EveryNonPublicRangeAndItsSubnetIsLawful(t *testing.T) {
	for _, r := range proxycircle.NonPublicRanges() {
		if _, err := proxycircle.Parse(r.String()); err != nil {
			t.Errorf("граница диапазона %s отвергнута: %v", r, err)
		}
		sub := r.Bits() + 8
		if r.Addr().Is4() && sub > 32 || sub > 128 {
			continue
		}
		p, _ := r.Addr().Prefix(sub)
		if _, err := proxycircle.Parse(p.String()); err != nil {
			t.Errorf("сеть %s внутри %s отвергнута: %v", p, r, err)
		}
	}
}
