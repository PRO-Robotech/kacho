// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// casecoverageblock_injection_test.go — ДОКАЗАТЕЛЬСТВО, что гейт блока состава
// способен упасть, и что он молчит на законных близнецах.
//
// # Как устроено доказательство
//
// Инъекция подаёт НАСТОЯЩИЙ вход той же функции, которую зовёт гейт
// (`ScanCaseCoverage`), а не повторяет её логику своей копией: копия осталась бы
// зелёной ровно тогда, когда гейт перестал бы работать.
//
// Одно-фактность: мир каждого отрицательного случая отличается от
// положительного близнеца РОВНО ОДНИМ фактом — снятой позицией, лишней
// позицией, перенесённой формой записи.
//
// # Законные близнецы взяты ИЗ ДЕРЕВА, а не выдуманы
//
// Каждый близнец воспроизводит форму, которая в дереве ЕСТЬ и является
// законной: модуль без блока (98 из 100), `id=` внутри комментария (два
// вхождения в `iam-rbac-subjects.py`), `id=` в прозе шапки, строка-продолжение
// описания внутри блока. Близнец, выдуманный из головы, доказывал бы
// чувствительность к правке, а не различение существа.
package repohygiene

import (
	"strings"
	"testing"
)

// coverageWorld — модуль кейсов с блоком состава на две позиции.
const coverageWorld = `# Copyright
"""Case-set для проверки.

Coverage:
  DOM-RES-OK-ALPHA    — первый кейс
  DOM-RES-NEG-BETA    — второй кейс
"""

CASES = []

CASES.append(case(
    id="DOM-RES-OK-ALPHA",
    why="первый",
))
CASES.append(case(
    id="DOM-RES-NEG-BETA",
    why="второй",
))
`

func scanCoverageOne(t *testing.T, src string) ([]string, CaseCoverageCensus) {
	t.Helper()
	return ScanCaseCoverage(map[string]string{"cases/probe.py": src})
}

