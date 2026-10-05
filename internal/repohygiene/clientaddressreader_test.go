// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestClientAddressHasOneReaderInTheEdge — гейт над деревом края: заголовки
// пересылки читает только оператор адреса, метаданные моста не читает никто
// (clientaddressreader.go).
func TestClientAddressHasOneReaderInTheEdge(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var census clientAddressCensus
	var findings []string
	err := rootedWalk(filepath.Join(root, "gateway"), func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") &&
			!IsTestHarnessPath("gateway/"+rel) && !strings.Contains("/"+rel, "/testdata/")
	}, func(abs string, body []byte) error {
		rel, rErr := filepath.Rel(root, abs)
		if rErr != nil {
			return rErr
		}
		got, jErr := judgeClientAddressReaders(filepath.ToSlash(rel), body, &census)
		findings = append(findings, got...)
		return jErr
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("перепись: %s", census.Summary())
	if census.Files == 0 {
		t.Fatal("не прочитано ни одного прод-файла края — обход не выполнился, это не зелёный")
	}
	if census.ReaderReads == 0 {
		t.Fatalf("в читателях %v не найдено ни одного чтения — гейт смотрит не туда, либо читатель переименован",
			clientAddressReaders)
	}
	if len(findings) > 0 {
		t.Fatalf("адрес клиента читается не одним оператором (%d):\n%s\n\nперепись: %s",
			len(findings), strings.Join(findings, "\n"), census.Summary())
	}
}
