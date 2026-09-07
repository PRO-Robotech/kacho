// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_rest_front_header_names_declared_keys_injection_test.go — доказательство
// того, что проверка координат значений СПОСОБНА упасть, и того, что она молчит
// на законных близнецах.
//
// Ввод синтетический: правка настоящей шапки ради доказательства меняла бы общее
// состояние дерева, а разбор вынесен в чистую функцию над ПРОИЗВОЛЬНЫМ текстом и
// ПРОИЗВОЛЬНЫМ набором объявлений.
//
// Каждая ось меняет ровно один факт против контроля: к контрольной шапке
// добавляется один токен. Контроль обязан молчать И быть предметным — у него
// есть координата, которая резолвится, поэтому молчание осей не может быть
// принято за молчание мёртвого правила.
package deploy_test

import (
	"strings"
	"testing"
)

// orfDecls — синтетические объявления посадки. Их две штуки по той же причине,
// по какой их две у настоящей проверки: ключ вправе резолвиться в ЛЮБОМ, и
// одного источника было бы мало, чтобы это доказать.
func orfDecls() map[string]map[string]any {
	return map[string]map[string]any{
		"charts/kaname/values.yaml": {
			"service": map[string]any{
				"public":   map[string]any{"restPort": 9098},
				"internal": map[string]any{"internalRestPort": 9099},
			},
			// Общая ручка транспорта — СКАЛЯР: вложения под ней не бывает.
			"mtls": map[string]any{"httpListeners": false},
		},
		"values.prod.yaml": {
			"mtls": map[string]any{"publicRest": true, "internalRest": true},
		},
	}
}

// orfHeader — контрольная шапка плюс не более одного добавленного токена.
func orfHeader(extra string) string {
	h := "Номер порта объявляет посадка (`service.public.restPort`,\n" +
		"`mtls.publicRest`). Порт берётся ПО ИМЕНИ (`http-rest`), транспорт — по\n" +
		"переменной процесса (`KANAME_REST_SERVER_MTLS_ENABLE`); профиль —\n" +
		"`values.dev-prod.yaml`, шаблон — `templates/service-public.yaml`.\n"
	if extra != "" {
		h += "Добавлено осью: `" + extra + "`.\n"
	}
	return h
}

func TestOwnRestFrontValuesKeyScanIsProvenByInjection(t *testing.T) {
	control, controlFindings := scanHeaderValuesKeys(orfHeader(""), orfDecls())
	t.Logf("контроль: %s", control)
	if len(controlFindings) != 0 {
		t.Fatalf("контроль обязан молчать, а дал находки: %v", controlFindings)
	}
	if control.distinct != 2 {
		t.Fatalf("контроль беспредметен: распознано %d координат вместо 2 — распознаватель "+
			"либо ослеп, либо забирает лишнее: %s", control.distinct, control)
	}

	cases := []struct {
		name       string
		extra      string
		red        bool
		wantReason string
		wantMore   int // на сколько обязано вырасти число распознанных координат
	}{
		{
			name:       "ОСЬ: вложение под скаляром — находка, и она называет виновника",
			extra:      "mtls.httpListeners.publicRest",
			red:        true,
			wantReason: "не карта, вложение невыразимо",
			wantMore:   1,
		},
		{
			name:       "ОСЬ: ключа нет нигде — находка с ДРУГОЙ причиной",
			extra:      "service.public.restPortNumber",
			red:        true,
			wantReason: "ключа нет ни в одном объявлении посадки",
			wantMore:   1,
		},
		{
			name:     "близнец: ключ второго источника — молчит (достаточно любого)",
			extra:    "mtls.internalRest",
			wantMore: 1,
		},
		{
			name:     "близнец: сам скаляр — молчит (он ОБЪЯВЛЕН, вложения у него нет)",
			extra:    "mtls.httpListeners",
			wantMore: 1,
		},
		{
			name:     "близнец: имя файла профиля с дефисом — координатой не является",
			extra:    "values.fe3455-prod.yaml",
			wantMore: 0,
		},
		{
			name:     "близнец: имя файла профиля БЕЗ дефиса — тоже не координата",
			extra:    "values.prod.yaml",
			wantMore: 0,
		},
		{
			name:     "близнец: переменная процесса — координатой не является",
			extra:    "KANAME_INTERNALREST_SERVER_MTLS_ENABLE",
			wantMore: 0,
		},
		{
			name:     "близнец: путь шаблона — координатой не является",
			extra:    "templates/service-internal.yaml",
			wantMore: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			census, findings := scanHeaderValuesKeys(orfHeader(tc.extra), orfDecls())
			t.Logf("перепись: %s", census)

			if got := census.distinct - control.distinct; got != tc.wantMore {
				t.Fatalf("распознано на %d координат больше контроля вместо %d — инъекция "+
					"меряет не тот факт: %s", got, tc.wantMore, census)
			}

			if !tc.red {
				if len(findings) != 0 {
					t.Fatalf("законный близнец объявлен находкой: %v", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("ожидалась ровно одна находка, получено %d: %v", len(findings), findings)
			}
			got := findings[0].String()
			if !strings.Contains(got, tc.extra) {
				t.Fatalf("находка не называет координату %q: %s", tc.extra, got)
			}
			if !strings.Contains(got, tc.wantReason) {
				t.Fatalf("находка не называет причину %q: %s", tc.wantReason, got)
			}
		})
	}
}

// TestOwnRestFrontValuesKeyScanRefusesAnEmptyHeader — шапка без единой
// координаты обязана быть ОТКАЗОМ у гейта дерева, а не тихим успехом. Здесь
// проверяется величина, на которой гейт этот отказ и строит.
func TestOwnRestFrontValuesKeyScanRefusesAnEmptyHeader(t *testing.T) {
	census, findings := scanHeaderValuesKeys(
		"Порт берётся по имени (`http-rest`), профиль — `values.prod.yaml`.\n", orfDecls())
	if len(findings) != 0 {
		t.Fatalf("шапка без координат находок дать не может: %v", findings)
	}
	if census.distinct != 0 {
		t.Fatalf("распознано %d координат там, где их нет: %s", census.distinct, census)
	}
	if census.quoted == 0 {
		t.Fatalf("перепись не отличает «координат нет» от «ничего не прочитано»: %s", census)
	}
}
