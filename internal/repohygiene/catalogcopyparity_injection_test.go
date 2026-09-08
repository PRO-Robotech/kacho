// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogcopyparity_injection_test.go — доказательство, что оба утверждения шва
// СПОСОБНЫ упасть и СПОСОБНЫ смолчать.
//
// Осей три, и они проверяются порознь, потому что предметы у них разные:
//
//   - ПОВЕДЕНЧЕСКАЯ — отказ цели при отсутствующем операнде. Проверяется
//     ЗАПУСКОМ настоящей цели с подставленными операндами: подделки цели здесь
//     нет вовсе, подставлены только файлы, которые она сверяет.
//   - СТРУКТУРНАЯ, ОДНОСТРОЧНАЯ — находка распознавателя обёрнутой сверки в
//     рецепте make, где обёртка помещается в одну логическую строку.
//   - СТРУКТУРНАЯ, БЛОЧНАЯ — та же находка в скрипте шелла, где `if`, сверка и
//     `fi` стоят на РАЗНЫХ строках (#2086). Ось отдельная не для симметрии:
//     построчный распознаватель на этой форме молчит by construction, и без
//     собственной инъекции расширение охвата было бы неотличимо от холостого.
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
    "fqn": "kaname.cloud.iam.v1.UserService/Get",
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

// ─── ОСЬ 3: БЛОЧНАЯ ФОРМА В СКРИПТЕ ШЕЛЛА ───────────────────────────────────

// blockGuardedScript — НАХОДКА: та же обёртка, что в guardedMakefile, но
// записанная блоком. Построчный распознаватель здесь не видит ни одной из трёх
// строк: `if … then` не несёт сверки, сверка не несёт условия, `fi` не несёт
// ничего.
const blockGuardedScript = `#!/usr/bin/env bash
IAM="../services/iam/embedded/catalog.json"
TARGET="internal/embed/catalog.json"
diff -u "$TARGET" "$TARGET.built" || { echo "STALE"; exit 1; }
if [ -f "$IAM" ]; then
    diff -u "$IAM" "$TARGET" || { echo "drifted"; exit 1; }
fi
`

// refusingScript — ЗАКОННЫЙ БЛИЗНЕЦ: проверка файла на месте, сверка та же, но
// условие не открывает ветку, а закрывает исполнение. Различает их ОПЕРАТОР.
const refusingScript = `#!/usr/bin/env bash
IAM="../services/iam/embedded/catalog.json"
TARGET="internal/embed/catalog.json"
test -f "$IAM" || { echo "нет копии iam: $IAM" >&2; exit 1; }
diff -u "$IAM" "$TARGET" || { echo "drifted"; exit 1; }
`

// elseNamesTheAbsentPathScript — ЗАКОННЫЙ БЛИЗНЕЦ, снятый с дерева дословно
// (gateway/scripts/check-domain-generation.sh, A4): ветка открыта проверкой
// файла, но путь «файла нет» НАЗВАН — и назван отказом. Предмет гейта — ТИХИЙ
// пропуск; названный путь тихим не бывает.
//
// Близнец не декоративный: без него первое же расширение охвата покраснело бы на
// живом дереве, то есть на коде, который никто не ломал.
const elseNamesTheAbsentPathScript = `#!/usr/bin/env bash
if [[ -s "$WORK/ext_catalog.json" && -s "$WORK/str_catalog.json" ]]; then
  cmp -s "$WORK/ext_catalog.json" "$WORK/str_catalog.json" \
    || finding A4 "каталог домена от двух прогонов различается побайтово"
else
  finding A4 "каталога домена нет ни у одного из двух прогонов"
fi
`

// dataNotCommandsScript — ЗАКОННЫЙ БЛИЗНЕЦ: та же обёртка ДОСЛОВНО, но как
// ДАННЫЕ — в теле heredoc, во встроенной программе awk, в строке echo и в
// комментарии. Разбор, читающий сырой текст, покраснел бы здесь четырежды.
//
// Последняя строка — положительный контроль фикстуры: она несёт НАСТОЯЩУЮ
// сверку, поэтому «сверок ноль» не может означать «файл погашен целиком».
const dataNotCommandsScript = `#!/usr/bin/env bash
cat <<'EOF'
if [ -f X ]; then diff -u X Y; fi
EOF
awk '
  BEGIN { print "if [ -f X ]; then diff -u X Y; fi" }
' </dev/null
echo "if [ -f X ]; then diff -u X Y; fi"
# if [ -f X ]; then diff -u X Y; fi
cmp -s a b || exit 1
`

