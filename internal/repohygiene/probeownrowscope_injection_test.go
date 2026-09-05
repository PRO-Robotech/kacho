// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «проба отбирает свои строки положительно»
// СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало (молчание
// бывает от того, что читать не стали):
//
//	отрицательный отбор в запросе     → краснеет, называя координату;
//	тот же отбор ОТДЕЛЬНОЙ константой → краснеет (сканер по целому литералу его
//	                                    не видел — это и была слепая зона);
//	отбор по своим объектам           → молчит, и перепись запрос ЗАСЧИТЫВАЕТ;
//	дельта до/после                   → молчит, запрос засчитан;
//	`NOT IN` в тексте, не уходящем     → молчит: гейт читает то, что уходит в
//	  в базу (фикстура чужого гейта)     базу, а не то, что похоже на SQL;
//	`NOT IN` в комментарии            → молчит: разбор идёт по AST;
//	`not` внутри слова (`cannot in`)  → молчит: слева требуется граница слова.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditProbeOwnRowScope`), что и прогон по
// дереву: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические исходники. Каждый — настоящая форма из этого дерева, а не
// выдумка: каркас взят у `register_resource_integration_test.go` до и после
// починки #510 и у `outboxeventdictionary_test.go` (законный близнец).
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ 1 — историческая форма: отрицательный отбор прямо в запросе.
const synthProbeNegativeInline = `package p

func probe(ctx c, pool P, t T) {
	var n int
	pool.QueryRow(ctx,
		` + "`" + `SELECT count(*) FROM kaname.fga_outbox
		  WHERE payload->>'object' NOT IN ('iam_fgaproxy:system', 'cluster:cluster_kacho_root')` + "`" + `).Scan(&n)
}
`

// ДЕФЕКТ 2 — та же форма, но предикат вынесен в КОНСТАНТУ и приклеен к запросу.
// Сканер по целому строковому литералу такого не видит: ни один литерал не
// является SELECT'ом с `NOT IN` внутри. Ровно так дефект и жил.
const synthProbeNegativeViaConst = `package p

func probe(ctx c, pool P, t T) {
	const notSeed = ` + "`" + `payload->>'object' NOT IN ('iam_fgaproxy:system', 'cluster:cluster_kacho_root')` + "`" + `
	var n int
	pool.QueryRow(ctx,
		` + "`" + `SELECT count(*) FROM kaname.fga_outbox WHERE ` + "`" + `+notSeed).Scan(&n)
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — канон: отбор по объектам, названным самим тестом.
const synthProbePositiveScope = `package p

