// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journalprobe_helpers_test.go — обвязка проб NTF3-62 и УК3-28 над журналом модуля
// (приёмка NTF-3, NTF-3 «модули kacho подключаются к сервису уведомлений» (каталог `docs/specs` воркспейса)).
//
// # Почему файл повторяется в пяти пакетах
//
// Проба живёт в пакете миграций КАЖДОГО модуля: предмет — схема его журнала, а
// цепочку миграций модуля видит только его пакет (`services/<svc>/internal/...`
// закрыт для соседей правилом internal). Общий пакет обвязки вне `internal`
// завёл бы новый путь ради тестов; файл одинаков побайтово во всех пяти пакетах
// (сверка — `sha256sum services/{compute,vpc,nlb,registry,storage}/internal/migrations/journalprobe_helpers_test.go`).
//
// # Как проба ставит инициатора
//
// Тем же механизмом, что помощник транзакции записи журнала: настройка
// `kacho_journal.initiator`, выставленная ЛОКАЛЬНО к транзакции
// (`set_config(…, true)`). Оператор вставки колонку `initiator` не называет
// никогда: её единственный производитель — умолчание колонки.
package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/pgtest"
)

// journalInitiatorSetting — настройка, из которой умолчание колонки берёт инициатора.
const journalInitiatorSetting = "kacho_journal.initiator"

// journalInitiatorColumn — колонка инициатора строки журнала.
const journalInitiatorColumn = "initiator"

// probeInitiator — инициатор положительного близнеца (NTF3-62: «строка с `user:usr-A`
// и временем вставлена»). Он же — инициатор посева фикстуры: посев меняет таблицы,
// чьи функции базы пишут журнал, и без инициатора после миграции был бы отвергнут
// сам — проба упала бы на фикстуре, а не на предмете.
const probeInitiator = "user:usr-A"

// sqlStateNotNull, sqlStateCheck — коды отказа, которых ждёт NTF3-62.
const (
	sqlStateNotNull = "23502"
	sqlStateCheck   = "23514"
)

// initiatorOf — значение настройки инициатора на транзакцию; nil — не выставлена вовсе.
type initiatorOf = *string

func initiator(s string) initiatorOf { return &s }

// openJournalProbeDB — собственная пустая база пробы на контейнере пакета, цепочка
// миграций модуля применена до головы, одно соединение pgx.
//
// Одно соединение, а не пул, — нарочно: УК3-28 судит переиспользованное соединение,
// и каждая транзакция пробы идёт по нему же.
func openJournalProbeDB(t *testing.T, chain fs.FS) *pgx.Conn {
	t.Helper()
	dsn := pgtest.NewEmptyDB(t)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	goose.SetBaseFS(chain)
	require.NoError(t, goose.SetDialect("postgres"))
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.Up(db, "."), "фикстура: цепочка миграций модуля применяется до головы")
	require.NoError(t, db.Close())

	conn, err := pgx.Connect(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// declareQuotaAuthorityAbsent — фикстура: домен величин объявлен неразвёрнутым
// (`quota_sync_cursor.authority_state = 'not-deployed'`), и списывающие триггеры
// модуля не отвергают вставку `KQ002` «потолок не назван». Предмет пробы — журнал,
// а не учёт; без этого посев и изменение падали бы на учёте раньше журнала.
func declareQuotaAuthorityAbsent(t *testing.T, conn *pgx.Conn, schema string) {
	t.Helper()
	_, err := conn.Exec(context.Background(), `
		INSERT INTO `+schema+`.quota_sync_cursor (id, authority_state) VALUES ('limits', 'not-deployed')
		ON CONFLICT (id) DO UPDATE SET authority_state = 'not-deployed'`)
	require.NoError(t, err, "фикстура: домен величин объявлен неразвёрнутым в схеме %s", schema)
}

// open — база пробы для журнала модуля: цепочка применена, домен величин объявлен
// неразвёрнутым.
func (j journalTable) open(t *testing.T, chain fs.FS) *pgx.Conn {
	t.Helper()
	conn := openJournalProbeDB(t, chain)
	declareQuotaAuthorityAbsent(t, conn, j.schema)
	return conn
}

// dryRun исполняет fn под инициатором пробы и ОТКАТЫВАЕТ транзакцию: проверка
// предпосылки «изменение законно во всём, кроме инициатора». Стоит до отрицательного
// кейса — иначе отказ посторонним ограничением выдал бы себя за отказ по инициатору
// либо за его отсутствие.
func dryRun(conn *pgx.Conn, fn func(pgx.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, journalInitiatorSetting, probeInitiator); err != nil {
		return err
	}
	return fn(tx)
}

// inJournalTx исполняет fn в транзакции, первым оператором которой — установка
// инициатора локально к транзакции (если who != nil). Ошибка fn или фиксации
// откатывает транзакцию и возвращается как есть: часть утверждений ждёт отказа базы.
func inJournalTx(conn *pgx.Conn, who initiatorOf, fn func(pgx.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if who != nil {
		if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, journalInitiatorSetting, *who); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// execJournalTx — inJournalTx для одного оператора.
func execJournalTx(conn *pgx.Conn, who initiatorOf, stmt string, args ...any) error {
	return inJournalTx(conn, who, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), stmt, args...)
		return err
	})
}

