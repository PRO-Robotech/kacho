import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

import { discoverApps, entryPoints, walkApp } from "./module-reachability";

/**
 * Способность гейта достижимости УПАСТЬ — и промолчать там, где падать не на чем.
 *
 * Гейт над настоящим деревом сегодня зелёный (недостижимое внесено в ведомость),
 * поэтому его собственная работоспособность из его зелени не следует НИКАК.
 * Здесь она доказывается инъекцией на синтетическом дереве: настоящий вход,
 * настоящий импорт, настоящий сирота — и законный близнец рядом, иначе гейт
 * ловил бы форму, а не существо.
 */

let root: string;

/** Кладёт файл, создавая каталоги. */
function put(rel: string, body: string): void {
  const full = path.join(root, rel);
  mkdirSync(path.dirname(full), { recursive: true });
  writeFileSync(full, body, "utf8");
}

beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), "kacho-reach-"));
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

/** Приложение с одним входом, одним живым модулем и одним сиротой. */
function seedApp(): void {
  put(
    "demo/vite.config.ts",
    [
      "export default {",
      "  plugins: [",
      "    federation({",
      "      exposes: {",
      '        "./DemoPage": "./src/pages/DemoPage/index.ts",',
      '        "./navigation": "./src/navigation.ts",',
      "      },",
      "    }),",
      "  ],",
      "};",
      "",
    ].join("\n"),
  );
  put("demo/src/main.tsx", 'import { DemoPage } from "@/pages/DemoPage";\nexport default DemoPage;\n');
  put("demo/src/navigation.ts", "export const nav = [];\n");
  put("demo/src/pages/DemoPage/index.ts", 'export * from "./DemoPage";\n');
  put("demo/src/pages/DemoPage/DemoPage.tsx", 'import { helper } from "@/lib/helper";\nexport const DemoPage = helper;\n');
  put("demo/src/lib/helper.ts", "export const helper = 1;\n");
}

describe("гейт достижимости способен упасть", () => {
  it("сирота, до которого нет пути от входа, объявляется находкой", () => {
    seedApp();
    put("demo/src/pages/OrphanPage.tsx", "export const OrphanPage = () => null;\n");

    const walk = walkApp(root, "demo");

    expect(walk.unreachable).toEqual(["src/pages/OrphanPage.tsx"]);
  });

  it("законный близнец той же формы находкой НЕ объявляется", () => {
    // Тот же файл, в том же каталоге, с тем же именем — но на него есть ссылка
    // из достижимого модуля. Без этой половины гейт ловил бы «файл лежит в
    // pages/», а не «пути от входа нет».
    seedApp();
    put("demo/src/pages/OrphanPage.tsx", "export const OrphanPage = () => null;\n");
    put(
      "demo/src/pages/DemoPage/DemoPage.tsx",
      'import { helper } from "@/lib/helper";\nimport { OrphanPage } from "@/pages/OrphanPage";\n' +
        "export const DemoPage = [helper, OrphanPage];\n",
    );

    const walk = walkApp(root, "demo");

    expect(walk.unreachable).toEqual([]);
    expect(walk.reachable).toContain("src/pages/OrphanPage.tsx");
  });

  it("ссылка вида `from \".\"` считается ребром, а не отсутствием ребра", () => {
    // Регрессия на РЕАЛЬНЫЙ промах этого обходчика: голый `.` — импорт индекса
    // своего каталога, и без его разбора обходчик объявлял бы мёртвым модуль,
    // который импортируют. Ошибка в эту сторону — ложная находка, а первый же
    // ложный срабат отключает гейт.
    seedApp();
    put("demo/src/widget/index.ts", 'export * from "./Widget";\n');
    put("demo/src/widget/Widget.tsx", "export const Widget = () => null;\n");
    put("demo/src/widget/use-widget.ts", 'import { Widget } from ".";\nexport const useWidget = () => Widget;\n');
    put(
      "demo/src/pages/DemoPage/DemoPage.tsx",
      'import { helper } from "@/lib/helper";\nimport { useWidget } from "@/widget/use-widget";\n' +
        "export const DemoPage = [helper, useWidget];\n",
    );

    const walk = walkApp(root, "demo");

    expect(walk.unreachable).toEqual([]);
    expect(walk.reachable).toContain("src/widget/Widget.tsx");
  });

  it("файл, который лишь ВЫГЛЯДИТ входом, входом не считается", () => {
    // `src/App.tsx` — не вход. Ровно на этом допущении половина недостижимого
    // пряталась годами: пока App.tsx считали входом, за ним скрывались модули,
    // до которых от настоящих входов пути нет.
    seedApp();
    put("demo/src/App.tsx", 'import { hidden } from "@/lib/hidden";\nexport const App = hidden;\n');
    put("demo/src/lib/hidden.ts", "export const hidden = 2;\n");

    const walk = walkApp(root, "demo");

    expect(walk.entries).not.toContain("src/App.tsx");
    expect(walk.unreachable).toEqual(["src/App.tsx", "src/lib/hidden.ts"]);
  });

  it("пробы модулем не считаются — их предмет судится своим прогоном", () => {
    seedApp();
    put("demo/src/lib/helper.test.ts", 'import { helper } from "./helper";\nit("x", () => expect(helper).toBe(1));\n');
    put("demo/src/test/setup.ts", "export const setup = 1;\n");

    const walk = walkApp(root, "demo");

    expect(walk.modules).not.toContain("src/lib/helper.test.ts");
    expect(walk.modules).not.toContain("src/test/setup.ts");
  });
});

describe("предпосылка обходчика проверяема, а не подразумевается", () => {
  it("приложение без входов даёт ноль входов — и это видно вызывающему", () => {
    // Гейт на этом падает своим сообщением: с нулём входов недостижимым
    // оказывается ВСЁ, и находки перестают что-либо значить.
    put("demo/src/lib/thing.ts", "export const thing = 1;\n");

    expect(entryPoints(root, "demo")).toEqual([]);
  });

  it("приложения выводятся из дерева, а не из выписанного перечня", () => {
    seedApp();
    put("later/src/main.tsx", "export default 1;\n");

    expect(discoverApps(root)).toEqual(["demo", "later"]);
  });

  it("библиотека shared приложением не считается", () => {
    seedApp();
    put("shared/src/lib/thing.ts", "export const thing = 1;\n");

    expect(discoverApps(root)).not.toContain("shared");
  });
});