// nonFileConditionScript — ЗАКОННЫЙ БЛИЗНЕЦ: ветка открыта, но условие не про
// существование файла. Гейт судит ОДНУ ось и вменять условность вообще не вправе.
const nonFileConditionScript = `#!/usr/bin/env bash
if [ -n "$MODE" ]; then
    diff -u a b || exit 1
fi
`

// comparisonInElseBranchScript — ЗАКОННЫЙ БЛИЗНЕЦ: сверка стоит в ветке `else`
// файлового условия, то есть исполняется, когда файла НЕТ. Это другой класс, и
// рамка её не присваивает.
const comparisonInElseBranchScript = `#!/usr/bin/env bash
if [ -f "$IAM" ]; then
    echo "копия на месте"
else
    diff -u a b || exit 1
fi
`

// nestedGuardScript — НАХОДКА: сверка под вложенным НЕфайловым условием, но
// внутри файлового. Внутреннее `if` по своему условию не отменяет того, что
// снаружи стоит проверка файла.
const nestedGuardScript = `#!/usr/bin/env bash
if [ -f "$IAM" ]; then
    if [ -n "$MODE" ]; then
        cmp -s "$IAM" "$TARGET" || exit 1
    fi
fi
`

// andChainAcrossLinesScript — НАХОДКА: `&&`-цепочка, разорванная переводом
// строки. Оператор требует следующей команды, поэтому обе половины принадлежат
// ОДНОЙ логической строке — и без склейки условие и сверка попали бы в разные
// единицы разбора.
const andChainAcrossLinesScript = `#!/usr/bin/env bash
test -f "$IAM" &&
  diff -u "$IAM" "$TARGET"
`

// conditionPositionGuardedScript — НАХОДКА: сверка стоит УСЛОВИЕМ внутреннего
// `if`, а снаружи — проверка файла. Форма проверяет, что расширенная командная
// позиция (`if`/`elif`/`while`/`until`/`!`) расширяет ОХВАТ гейта, а не только
// его счётчик.
const conditionPositionGuardedScript = `#!/usr/bin/env bash
if [ -f "$IAM" ]; then
    if cmp -s "$IAM" "$TARGET"; then
        echo "равны"
    fi
fi
`

// shellFindingsOn — находки распознавателя на синтетическом скрипте плюс число
// осмотренных сверок: второе есть положительный контроль — молчание при нуле
// сверок означало бы слепоту, а не чистоту входа.
func shellFindingsOn(t *testing.T, body string) (found []guardedComparison, comparisons int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.sh")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("синтетический скрипт не записан: %v", err)
	}
	ok, err := isShellScript(path)
	if err != nil {
		t.Fatalf("вид синтетического файла не установлен: %v", err)
	}
	if !ok {
		t.Fatalf("синтетический файл не опознан как скрипт шелла — отбор не создаёт "+
			"условия, и вердикт по нему беспредметен:\n%s", body)
	}
	src, err := parseShellSource(path)
	if err != nil {
		t.Fatalf("синтетический скрипт не разобран: %v", err)
	}
	if len(src.Lines) == 0 {
		t.Fatalf("в синтетическом скрипте не прочитано ни одной логической строки — "+
			"фикстура не создаёт условия:\n%s", body)
	}
	if src.UnclosedQuote {
		t.Fatalf("разбор синтетического скрипта кончил файл ВНУТРИ литерала — с места "+
			"сбоя он гасил всё подряд, и вердикт по такой фикстуре беспредметен:\n%s", body)
	}
	return findGuardedComparisonsInShell("probe.sh", src)
}

