// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
)

// fullRecord — запись перечня, у которой есть каждое поле. Отрицания снимают
// или портят ровно одно.
func fullRecord() map[string]any {
	return map[string]any{
		"module":         "probe",
		"feedAddr":       "notify-probe:9091",
		"san":            "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify-probe",
		"classes":        []string{"notice"},
		"recipientForms": []string{"address"},
		"authorization":  "resolveSend",
	}
}

func roster(t *testing.T, recs ...map[string]any) *string {
	t.Helper()
	b, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("сборка перечня пробы: %v", err)
	}
	s := string(b)
	return &s
}

// NTF1-G01 — перечень источников: пустой или неполный — отказ старта с именем
// ручки и, для неполной записи, с её модулем и недостающим полем; полный — старт.
func TestNTF1G01SourceRosterEmptyOrIncompleteRefusesStart(t *testing.T) {
	t.Run("перечень не задан", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": nil})
		requireOnlyRefusal(t, start(t), "notify.sources", "не задана")
	})
	t.Run("перечень пуст", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": str("[]")})
		requireOnlyRefusal(t, start(t), "notify.sources", "пуст")
	})
	t.Run("перечень null", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": str("null")})
		requireOnlyRefusal(t, start(t), "notify.sources", "пуст")
	})
	t.Run("перечень не разбирается", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": str("[{")})
		requireOnlyRefusal(t, start(t), "notify.sources")
	})

	fields := []string{"module", "feedAddr", "san", "classes", "recipientForms", "authorization"}
	if got := config.SourceRecordFields(); !reflect.DeepEqual(got, fields) {
		t.Fatalf("перечень полей записи %v, проба знает %v — проба обязана перебирать каждое поле", got, fields)
	}
	for _, field := range fields {
		t.Run("нет поля "+field, func(t *testing.T) {
			rec := fullRecord()
			delete(rec, field)
			useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, rec)})
			want := []string{`нет поля "` + field + `"`}
			if field != "module" {
				want = append(want, `модуль "probe"`)
			} else {
				want = append(want, "запись #1")
			}
			requireOnlyRefusal(t, start(t), "notify.sources", want...)
		})
	}

	t.Run("близнец: полный перечень", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, fullRecord())})
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("загрузка: %v", err)
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("полный перечень отвергнут: %v", err)
		}
		got := cfg.SourceRoster()
		want := []config.Source{{
			Module:         "probe",
			FeedAddr:       "notify-probe:9091",
			SAN:            "spiffe://kacho.cloud/ns/kacho/sa/kacho-notify-probe",
			Classes:        []feed.Class{feed.ClassNotice},
			RecipientForms: []config.RecipientForm{config.RecipientAddress},
			Authorization:  config.AuthorizationResolveSend,
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("разобранный перечень %+v, ожидался %+v", got, want)
		}
	})
	t.Run("близнец: формы адресата пусты, но поле есть", func(t *testing.T) {
		rec := fullRecord()
		rec["recipientForms"] = []string{}
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, rec)})
		if err := start(t); err != nil {
			t.Fatalf("запись без форм адресата отвергнута: %v", err)
		}
	})
}

// Значения записи — из закрытых перечней; иное — отказ с модулем и полем.
func TestSourceRosterValuesAreClosed(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
	}{
		{"модуль пуст", "module", ""},
		{"модуль не DNS-метка", "module", "Probe_1"},
		{"адрес ленты без порта", "feedAddr", "notify-probe"},
		{"адрес ленты с портом 0", "feedAddr", "notify-probe:0"},
		{"адрес ленты без узла", "feedAddr", ":9091"},
		{"SAN не spiffe", "san", "https://kacho.cloud/ns/kacho/sa/x"},
		{"SAN без пути", "san", "spiffe://kacho.cloud"},
		{"SAN с запросом", "san", "spiffe://kacho.cloud/ns/kacho/sa/x?y=1"},
		{"класс вне перечня", "classes", []string{"marketing"}},
		{"классов нет", "classes", []string{}},
		{"класс дважды", "classes", []string{"notice", "notice"}},
		{"форма адресата вне перечня", "recipientForms", []string{"subject"}},
		{"форма адресата дважды", "recipientForms", []string{"address", "address"}},
		{"authorization вне перечня", "authorization", "anonymous"},
		{"authorization пуст", "authorization", ""},
		{"поле не той формы", "classes", "notice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := fullRecord()
			rec[c.field] = c.value
			useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, rec)})
			requireOnlyRefusal(t, start(t), "notify.sources", `"`+c.field+`"`)
		})
	}
	t.Run("лишнее поле", func(t *testing.T) {
		rec := fullRecord()
		rec["templates"] = "x"
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, rec)})
		requireOnlyRefusal(t, start(t), "notify.sources", `"templates"`)
	})
	t.Run("модуль дважды", func(t *testing.T) {
		useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, fullRecord(), fullRecord())})
		requireOnlyRefusal(t, start(t), "notify.sources", `"probe"`, "дважды")
	})
	t.Run("перечень классов закрытый — тот же, что у ленты", func(t *testing.T) {
		for _, c := range feed.Classes() {
			rec := fullRecord()
			rec["classes"] = []string{string(c)}
			useFixture(t, map[string]*string{"KACHO_NOTIFY_SOURCES": roster(t, rec)})
			if err := start(t); err != nil {
				t.Fatalf("класс ленты %q отвергнут перечнем notify: %v", c, err)
			}
		}
		if len(feed.Classes()) == 0 {
			t.Fatal("перечень классов ленты пуст — проба не перебрала ничего")
		}
	})
}
