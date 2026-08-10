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
	p("\ncell format  p50 (p95) ms, then round trips / items where both apply\n")
	p("             a page at N below the page size IS the whole set — read the item count\n\n")

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

	for _, op := range []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpCheck, OpPage50, OpPageFull} {
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

	p("── V-volume ── grant tuples / logical row bytes (structural excluded) ────\n")
	p("%-16s", "form")
	for _, n := range nlist {
		p("  N=%-24d", n)
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
				p("  %-26s", txt)
				continue
			}
			p("  %-26s", fmt.Sprintf("%d tup / %dB", c.GrantTotal, c.GrantBytes))
		}
		p("\n")
	}
	p("\n")

	var notRun []Cell
	for _, c := range cells {
		if c.Outcome != Measured {
			notRun = append(notRun, c)
		}
	}
	p("outcomes other than 'measured': %d of %d cells\n", len(notRun), len(cells))
	for _, c := range notRun {
		p("  %-16s N=%-7d %-16s %-9s %s\n", c.Form, c.N, c.Op, c.Outcome, c.Reason)
	}
	if len(notRun) == 0 {
		p("  (none — every cell in the matrix produced a number)\n")
	}
}

func cellText(c Cell) string {
	if c.Outcome != Measured {
		return string(c.Outcome) + ": " + short(c.Reason)
	}
	// The item count travels with the duration on purpose. A page cell without it
	// cannot say whether a form was quick or merely asked about fewer objects — at
	// N below the page size the page IS the whole set, and reading those columns as
	// "a 1000-object page" would be reading a number that was never measured.
	if c.Requests > 0 && c.Tuples > 0 {
		return fmt.Sprintf("%.1f (%.1f) %dr/%di", c.P50, c.P95, c.Requests, c.Tuples)
	}
	if c.Requests > 0 {
		return fmt.Sprintf("%.1f (%.1f) %dr", c.P50, c.P95, c.Requests)
	}
	return fmt.Sprintf("%.1f (%.1f)", c.P50, c.P95)
}

func short(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func key(f Form, n int, op Op) string { return fmt.Sprintf("%s|%d|%s", f, n, op) }
