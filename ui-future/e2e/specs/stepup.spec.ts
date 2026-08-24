// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { createHmac } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { register, runTag } from "./fixtures";

/**
 * Достижимость ВТОРОГО уровня уверенности из браузера.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Каталог прав объявляет части глаголов пол уровня «2», и край спрашивает его в
 * том числе на браузерной полосе. Если поднять уровень нечем, объявленный пол
 * означает не «подтвердите второй фактор», а «этого действия из браузера не
 * существует» — и означает это для ВСЕХ, а не для нарушителей.
 *
 * Проба утверждает ПАРУ, и без второй половины первая ничего не стоит:
 *   отрицание — глагол с полом «2» отвергается на первом уровне;
 *   контроль  — ТОТ ЖЕ глагол проходит после поднятия уровня.
 *
 * Одно отрицание зеленело бы на продукте, где действие запрещено вообще всем.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ГЛАГОЛ — УДАЛЕНИЕ ГРУППЫ
 *
 * Заведение группы полом не связано (пол «1»), а удаление связано полом «2» —
 * то есть проба сама создаёт свой предмет на доступном уровне и сама его
 * убирает, ничего за собой не оставляя. Ни один другой арендатор ею не задет.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ПОДЪЁМ УРОВНЯ ИДЁТ ПОТОКАМИ, А НЕ КЛИКАМИ
 *
 * Второй фактор арендатор заводит в разделе параметров безопасности, который
 * раздаёт та же консоль, — но заполнение его формы браузером не проверяет
 * ничего сверх того, что уже проверено пробой окна подтверждения. Предмет
 * ЗДЕСЬ — что уровень ДОСТИЖИМ: что удостоверение второго фактора заводится
 * арендатором, у которого есть только пароль, и что край видит поднятый
 * уровень. Поэтому потоки проходятся тем же способом, каким их проходит
 * консоль, — из браузерного контекста, с его печеньями.
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
      bits -= 8;
      out.push((value >>> bits) & 0xff);
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
async function assuranceLevel(page: Page): Promise<string> {
  const res = await page.request.get(`${KRATOS}/sessions/whoami`, { headers: { Accept: "application/json" } });
  if (!res.ok()) return "";
  const s = (await res.json()) as { authenticator_assurance_level?: string };
  return s.authenticator_assurance_level ?? "";
}

test("iam: глагол с полом «2» отвергается на первом уровне и проходит после поднятия", async ({ page }) => {
  // verifies #1213 — второй уровень уверенности обязан быть ДОСТИЖИМ из браузера.
  //
  // До фикса поднять его было нечем ни при каком вводе: единственный способ,
  // который предлагала консоль, настройки объявляли ПЕРВЫМ фактором, а профиль
  // стенда вдобавок выключал одноразовый код рядом с включённым в единственном
  // объявлении продукта. 32 глагола каталога были закрыты из браузера для всех.
  test.setTimeout(240_000);

  await register(page);

  // Арендатор заведён паролем: первый уровень, второго фактора нет ни одного.
  expect(await assuranceLevel(page), "свежий арендатор обязан быть на первом уровне").toBe("aal1");

  const accountId = await expect
    .poll(
      async () => {
        const res = await page.request.get("/iam/v1/accounts?pageSize=1000");
        if (!res.ok()) return "";
        const body = (await res.json()) as { accounts?: Array<{ id: string }> };
        return body.accounts?.[0]?.id ?? "";
      },
      { message: "аккаунт арендатора не появился — предмет пробы не собран", timeout: 45_000 },
    )
    .not.toBe("");

  // ── предмет: группа, заведённая на ДОСТУПНОМ уровне (пол «1») ───────────
  const created = await page.request.post("/iam/v1/groups", {
    data: { accountId, name: `e2e-stepup-${runTag()}`, description: "проба достижимости второго уровня" },
  });
  expect(created.status(), `заведение группы не удалось: ${await created.text()}`).toBe(200);
  const op = (await created.json()) as { metadata?: { groupId?: string } };
  const groupId = op.metadata?.groupId ?? "";
  expect(groupId, "операция не назвала идентификатор группы").not.toBe("");

  // Ждём УСЛОВИЕ, а не время, и условие это закрывает СРАЗУ ДВЕ дыры: право на
  // свой свежий ресурс материализуется в ограниченном окне, а идентификатор из
  // метаданных операции чеканится ДО того, как асинхронная часть могла отказать,
  // — то есть непрочитанный идентификатор был бы фантомом.
  await expect
    .poll(async () => (await page.request.get(`/iam/v1/groups/${groupId}`)).status(), {
      message:
        "своя свежая группа не читается: либо право ещё не материализовалось, либо " +
        "идентификатор — фантом несозданного ресурса",
      timeout: 45_000,
    })
    .toBe(200);

  // ── ОТРИЦАНИЕ: удаление (пол «2») на первом уровне отвергается ──────────
  const denied = await page.request.delete(`/iam/v1/groups/${groupId}`);
  // Код именно 401, а не 403: RFC 9470 объявляет «предъявленное годно и лишь
  // недостаточно сильно» вызовом повышения, а не отказом в правах. Права у
  // арендатора есть — группа его собственная, — и отвергнут он полом.
  expect(
    denied.status(),
    "удаление группы объявлено полом уровня «2»; на первом уровне край обязан его отвергнуть",
  ).toBe(401);
  expect(
    denied.headers()["www-authenticate"] ?? "",
    "отказ обязан назвать ПРИЧИНУ машинно: без вызова повышения клиент не отличит " +
      "«поднимите уровень» от «вам это запрещено навсегда», и консоль не откроет окно " +
      "подтверждения — она ключуется именно на этот вызов",
  ).toContain("insufficient_user_authentication");

  // ── поднятие уровня: одноразовый код заводится САМИМ арендатором ────────
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

  // ── ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: ТОТ ЖЕ глагол проходит ─────────────────────
  //
  // Край держит вердикт о сессии недолгим кэшем, поэтому ждём УСЛОВИЕ, а не
  // время; окно ограничено, и без ожидания проба краснела бы на исправном
  // продукте.
  await expect
    .poll(async () => (await page.request.delete(`/iam/v1/groups/${groupId}`)).status(), {
      message:
        "тот же глагол не прошёл и на втором уровне — значит отрицание выше ничего не " +
        "доказывает: оно зеленело бы и на продукте, где действие запрещено всем",
      timeout: 60_000,
    })
    .toBe(200);
});
