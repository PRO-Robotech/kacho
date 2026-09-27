// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { afterAll, describe, expect, it, jest } from "@jest/globals";
import { orderedTransport } from "@shared/api/carrier-order";
import { stubNetworkOnce } from "./issuance-guard";
import { ISSUANCE_CANARY } from "./issuance-wiring";

/**
 * КАНАРЕЙКА ПОДКЛЮЧЕНИЯ СТРАЖА МЕСТ ВЫПУСКА (приёмка F8, Р10, F8-46) — не проба,
 * а ВХОД пробы `issuance-wiring.ts`. Её исполняет отдельный процесс jest с
 * настоящей конфигурацией модуля, и две из трёх её единиц ОБЯЗАНЫ упасть: их
 * падение и есть свидетельство того, что окружение модуля роняет пробу, за
 * которой записан обход. Поэтому имя файла — не `*.test.ts`: прогон модуля её не
 * собирает, собирает только процесс пробы подключения.
 *
 * Изменён ровно один факт между канарейкой и близнецом — чем выпущено обращение.
 * Отказ стража код здесь глотает намеренно: так глотает его код продукта, и
 * пробу обязана уронить запись, а не отказ.
 */

describe("F8-46 · канарейка подключения стража", () => {
  it(ISSUANCE_CANARY.swallowed.title, async () => {
    await fetch(ISSUANCE_CANARY.swallowed.path).then(
      () => undefined,
      () => undefined,
    );
  });

  it(ISSUANCE_CANARY.twin.title, async () => {
    const network = jest.fn(() =>
      Promise.resolve({ ok: true, status: 200, headers: { get: () => null } } as unknown as Response),
    );
    stubNetworkOnce(globalThis, network);
    await orderedTransport.fetch(ISSUANCE_CANARY.twin.path).then(
      () => undefined,
      () => undefined,
    );
    // Близнец обязан ДОЙТИ до сети: иначе он зеленеет и на страже, который
    // отказывает всему, и не отличает законный выпуск от обхода.
    expect(network).toHaveBeenCalledTimes(1);
  });
});

// Обход ПОСЛЕ последнего `afterEach` — в разборе блока. Его держит только
// `afterAll` окружения: блок стоит последним, и следом за ним единиц нет.
describe(ISSUANCE_CANARY.late.title, () => {
  afterAll(async () => {
    await fetch(ISSUANCE_CANARY.late.path).then(
      () => undefined,
      () => undefined,
    );
  });

  it(ISSUANCE_CANARY.late.unit, () => {
    expect(ISSUANCE_CANARY.late.path).not.toBe("");
  });
});
