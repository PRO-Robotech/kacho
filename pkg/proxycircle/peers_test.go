// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// peers_test.go — ЗВЕНЬЯ ФРОНТА ПОИМЁННО: что законно в перечне имён служб,
// чьи поды край признаёт доверенными звеньями (kacho#3028, круг 3).
//
// Сеть круга выделяет «под кластера», а не «раздачу консоли»: звено сужается
// до адресов подов, которые выбирает безголовая служба фронта. Имя — то, что
// край разрешает в адреса; адрес вместо имени сужение обходит (он «разрешается»
// сам в себя, и звеном стал бы кто угодно по этому адресу). Каждый отказ — в
// паре с законным близнецом, отличным в один факт.
package proxycircle_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/proxycircle"
)

func TestParsePeers_EmptyIsLawfulAndNamesNobody(t *testing.T) {
	for _, raw := range []string{"", " ", " , ,"} {
		got, err := proxycircle.ParsePeers(raw)
		if err != nil || len(got) != 0 {
			t.Errorf("ParsePeers(%q) = %v, %v; ожидался пустой перечень без отказа", raw, got, err)
		}
	}
}

func TestParsePeers_ReadsTheDeclaredNames(t *testing.T) {
	got, err := proxycircle.ParsePeers(" api-gateway-front-console , api-gateway-front-ingress.kacho.svc,api-gateway-front-console")
	if err != nil {
		t.Fatalf("законный перечень отвергнут: %v", err)
	}
	want := []string{"api-gateway-front-console", "api-gateway-front-ingress.kacho.svc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("перечень %v, ожидался %v (порядок объявления, повтор — один раз)", got, want)
	}
}

func TestParsePeers_RefusesWhatIsNotAServiceName(t *testing.T) {
	for _, c := range []struct{ raw, mustSay string }{
		{"api-gateway-front-console,10.244.1.17", "10.244.1.17"},
		{"api-gateway-front-console,fd00::17", "fd00::17"},
		{"api-gateway-front-console,Front_Console", "Front_Console"},
		{"api-gateway-front-console,-front", "-front"},
		{"api-gateway-front-console,front..console", "front..console"},
		{"api-gateway-front-console," + strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{"api-gateway-front-console,front.console.", "front.console."},
	} {
		got, err := proxycircle.ParsePeers(c.raw)
		if err == nil {
			t.Errorf("ParsePeers(%q) = %v без отказа; ожидался отказ, называющий %q", c.raw, got, c.mustSay)
			continue
		}
		if !strings.Contains(err.Error(), c.mustSay) {
			t.Errorf("отказ на %q не называет запись %q: %v", c.raw, c.mustSay, err)
		}
	}
}
