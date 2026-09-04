// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogcopyparity_injection_test.go — доказательство, что оба утверждения шва
// СПОСОБНЫ упасть и СПОСОБНЫ смолчать.
//
// Осей две, и они проверяются порознь, потому что предметы у них разные:
//
//   - ПОВЕДЕНЧЕСКАЯ — отказ цели при отсутствующем операнде. Проверяется
//     ЗАПУСКОМ настоящей цели с подставленными операндами: подделки цели здесь
//     нет вовсе, подставлены только файлы, которые она сверяет.
//   - СТРУКТУРНАЯ — находка распознавателя обёрнутой сверки. Проверяется на
//     синтетических Makefile.
//
// # Законный близнец у КАЖДОЙ оси
//
// Одностороннее доказательство зеленело бы на цели, отказывающей ВСЕГДА, и на
// гейте, краснеющем на любом рецепте. Поэтому рядом с каждой находкой стоит вход
// той же формы, на котором обязано быть молчание: две сошедшиеся копии для цели
// и форма явного отказа для гейта.
//
// # Инъекция роняет ТОЛЬКО проверяемое
//
// Ни одна фикстура ниже не нарушает чужого контроля: синтетические Makefile
// живут в t.TempDir() и разбираются напрямую, минуя обход дерева, а
// поведенческая ось подменяет ровно операнды сверки и ничего больше. Красное,
// пришедшее от соседа, доказывало бы способность соседа, а не этих двух.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── ОСЬ 1: ПОВЕДЕНИЕ ЦЕЛИ ───────────────────────────────────────────────────

// twoCopies — два файла с заданным содержимым во временном каталоге.
func twoCopies(t *testing.T, iam, edge string) (iamPath, edgePath string) {
	t.Helper()
	dir := t.TempDir()
	iamPath = filepath.Join(dir, "iam_permission_catalog.json")
	edgePath = filepath.Join(dir, "edge_permission_catalog.json")
	if err := os.WriteFile(iamPath, []byte(iam), 0o600); err != nil {
		t.Fatalf("копия iam не записана: %v", err)
	}
	if err := os.WriteFile(edgePath, []byte(edge), 0o600); err != nil {
		t.Fatalf("копия края не записана: %v", err)
	}
	return iamPath, edgePath
}

// oneEntryCatalog — минимально законный каталог: одна запись, поэтому перепись
// цели непуста и «копии равны» не может означать «сверять было нечего».
const oneEntryCatalog = `[
  {
    "fqn": "kacho.cloud.iam.v1.UserService/Get",
    "permission": "iam.users.get",
    "required_relation": "viewer"
  }
]
`