func probe(ctx c, pool P, t T, own []string) {
	var n int
	pool.QueryRow(ctx,
		` + "`" + `SELECT count(*) FROM kaname.fga_outbox
		  WHERE payload->>'object' = ANY($1::text[])` + "`" + `, own).Scan(&n)
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — дельта: абсолютного числа не утверждается вовсе,
// поэтому посев на вердикт не влияет by construction.
const synthProbeDelta = `package p

func probe(ctx c, pool P, t T) {
	var before, after int
	pool.QueryRow(ctx, ` + "`" + `SELECT count(*) FROM kaname.fga_outbox` + "`" + `).Scan(&before)
	act()
	pool.QueryRow(ctx, ` + "`" + `SELECT count(*) FROM kaname.fga_outbox` + "`" + `).Scan(&after)
	requireEqual(t, before, after)
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — `NOT IN` в тексте, который в базу НЕ уходит: это
// синтетическая фикстура соседнего гейта, разбирающего SQL как данные. Гейт,
// читающий «всё похожее на SQL», покраснел бы здесь — и был бы снят первым же
// автором как непонятный.
const synthProbeFixtureOnly = `package p

var cases = []struct{ expr string }{
	{expr: ` + "`" + `cur_status NOT IN ('INACTIVE','ACTIVE')` + "`" + `},
	{expr: ` + "`" + `SELECT 1 FROM t WHERE k NOT IN ('a','b')` + "`" + `},
}

func check(t T) { _ = cases }
`

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — форма названа в КОММЕНТАРИИ, объясняющем запрет. Гейт по
// сырому тексту покраснел бы на собственном объяснении.
const synthProbeCommentOnly = `package p

// Прежняя редакция отбирала строки как payload->>'object' NOT IN ('seed:a') —
// список стареет молча, поэтому отбор переведён на положительный.
func probe(ctx c, pool P, t T, own []string) {
	var n int
	pool.QueryRow(ctx,
		` + "`" + `SELECT count(*) FROM kaname.fga_outbox WHERE payload->>'object' = ANY($1::text[])` + "`" + `, own).Scan(&n)
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — `not` внутри слова. Без границы слева гейт покраснел бы на
// форме, к отбору отношения не имеющей.
const synthProbeNotInsideWord = `package p

func probe(ctx c, pool P, t T, own []string) {
	var n int
	pool.QueryRow(ctx,
		` + "`" + `SELECT count(*) FROM t WHERE cannot in (SELECT k FROM u) AND obj = ANY($1::text[])` + "`" + `, own).Scan(&n)
}
`

func TestProbeOwnRowScopeGateCutsBothWays(t *testing.T) {
	defects := map[string]string{
		"negative_inline.go":    synthProbeNegativeInline,
		"negative_via_const.go": synthProbeNegativeViaConst,
	}
	legit := map[string]string{
		"positive_scope.go":  synthProbePositiveScope,
		"delta.go":           synthProbeDelta,
		"fixture_only.go":    synthProbeFixtureOnly,
		"comment_only.go":    synthProbeCommentOnly,
		"not_inside_word.go": synthProbeNotInsideWord,
	}

	// ── красная половина: каждый дефект найден, и находка НАЗЫВАЕТ координату ──
	for name, src := range defects {
		census, findings := auditProbeOwnRowScope(map[string]string{name: src})
		if len(findings) == 0 {
			t.Errorf("%s: гейт молчит на дефекте — он не измеряет свойство, ради "+
				"которого написан", name)
			continue
		}
		if findings[0].File != name || findings[0].Line == 0 {
			t.Errorf("%s: находка без годной координаты (%s:%d) — читателю некуда "+
				"идти", name, findings[0].File, findings[0].Line)
		}
		if !strings.Contains(strings.ToUpper(findings[0].Query), "NOT IN") {
			t.Errorf("%s: находка не показывает предъявляемый запрос: %q",
				name, findings[0].Query)
		}
		if census.Queries == 0 {
			t.Errorf("%s: запрос не засчитан переписью — «ноль находок» стало бы "+
				"неотличимо от «ноль прочитанного»", name)
		}
	}

	// ── зелёная половина: молчит, но ЧИТАЕТ ──
	for name, src := range legit {
		census, findings := auditProbeOwnRowScope(map[string]string{name: src})
		if len(findings) != 0 {
			t.Errorf("%s: гейт краснеет на законной форме (%q) — ловит форму, а не "+
				"существо; первый же ложный срабат его отключит",
				name, findings[0].Query)
		}
		if census.Files != 1 {
			t.Errorf("%s: файл не разобран (files=%d) — молчание получено от того, "+
				"что читать не стали", name, census.Files)
		}
	}

	// Отдельно: у близнецов, которые ХОДЯТ в базу, запрос обязан быть засчитан.
	// Без этой сверки «молчит» было бы неотличимо от «не собрало текст запроса».
	for _, name := range []string{"positive_scope.go", "delta.go", "comment_only.go", "not_inside_word.go"} {
		census, _ := auditProbeOwnRowScope(map[string]string{name: legit[name]})
		if census.Queries == 0 {
			t.Errorf("%s: законный запрос не собран (queries=0) — гейт молчит "+
				"потому, что не прочитал", name)
		}
	}

	// Нечитаемый исходник — находка разбора, а не тишина.
	_, broken := auditProbeOwnRowScope(map[string]string{"broken.go": "package p\nfunc ("})
	if len(broken) == 0 {
		t.Error("нечитаемый исходник объявлен чистым — гейт вправе молчать только " +
			"о том, что он прочитал")
	}
}
