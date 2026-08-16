// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «порядок суит задан деревом» СПОСОБЕН упасть —
// и что падает он на существе, а не на форме.
//
// Инъекция идёт в обе стороны: гейт, краснеющий на всяком конфиге, ничего не
// измеряет, а гейт, молчащий на всём, не измеряет тем более.
//
//	конфиг без `testSequencer`                 → краснеет, называя координату;
//	конфиг, где ключ есть ТОЛЬКО в комментарии → краснеет (иначе разбор читает текст);
//	ссылка на несуществующий файл              → краснеет;
//	законный близнец (ключ + существующий файл)→ молчит, И ПРИ ЭТОМ перепись растёт.
//
// Третий случай — не украшение: провязка «в никуда» выглядит исполненной ровно так
// же, как настоящая, и именно этим опасна.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditConsoleSuiteOrder`), что и обход дерева.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические конфиги. Каждый — настоящая форма из дерева, а не выдумка.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ 1 — исходное состояние всех девяти конфигов консоли: порядок не объявлен.
const synthJestConfigNoSequencer = `module.exports = {
  preset: "ts-jest",
  testEnvironment: "jsdom",
  roots: ["<rootDir>/src", "<rootDir>/../shared/src"],
  moduleNameMapper: {
    "^@shared/(.*)$": "<rootDir>/../shared/src/$1",
  },
};
`

// ДЕФЕКТ 2 — самый тихий: ключ снят, а объяснение про него осталось. Гейт,
// ищущий подстроку в сыром файле, нашёл бы слово в комментарии и промолчал бы
// при снятой провязке — то есть удостоверил бы ровно ту дыру, ради которой написан.
const synthJestConfigSequencerOnlyInComment = `module.exports = {
  // Порядок суит закрепляется ключом testSequencer — см. shared/jest-sequencer-by-path.cjs.
  // Здесь он временно снят: разбираемся с медленной суитой.
  preset: "ts-jest",
  testEnvironment: "jsdom",
};
`

// ДЕФЕКТ 3 — провязка в никуда: ключ есть, файла нет. Со стороны неотличимо от
// исполненной провязки, поэтому проверять надо разрешимость ссылки, а не её наличие.
const synthJestConfigDanglingSequencer = `module.exports = {
  testSequencer: require.resolve("../shared/jest-sequencer-by-path.cjs"),
  preset: "ts-jest",
};
`

// ДЕФЕКТ 4 — значение не литерал, а переменная, и СЛЕДОМ идут чужие литералы
// (`moduleNameMapper` несёт их десятками). Без границы «одна строка объявления»
// разбор взял бы первый попавшийся литерал файла и объявил бы секвенсором чужой
// путь — то есть промолчал бы, прочитав не то. Ждём честное «значение — не путь».
const synthJestConfigSequencerFromVariable = `const SEQ = require("./seq.cjs");

module.exports = {
  testSequencer: SEQ,
  moduleNameMapper: {
    "^@shared/(.*)$": "<rootDir>/../shared/src/$1",
    "^@/(.*)$": "<rootDir>/src/$1",
  },
};
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — каноничная форма дерева: `require.resolve` на общий файл.
const synthJestConfigGoodResolve = `module.exports = {
  // Порядок суит — свойство дерева, а не кэша машины: см. шапку
  // ../shared/jest-sequencer-by-path.cjs. Ключ testSequencer принимает ПУТЬ.
  testSequencer: require.resolve("../shared/jest-sequencer-by-path.cjs"),
  preset: "ts-jest",
};
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — та же провязка через `<rootDir>`: форма другая, свойство то же.
// Без него гейт закреплял бы СПОСОБ ЗАПИСИ, а не порядок суит.
const synthJestConfigGoodRootDir = `module.exports = {
  testSequencer: "<rootDir>/../shared/jest-sequencer-by-path.cjs",
  preset: "ts-jest",
};
`

// existsAllBut — резолвер для инъекции: разрешает всё, кроме названных конфигов.
func existsAllBut(missing ...string) func(string, string) bool {
	set := map[string]bool{}
	for _, m := range missing {
		set[m] = true
	}
	return func(configRel, _ string) bool { return !set[configRel] }
}

func TestConsoleSuiteOrderGateFailsOnUnpinnedOrder(t *testing.T) {
	findings, good := auditConsoleSuiteOrder(map[string]string{
		"ui-future/x/jest.config.cjs": synthJestConfigNoSequencer,
		"ui-future/y/jest.config.cjs": synthJestConfigSequencerOnlyInComment,
		"ui-future/z/jest.config.cjs": synthJestConfigDanglingSequencer,
		"ui-future/w/jest.config.cjs": synthJestConfigSequencerFromVariable,
	}, existsAllBut("ui-future/z/jest.config.cjs"))

	if len(findings) != 4 {
		t.Fatalf("гейт нашёл %d находок вместо 4 — он не краснеет на дефекте, ради которого написан; "+
			"годных засчитано %d", len(findings), good)
	}
	if good != 0 {
		t.Errorf("годными засчитано %d конфигов вместо 0 — перепись разошлась с подложенным", good)
	}

	var files []string
	for _, f := range findings {
		files = append(files, f.Config)
		if f.Why == "" {
			t.Errorf("находка %s не называет причину — по такому сообщению чинить нечего", f.Config)
		}
	}
	if files[0] != "ui-future/w/jest.config.cjs" ||
		files[1] != "ui-future/x/jest.config.cjs" ||
		files[2] != "ui-future/y/jest.config.cjs" ||
		files[3] != "ui-future/z/jest.config.cjs" {
		t.Errorf("координаты находок не те и/или не упорядочены: %v", files)
	}

	// Значение-переменная обязано быть названо «не путь». Если бы разбор взял
	// следующий литерал файла, он объявил бы секвенсором `<rootDir>/../shared/src/$1`
	// из `moduleNameMapper` и промолчал бы, прочитав не то.
	if !strings.Contains(findings[0].Why, "не путь") {
		t.Errorf("значение-переменная объяснено как %q — разбор вышел за строку объявления", findings[0].Why)
	}
	// Комментарий-ловушка обязан быть распознан именно как «ключа нет», а не как
	// «значение не путь»: иначе разбор читает текст и сообщение уводит не туда.
	if !strings.Contains(findings[2].Why, "исполняемой части") {
		t.Errorf("конфиг, где ключ только в комментарии, объяснён как %q — "+
			"значит разбор принял комментарий за код", findings[2].Why)
	}
	// Провязка в никуда обязана называть НЕРАЗРЕШИМУЮ ссылку.
	if !strings.Contains(findings[3].Why, "jest-sequencer-by-path.cjs") {
		t.Errorf("висячая ссылка объяснена как %q — координата не названа", findings[3].Why)
	}
}

func TestConsoleSuiteOrderGateStaysSilentOnPinnedOrder(t *testing.T) {
	findings, good := auditConsoleSuiteOrder(map[string]string{
		"ui-future/x/jest.config.cjs": synthJestConfigGoodResolve,
		"ui-future/y/jest.config.cjs": synthJestConfigGoodRootDir,
	}, existsAllBut())

	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на законной форме — %v. Первый же ложный срабат его отключит, "+
			"и тогда он не поймает ни настоящей находки", findings)
	}

	// Молчание обязано быть молчанием ПРОЧИТАВШЕГО: со сломанным разбором гейт
	// выглядел бы точно так же.
	if good != 2 {
		t.Errorf("годными засчитаны %d конфига вместо 2 — молчание выше означает «не прочитал», "+
			"а не «чисто»", good)
	}
}
