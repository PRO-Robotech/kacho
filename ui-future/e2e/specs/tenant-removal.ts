// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type APIResponse, type TestInfo } from "@playwright/test";
import {
  LANE,
  SEED_PASSWORD,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedSecondFactor,
  type Seed,
} from "./ceremony-seed";
import { acceptedOperation, operationSucceeded, readerOf } from "./cloud-admin";

/**
 * Снятие тестового арендатора, заведённого `registerAndSignIn` (kacho#3106).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Проба приносит своё и убирает за собой (`e2e-flow.md` §5, probe-brings-own):
 * арендатор, оставленный на стенде, делает вердикт следующих прогонов функцией
 * их порядка, а стенд — свалкой, которую никто не переписывает. Прежде снять
 * его было нечем: строка человека и его личный аккаунт держали друг друга
 * ключами `ON DELETE RESTRICT`. Цикл разорван службой доступа (kaname#664,
 * приёмка «own-account-and-identity-are-removed-in-order»): арендатор уходит
 * ТРЕМЯ существующими глаголами по порядку — проект, аккаунт, личность (KN664-50).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПУТЬ — ТОТ, ЧТО У АРЕНДАТОРА, А НЕ ОБХОД
 *
 *   1. Новый вход арендатора паролем своим контекстом (`newSeed`): страница
 *      пробы к разбору уже закрыта, а носитель, выданный ей, мог смениться.
 *   2. Ресурсы платформы, заведённые пробой в его проекте, — в обратном порядке
 *      заведения (подсеть раньше сети, репозиторий раньше реестра). Каскада
 *      через границу службы нет (ban #4): непустой проект служба доступа не
 *      снимает, и отказ называет это словами.
 *   3. Второй фактор: снятие проекта и аккаунта несёт пол уверенности «2», а
 *      вход паролем даёт «1». Фактора нет — он заводится здесь же. Фактор уже
 *      заведён самой пробой (`raiseAssurance`) — второй раз его не завести
 *      (служба отвечает `SECOND_FACTOR_ALREADY_ENROLLED`, kacho#3247), и уровень
 *      поднимается повышением ЗАПАСНЫМ кодом, который проба записала при
 *      заведении. Фактор, заведённый мимо записи, — отказ уборки с этим текстом,
 *      а не догадка. Это путь арендатора, а не послабление.
 *   4. Проект → аккаунт → личность; исход каждой операции утверждается чтением
 *      (`done` без `error`). Снятие личности гасит сессии человека каскадом,
 *      поэтому её исход читается отказом края «кто я» на том же носителе — он
 *      пишется только когда строки человека больше нет.
 *
 * Каждый шаг утверждает свой исход и называет арендатора: провал уборки не
 * прячется за провалом пробы и не прячет его.
 *
 * ПОЧЕМУ ЗДЕСЬ, А НЕ В `fixtures.ts`. Путь стоит на посеве (`ceremony-seed.ts`)
 * и операциях распорядителя (`cloud-admin.ts`); фикстуры зовут его разбором
 * пробы. Кольца импорта нет: посев берёт величины арендатора у листа
 * `run-identity.ts`, а не у фикстур.
 */

/** Арендатор пробы и ресурсы платформы, которые проба завела в его проекте. */
export interface TenantRecord {
  email: string;
  projectId: string;
  /** Адреса ресурсов в порядке заведения; снимаются в обратном. */
  resources: string[];
  /**
   * Неизрасходованные запасные коды второго фактора, который завела сама проба
   * (`raiseAssurance`); пусто — проба фактора не заводила. Код одноразовый:
   * уборка снимает с начала тот, который предъявила.
   */
  backupCodes: string[];
}

/** Ответ края о втором факторе человека — заведён ли одноразовый код. */
interface SecondFactorView {
  totp?: { enrolled?: unknown };
}

/**
 * Поднять сессию уборки до «2». Фактора нет — заводится здесь же; заведён
 * пробой — повышение её запасным кодом (заводить второй раз нечего: служба
 * отвечает `SECOND_FACTOR_ALREADY_ENROLLED`, kacho#3247). Уровень утверждается
 * ответом края о сессии, а не ответом глагола.
 */
async function raiseToTwo(
  seed: Seed,
  who: string,
  tenant: TenantRecord,
): Promise<void> {
  const status = await seed.read(LANE.secondFactor);
  const sf = lastIssued(seed, LANE.secondFactor)
    .body as SecondFactorView | null;
  expect(
    status.status(),
    `${who}: состояние второго фактора не прочитано — ${JSON.stringify(sf)}`,
  ).toBe(200);

  if (sf?.totp?.enrolled !== true) {
    const factor = await seedSecondFactor(seed);
    expect(
      factor.assuranceLevel,
      `${who}: подтверждение второго фактора обязано поднять сессию до «2»`,
    ).toBe("2");
    return;
  }

  const code = tenant.backupCodes.shift();
  expect(
    code,
    `${who}: второй фактор арендатора заведён, а запасных кодов у уборки нет — ` +
      "фактор заведён мимо записи пробы (`raiseAssurance` пишет их в запись арендатора)",
  ).toBeTruthy();
  const raised = await seed.submit(LANE.stepUp, "step-up", {
    method: "lookup_secret",
    code: code!,
  });
  expect(
    raised.status(),
    `${who}: повышение запасным кодом отвергнуто — ${JSON.stringify(lastIssued(seed, LANE.stepUp).body)}`,
  ).toBe(200);
  const me = await seed.read(SESSION_IDENTITY);
  const view = lastIssued(seed, SESSION_IDENTITY).body as {
    session?: { assuranceLevel?: unknown };
  } | null;
  expect(
    me.status(),
    `${who}: «кто я» после повышения — ${JSON.stringify(view)}`,
  ).toBe(200);
  expect(
    String(view?.session?.assuranceLevel ?? ""),
    `${who}: повышение запасным кодом обязано поднять сессию до «2»`,
  ).toBe("2");
}

