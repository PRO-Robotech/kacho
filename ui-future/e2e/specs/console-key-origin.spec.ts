// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type APIResponse, type Page, type Response, type TestInfo } from "@playwright/test";
import { pageAuthenticator, raiseSeedToLevelTwo, submitRegistration } from "./access-key-seed";
import { ORIGIN_NOT_ALLOWED, ORIGIN_NOT_IN_LIST } from "./access-key-texts";
import { acceptedOperation, operationSucceeded, readerOf } from "./cloud-admin";
import {
  LANE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  transferSession,
  type Seed,
} from "./ceremony-seed";
import { ceremonyCensus, test } from "./fixtures";
import { openSecurity } from "./security-screen";

/**
 * Происхождение консоли в перечне ключей доступа стенда (kacho#3229, предикат
 * пп. 1–3).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕМ ЭТА ПРОБА ОТЛИЧАЕТСЯ ОТ СОСЕДНИХ
 *
 * Пробы ключей доступа (`*access-key*.spec.ts`) стоят за условием части набора
 * (`preconditions/access-key-origin.precondition.ts`): происхождение браузера
 * не в перечне — «не выполнилось: условие не создано», и о продукте они молчат.
 * Ровно этот исход и прятал находку: управляемая площадка объявила перечень
 * пустым («никого»), консоль при этом показала раздел «Ключи доступа», и
 * человек получил `ORIGIN_NOT_ALLOWED` на регистрации ключа.
 *
 * Здесь пустой либо чужой перечень при ПОКАЗАННОМ разделе — КРАСНОЕ. Консоль,
 * предлагающая завести ключ, обязана стоять на стенде, служба которого
 * принимает её происхождение. Поэтому имя файла не попадает под образец
 * `ACCESS_KEY_SPECS` (`playwright.config.ts`) и условия ключа над пробой нет.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ
 *
 * KO-01: ключ заводится ЭКРАНОМ `/settings` настоящей церемонией браузера
 * (виртуальный аутентификатор страницы), и в подписанных браузером клиентских
 * данных стоит происхождение консоли. Приём результата — `200` и Operation,
 * исход операции — `done` без `error`, ключ — в перечне края и на экране.
 *
 * KO-02: отрицательный близнец. Та же регистрация глаголами края тем же
 * человеком и той же сессией; меняется ровно один факт — происхождение в
 * клиентских данных. Своё происхождение принято (положительный контроль в том
 * же сценарии), чужое — `400` · `code` 3 · `ORIGIN_NOT_ALLOWED`, и в перечне
 * остаётся только ключ контроля.
 *
 * ЧЕМ ПРОБА КРАСНЕЕТ. Стенд, где перечень происхождений ключей доступа не
 * содержит происхождение его консоли (`authn.access-keys.origins` службы
 * удостоверений), роняет KO-01 на приёме результата и KO-02 на контроле.
 *
 * KO-03: раздел ключей согласован со службой В ОБЕ СТОРОНЫ (предикат п.4).
 * Объявление службы читается из её настоящего ответа экрану — подстановки
 * ответа нет, — и судится вместе с её исполнением: консоль предлагает завести
 * ключ ровно тогда, когда служба принимает происхождение консоли.
 *   — служба объявила «никого» (`accessKeys.available=false`): раздел пишет
 *     «выключены администратором», кнопки «Добавить ключ доступа» нет,
 *     обращений BeginRegistration со страницы — 0, а регистрация глаголами
 *     края с происхождением консоли отвергнута `ORIGIN_NOT_ALLOWED`;
 *   — объявила ключи (`available=true`): кнопка есть, текста «выключены» нет,
 *     и регистрация с происхождением консоли принята, операция завершена.
 * Красное — любое расхождение объявления с исполнением: объявлено «доступны»
 * при пустом перечне (консоль снова поведёт человека в ORIGIN_NOT_ALLOWED)
 * либо «никого» при перечне, принимающем консоль. Обе стороны исполняются
 * одной пробой на двух стендах: со значением перечня как есть и со слоем
 * `originFromConsole: false` · `origins: []`.
 *
 * Ключи, заведённые сценарием, снимаются тем же сценарием, в том числе на
 * падении. Ожидания временем нет: ждётся условие — отрисованный раздел, ответ,
 * исход операции.
 */

