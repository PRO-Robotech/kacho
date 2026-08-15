import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { REMOTE_MODULES, moduleLabelOf } from "./moduleCatalog";

/**
 * Гейт покрытия границами отказа (#371).
 *
 * Норма: у КАЖДОГО удалённого модуля федерации есть своя граница отказа и своё
 * ИМЯ на экране отказа. Одной границы на корне мало — она оставила бы
 * пользователя без имени отказавшего раздела; одного `Suspense` мало вовсе —
 * он ловит ожидание, а не отказ.
 *
 * Предмет гейта — не «файл существует», а СХОДИМОСТЬ трёх перечней, каждый из
 * которых выведен из дерева, а не выписан:
 *   1. remotes, объявленные федерацией в `vite.config.ts` (кого host грузит);
 *   2. обёртки `*Remote.tsx` (кто из них обёрнут);
 *   3. каталог имён `moduleCatalog.ts` (у кого есть имя для экрана отказа).
 * Расхождение в любую сторону — находка: новый remote, заведённый без границы,
 * покраснит этот гейт, а не тихо унесёт консоль в проде.
 *
 * Перепись печатается в имени пробы, поэтому «расхождений нет» отличимо от
 * «ничего не прочитано»: переехавший каталог упадёт на первой пробе.
 */

const here = path.dirname(fileURLToPath(import.meta.url));
const hostRoot = path.resolve(here, "../..");

/** Имена remote'ов, которые host грузит по сети, — из объявления федерации. */
function federationRemotes(): string[] {
  const src = readFileSync(path.join(hostRoot, "vite.config.ts"), "utf8");
  const block = src.match(/remotes:\s*\{([\s\S]*?)\n {6}\}/);
  if (!block) throw new Error("vite.config.ts: блок federation remotes не найден — гейт читает не то");
  return [...block[1].matchAll(/^\s{8}(\w+):/gm)].map((m) => m[1]).sort();
}

/** Обёртки удалённых страниц: файл → literal-спецификатор его import(). */
function remoteWrappers(): Map<string, { file: string; source: string }> {
  const dir = path.join(here);
  const out = new Map<string, { file: string; source: string }>();
  for (const file of readdirSync(dir)) {
    if (!/^[A-Z]\w*Remote\.tsx$/.test(file)) continue;
    const source = readFileSync(path.join(dir, file), "utf8");
    const spec = source.match(/import\("([^"/]+)\/[^"]+"\)/);
    if (!spec) throw new Error(`${file}: literal-спецификатор import("<remote>/<Page>") не найден`);
    out.set(spec[1], { file, source });
  }
  return out;
}

const FEDERATION = federationRemotes();
const WRAPPERS = remoteWrappers();

describe("границы отказа покрывают каждый удалённый модуль", () => {
  it(`перепись: federation=${FEDERATION.length}, обёрток=${WRAPPERS.size}, имён в каталоге=${REMOTE_MODULES.length}`, () => {
    expect(FEDERATION.length).toBeGreaterThan(0);
    expect(WRAPPERS.size).toBeGreaterThan(0);
    expect(REMOTE_MODULES.length).toBeGreaterThan(0);
  });

  it("у каждого объявленного remote есть обёртка", () => {
    // Координаты, а не число: красный гейт обязан назвать, кого не хватает.
    expect(FEDERATION.filter((remote) => !WRAPPERS.has(remote))).toEqual([]);
  });

  it("у каждого объявленного remote есть имя в каталоге", () => {
    const named = new Set(REMOTE_MODULES.map((m) => m.remote));
    expect(FEDERATION.filter((remote) => !named.has(remote))).toEqual([]);
  });

  it("каталог не называет того, кого федерация не грузит (исключению нечего исключать)", () => {
    expect(REMOTE_MODULES.map((m) => m.remote).filter((remote) => !FEDERATION.includes(remote))).toEqual([]);
  });

  it("каждая обёртка строится общей фабрикой и берёт имя СВОЕГО раздела из каталога", () => {
    const wrong: string[] = [];
    for (const [remote, { file, source }] of WRAPPERS) {
      // Собственная копия lazy()+Suspense — форк: правка makeRemote (в том числе
      // заведение границы отказа) до неё не доезжает. Так уже было у двух обёрток.
      if (!source.includes("makeRemote(")) wrong.push(`${file}: не построен makeRemote — форк фабрики`);
      // Имя берётся из каталога по СВОЕМУ ключу, а не копируется у соседа.
      if (!source.includes(`moduleLabelOf("${remote}")`)) {
        wrong.push(`${file}: ожидалось moduleLabelOf("${remote}") — имя раздела берётся из каталога`);
      }
    }
    expect(wrong).toEqual([]);
    // Собственная предпосылка: каталог действительно отдаёт имена, а не пустые строки.
    for (const remote of WRAPPERS.keys()) expect(moduleLabelOf(remote).length).toBeGreaterThan(0);
  });
});