// TestCatalogCopyParityTarget_Injection — цель обязана отказывать на
// отсутствующем операнде и на расхождении, и обязана молчать на сошедшихся
// копиях.
func TestCatalogCopyParityTarget_Injection(t *testing.T) {
	root := repoRoot(t)

	// ── КОНТРОЛЬ: два одинаковых законных операнда — цель обязана МОЛЧАТЬ ────
	//
	// Стоит первым и формальностью не является: без него всякое отрицание ниже
	// объяснялось бы целью, которая отказывает при любом входе.
	iam, edge := twoCopies(t, oneEntryCatalog, oneEntryCatalog)
	out, code := runCatalogParity(t, root, "IAM_CATALOG="+iam, "PERMISSION_CATALOG_TARGET="+edge)
	if code != 0 {
		t.Fatalf("КОНТРОЛЬ: на двух сошедшихся копиях цель отказала (код %d) — она "+
			"краснеет на исправном входе, и ни одна находка ниже ничего не доказывает:\n%s",
			code, out)
	}
	if !strings.Contains(out, "записей 1") {
		t.Fatalf("КОНТРОЛЬ: цель прошла, но не назвала объём осмотренного — «копии равны» "+
			"неотличимо от «сверять было нечего»:\n%s", out)
	}

	// ── НАХОДКА 1: копии iam НЕТ ────────────────────────────────────────────
	//
	// Это предмет полосы: прежде обёртка `if [ -f … ]` делала этот вход тихим
	// успехом, то есть сверка не исполнялась ни разу и цель была зелена.
	missing := filepath.Join(t.TempDir(), "уехала_вместе_с_выносом_iam.json")
	out, code = runCatalogParity(t, root, "IAM_CATALOG="+missing, "PERMISSION_CATALOG_TARGET="+edge)
	if code == 0 {
		t.Fatalf("отсутствующая копия iam прошла БЕЗ отказа (код 0) — сверка пропускается "+
			"молча, ровно как под снятой обёрткой `if [ -f … ]`:\n%s", out)
	}
	if !strings.Contains(out, missing) {
		t.Fatalf("цель отказала, но НЕ НАЗВАЛА недостающий путь %s — отказ, не называющий "+
			"операнд, посылает читателя искать не там:\n%s", missing, out)
	}

	// ── НАХОДКА 2: копии КРАЯ нет — та же ось с другой стороны ──────────────
	//
	// Без неё «отказ при отсутствии» было бы утверждением про один операнд из
	// двух, а второй остался бы вне наблюдения.
	out, code = runCatalogParity(t, root, "IAM_CATALOG="+iam, "PERMISSION_CATALOG_TARGET="+missing)
	if code == 0 {
		t.Fatalf("отсутствующая копия края прошла БЕЗ отказа (код 0):\n%s", out)
	}
	if !strings.Contains(out, missing) {
		t.Fatalf("цель отказала, но не назвала недостающий путь края %s:\n%s", missing, out)
	}

	// ── НАХОДКА 3: копии расходятся на ОДИН БАЙТ ───────────────────────────
	//
	// Предмет самой сверки. Отказ на отсутствии операнда ничего не говорит о
	// том, сверяет ли цель содержимое: без этой оси она могла бы лишь проверять
	// наличие двух файлов.
	drifted := strings.Replace(oneEntryCatalog, `"viewer"`, `"editor"`, 1)
	if drifted == oneEntryCatalog {
		t.Fatal("фикстура расхождения не отличается от исходной — ось не проверена")
	}
	iamDrift, edgeDrift := twoCopies(t, drifted, oneEntryCatalog)
	out, code = runCatalogParity(t, root, "IAM_CATALOG="+iamDrift, "PERMISSION_CATALOG_TARGET="+edgeDrift)
	if code == 0 {
		t.Fatalf("разошедшиеся копии прошли БЕЗ отказа (код 0) — цель проверяет наличие "+
			"файлов, а не их содержимое:\n%s", out)
	}
	if !strings.Contains(out, "drifted") {
		t.Fatalf("цель отказала на расхождении, но не назвала его предметом — текст "+
			"отказа не отличим от отказа по отсутствию операнда:\n%s", out)
	}

	// ── НАХОДКА 4: копии равны и ПУСТЫ ──────────────────────────────────────
	//
	// «Ноль находок» обязано быть отличимо от «ноль прочитанного» и у самой
	// цели: два пустых каталога побайтово равны, и без этой ветки цель
	// отчиталась бы зелёным, не сверив ни одной записи.
	iamEmpty, edgeEmpty := twoCopies(t, "[]\n", "[]\n")
	out, code = runCatalogParity(t, root, "IAM_CATALOG="+iamEmpty, "PERMISSION_CATALOG_TARGET="+edgeEmpty)
	if code == 0 {
		t.Fatalf("две ПУСТЫЕ равные копии прошли зелёным (код 0) — «копии сошлись» "+
			"неотличимо от «сверять было нечего»:\n%s", out)
	}
}

// ─── ОСЬ 2: РАСПОЗНАВАТЕЛЬ ОБЁРНУТОЙ СВЕРКИ ─────────────────────────────────

// guardedMakefile — находка: сверка исполняется, только если файл существует.
// Дословная форма, снятая с gateway/Makefile до правки, включая продолжение
// строки: обёртка в этом дереве написана именно так.
const guardedMakefile = `TARGET := internal/embed/catalog.json
IAM := ../services/iam/embedded/catalog.json
.PHONY: check
check:
	@diff -u $(TARGET) $(BUILD) || { echo "STALE"; exit 1; }
	@if [ -f $(IAM) ]; then diff -u $(IAM) $(TARGET) \
	  || { echo "drifted"; exit 1; }; fi
`

// refusingMakefile — ЗАКОННЫЙ БЛИЗНЕЦ той же формы: проверка файла на месте,
// сверка та же, но условие не открывает ветку, а закрывает исполнение.
// Различает их ОПЕРАТОР, а не наличие проверки файла, — и гейт обязан молчать.
const refusingMakefile = `TARGET := internal/embed/catalog.json
IAM := ../services/iam/embedded/catalog.json
.PHONY: check
check:
	@test -f $(IAM) || { echo "нет копии iam: $(IAM)"; exit 1; }
	@diff -u $(IAM) $(TARGET) \
	  || { echo "drifted"; exit 1; }
`

