// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция гейта «у прямого факта один производитель» — В ОБЕ СТОРОНЫ.
//
// Гейт обходит дерево, поэтому его разбор здесь воспроизводится над синтетическим
// входом: доказывается, что признак различает ЗАПИСЬ и ЧТЕНИЕ, а не наличие имени
// таблицы. Без этого он ловил бы форму — и краснел бы на самой форме, которая эту
// таблицу читает на каждом вопросе о доступе.

// findFactMutations — тот же признак, что исполняет гейт, вынесенный для инъекции.
func findFactMutations(body string) []string {
	var out []string
	if !strings.Contains(body, factTable) {
		return nil
	}
	upper := strings.ToUpper(body)
	for _, verb := range factMutations {
		if strings.Contains(upper, strings.ToUpper(verb)) {
			out = append(out, verb)
		}
	}
	return out
}

// TestR7_3_27_InjectionRedOnASecondProducer — завели писателя факта из кода →
// признак КРАСНЕЕТ.
func TestR7_3_27_InjectionRedOnASecondProducer(t *testing.T) {
	body := `package pg

const backfill = ` + "`" + `INSERT INTO kaname.relation_fact (object_type, object_id, relation, subject)
	VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING` + "`" + `
`
	got := findFactMutations(body)
	if len(got) == 0 {
		t.Fatal("признак МОЛЧИТ на записи в таблицу прямого факта — гейт не способен " +
			"покраснеть, и его зелёный на дереве ничего не значит")
	}
}

// TestR7_3_27_InjectionSilentOnAReader — ЗАКОННЫЙ БЛИЗНЕЦ: та же таблица, но
// ЧТЕНИЕ. Признак обязан молчать.
//
// Без этой половины гейт краснел бы на самой реляционной форме: она читает эту
// таблицу на КАЖДОМ вопросе о доступе, и первый же ложный срабат его отключил бы.
func TestR7_3_27_InjectionSilentOnAReader(t *testing.T) {
	body := `package relverdict

const groundsSQL = ` + "`" + `SELECT f.relation, f.subject
	  FROM kaname.relation_fact f
	 WHERE f.object_type = $1 AND f.object_id = $2` + "`" + `
`
	if got := findFactMutations(body); len(got) != 0 {
		t.Fatalf("признак краснеет на ЧТЕНИИ прямого факта (%v) — то есть на том, ради "+
			"чего таблица и заведена", got)
	}
}

// TestR7_3_27_InjectionSilentWithoutTheTable — файл, не называющий таблицу вовсе.
func TestR7_3_27_InjectionSilentWithoutTheTable(t *testing.T) {
	if got := findFactMutations("package pg\n\nconst q = `INSERT INTO kaname.accounts (id) VALUES ($1)`\n"); len(got) != 0 {
		t.Fatalf("признак краснеет на записи в ЧУЖУЮ таблицу (%v)", got)
	}
}
