// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// scopedepthagreement_injection_test.go — доказательство в ОБЕ стороны.
//
// (а) верни дефект — сверка краснеет и НАЗЫВАЕТ ОБЕ координаты;
// (б) поставь рядом ЗАКОННУЮ конструкцию той же формы — сверка молчит.
//
// # Почему инъекция бьёт именно в ОХВАТ, а не только в арифметику
//
// Предмет #918 — не сравнение чисел, а ГРАНИЦА ОБХОДА. Прежняя редакция искала
// третью величину только под `services/iam/internal`, а объявлена она в корневом
// `internal/authzplan` — то есть в поддереве, куда обход не заходил by
// construction. Гейт печатал «не объявлена в дереве (сверять не с чем — названо,
// а не проглочено)» при объявленной величине: форма честной оговорки о границе
// поверх признания, что искали не там.
//
// Такую слепую зону синтетическое дерево не измеряет — оно содержит ровно то,
// что положила проба. Поэтому охват проверяется НА НАСТОЯЩЕМ СОСТАВЕ: величина
// обязана находиться, и обязана находиться ВНЕ прежнего поддерева. Второе
// условие несущее: найдись она сегодня внутри `services/iam/internal`, прежний
// охват был бы достаточен, и утверждение о закрытой слепой зоне ничего бы не
// стоило.
//
// # Почему сверка вызывается напрямую, а не через настоящее дерево
//
// Уронить настоящее дерево нарочно нельзя, а гейт, который нельзя уронить, не
// отличается от гейта, который не может упасть. Поэтому суждение отделено от
// добычи (`adjudicateScopeDepth`) и здесь получает величины на вход.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// scopeDepthPreviousScope — поддерево, которым ограничивался ПРЕЖНИЙ обход.
// Здесь оно нужно ровно затем, чтобы выбрать величину ВНЕ него, и больше нигде
// в надзоре не участвует.
const scopeDepthPreviousScope = "services/iam/internal"

