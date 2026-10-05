// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type APIResponse, type TestInfo } from "@playwright/test";
import {
  LANE,
  SESSION_IDENTITY,
  lastIssued,
  newSeed,
  seedSecondFactor,
  type Seed,
  type SeededSecondFactor,
} from "./ceremony-seed";
import { conditionNotCreated } from "./mail-receiver";

/**
 * Администратор облака стенда — «Дано» сквозных позиций, где действует
 * распорядитель уровня «2» над чужой (или своей) записью человека: блокировка
 * (Ф3-24) и одно-фактный близнец сброса второго фактора (Ф12-45 «б»).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ОТКУДА ЧЕЛОВЕК И ЕГО УДОСТОВЕРЕНИЕ
 *
 * Человека заводит шаг подъёма стенда `bootstrap-cloud-admin` путём продукта, а
 * удостоверение лежит в секрете стенда (kacho#2878, `deploy/README.md`,
 * «Администратор облака стенда»). Прогону его отдаёт тот, кто прогон запускает,
 * — переменными с ТЕМИ ЖЕ именами, что читает шаг подъёма. Умолчания нет: без
 * удостоверения позиции не исполняются, и это «условие не создано», а не
 * красное (`e2e-flow.md` §1). Чеканить своего администратора проба не может:
 * право `system_admin` на кластере выдаёт только посев бутстрапа службы. В
 * конвейере `console-e2e.yml` удостоверение отдаёт шаг проб из секрета, который
 * заводит шаг условия `.github/scripts/console-cloud-admin.sh`.
 *
 * ПОЧЕМУ ВТОРОЙ ФАКТОР ЗАВОДИТСЯ ПРОГОНОМ И СНИМАЕТСЯ ИМ ЖЕ
 *
 * Глаголы распорядителя несут пол «2» (`required_acr_min` каталога), а вход
 * паролем даёт «1». Фактор администратора заводит САМ прогон (заведение из
 * свежей сессии входа), и снимает его тоже прогон — `ResetSecondFactor` на
 * своей записи: администратор облака держит отношение и на ней (Ф12 Р10), и
 * этот вызов есть одно-фактный близнец Ф12-45 «б». Фактор, оставленный вне
 * прогона, — это запасные коды, которых у прогона нет: такой стенд условия не
 * создаёт, и проба говорит это, а не краснеет на чужом состоянии.
 */

export const CLOUD_ADMIN_EMAIL_ENV = "KACHO_CLOUD_ADMIN_EMAIL";
export const CLOUD_ADMIN_PASSWORD_ENV = "KACHO_CLOUD_ADMIN_PASSWORD";

export interface CloudAdmin {
  seed: Seed;
  userId: string;
  factor: SeededSecondFactor;
  /** Фактор уже снят своим `ResetSecondFactor` — сессии администратора погашены (Ф12-30). */
  released: boolean;
}

interface Operation {
  id?: string;
  done?: boolean;
  error?: { code?: number; message?: string };
  response?: Record<string, unknown>;
}

/** Срок операции глагола распорядителя: запись одной строки и событие — секунды, не минуты. */
const OPERATION_BUDGET_MS = 30_000;

/** Тело операции, принятой краем: `200` и непустой `id`; шаг утверждает свой исход. */
export async function acceptedOperation(res: APIResponse, subject: string): Promise<Operation> {
  const text = await res.text();
  expect(res.status(), `${subject}: край не принял мутацию — ${res.status()} ${text.slice(0, 300)}`).toBe(200);
  const op = JSON.parse(text) as Operation;
  expect(op.id ?? "", `${subject}: ответ мутации без идентификатора операции — ${text.slice(0, 300)}`).not.toBe("");
  return op;
}

/** Чтение операции: `200` и её тело либо статус отказа, как ответил край. */
type OperationReader = (path: string) => Promise<{ status: number; body: Operation | null }>;

/** Читатель операции носителем посева. */
export function readerOf(seed: Seed): OperationReader {
  return async (path) => {
    const res = await seed.read(path);
    return { status: res.status(), body: res.ok() ? ((await res.json()) as Operation) : null };
  };
}

/**
 * Дождаться завершения операции и утвердить её ИСХОД: `done=true` без `error`.
 * `done` — это долговечность, а не успех (`e2e-flow.md` §3, assert-operation-outcome).
 */
export async function operationSucceeded(
  read: OperationReader,
  accepted: Operation,
  subject: string,
): Promise<Operation> {
  let last: Operation = accepted;
  if (!accepted.done) {
    await expect
      .poll(
        async () => {
          const got = await read(`/operations/${accepted.id}`);
          if (got.status !== 200 || !got.body) return `опрос операции: ${got.status}`;
          last = got.body;
          return last.done === true ? "done" : "идёт";
        },
        { message: `${subject}: операция ${accepted.id} не завершилась`, timeout: OPERATION_BUDGET_MS },
      )
      .toBe("done");
  }
  expect(last.error, `${subject}: операция ${accepted.id} завершилась отказом — ${JSON.stringify(last.error)}`).toBe(
    undefined,
  );
  return last;
}