func TestCaseCoverageGateCanFail(t *testing.T) {
	t.Parallel()
	// ── КОНТРОЛЬ: перечень сходится — находок нет.
	if f, c := scanCoverageOne(t, coverageWorld); len(f) != 0 {
		t.Errorf("на сошедшемся перечне гейт нашёл %d: %v — он краснеет на исправном", len(f), f)
	} else if c.Blocks != 1 || c.Listed != 2 || c.Declared != 2 {
		t.Errorf("перепись контроля неверна: блоков %d, позиций %d, объявлений %d "+
			"(ждали 1/2/2) — доказательство шло бы не о том мире", c.Blocks, c.Listed, c.Declared)
	}

	// ── (а) ОДИН ФАКТ: позиция снята из блока. Это ровно тот дефект дерева,
	// ради которого гейт заведён (#2207).
	t.Run("a_позиция_снята_из_блока", func(t *testing.T) {
		w := strings.Replace(coverageWorld,
			"  DOM-RES-NEG-BETA    — второй кейс\n", "", 1)
		f, c := scanCoverageOne(t, w)
		if len(f) != 1 {
			t.Fatalf("ждали ровно одну находку, получили %d: %v", len(f), f)
		}
		if !strings.Contains(f[0], "DOM-RES-NEG-BETA") {
			t.Errorf("находка не назвала предмет: %s", f[0])
		}
		if c.Listed != 1 || c.Declared != 2 {
			t.Errorf("перепись не показала расхождение: позиций %d, объявлений %d", c.Listed, c.Declared)
		}
	})

	// ── (б) ОДИН ФАКТ: в блоке позиция, которой модуль не объявляет. Обратная
	// сторона: запись, пережившая снятый кейс, — тоже находка.
	t.Run("b_позиция_пережила_свой_кейс", func(t *testing.T) {
		w := strings.Replace(coverageWorld,
			"  DOM-RES-NEG-BETA    — второй кейс\n",
			"  DOM-RES-NEG-BETA    — второй кейс\n  DOM-RES-NEG-GONE    — снятый кейс\n", 1)
		f, _ := scanCoverageOne(t, w)
		if len(f) != 1 || !strings.Contains(f[0], "DOM-RES-NEG-GONE") {
			t.Fatalf("лишняя позиция не названа находкой: %v", f)
		}
		if !strings.Contains(f[0], "пережила свой предмет") {
			t.Errorf("находка не назвала КЛАСС, а он другой, чем у (а): %s", f[0])
		}
	})

	// ── (в) БЛИЗНЕЦ: модуль БЕЗ блока. Так устроены 98 модулей из 100 —
	// требовать блок значило бы краснеть на дереве, а не на дефекте.
	t.Run("v_близнец_модуль_без_блока", func(t *testing.T) {
		w := `"""Case-set без блока состава: описание прозой."""

CASES = []
CASES.append(case(id="DOM-RES-OK-ALPHA"))
`
		f, c := scanCoverageOne(t, w)
		if len(f) != 0 {
			t.Errorf("модуль без блока объявлен находкой: %v", f)
		}
		if c.Blocks != 0 || c.Declared != 1 {
			t.Errorf("перепись: блоков %d (ждали 0), объявлений %d (ждали 1)", c.Blocks, c.Declared)
		}
	})

	// ── (г) БЛИЗНЕЦ ИЗ ДЕРЕВА: `id = "…"` внутри КОММЕНТАРИЯ. В дереве таких
	// два, оба в `iam-rbac-subjects.py`; наивный разбор счёл бы их объявлениями
	// и потребовал бы от блока называть чужой идентификатор.
	t.Run("g_близнец_id_в_комментарии", func(t *testing.T) {
		w := strings.Replace(coverageWorld, "CASES = []",
			"# Same role the suites use. id = \"rol\" + md5(\"view\")[:17].\nCASES = []", 1)
		f, c := scanCoverageOne(t, w)
		if len(f) != 0 {
			t.Errorf("идентификатор из комментария принят за объявление: %v", f)
		}
		if c.Declared != 2 {
			t.Errorf("объявлений %d, ждали 2 — комментарий попал в счёт", c.Declared)
		}
	})

	// ── (д) БЛИЗНЕЦ: `id=` в ПРОЗЕ шапки. Шапка — литерал, и объявлением её
	// содержимое не является; отделить одно от другого можно, только зная, где
	// литерал кончается.
	t.Run("d_близнец_id_в_прозе_шапки", func(t *testing.T) {
		w := strings.Replace(coverageWorld, "\"\"\"Case-set для проверки.",
			"\"\"\"Case-set для проверки. Идентификатор строится как\nid=\"dom\" + суффикс.", 1)
		f, c := scanCoverageOne(t, w)
		if len(f) != 0 {
			t.Errorf("идентификатор из прозы шапки принят за объявление: %v", f)
		}
		if c.Declared != 2 {
			t.Errorf("объявлений %d, ждали 2 — проза шапки попала в счёт", c.Declared)
		}
	})

	// ── (е) БЛИЗНЕЦ ИЗ ДЕРЕВА: описание ПЕРЕНЕСЕНО на следующую строку. Так
	// устроен блок `iam-internal-only-check.py`; разбор, требующий подряд идущих
	// строк-заголовков, обрывался бы на продолжении и недосчитывал позиции.
	t.Run("e_близнец_описание_перенесено", func(t *testing.T) {
		w := strings.Replace(coverageWorld,
			"  DOM-RES-NEG-BETA    — второй кейс\n",
			"  DOM-RES-NEG-BETA\n                      — второй кейс, описание перенесено\n", 1)
		f, c := scanCoverageOne(t, w)
		if len(f) != 0 {
			t.Errorf("перенос описания сломал разбор позиции: %v", f)
		}
		if c.Listed != 2 {
			t.Errorf("позиций прочитано %d, ждали 2 — строка-продолжение не распознана", c.Listed)
		}
	})

	// ── (ж) ПРЕДПОСЫЛКА: пустой обход — ОТКАЗ, а не молчание. Иначе «ноль
	// находок» стало бы свойством обхода.
	t.Run("zh_пустой_обход_отказывает", func(t *testing.T) {
		f, c := ScanCaseCoverage(map[string]string{})
		if len(f) != 1 || !strings.Contains(f[0], "обход") {
			t.Fatalf("пустой обход промолчал: %v", f)
		}
		if c.Modules != 0 {
			t.Errorf("перепись пустого обхода: модулей %d, ждали 0", c.Modules)
		}
	})

	// ── (з) ПРЕДПОСЫЛКА, вторая половина: модули есть, а объявлений не прочитано
	// ни одного — предикат объявлений ослеп.
	t.Run("z_предикат_объявлений_ослеп", func(t *testing.T) {
		f, _ := scanCoverageOne(t, "\"\"\"Пустой модуль.\"\"\"\n\nCASES = []\n")
		if len(f) != 1 || !strings.Contains(f[0], "ослеп") {
			t.Fatalf("слепой предикат объявлений промолчал: %v", f)
		}
	})
}
