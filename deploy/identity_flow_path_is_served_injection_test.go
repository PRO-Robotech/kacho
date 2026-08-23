// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"strings"
	"testing"
)

// TestIdentityFlowPathGate_ProvenByInjection — гейт согласия адресов способен
// упасть, смолчать и отличить «не обслуживается» от «неразрешимо».
//
// Разбор ведётся над СИНТЕТИЧЕСКИМ входом: гейт, доказанный только зелёным
// деревом, доказан не был — он остаётся зелёным и когда перестаёт читать.
// Зовётся ТА ЖЕ функция вердикта, что исполняет гейт, а не её копия.
func TestIdentityFlowPathGate_ProvenByInjection(t *testing.T) {
	// Множество сегментов — как его выводит гейт из регулярки раздачи.
	served := map[string]bool{
		"login": true, "registration": true, "recovery": true, "verification": true,
		"settings": true, "error": true, "consent": true, "logout": true,
	}

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "дефект #1048: лишний сегмент перед потоком — находка",
			raw:  `https://{{ .Values.kacho.subdomains.app }}.{{ .Values.kacho.domain }}/auth/registration`,
			want: "не обслуживается",
		},
		{
			name: "законный близнец: ТОТ ЖЕ адрес в корне — молчит",
			raw:  `https://{{ .Values.kacho.subdomains.app }}.{{ .Values.kacho.domain }}/registration`,
			want: "обслуживается",
		},
		{
			name: "относительный корневой путь (профиль стенда) — молчит",
			raw:  `"/registration"`,
			want: "обслуживается",
		},
		{
			name: "относительный путь с лишним сегментом — находка",
			raw:  `"/auth/login"`,
			want: "не обслуживается",
		},
		{
			name: "шаблон ВНУТРИ власти адреса не мешает прочесть путь — молчит",
			raw:  `http://{{ .Values.host }}:{{ .Values.port }}/recovery`,
			want: "обслуживается",
		},
		{
			// Форма, в которую переедет объявление, когда префикс станет ручкой
			// (ветка issue-904, #931). Отсюда не видно, что положит `$flow`, —
			// значит согласие НЕ УСТАНОВЛЕНО, и молчать здесь нельзя.
			name: "начало пути вычисляет шаблон — неразрешимо, а не «обслуживается»",
			raw:  `{{ $flow }}/registration`,
			want: "неразрешимо",
		},
		{
			name: "шаблон в СЕРЕДИНЕ пути — тоже неразрешимо",
			raw:  `https://app.example.io/{{ .Values.prefix }}/login`,
			want: "неразрешимо",
		},
		{
			name: "поток, которого раздача не знает вовсе — находка",
			raw:  `https://app.example.io/passkeys`,
			want: "не обслуживается",
		},
	}

	var found, silent, unresolved int
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := adjudicateFlowURL(c.raw, served)
			if got != c.want {
				t.Fatalf("вердикт %q, ожидался %q (объявление: %s)", got, c.want, strings.TrimSpace(c.raw))
			}
			switch got {
			case "не обслуживается":
				found++
			case "обслуживается":
				silent++
			case "неразрешимо":
				unresolved++
			}
		})
	}

	t.Logf("перепись инъекции: случаев %d; находок %d; законных близнецов %d; неразрешимых %d",
		len(cases), found, silent, unresolved)
	if found == 0 || silent == 0 || unresolved == 0 {
		t.Fatal("инъекция не покрыла все три исхода — доказательство неполно: " +
			"гейт, у которого не проверена одна из сторон, отличает не то, что заявляет")
	}
}
