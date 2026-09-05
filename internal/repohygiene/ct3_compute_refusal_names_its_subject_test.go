// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1" // регистрирует дескрипторы contract-а compute
)

// ct3ComputeSources — прод-код compute: путь → исходник.
//
// Состав берётся у ИНДЕКСА git (`pkg/treecorpus`), а не с диска. Правила
// игнорирования действуют на любой глубине, и под `services/` на всякой машине,
// где поднимали стенд или собирали консоль, лежат распакованные чарты,
// сборочные каталоги и отчёты прогонов. Обход по диску подобрал бы их, и
// перепись стала бы свойством рабочего каталога, а не коммита.
//
// Пробы исключены намеренно: синтетика проб содержит заведомо дефектные примеры.
func ct3ComputeSources(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services", "compute"), ".go")
	if err != nil {
		t.Fatalf("состав services/compute: %v", err)
	}
	out := map[string]string{}
	for _, abs := range files {
		if strings.HasSuffix(abs, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus), а не из ввода
		if rerr != nil {
			t.Fatalf("чтение %s: %v", abs, rerr)
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", abs, rerr)
		}
		out[rel] = string(b)
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
