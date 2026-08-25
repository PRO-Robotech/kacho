// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanfixturenameform_injection_test.go — инъекция в обе стороны для судьи
// `judgeFixtureNames`.
//
// Зелёный гейт доказывает что-то, только если известно, что он СПОСОБЕН покраснеть.
// Здесь это проверяется на входе, собранном пробой: одна и та же форма шага, одно и
// то же негодное имя, различие ровно одно — утверждает шаг отказ или нет. Тогда
// расхождение вердикта нельзя списать на разный вход.
//
// Законный близнец обязателен: без него «краснеет» означало бы лишь «ловит форму»,
// а первый же ложный срабат на кейсе `…-VAL-NAME-UPPERCASE` (он шлёт негодное имя
// НАМЕРЕННО) заставил бы гейт снять.
package repohygiene

import (
	"encoding/json"
	"strings"
	"testing"
)

// nameFormShape — шаг создания с именем вне канона. Полярность задаётся утверждением:
// %s подставляет тело test-скрипта, всё остальное совпадает побайтово.
const nameFormShape = `{"item":[{"name":"НФ — фикстура даёт имя","item":[
      {"name":"mk","request":{"method":"POST","url":{"raw":"{{baseUrl}}/geo/v1/internal/regions"},
        "body":{"raw":"{\"id\":\"qa-r\",\"name\":\"QA Region CRUD {{runId}}\"}"}},
        "event":[{"listen":"test","script":{"exec":[%s]}}]}
    ]}]}`

const (
	// шаг ожидает успеха — фикстура; негодное имя здесь дефект
	assertSuccess = `"pm.test('ok', () => pm.expect(pm.response.code).to.eql(200));"`
	// шаг УТВЕРЖДАЕТ отказ — негодное имя здесь предмет кейса
	assertRefusal = `"pm.test('rejected', () => pm.expect(pm.response.code).to.eql(400));"`
)

func parseShape(t *testing.T, script string) pmCollection {
	t.Helper()
	var c pmCollection
	body := strings.Replace(nameFormShape, "%s", script, 1)
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("фикстура пробы не разбирается — проверять нечем: %v", err)
	}
	return c
}

