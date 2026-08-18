// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что оба гейта объявленной команды консоли СПОСОБНЫ
// упасть — и что падают они на существе, а не на форме.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (гейт,
// краснеющий на всём, ничего не измеряет), и одного «молчит» тоже мало
// (молчание бывает от того, что читать не стали):
//
//	ГЕЙТ 1 — исполнимость команды
//	  вызов по шаблону без объявления → находка, называющая скрипт и шаблон;
//	  тот же вызов с --allow-empty-input → молчит, и перепись его ЗАСЧИТЫВАЕТ;
//	  он же с коротким --aei            → молчит (обе формы ключа законны);
//	  вызов НАЗВАННОГО файла            → молчит: отсутствие названного файла —
//	                                      настоящая находка, а не этот класс;
//	  слово stylelint как аргумент чужой команды → молчит (читается ГЛАГОЛ);
//	  манифест без stylelint вовсе      → молчит: пустой перечень есть цель.
//
//	ГЕЙТ 2 — исполняет ли конвейер объявленное
//	  конвейер не зовёт объявленную команду → узел НЕ достигнут;
//	  зовёт через сводный скрипт корня      → достигнут, вместе с потомками;
//	  зовёт, но пакета нет в сводном скрипте → НЕ достигнут (дрейф перечня);
//	  шаг вне каталога консоли              → узлов не даёт;
//	  имя пакета собрано подстановкой       → НЕРАЗРЕШИМ, а не «достигнут».
//
// Обе половины гоняют ТЕ ЖЕ функции, что и прогон по дереву: проба,
// повторяющая логику гейта своей копией, доказывала бы свойство копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 1. Каркас манифестов взят у ui-future/vpc/package.json, а не выдуман.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ — ровно тот, с которого заведена задача #602.
const uiSynthManifestBare = `{
  "name": "kacho-ui-future-vpc",
  "scripts": {
    "lint": "npm run lint:js && npm run lint:css && npm run typecheck",
    "lint:js": "eslint .",
    "lint:css": "stylelint \"src/**/*.{css,scss}\""
  }
}`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — канон: шаблон тот же, пустой набор объявлен не ошибкой.
const uiSynthManifestAllowed = `{
  "scripts": {
    "lint:css": "stylelint \"src/**/*.{css,scss}\" --allow-empty-input",
    "lint:css:fix": "stylelint \"src/**/*.{css,scss}\" --allow-empty-input --fix"
  }
}`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — короткая форма того же ключа. Гейт, знающий только
// длинную, выдал бы находку за верное объявление.
const uiSynthManifestShortFlag = `{
  "scripts": {"lint:css": "stylelint \"src/**/*.css\" --aei"}
}`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — НАЗВАННЫЙ файл, а не шаблон. Его отсутствие тоже роняет
// stylelint, но это находка другого класса: назван файл, которого нет, и
// молчать о нём было бы неверно. Здесь предмета нет — набор не функция дерева.
const uiSynthManifestNamedFile = `{
  "scripts": {"lint:css": "stylelint src/styles.css"}
}`

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — слово стоит АРГУМЕНТОМ чужой команды. Гейт, ищущий
// подстроку, покраснел бы на строке, которая stylelint не запускает вовсе.
const uiSynthManifestWordAsArgument = `{
  "scripts": {"note": "echo stylelint \"src/**/*.css\" запускается в другом месте"}
}`

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — пустой перечень: предмета не стало. Это ЦЕЛЬ, а не
// поломка, и держать вызов ради зелёного не требуется.
const uiSynthManifestNoStylelint = `{
  "scripts": {"lint": "npm run lint:js", "lint:js": "eslint ."}
}`

// ДЕФЕКТ в цепочке — вызов стоит второй командой. Гейт, читающий только первое
// слово скрипта, прошёл бы мимо.
const uiSynthManifestChained = `{
  "scripts": {"lint": "eslint . && stylelint \"src/**/*.css\""}
}`

func uiSynthManifest(t *testing.T, rel, body string) uiManifest {
	t.Helper()
	m, ok := uiParseManifest(rel, body)
	if !ok {
		t.Fatalf("%s: синтетический манифест не разобрался — проба измеряет не то", rel)
	}
	return m
}

