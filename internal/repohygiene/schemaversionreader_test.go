// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// schemaversionreader_test.go — ГЕЙТ: у объявленной точки невозврата есть
// читатель на пути старта, и он провязан у КАЖДОГО сервиса с набором миграций.
//
// Предмет, единица счёта и граница — в шапке schemaversionreader.go; здесь они
// не пересказываются, чтобы два места об одном предмете не разошлись.
//
// Что делать при отказе — говорит сам текст находки.
//
// Доказательство способности упасть и смолчать — schemaversionreader_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// serviceMigrationSets и serviceRootSources читают ИНДЕКС git: вердикт обязан
// быть свойством коммита, а не рабочего каталога.
func schemaReaderTreeFacts(t *testing.T) (services []string, withMigrations []string,
	roots []schemaReaderSource,
) {
	t.Helper()
	root := repoRoot(t)
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}

	seenService := map[string]bool{}
	hasMigrations := map[string]bool{}
	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, "services/") {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 3 {
			continue
		}
		svc := parts[1]
		seenService[svc] = true

		if strings.HasPrefix(rel, "services/"+svc+"/internal/migrations/") &&
			strings.HasSuffix(rel, ".sql") {
			hasMigrations[svc] = true
		}

		if !strings.HasPrefix(rel, "services/"+svc+"/cmd/") ||
			!strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			// Файл в индексе и не читается с диска — обход неполон, и «ноль
			// находок» ниже относилось бы к меньшему, чем дерево.
			t.Fatalf("файл %s есть в индексе и не читается с диска: %v", rel, rerr)
		}
		roots = append(roots, schemaReaderSource{Service: svc, Rel: rel, Body: string(b)})
	}

	for s := range seenService {
		services = append(services, s)
	}
	for s := range hasMigrations {
		withMigrations = append(withMigrations, s)
	}
	sort.Strings(services)
	sort.Strings(withMigrations)
	return services, withMigrations, roots
}

// TestEveryServiceWithMigrationsReadsItsSchemaVersionOnStartup — ядро гейта.
func TestEveryServiceWithMigrationsReadsItsSchemaVersionOnStartup(t *testing.T) {
	services, withMigrations, roots := schemaReaderTreeFacts(t)

	census, missing := findServicesMissingSchemaReader(withMigrations, roots)
	census.Services = len(services)
	t.Logf("осмотрено: %s", census)

	// Предпосылка самого гейта: обход не пуст. «Читателя не хватает никому»
	// обязано быть отличимо от «сервисов с миграциями не найдено».
	if census.WithMigrations == 0 {
		t.Fatalf("сервисов со встроенным набором миграций не найдено ни одного (сервисов "+
			"осмотрено %d, файлов корней %d) — обход пуст, вердикт беспредметен: либо раскладка "+
			"сменилась, либо предикат перестал её читать", census.Services, census.RootFiles)
	}
	if census.RootFiles == 0 {
		t.Fatalf("не прочитано ни одного файла композиционного корня — вердикт беспредметен")
	}

	if len(missing) > 0 {
		t.Errorf("%s", schemaReaderFindingText(missing, census))
	}
}
