// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// runner_test.go — unit-тесты на чистую логику Runner / Config, без обращения к
// БД. Реальный apply покрыт integration-suite'ом в `internal/repo/...`; тесты на
// Dialect-фабрику и spec'ы — в dialect_test.go.
package migrator

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestConfigValidate(t *testing.T) {
	fsys := fstest.MapFS{}
	pg, err := NewDialect("postgres")
	if err != nil {
		t.Fatalf("NewDialect(postgres) failed: %v", err)
	}
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		// Каждый случай опускает РОВНО ОДНО поле: иначе первая же проверка в
		// Validate заслоняет остальные, и таблица перестаёт различать, какое
		// именно поле она стережёт.
		{name: "missing dialect", cfg: Config{Service: "vpc", DSN: "x", FS: fsys, MigrationsDir: "."}, wantErr: "dialect"},
		{name: "missing dsn", cfg: Config{Service: "vpc", Dialect: pg, FS: fsys, MigrationsDir: "."}, wantErr: "dsn"},
		{name: "missing fs", cfg: Config{Service: "vpc", Dialect: pg, DSN: "x", MigrationsDir: "."}, wantErr: "migrations FS"},
		{name: "missing dir", cfg: Config{Service: "vpc", Dialect: pg, DSN: "x", FS: fsys}, wantErr: "migrations dir"},
		// Безымянный сервис — отказ старта, а не умолчание: живой счёт строк перед
		// сносом иначе не может назвать, что он стережёт.
		{name: "missing service", cfg: Config{Dialect: pg, DSN: "x", FS: fsys, MigrationsDir: "."}, wantErr: "service is empty"},
		{name: "ok", cfg: Config{Service: "vpc", Dialect: pg, DSN: "x", FS: fsys, MigrationsDir: "."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to contain %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for empty config, got nil")
	}
}

// TestParseTargetVersion снят ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ (#1383): локальной
// parseTargetVersion в пакете больше нет — разбор `--target` у всех семи точек
// наката один, [migratorcli.ParseTargetVersion], и его пробы строго ШИРЕ снятой:
// снятая утверждала «10», «0010», «12345», «abc»✗, «-5»✗, «""»✗ — все шесть
// утверждений живы в `pkg/migratorcli/parse_test.go` и
// `pkg/migratorcli/parsetarget_test.go`.
//
// Замечание, ради которого эта врезка оставлена, а не просто удалён код: снятая
// проба «abc» проверяла, а «12abc» — НЕТ, и потому не ловила настоящую дыру
// прежнего разбора: `fmt.Sscanf("12abc", "%d", &v)` возвращает 12 БЕЗ ошибки,
// то есть накат уезжал к версии, которой оператор не называл. Проба была, и
// была зелёной, и предмета не измеряла.
