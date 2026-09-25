// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import * as stepUpMethods from "./step-up-methods";

// Сторона КОНСОЛИ гейта достижимости пола «2» (`deploy/identity_second_factor_reachable_test.go`,
// `TestIdentity_SecondFactorReachesTheBrowser`; условие C0 приёмки F8).
//
// На посадке `own` — единственной посадке стендов — гейт берёт у консоли
// перечень `OWN_STEP_UP_METHODS`: способы, которые консоль доводит до конца
// НАШЕЙ церемонией повышения (`POST /iam/v1/auth/step-up`). Перечня потока
// поставщика (`STEP_UP_METHODS`) у консоли нет: поток поставщика она не ведёт
// вовсе, и объявление, которого она не исполняет, гейт засчитал бы ей как умение.
//
// Проба ЗАГРУЖАЕТ модуль и утверждает то, что он отдаёт: значение перечня под
// тем именем, которое читает гейт, и отсутствие второго перечня среди его
// экспортов. Текст объявления читает сам гейт — повторять его разбор здесь
// значило бы подтвердить модуль его же текстом, ничего не исполнив.

describe("C0 · способы нашей церемонии повышения отданы под именем, которое читает гейт", () => {
  it("C0 · OWN_STEP_UP_METHODS — код из приложения и запасной код", () => {
    expect([...stepUpMethods.OWN_STEP_UP_METHODS].sort()).toEqual(["lookup_secret", "totp"]);
  });

  it("C0 · перечня потока поставщика нет: консоль его не ведёт", () => {
    expect(Object.keys(stepUpMethods)).not.toContain("STEP_UP_METHODS");
    expect(Object.keys(stepUpMethods)).toContain("OWN_STEP_UP_METHODS");
  });
});
