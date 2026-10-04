// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package migrationchains_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
)

// listSynthetic — перечень цепочек синтетического дерева во временном каталоге.
func listSynthetic(t *testing.T, root string) ([]migrationchains.Chain, error) {
	t.Helper()
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	return migrationchains.FromTree(tree)
}

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

	got, err := listSynthetic(t, root)
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

	got, err := listSynthetic(t, root)
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
			got, err := listSynthetic(t, root)
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
	chains, census, err := migrationchains.Survey(root)
	if err != nil {
		t.Fatalf("List дерева: %v", err)
	}
	t.Logf("поиск сирот: файлов .sql под services/*/internal/** %d, каталогов %d", census.SQLFiles, census.SQLDirs)
	if census.SQLFiles == 0 || census.SQLDirs < len(chains) {
		t.Fatalf("поиск сирот осмотрел %d файлов в %d каталогах при %d цепочках — обход не видит "+
			"даже каталогов перечня", census.SQLFiles, census.SQLDirs, len(chains))
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

// DatabaseOf читает имя базы тем же разбором, каким соединяется драйвер
// (pgconn.ParseConfig, Д83): в обеих записях DSN и при втором источнике имени в
// одной строке — query-параметр dbname/database у URL, ключ database у
// ключевой формы — решает то имя, к которому драйвер и соединится.
func TestDatabaseOfReadsBothDSNForms(t *testing.T) {
	t.Setenv("PGDATABASE", "")
	cases := map[string]string{
		// Близнецы: один источник имени.
		"postgres://u:p@db:5432/kacho_notifyprobe?sslmode=require":                     "kacho_notifyprobe",
		"postgresql://u@db/kacho_notify":                                               "kacho_notify",
		"host=db port=5432 user=u password=p dbname=kacho_notifyprobe sslmode=require": "kacho_notifyprobe",
		"host=db password='a b\\' c' dbname = 'kacho_x'":                               "kacho_x",
		// Второй источник имени (DSN-DB-DIVERGENCE): драйвер соединяется по нему.
		"postgres://u@h/kacho_notifyprobe?dbname=kacho_notify":   "kacho_notify",
		"postgres://u@h/kacho_notifyprobe?database=kacho_notify": "kacho_notify",
		"dbname=kacho_notifyprobe database=kacho_notify":         "kacho_notify",
	}
	for dsn, want := range cases {
		got, err := migrationchains.DatabaseOf(dsn)
		if err != nil || got != want {
			t.Errorf("DatabaseOf(%q) = %q, %v; ожидалось %q", dsn, got, err, want)
		}
	}
	for _, dsn := range []string{"host=db user=u", "postgres://u@db:5432/", "host=db dbname='open", "::bad"} {
		if got, err := migrationchains.DatabaseOf(dsn); err == nil {
			t.Errorf("DatabaseOf(%q) = %q без отказа — DSN базы не называет", dsn, got)
		}
	}
}

// DatabaseOf и драйвер — один разбор: окружение PGDATABASE, которое драйвер
// читает при DSN без имени базы, решает и выбор цепочки. Близнец — DSN с
// именем базы: окружение его не перебивает.
func TestDatabaseOfFollowsTheDriverOnEnvironment(t *testing.T) {
	t.Setenv("PGDATABASE", "kacho_from_env")
	if got, err := migrationchains.DatabaseOf("host=db user=u"); err != nil || got != "kacho_from_env" {
		t.Errorf("DSN без имени при PGDATABASE: %q, %v; драйвер соединится с kacho_from_env", got, err)
	}
	if got, err := migrationchains.DatabaseOf("host=db user=u dbname=kacho_notifyprobe"); err != nil || got != "kacho_notifyprobe" {
		t.Errorf("DSN с именем при PGDATABASE: %q, %v; ожидалось kacho_notifyprobe", got, err)
	}
}

// SEC-W1-01 — текст отказа разбора не несёт ни строки DSN, ни её кусков, в
// обеих записях. Маркеры — пароль целиком и его части, узел и пользователь.
func TestDatabaseOfRefusalCarriesNoPieceOfTheDSN(t *testing.T) {
	t.Setenv("PGDATABASE", "")
	cases := map[string][]string{
		"postgres://notifyprobe:Sup3r%Secret@db-host-x:5432/kacho_notifyprobe?sslmode=require": {
			"Sup3r", "Secret", "%Se", "notifyprobe:", "db-host-x", "postgres://"},
		"host=db-host-x port=5432 user=notify password=Tail0f Secret dbname='open": {
			"Tail0f", "Secret", "db-host-x", "notify", "password", "host="},
		"host=db-host-x user=u password='Tail0f Secret": {
			"Tail0f", "Secret", "db-host-x", "password", "host="},
	}
	for dsn, markers := range cases {
		_, err := migrationchains.DatabaseOf(dsn)
		if err == nil {
			t.Errorf("DSN %q принят — проба отказа без отказа", dsn)
			continue
		}
		for _, m := range markers {
			if strings.Contains(err.Error(), m) {
				t.Errorf("отказ разбора несёт кусок DSN %q: %v", m, err)
			}
		}
	}
}

// GS-C3 — DatabaseOf и соединение наката — ОДИН разбор: DSN, который разбор
// соединения (`migratorcli.OpenDB` → `pgx.ParseConfig`) отвергает, DatabaseOf
// тоже отвергает, и отказ остаётся у DatabaseOf — без кусков строки. Иначе
// DSN проходит выбор цепочки, а отказ разбора приходит позже, текстом драйвера
// с узлом, пользователем и базой. Ключи — три собственных ключа драйвера поверх
// libpq (statement_cache_capacity, description_cache_capacity,
// default_query_exec_mode) в обеих записях. Близнец каждого — тот же DSN с
// годным значением ключа: принят с именем базы.
func TestDatabaseOfRefusesWhatTheConnectionRefuses(t *testing.T) {
	t.Setenv("PGDATABASE", "")
	cases := []struct{ bad, good string }{
		{"postgres://u:s3cr%40t@db-host-x:5432/kacho_notifyprobe?statement_cache_capacity=zz",
			"postgres://u:s3cr%40t@db-host-x:5432/kacho_notifyprobe?statement_cache_capacity=10"},
		{"postgres://u@db-host-x/kacho_notifyprobe?description_cache_capacity=zz",
			"postgres://u@db-host-x/kacho_notifyprobe?description_cache_capacity=10"},
		{"host=db-host-x user=u dbname=kacho_notifyprobe default_query_exec_mode=zz",
			"host=db-host-x user=u dbname=kacho_notifyprobe default_query_exec_mode=exec"},
	}
	for _, c := range cases {
		if got, err := migrationchains.DatabaseOf(c.good); err != nil || got != "kacho_notifyprobe" {
			t.Errorf("близнец %q: %q, %v; ожидалось kacho_notifyprobe", c.good, got, err)
		}
		got, err := migrationchains.DatabaseOf(c.bad)
		if err == nil {
			t.Errorf("DSN %q принят с базой %q, а соединение наката его отвергнет текстом драйвера "+
				"с кусками строки — разборов два", c.bad, got)
			continue
		}
		for _, m := range []string{"db-host-x", "kacho_notifyprobe", "s3cr", "u@", "zz"} {
			if strings.Contains(err.Error(), m) {
				t.Errorf("отказ DatabaseOf(%q) несёт кусок DSN %q: %v", c.bad, m, err)
			}
		}
	}
}

// ORPHAN-CHAIN-BLIND — сирота-цепочка видна ВНЕ канонического каталога: любой
// `.sql` под services/<svc>/internal/** вне каталогов перечня — отказ с
// каталогом. Инъекция — строка таблицы снята при файлах цепочки на месте
// (каталог probemigrations-формы) и файл в произвольном каталоге internal/.
// Близнец — тот же файл в каталоге строки таблицы: перечень без отказа.
func TestOrphanSQLAnywhereUnderInternalIsRefused(t *testing.T) {
	for name, stray := range map[string]string{
		"строка снята, файлы на месте": "services/beta/internal/probemigrations",
		"каталог вне канона":           "services/beta/internal/strayq",
		"вложенный каталог":            "services/beta/internal/repo/sql",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			point(t, root, "beta")
			chain(t, root, "services/beta/internal/migrations")
			writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
				"chains:\n  - database: kacho_beta\n    dir: services/beta/internal/migrations\n")
			chain(t, root, stray)
			got, err := listSynthetic(t, root)
			if err == nil {
				t.Fatalf("сирота %s не увидена: перечень %+v", stray, got)
			}
			for _, s := range []string{stray, "без точки наката"} {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("отказ не называет %q: %v", s, err)
				}
			}
		})
	}
	t.Run("близнец: файл в каталоге строки", func(t *testing.T) {
		root := t.TempDir()
		point(t, root, "beta")
		chain(t, root, "services/beta/internal/migrations")
		chain(t, root, "services/beta/internal/probemigrations")
		writeFile(t, root, "services/beta/cmd/migrator/chains.yaml",
			"chains:\n  - database: kacho_beta\n    dir: services/beta/internal/migrations\n"+
				"  - database: kacho_betaprobe\n    dir: services/beta/internal/probemigrations\n")
		if got, err := listSynthetic(t, root); err != nil || len(got) != 2 {
			t.Fatalf("перечень %+v, %v; ожидались две цепочки без отказа", got, err)
		}
	})
}

