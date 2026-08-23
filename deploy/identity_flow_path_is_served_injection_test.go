// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"sort"
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

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ РАЗРЕШЕНИЯ ВЫЧИСЛЯЕМОГО НАЧАЛА ПУТИ
//
// Разбор ведётся над СИНТЕТИЧЕСКИМИ значениями, а не над деревом: гейт,
// доказанный только зелёным деревом, доказан не был — он остаётся зелёным и
// когда перестаёт читать. Зовётся ТА ЖЕ функция, что исполняет гейт.
// ─────────────────────────────────────────────────────────────────────────────

// identityValuesTree — дерево значений с узлом службы личности.
func identityValuesTree(kv map[string]any) map[string]any {
	return map[string]any{"global": map[string]any{"kacho": map[string]any{"identity": kv}}}
}

// identityTemplateVars — определения переменных, как их несёт шаблон настроек:
// `$flow` складывается из базового адреса и ручки префикса.
func identityTemplateVars(prefixKnob string) map[string]string {
	return map[string]string{
		"id":   ".Values.global.kacho.identity",
		"app":  `($id.appBaseURL | default (printf "https://%s.%s" $id.appSubdomain $id.domain))`,
		"flow": `printf "%s%s" $app ($id.` + prefixKnob + ` | toString)`,
	}
}

// TestIdentityFlowPathResolution_ProvenByInjection — гейт РАЗРЕШАЕТ вычисляемое
// начало пути, краснеет с именем профиля и молчит на законной посадке.
func TestIdentityFlowPathResolution_ProvenByInjection(t *testing.T) {
	served := map[string]bool{
		"login": true, "registration": true, "recovery": true, "verification": true,
		"settings": true, "error": true, "consent": true, "logout": true,
	}

	const chartDefault = "УМОЛЧАНИЕ-ЧАРТА/values.yaml"
	const prodProfile = "ПРОФИЛЬ-БОЯ/values.prod.yaml"
	const standProfile = "ПРОФИЛЬ-СТЕНДА/values.dev.yaml"

	rootDefault := map[string]any{
		"appBaseURL": "", "appSubdomain": "app", "domain": "api.kacho.cloud", "flowPathPrefix": "",
	}
	authDefault := map[string]any{
		"appBaseURL": "", "appSubdomain": "app", "domain": "api.kacho.cloud", "flowPathPrefix": "/auth",
	}

	decl := func(prefixKnob string) flowDecl {
		return flowDecl{file: "_kratos-identity.tpl", raw: "{{ $flow }}/registration", vars: identityTemplateVars(prefixKnob)}
	}

	// sources — умолчание плюс профили поверх него, как их строит гейт.
	sources := func(base map[string]any, profiles map[string]map[string]any) []flowValuesSource {
		out := []flowValuesSource{{name: chartDefault, own: identityValuesTree(base)}}
		names := make([]string, 0, len(profiles))
		for n := range profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, flowValuesSource{
				name: n, own: identityValuesTree(profiles[n]), base: identityValuesTree(base),
			})
		}
		return out
	}

	cases := []struct {
		name        string
		decl        flowDecl
		sources     []flowValuesSource
		wantRed     bool
		mustName    []string // находка обязана НАЗВАТЬ эти источники
		mustNotName []string // и НЕ обвинять эти
		mustSay     string   // подстрока, без которой находка непригодна к починке
	}{
		{
			// Предикат снятия #1074, п.2, красная сторона.
			name:     "дефект: /auth в профиле — находка НАЗЫВАЕТ профиль",
			decl:     decl("flowPathPrefix"),
			sources:  sources(rootDefault, map[string]map[string]any{prodProfile: {"flowPathPrefix": "/auth"}}),
			wantRed:  true,
			mustName: []string{prodProfile},
			// Умолчание здесь верно, и обвинять его нельзя: чинят там, где неверно.
			mustNotName: []string{chartDefault},
			mustSay:     `"auth"`,
		},
		{
			// Минимальная пара к предыдущему: различие ровно в величине ручки.
			name:    "законный близнец: тот же профиль с корнем — молчит",
			decl:    decl("flowPathPrefix"),
			sources: sources(rootDefault, map[string]map[string]any{prodProfile: {"flowPathPrefix": ""}}),
			wantRed: false,
		},
		{
			// Второй близнец — НЕ отрицание красного случая, а другая посадка:
			// свой абсолютный адрес консоли с портом, как у стенда разработки.
			// Здесь `default` не срабатывает, то есть разбор идёт ДРУГОЙ веткой.
			name: "законный близнец: своя посадка стенда (адрес с портом) — молчит",
			decl: decl("flowPathPrefix"),
			sources: sources(rootDefault, map[string]map[string]any{
				standProfile: {"appBaseURL": "http://console.kacho.local:28080", "flowPathPrefix": ""},
			}),
			wantRed: false,
		},
		{
			// Ровно предмет 2 задачи: профиль ручку НЕ переопределяет и наследует
			// умолчание. Молчать здесь значило бы не заметить то, что в бою и действует.
			name: "профиль не переопределяет ручку и наследует /auth — находка называет ОБА",
			decl: decl("flowPathPrefix"),
			sources: sources(authDefault, map[string]map[string]any{
				prodProfile: {"appBaseURL": "https://app.example.io"},
			}),
			wantRed:  true,
			mustName: []string{chartDefault, prodProfile},
			mustSay:  `"auth"`,
		},
		{
			// Имя ручки БЕРЁТСЯ ИЗ ШАБЛОНА, а не зашито рядом с гейтом. Здесь
			// шаблон читает другую ручку: зашитый разбор пошёл бы за пустым
			// `flowPathPrefix` и промолчал, выведенный — находит `/auth`.
			name: "ручка переименована в шаблоне — разбор идёт ЗА ШАБЛОНОМ",
			decl: decl("flowPrefixV2"),
			sources: sources(map[string]any{
				"appBaseURL": "", "appSubdomain": "app", "domain": "api.kacho.cloud",
				"flowPathPrefix": "", "flowPrefixV2": "/auth",
			}, nil),
			wantRed:  true,
			mustName: []string{chartDefault},
			mustSay:  `"auth"`,
		},
		{
			// Fail-closed: ручки нет ни в профиле, ни в умолчании. Молчание здесь
			// было бы дырой ровно там, где живёт дефект.
			name:     "ручки нет ни у кого — НЕ молчание, а находка с именем ручки",
			decl:     decl("flowPathPrefix"),
			sources:  sources(map[string]any{}, nil),
			wantRed:  true,
			mustName: []string{chartDefault},
			mustSay:  "не объявлена",
		},
		{
			// Fail-closed: звено, которого разбор не знает. Непонятое отвергается,
			// а не угадывается — иначе разбор молчит о том, чего не прочёл.
			name: "звено, разбору не известное, — находка, а не молчание",
			decl: flowDecl{
				file: "_kratos-identity.tpl", raw: "{{ $flow }}/login",
				vars: map[string]string{"flow": `include "kacho.identity.flowBase" .`},
			},
			sources:  sources(rootDefault, nil),
			wantRed:  true,
			mustName: []string{chartDefault},
			mustSay:  "не относится ни к одной разбираемой форме",
		},
		{
			// Вторая fail-closed ветка: звено разобрано, а ФУНКЦИЯ трубы — нет.
			// Ветки отказа две, и обе обязаны быть красными: доказанная одна
			// оставляет вторую непроверенной.
			name: "функция трубы, разбору не известная, — тоже находка",
			decl: flowDecl{
				file: "_kratos-identity.tpl", raw: "{{ $flow }}/login",
				vars: map[string]string{
					"id":   ".Values.global.kacho.identity",
					"flow": `$id.flowPathPrefix | trimSuffix "/"`,
				},
			},
			sources:  sources(rootDefault, nil),
			wantRed:  true,
			mustName: []string{chartDefault},
			mustSay:  "разбору не поддаётся",
		},
	}

	var red, silent int
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := adjudicateTemplatedDecl(c.decl, c.sources, served)
			joined := strings.Join(got, "\n")

			if c.wantRed && len(got) == 0 {
				t.Fatalf("находок нет, а ожидалась хотя бы одна — гейт на этом входе МОЛЧИТ")
			}
			if !c.wantRed && len(got) != 0 {
				t.Fatalf("законный близнец обвинён:\n%s", joined)
			}
			for _, n := range c.mustName {
				if !strings.Contains(joined, n) {
					t.Fatalf("находка не назвала источник %q — чинить по ней нельзя:\n%s", n, joined)
				}
			}
			for _, n := range c.mustNotName {
				if strings.Contains(joined, n) {
					t.Fatalf("находка обвинила исправный источник %q:\n%s", n, joined)
				}
			}
			if c.mustSay != "" && !strings.Contains(joined, c.mustSay) {
				t.Fatalf("находка не содержит %q — по ней не видно, что именно неверно:\n%s", c.mustSay, joined)
			}
			if c.wantRed {
				red++
			} else {
				silent++
			}
		})
	}

	t.Logf("перепись инъекции разрешения: случаев %d; красных %d; законных близнецов %d", len(cases), red, silent)
	if red == 0 || silent == 0 {
		t.Fatal("инъекция не покрыла обе стороны — доказательство неполно: гейт, у которого " +
			"проверена одна сторона, отличает не то, что заявляет")
	}
}

