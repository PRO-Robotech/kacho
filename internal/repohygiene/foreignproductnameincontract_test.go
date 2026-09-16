// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// foreignProductCensusFloor — порог переписи: ниже него «ноль находок»
// означало бы «ноль прочитанного». Дерево контрактов на день заведения — сотни
// файлов и десятки тысяч строк; порог поставлен заведомо ниже, чтобы он ловил
// пустой обход, а не рост дерева.
const foreignProductCensusFloor = 2000

// TestContractTreeNamesNoForeignProduct — сам гейт: публичный текст контракта
// не называет продуктов чужих платформ (ban #2).
func TestContractTreeNamesNoForeignProduct(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	rels := contractTreeFiles(tt)
	if len(rels) == 0 {
		t.Fatal("обход пуст: файлов контракта не найдено — гейт судил бы о непрочитанном")
	}

	names := ForeignProductNames()
	if len(names) == 0 {
		t.Fatal("словарь пуст — находок не будет ни при каком дереве")
	}

	var (
		sites     []ForeignProductSite
		linesRead int
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("файл контракта %s не прочитан: %v — обход неполон, "+
				"и молчание гейта сказало бы о меньшем, чем кажется", rel, err)
		}
		found, lines := ScanForeignProductNames(rel, src, names)
		linesRead += lines
		sites = append(sites, found...)
	}

	t.Logf("перепись: файлов контракта осмотрено %d; строк прочитано %d; имён в словаре %d",
		len(rels), linesRead, len(names))

	if linesRead < foreignProductCensusFloor {
		t.Fatalf("прочитано %d строк при пороге %d — обход усечён, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", linesRead, foreignProductCensusFloor)
	}

	if len(sites) > 0 {
		t.Errorf("публичный текст контракта называет продукт чужой платформы (%d мест):\n%s",
			len(sites), formatForeignProductSites(sites))
	}
}

// contractTreeFiles — популяция гейта: дерево контрактов, кроме ввезённого
// чужого (`google/**` — контракты gRPC и HTTP-аннотаций, чей текст не наш и
// правке не подлежит).
func contractTreeFiles(tt *trackedTree) []string {
	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".proto") {
			continue
		}
		if strings.HasPrefix(rel, "proto/google/") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels
}

func formatForeignProductSites(sites []ForeignProductSite) string {
	var b strings.Builder
	for _, s := range sites {
		fmt.Fprintf(&b, "  %s:%d — %q; вместо: %s\n", s.File, s.Line, s.Name, s.Instead)
	}
	b.WriteString("\nЗапрет #2: чужие облака не упоминаются в коде, доках, комментариях.\n" +
		"Комментарий контракта — ПУБЛИЧНЫЙ текст: он уезжает в порождённые заглушки\n" +
		"опубликованного модуля, то есть в чужие репозитории, где эта проверка его\n" +
		"уже не достанет.")
	return b.String()
}
