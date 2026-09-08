// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"fmt"
	"strings"
	"testing"
)

// docs_example_keys_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт ключей примеров
// способен упасть и способен смолчать.
//
// Инъекция подаётся НАСТОЯЩИМ входом — текстом страницы той же формы, что в
// корпусе, — и правит РОВНО ОДИН факт против положительного близнеца: имя ключа
// либо заголовок блока. Всё остальное в паре побайтово одинаково, поэтому
// красное не могло прийти от соседа.
//
// Судят инъекцию ТЕ ЖЕ функции, что исполняются обходом дерева
// (`extractDocsJSONExamples` → `docsExampleLaneOf` → `docsLaneMessage` →
// `docsExampleKeyFindings`). Проверка, собранная в пробе иначе, чем в боевом
// пути, доказывала бы способность падать у своей копии.
//
// Осей ПЯТЬ — четыре полосы плюс заголовок вне словаря, — и у каждой обе
// стороны: ключ, которого в сообщении ЭТОЙ полосы нет, обязан дать красное с
// координатой; законный ключ соседнего поля ТОГО ЖЕ сообщения обязан дать
// молчание. Односторонняя проба зеленела бы на распознавателе, отвергающем всё,
// и краснела бы на любом.

// docsKeysInjectionPage — страница той же формы, что корпус.
//
// Подставляемых мест четыре, и в КАЖДОЙ ПАРЕ близнецов меняется ровно одно:
// `%[1]s`/`%[2]s` — метод и адрес операции, `%[3]s` — объявление полосы
// (атрибут `title` целиком либо пусто), `%[4]s` — тело примера.
const docsKeysInjectionPage = `# Пробная страница

<ApiOperation method="%[1]s" endpoint="%[2]s">

#### Пример

<CodeBlock language="json"%[3]s>
  {dedent` + "`" + `
%[4]s
  ` + "`" + `}
</CodeBlock>

</ApiOperation>
`

// docsKeysInjectionCase — одна сторона одной оси.
type docsKeysInjectionCase struct {
	name string
	// method / endpoint — операция, внутри которой стоит пример.
	method, endpoint string
	// titleAttr — объявление полосы; пусто означает умолчание (ответ).
	titleAttr string
	// body — тело примера.
	body string
	// wantKeys — ключи, которые гейт обязан назвать находкой; пусто = молчание.
	wantKeys []string
	// wantUnjudgeable — полоса объявлена, но судить её не с чем.
	wantUnjudgeable bool
	why             string
}

// runDocsKeysInjection прогоняет один случай ТЕМ ЖЕ путём, что обход дерева.
func runDocsKeysInjection(t *testing.T, c docsKeysInjectionCase) (keys []string, unjudgeable bool, walked bool) {
	t.Helper()
	page := fmt.Sprintf(docsKeysInjectionPage, c.method, c.endpoint, c.titleAttr, c.body)
	examples := extractDocsJSONExamples("проба.mdx", page)
	if len(examples) != 1 {
		t.Fatalf("%s: разбор дал %d примеров вместо одного — инъекция не доехала до предмета",
			c.name, len(examples))
	}
	ex := examples[0]
	lane, known := docsExampleLaneOf(ex)
	if !known {
		return nil, true, false
	}
	b, ok := resolveHTTPBinding(ex.Method, docsPathParam.ReplaceAllString(ex.Endpoint, "x"))
	if !ok {
		t.Fatalf("%s: биндинг %s %s не резолвится — инъекция беспредметна", c.name, ex.Method, ex.Endpoint)
	}
	if _, why := docsLaneMessage(b, lane); why != "" {
		return nil, true, false
	}
	found, walkedOK := docsExampleKeyFindings(b, lane, ex)
	for _, f := range found {
		keys = append(keys, f.Key)
	}
	return keys, false, walkedOK
}

