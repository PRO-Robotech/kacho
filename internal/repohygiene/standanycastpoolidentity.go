// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
)

// Разбор ДВУХ объявлений одной личности пула аникаста, вынесенный из гейта,
// чтобы проба инъекции могла подать сюда синтетический вход и доказать, что
// сравнение умеет и краснеть, и молчать. Гейт читает дерево; здесь — чистая
// функция без файловой системы.

// StandPoolIdentity — личность зоне-независимого пула EXTERNAL_PUBLIC «по
// умолчанию»: имя, по которому его опознают, и оба объявленных блока.
type StandPoolIdentity struct {
	Name string
	V4   string
	V6   string
}

var (
	standPoolSQLHeadRe   = regexp.MustCompile(`(?s)VALUES\s*\(\s*'([^']+)',\s*'([^']+)',\s*'([^']+)'`)
	standPoolSQLBlockRe  = regexp.MustCompile(`ARRAY\['([^']+)'\]::text\[\]`)
	standPoolShellVarFmt = `(?m)^%s="([^"]+)"`
)

// ParseStandPoolIdentityFromSQL читает личность из посева стенда
// (deploy/scripts/vpc-address-pool-baseline.sql): первые три строковых литерала
// строки VALUES — идентификатор, имя, описание; два ARRAY-литерала — блоки.
func ParseStandPoolIdentityFromSQL(body string) (StandPoolIdentity, error) {
	head := standPoolSQLHeadRe.FindStringSubmatch(body)
	if len(head) != 4 {
		return StandPoolIdentity{}, fmt.Errorf("не разобрана строка личности пула (id/имя/описание)")
	}
	blocks := standPoolSQLBlockRe.FindAllStringSubmatch(body, -1)
	if len(blocks) != 2 {
		return StandPoolIdentity{}, fmt.Errorf("ожидались ровно два объявленных блока (v4 и v6), разобрано %d", len(blocks))
	}
	return StandPoolIdentity{Name: head[2], V4: blocks[0][1], V6: blocks[1][1]}, nil
}

// ParseStandPoolIdentityFromSeeder читает ту же личность из посева набора nlb
// (deploy/scripts/seed-nlb-fixtures.sh §3.6) — три переменные оболочки.
func ParseStandPoolIdentityFromSeeder(body string) (StandPoolIdentity, error) {
	get := func(name string) (string, error) {
		m := regexp.MustCompile(fmt.Sprintf(standPoolShellVarFmt, name)).FindStringSubmatch(body)
		if len(m) != 2 {
			return "", fmt.Errorf("не разобрано объявление %s", name)
		}
		return m[1], nil
	}
	var out StandPoolIdentity
	var err error
	if out.Name, err = get("ANY_POOL_NAME"); err != nil {
		return StandPoolIdentity{}, err
	}
	if out.V4, err = get("ANY_POOL_V4"); err != nil {
		return StandPoolIdentity{}, err
	}
	if out.V6, err = get("ANY_POOL_V6"); err != nil {
		return StandPoolIdentity{}, err
	}
	return out, nil
}

// DiffStandPoolIdentity возвращает перечень расхождений — по одной строке на
// разошедшуюся величину. Пустой перечень означает согласие ОБОИХ объявлений по
// ВСЕМ трём величинам, а не «сравнивать было нечего»: разбор обеих сторон
// обязан пройти до вызова.
func DiffStandPoolIdentity(fromSQL, fromSeeder StandPoolIdentity) []string {
	var out []string
	for _, c := range []struct{ what, a, b string }{
		{"имя пула", fromSQL.Name, fromSeeder.Name},
		{"блок v4", fromSQL.V4, fromSeeder.V4},
		{"блок v6", fromSQL.V6, fromSeeder.V6},
	} {
		if c.a != c.b {
			out = append(out, fmt.Sprintf("%s: посев стенда объявляет %q, посев набора nlb — %q", c.what, c.a, c.b))
		}
	}
	return out
}
