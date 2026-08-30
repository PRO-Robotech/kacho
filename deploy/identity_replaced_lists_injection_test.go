// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_replaced_lists_injection_test.go — гейт замещаемых списков способен
// УПАСТЬ и способен СМОЛЧАТЬ, и оба доказаны настоящим входом, а не зелёным
// деревом: гейт, доказанный только зелёным деревом, доказан не был — он остаётся
// зелёным и когда перестаёт читать.
//
// Зовутся ТЕ ЖЕ функции, что исполняет гейт (adjudicateReplacedIdentityLists,
// identityProviderLists, identityOurLists), а не их копии: копия предиката
// разошлась бы с оригиналом молча и доказывала бы саму себя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ФОРМ ЗАПИСИ СТОЛЬКО, СКОЛЬКО ИХ ЗДЕСЬ
//
// Список пишется в этом дереве ЧЕТЫРЬМЯ законными формами на каждой стороне, и
// форма, о которой распознаватель не знает, — не край и не редкость: всё
// записанное в ней уходит ИЗ-ПОД НАБЛЮДЕНИЯ. Поэтому каждая форма проверяется
// ОТДЕЛЬНО и в обе стороны: дефект в ней обязан находиться, законный близнец той
// же формы — молчать.
package deploy_test

import (
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────────────────────────────────────
// (A) ВЕРДИКТ.

func TestReplacedListsGate_VerdictProvenByInjection(t *testing.T) {
	const prov, ours = "поставщик", "наш"

	cases := []struct {
		name     string
		views    []identityStackLists
		ledger   map[string]string
		wantPath string // "" — молчание
		wantWhy  string
		wantLost []string
	}{
		{
			name: "ДЕФЕКТ #1238: наш список замещает и теряет записи, решения нет — находка с именем",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{"selfservice.allowed_return_urls": {"/", "/dashboard", "http://console.kacho.local/"}},
				Ours:     map[string][]string{"selfservice.allowed_return_urls": {"https://app.example/"}},
			}},
			ledger:   map[string]string{},
			wantPath: "selfservice.allowed_return_urls",
			wantWhy:  replacedListNotDecided,
			wantLost: []string{"/", "/dashboard", "http://console.kacho.local/"},
		},
		{
			name: "законный близнец: ТО ЖЕ сужение, но решение записано — молчит",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{"selfservice.allowed_return_urls": {"/", "/dashboard"}},
				Ours:     map[string][]string{"selfservice.allowed_return_urls": {"https://app.example/"}},
			}},
			ledger: map[string]string{"selfservice.allowed_return_urls": "сужение намеренное, причина названа"},
		},
		{
			name: "законный близнец: наш список ПОКРЫВАЕТ — молчит без всякой ведомости",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{"hooks": {"hook=session"}},
				Ours:     map[string][]string{"hooks": {"hook=web_hook", "hook=session"}},
			}},
			ledger: map[string]string{},
		},
		{
			name: "законный близнец: список объявляет ТОЛЬКО поставщик — мы его не замещаем, молчит",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{"courier.templates": {"a", "b"}},
				Ours:     map[string][]string{"secrets.cookie": {"x"}},
			}},
			ledger: map[string]string{},
		},
		{
			name: "законный близнец: список объявляем ТОЛЬКО мы — замещать нечего, молчит",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{},
				Ours:     map[string][]string{"serve.public.cors.allowed_headers": {"Authorization"}},
			}},
			ledger: map[string]string{},
		},
		{
			name: "самоистечение (3): решение на путь, который больше не объявлен обеими сторонами",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{"secrets.cookie": {"x"}},
				Ours:     map[string][]string{"secrets.cookie": {"y"}},
			}},
			ledger: map[string]string{
				"secrets.cookie":  "замена заглушки настоящим ключом — решение записано",
				"secrets.retired": "путь, которого в дереве больше нет",
			},
			wantPath: "secrets.retired",
			wantWhy:  replacedListNoSubject,
		},
		{
			name: "самоистечение (4): решение на путь, который ВЕЗДЕ покрыт вычислением",
			views: []identityStackLists{{
				Stack:    "dev",
				Provider: map[string][]string{"hooks": {"hook=session"}},
				Ours:     map[string][]string{"hooks": {"hook=session", "hook=web_hook"}},
			}},
			ledger:   map[string]string{"hooks": "когда-то сужали, теперь покрываем"},
			wantPath: "hooks",
			wantWhy:  replacedListRedundant,
		},
		{
			name: "сужение только на ОДНОМ стеке — находка называет ИМЕННО его",
			views: []identityStackLists{
				{
					Stack:    "dev",
					Provider: map[string][]string{"p": {"a"}},
					Ours:     map[string][]string{"p": {"a", "b"}},
				},
				{
					Stack:    "fe3455",
					Provider: map[string][]string{"p": {"a", "z"}},
					Ours:     map[string][]string{"p": {"a", "b"}},
				},
			},
			ledger:   map[string]string{},
			wantPath: "p",
			wantWhy:  replacedListNotDecided,
			wantLost: []string{"z"},
		},
		// ─── ГРАНИЦА ИМЕНИ ПУТИ ────────────────────────────────────────────
		//
		// Ведомость ключуется ПОЛНЫМ путём и сверяется точным равенством, а не
		// вхождением подстроки. Проверяется это, а не подразумевается: сверка по
		// вхождению отдала бы соседний путь под чужое решение — и записанное там
		// сужение молча накрыло бы список, о котором никто не решал. Обе стороны
		// границы названы отдельно, потому что подстрока ошибается в обе.
		{
			name: "граница имени: путь с СУФФИКСОМ не уходит под решение соседнего пути",
			views: []identityStackLists{{
				Stack: "dev",
				Provider: map[string][]string{
					"selfservice.allowed_return_urls":       {"/"},
					"selfservice.allowed_return_urls_extra": {"/dashboard"},
				},
				Ours: map[string][]string{
					"selfservice.allowed_return_urls":       {"https://app.example/"},
					"selfservice.allowed_return_urls_extra": {"https://app.example/"},
				},
			}},
			ledger:   map[string]string{"selfservice.allowed_return_urls": "сужение намеренное, причина названа"},
			wantPath: "selfservice.allowed_return_urls_extra",
			wantWhy:  replacedListNotDecided,
			wantLost: []string{"/dashboard"},
		},
		{
			name: "граница имени: ПРЕФИКС пути не уходит под решение более длинного",
			views: []identityStackLists{{
				Stack: "dev",
				Provider: map[string][]string{
					"selfservice.allowed":             {"/dashboard"},
					"selfservice.allowed_return_urls": {"/"},
				},
				Ours: map[string][]string{
					"selfservice.allowed":             {"https://app.example/"},
					"selfservice.allowed_return_urls": {"https://app.example/"},
				},
			}},
			ledger:   map[string]string{"selfservice.allowed_return_urls": "сужение намеренное, причина названа"},
			wantPath: "selfservice.allowed",
			wantWhy:  replacedListNotDecided,
			wantLost: []string{"/dashboard"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adjudicateReplacedIdentityLists(tc.views, tc.ledger)
			if tc.wantPath == "" {
				if len(got) != 0 {
					t.Fatalf("законный близнец объявлен находкой: %+v — гейт, краснеющий на "+
						"исправном, отключат первым", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("ждали ровно одну находку по %q, получили %d: %+v",
					tc.wantPath, len(got), got)
			}
			f := got[0]
			if f.Path != tc.wantPath || f.Reason != tc.wantWhy {
				t.Fatalf("находка не та: путь %q причина %q; ждали %q / %q",
					f.Path, f.Reason, tc.wantPath, tc.wantWhy)
			}
			if tc.wantLost != nil {
				if strings.Join(f.Lost, "|") != strings.Join(tc.wantLost, "|") {
					t.Fatalf("перечень ПОТЕРЯННЫХ записей не тот: %v; ждали %v — без него "+
						"отказ называет путь и не называет цену", f.Lost, tc.wantLost)
				}
			}
			if tc.wantWhy == replacedListNotDecided && f.Stack == "" {
				t.Fatalf("находка не называет стек — чинить её пришлось бы наугад")
			}
		})
	}
	_ = prov
	_ = ours
}

