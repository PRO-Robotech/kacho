import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Гейт единого источника — ОКРУЖЕНИЕ ПРОБ (issue #418).
 *
 * Брат `shared-organisms-single-source.test.ts`: тот стережёт реализацию
 * компонентов, этот — среду, в которой пробы вообще исполняются. Предмет тот же
 * (копия расходится молча), но цена расхождения выше и тише: разошедшееся
 * окружение отнимает не правку, а ЦЕЛЫЙ КЛАСС ПРОБ.
 *
 * ЧЕМ ЭТО СТОИЛО. `ResourceTable` меряет собственное тело наблюдателем
 * размеров, которого у jsdom нет. Четыре модуля несли свою копию `setup.ts`, и
 * у compute со storage заглушки в ней не было — значит смонтировать список в
 * пробе было НЕЛЬЗЯ. Перепись на момент заведения: vpc 1, iam 2, system 1,
 * nlb 1, compute 0, storage 0, registry 0. Двоение раздела compute (#416)
 * дожило до находки владельца именно поэтому, а соседняя проба маршрутов была
 * вынуждена носить свою локальную заглушку и оговаривать, что правкой общего
 * окружения она не является.
 *
 * ПРАВИЛО. Приложение федерации подключает окружение проб из `shared/`, а не
 * своё. Исключение живёт в ведомости ниже, несёт причину и САМОИСТЕКАЕТ:
 * приложение, которое уже подключено к общему окружению, но всё ещё названо
 * исключением, роняет гейт — послабление, пережившее свой предмет, есть тот же
 * класс, что мы ловим в коде.
 *
 * ПЕРЕПИСЬ. Первая проба называет объём осмотренного и требует его ненулевым:
 * «расхождений не найдено» обязано быть отличимо от «ни одного файла настроек
 * не прочитано» — переименованная раскладка иначе делает всё ниже вакуумно
 * истинным.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Канонический путь окружения проб, считая от каталога приложения. */
const SHARED_SETUP = "<rootDir>/../shared/src/test/setup.ts";

/**
 * Приложения, которым пока позволено своё окружение проб.
 *
 * Обе записи — про сборку ОБРАЗА, а не про пробы: у оболочки и витрины свой
 * корень сборки, и их окружение обсуждается вместе с ним. Запись снимается,
 * когда приложение подключат к общему окружению; пока она есть, гейт требует,
 * чтобы она была ЖИВОЙ (см. пробу самоистечения).
 */
const EXCUSED: ReadonlyMap<string, string> = new Map([
  ["host", "Оболочка: свой корень сборки образа, окружение проб обсуждается вместе с ним."],
  ["dashboard", "Витрина: свой корень сборки образа, окружение проб обсуждается вместе с ним."],
]);

interface AppConfig {
  app: string;
  setup: string | null;
  hasOwnSetupFile: boolean;
}

function readApps(): AppConfig[] {
  const out: AppConfig[] = [];
  for (const entry of readdirSync(uiRoot).sort()) {
    const dir = path.join(uiRoot, entry);
    if (entry === "node_modules" || !statSync(dir).isDirectory()) continue;
    const cfg = path.join(dir, "jest.config.cjs");
    if (!existsSync(cfg)) continue;
    const txt = readFileSync(cfg, "utf8");
    const m = /setupFilesAfterEnv:\s*\[\s*"([^"]+)"/.exec(txt);
    out.push({
      app: entry,
      setup: m ? m[1] : null,
      hasOwnSetupFile: existsSync(path.join(dir, "src/test/setup.ts")),
    });
  }
  return out;
}

const APPS = readApps();
const SUBJECT = APPS.filter((a) => a.app !== "shared");

describe("единый источник: окружение проб живёт в shared/", () => {
  it(`перепись: файлов настроек прочитано ${APPS.length}, из них судимых ${SUBJECT.length}`, () => {
    expect(APPS.length).toBeGreaterThan(0);
    expect(SUBJECT.length).toBeGreaterThan(0);
    // Известные приложения закреплены: регрессия обнаружения не должна сузить
    // обход незаметно — «нашли 0 расхождений в 0 приложениях» это не вердикт.
    expect(SUBJECT.map((a) => a.app)).toEqual(
      expect.arrayContaining(["compute", "iam", "nlb", "registry", "storage", "vpc"]),
    );
  });

  it("своя предпосылка: общее окружение существует и несёт заглушку наблюдателя размеров", () => {
    // Запрет обоснован фактом о дереве: общее окружение закрывает пробел jsdom.
    // Перестанет закрывать — это всплывёт здесь, а не превратит гейт в no-op,
    // оставив приложения подключёнными к среде, которая список не монтирует.
    const setup = path.join(uiRoot, "shared/src/test/setup.ts");
    expect(existsSync(setup)).toBe(true);
    expect(readFileSync(setup, "utf8")).toContain("ResizeObserver");
  });

  it("каждое приложение подключает окружение из shared/", () => {
    // Координаты, а не счёт: красный гейт обязан сказать ГДЕ.
    const findings = SUBJECT.filter((a) => !EXCUSED.has(a.app) && a.setup !== SHARED_SETUP).map(
      (a) => `${a.app}/jest.config.cjs: ${a.setup ?? "(setupFilesAfterEnv не объявлен)"}`,
    );
    expect(findings).toEqual([]);
  });

  it("копии окружения в приложении не остаётся", () => {
    // Остаток каталога — форк в засаде: файл лежит рядом, ничего не подключает,
    // и следующая правка сядет в него.
    const stray = SUBJECT.filter((a) => !EXCUSED.has(a.app) && a.hasOwnSetupFile).map(
      (a) => `${a.app}/src/test/setup.ts`,
    );
    expect(stray).toEqual([]);
  });

  it("ведомость исключений самоистекает", () => {
    const stale: string[] = [];
    for (const [app, reason] of EXCUSED) {
      const found = SUBJECT.find((a) => a.app === app);
      if (!found) {
        stale.push(`${app}: приложения больше нет — снять запись`);
        continue;
      }
      if (!reason.trim()) stale.push(`${app}: причина не написана`);
      if (found.setup === SHARED_SETUP) {
        stale.push(`${app}: уже подключено к общему окружению — снять запись`);
      }
    }
    expect(stale).toEqual([]);
  });
});