// seed — посев фикстуры под инициатором пробы; отказ посева — отказ фикстуры, а не предмета.
func seed(t *testing.T, conn *pgx.Conn, stmt string, args ...any) {
	t.Helper()
	require.NoError(t, execJournalTx(conn, initiator(probeInitiator), stmt, args...),
		"фикстура: посев под инициатором %q обязан пройти — иначе проба судила бы посев, а не журнал", probeInitiator)
}

// pgErrorOf — отказ базы внутри err либо nil.
func pgErrorOf(err error) *pgconn.PgError {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr
	}
	return nil
}

// assertRefusedNotNull — отказ 23502 по названной колонке. Текст утверждения называет
// и то, что база сделала вместо отказа: «принято» неотличимо от «не спрашивали»
// только если об этом молчать.
func assertRefusedNotNull(t *testing.T, err error, column, what string) bool {
	t.Helper()
	if !assert.Error(t, err, "%s: ждали отказа %s по колонке %q, а база ПРИНЯЛА изменение", what, sqlStateNotNull, column) {
		return false
	}
	pgErr := pgErrorOf(err)
	if !assert.NotNil(t, pgErr, "%s: отказ обязан прийти от базы, пришло: %v", what, err) {
		return false
	}
	ok := assert.Equal(t, sqlStateNotNull, pgErr.Code, "%s: код отказа; текст базы: %s", what, pgErr.Message)
	return assert.Equal(t, column, pgErr.ColumnName, "%s: колонка отказа; текст базы: %s", what, pgErr.Message) && ok
}

// assertRefusedByInitiatorCheck — отказ 23514 от ограничения формы инициатора.
func assertRefusedByInitiatorCheck(t *testing.T, err error, what string) {
	t.Helper()
	if !assert.Error(t, err, "%s: ждали отказа %s от ограничения формы инициатора, а база ПРИНЯЛА строку", what, sqlStateCheck) {
		return
	}
	pgErr := pgErrorOf(err)
	if !assert.NotNil(t, pgErr, "%s: отказ обязан прийти от базы, пришло: %v", what, err) {
		return
	}
	assert.Equal(t, sqlStateCheck, pgErr.Code, "%s: код отказа; текст базы: %s", what, pgErr.Message)
	assert.Contains(t, pgErr.ConstraintName, journalInitiatorColumn,
		"%s: отказ обязан прийти от ИМЕНОВАННОГО ограничения формы инициатора, а не от соседнего; текст базы: %s", what, pgErr.Message)
}

// plpgsqlFrame — первая (самая глубокая) рамка PL/pgSQL в контексте отказа.
var plpgsqlFrame = regexp.MustCompile(`PL/pgSQL function (?:[a-z_]+\.)?([a-z_]+)\(`)

// innermostFunction — имя функции базы, в которой случился отказ, либо "".
func innermostFunction(err error) string {
	pgErr := pgErrorOf(err)
	if pgErr == nil {
		return ""
	}
	for _, line := range strings.Split(pgErr.Where, "\n") {
		if m := plpgsqlFrame.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return ""
}

// journalRowsFor — число строк журнала по ресурсу. Читается вне транзакций пробы.
func journalRowsFor(t *testing.T, conn *pgx.Conn, table, idColumn, id string) int {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE `+idColumn+` = $1`, id).Scan(&n))
	return n
}

// initiatorsOf — инициаторы строк журнала по ресурсу, по порядку записи. Ошибка
// чтения возвращается: до миграции колонки нет, и это наблюдаемый исход пробы.
func initiatorsOf(conn *pgx.Conn, table, idColumn, id string) ([]string, error) {
	rows, err := conn.Query(context.Background(),
		`SELECT `+journalInitiatorColumn+` FROM `+table+` WHERE `+idColumn+` = $1 ORDER BY sequence_no`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// requireTableColumn — проверка предпосылки: таблица журнала и её колонка есть, колонка
// NOT NULL. Стоит ДО пробы предмета: сломанная фикстура не вправе выдать себя за
// отсутствующую возможность.
func requireTableColumn(t *testing.T, conn *pgx.Conn, schema, table, column string) {
	t.Helper()
	var nullable string
	err := conn.QueryRow(context.Background(), `
		SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
		schema, table, column).Scan(&nullable)
	require.NoError(t, err, "предпосылка: колонка %s.%s.%s существует", schema, table, column)
	require.Equal(t, "NO", nullable, "предпосылка: колонка %s.%s.%s NOT NULL", schema, table, column)
}

