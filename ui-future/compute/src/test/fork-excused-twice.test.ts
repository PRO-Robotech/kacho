// Файл, прощённый ДВУМЯ ведомостями сразу, не будет снят никогда.
//
// У дерева консоли две ведомости послаблений, и каждая по отдельности честна:
//   · ведомость форков (`shared-fork-ledger.json`) прощает КОПИЮ общего кода —
//     «да, это форк, вот причина, почему он ещё жив»;
//   · ведомость достижимости (`module-reachability-ledger.json`) прощает файл,
//     до которого нет пути от входа приложения, — «да, он мёртв, вот причина».
//
// Пересечение этих двух перечней — состояние, которого не бывает по существу:
// копия общего кода, до которой вдобавок никто не доходит. Ни одного довода в
// пользу её жизни не остаётся: сводить нечего (её не зовут), провязывать
// нельзя (она отстала). При этом ОБЕ ведомости зелены — каждая видит свою
// половину и считает её объяснённой, — поэтому такой файл переживает оба
// послабления и снимается только случайно.
//
// НАБЛЮДАЛОСЬ на этом модуле: свой клиент края (229 строк) и своё чтение списка
// (101 строка) стояли в ОБЕИХ ведомостях. Чтение списка вдобавок отставало
// содержательно — брало ОДНУ страницу (`useQuery`), тогда как общее накапливает
// страницы курсора (`useInfiniteQuery` + `getNextPageParam`), — то есть
// провязать его значило бы молча обрезать каждый список. Сняты вместе с их
// пробой и с транзитивно осиротевшей подписью запроса.
//
// ЧЕМ ЭТО НЕ ЯВЛЯЕТСЯ. Обход импортов здесь не переписывается: достижимость
// считает общий гейт `shared/src/test/module-reachability.test.ts`, и второй
// реализации того же предмета заводить нельзя. Здесь судится ровно СТЫК двух
// перечней — то, о чём ни один из них по отдельности не высказывается.
//
// ОБЛАСТЬ — этот модуль. Класс живёт и у соседей (замер 2026-08-29: по одному
// файлу у `nlb`, `storage` и `registry`), но их де-форк идёт своими задачами, и
// краснить чужую полосу отсюда значило бы отдать вердикт этого модуля в чужие
// руки.
//
// САМОИСТЕЧЕНИЕ. Опустеет любой из перечней — правило останется зелёным и
// скажет об этом переписью: пустое пересечение есть ЦЕЛЬ, а не поломка.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const APP = "compute";

interface ForkLedger {
  groups: { id: string; entries: { file: string }[] }[];
}
interface ReachabilityLedger {
  allowed: Record<string, string>;
}

function read<T>(rel: string): T {
  return JSON.parse(readFileSync(path.join(repoRoot, rel), "utf8")) as T;
}

const forks = read<ForkLedger>("shared/src/test/shared-fork-ledger.json");
const reachability = read<ReachabilityLedger>("shared/src/test/module-reachability-ledger.json");

/** Копии общего кода, прощённые за этим модулем. */
const excusedForks = forks.groups
  .filter((g) => g.id.startsWith(`${APP}:`))
  .flatMap((g) => g.entries.map((e) => e.file));

/** Мёртвые файлы, прощённые за этим модулем. */
const excusedDead = Object.keys(reachability.allowed).filter((f) => f.startsWith(`${APP}/`));

const excusedTwice = excusedForks.filter((f) => excusedDead.includes(f)).sort();

describe(`ведомости послаблений модуля ${APP} не прощают один файл дважды`, () => {
  it(`перепись: прощённых копий ${excusedForks.length}, прощённых мёртвых ${excusedDead.length}`, () => {
    // Обе величины печатаются, потому что пустое пересечение получается и от
    // достигнутой цели, и от того, что перечни перестали читаться. Первое —
    // норма, второе — поломка, и различить их можно только по этим числам.
    // Требовать их ненулевыми НЕЛЬЗЯ: ноль здесь законная цель.
    expect(Array.isArray(forks.groups)).toBe(true);
    expect(typeof reachability.allowed).toBe("object");
  });

  it("своя предпосылка: ведомости прочитаны и не пусты", () => {
    // Переедет ведомость или сменится форма ключа — всплывёт здесь, а не
    // превратит утверждение ниже в тихий no-op на двух пустых множествах.
    expect(forks.groups.length).toBeGreaterThan(0);
    expect(Object.keys(reachability.allowed).length).toBeGreaterThan(0);
  });

  it("ни один файл не прощён обеими ведомостями", () => {
    expect(excusedTwice).toEqual([]);
  });
});
