// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keyalgorithmdictionary_injection_test.go — доказательство того, что
// TestKeyAlgorithmDictionaryMatchesTheCode СПОСОБЕН упасть, и падает он на
// существе.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanKeyAlgorithmConstraints) и то же
// разделение значений (SplitAlgorithmValues), что и гейт.
//
// Вторая сторона пары обязательна: пустое значение — ЗАКОННЫЙ вход схемы, и
// гейт, считающий его алгоритмом, краснел бы на исправном дереве, а гейт,
// молчащий на его пропаже, пропустил бы исчезновение целого вида клиента.
package repohygiene

import (
	"testing"
)

// algInjectedExtraValue — словарь схемы ШИРЕ перечня кода.
//
// Расширять проще, чем сужать, поэтому расхождение и заводится в эту сторону:
// строка со значением, которого проверяющий не признаёт, вставляется без
// возражений, а клиент, ею заведённый, аутентифицироваться не может.
const algInjectedExtraValue = `-- +goose Up
CREATE TABLE kacho_iam.user_oauth_clients (
    key_algorithm text DEFAULT ''::text NOT NULL,
    CONSTRAINT user_oauth_clients_key_algorithm_check CHECK ((key_algorithm IN ('', 'ES256', 'RS256', 'EdDSA', 'HS256')))
);
-- +goose Down
DROP TABLE kacho_iam.user_oauth_clients;
`

// algInjectedLawful — словарь схемы, совпадающий с перечнем кода.
//
// Соседи, каждый со своим способом обмануть разбор:
//
//   - комментарий, называющий алгоритм, которого в словаре нет: разбор читает
//     ИСПОЛНЯЕМУЮ часть, а не текст, и предикат по подстроке краснел бы на
//     собственном объяснении;
//   - секция отката, объявляющая ДРУГОЙ словарь: она не применяется накатом, и
//     читать её значило бы судить схему по тому, чего в ней нет;
//   - второй столбец с похожим именем и своим словарём.
const algInjectedLawful = `-- +goose Up
CREATE TABLE kacho_iam.user_oauth_clients (
    -- Словарь намеренно не содержит HS256: симметричное семейство здесь
    -- неприменимо, и объяснение это лежит в комментарии, а не в ограничении.
    key_algorithm text DEFAULT ''::text NOT NULL,
    key_algorithm_legacy text DEFAULT ''::text NOT NULL,
    CONSTRAINT user_oauth_clients_key_algorithm_check CHECK ((key_algorithm IN ('', 'ES256', 'RS256', 'EdDSA'))),
    CONSTRAINT user_oauth_clients_key_algorithm_legacy_check CHECK ((key_algorithm_legacy IN ('', 'RS1')))
);
-- +goose Down
ALTER TABLE kacho_iam.user_oauth_clients
    ADD CONSTRAINT user_oauth_clients_key_algorithm_check CHECK ((key_algorithm IN ('', 'HS256')));
`

// algInjectedNoEmpty — словарь, из которого пропало пустое значение.
const algInjectedNoEmpty = `-- +goose Up
ALTER TABLE kacho_iam.user_oauth_clients
    ADD CONSTRAINT user_oauth_clients_key_algorithm_check CHECK ((key_algorithm IN ('ES256', 'RS256', 'EdDSA')));
`

// algInjectedRedeclared — ограничение снято и объявлено заново.
const algInjectedRedeclared = `-- +goose Up
ALTER TABLE kacho_iam.user_oauth_clients
    DROP CONSTRAINT user_oauth_clients_key_algorithm_check;
ALTER TABLE kacho_iam.user_oauth_clients
    ADD CONSTRAINT user_oauth_clients_key_algorithm_check CHECK ((key_algorithm IN ('', 'ES256')));
`

// TestAlgorithmScannerFindsAWiderDictionary — сторона (а): словарь шире кода
// становится находкой, и находка несёт координату.
func TestAlgorithmScannerFindsAWiderDictionary(t *testing.T) {
	found, dropped, census := ScanKeyAlgorithmConstraints(
		"synthetic/0046_user_oauth_clients.sql", algInjectedExtraValue, keyAlgorithmColumn)
	if census.Statements != 1 || len(found) != 1 {
		t.Fatalf("объявлений найдено %d (перепись %d), ожидалось 1: %+v",
			len(found), census.Statements, found)
	}
	if len(dropped) != 0 {
		t.Fatalf("снятий найдено %d, ожидалось 0: %v", len(dropped), dropped)
	}
	c := found[0]
	if c.File == "" || c.Line == 0 {
		t.Errorf("находка без координаты — по такому отказу нечего чинить: %+v", c)
	}
	if c.Name != "user_oauth_clients_key_algorithm_check" {
		t.Errorf("находка не называет ограничение: %+v", c)
	}

	algorithms, hasEmpty := SplitAlgorithmValues(c.Values)
	if !hasEmpty {
		t.Errorf("пустое значение синтетики потеряно разбором: %+v", c.Values)
	}
	extra := setDifference(algorithms, []string{"ES256", "EdDSA", "RS256"})
	if len(extra) != 1 || extra[0] != "HS256" {
		t.Fatalf("расхождение со словарём кода вычислено как %v, ожидалось [HS256] — гейт "+
			"на этом дефекте остался бы зелёным", extra)
	}
}

