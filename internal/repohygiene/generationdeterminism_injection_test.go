// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"strings"
	"testing"
)

// Способность гейта упасть доказывается ИНЪЕКЦИЕЙ в обе стороны на настоящем
// производителе входа — синтетическом генераторе, исполняемом тем же
// интерпретатором и по тому же контракту, что генераторы дерева.
//
// Почему инъекция не в дерево: после починки в дереве нет ни одного экземпляра
// класса, и «гейт молчит» перестало бы что-либо доказывать — молчать он стал бы
// и будучи сломанным. Синтетический генератор даёт обе стороны одновременно и
// переживает починку дерева.
//
// Почему близнец — тот же генератор, а не другой: законная форма отличается от
// дефектной ровно одной строкой (сбросом счётчика при загрузке набора). Близнец,
// отличающийся чем-то ещё, доказывал бы, что гейт различает генераторы, а не
// что он различает СВОЙСТВО.

// syntheticGenerator — минимальный генератор контракта `gen.py [модуль]`:
// читает `cases/*.py`, пишет `collections/<модуль>.postman_collection.json`,
// нумерует шаги счётчиком уровня модуля. resetPerModule решает, сбрасывается ли
// счётчик при загрузке очередного набора, — это и есть предмет.
func syntheticGenerator(resetPerModule bool) string {
	reset := "    # счётчик НЕ сброшен — имя шага становится функцией окружения\n"
	if resetPerModule {
		reset = "    _SEQ[0] = 0\n"
	}
	return `import json, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CASES_DIR = ROOT / "cases"
OUT_DIR = ROOT / "collections"

_SEQ = [0]


def wrap(name):
    _SEQ[0] += 1
    return "%s-rya%d" % (name, _SEQ[0])


def load_cases_module(path):
` + reset + `    names = [l.strip() for l in path.read_text().splitlines() if l.strip()]
    return [wrap(n) for n in names]


def main(argv):
    want = set(argv[1:])
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    for f in sorted(CASES_DIR.glob("*.py")):
        if want and f.stem not in want:
            continue
        steps = load_cases_module(f)
        out = OUT_DIR / (f.stem + ".postman_collection.json")
        out.write_text(json.dumps({"item": steps}, indent=2) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
`
}

// writeSyntheticSuite раскладывает набор по контракту дерева: `scripts/gen.py`
// плюс `cases/*.py`. Наборов два и более — иначе «сколько модулей загрузилось
// раньше» не имеет смысла и дефект не проявился бы даже будучи внесённым.
func writeSyntheticSuite(t *testing.T, resetPerModule bool) string {
	t.Helper()
	dir := t.TempDir()
	// mustWrite заводит родительские каталоги сам (sharedsigningliteral_test.go).
	mustWrite(t, filepath.Join(dir, "scripts", "gen.py"), syntheticGenerator(resetPerModule))
	mustWrite(t, filepath.Join(dir, "cases", "alpha.py"), "create-lb\nget-lb\n")
	mustWrite(t, filepath.Join(dir, "cases", "beta.py"), "create-listener\nget-listener\n")
	return dir
}

// TestGenerationDeterminismGateRedsOnTheInjectedDefect — дефект внесён: гейт
// обязан покраснеть И НАЗВАТЬ КООРДИНАТУ. Отказ без координаты диагнозом не
// является: имена шагов читают именно ради диагноза.
func TestGenerationDeterminismGateRedsOnTheInjectedDefect(t *testing.T) {
	t.Parallel()
	python := pythonInterpreter(t)

	suite := writeSyntheticSuite(t, false)
	v, err := compareGenerationModes(suite, t.TempDir(), python, syntheticFiles)
	if err != nil {
		t.Fatalf("осмотр синтетического набора: %v", err)
	}
	if v.Modules != 2 {
		t.Fatalf("модулей сверено %d, ожидалось 2 — фикстура не та, "+
			"и вердикт относится не к тому, что проверяется", v.Modules)
	}
	if len(v.Findings) == 0 {
		t.Fatal("гейт молчит на внесённом дефекте: одиночная генерация второго " +
			"набора обязана разойтись с полной — счётчик не сброшен")
	}
	joined := strings.Join(v.Findings, "\n")
	if !strings.Contains(joined, filepath.Join(suite, generatorRelPath)) {
		t.Fatalf("отказ не называет координату генератора:\n%s", joined)
	}
	if !strings.Contains(joined, "beta") {
		t.Fatalf("отказ не называет разошедшийся модуль:\n%s", joined)
	}
	if !strings.Contains(joined, "rya") {
		t.Fatalf("отказ не показывает расхождение, только факт неравенства:\n%s", joined)
	}
}

// TestGenerationDeterminismGateStaysSilentOnTheLawfulTwin — законный близнец
// той же формы: тот же счётчик, тот же способ именования, разница ровно в
// сбросе. Гейт обязан молчать, иначе он ловит форму (счётчик в имени), а не
// существо (зависимость от окружения), и первый же ложный срабат его отключит.
func TestGenerationDeterminismGateStaysSilentOnTheLawfulTwin(t *testing.T) {
	t.Parallel()
	python := pythonInterpreter(t)

	suite := writeSyntheticSuite(t, true)
	v, err := compareGenerationModes(suite, t.TempDir(), python, syntheticFiles)
	if err != nil {
		t.Fatalf("осмотр синтетического набора: %v", err)
	}
	if v.Modules != 2 {
		t.Fatalf("модулей сверено %d, ожидалось 2 — молчание относится не к тому, "+
			"что проверяется", v.Modules)
	}
	if v.BytesCompared == 0 {
		t.Fatal("сверено 0 байт: молчание означает «ничего не прочитано», " +
			"а не «расхождений нет»")
	}
	if len(v.Findings) != 0 {
		t.Fatalf("гейт краснеет на законной форме — он ловит счётчик в имени, "+
			"а не зависимость имени от окружения:\n%s", strings.Join(v.Findings, "\n"))
	}
}
