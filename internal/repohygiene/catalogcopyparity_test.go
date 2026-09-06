// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogcopyparity_test.go — гейты шва «две вшитые копии каталога прав обязаны
// сверяться, и сверка обязана исполняться».
//
// Утверждений ЧЕТЫРЕ, и они о РАЗНОМ. Три живут здесь, четвёртое — в соседнем
// файле, потому что читает другое дерево и другим механизмом:
//
//  1. КЛАСС по дереву — ни одна сверка ни в одном Makefile не исполняется
//     условно по наличию файла. Структурное свойство дерева, читается разбором
//     рецептов.
//  2. ПОВЕДЕНИЕ цели — сверка копий каталога прав на целом дереве проходит.
//     Читается ЗАПУСКОМ, а не прочтением: объявление и исход — разные предметы,
//     и корпус требует судить исход.
//  3. СВЯЗЬ ВНУТРИ Makefile — цель `permission-catalog-check` обязана звать
//     сверку: зависимостью в заголовке правила либо вызовом в рецепте. Читается
//     разбором gateway/Makefile, где заголовок и тело рецепта судятся порознь.
//  4. ПРОВЯЗКА С КОНВЕЙЕРОМ — эту цепочку обязан звать хотя бы один шаг
//     объявленного процесса. Живёт в catalogcheckwiring_test.go (#2084): предмет
//     у неё другой — не Makefile, а .github/workflows, — и механизм тоже другой,
//     общий с гейтами судей services/* (gatetargetwiring.go).
//
// Ни одно не заменяет трёх остальных. Первое зелено при мёртвой цели; второе —
// при цели, которую никто не зовёт; третье — при сверке, которая ничего не
// сверяет; четвёртое — при сверке, вызов которой сняли из конвейера, не тронув
// ни одной строки Makefile.
//
// # Здесь стояло ТРИ утверждения, и третье было ШИРЕ своей пробы (#2084)
//
// Прежняя редакция называла третье «цель, которую ЗОВЁТ КОНВЕЙЕР, обязана звать
// сверку». Оборот «которую зовёт конвейер» был ПРЕДПОСЫЛКОЙ, не утверждаемой
// ничем: проба читала только gateway/Makefile и о .github/workflows не
// высказывалась ни строкой. Снятие шага из ci.yaml оставляло её зелёной, а
// сверку — исполнимой и неисполняемой. Четвёртое утверждение заведено ровно на
// этот шов: предпосылка стала предметом, и шапка теперь совпадает с тем, что
// пробы делают.
//
// # Почему сверка вынесена ОТДЕЛЬНОЙ целью
//
// Она не требует ни buf, ни proto-дерева — это сравнение двух закоммиченных
// файлов. Пока она стояла внутри `permission-catalog-check`, её поведение было
// недоступно `go test`: первым же оператором рецепта идёт генерация каталога.
// Отдельная цель делает свойство проверяемым запуском везде, а не только в той
// единственной джобе конвейера, где стоит buf.
package repohygiene

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// catalogParityTarget — цель, исполняющая сверку двух копий каталога прав.
const catalogParityTarget = "permission-catalog-copies-in-sync"

// catalogCheckTarget — цель, зовущая сверку своей зависимостью; её же зовёт
// конвейер.
//
// Координата вызывающего здесь намеренно НЕ выписана: имя файла и заголовок шага
// стареют молча, а утверждение «её зовёт конвейер» держит проба
// TestCatalogCopyParityTargetIsCalledByThePipeline, которая выводит перечень
// процессов из дерева.
const catalogCheckTarget = "permission-catalog-check"

// makefilesOfTree — отслеживаемые Makefile и *.mk дерева.
//
// Состав берётся из ИНДЕКСА git, а не с диска: обход диска втянул бы рабочие
// копии агентов и распаковки чартов, и вердикт стал бы свойством рабочего
// каталога, а не коммита.
func makefilesOfTree(t *testing.T, root string) []string {
	t.Helper()
	all, err := treecorpus.Under(root)
	if err != nil {
		t.Fatalf("состав дерева не прочитан: %v — вердикт беспредметен", err)
	}
	var out []string
	for _, abs := range all {
		base := filepath.Base(abs)
		if base == "Makefile" || strings.HasSuffix(base, ".mk") {
			out = append(out, abs)
		}
	}
	return out
}

