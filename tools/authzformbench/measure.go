// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Outcome — three categories, never two.
//
// "not-run" is what a benchmark most wants to lose: a shape whose stack did not
// come up, whose operation timed out, whose store refused the request, has NOT
// won. It is carried to the report with its reason and is never averaged, never
// omitted and never counted in anybody's favour.
type Outcome string

const (
	Measured Outcome = "measured"
	Refused  Outcome = "refused" // the engine answered "no" — a fact about the engine
	NotRun   Outcome = "not-run" // nothing was measured; the reason says why
)

// Op names one measured operation.
type Op string

const (
	OpGrant    Op = "W1-grant"         // materialize the binding over N objects
	OpRevoke   Op = "W2-revoke-1"      // withdraw ONE subject
	OpRelabel1 Op = "W3-relabel-1"     // one object enters the set
	OpRelabelK Op = "W4-relabel-K"     // K objects enter the set
	OpCheck    Op = "R1-check"         // one object, one verb, one subject
	OpPage50   Op = "R2-one-partition" // ONE BatchCheck request (size = cfg.Partition)
	OpPageFull Op = "R3-page-full"     // a whole page: cfg.PageSize, partitioned+parallel
	OpVolume   Op = "V-volume"         // tuples and bytes
)

// Cell is one (form, N, op) result.
type Cell struct {
	Form    Form
	N       int
	Op      Op
	Outcome Outcome
	Reason  string

	Repeats            int
	P50, P95, Min, Max float64 // milliseconds; zero when Outcome != Measured

	Requests   int   // store round trips of ONE repeat (writes)
	Tuples     int   // tuples the operation touched
	GrantTotal int64 // volume only: grant tuples in the store
	GrantBytes int64 // volume only: logical row bytes for those tuples
	TableBytes int64 // volume only: whole `tuple` relation, incl. indexes — NOT per-shape
}

// Stats fills the percentile fields from raw millisecond samples.
func (c *Cell) Stats(ms []float64) {
	if len(ms) == 0 {
		c.Outcome = NotRun
		if c.Reason == "" {
			c.Reason = "zero samples"
		}
		return
	}
	s := append([]float64(nil), ms...)
	sort.Float64s(s)
	c.Repeats = len(s)
	c.Min, c.Max = s[0], s[len(s)-1]
	c.P50 = pct(s, 0.50)
	c.P95 = pct(s, 0.95)
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// Config governs one run.
type Config struct {
	Ns           []int // the curve — at least four orders, or the slope is unknown
	Subjects     int   // S
	Verbs        []string
	Role         string
	RelabelK     int // K for W4
	PageSize     int // R3 page size (the contract page is 1000)
	Partition    int // objects per BatchCheck request
	Parallelism  int // partitions in flight
	WriteRepeats int // measured repeats for write ops (warm-up is extra)
	ReadRepeats  int // measured samples for read ops (warm-up is extra)
	Forms        []Form
}

// DefaultConfig mirrors the production numbers wherever one exists: the partition
// and parallelism are authzfilter's (50 / 8) and the page is the contract's max
// (pkg/validate.MaxPageSize = 1000). Where no production number exists — repeats —
// the value is the minimum the methodology demands, not a comfortable one.
func DefaultConfig() Config {
	return Config{
		Ns:           []int{10, 100, 1000, 10000},
		Subjects:     10,
		Verbs:        DefaultVerbs(),
		Role:         "editor",
		RelabelK:     100,
		PageSize:     1000,
		Partition:    50,
		Parallelism:  8,
		WriteRepeats: 5,
		ReadRepeats:  20,
		Forms:        AllForms,
	}
}

// Provenance is what makes a number evidence rather than an anecdote.
type Provenance struct {
	When        string
	TreeRev     string
	Machine     string
	OpenFGA     string
	Postgres    string
	CLI         string
	BatchCap    int // MEASURED off the engine, not assumed
	ModelPath   string
	ModelDigest string
}

func CollectProvenance(st *Stack, modelPath string, modelDigest string) Provenance {
	rev := "unknown"
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil { // #nosec G204 -- fixed argv
		rev = strings.TrimSpace(string(out))
	}
	host, _ := os.Hostname()
	var mem string
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "MemTotal:") {
				mem = strings.TrimSpace(strings.TrimPrefix(ln, "MemTotal:"))
				break
			}
		}
	}
	return Provenance{
		When:        time.Now().Format(time.RFC3339),
		TreeRev:     rev,
		Machine:     fmt.Sprintf("%s %s/%s %d cpu, MemTotal %s", host, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), mem),
		OpenFGA:     st.OpenFGA,
		Postgres:    st.Postgres,
		CLI:         envOr("AUTHZFORMBENCH_CLI_IMAGE", defaultCLIImage),
		BatchCap:    st.BatchCap,
		ModelPath:   modelPath,
		ModelDigest: modelDigest,
	}
}

