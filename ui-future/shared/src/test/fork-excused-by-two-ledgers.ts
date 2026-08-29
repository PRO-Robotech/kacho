// Стык ДВУХ ведомостей послаблений дерева консоли — читается один раз и двумя
// потребителями: судом (`fork-excused-by-two-ledgers.test.ts`) и доказательством
// его способности упасть (`…injection.test.ts`). Оба обязаны видеть ОДНО И ТО
// ЖЕ, иначе инъекция удостоверяет не тот предикат, которым судят дерево.
//
// Предмет и его цена описаны в шапке суда; здесь только чтение и пересечение.

import { readFileSync } from "node:fs";
import path from "node:path";

export interface ForkLedger {
  groups: { id: string; entries: { file: string }[] }[];
}
export interface ReachabilityLedger {
  allowed: Record<string, string>;
}

export interface DoubleExcuse {
  /** Копии общего кода, прощённые ведомостью форков. */
  excusedForks: string[];
  /** Мёртвые файлы, прощённые ведомостью достижимости. */
  excusedDead: string[];
  /** Модули, о которых высказывается хоть одна ведомость — ВЫВЕДЕНЫ, не выписаны. */
  modules: string[];
  /** Ключи, не похожие на путь от корня дерева: по ним пересечение молчало бы
   *  при живом классе. */
  malformed: string[];
  /** Файлы, прощённые ОБЕИМИ ведомостями сразу. */
  excusedTwice: string[];
  /** Групп в ведомости форков — перепись предпосылки. */
  forkGroups: number;
}

/** Ключ ведомости — путь от корня дерева консоли: `<приложение>/src/…`. */
const LEDGER_KEY = /^[a-z][\w-]*\/src\//;

/**
 * Ядро правила — ЧИСТОЕ: принимает разобранные ведомости, файловой системы не
 * касается. Так доказательство способности упасть обходится без синтетического
 * дерева на диске, а значит и без записи, переживающей границу суиты.
 */
export function doubleExcusedFrom(forks: ForkLedger, reachability: ReachabilityLedger): DoubleExcuse {
  const excusedForks = forks.groups.flatMap((g) => g.entries.map((e) => e.file));
  const excusedDead = Object.keys(reachability.allowed);
  const deadSet = new Set(excusedDead);

  return {
    excusedForks,
    excusedDead,
    modules: [
      ...new Set([...excusedForks, ...excusedDead].map((f) => f.split("/")[0])),
    ].sort(),
    malformed: [...excusedForks, ...excusedDead].filter((f) => !LEDGER_KEY.test(f)),
    excusedTwice: [...new Set(excusedForks.filter((f) => deadSet.has(f)))].sort(),
    forkGroups: forks.groups.length,
  };
}

/** То же правило над ведомостями РЕАЛЬНОГО дерева. */
export function doubleExcused(repoRoot: string): DoubleExcuse {
  const read = <T,>(rel: string): T => JSON.parse(readFileSync(path.join(repoRoot, rel), "utf8")) as T;
  return doubleExcusedFrom(
    read<ForkLedger>("shared/src/test/shared-fork-ledger.json"),
    read<ReachabilityLedger>("shared/src/test/module-reachability-ledger.json"),
  );
}
