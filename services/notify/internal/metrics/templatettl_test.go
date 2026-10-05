// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

// TestTemplateTTL_SeriesPerTemplateInSeconds — ряд notify_template_ttl_seconds
// несёт срок каждого шаблона сборки в секундах с метками source, template,
// class: это вход тревоги «половина наименьшего ttl шаблонов security» (Р18,
// NTF1-G12), которую правило чарта вычисляет по сборке, а не по литералу.
func TestTemplateTTL_SeriesPerTemplateInSeconds(t *testing.T) {
	reg := prometheus.NewRegistry()
	err := RegisterTemplateTTL(reg, []Template{
		{Source: "kaname", Name: "recovery", Class: feed.ClassSecurity, TTL: 5 * time.Minute},
		{Source: "kaname", Name: "invite", Class: feed.ClassNotice, TTL: time.Hour},
	})
	if err != nil {
		t.Fatalf("RegisterTemplateTTL: %v", err)
	}
	want := `
# HELP notify_template_ttl_seconds ` + templateTTLHelp + `
# TYPE notify_template_ttl_seconds gauge
notify_template_ttl_seconds{class="notice",source="kaname",template="invite"} 3600
notify_template_ttl_seconds{class="security",source="kaname",template="recovery"} 300
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), TemplateTTLName); err != nil {
		t.Fatalf("ряд срока шаблонов: %v", err)
	}
}

// TestTemplateTTL_RejectsInputOutsideTheForm — вход вне формы отвергается
// с именем предмета и семейство не регистрируется: ряд с пустой меткой,
// классом вне перечня corelib, неположительным сроком или повтором шаблона
// сделал бы тревогу G12 ложной молча. Близнец — тот же вход по форме.
func TestTemplateTTL_RejectsInputOutsideTheForm(t *testing.T) {
	ok := Template{Source: "kaname", Name: "recovery", Class: feed.ClassSecurity, TTL: 5 * time.Minute}
	cases := []struct {
		name string
		in   []Template
		want string
	}{
		{"пустой перечень", nil, "перечень шаблонов пуст"},
		{"пустой источник", []Template{{Name: "recovery", Class: feed.ClassSecurity, TTL: time.Minute}}, "источник"},
		{"пустое имя", []Template{{Source: "kaname", Class: feed.ClassSecurity, TTL: time.Minute}}, "имя"},
		{"класс вне перечня", []Template{{Source: "kaname", Name: "recovery", Class: "urgent", TTL: time.Minute}}, `"urgent"`},
		{"нулевой срок", []Template{{Source: "kaname", Name: "recovery", Class: feed.ClassSecurity}}, "срок"},
		{"отрицательный срок", []Template{{Source: "kaname", Name: "recovery", Class: feed.ClassSecurity, TTL: -time.Second}}, "срок"},
		{"повтор шаблона", []Template{ok, ok}, "дважды"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			err := RegisterTemplateTTL(reg, c.in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидался отказ со словом %q, получено: %v", c.want, err)
			}
			if n, gerr := testutil.GatherAndCount(reg, TemplateTTLName); gerr != nil || n != 0 {
				t.Fatalf("при отказе семейство зарегистрировано: серий %d (%v)", n, gerr)
			}
		})
	}
	if err := RegisterTemplateTTL(prometheus.NewRegistry(), []Template{ok}); err != nil {
		t.Fatalf("близнец (вход по форме) отвергнут: %v", err)
	}
	if err := RegisterTemplateTTL(nil, []Template{ok}); err == nil {
		t.Fatalf("реестр nil принят")
	}
}