// classify turns an error into an outcome. A refusal by the engine and a failure to
// reach it are different facts and are kept apart; folding them would let a shape
// that the engine REJECTS look like a shape the harness merely could not reach.
func classify(err error) (Outcome, string) {
	if err == nil {
		return Measured, ""
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return Refused, apiErr.Error()
	}
	return NotRun, err.Error()
}

// Runner executes the matrix.
type Runner struct {
	Stack *Stack
	Cfg   Config
	Canon string

	models map[Form][]byte
	Notes  map[Form]string
}

func NewRunner(st *Stack, cfg Config, canon string) (*Runner, error) {
	r := &Runner{Stack: st, Cfg: cfg, Canon: canon,
		models: map[Form][]byte{}, Notes: map[Form]string{}}
	for _, f := range cfg.Forms {
		dsl, note, err := ModelFor(f, canon)
		if err != nil {
			return nil, fmt.Errorf("model for %s: %w", f, err)
		}
		js, err := TransformDSL(dsl)
		if err != nil {
			return nil, fmt.Errorf("transform model for %s: %w", f, err)
		}
		r.models[f] = js
		r.Notes[f] = note
	}
	return r, nil
}

// NewSeededStore builds a store for `f`, seeds the structural tuples and (when
// `grant`) the shape's grant tuples. Returned ready to be asked questions.
func (r *Runner) NewSeededStore(ctx context.Context, f Form, sc Scenario, grant bool, name string) (*Store, error) {
	st, err := r.Stack.NewStore(ctx, name, r.models[f])
	if err != nil {
		return nil, err
	}
	if _, err := st.WriteTuples(ctx, sc.Structural()); err != nil {
		_ = st.Delete(ctx)
		return nil, fmt.Errorf("seed structural: %w", err)
	}
	if grant {
		if _, err := st.WriteTuples(ctx, Grant(f, sc)); err != nil {
			_ = st.Delete(ctx)
			return nil, fmt.Errorf("seed grant: %w", err)
		}
	}
	return st, nil
}

