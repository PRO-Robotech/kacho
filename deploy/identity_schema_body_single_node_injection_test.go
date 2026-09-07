// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_schema_body_single_node_injection_test.go — гейт единственного узла
// тела схемы способен УПАСТЬ и способен СМОЛЧАТЬ, и оба доказаны настоящим
// входом, а не зелёным деревом: гейт, доказанный только зелёным деревом,
// доказан не был — он остаётся зелёным и когда перестаёт читать.
//
// Зовутся ТЕ ЖЕ функции, что исполняет гейт (identitySchemaBodyDecls,
// adjudicateIdentitySchemaBodies, identitySchemaBodyOf), а не их копии: копия
// предиката разошлась бы с оригиналом молча и доказывала бы саму себя.
//
// Форм записи тела ДВЕ, и каждая проверяется отдельно и в обе стороны: форма, о
// которой распознаватель не знает, — не край и не редкость, всё записанное в ней
// уходит ИЗ-ПОД НАБЛЮДЕНИЯ.
package deploy_test

import (
	"encoding/base64"
	"sort"
	"strings"
	"testing"
)

// injSchemaBody — тело схемы личности, каким его пишут в профилях. Форма, а не
// имя ключа: имя менялось трижды, форма — ни разу.
const injSchemaBody = `{"$id":"https://schemas.example/identity/v1","$schema":"http://json-schema.org/draft-07/schema#",` +
	`"type":"object","properties":{"traits":{"type":"object","properties":{"email":{"type":"string"}},` +
	`"required":["email"],"additionalProperties":false}}}`

