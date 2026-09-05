// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «проба интерфейса не подтверждает себя чтением
// модуля консоли» СПОСОБЕН упасть — и что падает он на существе, а не на форме.
//
// Прежний предикат ловил МЕХАНИЗМ (в файле встречается readFileSync) и потому
// не мог отличить дефект от конформансной переписи: обе формы читают файлы.
// Сужение до класса обязано быть доказано, а не объявлено, поэтому инъекция
// идёт в ТРИ стороны:
//
//	настоящий дефект            → краснеет, называя координату;
//	законная перепись дерева    → молчит;
//	законная сверка со стволом  → молчит (`.proto` проба исполнить не может).
//
// Одного «краснеет» мало: гейт, который краснеет на всём, ничего не измеряет.
// Одного «молчит» тоже: молчание бывает от того, что читать не стали. Поэтому
// древесная инъекция вдобавок сверяет ОБЪЁМ ОСМОТРЕННОГО — он обязан вырасти
// ровно на число подложенных проб.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditUISourceReads`) и ТОТ ЖЕ гейт, что и
// прогон по дереву: проба, повторяющая логику гейта своей копией, доказывала бы
// свойство копии.
package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические пробы. Каждая — настоящая форма из дерева, а не выдумка.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ. Ровно тот образец, что размножился копированием: компонент не
// отрисован, утверждается наличие символов в его исходнике.
const synthProbeSelfSource = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "InjectedWidget.tsx"), "utf8");

describe("InjectedWidget", () => {
  it("declares its public component exports", () => {
    expect(source).toContain("InjectedWidget");
  });
});
`

// ДЕФЕКТ второго вида: читается НЕ свой модуль, а чужой — но по координате,
// которую проба написала сама. Такой модуль она обязана загрузить.
const synthProbeNamedModule = `import { readFileSync } from "node:fs";
import { join } from "node:path";

const raw = readFileSync(join(process.cwd(), "vite.config.ts"), "utf8");

