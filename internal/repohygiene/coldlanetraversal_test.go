// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// coldlanetraversal_test.go — ХОЛОДНАЯ полоса замера обязана быть холодной (#764).
//
// # Предмет
//
// Полоса `net_get_cold` УТВЕРЖДАЕТ о себе, что каждый её запрос идёт про новый
// объект, и на этом утверждении стоит весь столбец «проверок на операцию». Само
// утверждение не проверялось ничем, а обход пула опирался на арифметическое
// СОВПАДЕНИЕ, зависящее от размера пула, о котором обход не знал.
//
// Замер, из которого задача выведена: при пуле 1113 столбец дал 0.23 на подачах
// 100 · 200 · 300 · 400 — постоянную, которой арифметика пятисекундного окна не
// производит вообще. При пуле 515 та же полоса вела себя по модели (1.00 · 1.00 ·
// 0.73 на 25 · 50 · 100) и «проверила сама себя» — то есть самопроверка прошла
// ровно там, где совпадение ещё держалось.
//
// # Что здесь доказывается
//
// Первая проба — МОДЕЛЬ обхода, а не сам обход: она показывает механизм и
// воспроизводит ОБА замера числом, поэтому объяснение можно перепроверить, а не
// принять на веру. Вторая проба привязывает модель к дереву: она требует, чтобы в
// полосе стоял разбитый на дольки обход, а не тот, чьё вырождение модель
// показывает. Порознь ни одна из них не достаточна: модель без привязки судила бы
// свою копию правила, а привязка без модели была бы запретом без причины.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// coldLaneFile — полоса замера.
const coldLaneFile = "services/vpc/tests/k6/resource_ops.js"

// verdictWindowIters — на сколько позиций нить продвигается за окно жизни
// вердикта.
//
// Пять, и это не константа вкуса: у аррival-rate исполнителя число
// предвыделенных нитей равно подаче (`PRE_VUS = max(32, TARGET_RPS)`), поэтому
// каждая нить исполняет РОВНО ОДНУ итерацию в секунду, а окно живёт 5 с. Отсюда
// и вырождение: запросов за окно 5N, а различных — сколько даст обход.
const verdictWindowIters = 5

// distinctPerWindowMultiplicative — различные объекты за окно при обходе
// `(VU × stride + ITER) mod P`.
func distinctPerWindowMultiplicative(pool, threads, stride int) int {
	seen := map[int]struct{}{}
	for v := 1; v <= threads; v++ {
		for d := 0; d < verdictWindowIters; d++ {
			seen[(v*stride+d)%pool] = struct{}{}
		}
	}
	return len(seen)
}

// distinctPerWindowSharded — различные объекты за окно при обходе по
// НЕПЕРЕСЕКАЮЩИМСЯ долькам (то, что стоит в полосе после фикса).
func distinctPerWindowSharded(pool, threads int) int {
	shards := threads
	if shards > pool {
		shards = pool
	}
	seen := map[int]struct{}{}
	for v := 1; v <= threads; v++ {
		shard := (v - 1) % shards
		lo := pool * shard / shards
		hi := pool * (shard + 1) / shards
		span := hi - lo
		if span < 1 {
			span = 1
		}
		for d := 0; d < verdictWindowIters; d++ {
			seen[lo+d%span] = struct{}{}
		}
	}
	return len(seen)
}

// degeneracyPeriod — наименьшее m, при котором нити, отстоящие на m, садятся на
// СОСЕДНИЕ слоты пула: (stride × m) mod P ∈ {1, P−1}. Пока m больше числа нитей,
// обход выглядит исправным; как только нитей становится больше — вырождается.
func degeneracyPeriod(pool, stride int) int {
	s := stride % pool
	for m := 1; m < pool; m++ {
		if r := (s * m) % pool; r == 1 || r == pool-1 {
			return m
		}
	}
	return 0
}

