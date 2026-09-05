// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_treefiles_test.go — как гейты семейства и их инъекции получают
// состав дерева.
//
// Конструктора ДВА, и выбирает его вызывающий — молчаливого отката между ними
// нет by construction (разбор — godoc `treecorpus.SyntheticTree`). Гейт о
// настоящем дереве спрашивает ИНДЕКС git; инъекция собирает дерево сама во
// временном каталоге, репозиторием оно не является, и обход файловой системы
// там — не откат, а единственный возможный авторитет.
//
// Помощники заведены здесь, а не по местам вызова: их двадцать с лишним, и
// двадцать копий одной и той же четвёрки строк разъехались бы в том, КАК они
// сообщают об отказе — то есть ровно там, где отказ и надо отличить от пустого
// успеха.

package repohygiene

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// clientTruthRepoTree — состав НАСТОЯЩЕГО дерева по индексу git.
//
// Недоступность git — отказ пробы, а не пропуск: «ноль находок» на «ноль
// прочитанного» неотличимо от чистого дерева.
func clientTruthRepoTree(t *testing.T) *treecorpus.Tree {
	t.Helper()
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	return tree
}

// clientTruthSyntheticTree — состав СИНТЕТИЧЕСКОГО дерева, собранного пробой во
// временном каталоге.
func clientTruthSyntheticTree(t *testing.T, root string) *treecorpus.Tree {
	t.Helper()
	tree, err := treecorpus.SyntheticTree(root)
	if err != nil {
		t.Fatalf("состав синтетического дерева %s: %v", root, err)
	}
	return tree
}
