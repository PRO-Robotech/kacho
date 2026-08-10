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
	})

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
		findings, reads, _ := auditUISourceReads(map[string]string{rel: src})
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
		findings, _, _ := auditUISourceReads(map[string]string{rel: src})
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
		findings, reads, _ := auditUISourceReads(map[string]string{rel: src})
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
// ─────────────────────────────────────────────────────────────────────────────

var censusLine = regexp.MustCompile(
	`перепись: проб интерфейса осмотрено (\d+), читают с диска (\d+), обходят дерево (\d+), находок (\d+)`)

type uiCensus struct{ scanned, reads, walks, findings int }

// runTreeGate прогоняет гейт по дереву отдельным процессом и возвращает его
// перепись. Перепись — отдельное утверждение: без неё «молчит» неотличимо от
// «не прочитал».
func runTreeGate(t *testing.T, root string) (uiCensus, string, bool) {
	t.Helper()
	cmd := exec.Command("go", "test", "./internal/repohygiene/", "-count=1", "-v",
		"-run", "TestUITestsDoNotReadTheirOwnSourceAsText")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	m := censusLine.FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("гейт не напечатал перепись — вердикт был бы утверждением ни о чём:\n%s", out)
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	return uiCensus{n(m[1]), n(m[2]), n(m[3]), n(m[4])}, string(out), err == nil
}

func TestUISourceReadGateOnTreeFailsOnInjectedDefect(t *testing.T) {
	// Краткого режима здесь НЕТ намеренно: пропуск сделал бы весь пакет
	// краткогейтящим, а он не входит ни в один отбор интеграционной джобы —
	// то есть доказательство способности гейта упасть не исполнялось бы нигде.
	// Цена — один вложенный `go test` на прогон пакета.
	root := repoRoot(t)

	base, _, basePassed := runTreeGate(t, root)
	if !basePassed {
		t.Fatalf("гейт красен ДО инъекции (находок %d) — инъекция ничего не доказала бы: "+
			"красное после неё было бы неотличимо от красного до", base.findings)
	}

	// Законные близнецы и дефект кладутся ВМЕСТЕ: так «краснеет» и «молчит»
	// доказываются одним прогоном, и ни одно из двух не объясняется тем, что
	// второе не читалось.
	injected := map[string]string{
		"ui-future/shared/src/test/injected-census.test.ts":       synthProbeTreeCensus,
		"ui-future/shared/src/test/injected-trunk.test.ts":        synthProbeTrunkContract,
		"ui-future/shared/src/test/injected-self-source.test.tsx": synthProbeSelfSource,
	}
	var rels []string
	for rel, body := range injected {
		abs := filepath.Join(root, rel)
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("подложить %s: %v", rel, err)
		}
		rels = append(rels, rel)
	}
	// Гейт берёт состав у git-индекса, поэтому файл на диске ему не виден:
	// без `git add` инъекция дала бы ложное «гейт мёртв».
	addArgs := append([]string{"-C", root, "add", "-f", "--"}, rels...)
	if out, err := exec.Command("git", addArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		rmArgs := append([]string{"-C", root, "rm", "-q", "-f", "--"}, rels...)
		if out, err := exec.Command("git", rmArgs...).CombinedOutput(); err != nil {
			t.Errorf("уборка инъекции не удалась: %v\n%s — дерево осталось грязным", err, out)
		}
	})

	after, log, passed := runTreeGate(t, root)

	if passed {
		t.Error("гейт зелен при подложенном дефекте — он не способен упасть")
	}
	if after.scanned != base.scanned+len(injected) {
		t.Errorf("объём осмотренного вырос с %d до %d, ожидалось +%d — гейт не прочитал подложенное, "+
			"и его молчание по законным близнецам ничего не значит",
			base.scanned, after.scanned, len(injected))
	}
	if after.findings != base.findings+1 {
		t.Errorf("находок стало %d при %d до инъекции: ожидалась ровно ОДНА новая (дефект). "+
			"Больше — гейт зачёл законного близнеца; меньше — не заметил дефекта", after.findings, base.findings)
	}
	if !strings.Contains(log, "injected-self-source.test.tsx") {
		t.Errorf("координата подложенного дефекта в вердикте не названа:\n%s", log)
	}
	for _, legal := range []string{"injected-census.test.ts", "injected-trunk.test.ts"} {
		if strings.Contains(log, legal) {
			t.Errorf("законный близнец %s объявлен находкой — гейт ловит форму, а не существо", legal)
		}
	}
	if after.walks != base.walks+1 {
		t.Errorf("обходящих проб стало %d при %d до инъекции — подложенная перепись не опознана как перепись, "+
			"значит её молчание объясняется не тем, чем мы думаем", after.walks, base.walks)
	}
}
