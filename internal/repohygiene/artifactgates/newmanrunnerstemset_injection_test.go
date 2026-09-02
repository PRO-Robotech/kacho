// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт выведенного набора коллекций СПОСОБЕН упасть —
// и что падает он на существе, а не на форме.
//
// Все пробы гоняют ТУ ЖЕ функцию, что и гейт по дереву (`auditRunnerStemSets`),
// а проба отбора наводится на изменённую КОПИЮ общего слоя и исполняет её тем
// же кодом (`runSelectorFrom`): проба, повторяющая логику гейта своей копией,
// доказывала бы свойство копии.
//
// У КАЖДОГО ОТРИЦАНИЯ ЕСТЬ ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ, и молчащая проба
// дополнительно утверждает ПЕРЕПИСЬ: гейт увидел прогонщика и промолчал по
// существу, а не потому, что смотрел мимо.
//
// ОСИ РАЗВЕДЕНЫ. «Перечень выписан» и «отбирает сам» проверяются порознь: одна
// инъекция, роняющая обе, оставила бы незамеченным распознаватель, знающий
// только одну форму.
package artifactgates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nmSharedStems — общий слой в форме, в какой он лежит в дереве: обе функции
// объявлены, поэтому предпосылка гейта выполняется и он судит по существу.
const nmSharedStems = `
newman_expected_stems() { :; }
newman_present_stems() { :; }
newman_all_stems() { :; }
`

// nmCleanRunner — прогонщик ПОСЛЕ сведения: отбор берётся из общего слоя, свой
// обход дерева снят. Законный близнец для обеих осей.
const nmCleanRunner = `#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
NEWMAN_DIR="$PWD"
. "$(_stems_lib)"   # tests/newman/kacholib/stems.sh
stems=()
while IFS= read -r s; do stems+=("$s"); done < <(newman_all_stems "$NEWMAN_DIR")
`

// nmSuiteStems — стемы коллекций набора, к которому относится прогонщик проб.
var nmSuiteStems = []string{"volume", "image", "snapshot"}

func nmRunnerAudit(t *testing.T, runner string) ([]string, runnerStemCensus) {
	t.Helper()
	const rel = "services/x/tests/newman/scripts/run.sh"
	return auditRunnerStemSets(nmSharedStems,
		map[string]string{rel: runner},
		map[string][]string{rel: nmSuiteStems})
}

// ─── красное на настоящем дефекте: ПЕРЕЧЕНЬ ВЫПИСАН ──────────────────────────

func TestRunnerStemSetInjectionWrittenListIsFound(t *testing.T) {
	// Возвращаем ровно тот дефект, ради которого гейт заведён: набор снова
	// объявляет перечень коллекций у себя.
	injected := strings.Replace(nmCleanRunner, `stems=()`,
		"COLLECTIONS=(volume image snapshot)\nstems=()", 1)
	findings, cen := nmRunnerAudit(t, injected)
	if len(findings) == 0 {
		t.Fatal("инъекция не поймана: перечень коллекций выписан массивом, а гейт молчит")
	}
	if !strings.Contains(strings.Join(findings, "\n"), "ВЫПИСАН массивом COLLECTIONS") {
		t.Fatalf("находка есть, но называет не тот предмет: %v", findings)
	}
	// Ось разведена: снятие ОТБОРА из общего слоя тут не при чём, и вторая
	// находка не должна приезжать заодно.
	if cen.takenFromShared != 1 {
		t.Fatalf("перепись: прогонщик по-прежнему берёт отбор из общего слоя, ожидалось 1, а не %d",
			cen.takenFromShared)
	}
	if cen.written != 1 || cen.writtenStems != 3 {
		t.Fatalf("перепись выписанного не сошлась: выписано %d перечней / %d имён", cen.written, cen.writtenStems)
	}
}

// ─── красное на настоящем дефекте: ОТБИРАЕТ САМ ─────────────────────────────

