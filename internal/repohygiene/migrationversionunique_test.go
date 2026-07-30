// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationversionunique_test.go — гейт против двух миграций с ОДНИМ номером в
// одном сервисе.
//
// ПОЧЕМУ ЭТО НЕ КОСМЕТИКА. Номер миграции — не имя, а ключ: сервис хранит в своей
// базе строку «версия N применена». Две разные миграции с одним N — это два разных
// изменения схемы под одним ключом, и различить их база уже не может. Инструмент
// миграций не пытается: он ПАДАЕТ ПАНИКОЙ при сборе списка, до подключения к базе.
// Паника случается в init-контейнере, поэтому сервис не поднимается вовсе.
//
// ПОЧЕМУ ЭТОГО НЕ ЛОВИЛО НИЧТО. Столкновение рождается не в ветке, а в СЛИЯНИИ:
// две ветки независимо взяли следующий свободный номер, каждая у себя была права,
// конфликта содержимого нет (файлы разные), поэтому слияние проходит чисто и оба
// файла оказываются рядом. Ни компилятор, ни обзор диффа этого не видят — виден
// только каталог целиком, а его никто не смотрит целиком.
//
// НАБЛЮДАВШЕЕСЯ СЛЕДСТВИЕ (2026-07-30). На стволе одновременно оказались
// `0024_instances_cursor_index.sql` и `0024_drop_decoy_pending_idx.sql` в машинах и
// такая же пара под номером 0012 в хранении. Оба сервиса ушли в цикл падений
// init-контейнера, и стенд поднять было нельзя. Причём номер 0024 в базе уже стоял
// применённым — за ним стояла ДРУГАЯ миграция, приехавшая раньше; то есть новая
// работа молча претендовала на ключ, который уже занят и уже что-то значит.
//
// ЧТО ДЕЛАТЬ ПРИ ОТКАЗЕ: переномеровать НОВУЮ миграцию на следующий свободный
// номер. Никогда не переномеровывать ту, что уже применена на живой базе, — её
// ключ там записан (запрет «не редактировать применённую миграцию»).
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var migrationFileRe = regexp.MustCompile(`^(\d+)_[^/]*\.sql$`)

type migrationDup struct {
	service string
	version string
	files   []string
}

// scanMigrationVersions — перепись и находки под указанным корнем. Отдельная функция,
// потому что запрет обязан быть доказан ИНЪЕКЦИЕЙ, а инъекция требует дерева, которое
// можно построить (см. TestMigrationVersions_GateProvenByInjection).
func scanMigrationVersions(t *testing.T, root string) (dirs int, files int, dups []migrationDup) {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "migrations"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	sort.Strings(found)
	for _, dir := range found {
		service := filepath.Base(filepath.Dir(filepath.Dir(dir)))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		byVersion := map[string][]string{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			m := migrationFileRe.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			files++
			// Ведущие нули — часть записи, а не значения: `0024` и `24` были бы одним
			// и тем же ключом для инструмента, поэтому сравниваем ЗНАЧЕНИЕ.
			v := strings.TrimLeft(m[1], "0")
			if v == "" {
				v = "0"
			}
			byVersion[v] = append(byVersion[v], e.Name())
		}
		versions := make([]string, 0, len(byVersion))
		for v := range byVersion {
			versions = append(versions, v)
		}
		sort.Strings(versions)
		for _, v := range versions {
			if len(byVersion[v]) > 1 {
				sort.Strings(byVersion[v])
				dups = append(dups, migrationDup{service: service, version: v, files: byVersion[v]})
			}
		}
	}
	return len(found), files, dups
}

