// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pgdsn

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// Каждая форма DSN пробы — URL и ключевая запись, с особыми символами в
// учётных данных и с параметрами сервера, — после подмены базы читается
// драйвером с ТОЙ ЖЕ учёткой, узлом, портом и параметрами; меняется ровно
// база.
func TestWithDatabaseChangesOnlyTheDatabaseTheDriverReads(t *testing.T) {
	for _, dsn := range []string{
		"postgres://vpc:vpc@localhost:32768/kacho_vpc_admin?sslmode=disable",
		"postgres://u%40x:p%40ss%2Fw%3Frd%231%252@db:5432/kacho_a?sslmode=disable&options=-c%20search_path%3Dkacho_a%2Cpublic",
		"host=db port=6432 user=u password='a b:c' dbname=kacho_a sslmode=disable application_name=probe",
	} {
		before, err := pgconn.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("вход пробы не разобран драйвером: %v", err)
		}
		got, err := WithDatabase(dsn, "kacho_other")
		if err != nil {
			t.Fatalf("WithDatabase(%q): %v", dsn, err)
		}
		after, err := pgconn.ParseConfig(got)
		if err != nil {
			t.Fatalf("результат %q не разобран драйвером: %v", got, err)
		}
		if after.Database != "kacho_other" {
			t.Errorf("%q: база %q, ожидалось kacho_other", dsn, after.Database)
		}
		if after.User != before.User || after.Password != before.Password ||
			after.Host != before.Host || after.Port != before.Port {
			t.Errorf("%q: учётка или адрес изменились: %s@%s:%d → %s@%s:%d",
				dsn, before.User, before.Host, before.Port, after.User, after.Host, after.Port)
		}
		if after.TLSConfig != nil || len(after.Fallbacks) != 0 {
			t.Errorf("%q: результат несёт TLS или запасные узлы — канал изменился", dsn)
		}
		for k, v := range before.RuntimeParams {
			if after.RuntimeParams[k] != v {
				t.Errorf("%q: параметр %s=%q потерян (стало %q)", dsn, k, v, after.RuntimeParams[k])
			}
		}
	}
}

// Отказы: строка, которую драйвер не разбирает, и строка с каналом, который
// подмена не воспроизвела бы (TLS, запасные узлы). Текст отказа не несёт
// кусков строки соединения.
func TestWithDatabaseRefusesWithoutLeakingTheDSN(t *testing.T) {
	for _, dsn := range []string{
		"postgres://u:Secr3tPw@db:5432/kacho_a?sslmode=require",
		"postgres://u:Secr3tPw@db1:5432,db2:5432/kacho_a?sslmode=disable",
		"postgres://u:Secr3tPw@db:5432/kacho_a?sslmode=prefer",
		"postgres://u:Secr3tPw@db:5432/kacho_a?sslmode=disable&connect_timeout=zz",
	} {
		got, err := WithDatabase(dsn, "kacho_other")
		if err == nil {
			t.Errorf("%q: отказа нет, результат %q", dsn, got)
			continue
		}
		for _, piece := range []string{"Secr3tPw", "db1", "kacho_a", "u:"} {
			if strings.Contains(err.Error(), piece) {
				t.Errorf("%q: текст отказа несёт %q: %v", dsn, piece, err)
			}
		}
	}
}