// TestColdLaneTraversalDegeneracyIsArithmetic — МЕХАНИЗМ, воспроизведённый числом.
//
// Постоянная доля проверок, не зависящая от подачи, — не загадка и не свойство
// продукта: это арифметика прежнего обхода. Проба показывает обе стороны, потому
// что одна без другой не объясняет ничего: почему СЛОМАЛОСЬ при 1113 и почему
// НЕ БЫЛО ВИДНО при 515.
func TestColdLaneTraversalDegeneracyIsArithmetic(t *testing.T) {
	const stride = 100003

	// Прежняя посадка: пул 515, нитей 32 · 50 · 100. Период вырождения БОЛЬШЕ
	// числа нитей — обход держится, и самопроверка проходит.
	require.Equal(t, 72, degeneracyPeriod(515, stride),
		"при пуле 515 совпадение держится до 72 нитей — вот почему та посадка вела себя по модели")
	for _, threads := range []int{32, 50} {
		require.Equal(t, verdictWindowIters*threads, distinctPerWindowMultiplicative(515, threads, stride),
			"нитей %d меньше периода вырождения — обход обязан давать по объекту на запрос", threads)
	}

	// Посадка, на которой замер сломался: пул 1113. Период вырождения — 20, а
	// нитей 100…400, то есть в 5…20 раз больше.
	require.Equal(t, 20, degeneracyPeriod(1113, stride),
		"при пуле 1113 нити, отстоящие на 20, садятся на соседние слоты — вот и весь механизм")
	for _, threads := range []int{100, 200, 300, 400} {
		distinct := distinctPerWindowMultiplicative(1113, threads, stride)
		requests := verdictWindowIters * threads
		ratio := float64(distinct) / float64(requests)
		t.Logf("пул 1113, нитей %3d: различных за окно %4d при %4d запросах ⇒ доля %.2f",
			threads, distinct, requests, ratio)
		require.InDelta(t, 0.25, ratio, 0.12,
			"доля обязана вырождаться в ≈1/5 и НЕ ЗАВИСЕТЬ от подачи — ровно это и намерено "+
				"на стенде (0.23 на всех четырёх ступенях)")
		require.Equal(t, threads+4*20, distinct,
			"различных за окно ровно N + 4×период: N начал долек плюс продвижение на 4 позиции")
	}

	// Обход по долькам: различных ровно столько, сколько полоса о себе утверждает.
	for _, c := range []struct{ pool, threads int }{
		{515, 32}, {515, 50}, {515, 100},
		{1113, 100}, {1113, 200}, {1113, 300}, {1113, 400},
	} {
		requests := verdictWindowIters * c.threads
		want := requests
		if c.pool < want {
			want = c.pool
		}
		require.Equal(t, want, distinctPerWindowSharded(c.pool, c.threads),
			"пул %d, нитей %d: разбиение обязано давать min(запросов, размер пула) — "+
				"без единого арифметического совпадения", c.pool, c.threads)
	}
}

// TestColdLaneWalksDisjointShardsAndCountsThem — привязка модели к дереву.
//
// Разбор синтаксического дерева здесь неприменим: полоса написана на JS, а
// анализатора JS в этом дереве нет. Поэтому проверка ТЕКСТОВАЯ, и это названо
// вслух: она может пропустить переписанный иначе, но столь же вырожденный обход.
// Чем она НЕ является — доказательством корректности; чем является — гейтом на
// возврат ИМЕННО ТОЙ формы, которая уже стоила замера.
func TestColdLaneWalksDisjointShardsAndCountsThem(t *testing.T) {
	path := filepath.Join(repoRoot(t), coldLaneFile)
	body, err := os.ReadFile(path)
	require.NoError(t, err, "полосы замера нет в дереве — предмет гейта отпал: "+
		"снимите его вместе с полосой либо почините координату")
	src := string(body)
	// Снималка комментариев — ОБЩАЯ с гейтом плана рассечения
	// (newmancarveplananchor_test.go): гейт обязан читать исполняемую часть, иначе
	// краснеет на объяснении собственного запрета — ровно это и случилось здесь на
	// первом прогоне. Второй экземпляр снималки разошёлся бы с первым молча.
	code := stripJSComments(src)
	t.Logf("осмотрено: %s, %d байт, из них исполняемых %d", coldLaneFile, len(src), len(code))
	require.Contains(t, code, "net_get_cold",
		"в файле нет холодной полосы — гейт судит не тот файл, и его молчание ничего не значит")
	require.Less(t, len(code), len(src),
		"снятие комментариев не убрало ни байта — либо файл без комментариев, либо "+
			"снималка сломана, и запрет ниже снова будет читать объяснение вместо кода")

	require.NotContains(t, code, "__VU * 100003",
		"вернулся мультипликативный обход пула: он опирается на арифметическое совпадение, "+
			"зависящее от размера пула, и при пуле 1113 вырождается в постоянную долю проверок "+
			"≈1/5 независимо от подачи (#764)")
	for _, want := range []string{"COLD_SHARDS", "coldPoolIndex", "op_distinct_objects"} {
		require.Contains(t, code, want,
			"полоса обязана ходить по непересекающимся долькам и СЧИТАТЬ различные объекты: "+
				"без счёта её утверждение о собственной холодности не проверяется ничем, "+
				"а именно на нём стоит весь столбец «проверок на операцию»")
	}
}
