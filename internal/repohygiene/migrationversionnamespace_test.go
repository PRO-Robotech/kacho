// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// migrationversionnamespace_test.go — гейт: номер миграции выводится из номера
// задачи, а не выбирается из каталога.
//
// Предмет, механизм и его цена разобраны в шапке migrationversionnamespace.go —
// здесь они не пересказываются, чтобы не завести двух мест об одном предмете.
//
// Этот файл утверждает три вещи и печатает объём осмотренного:
//
//  1. порядковая эра закрыта — её номера совпадают с переписью дословно;
//  2. новая форма исполнима — у каждой выведенной версии есть порядковый разряд;
//  3. предпосылки самого гейта верны — состав дерева прочитан, каталоги найдены,
//     а граница между формами действительно лежит выше всей порядковой эры.
//
// Доказательство способности упасть — в migrationversionnamespace_injection_test.go.
package repohygiene

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMigrationVersionIsDerivedFromItsIssue(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}
	byDir, census := collectMigrationVersions(tree.SortedFiles())

	// ПЕРЕПИСЬ — отдельное утверждение. Без неё «ноль находок» неотличимо от
	// «ноль прочитанного», а именно так этот гейт и умер бы молча: достаточно
	// сменить раскладку каталогов или соглашение об именах.
	t.Logf("перепись: файлов в дереве %d, каталогов миграций %d, файлов миграций %d "+
		"(порядковой формы %d, выведенной из задачи %d), наибольший порядковый номер %d, "+
		"окно выведенных номеров [%d..%d]",
		tree.Count(), census.Dirs, census.Files, census.Legacy, census.Issued,
		census.MaxLegacy, firstIssuedMigrationVersion, lastIssuedMigrationVersion)

	// ПРЕДПОСЫЛКИ. Каждая — факт о дереве, который может измениться и сделать
	// запрет ложным; поэтому гейт проверяет их сам, а не подразумевает.
	if tree.Count() == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: индекс дерева пуст — прочитано ноль файлов, " +
			"и любое «ноль находок» ниже ничего не значит")
	}
	if census.Dirs == 0 {
		t.Fatal("ПРЕДПОСЫЛКА ЛОЖНА: не найдено ни одного каталога с файлами вида " +
			"<номер>_<что>.sql — либо раскладка изменилась, либо соглашение об именах; " +
			"пока это так, гейт не проверяет ничего")
	}
	if census.Files == 0 {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: каталогов %d, а файлов миграций ноль — обход сломан",
			census.Dirs)
	}
	if census.MaxLegacy >= firstIssuedMigrationVersion {
		t.Fatalf("ПРЕДПОСЫЛКА ЛОЖНА: наибольший порядковый номер %d дорос до границы форм %d — "+
			"разделение по величине перестало различать формы, и гейт судит не то, что думает",
			census.MaxLegacy, firstIssuedMigrationVersion)
	}

	for _, f := range migrationVersionFindings(byDir, frozenLegacyMigrations) {
		t.Error(f)
	}
}
