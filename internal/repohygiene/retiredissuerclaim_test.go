// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

// retiredIssuerCorpus — тела файлов дерева, отобранные retiredIssuerProseFile.
func retiredIssuerCorpus(t *testing.T, tree *treecorpus.Tree) map[string]string {
	t.Helper()
	corpus := map[string]string{}
	for _, rel := range tree.SortedFiles() {
		if !retiredIssuerProseFile(rel) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel))) // #nosec G304 -- путь из индекса git под корнем дерева
		if err != nil {
			t.Fatalf("чтение %s: %v — гейт не вправе судить файл, которого не прочитал", rel, err)
		}
		corpus[rel] = string(body)
	}
	return corpus
}

// TestRetiredIssuerIsNamedAsActingOnlyInATombstone — перепись прозы дерева:
// утверждение «прежний OAuth-сервер остаётся подписантом/издателем» законно
// только как надгробие.
func TestRetiredIssuerIsNamedAsActingOnlyInATombstone(t *testing.T) {
	t.Parallel()
	tree, err := treecorpus.NewTree(repoRoot(t))
	if err != nil {
		t.Fatalf("состав дерева не установлен: %v — «ноль находок» здесь означало бы "+
			"«ноль прочитанного»", err)
	}
	findings, census, err := judgeRetiredIssuerClaims(retiredIssuerCorpus(t, tree))
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: %v", err)
	}
	t.Log(census)
	if census.Mentions == 0 {
		t.Fatalf("строк с именем прежнего издателя прочитано 0 при %d файлах — либо "+
			"компонент снят целиком (тогда снимите и этот гейт вместе с предметом), "+
			"либо распознаватель ослеп", census.Files)
	}
	for _, f := range findings {
		t.Error(f)
	}
}
