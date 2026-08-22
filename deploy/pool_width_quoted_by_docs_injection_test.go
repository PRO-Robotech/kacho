// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"strings"
	"testing"
)

// TestPoolWidthQuotedGate_ProvenByInjection — гейт цитируемой ширины пула
// способен упасть, смолчать и различить два вида цитаты.
//
// Разбор ведётся над СИНТЕТИЧЕСКИМ входом, поэтому проба не трогает дерево и
// не зависит от того, сходятся ли сегодняшние страницы: гейт, доказанный
// только зелёным деревом, доказан не был.
func TestPoolWidthQuotedGate_ProvenByInjection(t *testing.T) {
	const chart = 40

	cases := []struct {
		name string
		line string
		want string // "находка" | "молчит" | "таблица"
	}{
		{
			name: "пример с ЧУЖИМ числом — находка",
			line: "        max-conns: 200                       # размер pgxpool",
			want: "находка",
		},
		{
			name: "законный близнец: тот же пример с ВЕРНЫМ числом — молчит",
			line: "        max-conns: 40                        # размер pgxpool",
			want: "молчит",
		},
		{
			name: "ключ чарта в примере значений — сверяется так же",
			line: "      maxConns: 41",
			want: "находка",
		},
		{
			name: "ячейка таблицы умолчаний — НЕ сверяется",
			// Число здесь описывает умолчание САМОГО ключа (не задан ⇒ 0),
			// а не нашу посадку. Требовать от него величины чарта значило бы
			// требовать неправды — поэтому такая строка уходит в перепись.
			line: "    <tr><td><code>repository.postgres.max-conns</code></td><td><code>0</code></td><td>Ширина pgx-пула</td></tr>",
			want: "таблица",
		},
		{
			name: "упоминание ключа БЕЗ числа — не цитата",
			line: "Ширина пула задаётся ключом `repository.postgres.maxConns`.",
			want: "молчит",
		},
	}

	var examples, tables int
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Зовём ТУ ЖЕ функцию, что исполняет гейт, а не её копию.
			got := adjudicatePoolWidth(c.line, chart)
			if got != c.want {
				t.Fatalf("вердикт %q, ожидался %q (строка: %s)", got, c.want, strings.TrimSpace(c.line))
			}
			switch got {
			case "таблица":
				tables++
			case "находка":
				examples++
			}
		})
	}

	t.Logf("перепись инъекции: случаев %d; распознано находок %d; ячеек таблиц %d", len(cases), examples, tables)
	if examples == 0 || tables == 0 {
		t.Fatal("инъекция не покрыла оба вида цитаты — доказательство неполно")
	}
}
