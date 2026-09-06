// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// kanamenameresidue_test.go — держатель условия «ноль чужого имени там, где
// продукт называет себя» (пункт 3 предиката готовности линии, эпик #2119).
//
// Разбор класса, оси, границы и устройство обеих ведомостей — в шапке
// kanamenameresidue.go. Здесь только обход дерева, перепись и вердикт.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// kanameSurfaceCorpus — отслеживаемые файлы поверхности Kaname, спрошенные У
// ИНДЕКСА git, а не собранные обходом диска.
//
// Довод не стилистический: под теми же каталогами лежат рабочие копии полос,
// распаковки чартов и отчёты прогонов, и обход диска посчитал бы чужие имена
// пачками — вердикт стал бы свойством рабочего каталога, а не коммита.
//
// Каталог поверхности, под которым индекс не даёт НИ ОДНОГО файла, — отказ:
// переезд каталога иначе завёл бы слепую зону молча.
func kanameSurfaceCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	root := repoRoot(t)
	corpus := map[string][]byte{}
	for _, dir := range KanameSurface {
		files, err := treecorpus.Under(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("состав поверхности %s: %v — «ноль находок» здесь означало бы "+
				"«ноль прочитанного»", dir, err)
		}
		for _, abs := range files {
			rel, relErr := filepath.Rel(root, abs)
			if relErr != nil {
				t.Fatalf("путь %s: %v", abs, relErr)
			}
			rel = filepath.ToSlash(rel)
			if _, seen := corpus[rel]; seen {
				continue // каталоги поверхности могут вкладываться друг в друга
			}
			body, readErr := os.ReadFile(abs) // #nosec G304 -- путь из индекса своего дерева
			if readErr != nil {
				t.Fatalf("чтение %s: %v", rel, readErr)
			}
			corpus[rel] = body
		}
	}
	return corpus
}

// TestKanameSurfaceNameResidueMatchesItsLedgers — имя платформы на поверхности,
// которой Kaname называет себя, сходится с обеими ведомостями.
//
// Ведомость остатка сегодня НЕ пуста, и это законный исход: переход ведут
// отдельные задачи линии, у каждой полосы назван владелец. Держатель заведён,
// чтобы остаток не рос молча и чтобы его снижение было НАЗВАНО тем же
// изменением, которое его снизило.
func TestKanameSurfaceNameResidueMatchesItsLedgers(t *testing.T) {
	findings, ledgerFindings, census, err := FindKanameNameResidue(
		kanameSurfaceCorpus(t), KanameNameResidueStay, KanameNameResidueDebt)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	logNameResidueCensus(t, census)

	outstanding, lanes := KanameNameResidueOutstanding()
	t.Logf("УСЛОВИЕ П3 НЕ ВЫПОЛНЕНО: ведомость остатка признаёт неснятыми %d вхождений "+
		"на %d полосах. Зелёный прогон этого держателя означает лишь, что остаток НЕ ВЫРОС; "+
		"условие выполнено тогда и только тогда, когда ведомость ПУСТА",
		outstanding, lanes)
	if outstanding == 0 && lanes == 0 {
		t.Logf("ведомость остатка ПУСТА — условие П3 выполнено; снимайте ведомость и " +
			"эту строку одним изменением")
	}

	// ─── предпосылки: держатель ОТКАЗЫВАЕТ на беспредметности, а не молчит ───
	if census.FilesRead == 0 {
		t.Fatal("прочитано ноль файлов — обход не состоялся, и молчание держателя ничего не значит")
	}
	for _, dir := range KanameSurface {
		if census.FilesBySurface[dir] == 0 {
			t.Fatalf("каталог поверхности %q не дал ни одного прочитанного файла — "+
				"он переехал либо назван неверно, и по нему «ноль находок» означает "+
				"«ноль прочитанного»", dir)
		}
	}
	for _, axis := range KanameAxes {
		if census.AxisOffered(axis) == 0 {
			t.Fatalf("ось %q НЕ ЧИТАЛАСЬ: до её правил не дошло ни одного вхождения. "+
				"«Найдено 0» по такой оси означает «не искали», а не «чисто» — "+
				"проверьте порядок правил в kanameResidueRules", axis)
		}
	}
	if census.OccurrencesASCII == 0 || census.OccurrencesMacron == 0 {
		t.Fatalf("распознаватель увидел только одну форму записи имени "+
			"(обычная %d, диакритическая %d) — вторая осталась вне наблюдения, "+
			"и по ней «ноль находок» неотличимо от «ноль прочитанного»",
			census.OccurrencesASCII, census.OccurrencesMacron)
	}

	for _, lf := range ledgerFindings {
		where := lf.Lane
		if lf.Path != "" {
			where = lf.Path + " [" + lf.Lane + "]"
		}
		t.Errorf("ведомость %q, %s: записано %d, в дереве %d — %s",
			lf.Ledger, where, lf.Want, lf.Got, lf.Why)
	}

	// Перечень находок печатается ТОЛЬКО когда ведомость разошлась: на сошедшейся
	// он был бы четырьмя тысячами строк известного остатка.
	if len(ledgerFindings) > 0 {
		t.Logf("остаток, названный поимённо (первые %d из %d):",
			nameResidueShown(len(findings)), len(findings))
		for i, f := range findings {
			if i >= 20 {
				break
			}
			t.Logf("  %s:%d [%s] %s", f.Path, f.Line, f.Lane, f.Text)
		}
	}
}

