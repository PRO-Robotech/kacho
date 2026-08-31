// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1" // регистрирует дескрипторы contract-а compute
)

// ct3ComputeSources — прод-код compute: путь → исходник. Пробы исключены
// намеренно: синтетика проб содержит заведомо дефектные примеры.
func ct3ComputeSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "services", "compute")
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path) //nolint:gosec // путь получен обходом дерева репозитория
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(repoRoot(t), path)
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("обход services/compute: %v", err)
	}
	return out
}

// Отказ compute обязан ОБЪЯВЛЯТЬ то поле, о котором говорит его текст. Говорит о
// подполях — объявляет подполе, а не обязательного родителя.
func TestCt3ComputeRefusalDeclaresTheFieldItsTextIsAbout(t *testing.T) {
	idx, contractFields := ct3ComputeChildIndex()
	if contractFields == 0 {
		t.Fatal("ПРЕДПОСЫЛКА НЕВЕРНА: в дескрипторах compute ноль полей-сообщений — " +
			"индекс подполей пуст, и правило не может найти ничего by construction")
	}
	sources := ct3ComputeSources(t)
	findings, cen := ct3ComputeAuditRefusalSubjects(sources, idx)
	cen.ContractFields = contractFields

	t.Logf("перепись: файлов прод-кода %d · пар «поле · текст» %d · из них судимых (поле имеет подполя) %d · полей-сообщений в контракте %d",
		cen.FilesRead, cen.Refusals, cen.Judgeable, cen.ContractFields)

	// Обход обязан быть непустым по КАЖДОЙ величине: «ноль находок» иначе
	// неотличимо от «ноль прочитанного».
	if cen.FilesRead == 0 {
		t.Fatal("обход пуст: прочитано ноль файлов прод-кода compute — вердикт беспредметен")
	}
	if cen.Refusals == 0 {
		t.Fatal("распознано ноль пар «поле · текст»: распознаватель не читает ни одной формы вызова — вердикт беспредметен")
	}
	if cen.Judgeable == 0 {
		t.Fatal("судимых пар ноль: ни одно объявленное поле не имеет подполей — правило не применялось ни разу")
	}

	for _, f := range findings {
		t.Errorf("%s:%d — отказ объявляет поле %q, а текст говорит о его подполях %v: %q\n"+
			"   объявляй ПОДПОЛЕ (по одному нарушению на поле), а не родителя: клиент-автомат "+
			"действует по field_violations[].field, и снятие родителя ломает запрос",
			f.File, f.Line, f.Field, f.Children, f.Desc)
	}
}
