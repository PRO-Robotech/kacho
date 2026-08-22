// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * УСЛОВИЕ СОЗДАНО ДЛЯ ТОГО, КТО ИМ ПОЛЬЗУЕТСЯ (#935).
 *
 * Шаг-гейт конвейера спрашивает доступность консоли своим резолвером (curl), а
 * пробы ходят чужим — браузерным. Эти два ответа разошлись: гейт печатал «✓
 * консоль отвечает», и следом ВСЕ пробы падали на разрешении имени, не дойдя до
 * продукта. «Не выполнилось» подавалось как красное.
 *
 * Здесь тот же вопрос задан ТЕМ ЖЕ браузером, с тем же отображением имени и по
 * тому же адресу, что у проб. Отказ означает «условие не создано» и печатает
 * ЗАМЕР, а не догадку: чем имя разрешается у ОС, каким браузером идём, что
 * ответила навигация.
 *
 * Скрипт НЕ является пробой продукта: он ничего не утверждает о консоли, кроме
 * достижимости её служебного ответа.
 */
import { chromium } from "@playwright/test";
import { execFileSync } from "node:child_process";

const BASE = process.env.KACHO_CONSOLE_URL;
if (!BASE) {
  console.error("KACHO_CONSOLE_URL не задан — проверять нечего.");
  process.exit(1);
}

const HOST_IP = process.env.KACHO_CONSOLE_HOST_IP;
const hostname = new URL(BASE).hostname;
const args = HOST_IP ? [`--host-resolver-rules=MAP ${hostname} ${HOST_IP}`] : [];

function probeOS() {
  try {
    return execFileSync("getent", ["hosts", hostname], { encoding: "utf8" }).trim();
  } catch {
    return "(ОС имя не разрешает)";
  }
}

const launch = {
  args,
  ...(process.env.KACHO_CHROMIUM ? { executablePath: process.env.KACHO_CHROMIUM } : {}),
};

const browser = await chromium.launch(launch);
const page = await browser.newPage();

let reached = false;
let lastError = "";
// Ждём УСЛОВИЕ, а не время: стенд мог ещё дораскатываться.
for (let i = 1; i <= 12; i++) {
  try {
    const resp = await page.goto(`${BASE}/healthz`, { timeout: 10_000 });
    if (resp && resp.status() === 200) {
      console.log(`✓ браузер достаёт консоль на ${BASE}/healthz (попытка ${i})`);
      reached = true;
      break;
    }
    lastError = `статус ${resp ? resp.status() : "нет ответа"}`;
  } catch (e) {
    lastError = String(e && e.message ? e.message : e);
  }
  await new Promise((r) => setTimeout(r, 5_000));
}

if (!reached) {
  // Замер, а не гипотеза: три величины, по которым видно, чей резолвер отказал.
  console.log(`замер: ОС разрешает «${hostname}» как → ${probeOS()}`);
  console.log(`замер: браузер — ${browser.version()}, исполняемый файл: ` +
    `${process.env.KACHO_CHROMIUM ?? "(из playwright)"}`);
  console.log(`замер: отображение имени браузеру — ${args.length ? args[0] : "(не задано)"}`);
  console.log(`замер: последний ответ навигации — ${lastError}`);
  console.error(
    "::error title=Условие не создано::браузер не достал консоль на " +
      `${BASE}/healthz. Это НЕ вердикт по пробам: ни одна из них не дошла бы ` +
      "до продукта. Разбирать надо доступность стенда для браузера, а не консоль.",
  );
  await browser.close();
  process.exit(1);
}

await browser.close();
