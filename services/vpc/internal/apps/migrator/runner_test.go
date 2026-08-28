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

func TestParseTargetVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "10", want: 10},
		{in: "0010", want: 10}, // leading zeros — file-naming convention goose
		{in: "12345", want: 12345},
		{in: "abc", wantErr: true},
		{in: "-5", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseTargetVersion(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %d", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}
