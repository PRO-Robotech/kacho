// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция в обе стороны. Пары ОДНОФАКТНЫЕ: дефект и законный близнец
// различаются ровно одним числом, и полярность утверждения в этот факт не
// входит.

// healthyTreeWalk — исправный обход при живом предмете. Числа порядка
// сегодняшнего дерева, но вердикт от их величины не зависит: он зависит от
// СХОДИМОСТИ пар и от предпосылки продукта.
func healthyTreeWalk() (TreeWalkCensus, TreeWalkDenominator) {
	return TreeWalkCensus{
			Gate: "TestSome", Walked: 3578, Judged: 3578, Subjects: 3578, Unit: "файлов Go",
		}, TreeWalkDenominator{
			CommitPaths: 3578, Exact: true,
			Expression: "git ls-tree -r HEAD -- *.go", BuildGraphEdges: 2,
			ModulePath:        "github.com/PRO-Robotech/kacho",
			PkgPath:           "github.com/PRO-Robotech/kacho/internal/repohygiene",
			SelfFilesInCommit: 884,
		}
}

func treeWalkBlindBecause(t *testing.T, v TreeWalkVerdict, substr string) {
	t.Helper()
	if v.Outcome != TreeWalkFailed {
		t.Fatalf("исход %q, ожидался %q — обход, который не состоялся, назван вердиктом",
			v.Outcome, TreeWalkFailed)
	}
	for _, r := range v.Blind {
		if strings.Contains(r, substr) {
			return
		}
	}
	t.Fatalf("исход слепой, но ни одна причина не называет %q: %v — находка, называющая "+
		"симптом вместо предмета, чинится не там", substr, v.Blind)
}

// TestTreeWalkInjection_HealthyWalkHasItsSubject — законный близнец всех
// инъекций ниже.
func TestTreeWalkInjection_HealthyWalkHasItsSubject(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	v := JudgeTreeWalk(c, d)
	if v.Outcome != TreeWalkSubjectPresent {
		t.Fatalf("исход %q, ожидался %q; причины: %v", v.Outcome, TreeWalkSubjectPresent, v.Blind)
	}
	if len(v.Blind) != 0 {
		t.Fatalf("исправный обход назвал причины слепоты: %v", v.Blind)
	}
}

// TestTreeWalkInjection_EmptySubjectIsItsOwnNamedOutcome — ПРЕДМЕТ: предмета в
// дереве нет, и это ОТДЕЛЬНЫЙ названный зелёный, а не тот же самый.
//
// Один факт против близнеца: предметов ноль. Знаменатель тот же.
func TestTreeWalkInjection_EmptySubjectIsItsOwnNamedOutcome(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Subjects = 0

	v := JudgeTreeWalk(c, d)
	if v.Outcome != TreeWalkSubjectAbsent {
		t.Fatalf("исход %q, ожидался %q — «предмета нет» слито с «предмет есть», и "+
			"снятие класса нечем отличить от обычного зелёного; причины: %v",
			v.Outcome, TreeWalkSubjectAbsent, v.Blind)
	}
	if len(v.Blind) != 0 {
		t.Fatalf("снятый предмет назван слепотой: %v — проба падает на достижении "+
			"собственной цели", v.Blind)
	}
}

// TestTreeWalkInjection_TreeThatIsNotTheProductIsBlind — ПРЕДМЕТ: дерево, где
// считать было нечего, потому что это не продукт.
//
// Один факт: внутренних рёбер сборочного графа ноль. Обход при этом ЧЕСТЕН —
// первое выражение сошлось со вторым, — и ровно поэтому ни одно число о самом
// классе такой случай не ловит.
func TestTreeWalkInjection_TreeThatIsNotTheProductIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Walked, c.Judged, c.Subjects = 1, 1, 1
	d.CommitPaths = 1
	d.BuildGraphEdges = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "сборочного графа")
}

// TestTreeWalkInjection_OneEdgeIsEnough — ГРАНИЦА предыдущего с законной
// стороны: одно внутреннее ребро делает дерево продуктом.
func TestTreeWalkInjection_OneEdgeIsEnough(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.BuildGraphEdges = 1

	if v := JudgeTreeWalk(c, d); v.Outcome == TreeWalkFailed {
		t.Fatalf("дерево с одним внутренним ребром названо не-продуктом: %v", v.Blind)
	}
}

// TestTreeWalkInjection_EmptyCommitSelectionIsBlind — в коммите ноль путей того
// вида, который гейт судит.
func TestTreeWalkInjection_EmptyCommitSelectionIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Walked, c.Judged, c.Subjects = 0, 0, 0
	d.CommitPaths = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "в коммите ноль путей")
}

// TestTreeWalkInjection_ShortWalkIsBlind — обход недобрал пути коммита.
func TestTreeWalkInjection_ShortWalkIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Walked = 1

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "недобрал")
}

// TestTreeWalkInjection_OnePathShortIsBlind — ГРАНИЦА: недобран РОВНО один путь.
// Порог не имеет запаса, потому что незамеченным пропадает один файл, а не сотня.
func TestTreeWalkInjection_OnePathShortIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Walked--

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "недобрал")
}

