// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_mail_domain_alignment_test.go — домен возврата notify выровнен с
// доменом отправителя каждого профиля (kacho#3017; NTF-1 Р19, NTF-4 Р15).
//
// Подпись DKIM notify идёт доменом `From` установки (d= — домен адреса
// `global.kacho.identity.smtp.fromAddress`; приёмка NTF-1 Р19), и
// записи DKIM, SPF и DMARC публикуются у этого домена. Домен возврата
// (`notify.returnDomain`) — адрес, куда приходят отказы доставки и жалобы. Он
// обязан лежать в том же домене (равен ему либо его поддомен):
//
//   - иначе Return-Path не выровнен с `From`, и SPF не даёт DMARC выравнивания
//     (RFC 7489 §3.1.2) — у письма остаётся одна опора вместо двух;
//   - иначе отказы с адресами получателей уходят в чужой домен. Профиль a8f60d
//     наследовал `kacho.cloud` слоя стенда при отправителе `prorobotech.ru`, а
//     `kacho.cloud` не делегирован: зарегистрировавший его получил бы отказы
//     этой установки.
//
// Граница поддомена — по метке («.<домен>»), а не по подстроке: `xprorobotech.ru`
// поддоменом `prorobotech.ru` не является (инъекция ниже).
//
// Профиль без адреса отправителя (`prod`: узел задаёт оператор) судить не с
// чем — это печатается, а не молчит; ноль осуждённых профилей — красный.

import (
	"sort"
	"strings"
	"testing"
)

// mailDomains — домен отправителя и домен возврата слитых значений профиля.
// Пустая строка — значение не объявлено.
func mailDomains(merged map[string]any) (from, ret string) {
	raw, _ := lookup(merged, "global", "kacho", "identity", "smtp", "fromAddress")
	addr := strings.TrimSpace(nstr(raw))
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		from = addr[i+1:]
	}
	rd, _ := lookup(merged, "notify", "returnDomain")
	return normDomain(from), normDomain(nstr(rd))
}

func normDomain(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}

// returnDomainAligned — домен возврата равен домену отправителя либо его
// поддомен по границе метки.
func returnDomainAligned(from, ret string) bool {
	return from != "" && (ret == from || strings.HasSuffix(ret, "."+from))
}

// judgeMailDomainAlignment — чистая функция над слитыми значениями профилей:
// её зовут и дерево, и инъекции. judged — число профилей, у которых были оба
// домена.
func judgeMailDomainAlignment(merged map[string]map[string]any) (lines, findings []string, judged int) {
	var names []string
	for n := range merged {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		from, ret := mailDomains(merged[n])
		switch {
		case from == "":
			lines = append(lines, n+": адреса отправителя нет (узел задаёт оператор) — судить не с чем")
			continue
		case ret == "":
			lines = append(lines, n+": отправитель "+from+", домен возврата не объявлен — судить не с чем")
			continue
		}
		judged++
		if returnDomainAligned(from, ret) {
			lines = append(lines, n+": отправитель "+from+", возврат "+ret+" — выровнен")
			continue
		}
		lines = append(lines, n+": отправитель "+from+", возврат "+ret+" — КРАСНЫЙ")
		findings = append(findings, n+": notify.returnDomain «"+ret+"» не равен домену отправителя «"+
			from+"» и не его поддомен (global.kacho.identity.smtp.fromAddress)")
	}
	return lines, findings, judged
}

func treeMergedValuesByStack(t *testing.T) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for name, chain := range deployStacks(t) {
		merged, _, _ := mergedValuesOfStack(t, chain)
		out[name] = merged
	}
	return out
}

func TestNotifyReturnDomainIsAlignedWithTheSenderDomain(t *testing.T) {
	merged := treeMergedValuesByStack(t)
	lines, findings, judged := judgeMailDomainAlignment(merged)
	for _, l := range lines {
		t.Log(l)
	}
	t.Logf("профилей %d, осуждено %d", len(merged), judged)
	if judged == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ни у одного из %d профилей нет обоих доменов — судить нечего, "+
			"а не «всё выровнено»", len(merged))
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// Инъекции настоящим входом дерева: слитые значения профиля a8f60d, в которых
// меняется ровно домен возврата.
func TestNotifyReturnDomainAlignmentInjections(t *testing.T) {
	base := treeMergedValuesByStack(t)["a8f60d"]
	if base == nil {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: профиля a8f60d нет в deploy/stacks.txt — вход инъекции исчез")
	}
	from, _ := mailDomains(base)
	if from == "" {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: у профиля a8f60d нет адреса отправителя — вход инъекции исчез")
	}
	with := func(ret string) map[string]map[string]any {
		m := mergeValues(map[string]any{}, base)
		m = mergeValues(m, map[string]any{"notify": map[string]any{"returnDomain": ret}})
		return map[string]map[string]any{"a8f60d": m}
	}
	for _, c := range []struct {
		name, ret string
		red       bool
	}{
		{"домен стенда при внешнем отправителе", "kacho.cloud", true},
		{"подстрока без границы метки", "x" + from, true},
		{"родитель домена отправителя", from[strings.IndexByte(from, '.')+1:], true},
		{"сам домен отправителя", from, false},
		{"поддомен отправителя", "bounces." + from, false},
		{"регистр и завершающая точка", strings.ToUpper(from) + ".", false},
	} {
		_, findings, judged := judgeMailDomainAlignment(with(c.ret))
		if judged != 1 {
			t.Fatalf("%s: осуждено %d профилей, ожидался 1", c.name, judged)
		}
		if got := len(findings) > 0; got != c.red {
			t.Errorf("%s (возврат %q): красный=%v, ожидался %v; находки: %v", c.name, c.ret, got, c.red, findings)
		}
	}
}