// regeneratingMakefile — ВТОРОЙ законный близнец: сверка безусловна, а её исход
// информационен (`|| true`), потому что цель регенерирующая. Гейт обязан
// молчать: `|| true` — другая ось, и её адъюдикация требует знать, сверяет цель
// или порождает, то есть суждения, а не предиката.
const regeneratingMakefile = `TARGET := internal/embed/catalog.json
BUILD := build/catalog.json
.PHONY: regen
regen:
	./scripts/gen.sh $(BUILD)
	@echo "--- diff $(TARGET) vs regenerated ---"
	@diff -u $(TARGET) $(BUILD) || true
`

// prosaMakefile — ТРЕТИЙ законный близнец: слова «if», «-f» и «diff» стоят в
// комментарии и в тексте echo, командой не являясь. Гейт, читающий сырой текст,
// покраснел бы здесь на собственном объяснении.
const prosaMakefile = `# check — сверка копий. Здесь стояло ` + "`if [ -f $(IAM) ]; then diff …; fi`" + `,
# и это была обёртка: пока файла нет, diff не исполняется.
.PHONY: check
check:
	@echo "--- diff $(IAM) vs $(TARGET) ---"
	@echo "если бы стояло if [ -f ] ; then diff, сверка была бы условной"
`

// guardedFindingsOn — находки распознавателя на синтетическом Makefile плюс
// число осмотренных сверок: второе есть положительный контроль — молчание при
// нуле сверок означало бы слепоту, а не чистоту.
func guardedFindingsOn(t *testing.T, body string) (found []guardedComparison, comparisons int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Makefile")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("синтетический Makefile не записан: %v", err)
	}
	recipes, err := parseMakefileRecipes(path)
	if err != nil {
		t.Fatalf("рецепты синтетического Makefile не разобраны: %v", err)
	}
	if len(recipes.Lines) == 0 {
		t.Fatalf("в синтетическом Makefile не прочитано ни строки рецепта — фикстура "+
			"не создаёт условия, и вердикт по ней беспредметен:\n%s", body)
	}
	return findGuardedComparisons("Makefile", recipes)
}

// TestGuardedComparisonRecognizer_Injection — распознаватель обязан находить
// обёртку и молчать на каждой из трёх законных форм.
func TestGuardedComparisonRecognizer_Injection(t *testing.T) {
	// ── НАХОДКА: обёртка условием существования ─────────────────────────────
	found, comparisons := guardedFindingsOn(t, guardedMakefile)
	if len(found) != 1 {
		t.Fatalf("на обёрнутой сверке распознано находок %d, ожидалась 1 (сверок "+
			"осмотрено %d) — гейт не видит того класса, ради которого написан",
			len(found), comparisons)
	}
	// Координата — часть свойства, а не украшение: находка, называющая только
	// факт, посылает читателя искать самому.
	if found[0].Line != 6 || found[0].Target != "check" {
		t.Fatalf("находка названа неверной координатой: строка %d, цель %q — ожидались "+
			"6 и \"check\"", found[0].Line, found[0].Target)
	}
	// Положительный контроль ВНУТРИ находки: в этом же рецепте есть ВТОРАЯ,
	// безусловная сверка, и она обязана быть осмотрена и не попасть в находки.
	// Иначе «нашёл одну» могло бы означать «увидел одну».
	if comparisons != 2 {
		t.Fatalf("в обёрнутом рецепте осмотрено сверок %d, ожидалось 2 (безусловная "+
			"STALE-сверка и обёрнутая) — распознаватель команды видит не всё", comparisons)
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: гейт обязан МОЛЧАТЬ ──────────────────────────────
	for _, tc := range []struct {
		name string
		body string
		// wantComparisons — сколько сверок обязано быть ОСМОТРЕНО. Молчание при
		// нуле осмотренных доказывало бы слепоту, а не чистоту фикстуры.
		wantComparisons int
	}{
		{"явный отказ вместо обёртки", refusingMakefile, 1},
		{"регенерирующая цель, исход информационен", regeneratingMakefile, 1},
		{"проза и echo: слова есть, команды нет", prosaMakefile, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, n := guardedFindingsOn(t, tc.body)
			if len(got) != 0 {
				t.Fatalf("на законной форме гейт нашёл %d — он ловит форму, а не существо, "+
					"и первый же ложный срабат его отключит: %+v", len(got), got)
			}
			if n != tc.wantComparisons {
				t.Fatalf("осмотрено сверок %d, ожидалось %d — молчание гейта тогда означает "+
					"слепоту распознавателя, а не чистоту входа", n, tc.wantComparisons)
			}
		})
	}
}
