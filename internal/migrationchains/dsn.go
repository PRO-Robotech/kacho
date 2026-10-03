// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrationchains

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// DatabaseOf — имя базы, к которой соединится драйвер по этому DSN (Д83).
//
// Разбор — [pgconn.ParseConfig], ТОТ ЖЕ, каким общий тракт наката
// (`corelib/migratorcli.OpenDB` → `pgx.ParseConfig`) строит соединение. Своего
// разбора здесь нет: два разбора одной строки расходились (DSN-DB-DIVERGENCE) —
// при втором источнике имени в строке (query-параметр `dbname`/`database` у
// URL, ключ `database` у ключевой формы) точка выбирала цепочку по одному имени,
// а драйвер соединялся с другим, и цепочка уезжала в чужую базу. Окружение
// (`PGDATABASE`), которое драйвер читает при DSN без имени, решает и здесь — по
// той же причине: решение о цепочке судит ту базу, к которой будет соединение.
//
// Имя пусто (ни строка, ни окружение его не называют) — отказ, а не пустое имя.
//
// Текст отказа разбора — только вид отказа: ни строки DSN, ни её кусков. Текст
// ошибки драйвера сюда не переносится НИ целиком, НИ обёрткой: маска пароля у
// драйвера частичная (пароль с `@` или `%` уходит хвостом в «узел»), а обёрнутая
// ошибка `url.Parse` несёт строку целиком (SEC-W1-01). Отказ уходит в журнал
// init-контейнера, который читает каждый с правом `pods/log`.
func DatabaseOf(dsn string) (string, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return "", errors.New("DSN не разобран драйвером (" + dsnForm(dsn) + "); " +
			"текст разбора не печатается — он несёт куски строки соединения")
	}
	if cfg.Database == "" {
		return "", errors.New("DSN не называет базу: имени нет ни в строке, ни в окружении PGDATABASE")
	}
	return cfg.Database, nil
}

// dsnForm — запись DSN для текста отказа: вид, не содержимое.
func dsnForm(dsn string) string {
	s := strings.TrimSpace(dsn)
	if strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://") {
		return "запись URL"
	}
	return "ключевая запись libpq"
}
