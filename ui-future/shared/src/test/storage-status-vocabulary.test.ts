import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Гейт: значок состояния знает КАЖДОЕ состояние, которое ствол может произвести.
 *
 * # Класс
 *
 * `StatusBadge` раскрашивает состояние по таблице соответствий и на незнакомое
 * значение молча падает в нейтральный тон. Это не «некрасиво», а неверно:
 * состояние, которого таблица не знает, выглядит НЕАКТИВНЫМ — тем же тоном, что
 * «остановлен» и «освобождён». Тот же класс уже ловили на `AVAILABLE`: доступный
 * том рисовался неактивным, и по значку это было неотличимо от сломанного.
 *
 * Именно поэтому находка тихая: ошибки нет, ячейка заполнена, вердикта нет ни у
 * одного теста.
 *
 * # Что утверждается
 *
 * Источник истины — САМ контракт: значения `enum Status` в
 * `proto/kacho/cloud/storage/v1/{volume,snapshot,image}.proto`. Каждое
 * не-`UNSPECIFIED` значение обязано стоять в таблице тонов каждого приложения,
 * чей реестр объявляет колонку `format: "status"` у ресурса storage.
 *
 * Область берётся ПО ФАКТУ, а не списком: приложение, которое заведёт такую
 * колонку завтра, попадает под сверку само. Приложение без такой колонки под
 * сверку НЕ попадает намеренно — требовать от `nlb` знать состояния тома значило
 * бы требовать словарь чужого домена (LEAN).
 *
 * # Чего гейт НЕ видит — названо, а не умолчано
 *
 * Состояние, отрисованное МИМО реестра (значок, вставленный прямо в доменную
 * вкладку), в область не попадает: реестр — единственное, что здесь читается.
 * Замер на момент заведения: таких мест ноль (`InstanceDisksTab` и
 * `InstanceDetailPage` состояния тома не рисуют).
 *
 * # Объём осмотренного и собственная предпосылка
 *
 * Числа прочитанных файлов и извлечённых значений утверждаются непустыми, а
 * извлекатель проверяется в обе стороны: он обязан найти известное состояние и
 * обязан НЕ находить выдуманного. Иначе «все состояния известны» означало бы
 * лишь, что из proto не извлечено ничего.
 */

const consoleRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);
const repoRoot = path.resolve(consoleRoot, "..");
const storageProtoDir = path.join(repoRoot, "proto/kacho/cloud/storage/v1");

/** Ресурсы storage, чьи состояния показывает консоль, и файл контракта каждого. */
const STORAGE_RESOURCES = [
  { apiPath: "/storage/v1/volumes", proto: "volume.proto" },
  { apiPath: "/storage/v1/snapshots", proto: "snapshot.proto" },
  { apiPath: "/storage/v1/images", proto: "image.proto" },
] as const;

/** Значения `enum Status { … }` из файла контракта, без `*_UNSPECIFIED`. */
export function contractStatuses(protoSource: string): string[] {
  const block = /enum Status \{([\s\S]*?)\n\s*\}/.exec(protoSource);
  if (block === null) return [];
  // Читается ОБЪЯВЛЕНИЕ значения (`ИМЯ = N;`), а не всякое слово капсом: имена
  // состояний встречаются и в прозе комментариев рядом.
  return [...block[1].matchAll(/^\s*([A-Z][A-Z0-9_]*)\s*=\s*\d+\s*;/gm)]
    .map((m) => m[1])
    .filter((name) => !name.endsWith("_UNSPECIFIED"));
}

/** Ключи таблицы тонов `TONE_BY_STATUS` одного `StatusBadge`. */
export function toneTableKeys(badgeSource: string): string[] {
  const block = /TONE_BY_STATUS[^=]*=\s*\{([\s\S]*?)\n\};/.exec(badgeSource);
  if (block === null) return [];
  return [...block[1].matchAll(/^\s*([A-Z][A-Z0-9_]*)\s*:/gm)].map((m) => m[1]);
}

/** Приложения дерева (каталог верхнего уровня с `src/`). */
function apps(): string[] {
  const NOT_APPS = new Set([
    "node_modules",
    "deploy",
    "docs",
    "scripts",
    ".git",
    "e2e",
  ]);
  return readdirSync(consoleRoot)
    .filter((n) => !NOT_APPS.has(n))
    .filter((n) => {
      const dir = path.join(consoleRoot, n);
      if (!statSync(dir).isDirectory()) return false;
      try {
        return statSync(path.join(dir, "src")).isDirectory();
      } catch {
        return false;
      }
    })
    .sort();
}

/** Объявляет ли реестр приложения колонку состояния у ресурса storage. */
function storageStatusColumns(registrySource: string): string[] {
  const out: string[] = [];
  for (const res of STORAGE_RESOURCES) {
    const at = registrySource.indexOf(`apiPath: "${res.apiPath}"`);
    if (at === -1) continue;
    // Ищем в пределах спеки: до следующего `apiPath:` либо до конца файла.
    const next = registrySource.indexOf('apiPath: "', at + 10);
    const body = registrySource.slice(
      at,
      next === -1 ? registrySource.length : next,
    );
    if (
      /path:\s*"status"[\s\S]{0,80}?format:\s*"status"|format:\s*"status"[\s\S]{0,80}?path:\s*"status"/.test(
        body,
      )
    ) {
      out.push(res.apiPath);
    }
  }
  return out;
}