// CX1-125 (Д93) — ветка отказа по каждой записи DSN, которую разбор драйвера
// ОТВЕРГАЕТ, и отдельно — запись, которую он принимает без имени базы. Маркер
// пароля стоит хвостом: в ключевой записи драйвер маскирует только значение
// `password=` и печатает хвост `MARKERTAIL` как есть, в записи URL — `%` без
// двух знаков. Проба печатает ветку («разбор» | «нет базы») и требует ту,
// которую даёт драйвер; ни одна подстрока маркера длиной ≥ 4 в текст не
// попадает, и отказ ничего не оборачивает (`errors.Unwrap == nil`): обёрнутая
// ошибка драйвера несла бы хвост маркера в журнал init-контейнера.
func TestDatabaseOfRefusalBranchCarriesNoMarker(t *testing.T) {
	t.Setenv("PGDATABASE", "")
	const marker = "MARKERTAIL"
	cases := []struct{ form, dsn, branch string }{
		{"ключевая, хвост пароля последним", "host=db-host-x user=u dbname=kacho_notify password=abc " + marker, "разбор"},
		{"URL, % без двух знаков", "postgres://u:abc%" + marker + "@db-host-x/kacho_notify", "разбор"},
		{"ключевая, хвост пароля перед dbname", "host=db-host-x user=u password=abc " + marker + " dbname=kacho_notify", "нет базы"},
	}
	for _, c := range cases {
		got, err := migrationchains.DatabaseOf(c.dsn)
		if err == nil {
			t.Errorf("%s: DSN принят с базой %q — проба отказа без отказа", c.form, got)
			continue
		}
		branch := "иная"
		switch {
		case strings.HasPrefix(err.Error(), "DSN не разобран драйвером"):
			branch = "разбор"
		case strings.HasPrefix(err.Error(), "DSN не называет базу"):
			branch = "нет базы"
		}
		t.Logf("%s: ветка отказа %q", c.form, branch)
		if branch != c.branch {
			t.Errorf("%s: ветка отказа %q, драйвер даёт %q: %v", c.form, branch, c.branch, err)
		}
		if errors.Unwrap(err) != nil {
			t.Errorf("%s: отказ оборачивает ошибку (%v) — текст драйвера уходит в журнал вместе с ним", c.form, errors.Unwrap(err))
		}
		for i := 0; i+4 <= len(marker); i++ {
			for j := i + 4; j <= len(marker); j++ {
				if s := marker[i:j]; strings.Contains(err.Error(), s) {
					t.Errorf("%s: отказ несёт кусок маркера %q: %v", c.form, s, err)
				}
			}
		}
	}
}
