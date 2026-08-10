// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMain terminates the shared stack after the package's tests.
func TestMain(m *testing.M) {
	code := m.Run()
	CloseSharedStack()
	os.Exit(code)
}

// bootForTest brings up the stack and returns it with the canonical DSL.
//
// A stack that will not start FAILS; it never skips. Inverting that would report
// "the comparison did not run" as green — the exact third outcome this package
// exists to keep visible.
func bootForTest(ctx context.Context, t *testing.T) (*Stack, string) {
	t.Helper()
	stack, err := SharedStack(ctx)
	require.NoError(t, err, "authzformbench: the measurement stack did not come up")
	path, canon, err := ResolveCanonicalModel()
	require.NoError(t, err)
	require.NotEmpty(t, canon)
	_ = path
	return stack, string(canon)
}

// ── requirement 7: the harness must be shown able to SHOW A DIFFERENCE ─────────

// TestHarnessDiscriminatesOnDeliberatelyDifferentInput is requirement 7.
//
// Before any conclusion is drawn from this harness, it must be proved capable of
// producing DIFFERENT numbers for deliberately different inputs. A harness that
// answers the same on knowingly unequal input measures nothing, and its verdict
// "the shapes are equal" would be an artefact of the instrument rather than a fact
// about the shapes.
//
// The control is a doubling of N against the SHAPE THAT MUST SCALE WITH IT: form A
// materializes N·M·S tuples, so twice the objects is twice the tuples and twice the
// round trips. The assertion is on the round-trip count (exact, deterministic,
// immune to a loaded machine) AND on the direction of the duration; asserting only
// duration would make this proof itself flaky, and asserting only the count would
// prove the arithmetic rather than the instrument.
func TestHarnessDiscriminatesOnDeliberatelyDifferentInput(t *testing.T) {
	if testing.Short() {
		t.Skip("real-OpenFGA proof; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormA}
	cfg.WriteRepeats = 3
	cfg.ReadRepeats = 5
	cfg.RelabelK = 5
	cfg.PageSize = 50
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)

	small := NewScenario(20, 10, 5, "editor", DefaultVerbs())
	large := NewScenario(40, 10, 5, "editor", DefaultVerbs())

	cs := cellsByOp(r.RunWrites(ctx, FormA, small))
	cl := cellsByOp(r.RunWrites(ctx, FormA, large))

	for _, op := range []Op{OpGrant, OpVolume} {
		require.Equalf(t, Measured, cs[op].Outcome, "%s at N=20: %s", op, cs[op].Reason)
		require.Equalf(t, Measured, cl[op].Outcome, "%s at N=40: %s", op, cl[op].Reason)
	}

	// (a) tuple volume doubles — the instrument sees the input it was given
	require.Equal(t, int64(20*4*5), cs[OpVolume].GrantTotal,
		"volume accounting is wrong at N=20; every later number would inherit the error")
	require.Equal(t, int64(40*4*5), cl[OpVolume].GrantTotal)

	// (b) store round trips double — exact and machine-independent
	require.Equal(t, 2*cs[OpGrant].Requests, cl[OpGrant].Requests,
		"doubling N did not double the round trips of the shape whose cost IS N·M·S — "+
			"the harness is not seeing its own input")

	// (c) duration moves in the same direction. A weaker claim than the two above on
	// purpose: it is the noisy signal, so it is asserted as a direction and never as
	// a ratio.
	require.Greaterf(t, cl[OpGrant].P50, cs[OpGrant].P50,
		"twice the work was not slower (N=20 p50=%.1fms, N=40 p50=%.1fms) — "+
			"a harness whose duration does not move on knowingly different input "+
			"cannot be used to say two shapes are equal", cs[OpGrant].P50, cl[OpGrant].P50)

	// (d) and the spread is reported, not collapsed: a p50 with no p95 cannot tell a
	// real difference from noise.
	require.GreaterOrEqual(t, cl[OpGrant].P95, cl[OpGrant].P50)
	require.GreaterOrEqual(t, cs[OpGrant].Repeats, 3)
}

// TestHarnessDiscriminatesBetweenShapes is the same proof on the other axis: two
// shapes on the SAME data must not come back with the same volume, or the harness
// is measuring one thing and labelling it five.
func TestHarnessDiscriminatesBetweenShapes(t *testing.T) {
	if testing.Short() {
		t.Skip("real-OpenFGA proof; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.WriteRepeats = 2
	cfg.RelabelK = 5
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)

	sc := NewScenario(30, 10, 5, "editor", DefaultVerbs())
	vols := map[Form]int64{}
	for _, f := range AllForms {
		c := cellsByOp(r.RunWrites(ctx, f, sc))
		require.Equalf(t, Measured, c[OpVolume].Outcome, "%s volume: %s", f, c[OpVolume].Reason)
		vols[f] = c[OpVolume].GrantTotal
		require.Equalf(t, int64(ExpectedGrantTuples(f, sc)), vols[f],
			"%s: measured tuple count disagrees with the shape's own arithmetic — "+
				"one of the two is wrong and neither may be reported", f)
	}
	require.Greater(t, vols[FormA], vols[FormB])
	require.Greater(t, vols[FormB], vols[FormD])
	require.Greater(t, vols[FormC], vols[FormD])
	require.NotEqual(t, vols[FormA], vols[FormBCD])
}

// TestTransformsActuallyTransform guards the one failure mode of a text rewrite: a
// transform that matches nothing yields a perfectly valid model — the canonical one
// — so shape C would be measured as shape A while wearing C's name.
func TestTransformsActuallyTransform(t *testing.T) {
	_, canon, err := ResolveCanonicalModel()
	require.NoError(t, err)
	src := string(canon)

	c, err := ModelC(src)
	require.NoError(t, err)
	require.NotEqual(t, src, c.DSL, "ModelC produced the canonical model unchanged")
	require.Len(t, c.VerbsTouched, 4, "ModelC must rewrite all four verbs of %s", BenchType)
	require.Contains(t, c.DSL, "define grant_editor:")

	d, err := ModelD(src)
	require.NoError(t, err)
	require.NotEqual(t, src, d.DSL)
	require.Len(t, d.VerbsTouched, 4)
	require.Contains(t, d.DSL, "type label_set")
	require.Contains(t, d.DSL, "define labels: [label_set]")
	require.Contains(t, d.DSL, "or grant_editor from labels")

	// The canonical text is untouched on disk — this harness READS the product, it
	// does not edit it.
	_, again, err := ResolveCanonicalModel()
	require.NoError(t, err)
	require.Equal(t, src, string(again))

	// And a transform pointed at a type that does not exist must FAIL rather than
	// return the input: the negative control for the guard above.
	_, err = rewriteType(src, "no_such_type_anywhere", []string{"    define x: [user]"},
		func(_, cur string) string { return cur }, "")
	require.Error(t, err)
}

func cellsByOp(cs []Cell) map[Op]Cell {
	m := map[Op]Cell{}
	for _, c := range cs {
		m[c.Op] = c
	}
	return m
}
