// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Report renders the matrix as a CURVE — one block per operation, forms down, N
// across — because a table keyed by form alone would hide the only thing that
// matters: whether the lines cross, and where.
//
// Every cell prints its outcome. A "not-run" prints as such WITH its reason and is
// never rendered as a blank or a dash that a reader could mistake for a zero.
func Report(w io.Writer, prov Provenance, notes map[Form]string, cfg Config, cells []Cell) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

	p("authzformbench — comparative measurement of authorization-grant shapes\n")
	p("%s\n", strings.Repeat("=", 78))
	p("when         %s\n", prov.When)
	p("tree         %s\n", prov.TreeRev)
	p("machine      %s\n", prov.Machine)
	p("openfga      %s   (datastore: postgres %s)\n", prov.OpenFGA, prov.Postgres)
	p("форма E      своя БД: postgres %s, СВОЙ контейнер — иначе объём и нагрузка смешались бы\n", prov.RelPostgres)
	p("каскад       %s (глубина %d, объявлена — не подразумевается)\n", prov.CascadeChain, prov.CascadeDepth)
	p("cli          %s\n", prov.CLI)
	p("model        %s (sha256/16 %s)\n", prov.ModelPath, prov.ModelDigest)
	p("batch cap    %d   (MEASURED off the engine, not assumed)\n", prov.BatchCap)
	p("shape        S=%d subjects, M=%d verbs %v, role=%q, K=%d\n",
		cfg.Subjects, len(cfg.Verbs), cfg.Verbs, cfg.Role, cfg.RelabelK)
	p("page         size=%d, partition=%d, parallel=%d\n",
		cfg.PageSize, cfg.Partition, cfg.Parallelism)
	p("repeats      writes=%d reads=%d (+1 discarded warm-up each)\n",
		cfg.WriteRepeats, cfg.ReadRepeats)
	p("\nmodels under test\n")
	for _, f := range cfg.Forms {
		p("  %-16s %s\n", f, notes[f])
	}

	// Арифметика, объявленная ДО прогона, печатается рядом с числами: против неё
	// сверяется измеренное, и ПОСТОЯНСТВО величины — такой же результат, как рост.
	// Ослабить утверждение после прогона нельзя — оно напечатано здесь же.
	p("\nобъявленная до прогона арифметика формы E (против неё сверяется измеренное)\n")
	p("  выдача            %s строк (привязка + субъекты + селектор), ПОСТОЯННА по N\n", "S+2")
	p("  структурное       N + Spare + 2 + M строк — величина, обязанная расти с N\n")
	for _, op := range opsAll {
		if n := ExpectedStatements(FormE, op); n >= 0 {
			p("  q(%-16s) %d\n", op, n)
		}
	}
	p("\ncell format  p50 (p95) ms, затем e=<обращения к движку> q=<SQL-стейтменты> i=<строк намерения>\n")
	p("             колонки обращений РАЗДЕЛЬНЫ и не складываются: e — HTTP-вызовы движка,\n")
	p("             за каждым из которых стоит своё число запросов к его Postgres; q — стейтменты\n")
	p("             a page at N below the page size IS the whole set — read the item count\n\n")

	p("производители величины q (StmtSQL) — по месту снятия, с контролем в обе стороны\n")
	for _, s := range prov.StmtProducers {
		p("  %s\n", s)
	}
	if !allProducersOK(prov.StmtProducers) {
		p("  ВНИМАНИЕ: контроль пройден не везде ⇒ формулировка «на общем для форм уровне» СНЯТА;\n")
		p("  колонка q непрошедшего места не печатается вовсе — ни нулём, ни прочерком\n")
	}
	p("\n")

	ns := map[int]bool{}
	for _, c := range cells {
		ns[c.N] = true
	}
	nlist := make([]int, 0, len(ns))
	for n := range ns {
		nlist = append(nlist, n)
	}
	sort.Ints(nlist)

	idx := map[string]Cell{}
	for _, c := range cells {
		idx[key(c.Form, c.N, c.Op)] = c
	}

	for _, op := range []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpInlineGrant, OpInlineRevoke,
		OpCheck, OpPage50, OpPageFull, OpCascade} {
		p("── %s ──────────────────────────────────────────────\n", op)
		p("%-16s", "form")
		for _, n := range nlist {
			p("  N=%-24d", n)
		}
		p("\n")
		for _, f := range cfg.Forms {
			p("%-16s", f)
			for _, n := range nlist {
				c, ok := idx[key(f, n, op)]
				if !ok {
					p("  %-26s", "not-run: absent")
					continue
				}
				p("  %-26s", cellText(c))
			}
			p("\n")
		}
		p("\n")
	}

	p("── V-volume ── строки выдачи / логические байты ВСЕХ строк хранилища ─────\n")
	p("   структурное (указатели родителя у движка, строки зеркала у формы E) вынесено\n")
	p("   отдельной парой: счёт строк его вычитает, байты — нет, и эта асимметрия базы\n")
	p("   сравнения названа здесь, чтобы её можно было вычесть, а не проглотить молча\n")
	p("%-16s", "form")
	for _, n := range nlist {
		p("  N=%-30d", n)
	}
	p("\n")
	for _, f := range cfg.Forms {
		p("%-16s", f)
		for _, n := range nlist {
			c, ok := idx[key(f, n, OpVolume)]
			if !ok || c.Outcome != Measured {
				txt := "not-run: absent"
				if ok {
					txt = string(c.Outcome) + ": " + short(c.Reason)
				}
				p("  %-32s", txt)
				continue
			}
			p("  %-32s", fmt.Sprintf("%d стр / %dБ (+%d стр структ)",
				c.GrantTotal, c.GrantBytes, c.StructuralRows))
		}
		p("\n")
	}
	p("\n")

	p("── откуда снята каждая ячейка ───────────────────────────────────────────\n")
	p("   матрица собирается из мест с РАЗНЫМИ схемами; таблица без этого признака\n")
	p("   выдавала бы за один прогон два\n")
	for _, f := range cfg.Forms {
		p("  %-16s %s\n", f, placeOf(cells, f))
	}
	p("\n")

	byOutcome := map[Outcome][]Cell{}
	for _, c := range cells {
		byOutcome[c.Outcome] = append(byOutcome[c.Outcome], c)
	}
	p("── четыре категории исхода ячейки ───────────────────────────────────────\n")
	sum := 0
	for _, o := range []Outcome{Measured, Refused, NotRun, NotApplicable} {
		p("  %-16s %d\n", o, len(byOutcome[o]))
		sum += len(byOutcome[o])
	}
	p("  %-16s %d (сумма категорий обязана равняться числу ячеек)\n", "ячеек всего", len(cells))
	if sum != len(cells) {
		p("  ВНИМАНИЕ: сумма категорий %d не сошлась с числом ячеек %d\n", sum, len(cells))
	}

	// Неприменимость по построению печатается СВОЕЙ строкой с причиной. Свернуть
	// её в «не выполнилось» значило бы напечатать «не-измеренных N» и заставить
	// читателя решить, что замер не доехал, — тогда как это самый содержательный
	// результат таблицы.
	p("\nнеприменимо by construction — с причиной по каждой ячейке\n")
	if len(byOutcome[NotApplicable]) == 0 {
		p("  (ни одной — ни у одной формы не нашлось операции, отсутствующей by construction)\n")
	}
	for _, c := range byOutcome[NotApplicable] {
		p("  %-16s N=%-7d %-16s %s\n", c.Form, c.N, c.Op, short(c.Reason))
	}

	var notRun []Cell
	for _, c := range cells {
		if c.Outcome == Refused || c.Outcome == NotRun {
			notRun = append(notRun, c)
		}
	}
	p("\nотказ движка и «не выполнилось»: %d of %d cells\n", len(notRun), len(cells))
	for _, c := range notRun {
		p("  %-16s N=%-7d %-16s %-9s %s\n", c.Form, c.N, c.Op, c.Outcome, c.Reason)
	}
	if len(notRun) == 0 {
		p("  (none — every cell in the matrix produced a number or a by-construction verdict)\n")
	}
}