// logNameResidueCensus — перепись. По каждой оси ДВЕ величины, и «не читалась»
// печатается СЛОВАМИ: одно число скрывает ровно тот случай, ради которого
// держатель заведён.
func logNameResidueCensus(t *testing.T, c NameResidueCensus) {
	t.Helper()
	t.Logf("файлов в индексе поверхности %d; прочитано %d; двоичных %d; пустых %d",
		c.FilesTracked, c.FilesRead, c.FilesBinary, c.FilesEmpty)
	t.Logf("вхождений имени платформы %d: обычной формой %d, диакритической %d",
		c.Occurrences, c.OccurrencesASCII, c.OccurrencesMacron)
	t.Logf("файлов: только обычная форма %d; ОБЕ %d; ТОЛЬКО ДИАКРИТИЧЕСКАЯ %d — "+
		"последние невидимы ASCII-предикату ЦЕЛИКОМ, и это величина слепой зоны "+
		"односторонней переписи",
		c.FilesASCIIOnly, c.FilesBothForms, c.FilesMacronOnly)
	t.Logf("НЕ СУДЯТСЯ, и это границы, а не находки: имя, собираемое при рендере "+
		"(подстановка шаблона, форматная строка, склейка) — строк-кандидатов %d; "+
		"имя платформы в самом ПУТИ файла — %d файлов (предмет полос раскладки и "+
		"переезда контракта, здесь только сосчитано)",
		c.Assembled, c.FilesWithNameInPath)

	for _, axis := range KanameAxes {
		offered := c.AxisOffered(axis)
		if offered == 0 {
			t.Logf("ось %-22q: ОСЬ НЕ ЧИТАЛАСЬ — её правила не исполнялись ни разу; "+
				"ноль здесь означает «не искали»", axis)
			continue
		}
		t.Logf("ось %-22q: осмотрено %5d · найдено %5d", axis, offered, c.AxisOccurrences(axis))
		for _, lane := range lanesOfAxis(axis) {
			t.Logf("      полоса %-32q осмотрено %5d · найдено %5d · файлов %4d · прощено %3d",
				lane, c.OfferedByLane[lane], c.FoundByLane[lane],
				c.FilesByLane[lane], c.ForgivenByLane[lane])
		}
	}
	t.Logf("границы — вне шести осей, но с числом у каждой:")
	for _, lane := range lanesOfAxis(axisBorder) {
		t.Logf("      %-44q осмотрено %5d · признано %5d (%s)",
			lane, c.OfferedByLane[lane], c.FoundByLane[lane], kanameLanes[lane].Why)
	}
}

// lanesOfAxis — полосы оси в устойчивом порядке.
func lanesOfAxis(axis NameResidueAxis) []string {
	var out []string
	for lane, lg := range kanameLanes {
		if lg.Axis == axis {
			out = append(out, lane)
		}
	}
	sort.Strings(out)
	return out
}

// nameResidueShown — сколько находок печатать: перечень существует ради
// координаты, а не ради полноты, и четыре тысячи строк не читает никто.
func nameResidueShown(n int) int {
	if n > 20 {
		return 20
	}
	return n
}

