// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// jscallname_test.go — замок на класс «гейт сверяет имя вызова ПОДСТРОКОЙ».
//
// # Предмет
//
// `strings.Contains(code, имя+"(")` не отличает вызов от ХВОСТА чужого
// идентификатора: `rescan(` кончается на `scan(`, `goDeclaredForm(` — на `rm(`.
// Замер по 442 файлам охвата консоли: для одного имени `rm` — 26 ложных
// совпадений в 7 различных идентификаторах.
//
// # Почему замок, а не только правка
//
// Направление ошибки у вызывающих РАЗНОЕ, и опасное из двух себя не выдаёт.
//
//   - `walkingHelperExports` и `reachesTreeWalk` спрашивают «имя ВЫЗВАНО», и
//     ответ `да` ПРОЩАЕТ пробу. Лишнее совпадение гасит находку: гейт зелен,
//     проба-пустышка на месте, заметить нечем — ложное МОЛЧАНИЕ;
//   - `auditModuleMockFactories` спрашивает «зовётся ли динамический импорт», и
//     ответ `да` создаёт находку. Лишнее совпадение даёт ложную НАХОДКУ — она
//     себя выдаёт сразу, кто-то упирается в красное и идёт разбираться.
//
// Проба гоняет ТЕ ЖЕ функции, что и обход дерева, на синтетическом входе:
// вердикт о способности падать не должен зависеть от того, что сегодня лежит в
// дереве. Живых хвостов там на день правки ноль — по 308 парам замыкания
// помощников, 22 парам «проба × импортированный обходящий экспорт» и 244
// аргументам фабрик подмены, — поэтому предмет правки не в сегодняшнем числе, а
// в направлении ошибки.
package repohygiene

import "testing"

// helperWithTailNeighbour — помощник, где `outer` НЕ доходит до обхода: в его
// теле стоит `rescan(`, чужое имя, кончающееся на имя обходящей `scan`.
const helperWithTailNeighbour = `
export function scan() { return readdirSync("."); }
export function outer() { return rescan(); }
function rescan() { return 1; }
`

// helperWithRealCall — законный близнец: `outerOk` зовёт `scan` по-настоящему.
const helperWithRealCall = `
export function scan() { return readdirSync("."); }
export function outerOk() { return scan(); }
`

func TestWalkingHelperClosureJudgesWholeNamesNotTails(t *testing.T) {
	tail, _, _ := walkingHelperExports(map[string]string{"h.ts": helperWithTailNeighbour})

	// Положительный контроль: без него отрицание ниже зеленело бы на пустом
	// разборе — «ничего не распознано» неотличимо от «распознано верно».
	if !tail["scan"] {
		t.Fatalf("прямой обход не распознан: разбор помощников сломан, и тогда " +
			"вердикт ниже ничего не стоит")
	}
	if tail["outer"] {
		t.Errorf("`outer` объявлена доходящей до обхода, хотя в её теле стоит `rescan(` — " +
			"ЧУЖОЕ имя, кончающееся на `scan(`. Подстрочное совпадение расширяет перечень " +
			"обходящих экспортов, а он ПРОЩАЕТ пробы: находка гаснет молча")
	}

	real, _, _ := walkingHelperExports(map[string]string{"h.ts": helperWithRealCall})
	if !real["outerOk"] {
		t.Errorf("настоящий вызов `scan()` не засчитан замыканием — сужение вместо починки: " +
			"пробы, обходящие дерево через помощника, снова объявятся находками")
	}
}

func TestProbeForgivenessRequiresAWholeNameCall(t *testing.T) {
	helpers := map[string]string{"h.ts": helperWithRealCall}

	// Проба ЧИТАЕТ модуль консоли по своей же координате — то есть находка, если
	// её не простят за обход дерева. Имя обходящего экспорта импортировано, но
	// вызова нет: в тексте стоит только `rescan(`, хвост чужого имени.
	tailProbe := map[string]string{
		"ui-future/x/a.test.ts": `
import { scan } from "./h";
const src = readFileSync(join(here, "Comp.tsx"), "utf8");
function rescan() { return src; }
rescan();
`,
	}
	findings, reads, walks := auditUISourceReads(tailProbe, helpers)
	if reads != 1 {
		t.Fatalf("чтение с диска не распознано (reads=%d) — предмета у вердикта нет", reads)
	}
	if walks != 0 {
		t.Errorf("проба объявлена обходящей дерево по ХВОСТУ чужого имени (`rescan(` ⊃ `scan(`): "+
			"прощение выдано за вызов, которого нет, walks=%d", walks)
	}
	if len(findings) != 1 {
		t.Errorf("находка погашена: проба читает `Comp.tsx` как текст и обхода не делает, "+
			"но прощена совпадением подстроки. Находок %d, ожидалась 1", len(findings))
	}

	// Законный близнец той же формы: вызов настоящий — проба обязана быть прощена.
	realProbe := map[string]string{
		"ui-future/x/b.test.ts": `
import { scan } from "./h";
const src = readFileSync(join(here, "Comp.tsx"), "utf8");
scan();
`,
	}
	findings, _, walks = auditUISourceReads(realProbe, helpers)
	if walks != 1 {
		t.Errorf("настоящий вызов обходящего экспорта не засчитан (walks=%d) — предикат сузился "+
			"вместо того чтобы стать точнее: перепись состава дерева объявлялась бы находкой", walks)
	}
	if len(findings) != 0 {
		t.Errorf("законная проба-перепись объявлена находкой (%d): гейт ловит форму, а не существо, "+
			"и первый же ложный срабат его отключит", len(findings))
	}
}

func TestMockFactoryDynamicImportIsAWholeName(t *testing.T) {
	// Настоящий динамический импорт — находка.
	findings, calls, _ := auditModuleMockFactories(map[string]string{
		"ui-future/x/a.ts": "jest.unstable_mockModule(\"m\", () => import(\"./m\"));\n",
	})
	if calls != 1 {
		t.Fatalf("вызов подмены не распознан (calls=%d) — вердикт ниже был бы получен даром", calls)
	}
	if len(findings) != 1 {
		t.Errorf("динамический импорт в фабрике не найден (%d) — гейт потерял способность падать", len(findings))
	}

	// Законный близнец: имя, кончающееся на `import`, вызовом импорта не является.
	findings, calls, static := auditModuleMockFactories(map[string]string{
		"ui-future/x/b.ts": "jest.unstable_mockModule(\"m\", () => reimport(\"./m\"));\n",
	})
	if calls != 1 {
		t.Fatalf("вызов подмены не распознан у близнеца (calls=%d)", calls)
	}
	if len(findings) != 0 {
		t.Errorf("`reimport(` объявлен динамическим импортом (%d находок): подстрока меряет "+
			"хвост чужого имени, а не вызов", len(findings))
	}
	if static != 1 {
		t.Errorf("близнец не зачтён статическим (%d) — дискриминатору нечего отличать от находки", static)
	}
}
