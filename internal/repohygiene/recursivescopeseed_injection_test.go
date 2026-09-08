// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// recursivescopeseed_injection_test.go — доказательство, что гейт цепи СПОСОБЕН
// упасть, и что падает он на существе, а не на форме.
//
// Инъекция ведётся В ОБЕ СТОРОНЫ и настоящим входом:
//
//   - отрицательная — SQL, дословно взятый из дерева ДО этого перехода, и
//     промежуточное состояние того же перехода, на котором кривая ЕЩЁ РОСЛА.
//     Второе важнее первого: без него гейт закреплял бы половину класса и
//     зеленел бы ровно на том, что было измерено как всё ещё растущее;
//   - положительная — законные близнецы: сегодняшняя форма перечисления и форма
//     вопроса про один объект, у которой заход не набор вовсе. Без них гейт ловил
//     бы слово «RECURSIVE», а не раскрутку цепи на наборе, и первое же ложное
//     срабатывание сняли бы вместе с запретом.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bt — обратная кавычка. Отдельной константой, потому что тело инъекции само
// пишется сырым литералом, и вложить кавычку в него иначе нельзя.
const bt = "`"

// scopeSeedInjection — одна проба.
type scopeSeedInjection struct {
	name     string
	path     string
	sql      string
	wantFind bool
	origin   string
}

// goFileWith оборачивает SQL в компилируемый файл: гейт разбирает Go, а не текст,
// поэтому вход обязан быть настоящим исходником.
func goFileWith(sql string) string {
	return "package pg\n\nconst q = " + bt + sql + bt + "\n"
}

