// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// injectionproof_test.go — держатель разбора «названное доказательство
// существует», поднятый до корня МОНОРЕПО (#2519).
//
// Такой же разбор живёт у службы доступа и обходит корень своего модуля: она
// уезжает отдельным продуктом, и обход корня монорепо был бы для неё обходом
// дерева, которого не существует. Обещания соседних модулей платформы этому
// модулю не принадлежат — судить их оттуда значило бы краснеть на чужом дереве.
// Здесь держатель платформы судит ВСЁ дерево, включая модуль службы: в монорепо
// он есть, и второй вердикт о тех же файлах с первым не расходится — формы
// резолва у разбора одни и те же.
//
// Разбор класса, четыре законные формы координаты и довод про две реализации —
// в шапке `tools/injectionproofgate`. Здесь только обход, перепись и вердикт.
//
// Способность упасть и смолчать доказана инъекцией — injectionproof_injection_test.go.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
	"github.com/PRO-Robotech/kacho/tools/injectionproofgate"
)

// injectionProofCorpus — исходники и объявления модулей ВСЕГО дерева,
// спрошенные У ИНДЕКСА git.
//
// Довод не стилистический: под каталогами служб на всякой машине, где поднимали
// стенд или собирали фронтенд, лежат рабочие копии полос и распакованные чарты, и
// обход диска судил бы чужие обещания наравне с деревом — вердикт стал бы
// свойством рабочего каталога, а не коммита.
func injectionProofCorpus(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева: %v — «ноль находок» здесь означало бы «ноль прочитанного»", err)
	}
	corpus := map[string][]byte{}
	for _, abs := range files {
		base := filepath.Base(abs)
		if !strings.HasSuffix(abs, ".go") && base != "go.mod" {
			continue
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			t.Fatalf("путь %s: %v", abs, relErr)
		}
		rel = filepath.ToSlash(rel)
		if base == "go.mod" {
			// Содержимое не читается: ключ и есть объявление корня модуля.
			corpus[rel] = nil
			continue
		}
		body, readErr := os.ReadFile(abs) // #nosec G304 -- путь из индекса своего дерева
		if readErr != nil {
			t.Fatalf("чтение %s: %v", rel, readErr)
		}
		corpus[rel] = body
	}
	return corpus
}

// TestEveryNamedInjectionProofExistsInTheWholeTree — каждое названное
// доказательство инъекции резолвится, где бы в дереве его ни назвали.
func TestEveryNamedInjectionProofExistsInTheWholeTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	findings, census, err := injectionproofgate.Audit(injectionProofCorpus(t, root))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	t.Logf("перепись: исходников разобрано %d · корней модулей %d · называющих файлов %d · "+
		"обещаний %d · резолвится %d · в литералах %d (полоса НЕ судимая)",
		census.GoFiles, census.ModuleRoots, census.NamingFiles,
		census.InComments, census.Resolved, census.InStrings)

	if census.GoFiles == 0 || census.InComments == 0 {
		t.Fatalf("исходников %d, обещаний %d — обход не состоялся, и молчание держателя "+
			"ничего не утверждает", census.GoFiles, census.InComments)
	}
	if census.ModuleRoots < 2 {
		t.Fatalf("корней модулей в корпусе %d — форма «от корня модуля» не исполнялась ни "+
			"разу, и координаты службы, считанные от её модуля, стали бы ложными находками",
			census.ModuleRoots)
	}

	for _, f := range findings {
		t.Errorf("%s обещает %s — доказательства с таким именем в дереве НЕТ. Исходов два: "+
			"инъекция написана и падает на внесённом дефекте, либо обещание снято ВМЕСТЕ с "+
			"тем, что оно обещало. Третьего («оставить как есть») нет: обещание без предмета "+
			"читается как само доказательство.", f.NamedBy, f.Coordinate)
	}
}
