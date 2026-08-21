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

// ── предпосылка: прибор обязан быть показан РАЗЛИЧАЮЩИМ свой вход ─────────────
//
// Здесь стояли две пробы, и обе сняты вместе с движком отношений, а не «за
// ненадобностью»:
//
//   - различение по ВХОДУ снималось на форме A, чья цена ЕСТЬ N·M·S: удвоение N
//     обязано было удвоить и объём, и число круговых обращений. У формы E цена
//     выдачи ПОСТОЯННА по N — это её объявленная арифметика, а не сбой, — поэтому
//     то же утверждение на ней провалило бы законное поведение. Свойство не
//     потеряно: его несёт `TestHarnessSeesTheInputOfFormE` ниже, где растущей
//     величиной объявлена структурная часть, и оно перенесено туда вместе с
//     проверкой живости канала длительностей;
//   - различение между ФОРМАМИ (две формы на одних данных не должны дать один
//     объём) предмета лишилось буквально: форма осталась одна. Утверждать
//     «прибор не путает A с BCD» больше не о чем.

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
	stack, _ := bootForTest(ctx, t)

	cfg := DefaultConfig()
	cfg.Forms = []Form{FormE}
	cfg.WriteRepeats = 2
	cfg.RelabelK = 5
	r := NewRunner(stack, cfg)

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
	// (в) канал длительностей ЖИВ и снят с ЭТОЙ операции: число повторов ·
	// ненулевая длительность · непустой разброс · порядок процентов между собой.
	// Загрузка машины не двигает ни одну из них.
	//
	// Блок перенесён сюда со снятой пробы формы A — вместе с её уроком (#713).
	// Прежде рядом стояло «вдвое большая работа обязана быть медленнее»; мысль
	// верна, способ проверки — нет: он сравнивал два ЗАМЕРА, то есть спрашивал про
	// свойство машины, а отвечал как про свойство дерева, и в конвейере уже
	// перевернулся. Полоса шума НЕПОДВИЖНОГО входа шире удвоения, о котором
	// утверждение спрашивало. Осталось то, что можно утверждать о живом замере, не
	// спрашивая машину о её загрузке.
	//
	// «Разброс непустой» — единственная здесь не счётная величина, и она названа
	// вслух: две отдельные записи не занимают одинакового числа микросекунд, а
	// занятая машина делает разброс БОЛЬШЕ, не меньше. Направление её отказа
	// противоположно тому, из-за которого снято прежнее утверждение: она краснеет
	// на сломанном приборе (подставленная константа вместо замера), а не на
	// занятом ранере.
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

	// (г) направление длительности — НАБЛЮДЕНИЕ, а не вердикт. Печатается, чтобы
	// его можно было прочесть в логе рядом со счётными величинами, и никогда не
	// роняет прогон: величина, которую двигают соседи по машине, не может быть
	// условием посадки (#713).
	t.Logf("наблюдение, не вердикт: grant p50 N=20 %.1fмс (min %.1f, max %.1f) против "+
		"N=40 %.1fмс (min %.1f, max %.1f); счётные стейтменты %d → %d, структурных строк %d → %d",
		cs[OpGrant].P50, cs[OpGrant].Min, cs[OpGrant].Max,
		cl[OpGrant].P50, cl[OpGrant].Min, cl[OpGrant].Max,
		cs[OpGrant].StmtSQL, cl[OpGrant].StmtSQL,
		cs[OpVolume].StructuralRows, cl[OpVolume].StructuralRows)
}

func cellsByOp(cs []Cell) map[Op]Cell {
	m := map[Op]Cell{}
	for _, c := range cs {
		m[c.Op] = c
	}
	return m
}
