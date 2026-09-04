// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// TestUpdateMaskKnownSetKnowsBothFormsOfAField — известный набор `update_mask`
// знает ОБЕ формы имени каждого своего многословного поля.
//
// Разбор класса и довод в пользу паритета (а не «только snake») — в шапке
// updatemaskformparity.go. Здесь только обход дерева: скопируют набор рядом —
// свойство обязано требоваться и от копии.
func TestUpdateMaskKnownSetKnowsBothFormsOfAField(t *testing.T) {
	root := repoRoot(t)

	// Состав дерева спрашивается У ИНДЕКСА, а не у диска: обход диска судил бы
	// произведённые файлы и чужие рабочие копии.
	out, err := gitenv.Command(root, "ls-files", "-z", "--", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — состав дерева не установлен, и «ноль находок» "+
			"здесь означало бы «ноль прочитанного»", err)
	}

	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || skipPath(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			continue
		}
		sources[rel] = string(b)
	}

	sets, files, calls := CollectMaskSets(sources)
	findings, multiword := JudgeMaskForms(sets)

	t.Logf("перепись: прод-файлов прочитано %d; вызовов проверки маски %d; "+
		"наборов, реально переданных в проверку, %d; многословных ключей рассмотрено %d",
		files, calls, len(sets), multiword)

	// Предпосылка объявляется и проверяется: без вызовов и наборов гейт судит
	// пустоту, и его зелёное означало бы «ноль прочитанного».
	if files == 0 || calls == 0 || len(sets) == 0 {
		t.Fatalf("предмета нет: файлов %d, вызовов %d, наборов %d — проверка беспредметна, "+
			"а не пройдена", files, calls, len(sets))
	}

	for _, f := range findings {
		t.Errorf("%s:%d — набор %s знает «%s», но не знает «%s»: край приводит имя к форме "+
			"поля контракта, поэтому через край это поле не изменить ни при каком входе",
			f.File, f.Line, f.Set, f.Key, f.Want)
	}
}