// ─────────────────────────────────────────────────────────────────────────────
// (B) ФОРМЫ ЗАПИСИ СПИСКА НА СТОРОНЕ ПОСТАВЩИКА (YAML профилей).

func TestReplacedListsGate_ProviderFormsProvenByInjection(t *testing.T) {
	cases := []struct {
		name   string
		yaml   string
		path   string
		want   []string
		absent bool // путь НЕ должен быть распознан как список
	}{
		{
			name: "форма 1 — блочная последовательность (values.dev.yaml)",
			yaml: "selfservice:\n  allowed_return_urls:\n    - \"/\"\n    - \"/dashboard\"\n",
			path: "selfservice.allowed_return_urls",
			want: []string{"/", "/dashboard"},
		},
		{
			name: "форма 2 — поточная последовательность (values.fe3455-ory-posture.yaml)",
			yaml: "selfservice:\n  allowed_return_urls: [\"/\", \"/dashboard\"]\n",
			path: "selfservice.allowed_return_urls",
			want: []string{"/", "/dashboard"},
		},
		{
			name: "форма 3 — ссылка на якорь (identity.schemas: *kachoInheritedIdentitySchemas)",
			yaml: "global:\n  anchor: &a\n    - id: default\n      url: \"base64://xxx\"\nidentity:\n  schemas: *a\n",
			path: "identity.schemas",
			want: []string{"id=default,url=base64://xxx"},
		},
		{
			name: "форма 4 — последовательность карт, поточная (hooks: [{hook: session}])",
			yaml: "selfservice:\n  flows:\n    registration:\n      after:\n        password:\n          hooks: [{hook: session}]\n",
			path: "selfservice.flows.registration.after.password.hooks",
			want: []string{"hook=session"},
		},
		{
			name: "форма 4' — та же карта БЛОЧНО: нормализуется в ту же строку",
			yaml: "hooks:\n  - hook: session\n",
			path: "hooks",
			want: []string{"hook=session"},
		},
		{
			name: "вложенная карта внутри записи — раскладывается в листья",
			yaml: "hooks:\n  - hook: web_hook\n    config:\n      url: https://x/y\n      method: POST\n",
			path: "hooks",
			want: []string{"hook=web_hook,method=POST,url=https://x/y"},
		},
		{
			name: "пустой поточный список — это СПИСОК, а не отсутствие ключа",
			yaml: "selfservice:\n  allowed_return_urls: []\n",
			path: "selfservice.allowed_return_urls",
			want: []string{},
		},
		{
			name:   "законный близнец: под ключом СКАЛЯР — списком не считается",
			yaml:   "selfservice:\n  default_browser_return_url: \"/\"\n",
			path:   "selfservice.default_browser_return_url",
			absent: true,
		},
		{
			name:   "законный близнец: под ключом КАРТА — списком не считается",
			yaml:   "serve:\n  public:\n    cors:\n      enabled: true\n",
			path:   "serve.public.cors",
			absent: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tree map[string]any
			if err := yaml.Unmarshal([]byte(tc.yaml), &tree); err != nil {
				t.Fatalf("синтетика не разбирается: %v", err)
			}
			got, unknown := identityProviderLists(tree)
			if len(unknown) != 0 {
				t.Fatalf("нормализатор назвал форму неизвестной: %v", unknown)
			}
			items, ok := got[tc.path]
			if tc.absent {
				if ok {
					t.Fatalf("не-список признан списком по пути %q: %v — гейт потребовал бы "+
						"решения там, где замещения записей не бывает", tc.path, items)
				}
				return
			}
			if !ok {
				paths := make([]string, 0, len(got))
				for p := range got {
					paths = append(paths, p)
				}
				sort.Strings(paths)
				t.Fatalf("форма НЕ РАСПОЗНАНА: списка %q нет среди прочитанных %v. Всё, "+
					"записанное в этой форме, ушло бы из-под наблюдения молча", tc.path, paths)
			}
			if strings.Join(items, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("записи прочитаны не так: %v; ждали %v", items, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (C) ФОРМЫ ЗАПИСИ СПИСКА НА НАШЕЙ СТОРОНЕ (тело Go-шаблона).

func TestReplacedListsGate_OurFormsProvenByInjection(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		path    string
		want    []string
		absent  bool
		unknown string // подстрока, которую обязана назвать находка предпосылки
	}{
		{
			name: "форма 1 — блочная последовательность с подстановкой шаблона",
			body: "selfservice:\n  allowed_return_urls:\n    - {{ $app }}/\n",
			path: "selfservice.allowed_return_urls",
			want: []string{"{{ $app }}/"},
		},
		{
			name: "форма 2 — поточная последовательность",
			body: "serve:\n  public:\n    cors:\n      allowed_methods: [POST, GET, PUT]\n",
			path: "serve.public.cors.allowed_methods",
			want: []string{"POST", "GET", "PUT"},
		},
		{
			name: "форма 3 — последовательность карт с продолжением по отступу",
			body: "hooks:\n  - hook: web_hook\n    config:\n      url: https://x/y\n      method: POST\n",
			path: "hooks",
			want: []string{"hook=web_hook,method=POST,url=https://x/y"},
		},
		{
			name: "форма 3' — запись-карта в одну строку нормализуется как у поставщика",
			body: "hooks:\n  - hook: session\n",
			path: "hooks",
			want: []string{"hook=session"},
		},
		{
			name: "форма 4 — записи, порождённые `range`: строки управления не мешают",
			body: "identity:\n  schemas:\n    - id: kacho_user_v2\n      url: file:///x\n" +
				"{{- range $inherited }}\n    - id: {{ .id }}\n      url: {{ .url }}\n{{- end }}\n",
			path: "identity.schemas",
			want: []string{"id=kacho_user_v2,url=file:///x", "id={{ .id }},url={{ .url }}"},
		},
		{
			name: "форма 2' — пустой поточный список читается как список",
			body: "secrets:\n  cookie: []\n",
			path: "secrets.cookie",
			want: []string{},
		},
		{
			name:   "законный близнец: КАРТА, а не список — не считается",
			body:   "serve:\n  public:\n    cors:\n      enabled: true\n",
			path:   "serve.public.cors",
			absent: true,
		},
		{
			name:   "законный близнец: скаляр с двоеточием внутри величины не рвёт разбор",
			body:   "serve:\n  admin:\n    base_url: http://kacho-umbrella-kratos-admin:80/\n",
			path:   "serve.admin.base_url",
			absent: true,
		},
		{
			name: "законный близнец: строки управления (if/else/end/$var/комментарий) формой НЕ считаются",
			body: "{{- $x := .Values.a -}}\n{{- if $x }}\n{{- /* пояснение */ -}}\n{{- else }}\n{{- end }}\n" +
				"secrets:\n  cookie:\n    - ${K}\n",
			path: "secrets.cookie",
			want: []string{"${K}"},
		},
		{
			name: "законный близнец: комментарий шаблона с дефисом и пробелом (`{{- /*`) — не находка",
			body: "{{- /* ── ЗАГОЛОВОК ──────────────\n     вторая строка пояснения */ -}}\nsecrets:\n  cookie:\n    - ${K}\n",
			path: "secrets.cookie",
			want: []string{"${K}"},
		},
		{
			name:    "ПЯТАЯ форма: список печатает действие шаблона (`toYaml`) — НАХОДКА предпосылки",
			body:    "selfservice:\n  allowed_return_urls: {{ toYaml .Values.urls | nindent 4 }}\n",
			unknown: "toYaml",
		},
		{
			name:    "ПЯТАЯ форма: содержимое печатает одинокое действие (`include`) — НАХОДКА предпосылки",
			body:    "selfservice:\n{{ include \"kacho.identity.returnUrls\" . }}\n",
			unknown: "include",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, unknown := identityOurLists(tc.body)
			if tc.unknown != "" {
				if len(unknown) == 0 {
					t.Fatalf("форма, которой разбор НЕ ЧИТАЕТ, прошла молча: %q. Это худший "+
						"исход из возможных — ни красного, ни зелёного: всё записанное в ней "+
						"вне наблюдения", tc.body)
				}
				if !strings.Contains(strings.Join(unknown, " "), tc.unknown) {
					t.Fatalf("находка предпосылки не называет виновника %q: %v", tc.unknown, unknown)
				}
				return
			}
			if len(unknown) != 0 {
				t.Fatalf("законная форма объявлена неизвестной: %v — гейт, краснеющий на "+
					"исправном шаблоне, отключат первым", unknown)
			}
			items, ok := got[tc.path]
			if tc.absent {
				if ok {
					t.Fatalf("не-список признан списком по пути %q: %v", tc.path, items)
				}
				return
			}
			if !ok {
				paths := make([]string, 0, len(got))
				for p := range got {
					paths = append(paths, p)
				}
				sort.Strings(paths)
				t.Fatalf("форма НЕ РАСПОЗНАНА: списка %q нет среди прочитанных %v", tc.path, paths)
			}
			if strings.Join(items, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("записи прочитаны не так: %v; ждали %v", items, tc.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (D) СРАВНИМОСТЬ СТОРОН.
//
// Покрытие вычисляется сравнением строк, поэтому одна и та же запись, написанная
// у поставщика ПОТОЧНОЙ картой, а у нас БЛОЧНОЙ, обязана дать одну и ту же
// строку. Разойдись нормализаторы — покрытие перестало бы вычисляться вовсе, и
// КАЖДЫЙ общий список потребовал бы записи в ведомости: гейт превратился бы в
// ведомость исключений, то есть в форму без содержания.

func TestReplacedListsGate_BothSidesNormaliseTheSameItem(t *testing.T) {
	var provTree map[string]any
	if err := yaml.Unmarshal([]byte("hooks: [{hook: web_hook, config: {url: https://x/y, method: POST}}]\n"), &provTree); err != nil {
		t.Fatalf("синтетика поставщика не разбирается: %v", err)
	}
	prov, unknownP := identityProviderLists(provTree)
	if len(unknownP) != 0 {
		t.Fatalf("сторона поставщика: неизвестная форма %v", unknownP)
	}
	ours, unknownO := identityOurLists(
		"hooks:\n  - hook: web_hook\n    config:\n      url: https://x/y\n      method: POST\n")
	if len(unknownO) != 0 {
		t.Fatalf("наша сторона: неизвестная форма %v", unknownO)
	}

	if len(prov["hooks"]) != 1 || len(ours["hooks"]) != 1 {
		t.Fatalf("прочитано не по одной записи: поставщик %v, наша %v", prov["hooks"], ours["hooks"])
	}
	if prov["hooks"][0] != ours["hooks"][0] {
		t.Fatalf("нормализаторы сторон РАЗОШЛИСЬ: поставщик %q, наша %q — покрытие перестало бы "+
			"вычисляться, и гейт выродился бы в ведомость исключений",
			prov["hooks"][0], ours["hooks"][0])
	}
	if !identityListCovers(ours["hooks"], prov["hooks"]) {
		t.Fatalf("одна и та же запись не покрывает сама себя: %v против %v", ours["hooks"], prov["hooks"])
	}
	t.Logf("сравнимость доказана на записи %q", prov["hooks"][0])
}
