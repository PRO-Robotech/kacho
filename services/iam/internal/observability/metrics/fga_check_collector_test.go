// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

// fga_check_collector_test.go — предмет #720: три источника одиночного отказа
// недоступности («хранилище моргнуло» · «до хранилища не дозвонились» ·
// «связь оборвалась») приезжали вызывающему ОДНИМ кодом и были неразличимы
// иначе как чтением журнала построчно — а путь чтения прав, на котором это
// случилось, в журнал не пишет вовсе.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func fgaCheckSeries(t *testing.T, r *Registry, outcome string) (value float64, present bool) {
	t.Helper()
	return labelledCounter(t, r, FGACheckOutcomesMetric, map[string]string{"outcome": outcome})
}

// TestFGACheckCollector_OutageShapesAreDistinguishable — три источника обязаны
// давать РАЗНЫЙ наблюдаемый вывод. Отрицание в паре с положительным: сперва
// показываем, что по одному лишь «был отказ» они неразличимы.
func TestFGACheckCollector_OutageShapesAreDistinguishable(t *testing.T) {
	slow := NewRegistry()
	slow.NewFGACheckCollector(func() FGACheckCounts { return FGACheckCounts{Answered: 700, Deadline: 1} })

	down := NewRegistry()
	down.NewFGACheckCollector(func() FGACheckCounts { return FGACheckCounts{Answered: 700, Connect: 1} })

	dropped := NewRegistry()
	dropped.NewFGACheckCollector(func() FGACheckCounts { return FGACheckCounts{Answered: 700, Reset: 1} })

	// Предпосылка: по числу ОТВЕЧЕННЫХ три состояния совпадают — то есть без
	// разбора причин они и правда неотличимы.
	for _, r := range []*Registry{slow, down, dropped} {
		v, ok := fgaCheckSeries(t, r, "answered")
		require.True(t, ok)
		require.Equal(t, float64(700), v)
	}

	dl, ok := fgaCheckSeries(t, slow, "deadline")
	require.True(t, ok)
	require.Equal(t, float64(1), dl, "«не ответило вовремя» обязано быть названо своей клеткой")

	cn, ok := fgaCheckSeries(t, down, "connect")
	require.True(t, ok)
	require.Equal(t, float64(1), cn, "«не дозвонились» обязано быть названо своей клеткой")

	rs, ok := fgaCheckSeries(t, dropped, "reset")
	require.True(t, ok)
	require.Equal(t, float64(1), rs, "«связь оборвалась» обязана быть названа своей клеткой")

	// И перекрёстно: клетка одного источника молчит на другом.
	v, ok := fgaCheckSeries(t, down, "deadline")
	require.True(t, ok)
	require.Zero(t, v, "у «не дозвонились» клетка срока обязана молчать")
	v, ok = fgaCheckSeries(t, slow, "connect")
	require.True(t, ok)
	require.Zero(t, v, "у «не ответило вовремя» клетка соединения обязана молчать")
}

// TestFGACheckCollector_AbsorbedBlipIsVisibleWithoutAnyRedProbe — главный
// новый сигнал: перебой, поглощённый повтором, наружу отказом не выходит,
// поэтому НИ ОДНА проба на него не покраснеет. Единственное, чем он вообще
// наблюдаем, — эта клетка.
func TestFGACheckCollector_AbsorbedBlipIsVisibleWithoutAnyRedProbe(t *testing.T) {
	flapping := NewRegistry()
	flapping.NewFGACheckCollector(func() FGACheckCounts { return FGACheckCounts{Answered: 734, Recovered: 1} })

	calm := NewRegistry()
	calm.NewFGACheckCollector(func() FGACheckCounts { return FGACheckCounts{Answered: 735} })

	// Оба состояния снаружи выглядят одинаково: отказов нет ни в одном.
	for _, r := range []*Registry{flapping, calm} {
		for _, cell := range []string{"deadline", "connect", "reset", "server_error"} {
			v, ok := fgaCheckSeries(t, r, cell)
			require.True(t, ok)
			require.Zero(t, v, "предпосылка: по отказам эти два состояния не различаются")
		}
	}

	rec, ok := fgaCheckSeries(t, flapping, "recovered")
	require.True(t, ok)
	require.Equal(t, float64(1), rec, "поглощённый перебой обязан быть виден числом")

	rec, ok = fgaCheckSeries(t, calm, "recovered")
	require.True(t, ok)
	require.Zero(t, rec, "положительный контроль: на спокойном хранилище клетка молчит")
}

// TestFGACheckCollector_EveryDeclaredCellIsEmitted — набор клеток закрыт, и
// каждая объявленная обязана существовать до первого события: иначе «клетки
// нет» неотличимо от «в клетке ноль».
func TestFGACheckCollector_EveryDeclaredCellIsEmitted(t *testing.T) {
	r := NewRegistry()
	r.NewFGACheckCollector(func() FGACheckCounts { return FGACheckCounts{} })
	cells := FGACheckOutcomeCells()
	require.NotEmpty(t, cells, "набор клеток пуст — проверять нечего")
	for _, cell := range cells {
		_, ok := fgaCheckSeries(t, r, cell)
		require.True(t, ok, "объявленная клетка %q не выведена коллектором", cell)
	}
	t.Logf("осмотрено клеток: %d", len(cells))
}

// TestFGACheckCollector_NilSourceRefuses — вечный ноль выглядит как работающее
// наблюдение и утверждает неправду о хранилище, которое забыли подключить.
func TestFGACheckCollector_NilSourceRefuses(t *testing.T) {
	require.Panics(t, func() { NewRegistry().NewFGACheckCollector(nil) })
}