// injIndent — тело как блочный скаляр YAML с отступом.
func injIndent(body, pad string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// (A) ВЕРДИКТ — по настоящему тексту профиля, а не по собранной вручную структуре.

func TestSchemaBodySingleNodeGate_ProvenByInjection(t *testing.T) {
	cases := []struct {
		name        string
		yaml        string
		wantReasons []string // отсортированные причины; пусто — молчание
		wantPathSub string   // подстрока, которую отказ обязан назвать
	}{
		{
			name: "ДЕФЕКТ #1239: тело объявлено ВТОРЫМ узлом, копией без якоря — две находки",
			yaml: "global:\n  kacho:\n    identity:\n      inheritedSchemas: &anchorName\n        - id: default\n          url: \"base64://" +
				base64.StdEncoding.EncodeToString([]byte(injSchemaBody)) + "\"\n" +
				"kratos:\n  identitySchemas:\n    \"identity.default.schema.json\": |\n" + injIndent(injSchemaBody, "      ") + "\n",
			wantReasons: []string{schemaBodyDeclaredTwice, schemaBodyNotAnchored},
			wantPathSub: "kratos.identitySchemas",
		},
		{
			name: "законный близнец: тело ОДНО под якорем, второй потребитель берёт ССЫЛКУ — молчит",
			yaml: "global:\n  kacho:\n    identity:\n      inheritedSchemas: &anchorName\n        - id: default\n          url: \"base64://" +
				base64.StdEncoding.EncodeToString([]byte(injSchemaBody)) + "\"\n" +
				"kratos:\n  kratos:\n    config:\n      identity:\n        schemas: *anchorName\n",
		},
		{
			name: "законный близнец: профиль тела не несёт вовсе — судить нечего, молчит",
			yaml: "kratos:\n  enabled: true\n  kratos:\n    config:\n      log:\n        level: info\n",
		},
		{
			name: "законный близнец: АДРЕС тела (`file:///…`) телом не считается — молчит",
			yaml: "kratos:\n  kratos:\n    config:\n      identity:\n        schemas:\n          - id: kacho_user_v2\n" +
				"            url: file:///etc/kaname-identity/identity.schema.json\n",
		},
		{
			name:        "ось (2) в одиночку: тело ОДНО, но без якоря — сослаться нечем",
			yaml:        "kratos:\n  identitySchemas:\n    \"identity.default.schema.json\": |\n" + injIndent(injSchemaBody, "      ") + "\n",
			wantReasons: []string{schemaBodyNotAnchored},
			wantPathSub: "kratos.identitySchemas",
		},
		{
			name: "ось (1) в одиночку: ДВА тела, оба под якорями — единственность всё равно нарушена",
			yaml: "a: &one\n  url: \"base64://" + base64.StdEncoding.EncodeToString([]byte(injSchemaBody)) + "\"\n" +
				"b: &two\n  body: |\n" + injIndent(injSchemaBody, "    ") + "\n",
			wantReasons: []string{schemaBodyDeclaredTwice},
			wantPathSub: "b.body",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decls, unknown, err := identitySchemaBodyDecls([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("синтетика не разбирается как YAML: %v", err)
			}
			if len(unknown) != 0 {
				t.Fatalf("законная форма объявлена неизвестной: %v — гейт, краснеющий на "+
					"исправном профиле, отключат первым", unknown)
			}
			got := adjudicateIdentitySchemaBodies("синтетика.yaml", decls)
			reasons := make([]string, 0, len(got))
			all := make([]string, 0, len(got))
			for _, f := range got {
				reasons = append(reasons, f.Reason)
				all = append(all, strings.Join(f.Paths, " "))
			}
			sort.Strings(reasons)
			want := append([]string{}, tc.wantReasons...)
			sort.Strings(want)
			if strings.Join(reasons, "|") != strings.Join(want, "|") {
				t.Fatalf("вердикт не тот: причины %v; ждали %v (объявлений прочитано %d: %v)",
					reasons, want, len(decls), decls)
			}
			if tc.wantPathSub == "" {
				return
			}
			if !strings.Contains(strings.Join(all, " "), tc.wantPathSub) {
				t.Fatalf("отказ не называет координату %q: %v — чинить его пришлось бы наугад",
					tc.wantPathSub, all)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (B) РАСПОЗНАВАТЕЛЬ ТЕЛА — обе формы и их законные близнецы.
//
// Распознаватель судит ФОРМУ тела, а не имя ключа и не подстроку. Поэтому
// проверяется и обратное: величина, ПОХОЖАЯ на тело (объект JSON, слово `traits`
// внутри), но формы не имеющая, обязана молчать. Судить по вхождению слова
// значило бы считать телом любой комментарий, который о нём говорит.

func TestSchemaBodyRecogniser_ProvenByInjection(t *testing.T) {
	b64 := func(enc *base64.Encoding) string { return "base64://" + enc.EncodeToString([]byte(injSchemaBody)) }

	cases := []struct {
		name    string
		value   string
		want    bool
		unknown bool
	}{
		{name: "форма 1 — голый JSON", value: injSchemaBody, want: true},
		{name: "форма 1' — тот же JSON с пробелами вокруг", value: "\n  " + injSchemaBody + "  \n", want: true},
		{name: "форма 2 — base64 стандартная", value: b64(base64.StdEncoding), want: true},
		{name: "форма 2' — base64 стандартная без набивки", value: b64(base64.RawStdEncoding), want: true},
		{name: "форма 2'' — base64 URL-безопасная", value: b64(base64.URLEncoding), want: true},
		{name: "форма 2''' — base64 URL-безопасная без набивки", value: b64(base64.RawURLEncoding), want: true},

		{name: "законный близнец: адрес файла — телом не считается", value: "file:///etc/kaname-identity/identity.schema.json"},
		{name: "законный близнец: обычная строка настроек", value: "postgres://kratos@pg:5432/kratos?sslmode=require"},
		{name: "законный близнец: объект JSON БЕЗ properties.traits", value: `{"properties":{"email":{"type":"string"}}}`},
		{name: "законный близнец: `traits` не под `properties`", value: `{"traits":{"type":"object"},"properties":{"x":{}}}`},
		{name: "законный близнец: проза О ТЕЛЕ — слово есть, формы нет",
			value: "тело схемы личности с признаком traits и properties объявляется одним узлом"},
		{name: "законный близнец: обрезанный JSON не разбирается — телом не считается",
			value: `{"properties":{"traits":`},

		{name: "предпосылка П1: base64 не разбирается ни одной кодировкой — НАХОДКА, а не молчание",
			value: "base64://это-не-кодировка!!!", unknown: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, why := identitySchemaBodyOf(tc.value)
			if tc.unknown {
				if why == "" {
					t.Fatalf("форма, которой распознаватель НЕ ЧИТАЕТ, прошла молча: %q. Это "+
						"худший исход из возможных — ни красного, ни зелёного: всё записанное "+
						"в ней вне наблюдения", tc.value)
				}
				return
			}
			if why != "" {
				t.Fatalf("законная величина объявлена неизвестной формой (%s): %q", why, tc.value)
			}
			if got != tc.want {
				t.Fatalf("распознаватель ответил %v, ждали %v на величине %q", got, tc.want, tc.value)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (C) ССЫЛКА НЕ ЕСТЬ КОПИЯ — дискриминатор, на котором держится весь гейт.
//
// Разбор «в значения» развернул бы ссылку в те же данные, и якорь с копией стали
// бы неразличимы: гейт краснел бы ровно на том решении, ради которого заведён.
// Здесь это утверждение проверяется, а не подразумевается — ОДИН и тот же текст
// подаётся дважды, различаясь только тем, ссылка во втором употреблении или
// копия.

func TestSchemaBodyGate_AliasIsNotACopy(t *testing.T) {
	body := "base64://" + base64.StdEncoding.EncodeToString([]byte(injSchemaBody))
	head := "global:\n  kacho:\n    identity:\n      inheritedSchemas: &anchorName\n        - id: default\n          url: \"" + body + "\"\n"

	withAlias := head + "kratos:\n  kratos:\n    config:\n      identity:\n        schemas: *anchorName\n"
	withCopy := head + "kratos:\n  kratos:\n    config:\n      identity:\n        schemas:\n          - id: default\n            url: \"" + body + "\"\n"

	aDecls, aUnknown, err := identitySchemaBodyDecls([]byte(withAlias))
	if err != nil || len(aUnknown) != 0 {
		t.Fatalf("вариант со ССЫЛКОЙ не разобран: err=%v unknown=%v", err, aUnknown)
	}
	cDecls, cUnknown, err := identitySchemaBodyDecls([]byte(withCopy))
	if err != nil || len(cUnknown) != 0 {
		t.Fatalf("вариант с КОПИЕЙ не разобран: err=%v unknown=%v", err, cUnknown)
	}

	if len(aDecls) != 1 {
		t.Fatalf("ССЫЛКА засчитана объявлением: прочитано %d (%v) — гейт краснел бы на самом "+
			"решении, ради которого заведён", len(aDecls), aDecls)
	}
	if len(cDecls) != 2 {
		t.Fatalf("КОПИЯ не засчитана вторым объявлением: прочитано %d (%v) — именно этот вход "+
			"гейт обязан ловить", len(cDecls), cDecls)
	}
	if got := adjudicateIdentitySchemaBodies("ссылка.yaml", aDecls); len(got) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %+v", got)
	}
	if got := adjudicateIdentitySchemaBodies("копия.yaml", cDecls); len(got) == 0 {
		t.Fatalf("копия прошла молча — гейт не способен упасть на своём предмете")
	}
	t.Logf("дискриминатор доказан: ссылка → объявлений %d · копия → объявлений %d", len(aDecls), len(cDecls))
}
