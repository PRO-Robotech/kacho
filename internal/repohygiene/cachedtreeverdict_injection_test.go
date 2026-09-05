// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cachedtreeverdict_injection_test.go — доказательство, что гейт стража
// кешированного вердикта СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// По каждой оси — обе стороны: дефект обязан находиться, законный близнец обязан
// молчать. Односторонняя проба зеленела бы на гейте, отвергающем всё, и на гейте,
// не отвергающем ничего.

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// ─────────────── ось 1: пакет проб обязан объявить стража ───────────────

func TestCachedVerdictGate_PackageAxisFindsAndSpares(t *testing.T) {
	t.Run("краснеет: пакет проб без TestMain", func(t *testing.T) {
		f := judgeGuardedPackages([]testMainFacts{{pkgDir: "internal/x"}})
		if len(f) != 1 || !strings.Contains(f[0], "internal/x") {
			t.Fatalf("пакет без стража не найден: %v", f)
		}
	})

	t.Run("краснеет: TestMain есть, до стража не доходит", func(t *testing.T) {
		f := judgeGuardedPackages([]testMainFacts{{pkgDir: "internal/x", declaresTestMain: true}})
		if len(f) != 1 || !strings.Contains(f[0], guardName) {
			t.Fatalf("форма стража без содержания не найдена: %v", f)
		}
	})

	t.Run("молчит: TestMain доходит до стража", func(t *testing.T) {
		f := judgeGuardedPackages([]testMainFacts{
			{pkgDir: "internal/x", declaresTestMain: true, reachesGuard: true},
		})
		if len(f) != 0 {
			t.Fatalf("гейт краснеет на прикрытом пакете: %v", f)
		}
	})
}

// TestCachedVerdictGate_TestMainInAStringLiteralIsNotADeclaration — собственная
// предпосылка оси 1.
//
// Это НЕ теоретическая осторожность. В этом дереве объявление TestMain стоит
// дословно внутри строкового литерала фикстуры соседнего гейта, и поиск по тексту
// объявил бы пакет прикрытым при отсутствующем страже. Проба подаёт обе формы
// РЯДОМ: настоящее объявление обязано считаться, литерал — нет.
func TestCachedVerdictGate_TestMainInAStringLiteralIsNotADeclaration(t *testing.T) {
	// Нарушитель: TestMain только в тексте фикстуры, объявления нет.
	inLiteral := "package x\n\n" +
		"const fixture = `package y\n" +
		"func TestMain(m *testing.M) { treecorpus.CachedVerdictRefusal(); os.Exit(m.Run()) }\n" +
		"`\n"
	// Законный близнец: то же самое ПЛЮС настоящее объявление.
	declared := inLiteral + "\nfunc TestMain(m *testing.M) { _ = CachedVerdictRefusal() }\n"

	if declares, _ := testMainFactsFromAST(mustParse(t, inLiteral)); declares {
		t.Fatal("объявление найдено там, где есть только строковый литерал: " +
			"гейт ловил бы форму, а не существо, и первая же фикстура прикрыла бы пакет")
	}
	declares, guarded := testMainFactsFromAST(mustParse(t, declared))
	if !declares || !guarded {
		t.Fatalf("настоящее объявление не распознано (declares=%v, guarded=%v) — "+
			"тогда гейт краснел бы на каждом прикрытом пакете", declares, guarded)
	}
}

// ─────────── ось 2: конструктор реального индекса обязан звать стража ───────────

func TestCachedVerdictGate_ConstructorAxisFindsAndSpares(t *testing.T) {
	t.Run("краснеет: читает индекс, стража не зовёт", func(t *testing.T) {
		f := judgeGuardedConstructors([]constructorFacts{{name: "Third", reachesIndex: true}})
		if len(f) != 1 || !strings.Contains(f[0], "Third") {
			t.Fatalf("конструктор мимо стража не найден: %v", f)
		}
	})

	t.Run("молчит: читает индекс и зовёт стража", func(t *testing.T) {
		f := judgeGuardedConstructors([]constructorFacts{
			{name: "Under", reachesIndex: true, reachesGuard: true},
		})
		if len(f) != 0 {
			t.Fatalf("гейт краснеет на прикрытом конструкторе: %v", f)
		}
	})

	// Законный близнец, без которого гейт требовал бы стража от конструктора
	// СИНТЕТИЧЕСКОГО дерева: тот индекса не спрашивает, и его вердикт кешируется
	// законно — он свойство пакета, а не репозитория.
	t.Run("молчит: индекса не читает, стража не зовёт", func(t *testing.T) {
		f := judgeGuardedConstructors([]constructorFacts{{name: "SyntheticTree"}})
		if len(f) != 0 {
			t.Fatalf("гейт краснеет на конструкторе синтетического дерева: %v", f)
		}
	})
}

// ─────────── ось 3: рецепт Makefile обязан отключать кеш ───────────

func TestCachedVerdictGate_MakefileAxisFindsAndSpares(t *testing.T) {
	// Настоящий ввод: кусок Makefile в той же форме, что и в дереве, —
	// с целями, продолжениями строк и объяснением в комментариях.
	makefile := "" +
		"## test-unit — юниты всего дерева.\n" +
		"##\n" +
		"##   go test ./internal/repohygiene/... -race -short\n" +
		"test-unit:\n" +
		"\t$(GO) test ./... -race -short -count=1 -timeout $(UNIT_TIMEOUT)\n" +
		"\n" +
		"scale-grid:\n" +
		"\t$(GO) test ./services/iam/internal/repo/kaname/pg/relverdict/ \\\n" +
		"\t  -run TestScaleGrid \\\n" +
		"\t  -count=1 -v\n" +
		"\n" +
		"bad-target:\n" +
		"\tgo test ./internal/repohygiene/ -race -short\n"

	var recipes []makeRecipe
	for _, ln := range makefileLogicalLines(makefile) {
		if recipeInvokesGoTest(ln.text) {
			ln.file = "Makefile"
			recipes = append(recipes, ln)
		}
	}

	// Положительный контроль объёма: строка `##   go test …` — ОБЪЯСНЕНИЕ, а не
	// рецепт. Не сними гейт комментарии, он краснел бы на собственной шапке.
	if len(recipes) != 3 {
		t.Fatalf("рецептов распознано %d, ожидалось 3 — обход читает не исполняемую часть: %v",
			len(recipes), recipes)
	}

	f := judgeMakefileRecipes(recipes)
	if len(f) != 1 || !strings.Contains(f[0], "Makefile:") {
		t.Fatalf("рецепт без отключения кеша не найден ровно один раз: %v", f)
	}
	// Законный близнец назван поимённо: цель с продолжением строки, где
	// отключение кеша стоит на ТРЕТЬЕЙ строке рецепта.
	for _, finding := range f {
		if strings.Contains(finding, "scale-grid") {
			t.Fatalf("гейт краснеет на склеенном рецепте, несущем отключение кеша: %v", f)
		}
	}
}

func mustParse(t *testing.T, src string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", src, 0)
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	return file
}
