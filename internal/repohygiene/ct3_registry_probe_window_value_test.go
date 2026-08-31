// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// TestRegistryProbeCorpusDoesNotCiteARetiredWindowValue — корпус проб registry
// не обосновывает ожидание величиной, у которой в дереве нет производителя.
//
// Предмет, норма, границы и почему это негативное утверждение здесь не замолчит
// — в шапке ct3_registry_probe_window_value.go. Способность гейта упасть и
// смолчать доказана инъекцией в обе стороны, по КАЖДОЙ известной форме записи:
// ct3_registry_probe_window_value_injection_test.go.
func TestRegistryProbeCorpusDoesNotCiteARetiredWindowValue(t *testing.T) {
	root := repoRoot(t)

	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева: %v", err)
	}
	c, err := collectRegistryProbeWindow(tree)
	if err != nil {
		t.Fatalf("обход дерева: %v", err)
	}

	// ── ПЕРЕПИСЬ. Печатается ВСЕГДА и несёт ОБЕ величины: сколько осмотрено и
	// сколько найдено. Одно число скрывает ровно тот случай, ради которого гейт
	// заведён, — обход, переставший видеть предмет.
	var forms []string
	for _, f := range ct3RetiredWindowForms {
		forms = append(forms, f.Name)
	}
	t.Logf("перепись: корпус %s · файлов прочитано %d · строк осмотрено %d · "+
		"форм записи известно %d (%s) · цитат найдено %d",
		c.CorpusDir, c.FilesRead, c.LinesScanned, c.FormsKnown,
		strings.Join(forms, "; "), len(c.Citations))

	// ── ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ. Пустой обход обесценивает вердикт:
	// «ноль находок» тогда означает «ноль прочитанного». Это же — единственное,
	// что может отнять у гейта предмет (переезд корпуса), поэтому проверка
	// стоит именно здесь.
	if c.FormsKnown == 0 {
		t.Fatal("распознаватель не знает ни одной формы записи — предмета у гейта нет")
	}
	if c.FilesRead == 0 {
		t.Fatalf("в корпусе %s не прочитано ни одного файла — вердикт беспредметен: "+
			"корпус проб переехал либо перестал отслеживаться индексом", c.CorpusDir)
	}
	if c.LinesScanned == 0 {
		t.Fatalf("в корпусе %s прочитано %d файлов и ноль строк — обход пуст",
			c.CorpusDir, c.FilesRead)
	}

	for _, f := range registryProbeWindowFindings(c) {
		t.Errorf("окно видимости в корпусе проб: %s", f)
	}
}
