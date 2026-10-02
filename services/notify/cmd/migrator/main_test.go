// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"io/fs"
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