// TestTreeWalkInjection_WalkAheadOfTheCommitIsBlind — зеркальная сторона: обход
// прочитал БОЛЬШЕ, чем лежит в коммите. Так выглядит работа, оставленная в
// индексе.
func TestTreeWalkInjection_WalkAheadOfTheCommitIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Walked++

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "осталась в индексе")
}

// TestTreeWalkInjection_InexactSelectionAllowsContainment — законный близнец
// тождества: отбор коммита ШИРЕ отбора обхода, и тогда требуется вложение.
func TestTreeWalkInjection_InexactSelectionAllowsContainment(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.Exact = false
	c.Walked = 1200

	if v := JudgeTreeWalk(c, d); v.Outcome == TreeWalkFailed {
		t.Fatalf("вложенный отбор назван расхождением: %v", v.Blind)
	}
}

// TestTreeWalkInjection_InexactSelectionStillRefusesTheEmptyWalk — та же ветвь с
// дефектной стороны: вложение не оправдывает ПУСТОГО обхода.
func TestTreeWalkInjection_InexactSelectionStillRefusesTheEmptyWalk(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.Exact = false
	c.Walked = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "ВЛОЖЕН")
}

// TestTreeWalkInjection_NothingReachedTheJudgeIsBlind — обход был, а суда не
// было: путь отфильтрован целиком.
func TestTreeWalkInjection_NothingReachedTheJudgeIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Judged = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "суда не было")
}

// TestTreeWalkInjection_EveryBlindReasonIsNamed — судья, называющий ОДНУ
// причину, прячет остальные: чинить его пришлось бы по одной за прогон.
func TestTreeWalkInjection_EveryBlindReasonIsNamed(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	c.Judged = 0
	c.Walked = 1
	d.BuildGraphEdges = 0

	v := JudgeTreeWalk(c, d)
	if v.Outcome != TreeWalkFailed {
		t.Fatalf("исход %q, ожидался %q", v.Outcome, TreeWalkFailed)
	}
	if len(v.Blind) < 3 {
		t.Fatalf("названо причин %d при трёх внесённых: %v", len(v.Blind), v.Blind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ВТОРАЯ ПРЕДПОСЫЛКА — О НАШЕМ ДЕРЕВЕ. Пары однофактные: первая предпосылка
// (внутренние рёбра) во всех четырёх ВЫПОЛНЕНА, чтобы видно было, что работает
// именно вторая.

// TestTreeWalkInjection_ForeignModuleIsBlind — дерево объявляет ЧУЖОЙ модуль.
//
// Так выглядит прогон гейта внутри постороннего продукта на Go: обход честен,
// файлы есть, внутренние рёбра есть — а судит он чужой код.
func TestTreeWalkInjection_ForeignModuleIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.ModulePath = "github.com/klauspost/compress"
	d.SelfFilesInCommit = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "НЕ его дерево")
}

// TestTreeWalkInjection_OurModuleNameWithoutOurGateIsBlind — ПРЕДМЕТ: дереву
// приписали имя нашего модуля.
//
// Один факт против здорового близнеца: файлов собственного пакета гейта в
// коммите ноль. Первая предпосылка выполнена — две строки в файле модуля дают
// и имя, и внутренние рёбра, — и ровно поэтому одной её недостаточно.
func TestTreeWalkInjection_OurModuleNameWithoutOurGateIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.SelfFilesInCommit = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "имя нашего модуля просто приписали")
}

// TestTreeWalkInjection_OneSelfFileIsEnough — ГРАНИЦА предыдущего с законной
// стороны: ОДНОГО файла собственного пакета в коммите достаточно.
func TestTreeWalkInjection_OneSelfFileIsEnough(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.SelfFilesInCommit = 1

	if v := JudgeTreeWalk(c, d); v.Outcome == TreeWalkFailed {
		t.Fatalf("дерево с одним файлом собственного пакета названо чужим: %v", v.Blind)
	}
}

// TestTreeWalkInjection_TreeWithoutAModuleIsBlind — дерево не объявляет модуля
// вовсе: объяснять путь пакета гейта нечем.
func TestTreeWalkInjection_TreeWithoutAModuleIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.ModulePath = ""
	d.SelfFilesInCommit = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "не объявляет модуля")
}

// TestTreeWalkInjection_UnknownPkgPathIsBlind — путь пакета гейта не установлен:
// вторая предпосылка беспредметна, и молчать об этом нельзя.
func TestTreeWalkInjection_UnknownPkgPathIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.PkgPath = ""

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "путь пакета гейта не установлен")
}

// ─────────────────────────────────────────────────────────────────────────────
// ТРЕТИЙ ИСХОД: СПРОСИТЬ НЕ УДАЛОСЬ

// TestTreeWalkInjection_UnreadableTreeIsItsOwnReason — отказ инструмента не есть
// свойство дерева.
//
// Прежде добытчики знаменателя возвращали НОЛЬ и на «такого в коммите нет», и
// на «git не ответил»: один ноль в двух разных мирах. Теперь у второго своя
// причина, и она называется первой — пока она жива, числа ниже считаны по
// неполному знаменателю.
func TestTreeWalkInjection_UnreadableTreeIsItsOwnReason(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.Unreadable = "состав коммита не прочитан: exit status 128"

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "отказ инструмента")
}

