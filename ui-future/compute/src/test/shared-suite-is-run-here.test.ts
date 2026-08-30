// Правка в общем модуле обязана проверяться ЭТИМ доменом.
//
// ПРЕДМЕТ. Модуль отдаёт пользователю почти целиком общий код: из его файлов
// компонентов подавляющее большинство — тонкие прослойки `export * from
// "@shared/…"`. При этом общая суита исполнялась только участниками рабочей
// области (`vpc`, `iam`, `system`), а они разрешают зависимости из КОРНЕВОГО
// `node_modules`. Этот модуль standalone: у него собственный `node_modules` и
// собственный замок версий, и они с корневым РАСХОДЯТСЯ — замер на день
// заведения пробы (2026-08-29): antd 6.5.4 в корне против 6.5.0 здесь,
// @tanstack/react-query 5.101.4 против 5.101.2.
//
// Значит общий код, который отгружает ЭТОТ модуль, не был проверен ни разу ни
// под одной из тех версий, с которыми он поедет к пользователю. «Общая суита
// зелёная» до этой пробы означало «зелёная под ДРУГИМ деревом зависимостей».
// Тот же порядок уже применён к `storage` (#407) и `nlb` (#408) — здесь он
// доводится до последнего сведённого модуля.
//
// ИСХОД, А НЕ ОБЪЯВЛЕНИЕ. Проба спрашивает у самого jest, какие суиты он на
// этой посадке НАХОДИТ (`--listTests`), а не читает `roots`/`testMatch`
// глазами: объявление можно написать верно и получить пустой обход — например,
// если отображение путей перестанет резолвить общий каталог. Тогда «настройка
// на месте» и «ничего не гоняется» выглядели бы одинаково.

import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const moduleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const sharedSrc = path.resolve(moduleRoot, "../shared/src");

/** Суиты, которые jest НАХОДИТ на посадке этого модуля. */
function discoveredSuites(): string[] {
  const out = execFileSync(
    process.execPath,
    [path.join(moduleRoot, "node_modules/jest/bin/jest.js"), "--listTests", "--json"],
    { cwd: moduleRoot, encoding: "utf8", maxBuffer: 32 * 1024 * 1024, stdio: ["ignore", "pipe", "ignore"] },
  );
  return JSON.parse(out) as string[];
}

const suites = discoveredSuites();
const fromShared = suites.filter((f) => f.startsWith(sharedSrc + path.sep));
const ownSuites = suites.filter((f) => !f.startsWith(sharedSrc + path.sep));

describe("общая суита исполняется посадкой этого модуля", () => {
  it(`перепись: суит найдено ${suites.length}, из них общих ${fromShared.length}, своих ${ownSuites.length}`, () => {
    // Пустой обход сделал бы утверждения ниже вакуумно истинными в обе стороны:
    // «общих ноль» читалось бы как находка, а «своих ноль» — как исправность.
    expect(suites.length).toBeGreaterThan(0);
    expect(ownSuites.length).toBeGreaterThan(0);
  });

  it("общая суита в обходе есть", () => {
    expect(fromShared.length).toBeGreaterThan(0);
  });

  // Положительный контроль на второй конец: общая суита в дереве ДЕЙСТВИТЕЛЬНО
  // велика, поэтому «нашлась одна штука» не должно читаться как успех. Порог
  // намеренно грубый — он стережёт обвал обхода, а не число проб.
  it("обход общей суиты не выродился в единицы файлов", () => {
    expect(fromShared.length).toBeGreaterThan(100);
  });
});