func TestRunnerStemSetInjectionOwnSelectionIsFound(t *testing.T) {
	injected := strings.Replace(nmCleanRunner,
		`. "$(_stems_lib)"   # tests/newman/kacholib/stems.sh`,
		"for f in collections/*.postman_collection.json; do echo \"$f\"; done", 1)
	findings, cen := nmRunnerAudit(t, injected)
	if len(findings) != 1 {
		t.Fatalf("ожидалась ровно одна находка про собственный отбор, получено: %v", findings)
	}
	if !strings.Contains(findings[0], "отбирает коллекции САМ") {
		t.Fatalf("находка называет не тот предмет: %v", findings)
	}
	if cen.ownSelection != 1 || cen.takenFromShared != 0 {
		t.Fatalf("перепись не сошлась: своих отборов %d, из общего слоя %d", cen.ownSelection, cen.takenFromShared)
	}
}

// ─── молчание на ЗАКОННОМ близнеце: порядок вызовов рукописный ──────────────

func TestRunnerStemSetLegitimateOrderedCallsAreSilent(t *testing.T) {
	// Форма iam: ПОРЯДОК вызовов назван поимённо осознанно (посев, зависимость
	// между коллекциями), а сам НАБОР выводится из общего слоя. Предмет гейта —
	// множество, а не порядок, и требовать здесь красного значило бы запрещать
	// решение, которое дерево приняло сознательно.
	twin := nmCleanRunner + `
run_one "volume"
run_one "image"
run_one "snapshot"
`
	findings, cen := nmRunnerAudit(t, twin)
	if len(findings) != 0 {
		t.Fatalf("законный близнец объявлен нарушением: %v", findings)
	}
	if cen.takenFromShared != 1 {
		t.Fatalf("перепись: близнец не осмотрен как берущий отбор из общего слоя (%d) — "+
			"тогда молчание означало бы слепоту, а не суждение", cen.takenFromShared)
	}
}

// ─── молчание на ЗАКОННОМ близнеце: прогонщик ничего не отбирает ────────────

func TestRunnerStemSetLegitimateWrapperIsSilent(t *testing.T) {
	// Форма vpc/compute `run-incremental.sh`: обёртка над пошаговым прогоном.
	// Она не отбирает коллекций вовсе, и требовать от неё общего отбора значило
	// бы требовать отбора там, где его нет.
	twin := `#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec node "${SCRIPT_DIR}/run-incremental.js" "$@"
`
	findings, cen := nmRunnerAudit(t, twin)
	if len(findings) != 0 {
		t.Fatalf("обёртка объявлена нарушением: %v", findings)
	}
	if cen.noSelection != 1 {
		t.Fatalf("перепись: обёртка не осмотрена как «не отбирает вовсе» (%d)", cen.noSelection)
	}
}

// ─── молчание на СВОЁМ ОБЪЯСНЕНИИ: распознаватель читает исполняемое ────────

func TestRunnerStemSetProseAboutTheBanIsNotTheBan(t *testing.T) {
	// Шапка прогонщика ОБЯЗАНА называть предмет запрета — иначе следующий снимет
	// его как непонятный. Распознаватель по подстроке краснел бы ровно на этом
	// объяснении, а заодно считал бы упоминание слоя в комментарии за то, что
	// отбор оттуда взят.
	prose := `#!/usr/bin/env bash
# Здесь стоял COLLECTIONS=(volume image snapshot) — второе место об одном
# предмете. Отбор переехал в tests/newman/kacholib/stems.sh.
set -euo pipefail
`
	findings, cen := nmRunnerAudit(t, prose)
	if len(findings) != 0 {
		t.Fatalf("гейт покраснел на собственном объяснении: %v", findings)
	}
	if cen.written != 0 {
		t.Fatalf("перепись: комментарий засчитан выписанным перечнем (%d)", cen.written)
	}
	if cen.takenFromShared != 0 || cen.noSelection != 1 {
		t.Fatalf("перепись: упоминание слоя в комментарии засчитано за взятие отбора оттуда "+
			"(из слоя %d, не отбирают %d)", cen.takenFromShared, cen.noSelection)
	}
}

// ─── молчание на ПОРОГЕ: одиночное присваивание перечнем не является ────────