// RunWrites measures W1..W4 and the volume for one (form, N).
//
// Each repeat gets a FRESH store: a write measured against a store that already
// carries the tuples is measuring an idempotent no-op, which is a different
// operation wearing the same name.
func (r *Runner) RunWrites(ctx context.Context, f Form, sc Scenario) []Cell {
	cells := map[Op]*Cell{}
	samples := map[Op][]float64{}
	for _, op := range []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpVolume} {
		cells[op] = &Cell{Form: f, N: sc.N, Op: op, Outcome: Measured}
	}

	spares := make([]string, 0, sc.Spare)
	for i := sc.N; i < sc.N+sc.Spare; i++ {
		spares = append(spares, sc.Object(i))
	}
	relabelOne := spares[0]
	relabelK := spares[1:min(1+r.Cfg.RelabelK, len(spares))]

	fail := func(op Op, err error) {
		o, reason := classify(err)
		c := cells[op]
		c.Outcome, c.Reason = o, reason
	}

	total := r.Cfg.WriteRepeats + 1 // repeat 0 is warm-up and is discarded
	for rep := 0; rep < total; rep++ {
		st, err := r.NewSeededStore(ctx, f, sc, false, fmt.Sprintf("w-%s-n%d-r%d", f, sc.N, rep))
		if err != nil {
			for _, op := range []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpVolume} {
				if cells[op].Outcome == Measured {
					fail(op, err)
				}
			}
			return finish(cells, samples, rep > 0)
		}

		grant := Grant(f, sc)
		d, reqs, err := timed(func() (int, error) { return st.WriteTuples(ctx, grant) })
		if err != nil {
			fail(OpGrant, err)
		} else if rep > 0 {
			samples[OpGrant] = append(samples[OpGrant], d)
			cells[OpGrant].Requests, cells[OpGrant].Tuples = reqs, len(grant)
		}

		if rep == total-1 && cells[OpGrant].Outcome == Measured && cells[OpVolume].Outcome == Measured {
			// Volume is read HERE — after the grant and BEFORE the relabel and revoke
			// operations — because those mutate the store, and a count taken after them
			// is the count of a different thing.
			//
			// It was taken at the end of the repeat in the first version of this file,
			// and the guard in TestHarnessDiscriminates* caught it: at N=20, M=4, S=5 the
			// shape predicts 400 tuples and the store held 440 (+20 relabel-1, +100
			// relabel-K, −80 revoke). Recorded here rather than quietly corrected: a
			// volume figure is the one number in this report that looks unimpeachable,
			// so the fact that it was wrong once — and that only the cross-check against
			// the shape's own arithmetic found it — is the reason that cross-check stays.
			cnt, rowBytes, tableBytes, verr := r.Stack.TupleBytes(ctx, st.ID)
			if verr != nil {
				fail(OpVolume, verr)
			} else {
				structural := int64(len(sc.Structural()))
				cells[OpVolume].GrantTotal = cnt - structural
				cells[OpVolume].GrantBytes = rowBytes
				cells[OpVolume].TableBytes = tableBytes
				samples[OpVolume] = []float64{0} // volume has no duration; one sample keeps Stats honest
			}
		}

		if cells[OpGrant].Outcome == Measured {
			t1 := RelabelOne(f, sc, relabelOne)
			d, reqs, err := timed(func() (int, error) { return st.WriteTuples(ctx, t1) })
			if err != nil {
				fail(OpRelabel1, err)
			} else if rep > 0 {
				samples[OpRelabel1] = append(samples[OpRelabel1], d)
				cells[OpRelabel1].Requests, cells[OpRelabel1].Tuples = reqs, len(t1)
			}

			tk := RelabelMany(f, sc, relabelK)
			d, reqs, err = timed(func() (int, error) { return st.WriteTuples(ctx, tk) })
			if err != nil {
				fail(OpRelabelK, err)
			} else if rep > 0 {
				samples[OpRelabelK] = append(samples[OpRelabelK], d)
				cells[OpRelabelK].Requests, cells[OpRelabelK].Tuples = reqs, len(tk)
			}

			rv := RevokeSubject(f, sc, sc.Subjects[0])
			d, reqs, err = timed(func() (int, error) { return st.DeleteTuples(ctx, rv) })
			if err != nil {
				fail(OpRevoke, err)
			} else if rep > 0 {
				samples[OpRevoke] = append(samples[OpRevoke], d)
				cells[OpRevoke].Requests, cells[OpRevoke].Tuples = reqs, len(rv)
			}
		}

		_ = st.Delete(ctx)
	}
	return finish(cells, samples, true)
}

