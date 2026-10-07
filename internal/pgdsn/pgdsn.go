// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pgdsn — строка соединения Postgres пробы с другой базой, прочитанная
// разбором драйвера.
//
// Строку соединения читает ТОЛЬКО [pgconn.ParseConfig] — тот же разбор, каким
// драйвер соединяется. Разбор адресной библиотекой (`net/url`) знает одну форму
// записи из двух (ключевую запись он не читает вовсе), не знает второго
// источника имени базы (query-параметр `dbname`) и несёт строку целиком в тексте
// своей ошибки (Д93; тот же класс закрыт в `internal/migrationchains.DatabaseOf`).
//
// Обратного сериализатора у драйвера нет, поэтому результат СОБИРАЕТСЯ из
// разобранных полей, а не правится в исходной строке. Собирается ровно то, что
// подмена воспроизводит без потерь: один узел, канал без TLS, параметры
// сервера. Строка, канал которой подмена не воспроизвела бы (TLS, запасные
// узлы, сокет), — отказ, а не строка с молча изменённым каналом.
package pgdsn

import (
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// WithDatabase — dsn с базой name: учётка, узел, порт и параметры сервера —
// те, что драйвер читает из dsn; меняется ровно база.
//
// Текст отказа не несёт кусков dsn: ошибка разбора драйвера цитирует строку
// соединения вместе с паролем, и она не печатается.
func WithDatabase(dsn, name string) (string, error) {
	c, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return "", errors.New("pgdsn: строка соединения не разобрана драйвером; " +
			"текст разбора не печатается — он несёт куски строки соединения")
	}
	if c.TLSConfig != nil || len(c.Fallbacks) != 0 {
		return "", errors.New("pgdsn: строка соединения несёт TLS либо запасные узлы — " +
			"подмена базы воспроизводит только канал без TLS к одному узлу (sslmode=disable)")
	}
	if strings.HasPrefix(c.Host, "/") {
		return "", errors.New("pgdsn: строка соединения ведёт к сокету — подмена базы " +
			"воспроизводит только сетевой узел")
	}
	q := url.Values{"sslmode": []string{"disable"}}
	if c.ConnectTimeout > 0 {
		q.Set("connect_timeout", strconv.Itoa(int(c.ConnectTimeout.Seconds())))
	}
	keys := make([]string, 0, len(c.RuntimeParams))
	for k := range c.RuntimeParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		q.Set(k, c.RuntimeParams[k])
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Path:     "/" + name,
		RawQuery: q.Encode(),
	}
	return u.String(), nil
}