const keysOf = (userId: string) => `/iam/v1/users/${userId}/accessKeys`;

/** Тело отказа края — google.rpc.Status. */
interface Refusal {
  code?: unknown;
  message?: unknown;
  details?: Array<{ reason?: unknown }>;
}

function consoleOrigin(testInfo: TestInfo): string {
  const base = testInfo.project.use.baseURL;
  if (!base) throw new Error("у проекта нет baseURL — происхождение консоли не названо");
  return new URL(base).origin;
}

/** Происхождение, заведомо не объявленное ни одним стендом. */
const FOREIGN_ORIGIN = "https://origin-not-declared.kacho.invalid";

/**
 * Человек сценария: свой посев, сессия уровня «2» — пол регистрации ключа
 * (kaname#747, `raiseSeedToLevelTwo`); `userId` — из ответа края о сессии.
 */
async function seededHuman(seed: Seed, scenario: string): Promise<string> {
  await seedConfirmedHuman(seed, seedAddress(scenario));
  await raiseSeedToLevelTwo(seed);
  const me = await seed.read(SESSION_IDENTITY);
  expect(me.status(), `посев: ответ края о сессии — ${me.status()}`).toBe(200);
  const who = lastIssued(seed, SESSION_IDENTITY).body as { user?: { id?: unknown } } | null;
  const userId = typeof who?.user?.id === "string" ? who.user.id : "";
  expect(userId, "посев: край не назвал человека сессии").not.toBe("");
  return userId;
}

/** Отказ «происхождение не в перечне» — красное о стенде, с адресом консоли в тексте. */
function originRefusalMessage(what: string, origin: string, text: string): string {
  return (
    `${what}: служба отвергла происхождение консоли ${origin} — перечень происхождений ключей доступа стенда ` +
    `(authn.access-keys.origins) его не содержит, хотя консоль предлагает завести ключ. Ответ: ${text.slice(0, 300)}`
  );
}

/** Ключи человека по ответу края. */
async function listedKeys(seed: Seed, userId: string): Promise<Array<{ id: string; name: string }>> {
  const res = await seed.read(keysOf(userId));
  const text = await res.text();
  expect(res.status(), `перечень ключей: ${res.status()} ${text.slice(0, 300)}`).toBe(200);
  return ((JSON.parse(text) as { accessKeys?: Array<{ id?: unknown; name?: unknown }> }).accessKeys ?? []).map((k) => ({
    id: String(k.id ?? ""),
    name: String(k.name ?? ""),
  }));
}

/** Снять ключи человека и дождаться исхода каждой операции. */
async function revokeAll(seed: Seed, userId: string, ids: string[]): Promise<void> {
  for (const id of ids) {
    const op = await acceptedOperation(await seed.api.delete(`${keysOf(userId)}/${id}`), `снятие ключа ${id}`);
    await operationSucceeded(readerOf(seed), op, `снятие ключа ${id}`);
  }
}

/** Ответ на обращение страницы к пути и методу — ждать ставится ДО действия. */
function answerTo(page: Page, method: string, path: string): Promise<Response> {
  return page.waitForResponse((r) => r.request().method() === method && new URL(r.url()).pathname === path);
}

/** Регистрация глаголами края с заданным происхождением; исход — как ответил край. */
async function registerWith(seed: Seed, origin: string, name: string): Promise<{ res: APIResponse; text: string }> {
  const { response } = await submitRegistration(seed, origin, { userVerification: true, name });
  return { res: response, text: await response.text() };
}

