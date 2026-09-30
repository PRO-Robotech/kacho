// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, test } from "@playwright/test";

import { awaitLetter, Mailbox } from "../../specs/mail-receiver.ts";
import {
  EMPTY_ADDRESS,
  LETTER_ADDRESS,
  LETTER_CODE,
  MAIL_CANARY,
  MAIL_CANARY_DEAD_ENV,
  MAIL_CANARY_LIVE_ENV,
} from "./names.ts";

/**
 * КАНАРЕЙКА ЧТЕНИЯ ПРИЁМНИКА ПИСЕМ (приёмка F6b, Р13, F6b-34; kacho#2901) — не
 * проба, а ВХОД самопроверки `../mail-receiver-marks-selftest.ts`. Её исполняет
 * отдельный процесс прогонщика, а исход каждой единицы судит НАСТОЯЩИЙ
 * распознаватель гейта вердикта (`fixture_unmet`), а не его пересказ.
 *
 * Предмет — каждая форма, которой набор читает приёмник: снимок писем до
 * регистрации (`Mailbox.letters` — `register` в `specs/fixtures.ts`, F6b-32,
 * посев П-п, F8-13 и F8-17), число писем (`Mailbox.total` — условие F6b-34) и
 * ожидание письма (`awaitLetter`). Приёмник не отвечает — каждая обязана отдать
 * «не выполнилось». Близнец меняет ровно один факт — проба перехватила сорванное
 * чтение и упала по существу, — и обязан остаться красным. Контроли на
 * отвечающем приёмнике проходят.
 *
 * Имя файла — не `*.spec.ts`: в набор (`playwright.config.ts`, `specs/`) он не
 * входит и стенда не требует; его прогон задаёт `canary.playwright.config.ts`.
 */

const live = (): Mailbox => new Mailbox(process.env[MAIL_CANARY_LIVE_ENV] ?? "");
const dead = (): Mailbox => new Mailbox(process.env[MAIL_CANARY_DEAD_ENV] ?? "");

test(MAIL_CANARY.lettersDead.title, async () => {
  const before = new Set((await dead().letters(EMPTY_ADDRESS)).map((l) => l.id));
  expect(before.size, "у неотвечающего приёмника снимок обязан сорваться, а не прочитаться").toBe(-1);
});

test(MAIL_CANARY.totalDead.title, async () => {
  const letters = await dead().total();
  expect(letters, "у неотвечающего приёмника число писем обязано сорваться, а не прочитаться").toBe(-1);
});

test(MAIL_CANARY.awaitDead.title, async () => {
  await awaitLetter(dead(), EMPTY_ADDRESS, new Set(), 2_000);
});

test(MAIL_CANARY.substanceAfterDead.title, async () => {
  const before = await dead()
    .letters(EMPTY_ADDRESS)
    .then((got) => new Set(got.map((l) => l.id)))
    .catch(() => new Set<string>());
  expect(before.size, "по существу: продукт ответил не то").toBe(-1);
});

test(MAIL_CANARY.lettersLive.title, async () => {
  const before = new Set((await live().letters(EMPTY_ADDRESS)).map((l) => l.id));
  expect(before.size, "у приёмника петли на пустой адрес писем нет").toBe(0);
  expect(await live().total(), "у приёмника петли одно письмо").toBe(1);
});

test(MAIL_CANARY.awaitLive.title, async () => {
  const letter = await awaitLetter(live(), LETTER_ADDRESS, new Set(), 5_000);
  expect(letter.code, "код письма приёмника петли").toBe(LETTER_CODE);
});
