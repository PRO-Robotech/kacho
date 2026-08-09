// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunMatrix is the measurement itself.
//
// It is env-gated, and deliberately so: it starts containers and, at the upper end
// of the curve, writes hundreds of thousands of tuples. `go test ./...` must not
// pay for that by accident. It is NOT skipped for being slow when asked for — a
// benchmark that quietly declines to run is the "not executed reported as green"
// class this tree already has a name for.
//
//	AUTHZFORMBENCH=1 go test ./tools/authzformbench/ -run TestRunMatrix -v -timeout 4h
//
// Overrides (all optional):
//
//	AUTHZFORMBENCH_NS=10,100,1000,10000   the curve
//	AUTHZFORMBENCH_SUBJECTS=10            S
//	AUTHZFORMBENCH_FORMS=A-flat,D-container
//	AUTHZFORMBENCH_WRITE_REPEATS=5
//	AUTHZFORMBENCH_READ_REPEATS=20
//	AUTHZFORMBENCH_OUT=/path/report.txt   (default: stdout only)
func TestRunMatrix(t *testing.T) {
	if os.Getenv("AUTHZFORMBENCH") == "" {
		t.Skip("set AUTHZFORMBENCH=1 to run the measurement (it starts containers and writes many tuples)")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	if v := os.Getenv("AUTHZFORMBENCH_NS"); v != "" {
		cfg.Ns = parseInts(t, v)
	}
	if v := os.Getenv("AUTHZFORMBENCH_SUBJECTS"); v != "" {
		cfg.Subjects = parseInts(t, v)[0]
	}
	if v := os.Getenv("AUTHZFORMBENCH_WRITE_REPEATS"); v != "" {
		cfg.WriteRepeats = parseInts(t, v)[0]
	}
	if v := os.Getenv("AUTHZFORMBENCH_READ_REPEATS"); v != "" {
		cfg.ReadRepeats = parseInts(t, v)[0]
	}
	if v := os.Getenv("AUTHZFORMBENCH_FORMS"); v != "" {
		cfg.Forms = nil
		for _, f := range strings.Split(v, ",") {
			cfg.Forms = append(cfg.Forms, Form(strings.TrimSpace(f)))
		}
	}

	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)

	// The engine's BatchCheck ceiling is MEASURED, once, before anything is timed —
	// the report must print what this engine enforces, not what a comment says.
	probe, err := stack.NewStore(ctx, "cap-probe", r.models[cfg.Forms[0]])
	require.NoError(t, err)
	cap0, err := probe.probeBatchCap(ctx, BenchType, 1, 4096)
	require.NoError(t, err)
	stack.BatchCap = cap0
	require.NoError(t, probe.Delete(ctx))
	t.Logf("measured BatchCheck ceiling: %d (authzfilter.MaxBatchChecksPerRequest = 50)", cap0)
	if cap0 < cfg.Partition {
		t.Logf("engine ceiling %d is BELOW the configured partition %d — lowering the partition; "+
			"the report says which was used", cap0, cfg.Partition)
		cfg.Partition = cap0
		r.Cfg.Partition = cap0
	}

	sum := sha256.Sum256([]byte(canon))
	modelPath, _, _ := ResolveCanonicalModel()
	prov := CollectProvenance(stack, modelPath, hex.EncodeToString(sum[:])[:16])

	var cells []Cell
	for _, f := range cfg.Forms {
		for _, n := range cfg.Ns {
			spare := 1 + cfg.RelabelK
			sc := NewScenario(n, spare, cfg.Subjects, cfg.Role, cfg.Verbs)
			t.Logf("── %s N=%d (grant tuples predicted: %d)", f, n, ExpectedGrantTuples(f, sc))
			cells = append(cells, r.RunWrites(ctx, f, sc)...)
			cells = append(cells, r.RunReads(ctx, f, sc)...)
		}
	}

	var sb strings.Builder
	Report(&sb, prov, r.Notes, cfg, cells)
	fmt.Print(sb.String())
	if out := os.Getenv("AUTHZFORMBENCH_OUT"); out != "" {
		require.NoError(t, os.WriteFile(out, []byte(sb.String()), 0o600))
		t.Logf("report written to %s", out)
	}

	// The run does not fail on a not-run cell — that would hide the rest of the
	// matrix. It fails when NOTHING was measured, because a report of only
	// third-category outcomes must never read as a completed comparison.
	measured := 0
	for _, c := range cells {
		if c.Outcome == Measured {
			measured++
		}
	}
	require.Positive(t, measured, "not a single cell was measured — the report is not a comparison")
	t.Logf("cells: %d measured, %d other", measured, len(cells)-measured)
}

func parseInts(t *testing.T, s string) []int {
	t.Helper()
	var out []int
	for _, p := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		require.NoError(t, err)
		out = append(out, n)
	}
	require.NotEmpty(t, out)
	return out
}
