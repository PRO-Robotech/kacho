// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/contractroot"
	"github.com/PRO-Robotech/corelib/platformmodules"

	"github.com/PRO-Robotech/kacho/internal/contractsource"
)

// platformmodulevocabulary_test.go — гейт над ДЕРЕВОМ: словарь имён модулей
// платформы сверяется с тремя своими производителями (задача #1885).
//
// Предмет и обе стороны каждой колонки разобраны на
// platformmodulevocabulary.go; здесь — только добыча входа. Способность падать
// доказывает не этот прогон, а инъекция
// (platformmodulevocabulary_injection_test.go).
func TestPlatformModuleVocabularyMatchesTheTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	declared := platformmodules.All()
	if len(declared) == 0 {
		t.Fatal("словарь модулей пуст: судить нечего, и всякое «ноль находок» " +
			"относилось бы к непрочитанному")
	}

	faults, census := judgePlatformVocabulary(
		declared,
		dirNamesUnder(t, filepath.Join(root, "services")),
		contractDomainSet(contractProtoDirs(t, root)...),
		collectModelObjectTypes(t, root),
	)

	t.Logf("перепись: %s", census.Summary())

	if len(faults) > 0 {
		t.Fatalf("словарь имён модулей разошёлся с деревом (%d):\n  %s\n\nперепись: %s",
			len(faults), strings.Join(faults, "\n  "), census.Summary())
	}
}

// dirNamesUnder — имена подкаталогов, множеством. Пустой обход — отказ: «ноль
// находок» обязано быть отличимо от «ноль прочитанного».
func dirNamesUnder(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("читаю %s: %v", dir, err)
	}
	out := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = struct{}{}
		}
	}
	if len(out) == 0 {
		t.Fatalf("в %s не найдено ни одного подкаталога — обход пуст, и вердикт "+
			"относился бы к непрочитанному", dir)
	}
	return out
}

// contractDomainSet — имена деревьев доменов под ВСЕМИ объявленными корнями во
// ВСЕХ каталогах `proto/`, которые эти корни занимают.
//
// Заменяет обход одного литерального каталога: домен, переехавший под второй
// корень, выпал бы из словаря, и гейт объявил бы расхождением собственную
// слепоту, а не находку дерева. Каталогов стало ДВА — см. [contractProtoDirs].
func contractDomainSet(protoDirs ...string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, protoDir := range protoDirs {
		for _, d := range contractroot.Domains(protoDir) {
			out[d] = struct{}{}
		}
	}
	return out
}

// contractProtoDirs — каталоги `proto/`, в которых лежат деревья ОБЪЯВЛЕННЫХ
// корней контрактов.
//
// # Почему каталог перестал быть один
//
// Прежде он был один — `<корень репозитория>/proto`, — и читатели называли его
// склейкой. Решением владельца (kacho#2616, исход C, 2026-09-13) контракты
// службы доступа вынесены в её репозиторий: дерево корня `kaname` приезжает
// модулем `github.com/PRO-Robotech/kaname`, и склейка перестала его НАХОДИТЬ —
// не покраснела, а замолчала. Это тот же класс, ради которого заведён
// `contractroot` (литерал приставки корня), повторённый на уровень выше: там
// молчал отбор ПО КОРНЮ, здесь — отбор ПО КАТАЛОГУ.
//
// # Форма ответа выбрана под чужой вход, а не под удобство
//
// `contractroot` резолвит домен как `<protoDir>/<корень>/cloud/<домен>`, а
// [contractsource] отвечает каталогом `<…>/proto/<корень>` — нужный `protoDir`
// есть его РОДИТЕЛЬ. Поэтому здесь берётся родитель, а не собирается путь
// заново: собранный путь разошёлся бы с формой, которую contractsource
// объявляет, и разошёлся бы молча. Повторы отсеиваются — корни, лежащие в этом
// дереве, дают один и тот же каталог.
//
// Пустой ответ — ОТКАЗ: ноль каталогов означает, что дерева контрактов нет
// вовсе, и всякое «ноль находок» по нему было бы свойством непрочитанного.
func contractProtoDirs(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	var out, refused []string
	for _, r := range contractroot.Roots {
		dir, err := contractsource.Dir(root, r)
		if err != nil {
			refused = append(refused, fmt.Sprintf("%s (%v)", r, err))
			continue
		}
		p := filepath.Dir(dir)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		t.Fatalf("ни один объявленный корень контрактов %v не резолвится: %s — обход "+
			"пуст, и вердикт относился бы к непрочитанному",
			contractroot.Roots, strings.Join(refused, "; "))
	}
	return out
}