const CONTRACT = new Map<string, string[]>(
  STORAGE_RESOURCES.map((r) => [
    r.apiPath,
    contractStatuses(readFileSync(path.join(storageProtoDir, r.proto), "utf8")),
  ]),
);

/** Содержимое файла либо `null`, если его нет. */
function readIfPresent(file: string): string | null {
  try {
    return readFileSync(file, "utf8");
  } catch {
    return null;
  }
}

/** Приложение → (ресурсы storage со значком, ключи его таблицы тонов). */
const SUBJECTS = apps()
  .map((app) => {
    const registry = path.join(
      consoleRoot,
      app,
      "src/lib/resource-registry.tsx",
    );
    // Читается ТОТ значок, который приложение реально рисует, а не файл, который
    // оно случайно держит. Модуль, сведённый к общей реализации (#405), несёт
    // вместо копии прослойку `export * from "@shared/…"` — таблица тонов у него
    // ОБЩАЯ, и требовать от него собственной значило бы требовать вернуть форк:
    // гейт судил бы раскладку файлов вместо словаря, который видит оператор.
    const ownBadge = path.join(
      consoleRoot,
      app,
      "src/components/atoms/StatusBadge/StatusBadge.tsx",
    );
    const sharedBadge = path.join(
      consoleRoot,
      "shared/src/components/atoms/StatusBadge/StatusBadge.tsx",
    );
    const barrel =
      readIfPresent(
        path.join(consoleRoot, app, "src/components/atoms/StatusBadge/index.ts"),
      ) ?? "";
    const delegates = /@shared\/components\/atoms\/StatusBadge/.test(barrel);
    const badge = delegates ? sharedBadge : ownBadge;
    // Реестра нет — приложение не описывает ресурсы и под сверку не попадает.
    const registrySource = readIfPresent(registry);
    if (registrySource === null) return null;
    // Значка нет — таблица тонов пуста, и утверждение ниже это ПОКАЖЕТ: пустой
    // словарь при живой колонке состояния и есть находка, а не повод промолчать.
    const badgeSource = readIfPresent(badge) ?? "";
    const resources = storageStatusColumns(registrySource);
    if (resources.length === 0) return null;
    return { app, badge, resources, tones: toneTableKeys(badgeSource) };
  })
  .filter((s): s is NonNullable<typeof s> => s !== null);

describe("значок состояния знает каждое состояние контракта storage", () => {
  it(`объём осмотренного: контракт прочитан, носители значка найдены (subjects=${SUBJECTS.length})`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного».
    expect(SUBJECTS.length).toBeGreaterThan(0);
    for (const [apiPath, statuses] of CONTRACT) {
      expect({ apiPath, count: statuses.length > 0 }).toEqual({
        apiPath,
        count: true,
      });
    }
    for (const s of SUBJECTS)
      expect({ app: s.app, tones: s.tones.length > 0 }).toEqual({
        app: s.app,
        tones: true,
      });
    // Известные носители пинятся, чтобы регрессия обхода не сузила охват молча.
    expect(SUBJECTS.map((s) => s.app)).toEqual(
      expect.arrayContaining(["shared", "storage"]),
    );
  });

  it("собственная предпосылка: извлекатели читают объявления, а не прозу", () => {
    const volume = CONTRACT.get("/storage/v1/volumes") ?? [];
    // (а) известное значение обязано найтись — и новое из этого контракта тоже.
    expect(volume).toEqual(
      expect.arrayContaining([
        "CREATING",
        "AVAILABLE",
        "IN_USE",
        "DELETING",
        "ERROR",
        "MIGRATING",
      ]),
    );
    // (б) выдуманное — не найтись, иначе сверка согласна на всё.
    expect(volume).not.toContain("TELEPORTING");
    // `*_UNSPECIFIED` отсеян намеренно: значка у «состояние не названо» нет.
    expect(volume.some((v) => v.endsWith("_UNSPECIFIED"))).toBe(false);
    // Извлекатель таблицы тонов обязан отличать ключ от слова в комментарии.
    expect(
      toneTableKeys(
        'const TONE_BY_STATUS: X = {\n  ACTIVE: "ok",\n  // ERROR упоминается тут\n};',
      ),
    ).toEqual(["ACTIVE"]);
  });

  it.each(SUBJECTS.map((s) => [s.app, s] as const))(
    "%s — ни одного состояния мимо таблицы тонов",
    (app, subject) => {
      const missing: string[] = [];
      for (const apiPath of subject.resources) {
        for (const status of CONTRACT.get(apiPath) ?? []) {
          if (!subject.tones.includes(status))
            missing.push(`${apiPath} → ${status}`);
        }
      }
      // Координаты, а не счётчик: красный гейт обязан сказать, ЧЕГО не хватает.
      expect({ app, missing }).toEqual({ app, missing: [] });
    },
  );
});