func TestRunnerStemSetSingleElementAssignmentIsSilent(t *testing.T) {
	// `stems=("$SERVICE")` и `EXTRA=()` — разбор аргументов, а не перечень.
	// Гейт, считающий их перечнем, краснел бы на коде, к предмету отношения не
	// имеющем, — и его сняли бы первым.
	twin := nmCleanRunner + `
EXTRA=()
one=(volume)
`
	findings, cen := nmRunnerAudit(t, twin)
	if len(findings) != 0 {
		t.Fatalf("одиночное присваивание объявлено перечнем: %v", findings)
	}
	if cen.written != 0 {
		t.Fatalf("перепись: порог не сработал, выписанных перечней %d", cen.written)
	}
}

// ─── ОТБОР: узкое правило снова расходится с генератором ────────────────────

func TestSharedStemSelectorInjectionNarrowRuleIsFound(t *testing.T) {
	// Возвращаем правило, которое лежало в трёх копиях прогонщиков: пропускаются
	// только `__init__`/`__main__`. На наборе с `_helpers.py` оно объявляет
	// помощника ОЖИДАЕМОЙ коллекцией — суита доложила бы MISSING и покраснела бы
	// по причине, которой нет.
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, sharedStemsRel)) // #nosec G304 -- путь из индекса git этого модуля
	if err != nil {
		t.Fatalf("чтение общего слоя: %v", err)
	}
	narrow := strings.Replace(string(src),
		`    [[ "$stem" == _* ]] && continue`,
		"    case \"$stem\" in __init__|__main__) continue ;; esac", 1)
	if narrow == string(src) {
		t.Fatal("инъекция не наложилась: правило отбора записано иначе, чем ждёт проба — " +
			"чинить надо пробу, а не молча выходить успехом")
	}
	lib := filepath.Join(t.TempDir(), "stems.sh")
	if err := os.WriteFile(lib, []byte(narrow), 0o600); err != nil {
		t.Fatalf("запись изменённой копии слоя: %v", err)
	}

	// Набор с модулем-помощником в дереве — предмет инъекции.
	const suite = "services/registry/tests/newman"
	got := runSelectorFrom(t, lib, "newman_expected_stems", filepath.Join(root, suite))
	if !contains(got, "_helpers") {
		t.Fatalf("инъекция не поймана: узкое правило не объявило `_helpers` ожидаемой коллекцией, "+
			"получено %v — значит проба измеряет не то различие, ради которого заведена", got)
	}

	// ПАРНЫЙ ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: тот же набор, слой ИЗ ДЕРЕВА — помощника нет.
	clean := runSelectorFrom(t, filepath.Join(root, sharedStemsRel), "newman_expected_stems",
		filepath.Join(root, suite))
	if contains(clean, "_helpers") {
		t.Fatalf("слой из дерева объявил помощника коллекцией: %v", clean)
	}
	if len(clean) == 0 {
		t.Fatal("положительный контроль пуст: слой из дерева не вернул ни одной коллекции — " +
			"тогда молчание про `_helpers` ничего не доказывает")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// ─── ДИАГНОСТИКА ЕСТЬ ЧАСТЬ СВОЙСТВА: причина названа, а не симптом (#1780) ──
//
// Четыре пробы ниже гоняют ТУ ЖЕ судящую функцию, что и гейт по дереву
// (`classifySuiteStemDiff`), а левую сторону сравнения получают ИСПОЛНЕНИЕМ
// настоящего общего слоя (`runSelectorFrom`) над синтетическим набором на диске.
// Синтетика нужна ровно затем, чтобы «файла нет в индексе» можно было устроить
// НЕ трогая дерево, из которого проба запущена, — иначе она писала бы в свой же
// репозиторий (собственная находка гейтов).
//
// ОСИ РАЗВЕДЕНЫ И ПРОВЕРЯЮТСЯ ПОРОЗНЬ. Проба, роняющая обе причины разом,
// оставила бы незамеченным ровно тот дефект, ради которого #1780 заведена:
// классификатор, сваливающий всё в одну корзину, красным выглядит одинаково.

// nmSynthSuite — синтетический набор newman на диске: `<tmp>/cases/<имя>.py`.
// Возвращает абсолютный путь каталога набора.
func nmSynthSuite(t *testing.T, stems ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o750); err != nil {
		t.Fatalf("синтетический набор: %v", err)
	}
	for _, s := range stems {
		f := filepath.Join(dir, "cases", s+".py")
		if err := os.WriteFile(f, []byte("CASES = []\n"), 0o600); err != nil {
			t.Fatalf("синтетический модуль %s: %v", s, err)
		}
	}
	return dir
}

