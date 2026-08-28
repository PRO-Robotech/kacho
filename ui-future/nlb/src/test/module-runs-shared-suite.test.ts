// Правка в общем модуле обязана проверяться прогоном ЭТОГО домена.
//
// `ui-future/shared` собственного прогона не имеет: его исходники — часть сборки
// каждого удалённого модуля, и его пробы исполняют модули-потребители. Модуль,
// который импортирует `@shared` полусотней прослоек, но суиту общего модуля в
// свой прогон не берёт, о правках в нём не утверждает НИЧЕГО — и «зелёный nlb»
// означает «зелены 146 проб домена», а не «домен работает с тем общим кодом,
// что лежит рядом».
//
// Наблюдалось на этом дереве: nlb исполнял 24 суиты (146 проб), тогда как под
// тем же конфигом с общим корнем — 262 суиты (2348 проб). Шестнадцатикратная
// разница пряталась в двух строках конфигурации, и заметить её по исходу
// `npm test` нельзя: оба прогона печатают «все зелёные».
//
// Гейт судит ОБЪЯВЛЕНИЕ (`jest.config.cjs`), а не рендер: конфигурация читается
// как текст модуля Node, поэтому проба не поднимает второй jest и не зависит от
// того, что именно сегодня лежит в общем модуле.
//
// ГРАНИЦА НАЗВАНА ЧЕСТНО. Утверждение здесь — про ОДИН модуль, тот, в чьём
// дереве проба лежит. Перепись ниже печатает состояние ВСЕХ модулей консоли,
// чтобы «ноль находок» было отличимо от «ноль прочитанного» и чтобы соседний
// домен, у которого свойство отсутствует, был виден, а не подразумевался.
// Превратить перепись в утверждение по всему дереву — предмет задачи #410:
// полосы compute/storage/registry идут параллельно, и гейт, красный на их
// модулях, блокировал бы их работу вместо своей.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Каталоги верхнего уровня, которые модулями консоли не являются. */
const NOT_MODULES = new Set(["node_modules", "shared", "deploy", "docs", "scripts", "e2e", ".git"]);

interface ModuleVerdict {
  name: string;
  /** `roots` называет корень общего модуля. */
  rootsShared: boolean;
  /** `testMatch` называет пробы общего модуля. */
  matchShared: boolean;
}

/** Модули — по факту наличия `src/` и `jest.config.cjs`, а не по вписанному перечню. */
function modules(): string[] {
  return readdirSync(uiRoot)
    .filter((n) => !NOT_MODULES.has(n))
    .filter((n) => {
      const dir = path.join(uiRoot, n);
      return (
        statSync(dir).isDirectory() &&
        existsSync(path.join(dir, "src")) &&
        existsSync(path.join(dir, "jest.config.cjs"))
      );
    })
    .sort();
}

/**
 * Признак — по РАЗОБРАННОМУ значению поля, а не по вхождению подстроки:
 * `../shared/src` стоит и в `setupFilesAfterEnv`, и в `moduleNameMapper`, и в
 * комментариях этого самого файла. Проверка по подстроке была бы зелёной у
 * КАЖДОГО модуля дерева — то есть не измеряла бы ничего.
 */
function readVerdict(name: string): ModuleVerdict {
  const src = readFileSync(path.join(uiRoot, name, "jest.config.cjs"), "utf8");
  const field = (key: string): string[] => {
    // Комментарии снимаем до разбора: `testMatch` упоминается в прозе.
    const code = src.replace(/^\s*\/\/.*$/gm, "").replace(/\/\*[\s\S]*?\*\//g, "");
    const m = new RegExp(`\\b${key}\\s*:\\s*\\[([\\s\\S]*?)\\]`).exec(code);
    if (!m) return [];
    return [...m[1].matchAll(/"([^"]*)"/g)].map((x) => x[1]);
  };
  const pointsAtShared = (v: string) => v.includes("../shared/src");
  return {
    name,
    rootsShared: field("roots").some(pointsAtShared),
    matchShared: field("testMatch").some(pointsAtShared),
  };
}

const ALL = modules().map(readVerdict);
const MINE = "nlb";
const running = ALL.filter((m) => m.rootsShared && m.matchShared).map((m) => m.name);

describe("суита общего модуля исполняется прогоном домена", () => {
  it(`перепись: модулей консоли ${ALL.length}, исполняют суиту общего модуля ${running.length} (${running.join(", ")})`, () => {
    // Пустой обход означал бы, что утверждение ниже вакуумно истинно: сдвинутый
    // корень или переименованный конфиг иначе делают гейт зелёным ни о чём.
    expect(ALL.length).toBeGreaterThan(0);
    expect(ALL.map((m) => m.name)).toContain(MINE);
    // Контроль в обратную сторону: распознаватель обязан УМЕТЬ ответить «да» —
    // иначе он мог бы всегда отвечать «нет», и находка ниже была бы формой без
    // содержания. Эталон (vpc) свойство несёт с волны 2026-08.
    expect(running.length).toBeGreaterThan(0);
  });

  it(`${MINE}: roots и testMatch называют корень общего модуля`, () => {
    const mine = ALL.find((m) => m.name === MINE);
    expect(mine).toEqual({ name: MINE, rootsShared: true, matchShared: true });
  });
});
