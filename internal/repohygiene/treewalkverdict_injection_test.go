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