// nmIndexOf — предикат «имя в индексе git», собранный из явного перечня.
// Индекс здесь моделируется, а не спрашивается: предмет проб — РАЗЛИЧЕНИЕ
// причин, и оно обязано проверяться без поднятия репозитория.
func nmIndexOf(suite string, stems ...string) func(string) bool {
	in := map[string]bool{}
	for _, s := range stems {
		in[suite+"/cases/"+s+".py"] = true
	}
	return func(rel string) bool { return in[rel] }
}

// ─── причина ИНДЕКС: файл на диске есть, в индексе его нет ──────────────────

func TestSharedStemSelectorUntrackedNameIsNamedByTheIndex(t *testing.T) {
	// Ровно тот способ воспроизведения, что назван в #1780: модуль кейсов создан
	// и НЕ добавлен в индекс. Прежняя редакция называла это «расхождением
	// отбора» и печатала два списка имён — читатель шёл искать дефект в
	// правилах, которых причина не касается. Цена: полный локальный прогон.
	root := repoRoot(t)
	const suite = "services/x/tests/newman"
	dir := nmSynthSuite(t, "alpha", "beta", "zzz_untracked")

	got := runSelectorFrom(t, filepath.Join(root, sharedStemsRel), stemLaneExpected.fn, dir)
	if !contains(got, "zzz_untracked") {
		t.Fatalf("предпосылка пробы не выполняется: слой не вернул неотслеживаемого имени (%v) — "+
			"тогда различать было бы нечего", got)
	}
	want := []string{"alpha", "beta"} // правило генератора по ИНДЕКСУ: третьего файла там нет
	onDisk := func(rel string) bool {
		return strings.HasPrefix(rel, suite+"/cases/") && strings.HasSuffix(rel, ".py")
	}

	v := classifySuiteStemDiff(stemLaneExpected, suite, got, want,
		nmIndexOf(suite, "alpha", "beta"), onDisk)

	if len(v.index) != 1 {
		t.Fatalf("неотслеживаемое имя не отнесено к индексу: индекс %v, правила %v", v.index, v.rule)
	}
	if !strings.Contains(v.index[0], "ИНДЕКСЕ git ОТСУТСТВУЕТ") ||
		!strings.Contains(v.index[0], "zzz_untracked") {
		t.Fatalf("находка есть, но не называет ни индекс, ни имя: %q", v.index[0])
	}
	// ОСЬ РАЗВЕДЕНА: правила отбора тут ни при чём, и вторая находка не должна
	// приезжать заодно — иначе читателя снова пошлют искать не там.
	if len(v.rule) != 0 {
		t.Fatalf("неотслеживаемый файл объявлен расхождением ПРАВИЛ отбора: %v", v.rule)
	}
}

// ─── причина ИНДЕКС: имя в индексе есть, файла на диске нет ─────────────────

func TestSharedStemSelectorVanishedNameIsNamedByTheIndex(t *testing.T) {
	// Зеркало предыдущей: `git rm --cached` не делали, а файл из рабочего дерева
	// исчез. Сторона слоя (диск) о нём молчит, сторона генератора (индекс) — нет.
	root := repoRoot(t)
	const suite = "services/x/tests/newman"
	dir := nmSynthSuite(t, "alpha")

	got := runSelectorFrom(t, filepath.Join(root, sharedStemsRel), stemLaneExpected.fn, dir)
	want := []string{"alpha", "gone"}
	onDisk := func(rel string) bool { return rel == suite+"/cases/alpha.py" }

	v := classifySuiteStemDiff(stemLaneExpected, suite, got, want,
		nmIndexOf(suite, "alpha", "gone"), onDisk)

	if len(v.index) != 1 || len(v.rule) != 0 {
		t.Fatalf("исчезнувшее из рабочего дерева имя разнесено не по той причине: индекс %v, правила %v",
			v.index, v.rule)
	}
	if !strings.Contains(v.index[0], "числится в ИНДЕКСЕ git и на диске ОТСУТСТВУЕТ") {
		t.Fatalf("находка не называет причину: %q", v.index[0])
	}
}

