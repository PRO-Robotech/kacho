// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/probemigrations"
)

// firstSQL — метка встроенной FS цепочки пробы: первый файл миграции.
func firstSQL(t *testing.T, fsys fs.FS) string {
	t.Helper()
	m, err := fs.Glob(fsys, "*.sql")
	if err != nil || len(m) == 0 {
		t.Fatalf("в FS цепочки нет миграций (%v)", err)
	}
	return m[0]
}

// Проба точки (Д74): DSN с базой kacho_notifyprobe → цепочка пробы, в обеих
// записях DSN, которыми точку зовут (URL пробы и ключевая форма манифеста).
func TestProbeDatabaseSelectsTheProbeChain(t *testing.T) {
	want := firstSQL(t, probemigrations.FS)
	for _, dsn := range []string{
		"postgres://u:p@db:5432/kacho_notifyprobe?sslmode=require",
		"host=db port=5432 user=u password=p dbname=kacho_notifyprobe sslmode=require",
	} {
		c, fsys, err := selectChain(chainsTable, embedded, dsn)
		if err != nil {
			t.Fatalf("%q: отказ %v", dsn, err)
		}
		if c.Database != "kacho_notifyprobe" || c.Dir != "services/notify/internal/probemigrations" {
			t.Fatalf("%q: выбрана строка %+v", dsn, c)
		}
		if got := firstSQL(t, fsys); got != want {
			t.Fatalf("%q: FS выбранной строки не цепочка пробы (%s против %s)", dsn, got, want)
		}
	}
}

// Чужое имя базы — отказ с именем базы и перечнем допустимых, без наката.
// Имя pgtest (`kacho_<cfg>_tNNNN`) — та же форма отказа: запасной ветки на имя
// вне таблицы у точки нет (CX1-115 (б)).
func TestForeignDatabaseIsRefusedWithItsName(t *testing.T) {
	for _, db := range []string{"kacho_vpc", "kacho_notifyprobe_t0001", "postgres"} {
		_, _, err := selectChain(chainsTable, embedded, "postgres://u:p@db:5432/"+db)
		if err == nil {
			t.Fatalf("база %s принята — точка накатывает только базы своей таблицы", db)
		}
		for _, s := range []string{db, "kacho_notifyprobe", pointDir} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("отказ по базе %s не называет %q: %v", db, s, err)
			}
		}
	}
}

// DSN без имени базы — отказ, а не выбор «первой строки».
func TestDSNWithoutDatabaseIsRefused(t *testing.T) {
	if _, _, err := selectChain(chainsTable, embedded, "host=db user=u"); err == nil {
		t.Fatal("DSN без имени базы принят")
	}
}

// Страж равенства: множества каталогов таблицы и встроенных FS не равны —
// отказ старта с обоими множествами. Близнец — таблица и FS точки как есть.
func TestTableAndEmbeddedSetsMustBeEqual(t *testing.T) {
	const probeDSN = "postgres://u:p@db:5432/kacho_notifyprobe"
	if _, _, err := selectChain(chainsTable, embedded, probeDSN); err != nil {
		t.Fatalf("таблица точки и её встроенные FS расходятся: %v", err)
	}
	extra := map[string]fs.FS{
		"services/notify/internal/probemigrations": probemigrations.FS,
		"services/notify/internal/migrations":      fstest.MapFS{},
	}
	_, _, err := selectChain(chainsTable, extra, probeDSN)
	if err == nil {
		t.Fatal("встроенная FS без строки таблицы принята")
	}
	for _, s := range []string{"services/notify/internal/migrations", "services/notify/internal/probemigrations"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("отказ не называет %q: %v", s, err)
		}
	}
	if _, _, err := selectChain(chainsTable, map[string]fs.FS{}, probeDSN); err == nil {
		t.Fatal("строка таблицы без встроенной FS принята")
	}
}

// Инъекция (Д74): строка kacho_notifyprobe снята из таблицы → накат пробы
// отказывает с именем базы.
func TestRemovedRowRefusesTheProbeDatabase(t *testing.T) {
	table := []byte("chains:\n  - database: kacho_other\n    dir: services/notify/internal/probemigrations\n")
	_, _, err := selectChain(table, embedded, "postgres://u:p@db:5432/kacho_notifyprobe")
	if err == nil || !strings.Contains(err.Error(), "kacho_notifyprobe") {
		t.Fatalf("строка снята, а база пробы не отвергнута с именем: %v", err)
	}
}

