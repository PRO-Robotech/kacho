// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// dpop_sweep_integration_test.go — уборка записей однократности предъявления
// ИСПОЛНЯЕТСЯ и ДОГОНЯЕТ темп (#1293).
//
// # Почему пробы здесь утверждают состояние ТАБЛИЦЫ, а не факт вызова
//
// «Уборщик вызвался» зелено и на уборщике, который ничего не унёс: вызов есть
// исполнение, а предмет — исход. Поэтому каждая проба ниже смотрит на строки
// СВОИМ пулом, помимо хранилища: то, что хранилище рассказывает о себе, и то,
// что лежит в базе, — разные утверждения, и сходиться они обязаны наблюдаемо.
package idempotencypg_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// observer — независимый читатель базы: смотрит на строки помимо хранилища.
//
// Закрытие — через `pgtest.ClosePoolAtEnd`, а не `t.Cleanup(p.Close)`:
// отложенное закрытие ждёт соединение, которое проба, упавшая внутри открытой
// транзакции, не вернёт никогда, — и уносит с собой вердикт всего пакета. Это
// свойство держит гейт дерева `internal/repohygiene`
// `TestPoolCloseInTestsIsBounded`; он же и нашёл здесь первую редакцию.
func observer(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("наблюдатель: пул: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, p)
	return p
}

// countProofs — сколько всего строк доказательств лежит в таблице.
func countProofs(t *testing.T, p *pgxpool.Pool) int64 {
	t.Helper()
	var n int64
	if err := p.QueryRow(context.Background(),
		`SELECT count(*) FROM kacho_gateway.dpop_replay`).Scan(&n); err != nil {
		t.Fatalf("перепись строк: %v", err)
	}
	return n
}

// TestDPoPSweep_JanitorRemovesExpiredWithoutBeingCalled — ПРЕДИКАТ ЗАДАЧИ #1293:
// просроченная запись исчезает САМА, без явного вызова уборщика из пробы.
//
// Проба намеренно не зовёт уборку: предмет — есть ли у неё прод-вызывающий.
// Утверждается ИСХОД (строки нет), а не то, что кто-то куда-то сходил.
//
// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ рядом: живая запись обязана уцелеть. Без него проба
// зеленела бы на уборщике, сносящем таблицу целиком, — то есть на потере самой
// гарантии однократности.
func TestDPoPSweep_JanitorRemovesExpiredWithoutBeingCalled(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	obs := observer(t, dsn)

	// Шаг уборки короткий намеренно: предмет — что шаг ВООБЩЕ есть, а его
	// величина проверяется отдельной пробой ниже.
	s := replica(t, dsn, idempotencypg.Config{DPoPPurgeInterval: 200 * time.Millisecond})

	if err := s.AddDPoPProof(ctx, "jti-1293-stale", time.Millisecond); err != nil {
		t.Fatalf("просроченное предъявление: %v", err)
	}
	if err := s.AddDPoPProof(ctx, "jti-1293-live", time.Hour); err != nil {
		t.Fatalf("живое предъявление: %v", err)
	}
	if got := countProofs(t, obs); got != 2 {
		t.Fatalf("до уборки в таблице %d строк, ожидалось 2", got)
	}

	// Ждём УСЛОВИЯ, а не времени: пауза фиксированной длины либо флейкует, либо
	// удлиняет прогон на всех.
	deadline := time.Now().Add(15 * time.Second)
	var left int64
	for time.Now().Before(deadline) {
		if left = countProofs(t, obs); left == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if left != 1 {
		t.Fatalf("просроченная запись НЕ убрана: в таблице %d строк, ожидалась одна живая.\n"+
			"Уборщик объявлен, но его никто не зовёт: таблица растёт без границы,\n"+
			"а шапка уборщика утверждает, что его зовёт сборщик хранилища.", left)
	}

	// Уцелела именно живая: повтор по ней по-прежнему отвергается.
	if err := s.AddDPoPProof(ctx, "jti-1293-live", time.Hour); err == nil {
		t.Fatal("уборщик снёс ЖИВУЮ запись — гарантия однократности потеряна")
	}
}

// seedExpired кладёт n УЖЕ просроченных строк одним оператором.
//
// Не через AddDPoPProof намеренно: предмет проб ниже — тысячи строк, и тысяча
// обращений превратила бы пробу в замер сети, а не уборки.
func seedExpired(t *testing.T, p *pgxpool.Pool, prefix string, n int) {
	t.Helper()
	_, err := p.Exec(context.Background(), `
INSERT INTO kacho_gateway.dpop_replay (jti, expires_at)
SELECT $1 || i::text, now() - make_interval(secs => 60)
  FROM generate_series(1, $2) AS i`, prefix, n)
	if err != nil {
		t.Fatalf("посев просроченных: %v", err)
	}
}

// TestDPoPSweep_DrainsBacklogBeyondOneBatch — уборка уносит ВЕСЬ просроченный
// хвост, а не одну партию.
//
// # Почему это отдельный предмет, а не подробность
//
// Темп записи задаёт ПРЕДЪЯВИТЕЛЬ — внешняя сторона, и величины у него нет.
// Уборка, уносящая B строк за период P, догоняет только пока темп ≤ B/P; при
// B=1000 и P=1ч это 0.28 запроса в секунду. Выше — хвост растёт без границы, и
// всякая проверка вида «уборщик вызвался» при этом ЗЕЛЕНА. Значит ёмкость
// уборки обязана быть не постоянной, а тянуться за хвостом: партия остаётся
// ограниченной (блокировки), но партий за одну уборку столько, сколько нужно.
func TestDPoPSweep_DrainsBacklogBeyondOneBatch(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	obs := observer(t, dsn)
	// Шаг уборки заведомо больше жизни пробы: уборку зовём САМИ, чтобы исход
	// принадлежал вызову, а не гонке с тикером.
	s := replica(t, dsn, idempotencypg.Config{DPoPPurgeInterval: time.Hour})

	// Больше двух партий: одна партия предмета не показала бы.
	const backlog = 2*idempotencypg.DPoPPurgeBatch + 500
	seedExpired(t, obs, "jti-1293-drain-", backlog)
	if err := s.AddDPoPProof(ctx, "jti-1293-drain-live", time.Hour); err != nil {
		t.Fatalf("живое предъявление: %v", err)
	}

	got, err := s.PurgeExpiredDPoPProofs(ctx)
	if err != nil {
		t.Fatalf("уборка: %v", err)
	}
	if got.Removed != backlog {
		t.Fatalf("унесено %d строк из %d: уборка остановилась на партии и хвост её пережил",
			got.Removed, backlog)
	}
	if !got.Drained {
		t.Fatalf("уборка объявила себя недогнавшей, хотя унесла весь хвост (%d)", got.Removed)
	}
	if got.Lag != 0 {
		t.Fatalf("отставание %v при вычищенном хвосте — величина утверждает то, чего нет", got.Lag)
	}
	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: унесено просроченное, а не всё.
	if left := countProofs(t, obs); left != 1 {
		t.Fatalf("после уборки осталось %d строк, ожидалась одна живая", left)
	}
}

// TestDPoPSweep_ReportsLagWhenItCannotDrain — не догнав, уборка ГОВОРИТ об этом
// числом.
//
// Отставание обязано быть наблюдаемым, а не выводимым из спокойствия: уборщик,
// уносящий свою партию и молчащий, неотличим от уборщика, который догоняет.
// Здесь граница ставится искусственно (одна партия за уборку), потому что
// настоящий темп задаёт внешняя сторона и в пробе его не создать.
func TestDPoPSweep_ReportsLagWhenItCannotDrain(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	obs := observer(t, dsn)
	s := replica(t, dsn, idempotencypg.Config{
		DPoPPurgeInterval:   time.Hour,
		DPoPPurgeMaxBatches: 1,
	})

	const backlog = idempotencypg.DPoPPurgeBatch + 1
	seedExpired(t, obs, "jti-1293-lag-", backlog)

	got, err := s.PurgeExpiredDPoPProofs(ctx)
	if err != nil {
		t.Fatalf("уборка: %v", err)
	}
	if got.Removed != idempotencypg.DPoPPurgeBatch {
		t.Fatalf("унесено %d, ожидалась ровно одна партия (%d)",
			got.Removed, idempotencypg.DPoPPurgeBatch)
	}
	if got.Drained {
		t.Fatalf("уборка объявила себя догнавшей, оставив %d строк — молчание при живом хвосте",
			backlog-got.Removed)
	}
	if got.Lag <= 0 {
		t.Fatalf("отставание %v: хвост есть, а величина его не называет — "+
			"признак отставания выводился бы из спокойствия", got.Lag)
	}

	// Величина ДОХОДИТ ДО НАБЛЮДАТЕЛЯ, а не остаётся в возвращённом значении:
	// в проде уборку зовёт тикер, и её исход виден только снимком.
	st := s.DPoPSweepStats()
	if st.Sweeps == 0 {
		t.Fatal("снимок не насчитал ни одной уборки: ноль отставания был бы неотличим от неисполнения")
	}
	if st.RemovedTotal != uint64(got.Removed) {
		t.Fatalf("снимок насчитал %d унесённых, уборка вернула %d", st.RemovedTotal, got.Removed)
	}
	if st.Lag <= 0 || st.Drained {
		t.Fatalf("снимок объявляет благополучие (отставание %v, догнала=%v) при живом хвосте",
			st.Lag, st.Drained)
	}
}

// TestDPoPSweep_ThresholdCoincidesWithTheReaderPredicate — уборка НЕ открывает
// окна законного повтора.
//
// # Что здесь проверяется и почему это не самоочевидно
//
// Порог уборки обязан быть согласован с предикатом ЧИТАТЕЛЯ таблицы. Читатель
// здесь судит ПО СРОКУ (`ON CONFLICT … WHERE expires_at <= now()`), тем же
// выражением и по тем же часам — часам базы, — что и уборка. Значит уборка
// уносит ровно ту строку, которую читатель и так перестал бы принимать, и
// запаса ей не нужно.
//
// Разошлись бы предикаты — уборка снимала бы строку РАНЬШЕ, чем читатель
// перестаёт её принимать, и перехваченное доказательство прошло бы вторично.
// Проба утверждает обе стороны: живое не уносится и остаётся запретом,
// просроченное уносится и перестаёт им быть.
func TestDPoPSweep_ThresholdCoincidesWithTheReaderPredicate(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	s := replica(t, dsn, idempotencypg.Config{DPoPPurgeInterval: time.Hour})

	const live = "jti-1293-window-live"
	if err := s.AddDPoPProof(ctx, live, time.Hour); err != nil {
		t.Fatalf("живое предъявление: %v", err)
	}
	if _, err := s.PurgeExpiredDPoPProofs(ctx); err != nil {
		t.Fatalf("уборка: %v", err)
	}
	// Живое: уборка его не тронула, читатель по-прежнему отвергает повтор.
	if err := s.AddDPoPProof(ctx, live, time.Hour); err == nil {
		t.Fatal("уборка открыла окно: доказательство, которое читатель ещё считает " +
			"предъявленным, унесено, и повтор прошёл")
	}

	// Просроченное: читатель его уже допускает — значит и уборке оно не нужно.
	const stale = "jti-1293-window-stale"
	if err := s.AddDPoPProof(ctx, stale, time.Millisecond); err != nil {
		t.Fatalf("просроченное предъявление: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	got, err := s.PurgeExpiredDPoPProofs(ctx)
	if err != nil {
		t.Fatalf("уборка: %v", err)
	}
	if got.Removed != 1 {
		t.Fatalf("унесено %d строк, ожидалась одна просроченная", got.Removed)
	}
	if err := s.AddDPoPProof(ctx, stale, time.Hour); err != nil {
		t.Fatalf("за пределами окна свежести значение обязано освободиться: %v", err)
	}
}