// TestAlgorithmScannerIsSilentOnAMatchingDictionary — сторона (б).
func TestAlgorithmScannerIsSilentOnAMatchingDictionary(t *testing.T) {
	found, _, census := ScanKeyAlgorithmConstraints(
		"synthetic/0046_user_oauth_clients.sql", algInjectedLawful, keyAlgorithmColumn)
	if census.Statements == 0 {
		t.Fatalf("осмотрено ноль объявлений — разбирается не то дерево")
	}
	// Секция отката объявляет ДРУГОЙ словарь и накатом не применяется: разбор,
	// читающий её, объявил бы находку там, где схема исправна.
	if len(found) != 1 {
		t.Fatalf("объявлений найдено %d, ожидалось 1: %+v.\n\nЛибо прочитана секция отката "+
			"— тогда гейт судит схему по тому, чего в ней нет; либо чужой столбец с похожим "+
			"именем принят за наш.", len(found), found)
	}
	c := found[0]
	algorithms, hasEmpty := SplitAlgorithmValues(c.Values)
	if !hasEmpty {
		t.Fatalf("пустое значение не опознано (%v) — гейт объявил бы находкой исправную схему",
			c.Values)
	}
	if len(setDifference(algorithms, []string{"ES256", "EdDSA", "RS256"})) != 0 ||
		len(setDifference([]string{"ES256", "EdDSA", "RS256"}, algorithms)) != 0 {
		t.Fatalf("словарь разобран как %v, ожидалось совпадение с кодом — либо комментарий "+
			"прочитан как исполняемая часть, либо пустое значение сложено с алгоритмами",
			algorithms)
	}
}

// TestAlgorithmScannerFindsAMissingEmptyValue — пропажа пустого значения.
//
// Это не «схема стала строже»: на пустом значении стоит целый вид клиента,
// заведённый без ключевого материала.
func TestAlgorithmScannerFindsAMissingEmptyValue(t *testing.T) {
	found, _, _ := ScanKeyAlgorithmConstraints(
		"synthetic/0900_narrow.sql", algInjectedNoEmpty, keyAlgorithmColumn)
	if len(found) != 1 {
		t.Fatalf("объявлений найдено %d, ожидалось 1: %+v", len(found), found)
	}
	algorithms, hasEmpty := SplitAlgorithmValues(found[0].Values)
	if hasEmpty {
		t.Fatalf("пустое значение найдено там, где его нет (%v) — гейт молчал бы на "+
			"исчезновении целого вида клиента", found[0].Values)
	}
	// И при этом сами алгоритмы с кодом совпадают: без отдельной проверки на
	// пустое значение находки не было бы вовсе.
	if len(setDifference(algorithms, []string{"ES256", "EdDSA", "RS256"})) != 0 {
		t.Fatalf("синтетика подобрана неверно: алгоритмы %v расходятся с кодом, и находка "+
			"пришла бы не от пропажи пустого значения", algorithms)
	}
}

// TestAlgorithmScannerReadsTheLastDeclaration — действует ПОСЛЕДНЕЕ объявление,
// а снятое не действует вовсе.
//
// Применённая миграция не правится (ban #5), поэтому словарь меняют новой; гейт,
// читающий первое объявление, стерёг бы словарь, которого в схеме давно нет.
func TestAlgorithmScannerReadsTheLastDeclaration(t *testing.T) {
	found, dropped, census := ScanKeyAlgorithmConstraints(
		"synthetic/0900_redeclare.sql", algInjectedRedeclared, keyAlgorithmColumn)
	if census.Drops != 1 || len(dropped) != 1 {
		t.Fatalf("снятий найдено %d (перепись %d), ожидалось 1: %v",
			len(dropped), census.Drops, dropped)
	}
	if dropped[0] != "user_oauth_clients_key_algorithm_check" {
		t.Fatalf("снятие названо неверно: %q", dropped[0])
	}
	if len(found) != 1 {
		t.Fatalf("объявлений найдено %d, ожидалось 1: %+v", len(found), found)
	}
	algorithms, _ := SplitAlgorithmValues(found[0].Values)
	if len(algorithms) != 1 || algorithms[0] != "ES256" {
		t.Fatalf("прочитано не последнее объявление: %v", algorithms)
	}
}
