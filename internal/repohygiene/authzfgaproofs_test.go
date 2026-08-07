// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// authzfgaproofs_test.go — поведенческие пробы модели прав обязаны ИСПОЛНЯТЬСЯ
// конвейером, и перечень исполняемого обязан совпадать с деревом.
//
// # Предмет
//
// Пробы, спрашивающие настоящий OpenFGA, отвечают на вопрос «изменились ли
// права» — тот самый, на который ссылаются решения о модели. До цели
// `test-authz-fga` их не исполняла НИ ОДНА джоба, и разрыв был невидим с обеих
// сторон: быстрая джоба гоняет `./... -race -short`, под которым каждая из них
// пропускается, а интеграционная отбирает пакеты ПО ПУТИ
// (`/internal/(repo|clients)` внутри services/) — ни один из шести туда не
// попадает. Пропущенный пакет печатает `ok`, поэтому «зелёное» ничего о них не
// говорило.
//
// Найдено приёмкой снятия глагола `v_create` (2026-08-06): несущим
// доказательством «в правах не изменилось ничего» предъявлялась проба, которую
// конвейер не запускает. Доказательство, которого никто не исполняет,
// доказательством не является — это третья категория исхода, «не выполнилось»,
// и она не вычитается из вердикта.
//
// # Три звена, и каждое проверяется отдельно
//
//  1. конвейер зовёт цель (`make test-authz-fga` в ci.yaml) — иначе цель есть,
//     а вердикта нет;
//  2. цель гоняет пробы БЕЗ краткого режима и запрещает им пропускать себя —
//     иначе шаг зелёный при нуле исполненного;
//  3. перечень пакетов цели совпадает с деревом — иначе новый пакет с пробами
//     модели прав тихо остаётся вне наблюдения, ровно как остались эти шесть.
//
// Третье звено — единственное, которое нельзя написать списком: список
// переживает своё дерево. Поэтому перечень СВЕРЯЕТСЯ с обходом, и расхождение
// красное в ОБЕ стороны — и пакет без объявления, и объявление без пакета.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает,
// сколько пакетов признал носителями проб и сколько прочитал из объявления;
// пустой обход и пустое объявление — провал, а не тишина.
package repohygiene

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fgaHarnessPkg — общий стенд настоящего OpenFGA. Пакет, чьи тесты его
// импортируют, спрашивает живой сервер, а не текст модели.
//
// Сам стенд в перепись входит ОТДЕЛЬНО (себя он не импортирует), и это не
// формальность: его собственные пробы держат инвариант «сервер один, области
// разные, данные между ними не ходят», на который ссылается санкция соседнего
// гейта (containerperpackage_test.go не считает вызов к нему стартом
// контейнера). Санкция, стоящая на непроверяемом утверждении, — тот же класс.
const fgaHarnessPkg = "services/iam/internal/testsupport/fgatest"

// authzFGAMakeTarget — цель корневого Makefile, несущая перечень и числовой
// вердикт. Гейт ищет ИМЕННО вызов цели, а не имя задания: перенос шага между
// заданиями не должен тихо разрывать провязку.
const authzFGAMakeTarget = "make test-authz-fga"

// TestCIRunsTheAuthzFGAProofs — первое звено: конвейер зовёт цель.
func TestCIRunsTheAuthzFGAProofs(t *testing.T) {
	root := repoRoot(t)
	// #nosec G304 -- читается ci.yaml этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yaml"))
	if err != nil {
		t.Fatalf("не прочитан ci.yaml — провязку нечем проверить: %v", err)
	}
	ci := string(raw)
	if !strings.Contains(ci, authzFGAMakeTarget) {
		t.Fatalf("ci.yaml не зовёт %q. Пробы модели прав пропускаются под кратким "+
			"режимом, а отбор интеграционной джобы до их пакетов не достаёт, — значит "+
			"без этого шага они не исполняются нигде, и их зелёное ничего не означает",
			authzFGAMakeTarget)
	}
	for _, line := range strings.Split(ci, "\n") {
		if strings.Contains(line, authzFGAMakeTarget) && strings.Contains(line, "-short") {
			t.Fatalf("ci.yaml зовёт цель с `-short`, под которым эти пробы и пропускаются: %s",
				strings.TrimSpace(line))
		}
	}
}