// TestNoMakefileRunsAComparisonOnlyIfAFileExists — КЛАСС: сверка, обёрнутая
// условием существования файла, не сверяет ничего с того дня, как файл уедет.
//
// Что делать, если гейт сработал: снять обёртку и завести на её месте ЯВНЫЙ
// ОТКАЗ, называющий недостающий путь и причину, — по образцу
// `services/iam/Makefile`, цель `sync-permission-catalog`. Обёртку обратно не
// возвращать и ведомость прощённых не заводить: пока условие ложно, цель зелена
// и неотличима от исправной, поэтому у такого послабления нет предиката, по
// которому его снимут.
func TestNoMakefileRunsAComparisonOnlyIfAFileExists(t *testing.T) {
	root := repoRoot(t)
	makefiles := makefilesOfTree(t, root)

	var (
		found          []guardedComparison
		physical       int
		recipeLines    int
		logicalRecipes int
		comparisons    int
	)
	for _, abs := range makefiles {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			t.Fatalf("относительный путь для %s: %v", abs, err)
		}
		recipes, perr := parseMakefileRecipes(abs)
		if perr != nil {
			t.Fatalf("рецепты не разобраны: %v — вердикт беспредметен", perr)
		}
		physical += recipes.PhysicalLines
		recipeLines += recipes.RecipeLinesRaw
		logicalRecipes += len(recipes.Lines)
		g, n := findGuardedComparisons(rel, recipes)
		found = append(found, g...)
		comparisons += n
	}

	// ── ПРЕДПОСЫЛКИ ОБХОДА ──────────────────────────────────────────────────
	//
	// Пустой обход обязан ронять прогон: иначе «находок ноль» означало бы «ноль
	// прочитанного», и эти два состояния печатались бы одинаково.
	if len(makefiles) == 0 {
		t.Fatal("в индексе нет ни одного Makefile — гейт смотрит не туда, его вердикт беспредметен")
	}
	if logicalRecipes == 0 {
		t.Fatalf("прочитано %d Makefile и ни одной строки рецепта — разбор сломан", len(makefiles))
	}
	// Положительный контроль РАСПОЗНАВАТЕЛЯ: сверки в дереве есть, и гейт обязан
	// их видеть. Ноль сверок означает, что сломалось распознавание команды, а не
	// что дерево чисто, — и тогда молчание гейта не значит ничего.
	if comparisons == 0 {
		t.Fatalf("ни одной сверки (diff/cmp) не распознано в %d строках рецептов — "+
			"распознаватель команды сломан; «обёрнутых сверок ноль» тогда означает "+
			"«прочитано ноль сверок»", logicalRecipes)
	}

	t.Logf("перепись: Makefile %d · строк файлов %d · строк рецепта %d (логических %d) · "+
		"сверок осмотрено %d · обёрнутых условием %d",
		len(makefiles), physical, recipeLines, logicalRecipes, comparisons, len(found))

	if len(found) > 0 {
		var lines []string
		for _, g := range found {
			lines = append(lines, g.File+":"+itoa(g.Line)+" (цель "+g.Target+"): "+g.Text)
		}
		t.Fatalf("сверок, исполняемых условно по наличию файла: %d. Такая сверка не "+
			"сверяет НИЧЕГО с того дня, как файл перестанет существовать, — и цель "+
			"остаётся зелёной, неотличимой от исправной. Замени условие явным отказом, "+
			"называющим путь и причину:\n  %s", len(found), strings.Join(lines, "\n  "))
	}
}

