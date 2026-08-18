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
// round trips. Everything asserted here is COUNTED — round trips, tuple rows,
// sample count — because a counted quantity is the same number on an idle machine
// and on a runner this test does not have all to itself (#713).
//
// Здесь стояло ещё и «вдвое большая работа обязана быть медленнее» (#713). Мысль
// верна, способ проверки — нет: он сравнивал два ЗАМЕРА, то есть спрашивал про
// свойство машины, а отвечал как про свойство дерева, и в конвейере уже
// перевернулся — N=20 p50=315.1мс против N=40 p50=188.1мс на дереве, где ствол в
// тот же час был зелёным. Замер 2026-08-19 (один и тот же вход, 30 снятий, три
// условия: вхолостую · 36 занятых петель на 12 ядрах · четыре параллельных стека):
// p50 при N=20 гулял 85.1…148.8мс (×1.75), при N=40 — 171.8…387.5мс (×2.25), а
// счётная величина тех же тридцати снятий дала 4 и 8 без единого отклонения. Полоса
// шума НЕПОДВИЖНОГО входа шире удвоения, о котором утверждение спрашивало, и это
// не про среднее: в одном из прогонов повторы N=20 дали медиану 98.7мс при максимуме
// 739.5мс — на трёх выборках один такой выброс СТАНОВИТСЯ медианой.
//
// Вопрос «доносит ли канал длительностей разницу» этим не снят, он ПЕРЕНЕСЁН туда,
// где на него можно ответить детерминированно, — `durationchannel_test.go`: те же
// проценты вычисляются на известных выборках, без часов и без стека. Здесь остаётся
// то, что можно утверждать о живом замере, не спрашивая машину о её загрузке.
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

	// (c) канал длительностей ЖИВ и снят с ЭТОЙ операции: число повторов · ненулевая
	// длительность · непустой разброс, плюс порядок величин между собой. Загрузка
	// машины не двигает ни одну из них.
	//
	// «Разброс непустой» — единственная здесь не счётная величина, и она названа
	// вслух: две отдельные записи в движок не занимают одинакового числа микросекунд,
	// а занятая машина делает разброс БОЛЬШЕ, не меньше. То есть направление её
	// отказа противоположно тому, из-за которого снято прежнее утверждение: она
	// краснеет на сломанном приборе (подставленная константа вместо замера), а не на
	// занятом ранере. Предпосылку — что повторов больше одного — устанавливает первое
	// утверждение цикла, а не допущение.
	for _, c := range []struct {
		name string
		cell Cell
	}{{"N=20", cs[OpGrant]}, {"N=40", cl[OpGrant]}} {
		require.Equalf(t, cfg.WriteRepeats, c.cell.Repeats,
			"%s: измеренных повторов %d при заказанных %d — выборка снята не с того числа "+
				"прогонов, каким объявлена (нулевой повтор — разогрев и в счёт не идёт)",
			c.name, c.cell.Repeats, cfg.WriteRepeats)
		require.Greaterf(t, c.cell.Min, 0.0,
			"%s: самый быстрый повтор занял ровно ноль — часы не пошли, и колонка времени "+
				"в отчёте была бы нулями, неотличимыми от мгновенного ответа", c.name)
		require.Greaterf(t, c.cell.Max, c.cell.Min,
			"%s: все повторы одного входа заняли одинаковое время до микросекунды "+
				"(min=max=%.3fмс) — так отвечает не измерение, а подставленная константа",
			c.name, c.cell.Min)
		require.LessOrEqualf(t, c.cell.Min, c.cell.P50, "%s: min выше p50", c.name)
		require.LessOrEqualf(t, c.cell.P50, c.cell.P95, "%s: p50 выше p95", c.name)
		require.LessOrEqualf(t, c.cell.P95, c.cell.Max, "%s: p95 выше max", c.name)
	}

	// (d) направление длительности — НАБЛЮДЕНИЕ, а не вердикт. Печатается, чтобы
	// его можно было прочесть в логе прогона рядом со счётными величинами, и
	// никогда не роняет прогон: величина, которую двигают соседи по машине, не
	// может быть условием посадки (#713).
	t.Logf("наблюдение, не вердикт: grant p50 N=20 %.1fмс (min %.1f, max %.1f) против "+
		"N=40 %.1fмс (min %.1f, max %.1f), отношение ×%.2f; счётные обращения к движку %d → %d",
		cs[OpGrant].P50, cs[OpGrant].Min, cs[OpGrant].Max,
		cl[OpGrant].P50, cl[OpGrant].Min, cl[OpGrant].Max,
		cl[OpGrant].P50/cs[OpGrant].P50, cs[OpGrant].ReqEngine, cl[OpGrant].ReqEngine)
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