// TestAuthzFGATargetRefusesToSkip — второе звено: предпосылка самой цели.
//
// Вызов цели из конвейера ничего не стоит, если цель гоняет пробы так, что они
// вправе пропустить себя. Утверждается не текст рецепта ради текста, а три его
// свойства, без которых первое звено становится украшением.
//
// ЧИТАЕТСЯ РЕЦЕПТ, А НЕ ФАЙЛ, и это доказано, а не объявлено. Над целью стоит
// `##`-справка, в которой все три искомых слова присутствуют — она их и объясняет.
// Гейт по сырому тексту Makefile был бы зелёным при ЛЮБОМ рецепте, читая
// собственное объяснение. Класс известен этому же пакету: `executablePart` в
// shortgatedselection_test.go заведён ровно потому, что первая редакция соседнего
// гейта покраснела на собственном каталоге, а pipefailguard_test.go отбрасывает
// комментарии по той же причине.
// Проверено инъекцией: слово убрано из строки рецепта и оставлено в справке — гейт
// покраснел. `authzFGARecipe` берёт только строки, начинающиеся с табуляции, то есть
// то, что make действительно исполнит.
func TestAuthzFGATargetRefusesToSkip(t *testing.T) {
	recipe := authzFGARecipe(t, repoRoot(t))
	if !strings.Contains(recipe, "KACHO_IAM_REQUIRE_REAL_FGA") {
		t.Error("рецепт цели не выставляет KACHO_IAM_REQUIRE_REAL_FGA — пробы вправе " +
			"пропустить себя при недоступном Docker, и шаг останется зелёным при нуле " +
			"исполненного")
	}
	if strings.Contains(recipe, "-short") {
		t.Error("рецепт цели передаёт `-short` — под ним эти пробы пропускаются, то есть " +
			"цель заведена ради исхода, которого сама себе не даёт")
	}
	if !strings.Contains(recipe, "пропущено") {
		t.Error("рецепт цели не выносит вердикт по числу пропущенных — пропуск печатает " +
			"`ok` и неотличим от прохода")
	}
}

// TestAuthzFGAProofsListCoversTheTree — третье звено: перечень против дерева.
func TestAuthzFGAProofsListCoversTheTree(t *testing.T) {
	root := repoRoot(t)
	census, scanned := packagesProbingRealFGA(t, root)
	declared := authzFGADeclaredPkgs(t, root)
	t.Logf("осмотрено файлов тестов: %d; пакетов с пробами настоящего OpenFGA: %d; "+
		"объявлено в AUTHZ_FGA_PKGS: %d", scanned, len(census), len(declared))

	if scanned == 0 || len(census) == 0 {
		t.Fatalf("обход пуст (осмотрено %d, носителей %d) — гейт ничего не прочитал, "+
			"а значит ничего и не доказал", scanned, len(census))
	}
	if len(declared) == 0 {
		t.Fatal("AUTHZ_FGA_PKGS в корневом Makefile не прочитан или пуст — гейт сверяет " +
			"дерево с пустотой и на этом зеленеет")
	}
	for _, f := range judgeAuthzFGACoverage(census, declared) {
		t.Errorf("%s", f)
	}
}

// TestAuthzFGAOwnStepDeclarationsPointAtTheList — шов с соседним гейтом.
//
// shortgatedselection_test.go освобождает пакет от переписи долга, если тот
// назван исполняемым СВОИМ шагом конвейера, и проверяет это строкой в ci.yaml.
// Строка там одна на все шесть, поэтому сама по себе она не отвечает, дошёл ли
// КОНКРЕТНЫЙ пакет до прогона: перечень живёт в Makefile, а не в ci.yaml. Без
// этой сверки освобождение снова становится способом выйти из-под гейта одной
// строкой в списке — тем, от чего оба гейта и стоят.
func TestAuthzFGAOwnStepDeclarationsPointAtTheList(t *testing.T) {
	declared := map[string]bool{}
	for _, p := range authzFGADeclaredPkgs(t, repoRoot(t)) {
		declared[p] = true
	}
	checked := 0
	for pkg, inv := range shortGatedRunByOwnCIStep {
		if inv != authzFGAMakeTarget {
			continue
		}
		checked++
		if !declared[pkg] {
			t.Errorf("shortGatedRunByOwnCIStep освобождает %s ссылкой на %q, но в "+
				"AUTHZ_FGA_PKGS корневого Makefile этого пакета НЕТ — цель его не гоняет, "+
				"и освобождение выдаёт непрогон за прогон", pkg, authzFGAMakeTarget)
		}
	}
	if checked == 0 {
		t.Fatalf("ни одна запись shortGatedRunByOwnCIStep не ссылается на %q — либо шов "+
			"разошёлся, либо сверять нечего; и то и другое означает, что эта проверка "+
			"больше ничего не проверяет", authzFGAMakeTarget)
	}
	t.Logf("сверено освобождений, ссылающихся на цель: %d", checked)
}

