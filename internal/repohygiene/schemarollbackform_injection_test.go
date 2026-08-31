// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schemarollbackform_injection_test.go — доказательство того, что гейт СПОСОБЕН
// упасть и способен смолчать.
//
// Инъекция подаётся ТОМУ ЖЕ разбору, который судит настоящее дерево
// ([findSchemaRollbackFindings]), поэтому фикстура не может оказаться
// снисходительнее продукта.
//
// Утверждения парные по КАЖДОЙ судимой форме отдельно: одна инъекция «на все
// формы разом» доказала бы, что гейт краснеет, и не доказала бы, что краснеет
// именно на этой форме, — молчание одной было бы неотличимо от её отсутствия
// в перечне.
package repohygiene

import (
	"strings"
	"testing"
)

func upMigration(body string) string {
	return "-- +goose Up\n" + body + "\n-- +goose Down\nSELECT 1;\n"
}

const injRel = "services/vpc/internal/migrations/9001_injected.sql"

func findingsFor(t *testing.T, body string, baseline map[schemaRollbackKey]int) (schemaRollbackCensus, []schemaRollbackFinding) {
	t.Helper()
	if baseline == nil {
		baseline = map[schemaRollbackKey]int{}
	}
	return findSchemaRollbackFindings([]schemaRollbackSource{{Rel: injRel, Body: body}}, baseline)
}

// TestInjectedColumnRemovalIsAFinding — по одной инъекции на КАЖДУЮ форму.
// Красное обязано называть координату: находка без неё не действие.
func TestInjectedColumnRemovalIsAFinding(t *testing.T) {
	cases := []struct {
		form string
		sql  string
	}{
		{"DROP COLUMN", "ALTER TABLE kacho_vpc.networks DROP COLUMN vrf_id;"},
		{"RENAME COLUMN", "ALTER TABLE kacho_vpc.networks RENAME COLUMN vrf_id TO transport_id;"},
		{"SET NOT NULL", "ALTER TABLE kacho_vpc.networks ALTER COLUMN vrf_id SET NOT NULL;"},
	}
	for _, c := range cases {
		t.Run(c.form, func(t *testing.T) {
			census, got := findingsFor(t, upMigration(c.sql), nil)
			if census.WithForm != 1 {
				t.Fatalf("ПРЕДПОСЫЛКА: инъекция не распознана как отнимающая колонку (WithForm=%d)", census.WithForm)
			}
			if len(got) != 1 {
				t.Fatalf("форма %s: находок %d, ожидалась одна — гейт её не видит", c.form, len(got))
			}
			msg := got[0].String()
			if !strings.Contains(msg, injRel) {
				t.Errorf("находка не называет координату: %s", msg)
			}
			if !strings.Contains(msg, c.form) {
				t.Errorf("находка не называет форму %s: %s", c.form, msg)
			}
			if !strings.Contains(msg, pointOfNoReturnMarker) {
				t.Errorf("находка не называет, чем объявлять решение: %s", msg)
			}
		})
	}
}

// TestLegitimateTwinsStaySilent — законные близнецы. Без них гейт ловил бы
// форму записи, а не существо, и первый же ложный срабат его отключил бы.
func TestLegitimateTwinsStaySilent(t *testing.T) {
	twins := []struct {
		name string
		body string
		why  string
	}{
		{
			"признак в самой миграции",
			upMigration("-- +kacho point-of-no-return: колонка vrf_id снята, прежний образ её выбирает\nALTER TABLE kacho_vpc.networks DROP COLUMN vrf_id;"),
			"решение записано у предмета — это и есть законная форма объявления",
		},
		{
			"снятие колонки в секции Down",
			"-- +goose Up\nALTER TABLE kacho_vpc.networks ADD COLUMN vrf_id text;\n-- +goose Down\nALTER TABLE kacho_vpc.networks DROP COLUMN vrf_id;\n",
			"Down — это и есть откат; судится только Up",
		},
		{
			"снятие ограничения, индекса, триггера",
			upMigration("ALTER TABLE kacho_vpc.networks DROP CONSTRAINT networks_vrf_chk;\nDROP INDEX kacho_vpc.networks_vrf_idx;\nDROP TRIGGER t ON kacho_vpc.networks;"),
			"прежний образ читает и пишет КОЛОНКИ; снятое его обращений к ним не отменяет",
		},
		{
			"снятие ТАБЛИЦЫ — предмет dropguard, не этого гейта",
			upMigration("DROP TABLE kacho_vpc.legacy_routes;"),
			"вторая запись об одном предмете разошлась бы молча",
		},
		{
			"форма названа в комментарии",
			upMigration("-- эта миграция НЕ делает DROP COLUMN, она только добавляет\nALTER TABLE kacho_vpc.networks ADD COLUMN vrf_id text;"),
			"гейт читает исполняемую часть, а не текст",
		},
		{
			"форма названа в строковом литерале",
			upMigration("ALTER TABLE kacho_vpc.networks ADD COLUMN note text DEFAULT 'never DROP COLUMN here';"),
			"литерал не исполняется как SQL",
		},
	}
	for _, tw := range twins {
		t.Run(tw.name, func(t *testing.T) {
			_, got := findingsFor(t, tw.body, nil)
			if len(got) != 0 {
				t.Errorf("ложное срабатывание (%s): %v", tw.why, got[0].String())
			}
		})
	}
}

