// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protofieldreaders_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
	"github.com/PRO-Robotech/kacho/tools/unreadfieldaudit/protofieldreaders"
)

// TestIndexLeavesTestHarnessPackagesOutOfProductionCode — пакет-харнесс проб в
// индекс прод-кода не входит, и это НАЗЫВАЕТСЯ переписью.
//
// ПРЕДМЕТ. Харнесс (`repohygiene.TestHarnessPackages`) — обычный Go-пакет без
// `_test`: пробы, собираемые отдельной единицей сборки, иначе его не импортируют.
// Продуктом он не является — его серверы суть дублёры соседей. Пока индекс
// принимал его за прод-код, харнесс KA1, поднимающий дублёр службы доступа
// (`Register…Server` домена `iam`), отменял предикату границу дерева: «сервер
// домена здесь поднимается, а собственного дерева у него нет» — rc=2 на верном
// факте (служба вынесена отдельным продуктом). Тем же путём чтение поля в
// харнессе закрывало бы находку «поле без читателя» читателем, которого в
// продукте нет.
//
// Перечень харнессов берётся ИМПОРТОМ общего источника, а не выписывается здесь:
// вторая копия разошлась бы с первой молча. Что харнесс не стал продуктом,
// держит `TestHarnessPackagesAreImportedOnlyByTests`.
//
// Контроль в ОБЕ стороны:
//
//	(1) КРАСНОЕ  — пакет-харнесс в `Packages` индекса (его файлы прод-кодом
//	    названы) либо не назван в переписи `SkippedTestHarness`;
//	(2) МОЛЧАНИЕ — соседний пакет того же дерева (`gateway/internal/clients`),
//	    который харнессом не является, в индексе остаётся: исключение не шире
//	    своего предмета.
func TestIndexLeavesTestHarnessPackagesOutOfProductionCode(t *testing.T) {
	t.Chdir("../../..")

	if len(repohygiene.TestHarnessPackages) == 0 {
		// Пустой перечень — цель, а не отказ: исключать нечего, и утверждение
		// вернётся вместе с первым харнессом.
		t.Log("перепись: пакетов-харнессов в перечне 0 — исключать нечего")
		return
	}

	ix, err := protofieldreaders.Build("./gateway/...")
	if err != nil {
		t.Fatalf("индекс не построился: %v", err)
	}
	if len(ix.Errors) > 0 {
		t.Fatalf("предпосылка не выполнена — %d пакетов не протипизировано: %s",
			len(ix.Errors), strings.Join(ix.Errors, "; "))
	}
	t.Logf("перепись: пакетов прод-кода %d · файлов %d · харнессов вне индекса %d",
		len(ix.Packages), ix.FileCount(), len(ix.SkippedTestHarness))

	const module = "github.com/PRO-Robotech/kacho/"
	indexed := map[string]bool{}
	for _, p := range ix.Packages {
		indexed[p.Path] = true
		for _, f := range p.Files {
			if repohygiene.IsTestHarnessPath(filepath.ToSlash(f)) {
				t.Errorf("файл харнесса %s прочитан как прод-код (пакет %s)", f, p.Path)
			}
		}
	}
	skipped := map[string]bool{}
	for _, s := range ix.SkippedTestHarness {
		skipped[s] = true
	}
	for _, dir := range repohygiene.TestHarnessPackages {
		if !strings.HasPrefix(dir, "gateway/") {
			continue
		}
		pkg := module + dir
		if indexed[pkg] {
			t.Errorf("харнесс %s в индексе прод-кода", pkg)
		}
		if !skipped[pkg] {
			t.Errorf("харнесс %s не назван в переписи SkippedTestHarness: %v", pkg, ix.SkippedTestHarness)
		}
	}

	twin := module + "gateway/internal/clients"
	if !indexed[twin] {
		t.Errorf("законный близнец %s выпал из индекса — исключение шире предмета", twin)
	}
}
