package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Дублирующийся номер версии миграции валит мигратор НА СТАРТЕ ("duplicate version N
// detected"), то есть под уходит в бесконечный перезапуск инициализации, а сервис не
// поднимается вовсе. Класс рождается при параллельной работе: две ветки независимо берут
// следующий свободный номер, каждая по-своему права, и коллизия появляется только в
// момент слияния — ни один прогон тестов ветки её не видит.
//
// Проверка намеренно читает КАТАЛОГИ, а не список в коде: файл, положенный мимо реестра,
// всё равно попадёт к мигратору через встраивание каталога.
var migrationVersionName = regexp.MustCompile(`^0*(\d+)_.+\.sql$`)

func migrationDirsForVersionGate(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var dirs []string
	services, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("не удалось прочитать каталог сервисов: %v", err)
	}
	for _, s := range services {
		if !s.IsDir() {
			continue
		}
		d := filepath.Join(root, "services", s.Name(), "internal", "migrations")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func TestMigrationVersionsAreUniquePerService(t *testing.T) {
	dirs := migrationDirsForVersionGate(t)
	if len(dirs) == 0 {
		t.Fatal("предпосылка проверки не выполняется: не найдено ни одного каталога миграций — " +
			"либо раскладка дерева изменилась, либо обход сломан; молча зеленеть здесь нельзя")
	}

	var findings []string
	filesRead := 0

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("не удалось прочитать %s: %v", dir, err)
		}
		byVersion := map[int][]string{}
		for _, e := range entries {
			m := migrationVersionName.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			filesRead++
			v, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			byVersion[v] = append(byVersion[v], e.Name())
		}
		versions := make([]int, 0, len(byVersion))
		for v := range byVersion {
			versions = append(versions, v)
		}
		sort.Ints(versions)
		for _, v := range versions {
			names := byVersion[v]
			if len(names) > 1 {
				sort.Strings(names)
				rel, _ := filepath.Rel(repoRoot(t), dir)
				findings = append(findings, fmt.Sprintf(
					"%s: версия %d объявлена %d раза(-ами): %s — мигратор откажет на старте, "+
						"под уйдёт в перезапуск инициализации. Перенумеровать НЕПРИМЕНЁННУЮ сторону: "+
						"применённую миграцию править нельзя, и база уже считает эту версию выполненной",
					rel, v, len(names), strings.Join(names, ", ")))
			}
		}
	}

	// Перепись — отдельное утверждение: «ноль находок» обязано отличаться от «ноль прочитанного».
	t.Logf("migration-versions-unique: осмотрено каталогов %d, файлов миграций %d, находок %d",
		len(dirs), filesRead, len(findings))

	if filesRead == 0 {
		t.Fatal("прочитано НОЛЬ файлов миграций при непустом списке каталогов — обход не работает")
	}
	for _, f := range findings {
		t.Error(f)
	}
}