interface ProjectView {
  id?: string;
  accountId?: string;
}

/** Операция снятия по адресу: `200`, операция без отказа. `404` — ресурса уже нет, снимать нечего. */
async function removeAt(
  seed: Seed,
  address: string,
  subject: string,
): Promise<"снят" | "уже снят"> {
  const res: APIResponse = await seed.api.delete(address);
  if (res.status() === 404) return "уже снят";
  const accepted = await acceptedOperation(res, subject);
  await operationSucceeded(readerOf(seed), accepted, subject);
  return "снят";
}

async function accountOf(
  seed: Seed,
  who: string,
  projectId: string,
): Promise<string> {
  if (projectId !== "") {
    const res = await seed.read(`/iam/v1/projects/${projectId}`);
    const view = lastIssued(seed, `/iam/v1/projects/${projectId}`)
      .body as ProjectView | null;
    expect(
      res.status(),
      `${who}: проект ${projectId} не читается — ${JSON.stringify(view)}`,
    ).toBe(200);
    const accountId = view?.accountId ?? "";
    expect(
      accountId,
      `${who}: проект ${projectId} не назвал свой аккаунт`,
    ).not.toBe("");
    return accountId;
  }
  const res = await seed.read("/iam/v1/accounts");
  const body = lastIssued(seed, "/iam/v1/accounts").body as {
    accounts?: Array<{ id?: string }>;
  } | null;
  expect(
    res.status(),
    `${who}: перечень аккаунтов не прочитан — ${JSON.stringify(body)}`,
  ).toBe(200);
  const accounts = (body?.accounts ?? [])
    .map((a) => a.id ?? "")
    .filter((id) => id !== "");
  expect(
    accounts.length,
    `${who}: у арендатора без проекта ровно один личный аккаунт — ${JSON.stringify(body)}`,
  ).toBe(1);
  return accounts[0];
}

async function projectsOf(
  seed: Seed,
  who: string,
  accountId: string,
): Promise<string[]> {
  const path = `/iam/v1/projects?accountId=${encodeURIComponent(accountId)}`;
  const res = await seed.read(path);
  const body = lastIssued(seed, path).body as {
    projects?: Array<{ id?: string }>;
    nextPageToken?: string;
  } | null;
  expect(
    res.status(),
    `${who}: проекты аккаунта ${accountId} не прочитаны — ${JSON.stringify(body)}`,
  ).toBe(200);
  expect(
    body?.nextPageToken ?? "",
    `${who}: проектов аккаунта ${accountId} больше одной страницы`,
  ).toBe("");
  return (body?.projects ?? [])
    .map((p) => p.id ?? "")
    .filter((id) => id !== "");
}

/** Снять арендатора целиком; каждый шаг утверждает исход и называет арендатора. */
export async function removeTenant(
  tenant: TenantRecord,
  testInfo: TestInfo,
): Promise<void> {
  const who = `уборка арендатора ${tenant.email}`;
  const seed = await newSeed(testInfo);
  try {
    const signedIn = await seed.submit(LANE.login, "login", {
      email: tenant.email,
      password: SEED_PASSWORD,
    });
    expect(
      signedIn.status(),
      `${who}: вход паролем для уборки — ${JSON.stringify(lastIssued(seed, LANE.login).body)}`,
    ).toBe(200);

    const me = await seed.read(SESSION_IDENTITY);
    const view = lastIssued(seed, SESSION_IDENTITY).body as {
      user?: { id?: unknown };
    } | null;
    expect(
      me.status(),
      `${who}: «кто я» после входа — ${JSON.stringify(view)}`,
    ).toBe(200);
    const userId = String(view?.user?.id ?? "");
    expect(userId, `${who}: «кто я» не назвал идентификатор человека`).not.toBe(
      "",
    );

    for (const address of [...tenant.resources].reverse()) {
      await removeAt(seed, address, `${who}: ресурс ${address}`);
    }

    await raiseToTwo(seed, who, tenant);

    const accountId = await accountOf(seed, who, tenant.projectId);
    for (const projectId of await projectsOf(seed, who, accountId)) {
      await removeAt(
        seed,
        `/iam/v1/projects/${projectId}`,
        `${who}: проект ${projectId}`,
      );
    }
    await removeAt(
      seed,
      `/iam/v1/accounts/${accountId}`,
      `${who}: аккаунт ${accountId}`,
    );

    // Личность: снятие строки человека гасит его сессии каскадом, поэтому исход
    // читается отказом края на том же носителе, а не чтением операции им же.
    const accepted = await acceptedOperation(
      await seed.api.delete(`/iam/v1/users/${userId}`),
      `${who}: личность`,
    );
    if (accepted.done === true) {
      expect(
        accepted.error,
        `${who}: снятие личности завершилось отказом — ${JSON.stringify(accepted.error)}`,
      ).toBe(undefined);
    }
    await expect
      .poll(async () => (await seed.read(SESSION_IDENTITY)).status(), {
        message:
          `${who}: после снятия личности ${userId} (операция ${accepted.id}) край по-прежнему признаёт её сессию — ` +
          "строка человека не снята либо отказала",
        timeout: 30_000,
      })
      .toBe(401);
  } finally {
    await seed.dispose();
  }
}