describe("dev-прокси", () => {
  it("несёт запись домена", () => {
    expect(raw).toContain("\"/geo\"");
  });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 1 — перепись состава дерева. Та же пара «читает + утверждает
// о тексте», но координаты НАЙДЕНЫ обходом: исполнением одного модуля такой факт
// не получить.
const synthProbeTreeCensus = `import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const p = path.join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (p.endsWith(".tsx")) out.push(p);
  }
  return out;
}

describe("вторая копия поверхности", () => {
  const files = walk(uiRoot);

  it("объём осмотренного назван", () => {
    expect(files.length).toBeGreaterThan(10);
  });

  it("компонент объявлен в единственном месте", () => {
    const declaring = files.filter((f) => readFileSync(f, "utf8").includes("export function OnlyOnce"));
    expect(declaring.length).toBeLessThanOrEqual(1);
  });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 2 — сверка со стволом. Контракт `proto/` жест-исполнить
// нельзя: чтение здесь единственный доступный способ, и запрет на него был бы
// запретом на проверку.
const synthProbeTrunkContract = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { REGISTRY } from "@shared/lib/resource-registry";

const here = path.dirname(fileURLToPath(import.meta.url));
const proto = readFileSync(path.resolve(here, "../../../proto/kacho/cloud/geo/v1/region.proto"), "utf8");

describe("реестр консоли против контракта ствола", () => {
  it("поле, которое консоль показывает, ствол объявляет", () => {
    expect(proto).toMatch(/bool open_for_placement = \d+;/);
    expect(REGISTRY["regions"].columns.some((c) => c.path === "open_for_placement")).toBe(true);
  });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 4 — МЕТКА синтетического исходника, а не координата. Форма
// снята с `set-replacement-draft-composition.test.ts` (#498): разбор строится из
// строки В ПАМЯТИ, а первый аргумент служит именем для AST и выбора диалекта. За
// `"bad.ts"` нет ни файла, ни обращения к диску. Ровно на этой форме прежний
// предикат давал ложную находку (#523).
//
// Близнец НАМЕРЕННО не обходит дерево: обход — отдельная ветка прощения, и на
// обходящем близнеце молчание объяснялось бы ею, а не тем, что мы доказываем.
// Читает он контракт ствола — законное чтение, исполнить `.proto` проба не может.
const synthProbeSyntheticLabels = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const SITE_WITHOUT_DECLARATION = "const body = { targets: draft.targets };";
const SITE_WITH_DECLARATION = "// SetReplacementDraft\nconst body = { targets: draft.targets };";

const here = path.dirname(fileURLToPath(import.meta.url));
const contract = readFileSync(path.resolve(here, "../../../proto/kacho/cloud/nlb/v1/target_group.proto"), "utf8");

function inspectSource(fileName: string, source: string) {
  return ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true);
}

describe("состав черновика набора против контракта ствола", () => {
  it("набор объявлен контрактом", () => {
    expect(contract).toMatch(/repeated Target targets = \d+;/);
  });

  it("место без объявления распознаётся", () => {
    const bad = inspectSource("bad.ts", SITE_WITHOUT_DECLARATION);
    const good = inspectSource("good.ts", SITE_WITH_DECLARATION);
    expect(bad.statements.length).toBeGreaterThan(0);
    expect(good.statements.length).toBeGreaterThan(0);
  });
});
`

// ДЕФЕКТ третьего вида — та же метка, но уехавшая В ЧТЕНИЕ. Пара с близнецом
// выше держит дискриминатор с обеих сторон: имя одно и то же, вердикты разные,
// значит гейт различает МЕСТО литерала, а не его вид.
const synthProbeLabelThatIsReallyRead = `import { readFileSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";

const src = readFileSync(join(__dirname, "bad.ts"), "utf8");
const scan = ts.createSourceFile("bad.ts", src, ts.ScriptTarget.Latest, true);

describe("состав черновика набора", () => {
  it("объявление на месте", () => {
    expect(scan.statements.length).toBeGreaterThan(0);
  });
});
`

// ДЕФЕКТ четвёртого вида — путь собран СТРОКОЙ ВЫШЕ. Без него у запрета была бы
// дыра шириной в одну строку: literal в `join`, чтение по переменной.
const synthProbeIndirectPath = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const target = path.join(here, "IndirectWidget.tsx");
const source = readFileSync(target, "utf8");

describe("IndirectWidget", () => {
  it("declares its public component exports", () => {
    expect(source).toContain("IndirectWidget");
  });
});
`

// ЗАКОННЫЙ БЛИЗНЕЦ 3 — проба, утверждающая наблюдаемое. Файловой системы не
// касается вовсе.
const synthProbeBehaviour = `import { render, screen } from "@testing-library/react";

import { InjectedWidget } from "./InjectedWidget";

describe("InjectedWidget", () => {
  it("показывает имя ресурса, а не его идентификатор", () => {
    render(<InjectedWidget resource={{ id: "ins-1", name: "web" }} />);
    expect(screen.getByText("web")).toBeInTheDocument();
    expect(screen.queryByText("ins-1")).not.toBeInTheDocument();
  });
});
`

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в предикат.
// ─────────────────────────────────────────────────────────────────────────────

func TestUISourceReadPredicateSeparatesDefectFromCensus(t *testing.T) {
	const (
		defectSelf  = "ui-future/shared/src/components/organisms/InjectedWidget/InjectedWidget.test.tsx"
		defectNamed = "ui-future/nlb/src/injected-dev-proxy.test.ts"
		censusPath  = "ui-future/shared/src/test/injected-single-source.test.ts"
		trunkPath   = "ui-future/system/src/injected-trunk-contract.test.ts"
		behavePath  = "ui-future/shared/src/components/organisms/InjectedWidget/InjectedWidget.behaviour.test.tsx"
	)

	findings, reads, walks := auditUISourceReads(map[string]string{
		defectSelf:  synthProbeSelfSource,
		defectNamed: synthProbeNamedModule,
		censusPath:  synthProbeTreeCensus,
		trunkPath:   synthProbeTrunkContract,
		behavePath:  synthProbeBehaviour,
	}, nil)

	got := map[string]uiSourceFinding{}
	for _, f := range findings {
		got[f.File] = f
	}

	t.Run("настоящий дефект краснеет и называет координату", func(t *testing.T) {
		f, ok := got[defectSelf]
		if !ok {
			t.Fatal("проба, подтверждающая себя чтением своего .tsx, гейтом НЕ поймана — " +
				"это ровно тот образец, ради которого гейт заведён")
		}
		if !strings.Contains(strings.Join(f.Coords, ","), "InjectedWidget.tsx") {
			t.Errorf("координата не названа: %v — вердикт без координаты не приводит к правке", f.Coords)
		}
		if !strings.Contains(f.Why, "СВОЙ модуль") {
			t.Errorf("вид находки назван неверно: %q", f.Why)
		}
	})

	t.Run("чужой модуль по своей координате краснеет тоже", func(t *testing.T) {
		f, ok := got[defectNamed]
		if !ok {
			t.Fatal("проба, читающая vite.config.ts вместо того чтобы его загрузить, гейтом НЕ поймана")
		}
		if !strings.Contains(strings.Join(f.Coords, ","), "vite.config.ts") {
			t.Errorf("координата не названа: %v", f.Coords)
		}
	})

	t.Run("законная перепись дерева молчит", func(t *testing.T) {
		if f, ok := got[censusPath]; ok {
			t.Errorf("перепись состава дерева объявлена находкой (%s, %v) — гейт ловит форму, "+
				"а не существо: такой факт исполнением одного модуля не получить", f.Why, f.Coords)
		}
	})

	t.Run("законная сверка со стволом молчит", func(t *testing.T) {
		if f, ok := got[trunkPath]; ok {
			t.Errorf("сверка с `proto/` объявлена находкой (%s, %v) — контракт ствола проба "+
				"исполнить не может, запрет здесь был бы запретом на проверку", f.Why, f.Coords)
		}
	})

	t.Run("проба на наблюдаемое молчит", func(t *testing.T) {
		if _, ok := got[behavePath]; ok {
			t.Error("проба, рендерящая компонент, объявлена находкой — гейт наказывал бы за верный исход")
		}
	})

	t.Run("перепись сама различает виды", func(t *testing.T) {
		// Четыре пробы читают с диска, одна из них обходит дерево, пятая не
		// касается диска вовсе. Если счётчики схлопнутся, вердикты выше станут
		// свойством сломанного разбора, а не предиката.
		if reads != 4 {
			t.Errorf("читающих проб насчитано %d, ожидалось 4 — распознавание чтения сломано", reads)
		}
		if walks != 1 {
			t.Errorf("обходящих проб насчитано %d, ожидалось 1 — распознавание переписи сломано", walks)
		}
	})
}

// TestUISourceReadPredicateTellsCoordinateFromLabel — дискриминатор различает
// МЕСТО литерала, а не его вид.
//
// Пара держится с обеих сторон одним и тем же именем `bad.ts`: уехавшее в
// произвольную функцию — метка (молчание), уехавшее в чтение — координата
// (краснеет). Разойдись эти два вердикта в одну сторону, и гейт мерил бы вид
// имени: тогда он либо снова краснел бы на синтетике соседа, либо перестал бы
// видеть чтение по имени, похожему на метку.
//
// Третий случай — путь, собранный СТРОКОЙ ВЫШЕ: без него сужение оставило бы
// дыру шириной в одну строку.
func TestUISourceReadPredicateTellsCoordinateFromLabel(t *testing.T) {
	const (
		labelPath    = "ui-future/shared/src/test/injected-synthetic-labels.test.ts"
		labelReadRel = "ui-future/shared/src/test/injected-label-really-read.test.ts"
		indirectRel  = "ui-future/compute/src/injected-indirect-path.test.ts"
	)

	findings, reads, walks := auditUISourceReads(map[string]string{
		labelPath:    synthProbeSyntheticLabels,
		labelReadRel: synthProbeLabelThatIsReallyRead,
		indirectRel:  synthProbeIndirectPath,
	}, nil)

	got := map[string]uiSourceFinding{}
	for _, f := range findings {
		got[f.File] = f
	}

	t.Run("метка синтетического исходника молчит", func(t *testing.T) {
		if f, ok := got[labelPath]; ok {
			t.Errorf("имя синтетического исходника объявлено координатой (%s, %v).\n"+
				"За `bad.ts`/`good.ts` здесь нет ни файла, ни обращения к диску: разбор строится "+
				"из строки в памяти, имя служит меткой AST. Это ложная находка #523.", f.Why, f.Coords)
		}
	})

	t.Run("то же имя, уехавшее в чтение, краснеет", func(t *testing.T) {
		f, ok := got[labelReadRel]
		if !ok {
			t.Fatal("чтение файла по литералу НЕ поймано — сужение отняло у гейта предмет, " +
				"а не ложную находку: молчание выше тогда ничего не стоит")
		}
		if !strings.Contains(strings.Join(f.Coords, ","), "bad.ts") {
			t.Errorf("координата не названа: %v", f.Coords)
		}
	})

	t.Run("путь, собранный строкой выше, краснеет", func(t *testing.T) {
		f, ok := got[indirectRel]
		if !ok {
			t.Fatal("литерал в `path.join(…)`, прочитанный через переменную, НЕ пойман — " +
				"у запрета осталась дыра шириной в одну строку")
		}
		if !strings.Contains(strings.Join(f.Coords, ","), "IndirectWidget.tsx") {
			t.Errorf("координата не названа: %v", f.Coords)
		}
	})

	t.Run("перепись различает виды", func(t *testing.T) {
		// Все три читают с диска, ни одна не обходит дерево: значит молчание
		// первой объясняется дискриминатором координаты, а НЕ ветвью переписи.
		if reads != 3 {
			t.Errorf("читающих проб насчитано %d, ожидалось 3 — распознавание чтения сломано", reads)
		}
		if walks != 0 {
			t.Errorf("обходящих проб насчитано %d, ожидалось 0 — близнец прощён не тем, чем мы думаем", walks)
		}
	})
}

// TestUISourceReadPredicateReadsCodeNotText — разбор идёт по исполняемой части.
// Без этого гейт нашёл бы `readFileSync` в абзаце, который сам же объясняет
// запрет, и остался бы зелёным при снятой защите.
func TestUISourceReadPredicateReadsCodeNotText(t *testing.T) {
	const rel = "ui-future/shared/src/components/organisms/Decoy/Decoy.test.tsx"

	t.Run("запрещённая форма в КОММЕНТАРИИ — не находка", func(t *testing.T) {
		src := `// Так делать нельзя:
//   const source = readFileSync(join(here, "Decoy.tsx"), "utf8");
//   expect(source).toContain("Decoy");
// Поэтому компонент здесь рендерится.
import { render, screen } from "@testing-library/react";
import { Decoy } from "./Decoy";

it("показывает имя", () => {
  render(<Decoy name="web" />);
  expect(screen.getByText("web")).toBeInTheDocument();
});
`
		findings, reads, _ := auditUISourceReads(map[string]string{rel: src}, nil)
		if len(findings) != 0 {
			t.Errorf("гейт покраснел на собственном объяснении запрета: %v — такой гейт снимут первым", findings)
		}
		if reads != 0 {
			t.Errorf("чтение засчитано по комментарию (reads=%d) — разбор читает текст, а не код", reads)
		}
	})

	t.Run("та же форма в КОДЕ — находка", func(t *testing.T) {
		src := `import { readFileSync } from "node:fs";
import { join } from "node:path";
const source = readFileSync(join(__dirname, "Decoy.tsx"), "utf8");
it("declares", () => { expect(source).toContain("Decoy"); });
`
		findings, _, _ := auditUISourceReads(map[string]string{rel: src}, nil)
		if len(findings) != 1 {
			t.Fatalf("та же форма в коде НЕ поймана: %v", findings)
		}
	})

	t.Run("регулярный литерал с кавычками не рвёт разбор", func(t *testing.T) {
		// Настоящий вход из дерева: `matchAll(/"(\/[^"]+)":\s*\{/g)`. Принятый за
		// деление, он переключил бы разбор в строку и съел бы остаток файла —
		// гейт замолчал бы, не сказав об этом.
		src := "import { readFileSync } from \"node:fs\";\n" +
			"const raw = readFileSync(\"vite.config.ts\", \"utf8\");\n" +
			"const keys = [...raw.matchAll(/\"(\\/[^\"]+)\":\\s*\\{/g)].map((m) => m[1]);\n" +
			"it(\"proxied\", () => { expect(keys.length).toBeGreaterThan(0); });\n"
		findings, reads, _ := auditUISourceReads(map[string]string{rel: src}, nil)
		if reads != 1 {
			t.Fatalf("чтение не распознано (reads=%d) — регулярный литерал сорвал разбор", reads)
		}
		if len(findings) != 1 || !strings.Contains(strings.Join(findings[0].Coords, ","), "vite.config.ts") {
			t.Fatalf("координата за регулярным литералом потеряна: %v", findings)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Инъекция в дерево: гейт читает состав у git-индекса.
//
// Дерево для этой инъекции — СИНТЕТИЧЕСКОЕ, во временном каталоге. Прежняя
// редакция подкладывала пробы в индекс ЖИВОЙ рабочей копии (`git add` по её
// корню) и убирала их в `t.Cleanup`. Снятие прогона между этими двумя шагами —
// по времени, по памяти, по месту на диске — до уборки не доходит, и записи
// остаются staged: файлов нет ни на диске, ни в `HEAD`, а состав корпуса ВСЕ
// гейты этого дерева берут именно у индекса. Соседняя сессия получала от них
// вердикт о корпусе, которого не существует, — то есть порча тихая и делает
// лживыми ровно те проверки, ради которых состав и берётся у индекса (#696).
//
// Синтетическое дерево закрывает класс по построению, а не уборкой: живой копии
// проба не касается вовсе, поэтому её прерывание на ЛЮБОМ шаге не оставляет ни
// staged-записей, ни правок.
// ─────────────────────────────────────────────────────────────────────────────

var (
	censusLine = regexp.MustCompile(
		`перепись: проб интерфейса осмотрено (\d+), читают с диска (\d+), обходят дерево (\d+), находок (\d+)`)
	judgedTreeLine = regexp.MustCompile(`гейт судит дерево: (\S+)`)
)

type uiCensus struct{ scanned, reads, walks, findings int }

// runTreeGate прогоняет гейт отдельным процессом и возвращает его перепись.
//
// moduleRoot — откуда собирается пакет (корень модуля, читается только на
// чтение); treeRoot — дерево, состав которого гейт обязан судить. Пустой
// treeRoot означает «умолчание гейта» и служит положительным контролем: без
// явного входа гейт обязан судить живую рабочую копию, иначе ручка входа
// молча уводила бы его с настоящего дерева.
//
// Перепись — отдельное утверждение: без неё «молчит» неотличимо от «не прочитал».
func runTreeGate(t *testing.T, moduleRoot, treeRoot string) (uiCensus, string, string, bool) {
	t.Helper()
	cmd := exec.Command("go", "test", "./internal/repohygiene/", "-count=1", "-v",
		"-run", "TestUITestsDoNotReadTheirOwnSourceAsText")
	cmd.Dir = moduleRoot
	// Окружение чищено от переменных, которыми git выбирает репозиторий в обход
	// рабочего каталога: иначе `git ls-files` синтетического дерева ответил бы
	// про чужой индекс, и вердикт стал бы свойством запуска, а не дерева.
	cmd.Env = gitenv.Env()
	if treeRoot != "" {
		cmd.Env = append(cmd.Env, uiProbeTreeRootEnv+"="+treeRoot)
	}
	out, err := cmd.CombinedOutput()
	m := censusLine.FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("гейт не напечатал перепись — вердикт был бы утверждением ни о чём:\n%s", out)
	}
	j := judgedTreeLine.FindStringSubmatch(string(out))
	if j == nil {
		t.Fatalf("гейт не назвал дерево, которое судит — «ноль находок» неотличимо от "+
			"«ноль прочитанного»:\n%s", out)
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return uiCensus{n(m[1]), n(m[2]), n(m[3]), n(m[4])}, j[1], string(out), err == nil
}

// synthUIProbeTree собирает синтетическое дерево проб интерфейса и делает его
// видимым индексу: состав корпуса гейт берёт у `git ls-files`, поэтому файл,
// лежащий рядом с репозиторием, но не добавленный в индекс, ему не виден — и
// без `git add` инъекция дала бы ложное «гейт мёртв».
func synthUIProbeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	// Репозиторий заводится ДО первой записи: `git add` без него отказывает, а
	// отказ читался бы как «гейт мёртв» вместо «дерево не собрано».
	if out, err := gitenv.Command(root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init в синтетическом дереве: %v\n%s", err, out)
	}
	writeUIProbes(t, root, files)
	return root
}

// writeUIProbes кладёт пробы в дерево и добавляет их в индекс ЭТОГО дерева.
// Все пути — внутри временного каталога: живая рабочая копия не упоминается.
func writeUIProbes(t *testing.T, root string, files map[string]string) {
	t.Helper()
	rels := make([]string, 0, len(files))
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
			t.Fatalf("подложить %s: %v", rel, err)
		}
		rels = append(rels, rel)
	}
	if len(rels) == 0 {
		return
	}
	addArgs := append([]string{"add", "-f", "--"}, rels...)
	if out, err := gitenv.Command(root, addArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git add в синтетическом дереве: %v\n%s", err, out)
	}
}

