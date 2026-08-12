// Точка входа федеративного модуля (`remoteEntry.js`) — единственный ассет
// консоли, чьё имя НЕ меняется от сборки к сборке: все остальные несут хэш
// содержимого. Поэтому общее правило «js — на год, immutable» на неё
// распространяться не вправе: `immutable` означает «не перепроверяй никогда»,
// и обновление страницы его не сбрасывает.
//
// Наблюдалось живьём (2026-08-12): выкаченная правка не доходила до владельца,
// хотя под работал с новым образом и край отдавал новые чанки — браузер держал
// прежнюю точку входа и грузил по ней прежние куски. Запечённый в образ конфиг
// это правило НЕС (и объяснял причину комментарием), а шаблон чарта — нет; в
// кластере действует именно шаблон, поэтому починка лежала в месте, которым
// стенд не пользуется.
//
// Проба читает ОБЪЯВЛЕНИЕ (шаблон чарта), а не отрендеренный манифест: рендер
// требует helm и в этом харнессе недоступен, а объявление — то самое место, где
// правило либо есть, либо его нет.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const UI_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const CHART = path.join(UI_ROOT, "deploy/templates/configmap-nginx.yaml");

/** Блоки `data:` каждого ConfigMap — по одному на приложение консоли. */
function serverBlocks(): { name: string; body: string }[] {
  const raw = readFileSync(CHART, "utf8");
  return raw
    .split(/^---$/m)
    .map((doc) => {
      const name = /name: (.+)-nginx/.exec(doc)?.[1]?.trim() ?? "";
      return { name, body: doc };
    })
    .filter((d) => d.name !== "");
}

/** Приложения, отдающие СВОЙ `remoteEntry.js` (host — оболочка, у неё его нет). */
const REMOTES = ["iamName", "systemName", "dashboardName", "vpcName", "nlbName", "registryName", "computeName", "storageName"];

describe("чарт консоли — точка входа модуля обязана перепроверяться", () => {
  it("перепись: шаблон прочитан и в нём найден блок каждого приложения", () => {
    // «Ноль нарушений» обязано быть отличимо от «ноль прочитанного»: переехавший
    // файл или переименованный helper иначе дал бы пустой обход и зелёный вердикт.
    const blocks = serverBlocks();
    expect(blocks.length).toBeGreaterThanOrEqual(REMOTES.length + 1); // + оболочка
    for (const r of REMOTES) {
      expect(blocks.some((b) => b.name.includes(r))).toBe(true);
    }
  });

  it("у каждого модуля remoteEntry.js объявлен ДО общего правила для js", () => {
    // nginx выбирает ПЕРВЫЙ совпавший regex-location, поэтому важно не только
    // наличие правила, но и его место: стоя после общего, оно не сработает ни разу.
    for (const r of REMOTES) {
      const block = serverBlocks().find((b) => b.name.includes(r));
      expect(block).toBeDefined();
      const body = block!.body;

      const entryAt = body.indexOf("location ~* /remoteEntry");
      const genericAt = body.search(/location ~\*\s+\\\.\(\?:js\|/);

      // Имя приложения — в самом утверждении, чтобы падение называло виновника.
      expect({ app: r, hasEntryRule: entryAt > -1 }).toEqual({ app: r, hasEntryRule: true });
      expect({ app: r, hasGenericRule: genericAt > -1 }).toEqual({ app: r, hasGenericRule: true });
      expect({ app: r, entryBeforeGeneric: entryAt < genericAt }).toEqual({ app: r, entryBeforeGeneric: true });
    }
  });

  it("правило remoteEntry требует перепроверки, а общее правило остаётся immutable", () => {
    // Положительный контроль рядом с отрицанием: без него проба зеленела бы и на
    // чарте, где immutable снят у ВСЕХ ассетов — то есть на потерянном кэшировании.
    for (const r of REMOTES) {
      const body = serverBlocks().find((b) => b.name.includes(r))!.body;
      const entryRule = body.slice(
        body.indexOf("location ~* /remoteEntry"),
        body.search(/location ~\*\s+\\\.\(\?:js\|/),
      );

      expect({ app: r, revalidates: /no-cache/.test(entryRule) }).toEqual({ app: r, revalidates: true });
      expect({ app: r, hashedStayImmutable: /public, immutable/.test(body) }).toEqual({
        app: r,
        hashedStayImmutable: true,
      });
    }
  });
});
