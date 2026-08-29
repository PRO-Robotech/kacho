import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Гейт единого прогона — СУИТА ОБЩЕГО МОДУЛЯ ИСПОЛНЯЕТСЯ ДОМЕНОМ (issue #1469).
 *
 * Третий брат к `module-test-setup-single-source` (среда проб) и
 * `shared-organisms-single-source` (реализация компонентов). Те стерегут, ЧЕМ
 * пользуется домен; этот — проверяет ли домен то, чем пользуется.
 *
 * ПРЕДМЕТ. У `ui-future/shared` собственного прогона нет: его исходники — часть
 * сборки каждого удалённого модуля, и его пробы исполняют модули-потребители.
 * Модуль, который берёт из общего полсотни прослоек, но суиту общего в свой
 * прогон не включает, о правках в нём НЕ УТВЕРЖДАЕТ НИЧЕГО — «зелёный registry»
 * означает «зелены пробы домена», а не «домен работает с тем общим кодом, что
 * лежит рядом».
 *
 * ЧЕМ ЭТО СТОИЛО. Замер при заведении гейта: registry исполнял 21 суиту
 * (110 проб); под тем же конфигом с корнем общего модуля — на порядок больше.
 * Разница пряталась в двух строках конфигурации, и по исходу `npm test` она не
 * видна: оба прогона печатают «все зелёные».
 *
 * ПОПУЛЯЦИЯ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Судится модуль, подключивший ОБЩУЮ
 * среду проб (`setupFilesAfterEnv` → `shared/src/test/setup.ts`). Оболочка и
 * витрина держат свою среду осознанно — это решение принято и записано в
 * ведомости соседнего гейта (`module-test-setup-single-source`), — и суиту
 * общего модуля под своей средой они исполнить НЕ МОГУТ: общая среда несёт
 * заглушку наблюдателя размеров, без которой список не монтируется вовсе.
 * Поэтому здесь они выпадают из предмета ПО ПРИЗНАКУ, а не по второму списку
 * имён: два места об одном предмете разошлись бы молча.
 *
 * ВЕДОМОСТЬ САМОИСТЕКАЕТ. Модуль, у которого свойства ещё нет, живёт в
 * ведомости с причиной и номером полосы, которая его закрывает. Модуль,
 * свойство уже несущий, но всё ещё названный исключением, РОНЯЕТ гейт:
 * послабление, пережившее свой предмет, есть тот же класс, что мы ловим в коде.
 * Пустая ведомость — цель, а не поломка: на ней гейт проходит.
 *
 * ГЕЙТ СУДИТ ОБЪЯВЛЕНИЕ РАЗОБРАННЫМ, а не подстрокой. Корень общего модуля
 * упоминается в `setupFilesAfterEnv`, в отображении модулей и в комментариях
 * этого самого файла — проверка по вхождению подстроки была бы зелёной у
 * КАЖДОГО модуля дерева, то есть не измеряла бы ничего.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Канонический путь общей среды проб, считая от каталога модуля. */
const SHARED_SETUP = "<rootDir>/../shared/src/test/setup.ts";

/** Признак того, что объявленный путь указывает в общий модуль. */
const SHARED_MARK = "../shared/src";

/**
 * Модули, которым суита общего модуля пока не включена.
 *
 * Каждая запись называет ПОЛОСУ, которая её снимет. Полосы идут параллельно, и
 * гейт, красный на чужом модуле, блокировал бы чужую работу вместо своей —
 * поэтому послабление, а не находка. Запись снимается тем же изменением,
 * которым полоса включает суиту: см. пробу самоистечения ниже.
 */
// Пусто — и это ЦЕЛЬ ведомости, а не её поломка. Последняя запись (compute,
// полоса #406) снята вместе со своим предметом: модуль включил суиту общего в
// `roots` и `testMatch`, и проба самоистечения ниже потребовала запись убрать.
// Она пережила свою полосу на одно сведение — ровно тот случай, ради которого
// самоистечение и заведено.
const EXCUSED: ReadonlyMap<string, string> = new Map([]);

interface ModuleVerdict {
  name: string;
  /** Модуль подключил ОБЩУЮ среду проб — значит суиту общего он исполнить может. */
  usesSharedSetup: boolean;
  /** `roots` называет корень общего модуля. */
  rootsShared: boolean;
  /** `testMatch` называет пробы общего модуля. */
  matchShared: boolean;
}

/** Значения строкового массива по имени поля — из РАЗОБРАННОГО объявления. */
function arrayField(code: string, key: string): string[] {
  const found = new RegExp(`\\b${key}\\s*:\\s*\\[([\\s\\S]*?)\\]`).exec(code);
  if (!found) return [];
  return [...found[1].matchAll(/"([^"]*)"/g)].map((x) => x[1]);
}

function readVerdict(name: string): ModuleVerdict {
  const raw = readFileSync(path.join(uiRoot, name, "jest.config.cjs"), "utf8");
  // Комментарии снимаем ДО разбора: и `roots`, и `testMatch`, и путь общего
  // модуля стоят в прозе соседних конфигураций.
  const code = raw.replace(/^\s*\/\/.*$/gm, "").replace(/\/\*[\s\S]*?\*\//g, "");
  const points = (value: string) => value.includes(SHARED_MARK);
  return {
    name,
    usesSharedSetup: arrayField(code, "setupFilesAfterEnv").includes(SHARED_SETUP),
    rootsShared: arrayField(code, "roots").some(points),
    matchShared: arrayField(code, "testMatch").some(points),
  };
}

/** Модули — по факту наличия `src/` и `jest.config.cjs`, а не по вписанному перечню. */
function modules(): string[] {
  return readdirSync(uiRoot)
    .filter((name) => name !== "node_modules" && name !== "shared")
    .filter((name) => {
      const dir = path.join(uiRoot, name);
      return (
        statSync(dir).isDirectory() &&
        existsSync(path.join(dir, "src")) &&
        existsSync(path.join(dir, "jest.config.cjs"))
      );
    })
    .sort();
}

/** Пробы общего модуля — то, ради чего требование вообще существует. */
function sharedProbeCount(dir: string): number {
  let total = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules") continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) total += sharedProbeCount(full);
    else if (/\.test\.tsx?$/.test(entry.name)) total += 1;
  }
  return total;
}

const ALL = modules().map(readVerdict);
/** Предмет — модули на ОБЩЕЙ среде проб; своя среда выводит модуль из предмета. */
const SUBJECT = ALL.filter((m) => m.usesSharedSetup);
const CARRIES = SUBJECT.filter((m) => m.rootsShared && m.matchShared);

describe("суита общего модуля исполняется прогоном каждого домена", () => {
  it(`перепись: модулей ${ALL.length} · в предмете ${SUBJECT.length} · несут свойство ${CARRIES.length} (${CARRIES.map((m) => m.name).join(", ") || "нет"}) · освобождено ${EXCUSED.size}`, () => {
    // Перепись печатается, а не только стоит в заголовке: заголовок виден лишь
    // под `--verbose`, а прогон домена идёт без него — «ноль находок» осталось
    // бы неотличимо от «ноль прочитанного» ровно там, где это и опасно.
    // eslint-disable-next-line no-console
    console.log(
      `[#1469] модулей: ${ALL.length}; в предмете (общая среда проб): ${SUBJECT.length}; ` +
        `несут суиту общего: ${CARRIES.length} (${CARRIES.map((m) => m.name).join(", ") || "нет"}); ` +
        `освобождено: ${EXCUSED.size} (${[...EXCUSED.keys()].join(", ") || "нет"}); ` +
        `проб в общем модуле: ${sharedProbeCount(path.join(uiRoot, "shared/src"))}`,
    );
    // Пустой обход делает всё нижеследующее вакуумно истинным: сдвинутый корень
    // или переименованный конфиг иначе превращают гейт в зелёное ни о чём.
    expect(ALL.length).toBeGreaterThan(0);
    expect(SUBJECT.length).toBeGreaterThan(0);
    // Контроль в обратную сторону: распознаватель обязан УМЕТЬ ответить «да».
    // Иначе он мог бы всегда отвечать «нет», и находка ниже была бы формой без
    // содержания.
    expect(CARRIES.length).toBeGreaterThan(0);
  });

  it("своя предпосылка: у общего модуля есть пробы и есть общая среда", () => {
    // Запрет обоснован фактом о дереве: в общем модуле ЕСТЬ что исполнять.
    // Опустеет общий модуль — требование станет бессмысленным, и это всплывёт
    // здесь, а не превратит гейт в обряд.
    const probes = sharedProbeCount(path.join(uiRoot, "shared/src"));
    expect(probes).toBeGreaterThan(0);
    expect(existsSync(path.join(uiRoot, "shared/src/test/setup.ts"))).toBe(true);
  });

  it("каждый модуль на общей среде исполняет суиту общего модуля", () => {
    // Координаты, а не счёт: красный гейт обязан сказать ГДЕ и ЧЕГО не хватает.
    const findings = SUBJECT.filter((m) => !EXCUSED.has(m.name))
      .filter((m) => !m.rootsShared || !m.matchShared)
      .map((m) => {
        const missing = [
          m.rootsShared ? null : "roots",
          m.matchShared ? null : "testMatch",
        ].filter(Boolean);
        return `${m.name}/jest.config.cjs: не называет корень общего модуля в ${missing.join(" и ")}`;
      });
    expect(findings).toEqual([]);
  });

  it("ведомость исключений самоистекает", () => {
    const stale: string[] = [];
    for (const [name, reason] of EXCUSED) {
      const found = ALL.find((m) => m.name === name);
      if (!found) {
        stale.push(`${name}: модуля больше нет — снять запись`);
        continue;
      }
      if (!reason.trim()) stale.push(`${name}: причина не написана`);
      if (!found.usesSharedSetup) {
        stale.push(`${name}: держит свою среду проб — из предмета выпал, снять запись`);
        continue;
      }
      if (found.rootsShared && found.matchShared) {
        stale.push(`${name}: суита общего модуля уже включена — снять запись`);
      }
    }
    expect(stale).toEqual([]);
  });
});
