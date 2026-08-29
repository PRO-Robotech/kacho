// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	vpcMigrationsDir = "services/vpc/internal/migrations"
	vpcDbmlPath      = "services/vpc/docs/src/constants/database-schema.ts"
	vpcDataModelPath = "services/vpc/docs/content/architecture/data-model.mdx"
	vpcProtoDir      = "proto/kacho/cloud/vpc/v1"
	vpcSchemaHeading = "## Таблицы схемы"
)

// TestVpcSchemaPageAgreesWithTheTree — страница модели данных vpc не утверждает
// о схеме ничего, что дерево опровергает.
//
// # Зачем гейт, если страницу «уже сверили руками»
//
// Сверка руками — событие, а не держатель. Ровно на этой странице измерено, что
// она стареет за дни: перечень миграций был верен на девяти файлах и устарел на
// сорока четырёх, а число «ресурсных служб контракта» разошлось с деревом в том
// же коммите, которым его записали. Ни одно из расхождений не производит ни
// конфликта при слиянии, ни красного в сборке сайта — сборка ловит битые ссылки,
// а не ложные утверждения о схеме.
//
// # Что именно держится
//
// Граница диаграммы объявлена СВОЙСТВОМ, а не перечислением: рисуется таблица
// каждого ресурса, которым домен управляет полным набором глаголов, плюс две
// инфраструктурные. Свойство держится тем, что гейт выводит обе стороны из
// дерева — глаголы из контракта, живые таблицы из цепочки миграций — и сверяет
// МНОЖЕСТВА, а не мощности: совпадение счёта при разошедшемся составе — тот же
// класс, что он ловит.
func TestVpcSchemaPageAgreesWithTheTree(t *testing.T) {
	root := repoRoot(t)

	live := vpcSchemaLiveTables(readVpcMigrations(t, root))
	diagram := vpcSchemaDiagramTables(readFileOrFail(t, filepath.Join(root, vpcDbmlPath)))
	rows := vpcSchemaPageRows(readFileOrFail(t, filepath.Join(root, vpcDataModelPath)), vpcSchemaHeading)
	services := vpcSchemaFullVerbServices(readVpcProtos(t, root))

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("осмотрено: живых таблиц %d · на диаграмме %d · строк перечня %d · служб с полным набором глаголов %d",
		len(live), len(diagram), len(rows), len(services))

	// Предпосылка гейта: обход, не нашедший предмета, вердикта не выносит.
	switch {
	case len(live) == 0:
		t.Fatal("живых таблиц ноль — цепочка миграций не прочитана; вердикт недействителен")
	case len(diagram) == 0:
		t.Fatal("на диаграмме ноль таблиц — данные DBML не прочитаны; вердикт недействителен")
	case len(rows) == 0:
		t.Fatalf("перечень схемы пуст — раздел %q не найден либо его таблица не разобрана; вердикт недействителен",
			vpcSchemaHeading)
	case len(services) == 0:
		t.Fatal("служб с полным набором глаголов ноль — контракт не прочитан; вердикт недействителен")
	}

	if findings := vpcSchemaAdjudicate(live, diagram, rows, services); len(findings) > 0 {
		for _, f := range findings {
			t.Error(f)
		}
		t.Log("страница модели данных vpc разошлась с деревом; правится страница либо дерево — " +
			"но не молчанием: расхождение не производит ни конфликта при слиянии, ни красного в сборке сайта")
	}
}

func readFileOrFail(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // путь собран из констант пакета
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(b)
}

func readVpcMigrations(t *testing.T, root string) []vpcSchemaMigration {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, vpcMigrationsDir))
	if err != nil {
		t.Fatalf("чтение %s: %v", vpcMigrationsDir, err)
	}
	var out []vpcSchemaMigration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		out = append(out, vpcSchemaMigration{
			Name: e.Name(),
			Body: readFileOrFail(t, filepath.Join(root, vpcMigrationsDir, e.Name())),
		})
	}
	return out
}

func readVpcProtos(t *testing.T, root string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, vpcProtoDir))
	if err != nil {
		t.Fatalf("чтение %s: %v", vpcProtoDir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".proto") {
			continue
		}
		out[e.Name()] = readFileOrFail(t, filepath.Join(root, vpcProtoDir, e.Name()))
	}
	return out
}