test("KO-01 · ключ доступа заводится экраном /settings с происхождением консоли: приём 200, операция завершена, ключ в перечне", async ({
  page,
}, testInfo) => {
  // verifies #3229 — предикат п.1 и п.3: консоль показывает раздел ключей, и
  // служба стенда принимает её происхождение; пустой перечень — красное.
  test.setTimeout(120_000);
  const origin = consoleOrigin(testInfo);
  await pageAuthenticator(page, true);
  const seed = await newSeed(testInfo);
  let userId = "";
  try {
    userId = await seededHuman(seed, "KO-01");
    await transferSession(seed, page.context());

    await page.goto("/settings", { waitUntil: "domcontentloaded" });
    const region = page.getByRole("region", { name: "Ключи доступа" });
    await expect(region, "консоль не показала раздел «Ключи доступа» на /settings").toBeVisible({ timeout: 30_000 });

    // Добавление — окном «Добавить ключ доступа» раздела (приёмка F8-S7, Р3):
    // имя вводится в окне, церемонию выпускает «Добавить» окна.
    const adding = page.getByRole("dialog", { name: "Добавить ключ доступа" });
    const begun = answerTo(page, "POST", `${keysOf(userId)}:beginRegistration`);
    const finished = answerTo(page, "POST", keysOf(userId));
    await region.getByRole("button", { name: "Добавить ключ доступа", exact: true }).click();
    await adding.getByLabel("Имя", { exact: true }).fill("key-ko-01");
    await adding.getByRole("button", { name: "Добавить", exact: true }).click();

    const challenge = await begun;
    expect(challenge.status(), `выдача испытания регистрации: ${await challenge.text()}`).toBe(200);
    const rpId = ((await challenge.json()) as { rp?: { id?: string } }).rp?.id ?? "";
    const host = new URL(origin).hostname;
    expect(
      host === rpId || host.endsWith(`.${rpId}`),
      `служба выдаёт испытание для доверяющей стороны «${rpId}», а консоль открыта на ${host}`,
    ).toBe(true);

    const accepted = await finished;
    const text = await accepted.text();
    expect(text.includes(ORIGIN_NOT_ALLOWED), originRefusalMessage("приём результата церемонии", origin, text)).toBe(
      false,
    );
    expect(accepted.status(), `приём результата отвергнут: ${text.slice(0, 300)}`).toBe(200);

    // Происхождение подписал браузер консоли — это и есть «настоящий Origin».
    const sent = JSON.parse(accepted.request().postData() ?? "{}") as { credential?: { clientDataJson?: string } };
    const clientData = JSON.parse(Buffer.from(sent.credential?.clientDataJson ?? "", "base64").toString("utf8")) as {
      origin?: string;
    };
    expect(clientData.origin, "клиентские данные браузера несут не происхождение консоли").toBe(origin);

    const op = JSON.parse(text) as { id?: string; done?: boolean };
    expect(op.id ?? "", "ответ приёма результата — не Operation").not.toBe("");
    await operationSucceeded(readerOf(seed), op, "регистрация ключа экраном");

    await expect(
      region
        .getByRole("table")
        .getByRole("row")
        .filter({ hasNot: page.getByRole("columnheader") })
        .filter({ hasText: "key-ko-01" }),
      "раздел не показал заведённый ключ",
    ).toBeVisible({ timeout: 30_000 });
    expect(
      (await listedKeys(seed, userId)).map((k) => k.name),
      "перечень края не содержит ключ, заведённый экраном",
    ).toEqual(["key-ko-01"]);
  } finally {
    try {
      if (userId) await revokeAll(seed, userId, (await listedKeys(seed, userId)).map((k) => k.id));
    } finally {
      await seed.dispose();
    }
  }
});

test("KO-02 · регистрация с происхождением вне перечня отвергается ORIGIN_NOT_ALLOWED, а с происхождением консоли принята", async ({
  browserName: _browser,
}, testInfo) => {
  // verifies #3229 — предикат п.2: отрицательный близнец; меняется ровно одно
  // поле — происхождение в клиентских данных регистрации.
  const origin = consoleOrigin(testInfo);
  const seed = await newSeed(testInfo);
  let userId = "";
  try {
    userId = await seededHuman(seed, "KO-02");

    const own = await registerWith(seed, origin, "key-ko-02-own");
    expect(own.text.includes(ORIGIN_NOT_ALLOWED), originRefusalMessage("положительный контроль", origin, own.text)).toBe(
      false,
    );
    const op = await acceptedOperation(own.res, "положительный контроль: регистрация с происхождением консоли");
    await operationSucceeded(readerOf(seed), op, "положительный контроль: регистрация с происхождением консоли");

    const foreign = await registerWith(seed, FOREIGN_ORIGIN, "key-ko-02-foreign");
    expect(foreign.res.status(), `регистрация с происхождением ${FOREIGN_ORIGIN}: ${foreign.text.slice(0, 300)}`).toBe(
      400,
    );
    const body = JSON.parse(foreign.text) as Refusal;
    expect(
      { code: body.code, message: body.message, reasons: (body.details ?? []).map((d) => d.reason) },
      `регистрация с происхождением ${FOREIGN_ORIGIN}: отказ не назвал причину`,
    ).toEqual({ code: 3, message: ORIGIN_NOT_IN_LIST, reasons: [ORIGIN_NOT_ALLOWED] });

    expect(
      (await listedKeys(seed, userId)).map((k) => k.name),
      "после отвергнутой регистрации в перечне не только ключ контроля",
    ).toEqual(["key-ko-02-own"]);
  } finally {
    try {
      if (userId) await revokeAll(seed, userId, (await listedKeys(seed, userId)).map((k) => k.id));
    } finally {
      await seed.dispose();
    }
  }
});