func TestUiEmptyMatchGateCutsBothWays(t *testing.T) {
	for _, tc := range []struct {
		name                string
		rel, body           string
		wantInvocations     int
		wantPatterns        int
		wantFindings        int
		wantScriptInFinding string
	}{
		{"дефект: шаблон без объявления", "ui-future/vpc/package.json", uiSynthManifestBare, 1, 1, 1, "lint:css"},
		{"дефект в цепочке команд", "ui-future/vpc/package.json", uiSynthManifestChained, 1, 1, 1, "lint"},
		{"законно: --allow-empty-input", "ui-future/vpc/package.json", uiSynthManifestAllowed, 2, 2, 0, ""},
		{"законно: короткий --aei", "ui-future/vpc/package.json", uiSynthManifestShortFlag, 1, 1, 0, ""},
		{"законно: назван файл, не шаблон", "ui-future/vpc/package.json", uiSynthManifestNamedFile, 1, 0, 0, ""},
		{"законно: слово аргументом чужой команды", "ui-future/vpc/package.json", uiSynthManifestWordAsArgument, 0, 0, 0, ""},
		{"законно: предмета нет вовсе", "ui-future/vpc/package.json", uiSynthManifestNoStylelint, 0, 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv, pat, f := uiAuditEmptyMatch(uiSynthManifest(t, tc.rel, tc.body))
			if inv != tc.wantInvocations || pat != tc.wantPatterns || len(f) != tc.wantFindings {
				t.Fatalf("вызовов %d (ожидалось %d), по шаблону %d (ожидалось %d), находок %d (ожидалось %d)",
					inv, tc.wantInvocations, pat, tc.wantPatterns, len(f), tc.wantFindings)
			}
			if tc.wantFindings == 0 {
				return
			}
			// Находка обязана НАЗЫВАТЬ координату: без неё гейт сообщает, что
			// где-то плохо, и чинить приходится поиском.
			if f[0].Rel != tc.rel || f[0].Script != tc.wantScriptInFinding || f[0].Pattern == "" {
				t.Fatalf("находка не называет координату: %+v", f[0])
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ГЕЙТ 2. Разрешение вызова в узел и достижимость из конвейера.
// ─────────────────────────────────────────────────────────────────────────────

func TestUiRunTargetCutsBothWays(t *testing.T) {
	for _, tc := range []struct {
		from, cmd      string
		wantNode       uiScriptNode
		wantOK         bool
		wantUnresolved bool
	}{
		{"", "npm run typecheck --prefix vpc", uiScriptNode{"vpc", "typecheck"}, true, false},
		{"", "npm --prefix vpc run typecheck", uiScriptNode{"vpc", "typecheck"}, true, false},
		{"vpc", "npm run lint:css", uiScriptNode{"vpc", "lint:css"}, true, false},
		{"", "npm test --prefix host", uiScriptNode{"host", "test"}, true, false},
		{"", "do npm run lint --prefix nlb", uiScriptNode{"nlb", "lint"}, true, false},
		{"", "CI=1 npm run lint --prefix nlb", uiScriptNode{"nlb", "lint"}, true, false},
		// Подкоманды, которые скриптов не запускают: узла нет и неизвестности нет.
		{"", "npm ci", uiScriptNode{}, false, false},
		{"", "npm ci --prefix host", uiScriptNode{}, false, false},
		{"", "eslint .", uiScriptNode{}, false, false},
		{"", "node scripts/check-lint-coverage.mjs", uiScriptNode{}, false, false},
		// Имя собрано подстановкой: НЕ «достигнут» и НЕ «не достигнут» — неизвестно.
		{"", "npm test --prefix ${{ matrix.pkg }}", uiScriptNode{}, false, true},
		{"", "npm run lint --prefix \"$p\"", uiScriptNode{}, false, true},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			node, ok, unres := uiRunTarget(tc.from, tc.cmd)
			if ok != tc.wantOK || unres != tc.wantUnresolved || (ok && node != tc.wantNode) {
				t.Fatalf("uiRunTarget(%q, %q) = (%v, ok=%v, неразрешим=%v); ожидалось (%v, ok=%v, неразрешим=%v)",
					tc.from, tc.cmd, node, ok, unres, tc.wantNode, tc.wantOK, tc.wantUnresolved)
			}
		})
	}
}

// uiSynthTree — два пакета, объявляющих `lint`, и сводный скрипт корня.
func uiSynthTree(t *testing.T, rootScripts string) map[string]uiManifest {
	t.Helper()
	out := map[string]uiManifest{}
	add := func(rel, body string) {
		m := uiSynthManifest(t, rel, body)
		out[uiPkgOf(m.Pkg)] = m
	}
	add("ui-future/package.json", `{"scripts": `+rootScripts+`}`)
	for _, p := range []string{"vpc", "nlb"} {
		add("ui-future/"+p+"/package.json", `{"scripts": {
			"lint": "npm run lint:js && npm run lint:css",
			"lint:js": "eslint .",
			"lint:css": "stylelint \"src/**/*.css\" --allow-empty-input"
		}}`)
	}
	return out
}

// Конвейер, зовущий сводный скрипт корня. Каталог запуска объявлен файлом —
// ровно как в ui.yml.
const uiSynthWorkflowCallsDeclared = `
defaults:
  run:
    working-directory: ui-future
jobs:
  lint:
    steps:
      - uses: actions/checkout@v7
      - run: npm ci
      - run: npm run lint
`

// Конвейер со своей строкой линтера — объявленную команду он не зовёт. Это
// состояние дерева ДО правки #602.
const uiSynthWorkflowOwnString = `
defaults:
  run:
    working-directory: ui-future
jobs:
  lint:
    steps:
      - run: ../node_modules/.bin/eslint .
`

// Шаг вне каталога консоли: узлов давать не должен, иначе гейт судил бы чужой
// пакет по манифесту консоли.
const uiSynthWorkflowOutsideUI = `
defaults:
  run:
    working-directory: ui-future
jobs:
  other:
    steps:
      - run: npm run lint
        working-directory: tools/whatever
`

// Имя пакета собрано подстановкой матрицы.
const uiSynthWorkflowMatrix = `
defaults:
  run:
    working-directory: ui-future
jobs:
  test:
    steps:
      - run: npm test --prefix ${{ matrix.pkg }}
`

func TestUiPipelineReachGateCutsBothWays(t *testing.T) {
	const rootWithBoth = `{"lint": "npm run lint --prefix vpc && npm run lint --prefix nlb"}`
	const rootWithOne = `{"lint": "npm run lint --prefix vpc"}`

	t.Run("зовёт объявленную — достигнуты оба и их потомки", func(t *testing.T) {
		seeds, steps, unres := uiWorkflowSeeds(t, uiSynthWorkflowCallsDeclared)
		// Шагов С КОМАНДОЙ два: `uses: actions/checkout` команды не несёт и в
		// перепись не идёт — иначе число называло бы не то, что измерено.
		if steps != 2 || unres != 0 {
			t.Fatalf("шагов с командой %d (ожидалось 2), неразрешимых %d (ожидалось 0)", steps, unres)
		}
		reached, _ := uiReach(uiSynthTree(t, rootWithBoth), seeds)
		for _, want := range []uiScriptNode{
			{"vpc", "lint"}, {"vpc", "lint:js"}, {"vpc", "lint:css"},
			{"nlb", "lint"}, {"nlb", "lint:js"}, {"nlb", "lint:css"},
		} {
			if !reached[want] {
				t.Fatalf("узел %s не достигнут, хотя конвейер зовёт объявленную команду; достигнуто: %s",
					want, uiNodeList(uiSortNodes(reached)))
			}
		}
	})

	t.Run("дефект: конвейер зовёт СВОЮ строку — объявленная не исполняется", func(t *testing.T) {
		seeds, steps, _ := uiWorkflowSeeds(t, uiSynthWorkflowOwnString)
		if steps != 1 {
			t.Fatalf("шагов %d (ожидался 1)", steps)
		}
		reached, _ := uiReach(uiSynthTree(t, rootWithBoth), seeds)
		for _, no := range []uiScriptNode{{"vpc", "lint"}, {"nlb", "lint"}} {
			if reached[no] {
				t.Fatalf("узел %s объявлен достигнутым, хотя конвейер его не зовёт — гейт не упал бы "+
					"на состоянии дерева ДО правки", no)
			}
		}
	})

	t.Run("дефект: пакет выпал из сводного скрипта корня", func(t *testing.T) {
		seeds, _, _ := uiWorkflowSeeds(t, uiSynthWorkflowCallsDeclared)
		reached, _ := uiReach(uiSynthTree(t, rootWithOne), seeds)
		if !reached[uiScriptNode{"vpc", "lint"}] {
			t.Fatal("названный в сводном скрипте пакет обязан быть достигнут")
		}
		if reached[uiScriptNode{"nlb", "lint"}] {
			t.Fatal("пакет, выпавший из сводного скрипта, объявлен достигнутым — дрейф перечня " +
				"остался бы невидимым, а он и есть предмет гейта")
		}
	})

	t.Run("шаг вне каталога консоли узлов не даёт", func(t *testing.T) {
		seeds, steps, _ := uiWorkflowSeeds(t, uiSynthWorkflowOutsideUI)
		if steps != 1 {
			t.Fatalf("шагов %d (ожидался 1)", steps)
		}
		if len(seeds) != 0 {
			t.Fatalf("шаг вне %s дал узлы %v — гейт судил бы чужой пакет по манифесту консоли",
				uiRoot, seeds)
		}
	})

	t.Run("подстановка матрицы — неразрешима, а не достигнута", func(t *testing.T) {
		seeds, _, unres := uiWorkflowSeeds(t, uiSynthWorkflowMatrix)
		if len(seeds) != 0 || unres != 1 {
			t.Fatalf("узлов %d (ожидалось 0), неразрешимых %d (ожидался 1): имя, собранное "+
				"подстановкой, нельзя молча отнести ни к одной стороне", len(seeds), unres)
		}
	})
}

// TestUiShellSplitKeepsQuotedSeparators — разделитель ВНУТРИ кавычек командой не
// разделяет. Без этого шаблон `{css,scss}` или строка с `|` резались бы пополам,
// и гейт 1 перестал бы видеть собственный предмет.
func TestUiShellSplitKeepsQuotedSeparators(t *testing.T) {
	got := uiShellCommands(`stylelint "a|b;c" && eslint .`)
	if len(got) != 2 || !strings.Contains(got[0], "a|b;c") {
		t.Fatalf("разбор дал %q — разделитель внутри кавычек разорвал команду", got)
	}
	words := uiShellWords(`stylelint "src/**/*.{css,scss}" --fix`)
	if len(words) != 3 || words[1] != "src/**/*.{css,scss}" {
		t.Fatalf("слова %q — шаблон потерян вместе с кавычками, гейт перестал бы его видеть", words)
	}
}
