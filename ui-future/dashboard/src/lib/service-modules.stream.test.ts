// Соответствие счётчика витрины предмету потока — сверяется с ОБЩЕЙ картой,
// а не с копией.
//
// Цена ошибки здесь тихая и потому дорогая: неверное написание вида не даёт
// НИЧЕГО — поток откроется, словаря этого вида не принесёт, покрытие не
// объявится, и счётчик молча останется на опросе. Отличить это от исправной
// работы нечем, поэтому соответствие держится пробой, а не вниманием.

import { SERVICE_MODULES } from "./service-modules";
import { streamSubject } from "@shared/lib/subscription/subjects";

const allStats = SERVICE_MODULES.flatMap((m) => m.stats.map((stat) => ({ module: m.key, ...stat })));
const journaled = allStats.filter((s) => s.specId != null);
const polled = allStats.filter((s) => s.specId == null);

process.stdout.write(
  `\n  счётчики витрины: всего ${allStats.length}, ` + `с журналом ${journaled.length}, на опросе ${polled.length}\n`,
);

describe("счётчики витрины ↔ предметы потока", () => {
  it("предпосылка: витрина прочитана и счётчики найдены", () => {
    // Без этого «ноль расхождений» означало бы «ноль прочитанного».
    expect(allStats.length).toBeGreaterThan(0);
    expect(journaled.length).toBeGreaterThan(0);
  });

  it("каждый объявленный specId резолвится в предмет потока", () => {
    const broken = journaled
      .filter((s) => streamSubject(s.specId!) === null)
      .map((s) => `${s.module}/${s.key} → «${s.specId}»`);
    expect(
      broken.length === 0
        ? ""
        : `счётчиков с ненаходимым предметом ${broken.length}:\n  ${broken.join("\n  ")}\n\n` +
            `Идентификатор спеки обязан быть ключом карты STREAM_SUBJECTS. Не найденный там ` +
            `означает вечный опрос при живом журнале — и молча: покрытие просто не объявится.`,
    ).toBe("");
  });

  it("счётчик БЕЗ предмета стоит только там, где журнала нет у ВСЕГО модуля", () => {
    // Полумера — половина счётчиков модуля на потоке, половина на опросе — не
    // запрещена, но и не бывает случайной: модуль опрашивается целиком, пока
    // покрыт не каждый его вид. Смешанный модуль означает, что кто-то забыл
    // назвать предмет, а не решил его не называть.
    const mixed = SERVICE_MODULES.filter(
      (m) => m.stats.some((s) => s.specId != null) && m.stats.some((s) => s.specId == null),
    ).map((m) => m.key);
    expect(mixed).toEqual([]);
  });

  it("перепись счётчиков названа числом — новая плитка требует РЕШЕНИЯ, а не умолчания", () => {
    // Числа здесь — не украшение: плитка, добавленная без предмета потока,
    // молча уехала бы на опрос, и это выглядело бы как исправная работа.
    expect(`всего ${allStats.length}, с журналом ${journaled.length}, на опросе ${polled.length}`).toBe(
      "всего 14, с журналом 11, на опросе 3",
    );
    expect([...new Set(polled.map((s) => s.module))]).toEqual(["iam"]);
  });
});