func allProducersOK(ps []ProducerStatus) bool {
	for _, p := range ps {
		if !p.OK {
			return false
		}
	}
	return len(ps) > 0
}

func placeOf(cells []Cell, f Form) string {
	for _, c := range cells {
		if c.Form == f && c.Place != "" {
			return c.Place
		}
	}
	return "(не названо — ячеек этой формы в матрице нет)"
}

func cellText(c Cell) string {
	if c.Outcome != Measured {
		return string(c.Outcome) + ": " + short(c.Reason)
	}
	// The item count travels with the duration on purpose. A page cell without it
	// cannot say whether a form was quick or merely asked about fewer objects — at
	// N below the page size the page IS the whole set, and reading those columns as
	// "a 1000-object page" would be reading a number that was never measured.
	//
	// Колонки обращений печатаются РАЗДЕЛЬНО и только те, у которых есть предмет:
	// у формы E обращений к движку нет by construction, и ноль в этой колонке
	// читался бы как измеренная величина. Непрошедший контроль производителя даёт
	// не ноль, а прочерк с причиной в блоке производителей.
	out := fmt.Sprintf("%.1f (%.1f)", c.P50, c.P95)
	if c.ReqEngine > 0 {
		out += fmt.Sprintf(" e=%d", c.ReqEngine)
	}
	switch {
	case c.StmtNote != "":
		out += " q=н/д"
	case c.StmtSQL > 0:
		out += fmt.Sprintf(" q=%d", c.StmtSQL)
	}
	if c.Tuples > 0 {
		out += fmt.Sprintf(" i=%d", c.Tuples)
	}
	if c.Parts > 0 {
		out += fmt.Sprintf(" ×%d", c.Parts)
	}
	return out
}

// short режет по РУНАМ, а не по байтам: причина исхода пишется по-русски, и рез
// по байту рвал бы её посередине символа — читатель получил бы «фо<мусор>» и
// решил, что сломан отчёт, а не что строка длинная.
func short(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:57]) + "..."
	}
	return s
}

func key(f Form, n int, op Op) string { return fmt.Sprintf("%s|%d|%s", f, n, op) }