// requireFunction — проверка предпосылки: функция базы, которую судит проба, есть в схеме.
func requireFunction(t *testing.T, conn *pgx.Conn, name string) {
	t.Helper()
	var n int
	require.NoError(t, conn.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_proc WHERE proname = $1`, name).Scan(&n))
	require.Equal(t, 1, n, "предпосылка: функция базы %s определена ровно один раз", name)
}

// journalTable — описание журнала модуля для проб NTF3-62 и УК3-28.
type journalTable struct {
	schema, name string // таблица журнала (`Mapping.Table` модуля)
	timeColumn   string // колонка времени строки (`AgeColumn` модуля)
	idColumn     string // колонка id ресурса
	// insert — оператор прямой вставки минимальной строки журнала по ресурсу $1;
	// колонок `initiator` и времени он не называет.
	insert string
	// insertNullTime — тот же оператор, но с явным NULL во времени строки.
	insertNullTime string
}

func (j journalTable) qualified() string { return j.schema + "." + j.name }

// runJournalRowWithoutInitiatorIsRefused — NTF3-62, первая часть, на таблице журнала модуля.
//
// Отрицательные кейсы меняют ровно один факт против положительного близнеца
// (настройка инициатора `user:usr-A` выставлена, время по умолчанию):
//   - настройка не выставлена — 23502 по `initiator`;
//   - выставлена пустой строкой — 23502 по `initiator` (`NULLIF` делает пустое отсутствием);
//   - выставлена `usr-A` без типа — 23514 от ограничения формы;
//   - время строки явно NULL — 23502 по колонке времени.
func runJournalRowWithoutInitiatorIsRefused(t *testing.T, conn *pgx.Conn, j journalTable) {
	t.Helper()
	requireTableColumn(t, conn, j.schema, j.name, j.timeColumn)

	t.Run("twin_user_usr-A_is_inserted_and_carries_it", func(t *testing.T) {
		id := "ntf362-twin"
		require.NoError(t, execJournalTx(conn, initiator(probeInitiator), j.insert, id),
			"положительный близнец: строка с инициатором %q и временем обязана вставиться — иначе отрицания ниже беспредметны", probeInitiator)
		got, err := initiatorsOf(conn, j.qualified(), j.idColumn, id)
		if assert.NoError(t, err, "строка близнеца обязана нести колонку %q", journalInitiatorColumn) {
			assert.Equal(t, []string{probeInitiator}, got, "строка близнеца несёт инициатора транзакции")
		}
	})
	t.Run("no_initiator_is_refused_23502", func(t *testing.T) {
		err := execJournalTx(conn, nil, j.insert, "ntf362-unset")
		assertRefusedNotNull(t, err, journalInitiatorColumn, "настройка инициатора не выставлена")
	})
	t.Run("empty_initiator_is_refused_23502", func(t *testing.T) {
		err := execJournalTx(conn, initiator(""), j.insert, "ntf362-empty")
		assertRefusedNotNull(t, err, journalInitiatorColumn, "настройка инициатора выставлена пустой строкой")
	})
	t.Run("untyped_initiator_is_refused_23514", func(t *testing.T) {
		err := execJournalTx(conn, initiator("usr-A"), j.insert, "ntf362-untyped")
		assertRefusedByInitiatorCheck(t, err, "инициатор `usr-A` без типа")
	})
	t.Run("no_time_is_refused_23502", func(t *testing.T) {
		err := execJournalTx(conn, initiator(probeInitiator), j.insertNullTime, "ntf362-notime")
		assertRefusedNotNull(t, err, j.timeColumn, "время строки журнала NULL")
	})
}

// runReusedConnectionDoesNotCarryInitiator — УК3-28: на соединении, где предыдущая
// транзакция выставила инициатора локально и зафиксировалась, транзакция без
// инициатора отвергнута 23502 так же, как на свежем соединении.
func runReusedConnectionDoesNotCarryInitiator(t *testing.T, conn *pgx.Conn, j journalTable) {
	t.Helper()
	requireTableColumn(t, conn, j.schema, j.name, j.timeColumn)

	require.NoError(t, execJournalTx(conn, initiator(probeInitiator), j.insert, "uk328-first"),
		"первая транзакция соединения: инициатор выставлен локально, строка вставлена")
	var after string
	require.NoError(t, conn.QueryRow(context.Background(),
		`SELECT coalesce(current_setting($1, true), '<null>')`, journalInitiatorSetting).Scan(&after))
	require.NotEqual(t, probeInitiator, after,
		"предпосылка: установка локальна к транзакции — после фиксации соединение её не видит")

	err := execJournalTx(conn, nil, j.insert, "uk328-second")
	assertRefusedNotNull(t, err, journalInitiatorColumn,
		"вторая транзакция того же соединения без инициатора (после фиксации настройка читается как "+after+")")
}

// functionProbe — NTF3-62 «через каждую из шести функций базы»: изменение, от которого
// функция срабатывает, в транзакции без инициатора откатывается 23502 по колонке
// инициатора, и само изменение не зафиксировано; близнец — то же изменение под
// `user:usr-A` зафиксировано, строка журнала несёт `user:usr-A`.
type functionProbe struct {
	function string // функция базы, пишущая журнал
	// prepare сеет условие под тегом и возвращает id ресурса, по которому функция пишет
	// строку журнала (`resource_id`).
	prepare func(t *testing.T, conn *pgx.Conn, tag string) string
	// change — изменение, от которого функция срабатывает, под тегом.
	change func(tx pgx.Tx, tag string) error
	// applied — зафиксировано ли изменение (наблюдаемо по таблице ресурса).
	applied func(t *testing.T, conn *pgx.Conn, tag string) bool
}

func runFunctionProbe(t *testing.T, conn *pgx.Conn, j journalTable, p functionProbe) {
	t.Helper()
	requireFunction(t, conn, p.function)

	t.Run("without_initiator_rolls_back_23502", func(t *testing.T) {
		tag := "neg"
		journalID := p.prepare(t, conn, tag)
		before := journalRowsFor(t, conn, j.qualified(), j.idColumn, journalID)
		require.False(t, p.applied(t, conn, tag), "предпосылка: до изменения оно не применено")
		require.NoError(t, dryRun(conn, func(tx pgx.Tx) error { return p.change(tx, tag) }),
			"предпосылка: то же изменение под инициатором %q законно во всём остальном (транзакция откачена)", probeInitiator)
		require.False(t, p.applied(t, conn, tag), "предпосылка: пробный прогон откачен и следа не оставил")

		err := inJournalTx(conn, nil, func(tx pgx.Tx) error { return p.change(tx, tag) })

		if err == nil {
			// Изменение зафиксировано. Говорим, сработала ли функция: «база приняла» и
			// «функция не сработала» — разные исходы, и путать их нельзя.
			grown := journalRowsFor(t, conn, j.qualified(), j.idColumn, journalID) - before
			assert.Failf(t, "транзакция без инициатора ЗАФИКСИРОВАНА",
				"функция %s записала строк журнала: %d; отказа %s по колонке %q нет",
				p.function, grown, sqlStateNotNull, journalInitiatorColumn)
			return
		}
		if assertRefusedNotNull(t, err, journalInitiatorColumn, "изменение без инициатора через "+p.function) {
			assert.Equal(t, p.function, innermostFunction(err),
				"отказ обязан прийти из функции %s, а не из соседней; контекст базы: %s", p.function, pgErrorOf(err).Where)
		}
		assert.False(t, p.applied(t, conn, tag), "изменение ресурса откатилось вместе с транзакцией")
		assert.Equal(t, before, journalRowsFor(t, conn, j.qualified(), j.idColumn, journalID),
			"строк журнала по ресурсу не прибавилось")
	})

	t.Run("twin_with_user_usr-A_commits_and_journal_carries_it", func(t *testing.T) {
		tag := "twin"
		journalID := p.prepare(t, conn, tag)
		before := journalRowsFor(t, conn, j.qualified(), j.idColumn, journalID)

		require.NoError(t, inJournalTx(conn, initiator(probeInitiator), func(tx pgx.Tx) error { return p.change(tx, tag) }),
			"близнец: то же изменение под инициатором %q зафиксировано", probeInitiator)
		require.True(t, p.applied(t, conn, tag), "близнец: изменение применено")
		grown := journalRowsFor(t, conn, j.qualified(), j.idColumn, journalID) - before
		require.Positive(t, grown, "предпосылка: изменение заставляет %s записать строку журнала по %s", p.function, journalID)

		got, err := initiatorsOf(conn, j.qualified(), j.idColumn, journalID)
		if assert.NoError(t, err, "строка журнала, записанная %s, обязана нести колонку %q", p.function, journalInitiatorColumn) {
			tail := got[len(got)-grown:]
			for _, v := range tail {
				assert.Equal(t, probeInitiator, v, "строка журнала, записанная %s, несёт инициатора транзакции", p.function)
			}
		}
	})
}
