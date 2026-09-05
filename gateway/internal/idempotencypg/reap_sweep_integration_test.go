// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// reap_sweep_integration_test.go — уборка записей однократности ДОГОНЯЕТ темп и
// называет своё отставание величиной (#1302).
//
// # Почему это отдельный предмет от #1293
//
// Класс тот же — постоянная ёмкость уборки при внешнем темпе записи, — но
// таблица другая, писатель другой и жизнь строки другая: сутки против окна
// свежести доказательства. Поэтому и шаг здесь выводится из СВОЕЙ жизни строки,
// а не копируется у соседней уборки: скопированная величина была бы вторым
// экземпляром того же дефекта.
//
// # Почему пробы утверждают состояние ТАБЛИЦЫ, а не возвращённое значение
//
// «Уборщик вызвался» и «уборщик унёс» — разные утверждения, и зелено первое
// бывает при ложном втором. Пробы ниже смотрят на строки СВОИМ пулом, помимо
// хранилища; то, что хранилище рассказывает о себе, сверяется с этим отдельно.
package idempotencypg_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// countRecords — сколько всего строк однократности лежит в таблице.
func countRecords(t *testing.T, p *pgxpool.Pool) int64 {
	t.Helper()
	var n int64
	if err := p.QueryRow(context.Background(),
		`SELECT count(*) FROM kacho_gateway.idempotency_records`).Scan(&n); err != nil {
		t.Fatalf("перепись строк: %v", err)
	}
	return n
}

// seedRecords кладёт n записей ОДНИМ оператором со сроком, отстоящим от «сейчас»
// на age (отрицательный — просроченные, положительный — живые).
//
// Не через Reserve/Commit намеренно: предмет проб ниже — тысячи строк, и тысяча
// обращений превратила бы пробу в замер сети, а не уборки.
//
// `done=TRUE` вместе со `status_code` — законченная запись: иначе ограничение
// таблицы «законченная обязана нести ответ» отвергло бы посев, и проба падала бы
// на своей фикстуре, а не на предмете.
func seedRecords(t *testing.T, p *pgxpool.Pool, prefix string, n int, age time.Duration) {
	t.Helper()
	_, err := p.Exec(context.Background(), `
INSERT INTO kacho_gateway.idempotency_records
       (key, lease_owner, lease_expires_at, done, status_code, content_type, body, expires_at)
SELECT $1 || i::text, '', now() + make_interval(secs => $3::double precision),
       TRUE, 200, 'application/json', '\x7b7d'::bytea,
       now() + make_interval(secs => $3::double precision)
  FROM generate_series(1, $2) AS i`, prefix, n, age.Seconds())
	if err != nil {
		t.Fatalf("посев записей: %v", err)
	}
}

// TestReapSweep_DrainsBacklogBeyondOneBatch — ПРЕДИКАТ ЗАДАЧИ #1302: уборка
// уносит ВЕСЬ просроченный хвост, а не одну партию.
//
// Темп записи задаёт ВЫЗЫВАЮЩИЙ края: строка появляется на каждую мутацию с
// ключом однократности, то есть величина внешняя и границы у неё нет. Уборка,
// уносящая B строк за период P, догоняет ровно пока темп ≤ B/P; при B=1000 и
// P=1ч это 0.28 запроса в секунду. Выше — хвост растёт без границы, и всякая
// проверка вида «сборщик вызвался» при этом ЗЕЛЕНА.
//
// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ рядом: живая запись обязана уцелеть. Без него проба
// зеленела бы на уборщике, сносящем таблицу целиком, — то есть на потере самой
// однократности.
func TestReapSweep_DrainsBacklogBeyondOneBatch(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	obs := observer(t, dsn)
	// Шаг уборки заведомо больше жизни пробы: уборку зовём САМИ, чтобы исход
	// принадлежал вызову, а не гонке с тикером.
	s := replica(t, dsn, idempotencypg.Config{ReapInterval: time.Hour})

	// Больше двух партий: одна партия предмета не показала бы.
	const backlog = 2*idempotencypg.ReapBatch + 500
	seedRecords(t, obs, "idem-1302-drain-", backlog, -time.Minute)
	seedRecords(t, obs, "idem-1302-drain-live-", 1, time.Hour)
	if got := countRecords(t, obs); got != backlog+1 {
		t.Fatalf("до уборки в таблице %d строк, ожидалось %d", got, backlog+1)
	}

	if _, err := s.Reap(ctx); err != nil {
		t.Fatalf("уборка: %v", err)
	}

	left := countRecords(t, obs)
	if left != 1 {
		t.Fatalf("после уборки осталось %d строк, ожидалась одна живая: "+
			"уборка остановилась на партии (%d) и хвост её пережил.\n"+
			"Ёмкость уборки постоянна, а темп записи задаёт внешняя сторона — "+
			"значит ни одна постоянная величина здесь не может быть верной.",
			left, idempotencypg.ReapBatch)
	}
}

