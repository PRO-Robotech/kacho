// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrationchains_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// writeFile кладёт файл синтетического дерева, создавая каталоги.
func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("каталог %s: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		t.Fatalf("файл %s: %v", rel, err)
	}
}

// point кладёт точку наката службы svc.
func point(t *testing.T, root, svc string) {
	t.Helper()
	writeFile(t, root, "services/"+svc+"/cmd/migrator/main.go", "package main\n")
}

// chain кладёт одну миграцию в каталог цепочки dir.
func chain(t *testing.T, root, dir string) {
	t.Helper()
	writeFile(t, root, dir+"/0001_init.sql", "-- +goose Up\nSELECT 1;\n")
}

func TestPointWithoutTableIsOneChainThatDoesNotChooseByDatabase(t *testing.T) {
	root := t.TempDir()
	point(t, root, "alpha")
	chain(t, root, "services/alpha/internal/migrations")

	got, err := migrationchains.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := migrationchains.Chain{
		Service: "alpha", Point: "services/alpha/cmd/migrator",
		Database: "", Dir: "services/alpha/internal/migrations",
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("цепочки = %+v, ожидалась одна %+v", got, want)
	}
}

func TestTableRowsAreTheChainsOfThePoint(t *testing.T) {
	root := t.TempDir()
	point(t, root, "beta")
	chain(t, root, "services/beta/internal/probemigrations")
	writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
		"chains:\n  - database: kacho_betaprobe\n    dir: services/beta/internal/probemigrations\n")

	got, err := migrationchains.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := migrationchains.Chain{
		Service: "beta", Point: "services/beta/cmd/migrator",
		Database: "kacho_betaprobe", Dir: "services/beta/internal/probemigrations",
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("цепочки = %+v, ожидалась %+v", got, want)
	}
}

// Отказы List — каждый называет предмет. Близнец каждого — дерево двух
// тестов выше, где List отвечает перечнем.
func TestListRefusals(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, root string)
		says  []string
	}{
		{
			name:  "точек ноль",
			build: func(t *testing.T, root string) { chain(t, root, "services/alpha/internal/migrations") },
			says:  []string{"точек наката нет"},
		},
		{
			name: "таблица без строк",
			build: func(t *testing.T, root string) {
				point(t, root, "beta")
				writeFile(t, root, "services/beta/cmd/migrator/chains.yaml", "chains: []\n")
			},
			says: []string{"services/beta/cmd/migrator", "строк 0"},
		},
		{
			name: "строка без имени базы",
			build: func(t *testing.T, root string) {
				point(t, root, "beta")
				chain(t, root, "services/beta/internal/probemigrations")
				writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
					"chains:\n  - dir: services/beta/internal/probemigrations\n")
			},
			says: []string{"services/beta/cmd/migrator", "database"},
		},
		{
			name: "каталог строки вне службы точки",
			build: func(t *testing.T, root string) {
				point(t, root, "beta")
				chain(t, root, "services/gamma/internal/migrations")
				writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
					"chains:\n  - database: kacho_beta\n    dir: services/gamma/internal/migrations\n")
			},
			says: []string{"services/gamma/internal/migrations", "services/beta/internal/"},
		},
		{
			name: "каталога строки нет",
			build: func(t *testing.T, root string) {
				point(t, root, "beta")
				writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
					"chains:\n  - database: kacho_beta\n    dir: services/beta/internal/migrations\n")
			},
			says: []string{"services/beta/internal/migrations", "нет"},
		},
		{
			name: "две строки об одной базе",
			build: func(t *testing.T, root string) {
				point(t, root, "beta")
				chain(t, root, "services/beta/internal/migrations")
				chain(t, root, "services/beta/internal/probemigrations")
				writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
					"chains:\n  - database: kacho_beta\n    dir: services/beta/internal/migrations\n"+
						"  - database: kacho_beta\n    dir: services/beta/internal/probemigrations\n")
			},
			says: []string{"kacho_beta", "дважды"},
		},
		{
			name: "цепочка без точки наката",
			build: func(t *testing.T, root string) {
				point(t, root, "alpha")
				chain(t, root, "services/alpha/internal/migrations")
				chain(t, root, "services/delta/internal/migrations")
			},
			says: []string{"services/delta/internal/migrations", "без точки наката"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.build(t, root)
			got, err := migrationchains.List(root)
			if err == nil {
				t.Fatalf("List ответил перечнем %+v там, где обязан отказать", got)
			}
			for _, s := range tc.says {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("отказ не называет %q: %v", s, err)
				}
			}
		})
	}
}

// TestTheTreeHasItsChains — перечень цепочек дерева: печать на строку и
// число цепочек по каталогу notify. Ноль цепочек notify — красный: обе базы
// каталога (kacho_notifyprobe — D4, kacho_notify — N7) накатывает его точка.
func TestTheTreeHasItsChains(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("корень: %v", err)
	}
	chains, err := migrationchains.List(root)
	if err != nil {
		t.Fatalf("List дерева: %v", err)
	}
	notify := 0
	for _, c := range chains {
		db := c.Database
		if db == "" {
			db = "(по имени базы не выбирает)"
		}
		t.Logf("  %-12s %-28s %-30s %s", c.Service, c.Point, db, c.Dir)
		if c.Service == "notify" {
			notify++
		}
	}
	t.Logf("перепись: цепочек %d, из них по services/notify %d", len(chains), notify)
	if notify == 0 {
		t.Fatal("по services/notify цепочек 0 — точка наката каталога не видна перечню")
	}
}

// DatabaseOf читает имя базы из обеих записей DSN, которыми зовут точку
// наката, и не берёт его ниоткуда больше (окружение PGDATABASE — не источник).
func TestDatabaseOfReadsBothDSNForms(t *testing.T) {
	t.Setenv("PGDATABASE", "kacho_from_env")
	cases := map[string]string{
		"postgres://u:p@db:5432/kacho_notifyprobe?sslmode=require":                     "kacho_notifyprobe",
		"postgresql://u@db/kacho_notify":                                               "kacho_notify",
		"host=db port=5432 user=u password=p dbname=kacho_notifyprobe sslmode=require": "kacho_notifyprobe",
		"host=db password='a b\\' c' dbname = 'kacho_x'":                               "kacho_x",
	}
	for dsn, want := range cases {
		got, err := migrationchains.DatabaseOf(dsn)
		if err != nil || got != want {
			t.Errorf("DatabaseOf(%q) = %q, %v; ожидалось %q", dsn, got, err, want)
		}
	}
	for _, dsn := range []string{"host=db user=u", "postgres://u@db:5432/", "host=db dbname='open", "::bad"} {
		if got, err := migrationchains.DatabaseOf(dsn); err == nil {
			t.Errorf("DatabaseOf(%q) = %q без отказа — имя базы взято не из DSN", dsn, got)
		}
	}
}
