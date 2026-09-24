// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Сторона КОНСОЛИ гейта достижимости пола «2» (`deploy/identity_second_factor_reachable_test.go`,
// `TestIdentity_SecondFactorReachesTheBrowser`; условие C0 приёмки F8).
//
// Гейт читает объявление ТЕКСТОМ, по имени, и на посадке `own` — единственной
// посадке стендов — берёт перечень `OWN_STEP_UP_METHODS`: способы, которые
// консоль доводит до конца НАШЕЙ церемонией повышения
// (`POST /iam/v1/auth/step-up`). Перечня потока поставщика (`STEP_UP_METHODS`)
// у консоли нет: поток поставщика она не ведёт вовсе, и объявление, которого
// она не исполняет, гейт засчитал бы ей как умение. Проба читает файл тем же
// выражением, что гейт, с той же границей слова, — иначе совпадение имени
// одного перечня внутри другого прочиталось бы как второе объявление.

const FILE = path.join(path.dirname(fileURLToPath(import.meta.url)), "step-up-methods.ts");
const text = readFileSync(FILE, "utf8");

function declared(name: string): string[] | null {
  const m = new RegExp(`\\b${name}\\s*=\\s*\\[([\\s\\S]*?)\\]`).exec(text);
  return m ? [...m[1].matchAll(/"([a-z_]+)"/g)].map((g) => g[1]).sort() : null;
}

describe("C0 · способы нашей церемонии повышения объявлены под именем, которое читает гейт", () => {
  it("C0 · OWN_STEP_UP_METHODS — код из приложения и запасной код", () => {
    expect(declared("OWN_STEP_UP_METHODS")).toEqual(["lookup_secret", "totp"]);
  });

  it("C0 · перечня потока поставщика нет: консоль его не ведёт", () => {
    // Граница слова: `OWN_STEP_UP_METHODS` не есть объявление `STEP_UP_METHODS`.
    expect(declared("STEP_UP_METHODS")).toBeNull();
  });
});
