// Способность правила «прощён дважды» УПАСТЬ — и промолчать там, где падать не
// на чем.
//
// Над настоящим деревом правило сегодня зелёное (пересечение пусто — это его
// ЦЕЛЬ), поэтому его работоспособность из зелени не следует НИКАК. Здесь она
// доказывается инъекцией: настоящий двойной прощённый — находка; законный
// близнец (тот же файл, прощённый ОДНОЙ ведомостью) — молчание; сдвинутая форма
// ключа — находка отдельной ветвью, иначе пересечение молчало бы при живом
// классе.
//
// ФАЙЛОВОЙ СИСТЕМЫ ЗДЕСЬ НЕТ НАМЕРЕННО. Предмет правила — пересечение двух
// РАЗОБРАННЫХ перечней, а не обход дерева, поэтому синтетика подаётся объектами
// в памяти. Запись во временный каталог пережила бы границу суиты и сделала бы
// вердикт соседней пробы функцией порядка; здесь этой цены платить не за что.

import {
  doubleExcusedFrom,
  type ForkLedger,
  type ReachabilityLedger,
} from "./fork-excused-by-two-ledgers";

/** Ведомость форков из перечня файлов. */
function forkLedger(files: string[]): ForkLedger {
  return { groups: [{ id: "demo:lib", entries: files.map((file) => ({ file })) }] };
}

/** Ведомость достижимости из перечня файлов. */
function reachLedger(files: string[]): ReachabilityLedger {
  return { allowed: Object.fromEntries(files.map((f) => [f, "синтетика"])) };
}

describe("инъекция: правило падает на двойном послаблении и молчит на одинарном", () => {
  it("настоящий дефект — файл в ОБЕИХ ведомостях — находка, названная по имени", () => {
    const census = doubleExcusedFrom(
      forkLedger(["demo/src/lib/api-client.ts", "demo/src/lib/live-fork.ts"]),
      reachLedger(["demo/src/lib/api-client.ts", "demo/src/lib/dead-but-not-a-fork.ts"]),
    );
    expect(census.excusedTwice).toEqual(["demo/src/lib/api-client.ts"]);
  });

  it("законный близнец: прощённый ОДНОЙ ведомостью молчит — обеими формами сразу", () => {
    // Двусторонний контроль. Без него отрицание зеленело бы и на предикате,
    // который просто перестал читать одну из ведомостей: «ноль находок»
    // получилось бы из пустого множества, а не из отсутствия пересечения.
    const census = doubleExcusedFrom(
      forkLedger(["demo/src/lib/live-fork.ts"]),
      reachLedger(["demo/src/lib/dead-but-not-a-fork.ts"]),
    );
    expect(census.excusedTwice).toEqual([]);
    expect(census.excusedForks).toEqual(["demo/src/lib/live-fork.ts"]);
    expect(census.excusedDead).toEqual(["demo/src/lib/dead-but-not-a-fork.ts"]);
  });

  it("перечень модулей ВЫВОДИТСЯ из ведомостей, а не выписан", () => {
    const census = doubleExcusedFrom(forkLedger(["alpha/src/lib/a.ts"]), reachLedger(["beta/src/lib/b.ts"]));
    expect(census.modules).toEqual(["alpha", "beta"]);
  });

  it("сдвинутая форма ключа — находка, а не тихое пустое пересечение", () => {
    // Тот же файл в обеих ведомостях, но одна написала ключ с «./». Пересечение
    // по строке пусто — и молчало бы, будь эта ветвь единственной защитой.
    const census = doubleExcusedFrom(
      forkLedger(["./demo/src/lib/api-client.ts"]),
      reachLedger(["demo/src/lib/api-client.ts"]),
    );
    expect(census.excusedTwice).toEqual([]);
    expect(census.malformed).toEqual(["./demo/src/lib/api-client.ts"]);
  });

  it("предпосылка ловит пустую ведомость: числа переписи нулевые", () => {
    const census = doubleExcusedFrom({ groups: [] }, { allowed: {} });
    expect(census.excusedForks).toEqual([]);
    expect(census.excusedDead).toEqual([]);
    expect(census.forkGroups).toBe(0);
    // Суд требует эти числа НЕнулевыми — на таком входе он покраснеет
    // предпосылкой, а не тихо согласится с пустым пересечением.
    expect(census.excusedTwice).toEqual([]);
  });
});
