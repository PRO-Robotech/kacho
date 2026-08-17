// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// Доказательство, что сличение личности пула аникаста умеет И краснеть, И
// молчать. Гейт рядом читает дерево — на согласном дереве он зелен всегда, и
// одного его недостаточно: зелёный гейт неотличим от гейта, который ничего не
// сравнивает. Здесь вход синтетический и подаётся в ОБЕ стороны.

import "testing"

const injSQL = `SET search_path TO kacho_vpc, public;
WITH RECURSIVE
seed(id, name, description, kind, v4, v6) AS (
    VALUES (
        'aplsyntheticidentity',
        'pool-under-test',
        'synthetic',
        1::smallint,
        ARRAY['203.0.113.0/24']::text[],
        ARRAY['2001:db8:ffff::/64']::text[]
    )
)
INSERT INTO vpc_outbox (resource_kind) SELECT 'AddressPool' FROM seed;`

const injSeeder = `#!/usr/bin/env bash
# ANY_POOL_NAME="pool-mentioned-in-a-comment" — упоминание, а не объявление.
ANY_POOL_NAME="pool-under-test"
ANY_POOL_V4="203.0.113.0/24"
ANY_POOL_V6="2001:db8:ffff::/64"
`

// TestStandPoolIdentityDiffIsSilentWhenDeclarationsAgree — законный близнец:
// на согласных объявлениях расхождений нет. Без него всякое «краснеет» ниже
// было бы неотличимо от «краснеет всегда».
func TestStandPoolIdentityDiffIsSilentWhenDeclarationsAgree(t *testing.T) {
	a, err := ParseStandPoolIdentityFromSQL(injSQL)
	if err != nil {
		t.Fatalf("разбор посева стенда отказал на законном входе: %v", err)
	}
	b, err := ParseStandPoolIdentityFromSeeder(injSeeder)
	if err != nil {
		t.Fatalf("разбор посева набора отказал на законном входе: %v", err)
	}
	// Комментарий выше в фикстуре объявляет ДРУГОЕ имя той же переменной:
	// разбор обязан читать объявление, а не текст. Если бы он брал первое
	// совпадение по подстроке, здесь была бы находка на согласном входе.
	if got := DiffStandPoolIdentity(a, b); len(got) != 0 {
		t.Fatalf("на согласных объявлениях найдено %d расхождений: %v", len(got), got)
	}
}

// TestStandPoolIdentityDiffNamesEveryDivergence — каждая из трёх величин,
// разойдясь, обязана быть НАЗВАНА. Гейт, который краснеет одним общим «не
// совпало», не говорит, что чинить.
func TestStandPoolIdentityDiffNamesEveryDivergence(t *testing.T) {
	base, err := ParseStandPoolIdentityFromSQL(injSQL)
	if err != nil {
		t.Fatalf("разбор посева стенда отказал: %v", err)
	}
	for _, c := range []struct {
		what  string
		other StandPoolIdentity
	}{
		{"имя", StandPoolIdentity{Name: "renamed", V4: base.V4, V6: base.V6}},
		{"блок v4", StandPoolIdentity{Name: base.Name, V4: "198.51.100.0/24", V6: base.V6}},
		{"блок v6", StandPoolIdentity{Name: base.Name, V4: base.V4, V6: "2001:db8:1::/64"}},
	} {
		got := DiffStandPoolIdentity(base, c.other)
		if len(got) != 1 {
			t.Errorf("расхождение по «%s» дало %d находок, ожидалась ровно одна: %v", c.what, len(got), got)
		}
	}
}

// TestStandPoolIdentityParsersRefuseUnreadableDeclarations — разбор обязан
// ОТКАЗАТЬ, а не вернуть пустую личность: пустая сравнялась бы сама с собой, и
// гейт зеленел бы на файле, который перестал объявлять предмет.
func TestStandPoolIdentityParsersRefuseUnreadableDeclarations(t *testing.T) {
	for _, body := range []string{"", "-- посев переписан и личности больше не объявляет\n"} {
		if _, err := ParseStandPoolIdentityFromSQL(body); err == nil {
			t.Errorf("разбор посева стенда принял вход без личности (%d байт) — гейт стал бы вакуумным", len(body))
		}
	}
	// Одно объявление есть, двух других нет — этого достаточно для отказа.
	if _, err := ParseStandPoolIdentityFromSeeder("ANY_POOL_NAME=\"x\"\n"); err == nil {
		t.Error("разбор посева набора принял неполное объявление — сверять было бы нечего")
	}
	// И один блок вместо двух — тоже отказ: пул объявляет обе семьи.
	if _, err := ParseStandPoolIdentityFromSQL(`VALUES ( 'a', 'b', 'c', 1::smallint, ARRAY['10.0.0.0/8']::text[] )`); err == nil {
		t.Error("разбор посева стенда принял одно объявление блока вместо двух")
	}
}