func TestRecursiveScopeGateFiresOnTheStateThisChangeFixed(t *testing.T) {
	// Заход БЕЗ предела и обычное соединение в рекурсивной ветви — дословная
	// форма перечисления до перехода.
	const beforeBoth = `
WITH RECURSIVE speaker(subject) AS (
    SELECT $1::text
),
candidate(object_id) AS (
    SELECT m.object_id
      FROM kaname.resource_mirror m
     WHERE m.object_type = $2::text
       AND m.object_id > $3::text
),
scope(object_id, s_type, s_id, depth) AS (
    SELECT c.object_id, $2::text, c.object_id, 0 FROM candidate c
  UNION
    SELECT s.object_id, e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      JOIN kaname.resource_parent_edge e
        ON e.object_type = s.s_type AND e.object_id = s.s_id
     WHERE s.depth < $5::int
)
SELECT c.object_id FROM candidate c
 ORDER BY c.object_id
 LIMIT $4::int`

	// ПРОМЕЖУТОЧНОЕ состояние перехода: предел на кандидатах уже стоит, а цепь
	// по-прежнему достаёт рёбра обычным соединением. Измерено: страница из 50
	// объектов читала все рёбра типа, и кривая осталась растущей.
	const beforeSecondHalf = `
WITH RECURSIVE candidate(object_id) AS (
    SELECT m.object_id
      FROM kaname.resource_mirror m
     WHERE m.object_type = $2::text
       AND m.object_id > $3::text
     ORDER BY m.object_id
     LIMIT $4::int
),
scope(object_id, s_type, s_id, depth) AS (
    SELECT c.object_id, $2::text, c.object_id, 0 FROM candidate c
  UNION
    SELECT s.object_id, e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      JOIN kaname.resource_parent_edge e
        ON e.object_type = s.s_type AND e.object_id = s.s_id
     WHERE s.depth < $5::int
)
SELECT c.object_id, true AS allowed FROM candidate c ORDER BY c.object_id`

	// Заход читает таблицу НАПРЯМУЮ, без промежуточного выражения: та же
	// неограниченность, только без имени, за которое можно зацепиться.
	const seedReadsTableDirectly = `
WITH RECURSIVE scope(object_id, s_type, s_id, depth) AS (
    SELECT m.object_id, $2::text, m.object_id, 0
      FROM kaname.resource_mirror m
     WHERE m.object_type = $2::text
  UNION
    SELECT s.object_id, e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      CROSS JOIN LATERAL (
             SELECT pe.parent_type, pe.parent_id
               FROM kaname.resource_parent_edge pe
              WHERE pe.object_type = s.s_type AND pe.object_id = s.s_id
              ORDER BY pe.depth
              LIMIT $5::int
           ) e
     WHERE s.depth < $5::int
)
SELECT s.object_id FROM scope s`

	// Сегодняшняя форма перечисления: предел на кандидатах и соединение вбок с
	// пределом. Законный близнец.
	const afterBoth = `
WITH RECURSIVE candidate(object_id) AS (
    SELECT m.object_id
      FROM kaname.resource_mirror m
     WHERE m.object_type = $2::text
       AND m.object_id > $3::text
     ORDER BY m.object_id
     LIMIT $4::int
),
scope(object_id, s_type, s_id, depth) AS (
    SELECT c.object_id, $2::text, c.object_id, 0 FROM candidate c
  UNION
    SELECT s.object_id, e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      CROSS JOIN LATERAL (
             SELECT pe.parent_type, pe.parent_id
               FROM kaname.resource_parent_edge pe
              WHERE pe.object_type = s.s_type AND pe.object_id = s.s_id
              ORDER BY pe.depth
              LIMIT $5::int
           ) e
     WHERE s.depth < $5::int
)
SELECT c.object_id, true AS allowed FROM candidate c ORDER BY c.object_id`

	// Вопрос про ОДИН объект: заход — строка из доводов запроса, набора нет.
	// Дословная форма вердикта, субъектов и разворота.
	const singleObjectSeed = `
WITH RECURSIVE scope(s_type, s_id, depth) AS (
    SELECT $2::text, $3::text, 0
  UNION
    SELECT e.parent_type, e.parent_id, s.depth + 1
      FROM scope s
      JOIN kaname.resource_parent_edge e
        ON e.object_type = s.s_type AND e.object_id = s.s_id
     WHERE s.depth < $7::int
)
SELECT count(*) FROM scope`

	// Заход из вычисленных значений, без чтения чего бы то ни было: форма
	// материализации списка адресов.
	const computedSeed = `
WITH RECURSIVE ips(ip, stop) AS (
    SELECT (network($2::cidr) + 1)::inet, broadcast($2::cidr)::inet
     WHERE family($2::cidr) = 4
  UNION ALL
    SELECT (ip + 1)::inet, stop FROM ips WHERE ip + 1 < stop
)
SELECT ip FROM ips`

	cases := []scopeSeedInjection{
		{
			name: "инъекция: заход без предела И обычное соединение в цепи",
			path: "services/iam/internal/repo/kaname/pg/relverdict/list.go",
			origin: "дословно из перечисления до перехода: кандидаты берутся без предела, " +
				"предел стоит последним действием",
			sql: beforeBoth, wantFind: true,
		},
		{
			name: "инъекция: предел на кандидатах есть, цепь всё ещё берёт таблицу целиком",
			path: "services/iam/internal/repo/kaname/pg/relverdict/list.go",
			origin: "промежуточное состояние этого же перехода; измерено — кривая осталась " +
				"растущей, страница из 50 объектов читала все рёбра типа",
			sql: beforeSecondHalf, wantFind: true,
		},
		{
			name:   "инъекция: заход читает таблицу напрямую, без промежуточного выражения",
			path:   "services/vpc/internal/repo/kacho/pg/some_list.go",
			origin: "та же неограниченность без имени, за которое можно зацепиться",
			sql:    seedReadsTableDirectly, wantFind: true,
		},
		{
			name:   "законный близнец: сегодняшняя форма перечисления",
			path:   "services/iam/internal/repo/kaname/pg/relverdict/list.go",
			origin: "предел на источнике кандидатов + соединение вбок с пределом",
			sql:    afterBoth, wantFind: false,
		},
		{
			name:   "законный близнец: вопрос про один объект (вердикт, субъекты, разворот)",
			path:   "services/iam/internal/repo/kaname/pg/relverdict/query.go",
			origin: "заход — строка из доводов запроса; набора нет, раскручивать нечего",
			sql:    singleObjectSeed, wantFind: false,
		},
		{
			name:   "законный близнец: заход из вычисленных значений",
			path:   "services/vpc/internal/repo/kacho/pg/address_pool.go",
			origin: "материализация списка адресов: ни одна таблица не читается вовсе",
			sql:    computedSeed, wantFind: false,
		},
	}

	var injections, twins int
	for _, tc := range cases {
		if tc.wantFind {
			injections++
		} else {
			twins++
		}
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			abs := filepath.Join(root, filepath.FromSlash(tc.path))
			if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
				t.Fatalf("подготовка пробы: %v", err)
			}
			if err := os.WriteFile(abs, []byte(goFileWith(tc.sql)), 0o600); err != nil {
				t.Fatalf("подготовка пробы: %v", err)
			}
			// Корни, которых в подготовленном дереве нет, обход обязан пережить:
			// иначе проба падала бы на устройстве фикстуры, а не на предмете.
			for _, sub := range recursiveScopeRoots {
				if err := os.MkdirAll(filepath.Join(root, sub), 0o750); err != nil {
					t.Fatalf("подготовка пробы: %v", err)
				}
			}

			findings, c := collectUnboundedRecursiveScopeSeeds(t, root)
			if c.files == 0 || c.literals == 0 {
				t.Fatalf("предпосылка пробы не выполнена: прочитано файлов %d, литералов %d — "+
					"вердикт относился бы к пустоте, а не к вводу", c.files, c.literals)
			}
			if !tc.wantFind {
				if len(findings) != 0 {
					t.Fatalf("гейт покраснел на ЗАКОННОЙ форме (%s): %+v.\n"+
						"    Он ловит форму, а не существо, и первый же ложный срабат снимет его\n"+
						"    вместе с запретом.", tc.origin, findings)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("гейт смолчал на возвращённом дефекте (%s) — он не способен упасть", tc.origin)
			}
			for _, f := range findings {
				if f.file != tc.path {
					t.Fatalf("гейт назвал не ту координату: %s вместо %s", f.file, tc.path)
				}
				if f.line == 0 {
					t.Fatalf("гейт назвал находку без строки — по такой находке место не найти")
				}
				if !strings.Contains(f.why, "предел") {
					t.Fatalf("находка не называет предмет запрета: %q", f.why)
				}
			}
		})
	}

	t.Logf("перепись: проб %d (инъекций %d, законных близнецов %d)", len(cases), injections, twins)
}
