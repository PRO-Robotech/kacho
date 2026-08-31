// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"strings"
	"testing"
)

// TestRequireZotCredentials — в production/production-strict сервис ОБЯЗАН
// предъявляться своему хранилищу слоёв. Пустые учётные данные означают, что
// хранилище обслуживает анонимных (иначе реестр не смог бы в него ходить), то
// есть весь per-request контроль плоскости данных объезжается одним хопом из сети
// подов. Молчаливый старт в такой посадке запрещён — параллель
// requireDataplaneTLSAck / Config.TokenAcceptance.
//
// Полосы: хранилище не сконфигурировано (адрес пуст) — гейт молчит, ходить некуда;
// dev — no-op (in-process фикстуры поднимают zot без аутентификации).
func TestRequireZotCredentials(t *testing.T) {
	const addr = "http://zot.kacho.svc:5000"
	cases := []struct {
		name     string
		authMode string
		zotAddr  string
		user     string
		pass     string
		wantErr  bool
	}{
		{"dev-empty-ok", "dev", addr, "", "", false},
		{"prod-no-zot-configured-ok", "production", "", "", "", false},
		{"prod-both-set-ok", "production", addr, "kacho-registry", "s3cret", false},
		{"prod-user-missing-rejected", "production", addr, "", "s3cret", true},
		{"prod-pass-missing-rejected", "production", addr, "kacho-registry", "", true},
		{"prod-both-missing-rejected", "production", addr, "", "", true},
		{"prod-strict-both-missing-rejected", "production-strict", addr, "", "", true},
		{"prod-whitespace-is-not-a-credential", "production", addr, "  ", "  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requireZotCredentials(posture(t, tc.authMode), tc.zotAddr, tc.user, tc.pass)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want refusal, got nil")
				}
				// Сообщение отказа видит ОПЕРАТОР: без имени ручки стенд не поднять.
				for _, knob := range []string{"KACHO_REGISTRY_ZOT_USERNAME", "KACHO_REGISTRY_ZOT_PASSWORD"} {
					if !strings.Contains(err.Error(), knob) {
						t.Fatalf("refusal does not name %s: %q", knob, err.Error())
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}
