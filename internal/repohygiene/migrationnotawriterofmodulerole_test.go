// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationnotawriterofmodulerole_test.go — держатель Г2: миграция не
// вставляет роль модуля, у которого есть манифест (сценарий MOD-RD-23).
//
// Способность гейта упасть и смолчать доказана инъекцией —
// `migrationnotawriterofmodulerole_injection_test.go`.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// iamMigrationsDir — каталог миграций iam.
	iamMigrationsDir = "services/iam/internal/migrations/"
	// migrationRoleCensusFloor — блоков вставки роли, ниже которого обход
	// беспредметен: их в дереве десять, и обвал переписи означает, что разбор
	// перестал видеть предмет.
	migrationRoleCensusFloor = 5
)

// manifestBearingModules — модули, у которых манифест в дереве ЕСТЬ. Перечень
// выводится обходом, а не выписывается.
func manifestBearingModules(t *testing.T, root string, tt *trackedTree) (map[string]string, int) {
	t.Helper()
	out := map[string]string{}
	var scanned int
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".yaml") && !strings.HasSuffix(rel, ".yml") {
			continue
		}
		if strings.Contains(rel, "/testdata/") || strings.Contains(rel, "_test") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		scanned++
		if site, ok := ScanModuleManifest(rel, src); ok {
			out[site.Module] = site.File
		}
	}
	return out, scanned
}

// migrationRoleFindings — предикат находки. Тот же зовёт инъекция.
func migrationRoleFindings(sites []MigrationRoleSite, bearing map[string]string) []string {
	var out []string
	for _, s := range sites {
		if s.Owner == "" {
			continue
		}
		if manifestFile, ok := bearing[s.Owner]; ok {
			out = append(out, fmt.Sprintf("%s: роль %q модуля %q, чей манифест лежит в %s",
				s.File, s.Name, s.Owner, manifestFile))
		}
	}
	sort.Strings(out)
	return out
}

// TestMODRD23MigrationDoesNotWriteARoleOfAManifestBearingModule — сам гейт.
func TestMODRD23MigrationDoesNotWriteARoleOfAManifestBearingModule(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	bearing, yamlScanned := manifestBearingModules(t, root, tt)

	var (
		rels           []string
		blocks, names  int
		sites          []MigrationRoleSite
		parsedMigrated int
	)
	for rel := range tt.files {
		if strings.HasPrefix(rel, iamMigrationsDir) && strings.HasSuffix(rel, ".sql") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsedMigrated++
		s, census := ScanMigrationRoleInserts(rel, src)
		blocks += census.Blocks
		names += census.Names
		sites = append(sites, s...)
	}

	t.Logf("перепись: файлов YAML осмотрено %d, манифестов модулей найдено %d %v; "+
		"миграций iam прочитано %d, блоков вставки роли %d, имён ролей извлечено %d",
		yamlScanned, len(bearing), manifestModuleNames(bearing), parsedMigrated, blocks, names)

	if parsedMigrated == 0 {
		t.Fatalf("миграций iam прочитано ноль — каталог %s переехал, и гейт стережёт "+
			"координату, которой больше нет", iamMigrationsDir)
	}
	if blocks < migrationRoleCensusFloor {
		t.Fatalf("блоков вставки роли прочитано %d при пороге %d — разбор перестал видеть "+
			"предмет, и его молчание сказано ни о чём", blocks, migrationRoleCensusFloor)
	}
	if names == 0 {
		t.Fatalf("имён ролей извлечено ноль при %d блоках — форма записи имени сменилась, "+
			"и разбор МОЛЧИТ вместо того чтобы находить", blocks)
	}

	if len(bearing) == 0 {
		// Популяция пуста — и это НАЗВАНО, а не выдано за проверенное. Молчание
		// гейта здесь означает «сверять не с чем», а не «расхождений нет».
		// Производитель манифестов — задача #1091; гейт вступит в силу вместе с
		// первым из них, без единой правки здесь.
		t.Logf("манифестов модулей в дереве НОЛЬ — популяция гейта пуста: миграция сегодня " +
			"остаётся законным писателем КАЖДОЙ роли. Это не «проверено», это «сверять не с " +
			"чем»; производитель манифестов — задача #1091")
		return
	}

	if findings := migrationRoleFindings(sites, bearing); len(findings) > 0 {
		t.Fatalf("миграция вставляет роль модуля, у которого ЕСТЬ манифест — %d место(а):\n  %s\n\n"+
			"Это второй писатель одного предмета: правка манифеста до строки не доедет, "+
			"потому что строку держит миграция, и перепись применителя об этом промолчит.\n"+
			"Снятие: роль объявляется манифестом, а миграция её не вставляет.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// manifestModuleNames — имена модулей в устойчивом порядке для переписи.
func manifestModuleNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