// ─── причина ПРАВИЛА: обе стороны видят файл одинаково ──────────────────────

func TestSharedStemSelectorRuleDriftIsStillNamedByTheRule(t *testing.T) {
	// Свойство не снято вместе с симптомом: настоящее расхождение ПРАВИЛ
	// по-прежнему называется правилами. Инъекция — та же, что у соседней пробы:
	// узкое правило (`__init__`/`__main__`), при котором помощник становится
	// ожидаемой коллекцией. Файл при этом ЛЕЖИТ на диске И числится в индексе,
	// то есть полоса индекса обязана молчать.
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, sharedStemsRel)) // #nosec G304 -- путь из индекса git этого модуля
	if err != nil {
		t.Fatalf("чтение общего слоя: %v", err)
	}
	narrow := strings.Replace(string(src),
		`    [[ "$stem" == _* ]] && continue`,
		"    case \"$stem\" in __init__|__main__) continue ;; esac", 1)
	if narrow == string(src) {
		t.Fatal("инъекция не наложилась: правило отбора записано иначе, чем ждёт проба — " +
			"чинить надо пробу, а не молча выходить успехом")
	}
	lib := filepath.Join(t.TempDir(), "stems.sh")
	if err := os.WriteFile(lib, []byte(narrow), 0o600); err != nil {
		t.Fatalf("запись изменённой копии слоя: %v", err)
	}

	const suite = "services/x/tests/newman"
	dir := nmSynthSuite(t, "alpha", "_helpers")
	got := runSelectorFrom(t, lib, stemLaneExpected.fn, dir)
	if !contains(got, "_helpers") {
		t.Fatalf("инъекция не поймана: узкое правило не объявило `_helpers` ожидаемой коллекцией (%v)", got)
	}
	want := []string{"alpha"} // правило генератора: ведущее подчёркивание — помощник
	all := func(rel string) bool {
		return strings.HasPrefix(rel, suite+"/cases/") && strings.HasSuffix(rel, ".py")
	}

	v := classifySuiteStemDiff(stemLaneExpected, suite, got, want, all, all)

	if len(v.rule) != 1 {
		t.Fatalf("расхождение ПРАВИЛ не названо правилами: правила %v, индекс %v", v.rule, v.index)
	}
	if !strings.Contains(v.rule[0], stemLaneExpected.fn) ||
		!strings.Contains(v.rule[0], "расходятся имена: _helpers") {
		t.Fatalf("находка есть, но называет не тот предмет: %q", v.rule[0])
	}
	if len(v.index) != 0 {
		t.Fatalf("расхождение правил отнесено к индексу: %v", v.index)
	}
}

// ─── ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: стороны сходятся — обе полосы молчат ───────────

func TestSharedStemSelectorAgreeingSidesAreSilent(t *testing.T) {
	// Без этой пробы молчание классификатора на чистом дереве было бы неотличимо
	// от классификатора, который не смотрит вовсе: обе полосы отрицательные.
	root := repoRoot(t)
	const suite = "services/x/tests/newman"
	dir := nmSynthSuite(t, "alpha", "beta", "_helpers")

	got := runSelectorFrom(t, filepath.Join(root, sharedStemsRel), stemLaneExpected.fn, dir)
	if len(got) == 0 {
		t.Fatal("положительный контроль пуст: слой не вернул ни одного имени — " +
			"тогда молчание обеих полос ничего не доказывает")
	}
	if contains(got, "_helpers") {
		t.Fatalf("слой из дерева объявил помощника коллекцией: %v", got)
	}
	all := func(rel string) bool {
		return strings.HasPrefix(rel, suite+"/cases/") && strings.HasSuffix(rel, ".py")
	}

	v := classifySuiteStemDiff(stemLaneExpected, suite, got, []string{"alpha", "beta"}, all, all)

	if len(v.rule) != 0 || len(v.index) != 0 {
		t.Fatalf("сошедшиеся стороны объявлены расхождением: правила %v, индекс %v", v.rule, v.index)
	}
}