// TestKanameNameResidueRecognizerReadsBothLatinFormsOfTheTree — распознаватель
// читает ОБЕ латинские формы имени НА ЭТОМ дереве, а не только в синтетике.
//
// Синтетический мир доказывает, что разбор способен упасть; он не доказывает,
// что вторая форма в ЭТОМ дереве вообще есть и что односторонний предикат её
// теряет. Здесь считается ровно эта величина — файлы, невидимые ASCII-предикату
// целиком, — и её ноль объявляется отказом, а не тихим успехом.
func TestKanameNameResidueRecognizerReadsBothLatinFormsOfTheTree(t *testing.T) {
	corpus := kanameSurfaceCorpus(t)
	_, _, census, err := FindKanameNameResidue(corpus, KanameNameResidueStay, KanameNameResidueDebt)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	// Второй счёт той же величины — в обход всей машинерии полос, ведомостей и
	// пофайлового учёта. Он НЕ независим от распознавателя (зовёт ту же
	// platformNameAt) и этого не утверждает: он проверяет БУХГАЛТЕРИЮ переписи.
	// Сам распознаватель держит утверждение ниже — «файлов только с
	// диакритической формой не ноль»: оно краснеет ровно тогда, когда вторую
	// форму из распознавателя убирают.
	asciiOnly, macronOnly, both := 0, 0, 0
	for _, body := range corpus {
		if len(body) == 0 || containsNUL(body) {
			continue
		}
		sawASCII, sawMacron := false, false
		rs := []rune(string(body))
		for i := range rs {
			n, macron := platformNameAt(rs, i)
			if n == 0 {
				continue
			}
			if macron {
				sawMacron = true
			} else {
				sawASCII = true
			}
		}
		switch {
		case sawASCII && sawMacron:
			both++
		case sawMacron:
			macronOnly++
		case sawASCII:
			asciiOnly++
		}
	}

	t.Logf("файлов поверхности: только обычная форма %d, обе %d, ТОЛЬКО диакритическая %d",
		asciiOnly, both, macronOnly)

	if macronOnly != census.FilesMacronOnly || both != census.FilesBothForms ||
		asciiOnly != census.FilesASCIIOnly {
		t.Fatalf("два счёта одной величины разошлись: перепись говорит "+
			"(%d/%d/%d), независимый счёт — (%d/%d/%d); пока они не сойдутся, "+
			"ни одному числу держателя верить нельзя",
			census.FilesASCIIOnly, census.FilesBothForms, census.FilesMacronOnly,
			asciiOnly, both, macronOnly)
	}
	if macronOnly == 0 {
		t.Fatal("файлов, несущих ТОЛЬКО диакритическую форму, ноль — предпосылка " +
			"держателя не подтверждена этим деревом; если форма и вправду ушла из " +
			"дерева, снимайте вторую форму вместе с этой пробой, а не поодиночке")
	}
}

// TestKanameNameResidueEncodedFormsAreAbsentFromTheSurface — граница
// «закодированная запись имени» названа с ЧИСЛОМ, а не оговоркой.
//
// Разбор судит текст как он записан: имя внутри base64, процентного кодирования
// и `\uXXXX` ему не видно by construction. Пока таких форм на поверхности нет,
// граница беспредметна; появится — её обязан научиться читать разбор, а не эта
// проба объяснить, почему он не стал.
func TestKanameNameResidueEncodedFormsAreAbsentFromTheSurface(t *testing.T) {
	corpus := kanameSurfaceCorpus(t)
	// base64 обычной формы имени в трёх выравниваниях плюс процентное и \u.
	encodings := map[string]string{
		"base64 (выравнивание 0)": "a2FjaG8",
		"base64 (выравнивание 1)": "thY2hv",
		"base64 (выравнивание 2)": "rYWNob",
		"процентное кодирование":  "kach%6F",
		"escape JSON": "kach\\u006f",
	}
	found := map[string][]string{}
	read := 0
	for path, body := range corpus {
		if len(body) == 0 || containsNUL(body) {
			continue
		}
		read++
		text := string(body)
		for name, needle := range encodings {
			if strings.Contains(text, needle) {
				found[name] = append(found[name], path)
			}
		}
	}
	t.Logf("прочитано файлов %d; форм закодированной записи проверено %d", read, len(encodings))
	if read == 0 {
		t.Fatal("прочитано ноль файлов — граница измеряется на пустом корпусе")
	}
	for name, paths := range found {
		sort.Strings(paths)
		t.Errorf("на поверхности появилась закодированная запись имени (%s) — %d файл(ов), "+
			"первый %s. Разбор судит текст как он записан и такой формы НЕ ВИДИТ: "+
			"научите распознаватель читать её тем же изменением, которым она заведена",
			name, len(paths), paths[0])
	}
	if len(found) == 0 {
		t.Logf("закодированных записей имени на поверхности НЕТ — граница беспредметна, " +
			"и это сказано числом, а не умолчанием")
	}
}

