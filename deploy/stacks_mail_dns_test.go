// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// stacks_mail_dns_test.go — таблица записей SPF и DMARC установок
// (deploy/stacks-mail-dns.txt, kacho#3017) покрывает ровно профили с внешним
// ретранслятором.
//
//	(а) у каждого профиля с признаком `relay` (deploy/stacks-mail.txt) —
//	    ровно одна строка spf и ровно одна dmarc: страж notify читает обе, и
//	    профиль без объявленной записи поднял бы notify, который откажет
//	    стартом;
//	(б) строки профиля, у которого признак не `relay`, — находка: стенду
//	    отвечает зона стенда (посев), у `operator` записи объявляет оператор,
//	    и вторая запись об одном домене разошлась бы с первой.
//
// Форму самих записей судят таблицы стража DNS notify
// (services/notify/internal/dnscheck/installation_records_test.go), а не
// вторая реализация здесь.

import (
	"os"
	"sort"
	"strings"
	"testing"
)

const stacksMailDNSTable = "stacks-mail-dns.txt"

// mailDNSRows — счётчик строк по профилю и виду записи.
func readStacksMailDNS(t *testing.T) map[string]map[string]int {
	t.Helper()
	raw, err := os.ReadFile(stacksMailDNSTable)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица записей %s не читается: %v", stacksMailDNSTable, err)
	}
	out := map[string]map[string]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "|", 3)
		if len(f) != 3 || (f[1] != "spf" && f[1] != "dmarc") || strings.TrimSpace(f[2]) == "" {
			t.Fatalf("КРАСНЫЙ: строка %q таблицы %s не разобрана — форма <профиль>|spf|dmarc|<запись>", line, stacksMailDNSTable)
		}
		if out[f[0]] == nil {
			out[f[0]] = map[string]int{}
		}
		out[f[0]][f[1]]++
	}
	return out
}

// judgeStacksMailDNS — чистая функция: её зовут и дерево, и инъекции.
func judgeStacksMailDNS(lanes map[string]mailRow, rows map[string]map[string]int) (lines, findings []string, relays int) {
	var names []string
	for n := range lanes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		kind := lanes[n].kind
		got := rows[n]
		if kind != "relay" {
			if len(got) > 0 {
				findings = append(findings, n+": (б) строки записей при признаке "+kind)
			}
			continue
		}
		relays++
		var bad []string
		for _, k := range []string{"spf", "dmarc"} {
			if got[k] != 1 {
				bad = append(bad, k+" — строк "+itoa(got[k])+", ожидалась одна")
			}
		}
		if len(bad) > 0 {
			findings = append(findings, n+": (а) "+strings.Join(bad, "; "))
			lines = append(lines, n+": relay — КРАСНЫЙ")
			continue
		}
		lines = append(lines, n+": relay — spf и dmarc объявлены")
	}
	for n := range rows {
		if _, ok := lanes[n]; !ok {
			findings = append(findings, n+": строки записей без профиля в "+stacksMailTable)
		}
	}
	sort.Strings(findings)
	return lines, findings, relays
}

func TestStacksMailDNSCoversExactlyTheRelayProfiles(t *testing.T) {
	lines, findings, relays := judgeStacksMailDNS(readStacksMail(t), readStacksMailDNS(t))
	for _, l := range lines {
		t.Log(l)
	}
	if relays == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: профилей с признаком relay в %s ноль — судить нечего", stacksMailTable)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

func TestStacksMailDNSInjections(t *testing.T) {
	lanes := readStacksMail(t)
	tree := readStacksMailDNS(t)
	clone := func() map[string]map[string]int {
		out := map[string]map[string]int{}
		for p, m := range tree {
			out[p] = map[string]int{}
			for k, v := range m {
				out[p][k] = v
			}
		}
		return out
	}
	var relay, stand string
	for n, r := range lanes {
		switch {
		case r.kind == "relay" && relay == "":
			relay = n
		case r.kind == "stand" && stand == "":
			stand = n
		}
	}
	if relay == "" || stand == "" {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: в таблице признаков нет профиля relay или stand — вход инъекции исчез")
	}
	for _, c := range []struct {
		name string
		mut  func(m map[string]map[string]int)
		red  bool
	}{
		{"дерево как есть", func(map[string]map[string]int) {}, false},
		{"у relay нет dmarc", func(m map[string]map[string]int) { delete(m[relay], "dmarc") }, true},
		{"у relay две spf", func(m map[string]map[string]int) { m[relay]["spf"] = 2 }, true},
		{"строки у stand", func(m map[string]map[string]int) { m[stand] = map[string]int{"spf": 1, "dmarc": 1} }, true},
		{"профиль вне таблицы признаков", func(m map[string]map[string]int) { m["nosuch"] = map[string]int{"spf": 1} }, true},
	} {
		m := clone()
		c.mut(m)
		_, findings, _ := judgeStacksMailDNS(lanes, m)
		if got := len(findings) > 0; got != c.red {
			t.Errorf("%s: красный=%v, ожидался %v; находки: %v", c.name, got, c.red, findings)
		}
	}
}
