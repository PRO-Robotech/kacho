import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { sweepCopies, type CopyHit } from "./cross-module-copy-sweep";

/**
 * ТРЕТИЙ ПРИЗНАК форка: копия, у которой дома в общем НЕТ.
 *
 * Соседний гейт (`shared-organisms-single-source`) судит по двум признакам, и оба
 * якорятся на `shared/`: объявленный символ общего либо парный адрес под
 * `shared/src/`. Файл, лежащий в нескольких приложениях байт-в-байт и не имеющий
 * в общем ни имени, ни адреса, для обоих невидим BY CONSTRUCTION — не «пропущен»,
 * а вне наблюдения. Перепись это подтвердила: `lib/dpop.ts` живёт в четырёх
 * модулях по 285 строк и в ведомости форков не встречается НИ РАЗУ.
 *
 * Почему это худший вид копии, а не мягчайший: у двух других есть единственный
 * источник — общий, — и вопрос лишь в том, взят ли он. Здесь источника нет
 * вовсе. Правку надо повторить столько раз, сколько копий, и никто не считает,
 * доехала ли она; расхождение появляется молча и живёт до чужой отладки.
 *
 * ВЕДОМОСТЬ САМОИСТЕКАЕТ. Своя, а не общая с соседним гейтом: предмет другой
 * (там — «взят ли общий», здесь — «есть ли общий вовсе»), и запись, объясняющая
 * одно, не объясняет другого. Запись, которой больше нечего исключать, роняет
 * гейт: послабление, пережившее свой предмет, — тот же класс, который корпус
 * ловит в коде.
 *
 * ИСХОДОВ У НАХОДКИ ТРИ, четвёртого нет: завести дом в `shared/` и свести копии
 * к прослойке; снять копию, если она мертва (у этого класса так чаще всего и
 * бывает — четыре копии клиента края не импортирует ни один модуль); либо
 * записать в ведомость с причиной и предикатом снятия.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const LEDGER_PATH = path.join(repoRoot, "shared/src/test/cross-module-copy-ledger.json");

interface Ledger {
  comment: string[];
  allowed: Record<string, string>;
}

const LEDGER: Ledger = JSON.parse(readFileSync(LEDGER_PATH, "utf8")) as Ledger;
const SWEEP = sweepCopies(repoRoot);

/**
 * Ключ записи ведомости — путь И перечень приложений, а не один путь: один и тот
 * же путь бывает у ДВУХ разных наборов копий с разным содержимым (`test/style-mock.ts`
 * живёт двумя разными редакциями — одной у четырёх модулей, другой у двух). Ключ
 * по одному пути слил бы их в одну запись, и снятие одной группы молча прощало бы
 * вторую.
 */
function keyOf(h: CopyHit): string {
  return `${h.rel} [${h.apps.join(",")}]`;
}

function describeHit(h: CopyHit): string {
  return `${keyOf(h)}: ${h.apps.length} копий по ${h.lines} строк, дома в shared/src нет`;
}

describe("копия, у которой дома в общем нет", () => {
  it(`перепись: приложений ${SWEEP.apps.length}, файлов прочитано ${SWEEP.filesRead}, парных по содержимому ${SWEEP.pairedByContent} (с домом в общем ${SWEEP.withHome}, без дома ${SWEEP.hits.length})`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: сдвинутый
    // корень или переименованная раскладка иначе делают утверждения ниже
    // вакуумно истинными.
    expect(SWEEP.apps.length).toBeGreaterThan(0);
    expect(SWEEP.filesRead).toBeGreaterThan(0);
    // Признак обязан что-то РАЗЛИЧАТЬ: ноль парных по содержимому означал бы, что
    // сравнение перестало находить даже законные пары, и тогда «без дома» стало
    // бы нулём по той же причине, а не по достижению цели.
    expect(SWEEP.pairedByContent).toBeGreaterThan(0);
    expect(SWEEP.apps).toEqual(expect.arrayContaining(["compute", "nlb", "registry", "storage", "vpc"]));
  });

  it("ведомость несёт причину по каждой записи", () => {
    const withoutReason = Object.entries(LEDGER.allowed)
      .filter(([, why]) => !why || why.trim() === "")
      .map(([rel]) => rel);
    expect(withoutReason).toEqual([]);
  });

  it("ведомость самоистекает: записи, которой нечего исключать, быть не должно", () => {
    const live = new Set(SWEEP.hits.map(keyOf));
    const stale = Object.keys(LEDGER.allowed)
      .filter((key) => !live.has(key))
      .map((key) => `${key}: копии больше нет, состав изменился либо у неё появился дом — снять запись`);
    expect(stale).toEqual([]);
  });

  it("новых копий без дома нет: каждая названа в ведомости", () => {
    const findings = SWEEP.hits.filter((h) => !(keyOf(h) in LEDGER.allowed)).map(describeHit);
    expect(findings).toEqual([]);
  });
});