func TestDocsExampleKeysGateFallsAndStaysSilentOnItsTwin(t *testing.T) {
	cases := []docsKeysInjectionCase{
		// --- ось 1: полоса «ответ» (умолчание) ---
		{
			name: "ответ · законный ключ",
			// GET роли отдаёт `Role`, у которого есть `id` и `description`.
			method: "GET", endpoint: "/iam/v1/roles/{role_id}",
			body:     `    { "id": "rol-1", "description": "тот же ключ, что у соседнего поля" }`,
			wantKeys: nil,
			why:      "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ оси 1",
		},
		{
			name:   "ответ · ключа нет в сообщении",
			method: "GET", endpoint: "/iam/v1/roles/{role_id}",
			body:     `    { "id": "rol-1", "descriptionn": "один факт против близнеца — опечатка в имени" }`,
			wantKeys: []string{"descriptionn"},
			why:      "ОДИН факт: имя ключа",
		},
		{
			name:   "ответ · полоса объявлена ЯВНО",
			method: "GET", endpoint: "/iam/v1/roles/{role_id}",
			titleAttr: ` title="Ответ"`,
			body:      `    { "id": "rol-1", "descriptionn": "x" }`,
			wantKeys:  []string{"descriptionn"},
			why: "запись словаря обязана быть исполнима: объявленная явно полоса судится " +
				"тем же сообщением, что и взятая умолчанием",
		},
		{
			name:   "ответ · ключа нет во ВЛОЖЕННОМ сообщении",
			method: "GET", endpoint: "/iam/v1/roles",
			body: `    { "roles": [ { "id": "rol-1", "nosuchfield": 1 } ] }`,
			// Путь до вложенного ключа обязан быть в находке целиком: без него
			// читатель ищет `nosuchfield` в корне ответа.
			wantKeys: []string{"roles[0].nosuchfield"},
			why:      "спуск в повторяющееся поле-сообщение",
		},

		// --- ось 2: полоса «ответ операции» ---
		{
			name:   "ответ операции · законный ключ",
			method: "POST", endpoint: "/vpc/v1/addressPools",
			titleAttr: ` title="Ответ операции"`,
			body:      `    { "id": "apl-1", "kind": "EXTERNAL_PUBLIC" }`,
			wantKeys:  nil,
			why:       "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ оси 2: ключ есть в AddressPool",
		},
		{
			name:   "ответ операции · ключ конверта, а не нагрузки",
			method: "POST", endpoint: "/vpc/v1/addressPools",
			titleAttr: ` title="Ответ операции"`,
			body:      `    { "id": "apl-1", "done": true }`,
			// `done` есть у Operation и нет у AddressPool. Это и проверяет, что
			// полоса судится против НАГРУЗКИ, а не против конверта.
			wantKeys: []string{"done"},
			why:      "ОДИН факт: ключ принадлежит конверту операции",
		},
		{
			name:   "ответ операции · у RPC её нет",
			method: "GET", endpoint: "/iam/v1/roles/{role_id}",
			titleAttr:       ` title="Ответ операции"`,
			body:            `    { "id": "rol-1" }`,
			wantUnjudgeable: true,
			why:             "чтение операции не заводит: полоса объявлена, судить не с чем",
		},

		// --- ось 3: полоса «запрос» ---
		{
			name:   "запрос · законный ключ",
			method: "POST", endpoint: "/registry/v1/registries/{registryId}/repositories/{repository}:rename",
			titleAttr: ` title="Тело запроса"`,
			body:      `    { "newName": "backend/api-v2" }`,
			wantKeys:  nil,
			why:       "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ оси 3",
		},
		{
			name:   "запрос · ключа нет в сообщении запроса",
			method: "POST", endpoint: "/registry/v1/registries/{registryId}/repositories/{repository}:rename",
			titleAttr: ` title="Тело запроса"`,
			body:      `    { "newNam": "backend/api-v2" }`,
			wantKeys:  []string{"newNam"},
			why:       "ОДИН факт: имя ключа",
		},

		// --- ось 4: полоса «отказ» ---
		{
			name:   "отказ · законный ключ",
			method: "POST", endpoint: "/vpc/v1/subnets",
			titleAttr: ` title="Отказ"`,
			body:      `    { "code": 3, "message": "…" }`,
			wantKeys:  nil,
			why:       "ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ оси 4",
		},
		{
			name:   "отказ · ключ ресурса, а не Status",
			method: "POST", endpoint: "/vpc/v1/subnets",
			titleAttr: ` title="Отказ"`,
			body:      `    { "code": 3, "networkId": "net-1" }`,
			wantKeys:  []string{"networkId"},
			why:       "ОДИН факт: ключ принадлежит ресурсу",
		},

		// --- ось 5: заголовок вне словаря ---
		{
			name:   "заголовок вне словаря — находка, а не третье умолчание",
			method: "GET", endpoint: "/iam/v1/roles/{role_id}",
			titleAttr:       ` title="Пример ответа"`,
			body:            `    { "id": "rol-1" }`,
			wantUnjudgeable: true,
			why:             "иначе опечатка в заголовке молча меняла бы полосу",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keys, unjudgeable, walked := runDocsKeysInjection(t, c)
			if unjudgeable != c.wantUnjudgeable {
				t.Fatalf("«не рассудима» = %v, ожидалось %v (%s)", unjudgeable, c.wantUnjudgeable, c.why)
			}
			if c.wantUnjudgeable {
				return
			}
			if !walked {
				t.Fatalf("пример не обойдён — инъекция не доехала до предмета (%s)", c.why)
			}
			if strings.Join(keys, ",") != strings.Join(c.wantKeys, ",") {
				t.Fatalf("находки %v, ожидались %v (%s)", keys, c.wantKeys, c.why)
			}
		})
	}
	t.Logf("осей 5; сторон проверено %d", len(cases))
}

// TestDocsExampleKeysFindingNamesTheCoordinate — находка обязана называть
// координату и полосу, а не только число.
//
// Заведено отдельно, потому что переустройство гейта сохраняет способность
// падать и теряет ДИАГНОСТИКУ незаметно: находка, называющая симптом вместо
// места, посылает читателя искать не там, и её снимают как непонятную.
func TestDocsExampleKeysFindingNamesTheCoordinate(t *testing.T) {
	c := docsKeysInjectionCase{
		name: "координата", method: "GET", endpoint: "/iam/v1/roles/{role_id}",
		body: `    { "descriptionn": "x" }`,
	}
	page := fmt.Sprintf(docsKeysInjectionPage, c.method, c.endpoint, c.titleAttr, c.body)
	ex := extractDocsJSONExamples("services/iam/docs/content/api/role.mdx", page)[0]
	b, ok := resolveHTTPBinding(ex.Method, docsPathParam.ReplaceAllString(ex.Endpoint, "x"))
	if !ok {
		t.Fatal("биндинг не резолвится — проба беспредметна")
	}
	found, _ := docsExampleKeyFindings(b, docsLaneResponse, ex)
	if len(found) != 1 {
		t.Fatalf("находок %d вместо одной", len(found))
	}
	got := found[0].String()
	for _, want := range []string{
		"services/iam/docs/content/api/role.mdx:", // файл
		"GET",                      // метод операции
		"/iam/v1/roles/{role_id}",  // её адрес
		"полоса «ответ»",           // полоса, по которой судили
		"kaname.cloud.iam.v1.Role", // сообщение, в котором ключ искали
		`"descriptionn"`,           // сам ключ
	} {
		if !strings.Contains(got, want) {
			t.Errorf("текст находки не называет %q:\n  %s", want, got)
		}
	}
	t.Logf("текст находки: %s", got)
}