// TestKanameNameResidueDebtLedgerNamesAnOwnerForEveryLane — у каждой строки
// остатка назван владелец, а полоса с остатком без строки — находка.
//
// Остаток без владельца снимать некому: он переживает линию и становится
// свойством дерева, о котором никто не отвечает.
func TestKanameNameResidueDebtLedgerNamesAnOwnerForEveryLane(t *testing.T) {
	_, _, census, err := FindKanameNameResidue(
		kanameSurfaceCorpus(t), KanameNameResidueStay, KanameNameResidueDebt)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	declared := map[string]bool{}
	for _, row := range KanameNameResidueDebt {
		declared[row.Lane] = true
		if strings.TrimSpace(row.Owner) == "" {
			t.Errorf("полоса %q объявлена в ведомости остатка без владельца", row.Lane)
		}
	}
	missing := 0
	for lane, lg := range kanameLanes {
		if lg.Axis == axisBorder && lane != borderUnknownForm {
			continue
		}
		if census.FoundByLane[lane] > 0 && !declared[lane] {
			missing++
			t.Errorf("полоса %q несёт %d вхождений остатка, а строки в ведомости у неё нет",
				lane, census.FoundByLane[lane])
		}
	}
	t.Logf("строк ведомости остатка %d; полос с остатком без строки %d",
		len(KanameNameResidueDebt), missing)
}

// TestKanameNameResidueLedgerRowsAreReachable — каждая строка обеих ведомостей
// называет полосу, КОТОРУЮ распознаватель производит.
//
// Строка, называющая полосу, до которой ни одно правило не доходит, прощает
// вникуда и переживёт свой предмет молча.
func TestKanameNameResidueLedgerRowsAreReachable(t *testing.T) {
	produced := map[string]bool{}
	for _, rule := range kanameResidueRules {
		produced[rule.Lane] = true
	}
	// Ключ карты и собственное имя полосы обязаны совпадать: разойдясь, они
	// сделали бы находку указывающей на чужую полосу, а ведомость — прощающей
	// не то, что она называет.
	for key, lg := range kanameLanes {
		if lg.ID != key {
			t.Errorf("полоса записана под ключом %q, а зовёт себя %q — находка "+
				"назовёт чужую полосу", key, lg.ID)
		}
	}
	for lane := range kanameLanes {
		if !produced[lane] {
			t.Errorf("полоса %q объявлена, но ни одно правило её не производит — "+
				"ведомость по ней прощала бы вникуда", lane)
		}
	}
	for _, rule := range kanameResidueRules {
		if _, ok := kanameLanes[rule.Lane]; !ok {
			t.Errorf("правило производит полосу %q, которой нет в перечне осей", rule.Lane)
		}
	}
	seen := map[string]bool{}
	for _, row := range KanameNameResidueDebt {
		seen[row.Lane] = true
	}
	for _, e := range KanameNameResidueStay {
		if _, ok := kanameLanes[e.Lane]; !ok {
			t.Errorf("ведомость решённого остаться называет полосу %q, которой нет", e.Lane)
		}
	}
	t.Logf("правил %d; полос %d; строк остатка %d; записей решённого остаться %d",
		len(kanameResidueRules), len(kanameLanes), len(KanameNameResidueDebt),
		len(KanameNameResidueStay))
	if len(kanameLanes) == 0 || len(kanameResidueRules) == 0 {
		t.Fatal("полос либо правил ноль — сверка беспредметна")
	}
}