// helperEnv — переменная, по которой тестовый бинарь исполняет main() точки:
// отказ точки наблюдается тем же каналом, что у init-контейнера, — stderr
// процесса и его код выхода.
const helperEnv = "KACHO_NOTIFY_MIGRATOR_TEST_RUN_MAIN"

// TestHelperRunsMain — не проба: тело процесса-помощника.
func TestHelperRunsMain(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	os.Args = []string{binaryName, "up"}
	main()
	os.Exit(0)
}

// runPoint исполняет main() точки с DSN в KACHO_MIGRATOR_DSN; stderr и код.
func runPoint(t *testing.T, dsn string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperRunsMain$")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "KACHO_MIGRATOR_DSN="+dsn, "PGDATABASE=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// SEC-W1-01, наблюдаемое: отказ точки на DSN с паролем-маркером в обеих
// записях — stderr не несёт маркера ни целиком, ни частью. Близнец — годный
// DSN с тем же маркером и базой вне таблицы: отказ называет базу (канал
// наблюдения жив), маркера нет.
func TestPointRefusalCarriesNoPasswordMarker(t *testing.T) {
	cases := []struct {
		dsn     string
		markers []string
		names   string
	}{
		{"postgres://notifyprobe:Sup3r%Secret@db-host-x:5432/kacho_notifyprobe?sslmode=require",
			[]string{"Sup3r", "Secret", "%Se"}, pointDir},
		{"host=db-host-x port=5432 user=notify password=Tail0f Secret dbname='kacho_notifyprobe sslmode=require",
			[]string{"Tail0f", "Secret"}, pointDir},
		// Близнец: DSN разбирается, база вне таблицы — отказ с её именем.
		{"postgres://notifyprobe:Sup3rSecret@db-host-x:5432/kacho_vpc?sslmode=require",
			[]string{"Sup3r", "Secret"}, "kacho_vpc"},
	}
	for _, c := range cases {
		stderr, err := runPoint(t, c.dsn)
		if err == nil {
			t.Errorf("DSN %q: точка вышла успехом, а обязана отказать до соединения:\n%s", c.dsn, stderr)
			continue
		}
		if !strings.Contains(stderr, c.names) {
			t.Errorf("DSN %q: stderr не называет %q — отказ не тот:\n%s", c.dsn, c.names, stderr)
		}
		for _, m := range c.markers {
			if strings.Contains(stderr, m) {
				t.Errorf("DSN %q: stderr точки несёт кусок пароля %q:\n%s", c.dsn, m, stderr)
			}
		}
	}
}

// Д83 — после соединения точка сверяет current_database() со строкой таблицы
// до первого оператора goose: неравенство — отказ с обоими именами, без
// наката. Близнец — равенство → накат разрешён. Ошибка чтения — отказ, а не
// «сверка пройдена».
func TestConnectedDatabaseMustBeTheRowDatabase(t *testing.T) {
	ctx := context.Background()
	answer := func(name string, err error) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { return name, err }
	}
	if err := confirmDatabase(ctx, answer("kacho_notifyprobe", nil), "kacho_notifyprobe"); err != nil {
		t.Fatalf("близнец: база соединения равна строке, а отказ: %v", err)
	}
	err := confirmDatabase(ctx, answer("kacho_notify", nil), "kacho_notifyprobe")
	if err == nil {
		t.Fatal("соединение с kacho_notify при строке kacho_notifyprobe принято — накат ушёл бы в чужую базу")
	}
	for _, s := range []string{"kacho_notify", "kacho_notifyprobe", pointDir, "наката нет"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("отказ сверки не называет %q: %v", s, err)
		}
	}
	if err := confirmDatabase(ctx, answer("", errors.New("conn reset")), "kacho_notifyprobe"); err == nil {
		t.Fatal("ошибка чтения current_database() принята за пройденную сверку")
	}
}
