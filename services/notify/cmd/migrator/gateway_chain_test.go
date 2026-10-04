// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// gateway_chain_test.go — полоса N7 (kacho#2915, замысел З24, §6; Д74, Д84):
// база шлюза `kacho_notify` — вторая строка таблицы точки наката, и цепочка
// шлюза накатывается этой же точкой.
//
// Пробы стоят на ТЕХ ЖЕ входах, что пробы цепочки пробы-источника рядом
// (main_test.go): таблица `chains.yaml`, встроенная `embedded`, процесс-помощник
// `TestHelperRunsMain` на живой базе pgtest. Своей копии таблицы у пробы нет.
//
// Близнец каждой пробы — уже зелёная проба цепочки `kacho_notifyprobe`
// (TestProbeDatabaseSelectsTheProbeChain, TestPointConfirmsTheConnectedDatabaseBeforeGoose):
// мир пробы отличается от него одним фактом — именем базы строки.

import (
	"database/sql"
	"io/fs"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// gatewayDir — каталог цепочки шлюза от корня (строка N7 tasks.md:
// `services/notify/internal/migrations/**`).
const gatewayDir = "services/notify/internal/migrations"

// TestMigrator_NTF1H03_NotifyDatabaseSelectsTheGatewayChain — DSN с базой
// `kacho_notify` в обеих записях, которыми точку зовут, выбирает строку шлюза,
// и встроенная FS этой строки несёт миграции.
func TestMigrator_NTF1H03_NotifyDatabaseSelectsTheGatewayChain(t *testing.T) {
	for _, dsn := range []string{
		"postgres://u:p@db:5432/kacho_notify?sslmode=require",
		"host=db port=5432 user=u password=p dbname=kacho_notify sslmode=require",
	} {
		c, fsys, err := selectChain(chainsTable, embedded, dsn)
		if err != nil {
			t.Fatalf("%q: отказ точки — строки kacho_notify в таблице нет: %v", dsn, err)
		}
		if c.Database != "kacho_notify" || c.Dir != gatewayDir {
			t.Fatalf("%q: выбрана строка %+v, ожидалась {kacho_notify, %s}", dsn, c, gatewayDir)
		}
		if m, err := fs.Glob(fsys, "*.sql"); err != nil || len(m) == 0 {
			t.Fatalf("%q: во встроенной FS строки шлюза нет миграций (%v)", dsn, err)
		}
	}
	// Таблица — ровно две строки (печать числа цепочек по services/notify — 2).
	rows, err := migrationchains.ParseTable(pointDir, chainsTable)
	if err != nil {
		t.Fatalf("таблица точки не разобрана: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("строк таблицы точки %d, ожидалось 2 (kacho_notifyprobe, kacho_notify): %+v", len(rows), rows)
	}
}

// helperDirEnv — каталог строки, которую процесс-помощник подставляет вместо
// таблицы (в паре с helperRowEnv): накатываемая цепочка — шлюза, а не пробы.
const helperDirEnv = "KACHO_NOTIFY_MIGRATOR_TEST_ROW_DIR"

// TestMigrator_NTF1H03_GatewayChainCreatesTheLimitsSchema — точка, исполненная
// процессом против пустой базы pgtest со строкой «эта база → цепочка шлюза»,
// накатывает схему §6: `recipient_net`, `global_daily`, `recipient_key_fence`
// с инвариантами уровня базы (ban #10). Применено = объявлено цепочкой.
//
// Инварианты утверждаются ИСХОДОМ оператора, а не чтением каталога: счётчик
// ниже нуля, вторая строка ограды и отпечаток не в 16 байт — отказ базы.
// Адреса открытым текстом схема не несёт: колонок типа text, кроме имени класса
// и окна, у `recipient_net` нет (NTF1-H03).
func TestMigrator_NTF1H03_GatewayChainCreatesTheLimitsSchema(t *testing.T) {
	dsn := pgtest.NewEmptyDB(t)
	db, err := migrationchains.DatabaseOf(dsn)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: имя базы pgtest не разобрано: %v", err)
	}
	stderr, err := runPoint(t, dsn, helperRowEnv+"="+db, helperDirEnv+"="+gatewayDir)
	if err != nil {
		t.Fatalf("накат цепочки шлюза в %s отвергнут (%v):\n%s", db, err, stderr)
	}
	sqlFS, ok := embedded[gatewayDir]
	if !ok {
		t.Fatalf("встроенной FS цепочки шлюза %s у точки нет", gatewayDir)
	}
	want, err := fs.Glob(sqlFS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	exists, n := gooseState(t, dsn)
	if !exists || n != len(want) {
		t.Fatalf("применено %d (таблица goose есть: %v), объявлено цепочкой шлюза %d", n, exists, len(want))
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: база не открыта: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Положительный близнец каждого отказа ниже — та же вставка годным значением.
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("годная вставка отвергнута %q: %v", q, err)
		}
	}
	mustRefuse := func(what, q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err == nil {
			t.Errorf("%s: база приняла %q — инварианта уровня базы нет", what, q)
		}
	}
	key := make([]byte, 32)
	// Колонки — в порядке §6 (key, class, окно, window_start, count): имя колонки
	// окна проба не утверждает — `window` в Postgres зарезервировано.
	mustExec(`INSERT INTO recipient_net VALUES ($1, 'security', 'day', now(), 1)`, key)
	mustRefuse("счётчик сетки ниже нуля", `INSERT INTO recipient_net VALUES ($1, 'notice', 'hour', now(), -1)`, key)
	mustExec(`INSERT INTO global_daily (day, count) VALUES (current_date, 1)`)
	mustRefuse("потолок потока ниже нуля", `UPDATE global_daily SET count = -1`)
	mustExec(`INSERT INTO recipient_key_fence (fingerprint) VALUES ($1)`, make([]byte, 16))
	mustRefuse("вторая строка ограды", `INSERT INTO recipient_key_fence (singleton, fingerprint) VALUES (false, $1)`, make([]byte, 16))
	mustRefuse("отпечаток не в 16 байт", `UPDATE recipient_key_fence SET fingerprint = $1`, make([]byte, 32))

	rowsText, err := conn.Query(`SELECT column_name FROM information_schema.columns
		WHERE table_name = 'recipient_net' AND data_type IN ('text', 'character varying')`)
	if err != nil {
		t.Fatalf("каталог колонок не прочитан: %v", err)
	}
	defer func() { _ = rowsText.Close() }()
	var extra []string
	for rowsText.Next() {
		var c string
		if err := rowsText.Scan(&c); err != nil {
			t.Fatal(err)
		}
		extra = append(extra, c)
	}
	// Текстовых колонок две — класс и окно; третья — место для адреса.
	if len(extra) > 2 {
		t.Errorf("у recipient_net текстовых колонок %d (%s), ожидалось не больше двух (класс, окно) — "+
			"место для адреса открытым текстом", len(extra), strings.Join(extra, ", "))
	}
	t.Logf("цепочка шлюза в %s: применено %d / объявлено %d", db, n, len(want))
}