// TestIdentityFlowPathKnobDiscovery_ProvenByInjection — источником значений
// становится тот файл, который объявляет ручку, ЧИТАЕМУЮ шаблоном.
//
// Это вторая половина разрешения: ошибись отбор — профиль с `/auth` не попал бы
// в источники вовсе, и гейт промолчал бы, ничего не прочитав.
func TestIdentityFlowPathKnobDiscovery_ProvenByInjection(t *testing.T) {
	knobs := []string{"global.kacho.identity.flowPathPrefix", "global.kacho.identity.appBaseURL"}

	cases := []struct {
		name string
		tree map[string]any
		want bool
	}{
		{"профиль объявляет префикс — источник", identityValuesTree(map[string]any{"flowPathPrefix": "/auth"}), true},
		{"профиль объявляет пустой префикс — тоже источник", identityValuesTree(map[string]any{"flowPathPrefix": ""}), true},
		{"профиль объявляет базовый адрес — источник", identityValuesTree(map[string]any{"appBaseURL": "https://app.example.io"}), true},
		{"профиль трогает соседнюю ручку узла — НЕ источник", identityValuesTree(map[string]any{"hooks": map[string]any{"scheme": "https"}}), false},
		{"профиль о службе личности молчит — НЕ источник", map[string]any{"mtls": map[string]any{"enabled": true}}, false},
	}

	var yes, no int
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := declaresAnyOf(c.tree, knobs); got != c.want {
				t.Fatalf("отбор источника вернул %v, ожидалось %v", got, c.want)
			}
			if c.want {
				yes++
			} else {
				no++
			}
		})
	}

	t.Logf("перепись отбора источников: случаев %d; отобрано %d; отклонено %d", len(cases), yes, no)
	if yes == 0 || no == 0 {
		t.Fatal("отбор проверен только с одной стороны — отбор, который берёт всех, " +
			"и отбор, который не берёт никого, оба прошли бы такую пробу")
	}
}