func TestUISourceReadGateOnTreeFailsOnInjectedDefect(t *testing.T) {
	// Краткого режима здесь НЕТ намеренно: пропуск сделал бы весь пакет
	// краткогейтящим, а он не входит ни в один отбор интеграционной джобы —
	// то есть доказательство способности гейта упасть не исполнялось бы нигде.
	// Цена — вложенные `go test` на прогон пакета.
	module := repoRoot(t)

	const (
		censusRel = "ui-future/shared/src/test/injected-census.test.ts"
		trunkRel  = "ui-future/system/src/injected-trunk.test.ts"
		defectRel = "ui-future/shared/src/test/injected-self-source.test.tsx"
	)

	// Дерево ДО инъекции несёт только законных близнецов. Их молчание и есть
	// отрицательная половина пары, и доказывается она тем же прогоном, что и
	// положительная: базовый прогон обязан быть зелёным.
	tree := synthUIProbeTree(t, map[string]string{
		censusRel: synthProbeTreeCensus,
		trunkRel:  synthProbeTrunkContract,
	})

	base, judged, baseLog, basePassed := runTreeGate(t, module, tree)
	if !basePassed {
		t.Fatalf("гейт красен на дереве из одних законных близнецов (находок %d) — "+
			"он ловит форму, а не существо, и красное после инъекции было бы "+
			"неотличимо от красного до неё:\n%s", base.findings, baseLog)
	}
	if judged != tree {
		t.Fatalf("гейт судил %s, а инъекция шла в %s — вердикт ниже относился бы к "+
			"чужому дереву", judged, tree)
	}
	if base.scanned != 2 || base.reads != 2 || base.walks != 1 || base.findings != 0 {
		t.Fatalf("перепись до инъекции: осмотрено %d, читают %d, обходят %d, находок %d; "+
			"ожидалось 2/2/1/0 — предпосылки дискриминатора не выполнены, значит его "+
			"молчание ничего не стоит", base.scanned, base.reads, base.walks, base.findings)
	}

	// Тот же прогон, плюс настоящий дефект. Так «краснеет» и «молчит»
	// доказываются одним прогоном, и ни одно из двух не объясняется тем, что
	// второе не читалось.
	writeUIProbes(t, tree, map[string]string{defectRel: synthProbeSelfSource})

	after, judgedAfter, log, passed := runTreeGate(t, module, tree)

	if passed {
		t.Error("гейт зелен при подложенном дефекте — он не способен упасть")
	}
	if judgedAfter != tree {
		t.Errorf("гейт судил %s вместо %s", judgedAfter, tree)
	}
	if after.scanned != base.scanned+1 {
		t.Errorf("объём осмотренного вырос с %d до %d, ожидалось +1 — гейт не прочитал "+
			"подложенное, и его молчание по законным близнецам ничего не значит",
			base.scanned, after.scanned)
	}
	if after.findings != base.findings+1 {
		t.Errorf("находок стало %d при %d до инъекции: ожидалась ровно ОДНА новая (дефект). "+
			"Больше — гейт зачёл законного близнеца; меньше — не заметил дефекта",
			after.findings, base.findings)
	}
	if !strings.Contains(log, "injected-self-source.test.tsx") {
		t.Errorf("координата подложенного дефекта в вердикте не названа:\n%s", log)
	}
	for _, legal := range []string{"injected-census.test.ts", "injected-trunk.test.ts"} {
		if strings.Contains(log, legal) {
			t.Errorf("законный близнец %s объявлен находкой — гейт ловит форму, а не существо", legal)
		}
	}
	if after.walks != base.walks {
		t.Errorf("обходящих проб стало %d при %d до инъекции — разбор схлопнулся, "+
			"и молчание переписи объясняется не тем, чем мы думаем", after.walks, base.walks)
	}
}