func TestFixtureNameJudgeCatchesTheDefectAndSparesTheTwin(t *testing.T) {
	t.Run("на фикстуре с именем вне канона — краснеет и называет координату", func(t *testing.T) {
		f, withName, judged, _ := judgeFixtureNames("проба/коллекция.json", parseShape(t, assertSuccess))
		if withName != 1 || judged != 1 {
			t.Fatalf("предпосылка не создана: шагов с именем %d, судимых %d — ожидалось 1 и 1; "+
				"проба не воспроизвела условие, и её зелёное ничего не значило бы", withName, judged)
		}
		if len(f) != 1 {
			t.Fatalf("судья НЕ нашёл негодное имя в фикстуре (находок %d) — он не способен упасть", len(f))
		}
		if !strings.Contains(f[0], "mk") || !strings.Contains(f[0], "QA Region CRUD") {
			t.Errorf("находка не называет ни шаг, ни имя — по ней нельзя починить: %q", f[0])
		}
	})

	t.Run("на шаге, УТВЕРЖДАЮЩЕМ отказ, с тем же именем — молчит", func(t *testing.T) {
		f, withName, judged, _ := judgeFixtureNames("проба/коллекция.json", parseShape(t, assertRefusal))
		if withName != 1 {
			t.Fatalf("предпосылка не создана: шагов с именем %d — ожидался 1", withName)
		}
		if judged != 0 {
			t.Fatalf("шаг, утверждающий отказ, попал под суд (судимых %d) — гейт наказывал бы "+
				"кейсы, чей предмет и есть негодное имя", judged)
		}
		if len(f) != 0 {
			t.Errorf("ложное срабатывание на законном близнеце: %v", f)
		}
	})

	t.Run("каноничное имя не находка — иначе гейт краснел бы на всём", func(t *testing.T) {
		body := strings.Replace(nameFormShape, `QA Region CRUD {{runId}}`, `qa-region-crud-{{runId}}`, 1)
		var c pmCollection
		if err := json.Unmarshal([]byte(strings.Replace(body, "%s", assertSuccess, 1)), &c); err != nil {
			t.Fatalf("фикстура не разбирается: %v", err)
		}
		f, _, judged, _ := judgeFixtureNames("проба/коллекция.json", c)
		if judged != 1 {
			t.Fatalf("положительный контроль не судился (судимых %d) — он ничего не подтверждает", judged)
		}
		if len(f) != 0 {
			t.Errorf("каноничное имя объявлено находкой: %v", f)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Другой референт (задача #1279) — инъекция в обе стороны.
//
// iam мигрировал под канон шестым, и вместе с ним под меру попали шаги, создающие
// РОЛЬ. Имя роли каноном не судится: `roles/vpc.admin` — то, на что ссылаются
// привязки, и подчёркивание в нём законно по его собственной форме. Замер в день
// правки: без освобождения судья давал 50 находок, и ВСЕ 50 были ролями.
//
// Освобождение опасно ровно тем, чем полезно, поэтому проверяется парой: одно и то
// же негодное имя, один и тот же шаг, различие ровно одно — АДРЕС запроса.

// referentShape — шаг создания с именем, негодным по канону и законным по форме
// роли. %s подставляет адрес; всё остальное совпадает побайтово.
const referentShape = `{"item":[{"name":"НФ — другой референт","item":[
      {"name":"mk","request":{"method":"POST","url":{"raw":"%s"},
        "body":{"raw":"{\"name\":\"custom_reader_{{runId}}\"}"}},
        "event":[{"listen":"test","script":{"exec":[` + assertSuccess + `]}}]}
    ]}]}`

func parseReferent(t *testing.T, url string) pmCollection {
	t.Helper()
	var c pmCollection
	if err := json.Unmarshal([]byte(strings.Replace(referentShape, "%s", url, 1)), &c); err != nil {
		t.Fatalf("фикстура пробы не разбирается — проверять нечем: %v", err)
	}
	return c
}

func TestFixtureNameJudgeSparesTheOtherReferentOnly(t *testing.T) {
	t.Run("законный близнец: имя РОЛИ — судья молчит и считает освобождение", func(t *testing.T) {
		f, withName, judged, spared := judgeFixtureNames(
			"проба/коллекция.json", parseReferent(t, "{{baseUrl}}/iam/v1/roles"))
		if withName != 1 {
			t.Fatalf("предпосылка не создана: шагов с именем %d, ожидался 1", withName)
		}
		if len(f) != 0 {
			t.Fatalf("имя роли объявлено находкой (%d): %v — освобождение не действует", len(f), f)
		}
		if judged != 0 {
			t.Errorf("шаг попал в судимые (%d) — освобождение обязано выводить его из-под меры", judged)
		}
		if spared["/iam/v1/roles"] != 1 {
			t.Errorf("освобождение не сосчитано (%v) — перепись не отличит запись с предметом "+
				"от записи, пережившей его", spared)
		}
	})

	t.Run("то же имя по ДРУГОМУ адресу — краснеет и называет координату", func(t *testing.T) {
		// Различие с близнецом ровно одно: адрес. Значит расхождение вердикта
		// нельзя списать на разный вход — а без этой половины освобождение
		// ловило бы форму шага, а не существо его цели.
		f, withName, judged, spared := judgeFixtureNames(
			"проба/коллекция.json", parseReferent(t, "{{baseUrl}}/iam/v1/groups"))
		if withName != 1 || judged != 1 {
			t.Fatalf("предпосылка не создана: шагов с именем %d, судимых %d — ожидалось 1 и 1",
				withName, judged)
		}
		if len(f) != 1 {
			t.Fatalf("негодное имя вне освобождённой цели НЕ найдено (находок %d) — "+
				"освобождение течёт на соседние ресурсы", len(f))
		}
		if !strings.Contains(f[0], "custom_reader_") {
			t.Errorf("находка не называет имя — по ней нельзя починить: %q", f[0])
		}
		if len(spared) != 0 {
			t.Errorf("шаг вне освобождённой цели сосчитан освобождённым: %v", spared)
		}
	})

	t.Run("освобождение действует по ЦЕЛИ, а не по имени шага", func(t *testing.T) {
		// Шаг называется «create-role-…», но бьёт в группы. Предикат по имени шага
		// пропустил бы его — то есть стал бы маской для любой забытой фикстуры,
		// которую так назвали.
		body := strings.Replace(referentShape, "%s", "{{baseUrl}}/iam/v1/groups", 1)
		body = strings.Replace(body, `"name":"mk"`, `"name":"create-role-decoy"`, 1)
		var c pmCollection
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("фикстура пробы не разбирается: %v", err)
		}
		f, _, _, spared := judgeFixtureNames("проба/коллекция.json", c)
		if len(f) != 1 {
			t.Fatalf("шаг с именем «create-role-…», бьющий в ГРУППЫ, освобождён (находок %d) — "+
				"предикат судит прозу, а не цель запроса", len(f))
		}
		if len(spared) != 0 {
			t.Errorf("такой шаг сосчитан освобождённым: %v", spared)
		}
	})
}