func TestScopeDepthAgreement_ProvenByInjection(t *testing.T) {
	root := repoRoot(t)

	// ── КОНТРОЛЬ. Без него краснота ниже неотличима от красноты дерева ──────
	planDepth, planFound := findPlanDepth(t, root)
	if !planFound {
		t.Fatal("предел компилятора модели в дереве НЕ НАЙДЕН — доказывать нечего.\n" +
			"    Либо величину сняли (тогда снимите и эту пробу вместе с предметом), " +
			"либо обход снова смотрит не туда — то есть вернулся дефект #918.")
	}
	t.Logf("контроль: предел компилятора модели найден, значение %d", planDepth)

	t.Run("охват: величина лежит ВНЕ прежнего поддерева — слепая зона закрыта", func(t *testing.T) {
		files, err := treecorpus.UnderWithSuffix(root, ".go")
		if err != nil {
			t.Fatalf("состав дерева взять неоткуда: %v", err)
		}
		var all, insidePreviousScope int
		for _, abs := range files {
			if strings.HasSuffix(abs, "_test.go") {
				continue
			}
			rel, rerr := filepath.Rel(root, abs)
			if rerr != nil {
				continue
			}
			body, rerr := os.ReadFile(abs)
			if rerr != nil || !rePlanDepth.Match(body) {
				continue
			}
			all++
			if strings.HasPrefix(filepath.ToSlash(rel), scopeDepthPreviousScope+"/") {
				insidePreviousScope++
			}
		}
		t.Logf("ОБЪЁМ ОСМОТРЕННОГО: файлов Go в составе %d; объявлений с числом %d, "+
			"из них под %s — %d", len(files), all, scopeDepthPreviousScope, insidePreviousScope)

		if all == 0 {
			t.Fatal("объявлений с числом ноль — предикат перестал находить свой предмет")
		}
		if insidePreviousScope == all {
			t.Fatalf("ВСЕ %d объявления лежат под %s: прежнего охвата хватило бы, и "+
				"утверждение о закрытой слепой зоне ничего не стоит. Проба обязана "+
				"целиться в зону, которой прежний обход не видел.",
				all, scopeDepthPreviousScope)
		}
		if insidePreviousScope > 0 {
			t.Logf("часть объявлений (%d из %d) видел бы и прежний обход — доказательство "+
				"держится на остальных", insidePreviousScope, all)
		}
	})

	t.Run("расхождение третьей величины краснеет и называет ОБЕ координаты", func(t *testing.T) {
		found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 4, planDepth: 3, planFound: true,
		})
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		for _, want := range []string{scopeDepthPlanFile, scopeDepthConstFile, "3", "4"} {
			if !strings.Contains(found[0], want) {
				t.Fatalf("находка не называет %q — по ней не видно, что чинить:\n%s", want, found[0])
			}
		}
	})

	t.Run("расхождение первых двух краснеет и называет ОБЕ координаты", func(t *testing.T) {
		found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 2, planDepth: 4, planFound: true,
		})
		if len(found) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(found), found)
		}
		for _, want := range []string{scopeDepthConstFile, scopeDepthMigration} {
			if !strings.Contains(found[0], want) {
				t.Fatalf("находка не называет %q:\n%s", want, found[0])
			}
		}
	})

	t.Run("расходятся все три — названы обе пары, а не первая попавшаяся", func(t *testing.T) {
		found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 2, planDepth: 3, planFound: true,
		})
		if len(found) != 2 {
			t.Fatalf("ожидались 2 находки, получено %d: %v", len(found), found)
		}
	})

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ. Без них отрицание зеленело бы на всём сломанном ──
	t.Run("согласие трёх величин — сверка молчит", func(t *testing.T) {
		if found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 4, planDepth: 4, planFound: true,
		}); len(found) != 0 {
			t.Fatalf("ложное срабатывание на согласии: %v", found)
		}
	})

	t.Run("ненайденная третья величина НЕ выдумывает расхождения", func(t *testing.T) {
		// planDepth здесь — нулевое значение, а не «ноль как предел». Судить по
		// нему значило бы краснеть на дереве, где величины просто нет.
		if found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 4, sqlDepth: 4, planFound: false,
		}); len(found) != 0 {
			t.Fatalf("ненайденная величина принята за расхождение: %v", found)
		}
	})

	t.Run("согласие на ДРУГОМ числе — сверка по равенству, а не по литералу 4", func(t *testing.T) {
		if found := adjudicateScopeDepth(scopeDepthTriple{
			goDepth: 7, sqlDepth: 7, planDepth: 7, planFound: true,
		}); len(found) != 0 {
			t.Fatalf("сверка привязана к литералу, а не к равенству: %v", found)
		}
	})
}

// TestScopeDepthPlanFileCoordinateIsAlive — координата в тексте находки обязана
// существовать.
//
// Она не участвует в обходе (тот ищет по всему дереву), поэтому её устаревание
// ничего не роняет само по себе: находка просто начнёт посылать читателя в
// несуществующий файл. Ровно этот класс #918 и был — сообщение о границе,
// пережившее свою границу.
func TestScopeDepthPlanFileCoordinateIsAlive(t *testing.T) {
	root := repoRoot(t)
	tree, err := treecorpus.NewTree(root)
	if err != nil {
		t.Fatalf("состав дерева взять неоткуда: %v", err)
	}
	if !tree.HasFile(scopeDepthPlanFile) {
		t.Fatalf("координата третьей величины %q в составе дерева отсутствует: находка "+
			"послала бы читателя в файл, которого нет. Осмотрено файлов: %d",
			scopeDepthPlanFile, tree.Count())
	}
	t.Logf("ОБЪЁМ ОСМОТРЕННОГО: состав дерева %d файлов, координата %s жива",
		tree.Count(), scopeDepthPlanFile)
}
