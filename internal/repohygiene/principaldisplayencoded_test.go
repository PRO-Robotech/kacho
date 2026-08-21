// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// principaldisplayencoded_test.go — гейт: под ключ отображаемого имени
// принципала не кладут сырое значение.
//
// Предмет и его цена разобраны в шапке principaldisplayencoded.go — здесь они не
// пересказываются, чтобы не завести двух мест об одном предмете.
//
// Файл утверждает два свойства и печатает объём осмотренного: находок ноль и
// осматривать было ЧТО (иначе «ноль находок» неотличимо от «ноль прочитанного»
// — гейт умер бы молча от переименования пакета или ключа).
//
// Доказательство способности упасть — principaldisplayencoded_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

func TestPrincipalDisplayNameNeverGoesOnTheWireRaw(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}

	var (
		filesRead  int
		writesSeen int
		findings   []string
	)
	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		// Сам гейт называет ключ и кодек по имени — разбор его собственного
		// исходника дал бы находку на объяснении, а не на дефекте.
		if strings.HasPrefix(rel, "internal/repohygiene/principaldisplayencoded") {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("%s: не читается: %v", rel, rerr)
		}
		filesRead++
		raw, writes, perr := findRawDisplayNameWrites(rel, src)
		if perr != nil {
			t.Fatalf("%s", perr)
		}
		writesSeen += writes
		for _, f := range raw {
			findings = append(findings, rel+":"+strconv.Itoa(f.Line)+" — "+f.Expr)
		}
	}

	t.Logf("перепись: файлов в дереве %d, прочитано не-тестовых .go %d, "+
		"записей под ключ отображаемого имени %d, находок %d",
		tree.Count(), filesRead, writesSeen, len(findings))

	// ПРЕДПОСЫЛКА ГЕЙТА. Если записей под этот ключ в дереве не осталось вовсе,
	// запрет перестал иметь предмет — и молчал бы навсегда, что бы ни завели
	// завтра. Это находка, а не успех.
	if writesSeen == 0 {
		t.Fatal("ни одной записи под ключ отображаемого имени не найдено: " +
			"гейт потерял предмет (ключ переименован либо разбор перестал его узнавать) " +
			"и с этого момента не может покраснеть ни на чём")
	}

	if len(findings) > 0 {
		t.Errorf("значение кладут под ключ отображаемого имени БЕЗ кодирования — "+
			"транспорт отвергнет весь вызов на непечатаемом ASCII, а не «имя не покажется»:\n  %s\n"+
			"пропустить значение через grpcsrv.EncodePrincipalDisplayName "+
			"(на крае — principalmeta.SetPrincipalDisplay)",
			strings.Join(findings, "\n  "))
	}
}