// TestTreeWalkInjection_ReadableTreeSaysNothingAboutTheTool — ЗАКОННЫЙ БЛИЗНЕЦ:
// тот же знаменатель без отказа инструмента даёт вердикт о предмете.
func TestTreeWalkInjection_ReadableTreeSaysNothingAboutTheTool(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()

	if v := JudgeTreeWalk(c, d); v.Outcome == TreeWalkFailed {
		t.Fatalf("исправно прочитанное дерево названо несостоявшимся обходом: %v", v.Blind)
	}
}

// TestTreeWalkInjection_UnreadableComesFirst — причина инструмента называется
// ПЕРВОЙ: объяснять остальные числа свойствами дерева, пока она жива, нельзя.
func TestTreeWalkInjection_UnreadableComesFirst(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.Unreadable = "файл модуля коммита не прочитан: exit status 128"
	d.BuildGraphEdges = 0
	d.SelfFilesInCommit = 0

	v := JudgeTreeWalk(c, d)
	if v.Outcome != TreeWalkFailed {
		t.Fatalf("исход %q, ожидался %q", v.Outcome, TreeWalkFailed)
	}
	if len(v.Blind) < 2 || !strings.Contains(v.Blind[0], "отказ инструмента") {
		t.Fatalf("первой названа причина %q — отказ инструмента обязан идти первым, "+
			"иначе читатель объяснит числа свойствами дерева: %v", v.Blind[0], v.Blind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ПАКЕТ ГЕЙТА В КОРНЕ МОДУЛЯ

// TestTreeWalkInjection_PackageAtTheModuleRootIsNotForeign — путь пакета РАВЕН
// модулю: собственные файлы лежат в корне, и обвинять своё дерево не за что.
//
// Без этой ветви префикс с косой чертой не совпадал, файлов собственного пакета
// насчитывалось ноль, и судья называл НАШЕ дерево чужим.
func TestTreeWalkInjection_PackageAtTheModuleRootIsNotForeign(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.ModulePath = "github.com/PRO-Robotech/kacho"
	d.PkgPath = "github.com/PRO-Robotech/kacho"
	d.SelfFilesInCommit = 7

	if v := JudgeTreeWalk(c, d); v.Outcome == TreeWalkFailed {
		t.Fatalf("пакет в корне модуля назван чужим деревом: %v", v.Blind)
	}
}

// TestTreeWalkInjection_PackageAtTheModuleRootWithoutFilesIsBlind — та же ветвь
// с дефектной стороны: корень модуля объявлен, а файлов в нём нет.
func TestTreeWalkInjection_PackageAtTheModuleRootWithoutFilesIsBlind(t *testing.T) {
	t.Parallel()
	c, d := healthyTreeWalk()
	d.ModulePath = "github.com/PRO-Robotech/kacho"
	d.PkgPath = "github.com/PRO-Robotech/kacho"
	d.SelfFilesInCommit = 0

	treeWalkBlindBecause(t, JudgeTreeWalk(c, d), "имя нашего модуля просто приписали")
}

// ─────────────────────────────────────────────────────────────────────────────
// ОТБОР И ЕГО ОПИСАНИЕ — ОДНА ПАРА

// TestTreeWalkInjection_SelectorCarriesItsOwnDescription — описание отбора
// нельзя прочитать, не взяв тот же предикат.
//
// Прежде описание приезжало отдельной строкой рядом с предикатом и могло
// разъехаться с ним молча: правишь предикат — строка остаётся, и знаменатель
// печатает про один отбор, а считает другой.
func TestTreeWalkInjection_SelectorCarriesItsOwnDescription(t *testing.T) {
	t.Parallel()
	for _, sel := range []TreeSelector{
		treeWalkKeepAll, treeWalkKeepGo, treeWalkKeepProdGo,
		treeWalkKeepChart, treeWalkSelfPackage,
	} {
		if sel.Describe == "" {
			t.Errorf("отбор без описания: знаменатель нечем перепроверить")
		}
		if sel.Match == nil {
			t.Errorf("описание %q без отбора: перепись говорила бы о предикате, "+
				"которого нет", sel.Describe)
		}
	}
	// Отбор и описание обязаны быть СОГЛАСНЫ: проверяем на входе, который
	// описание накрывать не должно.
	if treeWalkKeepProdGo.Match("services/x/a_test.go") {
		t.Error("отбор «непроверочные» принял проверочный файл — описание и предикат разошлись")
	}
	if !treeWalkKeepGo.Match("services/x/a_test.go") {
		t.Error("отбор «все *.go» отверг файл Go — описание и предикат разошлись")
	}
	if treeWalkKeepChart.Match("services/x/values.yaml") {
		t.Error("отбор чартов принял путь вне deploy/ — описание и предикат разошлись")
	}
	if !treeWalkSelfPackage.Match("internal/repohygiene/a.go") {
		t.Error("отбор собственного пакета отверг обычный файл пакета — разбор снова " +
			"видел бы только пробные")
	}
}