const KEYS_TURNED_OFF =
  "Ключи доступа на этой установке выключены администратором";

test("KO-03 · раздел ключей согласован со службой: «выключены» ровно тогда, когда служба отвергает происхождение консоли", async ({
  page,
}, testInfo) => {
  // verifies #3229 — предикат п.4: консоль при объявленном «никого» не
  // предлагает регистрацию ключа; проба в обе стороны (перечень пуст / непуст),
  // объявление службы — её настоящим ответом экрану, без подстановки.
  test.setTimeout(120_000);
  const origin = consoleOrigin(testInfo);
  const seed = await newSeed(testInfo);
  let userId = "";
  try {
    userId = await seededHuman(seed, "KO-03");
    await transferSession(seed, page.context());

    const census = ceremonyCensus(page.context());
    const declared = answerTo(page, "GET", LANE.secondFactor);
    const s = await openSecurity(page);
    const answer = await declared;
    const answerText = await answer.text();
    expect(
      answer.status(),
      `объявление второго фактора: ${answer.status()} ${answerText.slice(0, 300)}`,
    ).toBe(200);
    const available = (
      JSON.parse(answerText) as { accessKeys?: { available?: unknown } }
    ).accessKeys?.available;
    expect(
      typeof available,
      `служба не объявила доступность ключей: ${answerText.slice(0, 300)}`,
    ).toBe("boolean");

    const add = s.keys.getByRole("button", {
      name: "Добавить ключ доступа",
      exact: true,
    });
    const own = await registerWith(seed, origin, "key-ko-03");
    if (available === false) {
      await expect(
        s.keys,
        "служба объявила «никого», а раздел не сказал об этом",
      ).toContainText(KEYS_TURNED_OFF);
      await expect(
        add,
        "служба объявила «никого», а консоль предлагает завести ключ",
      ).toHaveCount(0);
      expect(
        census.calls.filter((c) => c.path.endsWith(":beginRegistration")),
        "служба объявила «никого», а страница начала регистрацию ключа",
      ).toHaveLength(0);
      expect(
        {
          status: own.res.status(),
          refused: own.text.includes(ORIGIN_NOT_ALLOWED),
        },
        `служба объявила «никого», а регистрацию с происхождением консоли ${origin} не отвергла: ${own.text.slice(0, 300)}`,
      ).toEqual({ status: 400, refused: true });
    } else {
      await expect(
        add,
        "служба объявила ключи доступными, а консоль не предлагает завести ключ",
      ).toBeVisible();
      await expect(
        s.keys,
        "служба объявила ключи доступными, а раздел пишет «выключены»",
      ).not.toContainText(KEYS_TURNED_OFF);
      expect(
        own.text.includes(ORIGIN_NOT_ALLOWED),
        originRefusalMessage(
          "служба объявила ключи доступными",
          origin,
          own.text,
        ),
      ).toBe(false);
      const op = await acceptedOperation(
        own.res,
        "регистрация с происхождением консоли при объявленных ключах",
      );
      await operationSucceeded(
        readerOf(seed),
        op,
        "регистрация с происхождением консоли при объявленных ключах",
      );
    }
  } finally {
    try {
      if (userId)
        await revokeAll(
          seed,
          userId,
          (await listedKeys(seed, userId)).map((k) => k.id),
        );
    } finally {
      await seed.dispose();
    }
  }
});