// TestMigrationVersions_UniquePerService.
func TestMigrationVersions_UniquePerService(t *testing.T) {
	root := repoRootForMigrations(t)
	dirs, files, dups := scanMigrationVersions(t, root)

	// ПРЕДПОСЫЛКА ГЕЙТА. Он опирается на два факта о дереве: миграции лежат по пути
	// services/*/internal/migrations и называются `<номер>_<что>.sql`. Оба могут
	// измениться, и тогда гейт начнёт молча одобрять всё подряд. Поэтому «ноль
	// находок» отделено от «ноль прочитанного» ЯВНО — перепись это отдельное
	// утверждение, а не примечание в логе.
	if dirs == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: не найдено ни одного каталога миграций по "+
			"services/*/internal/migrations (искали от %s). Пока это так, гейт "+
			"не проверяет ничего.", root)
	}
	if files == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: в %d каталогах миграций не нашлось ни одного файла "+
			"вида <номер>_<что>.sql — изменилось соглашение об именах, и гейт слеп.", dirs)
	}
	t.Logf("перепись: каталогов миграций %d, файлов %d", dirs, files)

	for _, d := range dups {
		t.Errorf("%s: номер %s занят %d раза — %s.\n"+
			"    Инструмент миграций падает паникой на сборе списка, до подключения к "+
			"базе, поэтому сервис не стартует вовсе.\n"+
			"    Переномеруй НОВУЮ миграцию на следующий свободный номер; ту, что уже "+
			"применена на живой базе, не трогай.",
			d.service, d.version, len(d.files), strings.Join(d.files, " и "))
	}
}

// TestMigrationVersions_GateProvenByInjection — запрет доказан в ОБЕ стороны.
//
// (а) верни дефект — гейт находит его и называет сервис, номер и оба файла;
// (б) поставь рядом ЗАКОННУЮ конструкцию той же формы (соседние номера, тот же
//     префикс имени, файл не-.sql с тем же номером) — гейт молчит.
//
// Без (б) гейт ловил бы форму, а не существо: «два файла, начинающихся с цифр» —
// нормальное состояние любого каталога миграций.
func TestMigrationVersions_GateProvenByInjection(t *testing.T) {
	write := func(root, service, name string) {
		dir := filepath.Join(root, "services", service, "internal", "migrations")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- x\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	t.Run("дефект возвращён — гейт краснеет и называет координату", func(t *testing.T) {
		root := t.TempDir()
		write(root, "widget", "0007_first.sql")
		write(root, "widget", "0007_second.sql")
		dirs, files, dups := scanMigrationVersions(t, root)
		if dirs != 1 || files != 2 {
			t.Fatalf("перепись должна расти: dirs=%d files=%d", dirs, files)
		}
		if len(dups) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d", len(dups))
		}
		if dups[0].service != "widget" || dups[0].version != "7" || len(dups[0].files) != 2 {
			t.Fatalf("находка не называет координату: %+v", dups[0])
		}
	})

	t.Run("законная конструкция той же формы — гейт молчит", func(t *testing.T) {
		root := t.TempDir()
		write(root, "widget", "0007_first.sql")
		write(root, "widget", "0008_second.sql")
		// Тот же номер, но НЕ миграция: соседний файл в каталоге (README, json-описание)
		// столкновением не является и не должен им притворяться.
		write(root, "widget", "0008_notes.md")
		// Одинаковый номер в РАЗНЫХ сервисах — это норма: ключ версии живёт в базе
		// сервиса, а базы у сервисов свои.
		write(root, "gadget", "0007_first.sql")
		dirs, files, dups := scanMigrationVersions(t, root)
		if dirs != 2 || files != 3 {
			t.Fatalf("перепись: dirs=%d files=%d — ожидалось 2 и 3", dirs, files)
		}
		if len(dups) != 0 {
			t.Fatalf("ложное срабатывание на законной конструкции: %+v", dups)
		}
	})

	t.Run("ведущие нули не создают двух ключей из одного", func(t *testing.T) {
		root := t.TempDir()
		write(root, "widget", "0009_a.sql")
		write(root, "widget", "9_b.sql")
		_, _, dups := scanMigrationVersions(t, root)
		if len(dups) != 1 {
			t.Fatalf("`0009` и `9` — ОДИН ключ для инструмента; ожидалась находка, получено %d", len(dups))
		}
	})
}

// repoRootForMigrations поднимается до корня репозитория (каталог с go.mod).
func repoRootForMigrations(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("не найден корень репозитория (каталог с go.mod) выше %s", dir)
	return ""
}
