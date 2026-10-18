// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"
	"testing"
)

// kacho#3174 — домен печенья сессии у края — то же значение, что у службы
// (`authn.login.cookie-domain`), тем же словарём: `none` — только на хост,
// иначе имя домена. Умолчания нет: необъявленная и пустая ручка — отказ в
// старте с названием ручки, как у службы.
func TestSessionCookieDomainSpeaksTheServiceVocabulary(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		declared bool
		want     string
		refused  string
	}{
		{"не объявлено", "", false, "", "не объявлено"},
		{"пусто", "  ", true, "", "пусто"},
		{"только на хост", "none", true, "", ""},
		{"только на хост с пробелами", " none ", true, "", ""},
		{"домен", "kacho.example", true, "kacho.example", ""},
		{"домен с пробелами по краям", " kacho.example ", true, "kacho.example", ""},
		{"адрес вместо домена", "https://kacho.example", true, "", "не годится"},
		{"домен с портом", "kacho.example:443", true, "", "не годится"},
		{"пробел внутри", "kacho example", true, "", "не годится"},
	}
	for _, tc := range cases {
		got, err := ParseSessionCookieDomain(tc.raw, tc.declared)
		if tc.refused == "" {
			if err != nil {
				t.Errorf("%s: отказ %v, ожидалось значение %q", tc.name, err, tc.want)
			} else if got != tc.want {
				t.Errorf("%s: значение %q, ожидалось %q", tc.name, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: принято %q, ожидался отказ", tc.name, got)
			continue
		}
		if !strings.Contains(err.Error(), KnobSessionCookieDomain) || !strings.Contains(err.Error(), tc.refused) {
			t.Errorf("%s: отказ не называет ручку и причину %q: %v", tc.name, tc.refused, err)
		}
	}
}

// Загрузчик спрашивает окружение, объявлена ли ручка, и кладёт разобранное.
func TestLoadSessionCookieDomainAsksTheEnvironment(t *testing.T) {
	cfg := Config{SessionCookieDomainRaw: "kacho.example"}
	if err := loadSessionCookieDomain(&cfg, func(k string) (string, bool) { return "kacho.example", k == KnobSessionCookieDomain }); err != nil {
		t.Fatal(err)
	}
	if cfg.SessionCookieDomain != "kacho.example" {
		t.Fatalf("разобранное %q", cfg.SessionCookieDomain)
	}
	if err := loadSessionCookieDomain(&Config{}, func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("необъявленная ручка принята")
	}
}