// TestRubberStampMarkerIsRefused — признак без обоснования признаком не
// является: иначе токен становится печатью, которую ставят не читая.
func TestRubberStampMarkerIsRefused(t *testing.T) {
	body := upMigration(pointOfNoReturnMarker + "\nALTER TABLE kacho_vpc.networks DROP COLUMN vrf_id;")
	if _, got := findingsFor(t, body, nil); len(got) != 1 {
		t.Fatalf("пустое обоснование принято за объявление: находок %d", len(got))
	}
}

// TestBaselineExpiresByItself — исключение, которому нечего исключать, — находка.
func TestBaselineExpiresByItself(t *testing.T) {
	base := map[schemaRollbackKey]int{{File: injRel, Form: "DROP COLUMN"}: 1}
	_, got := findingsFor(t, upMigration("ALTER TABLE kacho_vpc.networks ADD COLUMN vrf_id text;"), base)
	if len(got) != 1 || got[0].Kind != "предмета больше нет" {
		t.Fatalf("запись без предмета не стала находкой: %v", got)
	}
	if !strings.Contains(got[0].String(), injRel) {
		t.Errorf("находка не называет запись: %s", got[0].String())
	}
}

// TestBaselineHoldsAnExactCountNotACeiling — потолок не краснеет никогда и
// потому не истекает; расхождение точного числа обязано быть находкой в ОБЕ
// стороны.
func TestBaselineHoldsAnExactCountNotACeiling(t *testing.T) {
	two := upMigration("ALTER TABLE a DROP COLUMN x;\nALTER TABLE a DROP COLUMN y;")
	for _, declared := range []int{1, 3} {
		base := map[schemaRollbackKey]int{{File: injRel, Form: "DROP COLUMN"}: declared}
		_, got := findingsFor(t, two, base)
		if len(got) != 1 || got[0].Kind != "ведомость разошлась" {
			t.Errorf("объявлено %d, в файле 2 — не находка: %v", declared, got)
		}
	}
	base := map[schemaRollbackKey]int{{File: injRel, Form: "DROP COLUMN"}: 2}
	if _, got := findingsFor(t, two, base); len(got) != 0 {
		t.Errorf("точная запись стала находкой: %v", got)
	}
}

// TestMalformedBaselineLineIsAFinding — ведомость, которую не разобрать, не
// имеет права молча означать «записей нет».
func TestMalformedBaselineLineIsAFinding(t *testing.T) {
	_, bad := parseSchemaRollbackBaseline("a|b\n")
	if len(bad) != 1 {
		t.Errorf("строка без третьего поля принята: %v", bad)
	}
	if _, bad := parseSchemaRollbackBaseline("a|b|нольцелых\n"); len(bad) != 1 {
		t.Errorf("нечисловое вхождение принято: %v", bad)
	}
	ok, bad := parseSchemaRollbackBaseline("# разбор\n\n a | b | 2 \n")
	if len(bad) != 0 || ok[schemaRollbackKey{File: "a", Form: "b"}] != 2 {
		t.Errorf("законная строка не разобрана: ok=%v bad=%v", ok, bad)
	}
}

// TestEmptyWalkIsNotAVerdict — «ноль находок» обязано быть отличимо от «ноль
// прочитанного»: на пустом обходе перепись обязана это показать.
func TestEmptyWalkIsNotAVerdict(t *testing.T) {
	census, got := findSchemaRollbackFindings(nil, map[schemaRollbackKey]int{})
	if census.Files != 0 || census.WithForm != 0 || len(got) != 0 {
		t.Fatalf("пустой вход дал непустую перепись: %s", census)
	}
	// Именно на этом состоянии гейт обязан ронять прогон предпосылкой, а не
	// печатать «находок 0» — утверждение проверяется в самом гейте
	// (census.Files == 0 → t.Fatal).
}

// TestSqlBlankStringsKeepsCodeAndOffsets — близнец существующей пробы
// забеливания комментариев: длина и позиции переводов строк обязаны
// сохраниться, иначе координаты находок поедут.
func TestSqlBlankStringsKeepsCodeAndOffsets(t *testing.T) {
	in := "SELECT 'a\nb', x;\n"
	out := sqlBlankStrings(in)
	if len(out) != len(in) {
		t.Fatalf("длина изменилась: %d != %d", len(out), len(in))
	}
	if strings.Count(out, "\n") != strings.Count(in, "\n") {
		t.Errorf("переводы строк потеряны: %q", out)
	}
	if !strings.Contains(out, "SELECT") || !strings.Contains(out, ", x;") {
		t.Errorf("код забелен вместе с литералом: %q", out)
	}
	if strings.Contains(out, "a\nb") {
		t.Errorf("содержимое литерала уцелело: %q", out)
	}
}
