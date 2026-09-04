// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// nameformcanon_injection_test.go — доказательство общего производного в ОБЕ стороны.
//
// # Что доказывается
//
// Два гейта — суффикса ограничения и имён фикстур — читают ОДИН вывод: какие схемы
// приняли канон формы имени. Прежде каждый выводил это сам, и оба выводили из имени
// файла. Общее производное снимает расхождение, но заводит новый риск: ошибись оно —
// ошибутся оба разом, и одинаково.
//
// Поэтому здесь утверждается ровно то, на чём прежний предикат сломался:
// распознаватель обязан знать КАЖДУЮ законную форму записи предмета, и обязан НЕ
// принимать за неё её же описание.
package repohygiene

import (
	"strings"
	"testing"
)

// nameFormLiteral — та же форма, что стоит в миграциях. Собирается один раз,
// чтобы фикстуры ниже отличались от законного входа ровно одним фактом.
const nameFormLiteral = `'^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$'`

func TestNameFormCanonAdoptions_ProvenByInjection(t *testing.T) {
	// ── ОБЕ ЗАКОННЫЕ ФОРМЫ ЗАПИСИ, каждая подачей ──────────────────────────
	//
	// Форма миграции пишется человеком: форма кладётся в переменную plpgsql.
	// Форма свода печатается pg_dump: то же ограничение стоит материализованным.
	// Формы разные, предмет один; знающий одну из них МОЛЧИТ на другой.
	migrationForm := []byte("DECLARE\n    form text := " + nameFormLiteral + ";\nBEGIN\n")
	dumpForm := []byte("    CONSTRAINT accounts_name_check CHECK ((name ~ " + nameFormLiteral + "::text))\n")

	got := nameFormCanonAdoptionsFrom(map[string][]byte{
		"services/alpha/internal/migrations/715001_resource_name_single_form.sql": migrationForm,
		"services/beta/internal/migrations/0001_initial.sql":                      dumpForm,
	})
	if len(got) != 2 {
		t.Fatalf("узнано схем %d, ожидалось 2 (%+v): форма, распознавателю неизвестная, "+
			"не краснеет и не зеленеет — она МОЛЧИТ, и записанное в ней проходит вне наблюдения", len(got), got)
	}
	if got[0].Service != "alpha" || got[1].Service != "beta" {
		t.Fatalf("схемы названы неверно: %+v", got)
	}
	if got[1].File != "services/beta/internal/migrations/0001_initial.sql" {
		t.Fatalf("координата объявления не названа — находке будет некуда послать читателя: %+v", got[1])
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них отбор зеленел бы на всём подряд ─────────
	for _, c := range []struct {
		what string
		rel  string
		body string
	}{
		{
			// Ровно эта строка стоит в шапке миграции vpc. Предикат по вхождению
			// формы принял бы ОБЪЯСНЕНИЕ за ОБЪЯВЛЕНИЕ.
			what: "форма прозой в комментарии — это описание, а не объявление",
			rel:  "services/gamma/internal/migrations/0007_prose.sql",
			body: "-- RFC 1123, `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`. Пустая строка остаётся\n",
		},
		{
			what: "файл вне каталога миграций сервиса",
			rel:  "docs/architecture/name-form.sql",
			body: "form text := " + nameFormLiteral + ";",
		},
		{
			what: "не .sql",
			rel:  "services/delta/internal/migrations/notes.md",
			body: "form text := " + nameFormLiteral + ";",
		},
		{
			what: "миграция сервиса, канон НЕ принявшего",
			rel:  "services/eps/internal/migrations/0017_quota.sql",
			body: "CREATE TABLE t (name text);\n",
		},
	} {
		if adopted := nameFormCanonAdoptionsFrom(map[string][]byte{c.rel: []byte(c.body)}); len(adopted) != 0 {
			t.Errorf("%s: принято за принятие канона (%+v) — отбор шире своего предмета, "+
				"а гейт с ложными находками снимают первым", c.what, adopted)
		}
	}

	// ── ЕДИНИЦА СЧЁТА — СЕРВИС, а не файл ──────────────────────────────────
	//
	// Прежде считались файлы, и совпадение «по одному на сервис» было
	// случайным: вторая миграция, тронувшая форму, раздула бы перепись, ничего
	// не сообщив о числе схем. Побеждает позднейшая по имени — у goose имя и
	// есть порядок.
	two := nameFormCanonAdoptionsFrom(map[string][]byte{
		"services/alpha/internal/migrations/0001_initial.sql": migrationForm,
		"services/alpha/internal/migrations/0900_later.sql":   dumpForm,
	})
	if len(two) != 1 {
		t.Fatalf("две миграции одного сервиса дали %d записей — единица счёта не сервис, "+
			"и перепись «схем под каноном» считала бы не схемы", len(two))
	}
	if two[0].File != "services/alpha/internal/migrations/0900_later.sql" {
		t.Fatalf("побеждает не позднейшее объявление, а %q — действующим считается отменённое", two[0].File)
	}
}

func TestNameFormConstraintNaming_ProvenByInjection(t *testing.T) {
	a := nameFormAdoption{Service: "alpha", File: "services/alpha/internal/migrations/0001_initial.sql"}
	const suffix = "_name_check"

	// ── ИНЪЕКЦИЯ: суффикс сменён. Отображение ошибки перестанет узнавать полосу ─
	got := adjudicateNameFormConstraintNaming(a, "PERFORM x || '_name_chk';", suffix)
	if len(got) == 0 {
		t.Fatal("смена суффикса не названа: разбор полос опирается на конструкцию имени, " +
			"и его расхождение с миграцией было бы молчаливым")
	}
	for _, want := range []string{"alpha", a.File, suffix} {
		if !strings.Contains(got[0], want) {
			t.Fatalf("находка не называет %q — по ней не видно, что чинить:\n%s", want, got[0])
		}
	}

	// ── ИНЪЕКЦИЯ во второй форме: материализованное ограничение названо иначе ──
	//
	// Она несущая для сведённой схемы. Суффикс может стоять где-то в файле —
	// у другого ограничения, — и «вхождение есть» было бы верным при
	// неправильно названном ограничении, несущем форму.
	mixed := "    CONSTRAINT accounts_name_check CHECK ((name ~ " + nameFormLiteral + "::text)),\n" +
		"    CONSTRAINT groups_label CHECK ((name ~ " + nameFormLiteral + "::text))\n"
	got = adjudicateNameFormConstraintNaming(a, mixed, suffix)
	if len(got) != 1 {
		t.Fatalf("ожидалась 1 находка про groups_label, получено %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "groups_label") {
		t.Fatalf("находка не называет виновное ограничение:\n%s", got[0])
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ ──────────────────────────────────────────────────
	ok := "    CONSTRAINT accounts_name_check CHECK ((name ~ " + nameFormLiteral + "::text))\n"
	if found := adjudicateNameFormConstraintNaming(a, ok, suffix); len(found) != 0 {
		t.Fatalf("ложное срабатывание на верно названном ограничении: %v", found)
	}
	// Неписаная цепь строит имена в рантайме: материализованных вхождений ноль,
	// и эта половина суждения вырождается в тождество. Так и должно быть — её
	// отсутствие находкой не является.
	dynamic := "    form text := " + nameFormLiteral + ";\n    EXECUTE format('... %I_name_check ...', t);\n"
	if found := adjudicateNameFormConstraintNaming(a, dynamic, suffix); len(found) != 0 {
		t.Fatalf("цепь, строящая имена в рантайме, объявлена находкой: %v", found)
	}
}