/** Новый вход администратора паролем своим контекстом; шаг утверждает исход. */
async function freshAdminSession(testInfo: TestInfo, who: string): Promise<Seed> {
  const seed = await newSeed(testInfo);
  const res = await seed.submit(LANE.login, "login", {
    email: process.env[CLOUD_ADMIN_EMAIL_ENV],
    password: process.env[CLOUD_ADMIN_PASSWORD_ENV],
  });
  expect(res.status(), `администратор: ${who} — вход паролем ${JSON.stringify(lastIssued(seed, LANE.login).body)}`).toBe(200);
  return seed;
}

/**
 * Администратор облака с сессией уровня «2». Каждый шаг утверждает свой исход; то,
 * что создаёт стенд, а не продукт, отказывает пометкой «условие не создано».
 */
export async function cloudAdminAtLevelTwo(testInfo: TestInfo): Promise<CloudAdmin> {
  const email = process.env[CLOUD_ADMIN_EMAIL_ENV] ?? "";
  const password = process.env[CLOUD_ADMIN_PASSWORD_ENV] ?? "";
  if (!email || !password) {
    conditionNotCreated(
      `условие не создано: удостоверение администратора облака стенда прогону не отдано ` +
        `(${CLOUD_ADMIN_EMAIL_ENV}, ${CLOUD_ADMIN_PASSWORD_ENV}; секрет стенда — kacho#2878)`,
    );
  }

  const seed = await newSeed(testInfo);
  const signedIn = await seed.submit(LANE.login, "login", { email, password });
  if (signedIn.status() !== 200) {
    await seed.dispose();
    conditionNotCreated(
      `условие не создано: удостоверение администратора облака из переменных прогона не входит — ` +
        `${signedIn.status()}; стенд его не завёл либо отдал другое`,
    );
  }

  const me = await seed.read(SESSION_IDENTITY);
  const view = lastIssued(seed, SESSION_IDENTITY).body as { user?: { id?: unknown } } | null;
  expect(me.status(), `администратор: «кто я» после входа — ${JSON.stringify(view)}`).toBe(200);
  const userId = String(view?.user?.id ?? "");
  expect(userId, "администратор: «кто я» не назвал идентификатор").not.toBe("");

  const status = await seed.read(LANE.secondFactor);
  const sf = lastIssued(seed, LANE.secondFactor).body as { totp?: { enrolled?: unknown } } | null;
  expect(status.status(), `администратор: состояние второго фактора — ${JSON.stringify(sf)}`).toBe(200);
  if (sf?.totp?.enrolled === true) {
    await seed.dispose();
    conditionNotCreated(
      "условие не создано: второй фактор администратора облака заведён вне прогона — " +
        "запасных кодов у прогона нет, пол «2» предъявить нечем",
    );
  }

  const factor = await seedSecondFactor(seed);
  expect(factor.assuranceLevel, "администратор: подтверждение фактора обязано поднять сессию до «2»").toBe("2");
  return { seed, userId, factor, released: false };
}

/**
 * Снять фактор администратора его собственным `ResetSecondFactor` — `200`,
 * операция без отказа, фактор не заведён. Это и уборка прогона, и
 * одно-фактный близнец Ф12-45 «б»: тот же вызов на своей записи, различие одно —
 * вызывающий держит отношение записи каталога.
 *
 * Сброс гасит все сессии человека (Ф12-30), поэтому состояние читается НОВЫМ
 * входом, а не прежним носителем.
 */
export async function resetOwnFactor(admin: CloudAdmin, testInfo: TestInfo): Promise<void> {
  const accepted = await acceptedOperation(
    await admin.seed.api.post(`/iam/v1/users/${admin.userId}:resetSecondFactor`, { data: {} }),
    "администратор: сброс своего фактора",
  );
  admin.released = true;
  // Сброс гасит сессии человека в момент исполнения, поэтому операция читается
  // СВЕЖИМ входом на каждом опросе: носитель, выданный до завершения, гаснет вместе
  // с прочими, и опрос получил бы 401 за свой же успех.
  await operationSucceeded(
    async (path) => {
      const seed = await freshAdminSession(testInfo, "опрос операции сброса");
      try {
        return await readerOf(seed)(path);
      } finally {
        await seed.dispose();
      }
    },
    accepted,
    "администратор: сброс своего фактора",
  );

  const after = await freshAdminSession(testInfo, "после сброса своего фактора");
  try {
    await after.read(LANE.secondFactor);
    const sf = lastIssued(after, LANE.secondFactor).body as { totp?: { enrolled?: unknown } } | null;
    expect(sf?.totp?.enrolled, `администратор: после сброса фактор обязан быть снят — ${JSON.stringify(sf)}`).toBe(
      false,
    );
  } finally {
    await after.dispose();
  }
}
