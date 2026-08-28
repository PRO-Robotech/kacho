import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { sweepCopies } from "./cross-module-copy-sweep";

/**
 * Способность третьего признака УПАСТЬ — и промолчать там, где падать не на чем.
 *
 * Гейт над настоящим деревом зелён ровно настолько, насколько заполнена его
 * ведомость, поэтому его работоспособность из его зелени не следует НИКАК.
 * Здесь она доказывается инъекцией на синтетическом дереве: настоящий обходчик,
 * настоящая копия — и рядом ЧЕТЫРЕ законных близнеца, каждый по своей оси, иначе
 * признак ловил бы форму, а не существо.
 *
 * Оси близнецов названы поимённо, потому что каждая — отдельный способ ошибиться:
 * файл в одном экземпляре (сравнивать не с чем); файл, у которого дом в общем
 * ЕСТЬ (это предмет соседнего гейта, а не этого); прослойка (разрешённая форма);
 * два файла одного пути, разошедшихся содержимым (это N разных вещей, а не одна
 * в N местах).
 */

let root: string;

function put(rel: string, body: string): void {
  const full = path.join(root, rel);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, body, "utf8");
}

/** Каркас: два приложения и общий модуль — иначе обходить нечего. */
function seed(): void {
  put("shared/src/lib/known.ts", "export const known = 1;\n");
  put("alpha/src/main.tsx", "export const main = 1;\n");
  put("beta/src/main.tsx", "export const main = 2;\n");
}

beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), "kacho-copy-"));
  seed();
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

describe("третий признак способен упасть", () => {
  it("копия в двух приложениях без дома в общем — находка, названная адресом и составом", () => {
    put("alpha/src/lib/orphan.ts", "export const orphan = 1;\n");
    put("beta/src/lib/orphan.ts", "export const orphan = 1;\n");

    const sweep = sweepCopies(root);

    expect(sweep.hits.map((h) => `${h.rel} [${h.apps.join(",")}]`)).toEqual(["lib/orphan.ts [alpha,beta]"]);
  });

  it("копия, отличающаяся ТОЛЬКО алиасом модуля, остаётся находкой", () => {
    // Алиас модуля и алиас общего указывают на разные корни: копия от оригинала
    // отличается именно этим и ничем больше. Без нормализации признак был бы
    // слеп ко всякой копии, которая что-нибудь импортирует, — то есть почти ко
    // всякой.
    put("alpha/src/lib/orphan.ts", 'import { x } from "@' + '/lib/known";\nexport const orphan = x;\n');
    put("beta/src/lib/orphan.ts", 'import { x } from "@shared/lib/known";\nexport const orphan = x;\n');

    const sweep = sweepCopies(root);

    expect(sweep.hits.map((h) => h.rel)).toEqual(["lib/orphan.ts"]);
  });

  it("законный близнец: файл в ОДНОМ экземпляре находкой не объявляется", () => {
    put("alpha/src/lib/only-here.ts", "export const onlyHere = 1;\n");

    const sweep = sweepCopies(root);

    expect(sweep.hits).toEqual([]);
  });

  it("законный близнец: копия, у которой дом в общем ЕСТЬ, — предмет соседнего гейта, не этого", () => {
    put("alpha/src/lib/known.ts", "export const known = 1;\n");
    put("beta/src/lib/known.ts", "export const known = 1;\n");

    const sweep = sweepCopies(root);

    expect(sweep.hits).toEqual([]);
    // И это не «ничего не увидели»: пара по содержимому найдена, просто у неё
    // есть дом. Без этой половины проба зеленела бы и на сломанном обходчике.
    expect(sweep.pairedByContent).toBe(1);
    expect(sweep.withHome).toBe(1);
  });

  it("законный близнец: прослойка копией не является", () => {
    put("alpha/src/lib/shim.ts", 'export * from "@shared/lib/known";\n');
    put("beta/src/lib/shim.ts", 'export * from "@shared/lib/known";\n');

    const sweep = sweepCopies(root);

    expect(sweep.hits).toEqual([]);
  });

  it("законный близнец: один путь, РАЗНОЕ содержимое — не копия", () => {
    put("alpha/src/lib/divergent.ts", "export const divergent = 1;\n");
    put("beta/src/lib/divergent.ts", "export const divergent = 2;\n");

    const sweep = sweepCopies(root);

    expect(sweep.hits).toEqual([]);
  });

  it("перепись растёт вместе с обходом: пустое дерево не выдаёт себя за чистое", () => {
    const sweep = sweepCopies(root);

    expect(sweep.apps).toEqual(["alpha", "beta"]);
    expect(sweep.filesRead).toBe(2);
    expect(sweep.hits).toEqual([]);
  });
});
