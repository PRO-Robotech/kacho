// Способность правила «конверт платформы объявлен один раз» УПАСТЬ — и
// промолчать там, где падать не на чем.
//
// Над настоящим деревом правило сегодня зелёное (копий нет — это его ЦЕЛЬ),
// поэтому его работоспособность из зелени не следует НИКАК. Здесь она
// доказывается инъекцией на синтетическом дереве: настоящая копия конверта —
// находка с координатой и именем символа; законный близнец (доменный тип того же
// модуля и ре-экспорт того же имени) — молчание.

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { envelopeCensus } from "./platform-envelope";

let root: string;

/** Кладёт файл, создавая каталоги. */
function put(rel: string, body: string): void {
  const full = path.join(root, rel);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, body, "utf8");
}

beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), "kacho-envelope-"));
  // Общий контракт провода — то, относительно чего судят остальных.
  put(
    "shared/src/api/types.ts",
    ["export interface Operation {", "  id: string;", "}", "export type OperationList = Operation[];", ""].join("\n"),
  );
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

describe("инъекция: правило падает на копии конверта и молчит на доменном типе", () => {
  it("настоящий дефект — модуль объявил конверт заново — находка с координатой и символами", () => {
    put("demo/src/api/types.ts", ["export interface Operation {", "  id: string;", "}", ""].join("\n"));
    const census = envelopeCensus(root);
    expect(census.hits.map((h) => `${h.file}: ${h.symbols.join(", ")}`)).toEqual([
      "demo/src/api/types.ts: Operation",
    ]);
  });

  it("законный близнец: доменный тип модуля правилом НЕ задет", () => {
    // Без него отрицание зеленело бы и на правке, которая просто перестала
    // читать исходники модуля: «ноль находок» получилось бы из пустого обхода.
    put("demo/src/api/types.ts", ["export interface NetworkLoadBalancer {", "  id: string;", "}", ""].join("\n"));
    const census = envelopeCensus(root);
    expect(census.hits).toEqual([]);
    expect(census.filesRead).toBeGreaterThan(0);
    expect(census.apps).toEqual(["demo"]);
  });

  it("законный близнец: РЕ-ЭКСПОРТ того же имени находкой не является", () => {
    // Это и есть форма, которую требуется писать вместо копии. Считай её
    // объявлением — и правило требовало бы невозможного.
    put("demo/src/api/types.ts", ['export type { Operation, OperationList } from "@shared/api/types";', ""].join("\n"));
    expect(envelopeCensus(root).hits).toEqual([]);
  });

  it("перечень приложений ВЫВОДИТСЯ обходом, а не выписан", () => {
    put("alpha/src/api/types.ts", ["export interface Alpha {", "  id: string;", "}", ""].join("\n"));
    put("beta/src/api/types.ts", ["export interface Beta {", "  id: string;", "}", ""].join("\n"));
    expect(envelopeCensus(root).apps).toEqual(["alpha", "beta"]);
  });

  it("проба модуля правилом не судится: копия в `.test.ts` находкой не является", () => {
    // Фикстура пробы объявляет `Operation`, чтобы подать его в проверяемый код.
    // Считай её копией — и правило краснело бы на собственных пробах дерева.
    put("demo/src/api/types.test.ts", ["export interface Operation {", "  id: string;", "}", ""].join("\n"));
    expect(envelopeCensus(root).hits).toEqual([]);
  });
});