// TestUISourceReadGateJudgesTheLiveTreeByDefault — положительный контроль к
// ручке входа.
//
// Гейт, чей корпус задаётся снаружи, обязан по умолчанию судить ЖИВУЮ рабочую
// копию: иначе ручка, забытая в окружении, увела бы его на пустое дерево, и
// «ноль находок» стало бы свойством запуска. Утверждается именно то, что гейт
// НАЗЫВАЕТ, а не его вердикт: вердикт про живое дерево даёт сам гейт в этом же
// пакете, и дублировать его здесь значило бы завести два места об одном.
func TestUISourceReadGateJudgesTheLiveTreeByDefault(t *testing.T) {
	module := repoRoot(t)
	_, judged, log, _ := runTreeGate(t, module, "")
	if judged != module {
		t.Errorf("без явного входа гейт судит %s, а живой корень — %s:\n%s", judged, module, log)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Обход дерева, записанный ЧЕРЕЗ ПОМОЩНИКА (#1517).
//
// Прежний распознаватель знал у обхода ровно одну форму записи — прямой вызов
// системной функции в теле самой пробы (`readdirSync(`). В этом дереве обход
// записывают и второй, столь же законной формой: он вынесен в модуль-помощник,
// а проба зовёт его экспорт. Форма не редкая и не краевая — помощников-обходчиков
// в дереве пять, и через них обходят пять проб.
//
// Для распознавателя, знающего одну форму, эти пробы — не находка и не чистое
// место: они ВНЕ НАБЛЮДЕНИЯ (`testing.md` §«Гейт на класс», п. 7). Ветка прощения
// переписи до них не доходит, и перепись состава дерева объявляется тем самым
// дефектом, ради которого гейт заведён.
//
// Инъекция держит обе стороны ОДНИМ помощником: он экспортирует и обходящий
// `sourceFiles`, и НЕ обходящий `declaredSymbols`. Проба, зовущая первый, —
// перепись (молчание); проба, взявшая у того же помощника второй и читающая
// названный ею модуль, — находка. Разойдись эти вердикты в одну сторону, и гейт
// мерил бы ИМПОРТ ПОМОЩНИКА, а не то, доходит ли проба до обхода: тогда одна
// строка импорта снимала бы запрет с любой пробы.
// ─────────────────────────────────────────────────────────────────────────────

// ПОМОЩНИК. Форма снята с `shared/src/test/shared-symbol-sweep.ts`: один модуль
// несёт и обходчик, и разбор, который дерева не касается вовсе.
const synthWalkerHelperModule = `import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";

const DECLARATION = /^[ \t]*export[ \t]+(?:function|const)[ \t]+([A-Za-z_$][\w$]*)/gm;

export function declaredSymbols(src: string): Set<string> {
  const out = new Set<string>();
  DECLARATION.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = DECLARATION.exec(src)) !== null) out.add(m[1]);
  return out;
}

export function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  const walk = (cur: string) => {
    for (const entry of readdirSync(cur, { withFileTypes: true })) {
      const full = path.join(cur, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (/\.tsx?$/.test(entry.name)) out.push(full);
    }
  };
  walk(dir);
  return out;
}

export function sweep(root: string): string[] {
  return sourceFiles(root).filter((f) => statSync(f).size > 0);
}

export function isReExportOnly(src: string): boolean {
  return /^\s*export \* from/m.test(src) && !readFileSync;
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 5 — перепись состава дерева, обход у которой вынесен в
// помощника. Названный ею модуль (`shared/src/api/types.ts`) — ЭТАЛОННЫЙ НАБОР
// переписи, а не предмет самоподтверждения: утверждение делается о находках
// обхода, и краснеет оно ровно тогда, когда в дереве заводят вторую копию.
const synthProbeHelperCensus = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { declaredSymbols, sourceFiles } from "@shared/test/shared-symbol-sweep";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const platform = declaredSymbols(readFileSync(path.join(repoRoot, "shared/src/api/types.ts"), "utf8"));
const files = sourceFiles(path.join(repoRoot, "compute/src"));

describe("типы провода платформы объявлены один раз", () => {
  it("перепись названа", () => {
    expect(files.length).toBeGreaterThan(0);
    expect(platform.size).toBeGreaterThan(0);
  });

  it("модуль не объявляет заново ни один символ платформы", () => {
    const again = files.filter((f) => [...declaredSymbols(readFileSync(f, "utf8"))].some((s) => platform.has(s)));
    expect(again).toEqual([]);
  });
});
`

// ДЕФЕКТ пятого вида — импорт ТОГО ЖЕ помощника, но взят НЕ обходящий экспорт.
// Проба до обхода не доходит и подтверждает названный ею модуль его же текстом.
// Без этого близнеца починка выродилась бы в маску: «есть импорт помощника —
// прощаем», и одна строка импорта снимала бы запрет с любой пробы.
const synthProbeHelperImportNoWalk = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { declaredSymbols } from "@shared/test/shared-symbol-sweep";

const here = path.dirname(fileURLToPath(import.meta.url));
const declared = declaredSymbols(readFileSync(path.join(here, "HelperlessWidget.tsx"), "utf8"));

describe("HelperlessWidget", () => {
  it("объявляет свой публичный экспорт", () => {
    expect(declared.has("HelperlessWidget")).toBe(true);
  });
});
`

// ДЕФЕКТ шестого вида — обход через помощника ЕСТЬ, и всё же проба читает СВОЙ
// модуль. Обход такую форму не оправдывает и не оправдывал: ветка «свой модуль»
// стоит до ветки прощения переписи, и починка не вправе её ослабить.
const synthProbeHelperCensusReadsOwn = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { sourceFiles } from "@shared/test/shared-symbol-sweep";

const here = path.dirname(fileURLToPath(import.meta.url));
const files = sourceFiles(path.resolve(here, ".."));
const source = readFileSync(path.join(here, "OwnWidget.tsx"), "utf8");

describe("OwnWidget", () => {
  it("перепись названа", () => {
    expect(files.length).toBeGreaterThan(0);
  });

  it("объявляет свой публичный экспорт", () => {
    expect(source).toContain("OwnWidget");
  });
});
`

// TestUISourceReadPredicateSeesTreeWalkBehindAHelper — распознаватель знает ОБЕ
// законные формы записи обхода, и различает их по тому, ДОХОДИТ ли проба до
// обхода, а не по факту импорта помощника.
func TestUISourceReadPredicateSeesTreeWalkBehindAHelper(t *testing.T) {
	const (
		helperPath = "ui-future/shared/src/test/shared-symbol-sweep.ts"
		censusPath = "ui-future/compute/src/api/injected-helper-census.test.ts"
		noWalkPath = "ui-future/compute/src/api/injected-helper-no-walk.test.ts"
		readsOwn   = "ui-future/shared/src/components/organisms/OwnWidget/OwnWidget.test.tsx"
		directWalk = "ui-future/shared/src/test/injected-single-source.test.ts"
	)

	helpers := map[string]string{helperPath: synthWalkerHelperModule}

	findings, _, walks := auditUISourceReads(map[string]string{
		censusPath: synthProbeHelperCensus,
		noWalkPath: synthProbeHelperImportNoWalk,
		readsOwn:   synthProbeHelperCensusReadsOwn,
		directWalk: synthProbeTreeCensus,
	}, helpers)

	got := map[string]uiSourceFinding{}
	for _, f := range findings {
		got[f.File] = f
	}

	t.Run("перепись с обходом через помощника молчит", func(t *testing.T) {
		if f, ok := got[censusPath]; ok {
			t.Errorf("перепись состава дерева объявлена находкой (%s, %v).\n"+
				"Обход записан второй законной формой — вынесен в помощника, — и распознаватель, "+
				"знающий только прямой вызов, объявляет такую пробу дефектом. Это не находка и не "+
				"чистое место: целый вид предмета вне наблюдения.", f.Why, f.Coords)
		}
	})

	t.Run("тот же помощник без обхода запрета НЕ снимает", func(t *testing.T) {
		f, ok := got[noWalkPath]
		if !ok {
			t.Fatal("проба, взявшая у помощника НЕ обходящий экспорт и подтверждающая названный " +
				"ею модуль его же текстом, гейтом НЕ поймана — прощение выродилось в маску: " +
				"одна строка импорта снимает запрет с любой пробы")
		}
		if !strings.Contains(strings.Join(f.Coords, ","), "HelperlessWidget.tsx") {
			t.Errorf("координата не названа: %v", f.Coords)
		}
	})

	t.Run("обход через помощника не оправдывает чтения СВОЕГО модуля", func(t *testing.T) {
		f, ok := got[readsOwn]
		if !ok {
			t.Fatal("проба, читающая свой .tsx, прощена за обход через помощника — " +
				"ветка «свой модуль» стоит ДО прощения переписи и ослаблена быть не может")
		}
		if !strings.Contains(f.Why, "СВОЙ модуль") {
			t.Errorf("вид находки назван неверно: %q", f.Why)
		}
	})

	t.Run("перепись считает обходом обе формы", func(t *testing.T) {
		// Три пробы доходят до обхода: две через помощника, одна прямым вызовом.
		// Схлопнись счётчик — молчание выше объяснялось бы сломанным разбором.
		if walks != 3 {
			t.Errorf("обходящих проб насчитано %d, ожидалось 3 — вторая форма записи обхода "+
				"распознавателю невидима, и «обходят дерево» занижено молча", walks)
		}
	})
}

// ПОМОЩНИК второго вида — обход вынесен в МЕСТНУЮ, не экспортируемую функцию, а
// экспорт зовёт её. Форма снята с `shared/src/test/identity-ceremony-carriers.ts`:
// там обходчик `sourceFiles` объявлен местным, и `walkCeremonyCarriers` доходит
// до обхода только через него.
//
// Замыкание, идущее лишь по ЭКСПОРТАМ, такой экспорт обходящим не признаёт —
// то есть внутри самой починки остаётся слепая зона того же класса, ради
// которого она делается.
const synthWalkerHelperViaLocal = `import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

function collect(dir: string, acc: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) collect(full, acc);
    else acc.push(full);
  }
  return acc;
}

export function carriers(uiRoot: string): string[] {
  return collect(uiRoot).filter((f) => readFileSync(f, "utf8").includes("Provider"));
}
`

// ЗАКОННЫЙ БЛИЗНЕЦ 6 — перепись, чей обход доезжает до системного вызова через
// МЕСТНУЮ функцию помощника.
const synthProbeCensusViaLocalWalker = `import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { carriers } from "@shared/test/identity-ceremony-carriers";

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const found = carriers(uiRoot);
const reference = readFileSync(path.join(uiRoot, "shared/src/api/types.ts"), "utf8");

describe("носители обряда объявлены один раз", () => {
  it("перепись названа", () => {
    expect(found.length).toBeGreaterThan(0);
    expect(reference.length).toBeGreaterThan(0);
  });

  it("второй копии носителя нет", () => {
    expect(found.filter((f) => f.includes("/legacy/"))).toEqual([]);
  });
});
`

// TestUISourceReadPredicateFollowsWalksThroughLocalHelpers — замыкание идёт по
// ВСЕМ функциям модуля, а не только по экспортируемым.
//
// Иначе починка сама несёт слепую зону того же класса: экспорт, доходящий до
// обхода через местную функцию, обходящим не признаётся, и перепись, записанная
// этой формой, снова объявляется дефектом.
func TestUISourceReadPredicateFollowsWalksThroughLocalHelpers(t *testing.T) {
	const (
		helperPath = "ui-future/shared/src/test/identity-ceremony-carriers.ts"
		censusPath = "ui-future/shared/src/test/injected-ceremony-census.test.ts"
	)

	walking, _, _ := walkingHelperExports(map[string]string{helperPath: synthWalkerHelperViaLocal})
	if !walking["carriers"] {
		t.Errorf("экспорт, доходящий до обхода через МЕСТНУЮ функцию, обходящим не признан "+
			"(признаны: %v).\nЗамыкание идёт только по экспортам — значит у починки та же "+
			"слепая зона, ради которой она делается.", uiWalkNamesOf(walking))
	}

	findings, _, walks := auditUISourceReads(
		map[string]string{censusPath: synthProbeCensusViaLocalWalker},
		map[string]string{helperPath: synthWalkerHelperViaLocal},
	)
	if len(findings) != 0 {
		t.Errorf("перепись объявлена находкой: %v", findings)
	}
	if walks != 1 {
		t.Errorf("обходящих проб насчитано %d, ожидалось 1 — обход через местную функцию "+
			"помощника распознавателю невидим", walks)
	}
}