// RunReads measures R1..R3 for one (form, N) against a single seeded store.
func (r *Runner) RunReads(ctx context.Context, f Form, sc Scenario) []Cell {
	cells := map[Op]*Cell{}
	samples := map[Op][]float64{}
	for _, op := range []Op{OpCheck, OpPage50, OpPageFull} {
		cells[op] = &Cell{Form: f, N: sc.N, Op: op, Outcome: Measured}
	}
	st, err := r.NewSeededStore(ctx, f, sc, true, fmt.Sprintf("rd-%s-n%d", f, sc.N))
	if err != nil {
		for _, op := range []Op{OpCheck, OpPage50, OpPageFull} {
			o, reason := classify(err)
			cells[op].Outcome, cells[op].Reason = o, reason
		}
		return finish(cells, samples, false)
	}
	defer func() { _ = st.Delete(ctx) }()

	subj := sc.Subjects[len(sc.Subjects)-1] // the LAST subject: never the one a
	// direct-index shape happens to have written first, so the sample is not
	// accidentally the cheapest row in the store.
	objs := sc.Objects()

	page := func(n int) []string {
		if n > len(objs) {
			n = len(objs)
		}
		return objs[:n]
	}

	// R1 walks the object set with a large prime stride rather than from the front.
	// Sampling objs[0..repeats) would keep asking about the rows a flat shape wrote
	// FIRST, which is the part of the index most likely to be warm — a cheapness
	// that belongs to the sampling, not to the shape.
	pick := func(rep int) string { return objs[(rep*7919)%len(objs)] }

	total := r.Cfg.ReadRepeats + 1
	for rep := 0; rep < total; rep++ {
		d, _, err := timed(func() (int, error) {
			ok, e := st.Check(ctx, subj, "v_get", pick(rep))
			if e == nil && !ok {
				return 0, fmt.Errorf("R1 asked a question the fixture says is ALLOWED and got deny — "+
					"the shape %s is not seeded as claimed", f)
			}
			return 0, e
		})
		if err != nil {
			o, reason := classify(err)
			cells[OpCheck].Outcome, cells[OpCheck].Reason = o, reason
			break
		}
		if rep > 0 {
			samples[OpCheck] = append(samples[OpCheck], d)
		}
	}

	for rep := 0; rep < total; rep++ {
		p := page(r.Cfg.Partition)
		d, _, err := timed(func() (int, error) {
			res, e := st.BatchCheck(ctx, subj, "v_get", p)
			if e == nil {
				e = allMustAllow(f, "R2", res)
			}
			return 0, e
		})
		if err != nil {
			o, reason := classify(err)
			cells[OpPage50].Outcome, cells[OpPage50].Reason = o, reason
			break
		}
		if rep > 0 {
			samples[OpPage50] = append(samples[OpPage50], d)
		}
	}

	for rep := 0; rep < total; rep++ {
		p := page(r.Cfg.PageSize)
		var parts int
		d, _, err := timed(func() (int, error) {
			res, np, e := st.CheckPage(ctx, subj, "v_get", p, r.Cfg.Partition, r.Cfg.Parallelism)
			parts = np
			if e == nil {
				e = allMustAllow(f, "R3", res)
			}
			return np, e
		})
		if err != nil {
			o, reason := classify(err)
			cells[OpPageFull].Outcome, cells[OpPageFull].Reason = o, reason
			break
		}
		if rep > 0 {
			samples[OpPageFull] = append(samples[OpPageFull], d)
			cells[OpPageFull].Requests = parts
			cells[OpPageFull].Tuples = len(p)
		}
	}
	return finish(cells, samples, true)
}

func finish(cells map[Op]*Cell, samples map[Op][]float64, keep bool) []Cell {
	order := []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpCheck, OpPage50, OpPageFull, OpVolume}
	var out []Cell
	for _, op := range order {
		c, ok := cells[op]
		if !ok {
			continue
		}
		if c.Outcome == Measured {
			if !keep {
				c.Outcome, c.Reason = NotRun, orDefault(c.Reason, "no repeat completed")
			} else {
				c.Stats(samples[op])
			}
		}
		out = append(out, *c)
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// allMustAllow refuses a page of denials.
//
// Every object in these pages is inside the granted set, so every verdict must be
// allow. Without this the fastest possible result — a shape seeded wrong, answering
// "no" to everything without touching the index — would win the read columns
// outright, and nothing in a duration would give it away.
func allMustAllow(f Form, op string, res []bool) error {
	if len(res) == 0 {
		return fmt.Errorf("%s/%s: empty verdict vector", f, op)
	}
	for i, ok := range res {
		if !ok {
			return fmt.Errorf("%s/%s: object %d of %d was DENIED, but every object of this page is "+
				"inside the granted set — the store is not seeded as the shape claims, and its read "+
				"timings would be the timing of a denial", f, op, i, len(res))
		}
	}
	return nil
}

func timed(fn func() (int, error)) (ms float64, n int, err error) {
	t0 := time.Now()
	n, err = fn()
	return float64(time.Since(t0).Microseconds()) / 1000.0, n, err
}
