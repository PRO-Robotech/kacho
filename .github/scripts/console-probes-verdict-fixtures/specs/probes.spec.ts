// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// `FX_MODE=flip` роняет первый повтор пробы «а» и пропускает второй: так
// снимается отчёт «первый упал, второй прошёл». Упавший запуск несёт вложение
// `trace`, как проба консоли (#1287), — иначе гейт краснел бы ещё и по трассе.
import { expect, test } from "@playwright/test";

test("проба а", async () => {
  if (process.env.FX_MODE === "flip" && test.info().repeatEachIndex === 0) {
    await test.info().attach("trace", { body: "trace", contentType: "application/zip" });
    expect(1).toBe(2);
  }
});

test("проба б", async () => {});

test("проба (в) со скобками", async () => {});
