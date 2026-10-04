// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// Прогон одной командой:
//
//	go test ./internal/repohygiene/ -run TestNoOwnPathCarriesTheRetiredVendorName -count=1 -v
//
// Печатает перепись (путей обойдено · несут имя издателя · из них имя дал
// поставщик — поимённо · находок) и по находке на каждый НАШ путь.

// TestNoOwnPathCarriesTheRetiredVendorName — ни один наш отслеживаемый путь не
// несёт имени снятого издателя личности; имя, данное поставщиком (архив
// объявленной внешней зависимости зонта), находкой не является. Шапка и довод —
// retiredvendorpathname.go.
func TestNoOwnPathCarriesTheRetiredVendorName(t *testing.T) {
	t.Parallel()

	repo := repoRoot(t)
	tree, err := treecorpus.NewTree(repo)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: состав дерева не установлен: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(RetiredVendorUmbrellaChart)))
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %s не прочитан: %v — имя поставщика от нашего "+
			"отличить не по чему", RetiredVendorUmbrellaChart, err)
	}
	v, err := JudgeRetiredVendorPaths(tree.SortedFiles(), raw)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(v.Census())
	for _, f := range v.Findings {
		t.Error(f)
	}
}