// TestBlockGuardedComparisonRecognizer_Injection — блочная форма: распознаватель
// обязан находить обёртку, называть КООРДИНАТУ и молчать на каждом законном
// близнеце.
func TestBlockGuardedComparisonRecognizer_Injection(t *testing.T) {
	// ── НАХОДКИ ─────────────────────────────────────────────────────────────
	for _, tc := range []struct {
		name string
		body string
		// wantLine — строка, которую находка обязана назвать. Координата есть
		// часть свойства: находка, называющая только факт, посылает читателя
		// искать самому.
		wantLine int
		// wantComparisons — сколько сверок обязано быть ОСМОТРЕНО: «нашёл одну»
		// не должно означать «увидел одну».
		wantComparisons int
	}{
		{"блочная обёртка if … then … fi", blockGuardedScript, 6, 2},
		{"вложенное нефайловое условие внутри файлового", nestedGuardScript, 4, 1},
		{"цепочка && через перевод строки", andChainAcrossLinesScript, 2, 1},
		{"сверка условием внутреннего if", conditionPositionGuardedScript, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, comparisons := shellFindingsOn(t, tc.body)
			if len(found) != 1 {
				t.Fatalf("распознано находок %d, ожидалась 1 (сверок осмотрено %d) — "+
					"гейт не видит того класса, ради которого написан", len(found), comparisons)
			}
			if found[0].Line != tc.wantLine {
				t.Fatalf("находка названа строкой %d, ожидалась %d: %q",
					found[0].Line, tc.wantLine, found[0].Text)
			}
			if comparisons != tc.wantComparisons {
				t.Fatalf("осмотрено сверок %d, ожидалось %d — распознаватель команды "+
					"видит не всё", comparisons, tc.wantComparisons)
			}
		})
	}

	// ── ЗАКОННЫЕ БЛИЗНЕЦЫ: гейт обязан МОЛЧАТЬ ──────────────────────────────
	for _, tc := range []struct {
		name            string
		body            string
		wantComparisons int
	}{
		{"явный отказ вместо обёртки", refusingScript, 1},
		{"ветка «файла нет» названа через else", elseNamesTheAbsentPathScript, 1},
		{"условие не про существование файла", nonFileConditionScript, 1},
		{"сверка в ветке else файлового условия", comparisonInElseBranchScript, 1},
		{"данные: heredoc, awk, echo, комментарий", dataNotCommandsScript, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, n := shellFindingsOn(t, tc.body)
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

// TestComparisonCommandPositionsAreAllKnown_Injection — распознаватель сверки
// обязан знать ВСЕ законные командные позиции, а не ту, в которой её впервые
// заметили.
//
// Форма, о которой распознаватель не знает, — не редкость и не край: она столь
// же законна, а всё записанное в ней оказывается ВНЕ НАБЛЮДЕНИЯ, то есть не даёт
// ни красного, ни зелёного. Две из перечисленных ниже форм живут в дереве
// (`if cmp` и `! diff`), и до расширения командной позиции обе были невидимы.
func TestComparisonCommandPositionsAreAllKnown_Injection(t *testing.T) {
	const shebang = "#!/usr/bin/env bash\n"
	for _, tc := range []struct {
		name string
		body string
	}{
		{"начало строки", shebang + "diff -u a b\n"},
		{"после ;", shebang + "true; diff -u a b\n"},
		{"после &&", shebang + "true && diff -u a b\n"},
		{"после |", shebang + "true | diff -u a -\n"},
		{"в подстановке $( )", shebang + "out=\"$(diff -u a b || true)\"\n"},
		{"условием if", shebang + "if cmp -s a b; then :; fi\n"},
		{"условием elif", shebang + "if false; then :\nelif cmp -s a b; then :\nfi\n"},
		{"условием while", shebang + "while cmp -s a b; do break; done\n"},
		{"условием until", shebang + "until cmp -s a b; do break; done\n"},
		{"под отрицанием !", shebang + "! diff -q a b\n"},
		{"после then", shebang + "if true; then diff -u a b; fi\n"},
		{"после else", shebang + "if false; then :; else diff -u a b; fi\n"},
		{"после do", shebang + "for f in a; do diff -u \"$f\" b; done\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found, comparisons := shellFindingsOn(t, tc.body)
			if comparisons == 0 {
				t.Fatalf("сверка в этой командной позиции НЕ ОСМОТРЕНА — всё записанное "+
					"так остаётся вне наблюдения: ни красного, ни зелёного:\n%s", tc.body)
			}
			if len(found) != 0 {
				t.Fatalf("безусловная сверка объявлена обёрнутой (%d) — гейт краснеет на "+
					"исправном коде: %+v", len(found), found)
			}
		})
	}

	// ЗЕРКАЛО: слово, командой НЕ являющееся, осмотру не подлежит. Без него
	// «все позиции видны» было бы верно и у распознавателя, ловящего подстроку.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"часть имени файла", shebang + "cat mydiff.txt\n"},
		{"слово в строке echo", shebang + "echo \"then diff -u a b\"\n"},
		// Рядом с комментарием стоит НАСТОЯЩАЯ команда: файл из одних
		// комментариев не даёт ни одной логической строки, и «сверок ноль»
		// означало бы «прочитано ноль», а не «команды нет».
		{"слово в комментарии", shebang + "# ; diff -u a b\ntrue\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, comparisons := shellFindingsOn(t, tc.body)
			if comparisons != 0 {
				t.Fatalf("осмотрено сверок %d там, где команды нет вовсе — распознаватель "+
					"читает текст, а не код:\n%s", comparisons, tc.body)
			}
		})
	}
}
