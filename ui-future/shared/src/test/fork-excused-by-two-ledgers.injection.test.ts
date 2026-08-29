// Способность правила «прощён дважды» УПАСТЬ — и промолчать там, где падать не
// на чем.
//
// Над настоящим деревом правило сегодня зелёное (пересечение пусто — это его
// ЦЕЛЬ), поэтому его работоспособность из зелени не следует НИКАК. Здесь она
// доказывается инъекцией на синтетических ведомостях: настоящий двойной
// прощённый — находка; законный близнец (тот же файл, прощённый ОДНОЙ
// ведомостью) — молчание; сдвинутая форма ключа — находка отдельной ветвью,
// иначе пересечение молчало бы при живом классе.

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { doubleExcused } from "./fork-excused-by-two-ledgers";

let root: string;

beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), "kacho-two-ledgers-"));
  mkdirSync(path.join(root, "shared/src/test"), { recursive: true });
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

/** Кладёт обе ведомости: копии — списком файлов, мёртвые — списком файлов. */
function seed(forks: string[], dead: string[]): void {
  writeFileSync(
    path.join(root, "shared/src/test/shared-fork-ledger.json"),
    JSON.stringify({
      groups: [{ id: "demo:lib", reason: "синтетика", entries: forks.map((file) => ({ file, symbols: [] })) }],
    }),
    "utf8",
  );
  writeFileSync(
    path.join(root, "shared/src/test/module-reachability-ledger.json"),
    JSON.stringify({ allowed: Object.fromEntries(dead.map((f) => [f, "синтетика"])) }),
    "utf8",
  );
}

describe("инъекция: правило падает на двойном послаблении и молчит на одинарном", () => {
  it("настоящий дефект — файл в ОБЕИХ ведомостях — находка, названная по имени", () => {
    seed(["demo/src/lib/api-client.ts", "demo/src/lib/live-fork.ts"], [
      "demo/src/lib/api-client.ts",
      "demo/src/lib/dead-but-not-a-fork.ts",
    ]);
    const census = doubleExcused(root);
    expect(census.excusedTwice).toEqual(["demo/src/lib/api-client.ts"]);
  });

  it("законный близнец: прощённый ОДНОЙ ведомостью молчит — обеими формами сразу", () => {
    // Двусторонний контроль. Без него отрицание зеленело бы и на предикате,
    // который просто перестал читать одну из ведомостей: «ноль находок»
    // получилось бы из пустого множества, а не из отсутствия пересечения.
    seed(["demo/src/lib/live-fork.ts"], ["demo/src/lib/dead-but-not-a-fork.ts"]);
    const census = doubleExcused(root);
    expect(census.excusedTwice).toEqual([]);
    expect(census.excusedForks).toEqual(["demo/src/lib/live-fork.ts"]);
    expect(census.excusedDead).toEqual(["demo/src/lib/dead-but-not-a-fork.ts"]);
  });

  it("перечень модулей ВЫВОДИТСЯ из ведомостей, а не выписан", () => {
    seed(["alpha/src/lib/a.ts"], ["beta/src/lib/b.ts"]);
    expect(doubleExcused(root).modules).toEqual(["alpha", "beta"]);
  });

  it("сдвинутая форма ключа — находка, а не тихое пустое пересечение", () => {
    // Тот же файл в обеих ведомостях, но одна написала ключ с «./». Пересечение
    // по строке пусто — и молчало бы, будь эта ветвь единственной защитой.
    seed(["./demo/src/lib/api-client.ts"], ["demo/src/lib/api-client.ts"]);
    const census = doubleExcused(root);
    expect(census.excusedTwice).toEqual([]);
    expect(census.malformed).toEqual(["./demo/src/lib/api-client.ts"]);
  });

  it("предпосылка ловит пустую ведомость: числа переписи нулевые", () => {
    seed([], []);
    const census = doubleExcused(root);
    expect(census.excusedForks).toEqual([]);
    expect(census.excusedDead).toEqual([]);
    // Суд требует эти числа НЕнулевыми — на таком входе он покраснеет
    // предпосылкой, а не тихо согласится с пустым пересечением.
    expect(census.excusedTwice).toEqual([]);
  });
});