// TestAuthzFGACoverageJudgeFiresAndStaysSilent — инъекция в обе стороны.
//
// Гейт выше на дереве зелёный, и зелёный сам по себе не значит ничего: ровно так
// же он выглядел бы, не умей вердикт краснеть. Поэтому решающая часть вынесена в
// чистую функцию и проверяется подставными входами — каждый случай, который она
// ОБЯЗАНА поймать, и каждый, который обязана пропустить.
func TestAuthzFGACoverageJudgeFiresAndStaysSilent(t *testing.T) {
	const outside = "services/iam/internal/authzmap"
	const inSelection = "services/iam/internal/repo/kacho/pg"

	t.Run("краснеет: носитель проб не объявлен", func(t *testing.T) {
		f := judgeAuthzFGACoverage([]string{outside}, nil)
		if len(f) != 1 || !strings.Contains(f[0], outside) {
			t.Fatalf("гейт не назвал пакет, который никто не гоняет: %v", f)
		}
	})

	t.Run("молчит: тот же носитель, объявленный в перечне", func(t *testing.T) {
		if f := judgeAuthzFGACoverage([]string{outside}, []string{outside}); len(f) != 0 {
			t.Fatalf("гейт краснеет на объявленном пакете: %v", f)
		}
	})

	t.Run("молчит: носитель ВНУТРИ отбора интеграционной джобы", func(t *testing.T) {
		if f := judgeAuthzFGACoverage([]string{inSelection}, nil); len(f) != 0 {
			t.Fatalf("гейт требует объявлять то, что интеграционная джоба и так гоняет: %v", f)
		}
	})

	t.Run("краснеет: объявление без предмета", func(t *testing.T) {
		f := judgeAuthzFGACoverage(nil, []string{"services/iam/internal/gone"})
		if len(f) != 1 || !strings.Contains(f[0], "gone") {
			t.Fatalf("объявление, которому нечего гонять, не найдено: %v", f)
		}
	})

	t.Run("краснеет: объявлен пакет, который и так в отборе", func(t *testing.T) {
		f := judgeAuthzFGACoverage([]string{inSelection}, []string{inSelection})
		if len(f) != 1 || !strings.Contains(f[0], inSelection) {
			t.Fatalf("двойной прогон не назван: %v", f)
		}
	})
}

// judgeAuthzFGACoverage — вердикт, отделённый от измерения. census — пакеты,
// чьи пробы спрашивают настоящий OpenFGA; declared — перечень цели.
//
// Отбор интеграционной джобы здесь тот же, что у соседнего гейта
// (integrationSelectionRe, копия сверяется с Makefile его же тестом): пакет,
// который она подбирает, объявлять не нужно, а объявленный — находка, потому
// что такое объявление описывает несуществующий предмет и молча удваивает
// прогон.
func judgeAuthzFGACoverage(census, declared []string) []string {
	left := map[string]bool{}
	for _, p := range declared {
		left[p] = true
	}
	var findings []string

	for _, pkg := range census {
		if integrationSelectionRe.MatchString(pkg) {
			if left[pkg] {
				findings = append(findings, "AUTHZ_FGA_PKGS называет "+pkg+", но этот пакет "+
					"ВХОДИТ в отбор интеграционной джобы — она его и так гоняет, а перечень "+
					"описывает долг, которого нет, и удваивает прогон")
			}
			delete(left, pkg)
			continue
		}
		if !left[pkg] {
			findings = append(findings, "пакет "+pkg+" спрашивает НАСТОЯЩИЙ OpenFGA, но не "+
				"назван в AUTHZ_FGA_PKGS корневого Makefile и не входит в отбор интеграционной "+
				"джобы. Значит его пробы не исполняются нигде: под кратким режимом они "+
				"пропускаются, а пропущенный пакет печатает `ok`. Внесите его в перечень цели "+
				"test-authz-fga — либо в отбор, если ему там место")
		}
		delete(left, pkg)
	}

	rest := make([]string, 0, len(left))
	for p := range left {
		rest = append(rest, p)
	}
	sort.Strings(rest) // порядок находок не должен зависеть от обхода карты
	for _, p := range rest {
		findings = append(findings, "AUTHZ_FGA_PKGS называет "+p+", но его пробы больше не "+
			"спрашивают настоящий OpenFGA (или пакет исчез) — гонять по этому имени нечего, "+
			"и запись достанется следующему как слепая зона")
	}
	return findings
}