// runCatalogParity — запуск цели сверки с переопределением операндов.
//
// Переопределение идёт АРГУМЕНТОМ make, а не переменной окружения: аргумент
// перебивает присваивание в Makefile, окружение — нет, и молчаливое
// переопределение из окружения дало бы пробе судить не то, что судит конвейер.
func runCatalogParity(t *testing.T, root string, overrides ...string) (string, int) {
	t.Helper()
	args := append([]string{"-C", filepath.Join(root, "gateway"), "--no-print-directory", catalogParityTarget}, overrides...)
	cmd := exec.Command("make", args...) //nolint:gosec // аргументы — литералы пробы и пути из t.TempDir()
	out, err := cmd.CombinedOutput()
	code := 0
	// asExitError (prverdictwait_test.go) отделяет «команда отработала и вернула
	// ненулевой код» от «команду не удалось запустить». Первое — вердикт, второе
	// — «не выполнилось», и смешивать их нельзя: непойманное «не выполнилось»
	// зачлось бы за находку.
	var ee *exec.ExitError
	if err != nil {
		if !asExitError(err, &ee) {
			t.Fatalf("make не запустился: %v — исход не наблюдался, вердикт беспредметен\n%s", err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// TestCatalogCopyParityPassesOnTheWholeTree — ПОВЕДЕНИЕ: на целом дереве сверка
// копий проходит, и она действительно что-то сверила.
//
// Это положительный контроль ко всем отрицаниям инъекции: без него «отсутствие
// копии даёт отказ» было бы верно и у цели, которая отказывает всегда.
func TestCatalogCopyParityPassesOnTheWholeTree(t *testing.T) {
	root := repoRoot(t)
	out, code := runCatalogParity(t, root)
	if code != 0 {
		t.Fatalf("цель %s на целом дереве не прошла (код %d) — копии каталога прав "+
			"разошлись либо цель сломана:\n%s", catalogParityTarget, code, out)
	}
	// Цель обязана отчитаться ОБЪЁМОМ: молчаливый успех неотличим от успеха,
	// при котором сверять было нечего.
	if !strings.Contains(out, "перепись") {
		t.Fatalf("цель %s прошла молча — в выводе нет переписи осмотренного, "+
			"поэтому «копии равны» неотличимо от «сверять было нечего»:\n%s",
			catalogParityTarget, out)
	}
	t.Logf("вывод цели: %s", strings.TrimSpace(out))
}

// TestCatalogCheckInvokesTheCopyParityTarget — СВЯЗЬ ВНУТРИ Makefile: цель,
// которую зовёт конвейер, обязана звать сверку копий.
//
// Без этого утверждения сверка остаётся исполнимой и неисполняемой: конвейер
// зовёт `permission-catalog-check`, и если сверка выпала из его зависимостей,
// расхождение копий снова не ловится ничем — только теперь тихо и без обёртки.
//
// Связь засчитывается в ДВУХ формах, и обе исполняемы: зависимость в заголовке
// правила и вызов `$(MAKE) <цель>` в рецепте. Имя цели, стоящее в комментарии,
// связью не является. Распознаватель — общий (makefileTargetsReaching,
// gatetargetwiring.go): им же гейт провязки с конвейером выводит множество
// целей, вызов которых исполняет сверку. Две копии этого предиката разошлись бы
// молча — и разошлись бы там, где обе зелены.
func TestCatalogCheckInvokesTheCopyParityTarget(t *testing.T) {
	root := repoRoot(t)
	reach, err := makefileTargetsReaching(
		filepath.Join(root, catalogMakefileDir, "Makefile"), catalogParityTarget)
	if err != nil {
		t.Fatalf("достижимость не прочитана: %v — вердикт беспредметен", err)
	}
	if reach.RecipeLines == 0 {
		t.Fatalf("в %s/Makefile не прочитано ни одной строки рецепта — разбор сломан", catalogMakefileDir)
	}

	// Положительный контроль: сама сверяющая цель обязана быть НАЙДЕНА
	// объявленной. Ноль объявлений означает, что цель сняли либо разбор сломан,
	// и тогда «связь в порядке» ничего не значит.
	if !reach.Declared {
		t.Fatalf("цель %s не объявлена в %s/Makefile (прочитано %d строк рецепта) — "+
			"сверять копии некому", catalogParityTarget, catalogMakefileDir, reach.RecipeLines)
	}

	wired := false
	for _, entry := range reach.Reaching {
		if entry == catalogCheckTarget {
			wired = true
		}
	}
	if !wired {
		t.Fatalf("цель %s объявлена, но %s её не зовёт — ни зависимостью, ни вызовом. "+
			"Конвейер зовёт %s, значит сверка копий исполнима и не исполняется. "+
			"Достигают сверку сейчас: %s",
			catalogParityTarget, catalogCheckTarget, catalogCheckTarget,
			strings.Join(reach.Reaching, ", "))
	}
	t.Logf("перепись: строк рецепта прочитано %d · целей, достигающих %s, %d (%s)",
		reach.RecipeLines, catalogParityTarget, len(reach.Reaching), strings.Join(reach.Reaching, ", "))
}