// TestReapSweep_JanitorStepFollowsTheRecordLifetime — шаг уборки ВЫВЕДЕН из
// жизни записи, а не взят постоянной величиной.
//
// # Почему проба смотрит на исход, а не на объявление
//
// Величину шага можно объявить верно и не провязать: тикер построится по
// прежней постоянной, и «шаг выведен» останется утверждением о коде, а не о
// поведении. Поэтому проба строит хранилище с КОРОТКОЙ жизнью записи и ждёт,
// когда просроченная строка исчезнет САМА — без единого вызова уборки.
//
// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: живая запись обязана уцелеть.
func TestReapSweep_JanitorStepFollowsTheRecordLifetime(t *testing.T) {
	dsn := pgtest.NewDB(t)
	obs := observer(t, dsn)

	// Жизнь записи короткая, шаг уборки НЕ задан: он обязан приехать из неё.
	// 24 секунды выбраны не наугад — столько же заходов уборки приходится на
	// жизнь строки и в проде (сутки → час).
	s := replica(t, dsn, idempotencypg.Config{TTL: 24 * time.Second})

	seedRecords(t, obs, "idem-1302-step-stale-", 1, -time.Minute)
	seedRecords(t, obs, "idem-1302-step-live-", 1, time.Hour)
	if got := countRecords(t, obs); got != 2 {
		t.Fatalf("до уборки в таблице %d строк, ожидалось 2", got)
	}

	// Ждём УСЛОВИЯ, а не времени: пауза фиксированной длины либо флейкует, либо
	// удлиняет прогон на всех.
	deadline := time.Now().Add(20 * time.Second)
	var left int64
	for time.Now().Before(deadline) {
		if left = countRecords(t, obs); left == 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if left != 1 {
		t.Fatalf("просроченная запись НЕ убрана: в таблице %d строк, ожидалась одна живая.\n"+
			"Шаг уборки взят постоянной величиной и с жизнью записи не связан: "+
			"на жизни строки в 24 с уборка не исполнилась ни разу.", left)
	}

	// Заход ДОШЁЛ ДО НАБЛЮДАТЕЛЯ, а не только до таблицы: в проде исход уборки
	// виден лишь снимком, и счётчик заходов — то единственное, чем «догоняет»
	// отличается от «не исполнялась ни разу».
	if st := s.ReapSweepStats(); st.Sweeps == 0 {
		t.Fatal("снимок не насчитал ни одной уборки, хотя строка убрана: " +
			"ноль отставания остался бы неотличим от неисполнения")
	}
}

// TestReapSweep_ReportsLagWhenItCannotDrain — не догнав, уборка ГОВОРИТ об этом
// числом.
//
// Отставание обязано быть наблюдаемым, а не выводимым из спокойствия: уборщик,
// уносящий свою партию и молчащий, неотличим от уборщика, который догоняет.
// Здесь граница ставится искусственно (одна партия за заход), потому что
// настоящий темп задаёт внешняя сторона и в пробе его не создать.
func TestReapSweep_ReportsLagWhenItCannotDrain(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	obs := observer(t, dsn)
	s := replica(t, dsn, idempotencypg.Config{
		ReapInterval:   time.Hour,
		ReapMaxBatches: 1,
	})

	const backlog = idempotencypg.ReapBatch + 1
	seedRecords(t, obs, "idem-1302-lag-", backlog, -time.Minute)

	got, err := s.Reap(ctx)
	if err != nil {
		t.Fatalf("уборка: %v", err)
	}
	if got.Removed != idempotencypg.ReapBatch {
		t.Fatalf("унесено %d, ожидалась ровно одна партия (%d)",
			got.Removed, idempotencypg.ReapBatch)
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
	st := s.ReapSweepStats()
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

// TestReapSweep_DoesNotOpenAReplayWindow — уборка НЕ открывает окна повторного
// допуска по живому ключу.
//
// Порог уборки обязан быть согласован с предикатом ДЕРЖАТЕЛЯ: тот перехватывает
// строку по условию ШИРЕ (истёк срок записи ЛИБО умерла бронь незаконченной), а
// уборка уносит строгое подмножество. Разойдись они — уборка снимала бы запись
// раньше, чем держатель перестаёт её honorировать, и мутация, уже исполненная,
// прошла бы вторично.
//
// Проба утверждает ОБЕ стороны: живое не уносится и остаётся погашением,
// просроченное уносится и погашением быть перестаёт (вторая сторона — соседняя
// проба `TestExpiredRecordIsReapedAndTheKeyBecomesClaimableAgain`).
func TestReapSweep_DoesNotOpenAReplayWindow(t *testing.T) {
	dsn := pgtest.NewDB(t)
	ctx := context.Background()
	s := replica(t, dsn, idempotencypg.Config{ReapInterval: time.Hour})

	res, err := s.Reserve(ctx, "idem-1302-window-live")
	if err != nil {
		t.Fatalf("допуск: %v", err)
	}
	if res.Outcome != middleware.IdempotencyOwn {
		t.Fatalf("первый предъявитель получил исход %v, ожидался «держатель»", res.Outcome)
	}
	s.Commit(ctx, res, middleware.IdempotencyRecord{
		StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(`{}`),
	}, true)

	sw, err := s.Reap(ctx)
	if err != nil {
		t.Fatalf("уборка: %v", err)
	}
	if sw.Removed != 0 {
		t.Fatalf("уборка унесла %d живых записей — окно повторного допуска открыто", sw.Removed)
	}

	again, err := s.Reserve(ctx, "idem-1302-window-live")
	if err != nil {
		t.Fatalf("повторный допуск: %v", err)
	}
	if again.Outcome != middleware.IdempotencyReplay {
		t.Fatalf("повтор получил исход %v, ожидался «сохранённый ответ»: "+
			"уборка сняла запись, которую держатель ещё считает погашением", again.Outcome)
	}
}

// TestReapIntervalFor_FollowsTheRecordLifetime — шаг уборки есть ФУНКЦИЯ жизни
// записи, а не постоянная.
//
// # Почему проверяется отношение, а не одно число
//
// Одно число зелено и у постоянной: `ReapIntervalFor`, возвращающая час на что
// угодно, прошла бы проверку «сутки → час». Поэтому утверждается ОТНОШЕНИЕ
// (шаг × число заходов = жизнь записи) и то, что РАЗНЫМ TTL отвечают РАЗНЫЕ
// шаги, — контроль в ту сторону, в которую постоянная и лжёт.
func TestReapIntervalFor_FollowsTheRecordLifetime(t *testing.T) {
	// Отношение: заходов уборки — двадцать четыре на жизнь записи.
	for _, ttl := range []time.Duration{24 * time.Hour, 12 * time.Hour, 48 * time.Minute} {
		if got := idempotencypg.ReapIntervalFor(ttl); got*24 != ttl {
			t.Errorf("жизнь записи %v → шаг %v: заходов %v, ожидалось 24",
				ttl, got, ttl/got)
		}
	}
	// Читаемый якорь: сутки жизни — час шага.
	if got := idempotencypg.ReapIntervalFor(24 * time.Hour); got != time.Hour {
		t.Errorf("на суточной жизни записи шаг %v, ожидался час", got)
	}
	// КОНТРОЛЬ: разным TTL отвечают разные шаги. Без него постоянная величина
	// прошла бы якорь и осталась бы незамеченной.
	if a, b := idempotencypg.ReapIntervalFor(24*time.Hour), idempotencypg.ReapIntervalFor(12*time.Hour); a == b {
		t.Errorf("вдвое разной жизни записи отвечает ОДИН шаг %v — величина постоянна, а не выведена", a)
	}
	// Пол: тикер с неположительным шагом паникует, поэтому исчезающе малая
	// жизнь записи обязана дать положительный шаг.
	if got := idempotencypg.ReapIntervalFor(time.Nanosecond); got <= 0 {
		t.Errorf("на исчезающе малой жизни записи шаг %v — тикер с ним паникует", got)
	}
	// Незаданная жизнь записи — не ноль: шаг выводится из прод-умолчания.
	if got := idempotencypg.ReapIntervalFor(0); got != idempotencypg.ReapIntervalFor(middleware.IdempotencyTTL) {
		t.Errorf("незаданной жизни записи отвечает шаг %v, ожидался выведенный из прод-умолчания", got)
	}
}
