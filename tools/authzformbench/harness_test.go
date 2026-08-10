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
	require.Equal(t, 2*cs[OpGrant].ReqEngine, cl[OpGrant].ReqEngine,
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

	// Форма E втянута в доказательство ПОИМЁННО, а не «входит в перечень форм».
	// Попадание в перечень втягивает её только в вопросник эквивалентности — он
	// итерируется по перечню; неравенства объёмов здесь выписаны именами, и без
	// своей строки шестая форма измерялась бы ВНЕ доказательства различимости, а
	// «E неотличима от A» было бы неотличимо от «прибор для E сломан».
	require.Greaterf(t, vols[FormD], vols[FormE],
		"самая компактная форма движка (%d строк) не отличается от реляционной (%d) — "+
			"прибор не видит разницы там, где арифметика форм её предсказывает",
		vols[FormD], vols[FormE])
	require.NotEqual(t, vols[FormA], vols[FormE])
}

// TestHarnessSeesTheInputOfFormE — вторая предпосылка на шестой форме: прибор
// обязан быть показан различающим ЕЁ вход, а не только вход формы A.
//
// Точное удвоение здесь не требуется и требоваться не может: оно — арифметика
// формы A (её цена ЕСТЬ N·M·S), и три формы из пяти уже сегодня его не дают.
// Требование «вырасти вдвое», перенесённое на форму E, либо провалило бы её за
// законное поведение, либо вынудило бы написать её пооператорно — то есть
// исказить измеряемое ради утверждения о нём.
//
// Поэтому проверяется ПАРА, объявленная до прогона: (а) арифметика формы E
// называет величину, обязанную вырасти с N, — это структурная часть (строка
// зеркала на объект); (б) измерение показывает рост именно там и именно такой,
// какой арифметика предсказала. И отдельно — что величина выдачи ПОСТОЯННА, как
// её арифметика и объявляет: постоянство здесь результат, а не отказ прибора.
func TestHarnessSeesTheInputOfFormE(t *testing.T) {
	if testing.Short() {
		t.Skip("real-OpenFGA proof; -short")
	}
	ctx := t.Context()
	stack, canon := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormE}
	cfg.WriteRepeats = 2
	cfg.RelabelK = 5
	r, err := NewRunner(stack, cfg, canon)
	require.NoError(t, err)

	small := NewScenario(20, 10, 5, "editor", DefaultVerbs())
	large := NewScenario(40, 10, 5, "editor", DefaultVerbs())

	cs := cellsByOp(r.RunWrites(ctx, FormE, small))
	cl := cellsByOp(r.RunWrites(ctx, FormE, large))
	require.Equalf(t, Measured, cs[OpVolume].Outcome, "объём при N=20: %s", cs[OpVolume].Reason)
	require.Equalf(t, Measured, cl[OpVolume].Outcome, "объём при N=40: %s", cl[OpVolume].Reason)

	// (а)+(б) величина, объявленная растущей, выросла — и ровно до предсказанного.
	require.Equal(t, int64(ExpectedStructuralRows(FormE, small)), cs[OpVolume].StructuralRows,
		"структурная часть формы E при N=20 разошлась с её объявленной арифметикой")
	require.Equal(t, int64(ExpectedStructuralRows(FormE, large)), cl[OpVolume].StructuralRows)
	require.Greater(t, cl[OpVolume].StructuralRows, cs[OpVolume].StructuralRows,
		"ни одна величина формы E не ответила на удвоение входа — предпосылка «прибор различает» "+
			"для шестой формы осталась бы недоказанной, и это находка постановки, а не результат")

	// Величина ВЫДАЧИ постоянна — и это её объявленная арифметика, а не сбой.
	require.Equal(t, int64(ExpectedGrantTuples(FormE, small)), cs[OpVolume].GrantTotal)
	require.Equal(t, cs[OpVolume].GrantTotal, cl[OpVolume].GrantTotal,
		"цена выдачи формы E изменилась с N, хотя её арифметика объявляет постоянство (S+2) — "+
			"расходятся измерение и арифметика, и публиковать нельзя ни то, ни другое")

	// Колонка стейтментов у формы E измерена, а не подставлена нулём, и сверена
	// со СВОЕЙ объявленной до прогона арифметикой — по каждой операции.
	//
	// Это не педантизм, а дыра, найденная инъекцией в эту самую пробу: форма E,
	// которая вдобавок к выдаче переразмечает весь набор, проходит ВСЕ утверждения
	// об объёме (правка метки строк не добавляет) и падает только здесь. Пока
	// величина сверялась только с «больше нуля», удвоение работы было невидимо.
	require.Empty(t, cs[OpGrant].StmtNote, "производитель StmtSQL формы E не прошёл контроль")
	for _, op := range []Op{OpGrant, OpRevoke, OpRelabel1, OpRelabelK, OpInlineGrant, OpInlineRevoke} {
		want := ExpectedStatements(FormE, op)
		require.Equalf(t, want, cs[op].StmtSQL,
			"%s при N=20: измерено %d стейтментов против объявленных %d — расходятся измерение "+
				"и арифметика, и публиковать нельзя ни то, ни другое", op, cs[op].StmtSQL, want)
		require.Equalf(t, want, cl[op].StmtSQL,
			"%s при N=40 стоила %d стейтментов против %d при N=20 — величина, объявленная "+
				"постоянной по N, с N изменилась", op, cl[op].StmtSQL, cs[op].StmtSQL)
	}
	require.Zero(t, cs[OpGrant].ReqEngine,
		"у формы E появились обращения к движку — она измеряется не тем, чем объявлена")
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
