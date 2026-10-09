// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHash } from "node:crypto";
import { leadingZeroBitsIndependently, loadPowVectors, POW_VECTORS_PATH } from "@shared/test/pow-worker-fake";

// Решатель вызова края — одна функция, две реализации, одни векторы
// (приёмка NTF-2, Р5; замысел `issue-2917` З10, CX2-30 (в)).
//
// Край проверяет доказательство (Go, `gateway/internal/middleware/anonmail`),
// консоль его ищет (TS, `Worker`). Обе стороны судятся ОДНИМ файлом векторов
// края: одни входы, одни ответы, и расхождение любой стороны — красный. Тот же
// файл несёт срок вызова и бюджет решателя; константа консоли сверяется с ним
// здесь, инвариант «бюджет меньше срока» — над тем же файлом.
//
// ПОРЯДОК. Каждая проба сперва проверяет фикстуру (`loadPowVectors`: файл есть,
// обе стороны представлены, каждый вектор сходится с SHA-256 `node:crypto`), и
// только потом спрашивает решатель консоли. Сломанный файл векторов не вправе
// выдать себя за отсутствующий решатель.

/** Решатель консоли — модуль, который читает и `Worker`, и эти пробы. */
async function solver() {
  return import("./pow");
}

describe("решатель вызова края — общие векторы края и консоли (NTF2-58, CX2-30)", () => {
  it("CX2-30 (в) · число ведущих нулевых битов и вердикт — те же, что у края, на каждом векторе", async () => {
    // verifies #2917
    const file = loadPowVectors();
    const { leadingZeroBits, meetsDifficulty } = await solver();
    for (const v of file.vectors) {
      expect([v.challenge, v.nonce, leadingZeroBits(v.challenge, v.nonce)]).toEqual([
        v.challenge,
        v.nonce,
        v.leadingZeroBits,
      ]);
      expect([v.challenge, v.nonce, v.bits, meetsDifficulty(v.challenge, v.nonce, v.bits)]).toEqual([
        v.challenge,
        v.nonce,
        v.bits,
        v.accepted,
      ]);
    }
    // Перепись: «ноль расхождений» отличим от «ноль прочитанного».
    expect(file.vectors.length).toBeGreaterThan(0);
  });

  it("CX2-30 (в) · собственная SHA-256 консоли совпадает с node:crypto на границах блока и многобайтовом тексте", async () => {
    // verifies #2917
    loadPowVectors();
    const { sha256 } = await solver();
    // Длины 0…130 пересекают обе границы дополнения блока (55/56 и 119/120 байт);
    // кириллица — многобайтовая UTF-8: вызов края base64url, но `nonce` и
    // разделитель консоль собирает сама.
    const inputs = Array.from({ length: 131 }, (_, n) => "a".repeat(n)).concat(["вызов:42", "ключ:0"]);
    for (const text of inputs) {
      const expected = createHash("sha256").update(text, "utf8").digest("hex");
      const got = Buffer.from(sha256(text)).toString("hex");
      expect([text.length, got]).toEqual([text.length, expected]);
    }
  });

  it("CX2-30 (в) · найденный решателем nonce проходит проверку края — по независимой SHA-256", async () => {
    // verifies #2917
    const file = loadPowVectors();
    const { solve } = await solver();
    const seen = new Set<string>();
    for (const v of file.vectors.filter((x) => x.accepted)) {
      if (seen.has(`${v.challenge}/${v.bits}`)) continue;
      seen.add(`${v.challenge}/${v.bits}`);
      const nonce = solve(v.challenge, v.bits);
      expect(/^\d+$/.test(nonce)).toBe(true);
      expect(leadingZeroBitsIndependently(v.challenge, nonce)).toBeGreaterThanOrEqual(v.bits);
    }
    expect(seen.size).toBeGreaterThan(0);
  });

  it("NTF2-58 · бюджет решателя консоли — тот, что в общем файле, и он меньше срока вызова", async () => {
    // verifies #2917
    const file = loadPowVectors();
    // Инвариант над самим файлом — сторона фикстуры: без него проба (б) NTF2-58
    // строила бы бюджет, который край пережить не успевает.
    expect(file.solverBudgetSeconds).toBeLessThan(file.challengeTtlSeconds);
    const { SOLVER_BUDGET_MS } = await solver();
    expect([POW_VECTORS_PATH, SOLVER_BUDGET_MS]).toEqual([POW_VECTORS_PATH, file.solverBudgetSeconds * 1000]);
  });
});