// ─── измерение ───────────────────────────────────────────────────────────────

// packagesProbingRealFGA — repo-относительные пути пакетов, чьи ТЕСТЫ
// импортируют общий стенд настоящего OpenFGA, плюс сам стенд.
//
// Предикат — импорт из РАЗОБРАННОГО дерева (executablePart возвращает пути
// импортов отдельно от затёртых литералов), а не совпадение подстроки в тексте.
// Разница измерена, а не предположена: текстовый `grep` по тому же имени даёт
// лишний пакет — `internal/repohygiene`, где имя стенда стоит в комментарии
// соседнего гейта. Гейт, считающий комментарии, завышает перепись и требует
// гонять то, чего нет.
func packagesProbingRealFGA(t *testing.T, root string) (pkgs []string, scanned int) {
	t.Helper()
	need := `"github.com/PRO-Robotech/kacho/` + fgaHarnessPkg + `"`
	seen := map[string]bool{}
	walkGoFiles(t, root, func(rel string, raw []byte) {
		if !strings.HasSuffix(rel, "_test.go") {
			return
		}
		scanned++
		if strings.Contains(executablePart(rel, raw), need) {
			seen[filepath.ToSlash(filepath.Dir(rel))] = true
		}
	})

	// Стенд себя не импортирует, поэтому входит в перепись по построению. И это
	// объявление проверяется: пакета не станет — предикат обязан покраснеть, а не
	// молча уменьшить перепись на единицу.
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(fgaHarnessPkg))); err != nil {
		t.Fatalf("общего стенда %s в дереве нет (%v) — предпосылка этого гейта потеряла "+
			"предмет, и его перепись описывает не это дерево", fgaHarnessPkg, err)
	}
	seen[fgaHarnessPkg] = true

	for d := range seen {
		pkgs = append(pkgs, d)
	}
	sort.Strings(pkgs)
	return pkgs, scanned
}

// authzFGADeclaredPkgs читает перечень AUTHZ_FGA_PKGS из корневого Makefile.
// Пути там записаны как аргументы `go test` (`./services/...`) — приводятся к
// той же форме, что у обхода дерева, иначе сверялись бы разные вещи.
func authzFGADeclaredPkgs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, f := range strings.Fields(authzFGAAssignment(t, root)) {
		if p, ok := strings.CutPrefix(f, "./"); ok {
			out = append(out, strings.TrimSuffix(p, "/"))
		}
	}
	sort.Strings(out)
	return out
}

// authzFGAAssignment — правая часть присваивания AUTHZ_FGA_PKGS, склеенная по
// продолжениям строк.
func authzFGAAssignment(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	inside := false
	for _, line := range strings.Split(rootMakefile(t, root), "\n") {
		if !inside {
			rest, ok := strings.CutPrefix(line, "AUTHZ_FGA_PKGS")
			if !ok {
				continue
			}
			if _, after, found := strings.Cut(rest, "="); found {
				b.WriteString(after + " ")
			}
			inside = true
		} else {
			b.WriteString(line + " ")
		}
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), `\`) {
			break
		}
	}
	return strings.ReplaceAll(b.String(), `\`, " ")
}

// authzFGARecipe — рецепт цели test-authz-fga: строки от заголовка цели до
// первой строки, не начинающейся с табуляции.
func authzFGARecipe(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	inside := false
	for _, line := range strings.Split(rootMakefile(t, root), "\n") {
		if !inside {
			inside = strings.HasPrefix(line, "test-authz-fga:")
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		b.WriteString(line + "\n")
	}
	if b.Len() == 0 {
		t.Fatalf("в корневом Makefile нет цели test-authz-fga с рецептом — конвейер зовёт " +
			"её по имени, и без рецепта этот вызов ничего не исполняет")
	}
	return b.String()
}

func rootMakefile(t *testing.T, root string) string {
	t.Helper()
	// #nosec G304 -- читается корневой Makefile этого же репозитория.
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("не прочитан корневой Makefile: %v", err)
	}
	return string(raw)
}
