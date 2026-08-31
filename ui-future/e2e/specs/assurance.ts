// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHmac } from "node:crypto";
import { expect, type Page } from "@playwright/test";

/**
 * Второй уровень уверенности: как его поднять и как под ним позвать глагол.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ОТДЕЛЬНЫЙ МОДУЛЬ, А НЕ КОПИЯ РЯДОМ
 *
 * Механику подъёма уровня спрашивают ДВЕ пробы: `stepup.spec.ts` (уровень
 * ДОСТИЖИМ) и `users.spec.ts` (второе членство человека — сценарий IAM-ID-2-20,
 * задача #1357), а следом за ней — долг #1208 о втором ЧЕЛОВЕКЕ в аккаунте.
 * Копия разошлась бы с оригиналом молча и разошлась бы там, где расхождение не
 * видно: обе стороны отвечают «уровень поднят» на арендаторе, у которого он и
 * так поднят.
 *
 * Модуль НЕ является пробой (`playwright` собирает `*.spec.ts`), поэтому набор
 * от него не прирастает ни одним кейсом.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ПОТОКАМИ, А НЕ КЛИКАМИ
 *
 * Второй фактор арендатор заводит в разделе параметров безопасности, который
 * раздаёт та же консоль, — но заполнение его формы браузером не проверяет
 * ничего сверх того, что уже проверено пробой окна подтверждения. Предмет
 * здесь — что уровень ДОСТИЖИМ. Поэтому потоки проходятся тем же способом,
 * каким их проходит консоль, — из браузерного контекста, с его печеньями.
 */

const KRATOS = "/.ory/kratos/public";

interface FlowNode {
  type: string;
  group: string;
  attributes: Record<string, unknown> & { name?: string; id?: string; value?: unknown };
}
interface Flow {
  id: string;
  ui: { nodes: FlowNode[] };
}

/** Значение csrf-узла потока. */
function csrf(flow: Flow): string {
  const n = flow.ui.nodes.find((x) => x.attributes?.name === "csrf_token");
  const v = n?.attributes?.value;
  expect(typeof v === "string" && v.length > 0, "поток не отдал csrf-узел").toBe(true);
  return String(v);
}

/**
 * Секрет одноразового кода из потока параметров.
 *
 * Узел текстовый, и его форма у службы личности отличается от полей ввода;
 * поэтому ищется он по идентификатору, а значение достаётся из текста.
 */
function totpSecret(flow: Flow): string {
  for (const n of flow.ui.nodes) {
    const id = String(n.attributes?.id ?? n.attributes?.name ?? "");
    if (id !== "totp_secret_key") continue;
    const text = (n.attributes as { text?: { text?: string } }).text?.text;
    if (typeof text === "string" && text.length > 0) return text.replace(/\s+/g, "");
  }
  throw new Error(
    "поток параметров не предложил секрет одноразового кода: значит метод второго " +
      "фактора выключен в настройках службы личности — уровень поднять нечем",
  );
}

/** Крокфордовой здесь нет: секрет второго фактора — обычный base32 (RFC 4648). */
function base32Decode(s: string): Buffer {
  const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = 0;
  let value = 0;
  const out: number[] = [];
  for (const ch of s.toUpperCase().replace(/=+$/, "")) {
    const idx = A.indexOf(ch);
    if (idx < 0) continue;
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      out.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(out);
}

/** RFC 6238, шаг 30 секунд, SHA-1, шесть знаков — то, что объявляют настройки. */
function totpCode(secret: string, at = Date.now()): string {
  const counter = Math.floor(at / 1000 / 30);
  const buf = Buffer.alloc(8);
  buf.writeUInt32BE(Math.floor(counter / 2 ** 32), 0);
  buf.writeUInt32BE(counter >>> 0, 4);
  const mac = createHmac("sha1", base32Decode(secret)).update(buf).digest();
  const off = mac[mac.length - 1] & 0x0f;
  const bin = ((mac[off] & 0x7f) << 24) | (mac[off + 1] << 16) | (mac[off + 2] << 8) | mac[off + 3];
  return String(bin % 1_000_000).padStart(6, "0");
}

/** Ждёт следующего окна кода: тот же код дважды подряд предъявлять незачем. */
async function nextWindow(): Promise<void> {
  const ms = 30_000 - (Date.now() % 30_000) + 1_000;
  await new Promise((r) => setTimeout(r, ms));
}

/** Поток в режиме одностраничного приложения: приходит тело, а не перенаправление. */
async function initFlow(page: Page, path: string): Promise<Flow> {
  const res = await page.request.get(`${KRATOS}${path}`, { headers: { Accept: "application/json" } });
  expect(res.status(), `инициация потока ${path} не удалась: ${await res.text()}`).toBe(200);
  return (await res.json()) as Flow;
}

async function submitFlow(page: Page, path: string, id: string, body: Record<string, unknown>) {
  return page.request.post(`${KRATOS}${path}?flow=${encodeURIComponent(id)}`, {
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    data: body,
  });
}

/** Уровень уверенности, с которым край видит текущую сессию. */
export async function assuranceLevel(page: Page): Promise<string> {
  const res = await page.request.get(`${KRATOS}/sessions/whoami`, { headers: { Accept: "application/json" } });
  if (!res.ok()) return "";
  const s = (await res.json()) as { authenticator_assurance_level?: string };
  return s.authenticator_assurance_level ?? "";
}

/**
 * Поднять сессию арендатора до второго уровня: завести одноразовый код и
 * подтвердить им вход.
 *
 * КАЖДЫЙ шаг утверждает свой исход. Это не педантизм: шаг, собирающий условие
 * и не утверждающий его, при отказе оставляет сессию на первом уровне, а
 * падает потом — на глаголе, который отвергнут законно. Тогда виновником
 * называется невиновный, и разбор уходит в модель прав.
 */
export async function raiseAssurance(page: Page): Promise<void> {
  const settings = await initFlow(page, "/self-service/settings/browser");
  const secret = totpSecret(settings);
  const enrolled = await submitFlow(page, "/self-service/settings", settings.id, {
    csrf_token: csrf(settings),
    method: "totp",
    totp_code: totpCode(secret),
  });
  expect(enrolled.status(), `второй фактор не завёлся: ${await enrolled.text()}`).toBe(200);

  // Тот же код дважды подряд не предъявляем.
  await nextWindow();

  const stepUp = await initFlow(page, "/self-service/login/browser?refresh=true&aal=aal2");
  const raised = await submitFlow(page, "/self-service/login", stepUp.id, {
    csrf_token: csrf(stepUp),
    method: "totp",
    totp_code: totpCode(secret),
  });
  expect(raised.status(), `подтверждение вторым фактором не прошло: ${await raised.text()}`).toBe(200);

  await expect
    .poll(async () => assuranceLevel(page), {
      message: "сессия осталась на первом уровне после подтверждения вторым фактором",
      timeout: 30_000,
    })
    .toBe("aal2");
}
