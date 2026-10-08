// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import {
  expect,
  type Page,
  type Request,
  type TestInfo,
} from "@playwright/test";
import {
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  transferSession,
  type Seed,
} from "./ceremony-seed";
import {
  acceptedOperation,
  cloudAdminAtLevelTwo,
  operationSucceeded,
  readerOf,
  resetOwnFactor,
  type CloudAdmin,
} from "./cloud-admin";
import { registerAndSignIn, runTag, test } from "./fixtures";

/**
 * Экраны регионов, зон и администраторов кластера раздела «Система» — на
 * ПУБЛИЧНЫХ службах администрирования (kacho#3094; geo — #3092, администраторы
 * кластера — край #3093).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРИЗНАК
 *
 * Экраны звали внутренние пути (`/geo/v1/internal/…`, `/iam/v1/internal/cluster…`),
 * которые внешний край не обслуживает. На посадке внешнего края администратор
 * облака этими экранами не мог сделать ничего: раздел называл себя недоступным
 * и кнопок мутаций не показывал, хотя край те же действия обслуживает
 * публичными службами.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ — то, что видит и получает человек у экрана
 *
 *   - администратор облака создаёт, правит и удаляет регион и зону ЧЕРЕЗ ЭКРАН:
 *     операция доходит до `done` без отказа, строка появляется в списке, правка
 *     видна в строке, после удаления строки нет;
 *   - администратор облака выдаёт и снимает администратора кластера через экран:
 *     строка человека появляется в перечне и исчезает из него;
 *   - арендатор без права нажимает ту же кнопку и получает отказ СЛОВАМИ
 *     (объяснение вердикта края), а не «маршрута нет»; региона не появляется
 *     (близнец первой позиции, различие одно — держит ли вызывающий право);
 *   - ни одно обращение страницы не уходит во внутреннее пространство путей.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ФИКСТУРА И УБОРКА
 *
 * Идентификаторы каталога несут метку прогона и начинаются с `zz-`: каталог
 * читается в порядке идентификатора, и соседние пробы берут из него первую
 * запись (`mutate.spec.ts`, `row-move-stub.spec.ts`). Запись, которую эта проба
 * заводит, обязана не стать чужой «первой» даже на миг. Заводится она закрытой
 * (`DOWN`, умолчание службы) — для размещения её нет.
 *
 * Всё, что проба заводит, она снимает в `finally` краем, с проверкой исхода:
 * зону, регион, выдачу прав администратора и второй фактор администратора
 * облака (`resetOwnFactor`). Провал уборки не прячет провал пробы и не
 * прячется за ним.
 */

/** Объяснение вердикта консоли по `AUTHZ_DENIED` — выписано ЗДЕСЬ, а не взято у консоли. */
const FORBIDDEN_EXPLANATION =
  "Этот раздел доступен администраторам платформы. Если доступ нужен по работе — запросите его у администратора вашей организации.";

/** Срок, за который строка мутации обязана появиться на экране: одна запись и её чтение. */
const ROW_BUDGET_MS = 60_000;

/** Обращения страницы к краю за время пробы — чтобы утвердить, что во внутреннее пространство не ходили. */
function edgeCalls(page: Page): {
  internal: () => string[];
  all: () => string[];
} {
  const seen: string[] = [];
  const onRequest = (req: Request) => {
    const url = new URL(req.url());
    if (
      url.pathname.startsWith("/geo/") ||
      url.pathname.startsWith("/iam/v1/") ||
      url.pathname.startsWith("/operations/")
    ) {
      seen.push(`${req.method()} ${url.pathname}`);
    }
  };
  page.on("request", onRequest);
  return {
    internal: () =>
      seen.filter((s) => s.includes("/internal/") || s.endsWith("/internal")),
    all: () => [...seen],
  };
}

/**
 * Проба с администратором облака уровня «2»: уборка — тело `cleanup` и снятие
 * фактора — идёт ВСЕГДА; её провал при зелёном теле — отказ пробы, при красном —
 * пометка рядом с первопричиной.
 */
async function withCloudAdmin(
  testInfo: TestInfo,
  body: (admin: CloudAdmin) => Promise<void>,
  cleanup: (admin: CloudAdmin) => Promise<void>,
): Promise<void> {
  const admin = await cloudAdminAtLevelTwo(testInfo);
  let failure: unknown = null;
  try {
    await body(admin);
  } catch (e) {
    failure = e;
  }
  for (const step of [
    () => cleanup(admin),
    () =>
      admin.released ? Promise.resolve() : resetOwnFactor(admin, testInfo),
  ]) {
    try {
      await step();
    } catch (e) {
      if (failure === null) failure = e;
      else
        testInfo.annotations.push({
          type: "уборка не прошла",
          description: String(e),
        });
    }
  }
  await admin.seed.dispose();
  if (failure !== null) throw failure;
}

/** Снять запись каталога краем, если она есть; исход операции утверждается. */
async function removeGeo(
  seed: Seed,
  path: string,
  subject: string,
): Promise<void> {
  const probe = await seed.read(path);
  if (probe.status() === 404) return;
  expect(
    probe.status(),
    `уборка: чтение ${subject} — ${probe.status()} ${(await probe.text()).slice(0, 200)}`,
  ).toBe(200);
  const op = await acceptedOperation(
    await seed.api.delete(path),
    `уборка: удаление ${subject}`,
  );
  await operationSucceeded(readerOf(seed), op, `уборка: удаление ${subject}`);
}

/** Строка ресурса в таблице списка — по вхождению идентификатора. */
function rowOf(page: Page, id: string) {
  return page.locator("tr").filter({ hasText: id }).first();
}

/** Открыть меню строки и выбрать пункт. */
async function rowAction(
  page: Page,
  id: string,
  item: "Редактировать" | "Удалить",
): Promise<void> {
  const row = rowOf(page, id);
  await expect(
    row,
    `строки ${id} нет в списке — действие открывать не над чем`,
  ).toBeVisible({ timeout: ROW_BUDGET_MS });
  await row.getByRole("button", { name: "Действия" }).first().click();
  const entry = page.getByRole("menuitem", { name: item }).first();
  await expect(entry, `в меню строки ${id} нет «${item}»`).toBeVisible({
    timeout: 15_000,
  });
  await entry.click();
}

/** Подтвердить удаление в окне и дождаться, что строки больше нет. */
async function confirmDelete(
  page: Page,
  id: string,
  subject: string,
): Promise<void> {
  const dialog = page.getByRole("dialog").filter({ hasText: id });
  await expect(
    dialog,
    `окна подтверждения удаления ${subject} нет`,
  ).toBeVisible({ timeout: 15_000 });
  await dialog.getByRole("button", { name: "Удалить" }).click();
  await expect(
    rowOf(page, id),
    `${subject} удалён(а) через экран, а строка осталась в списке`,
  ).toHaveCount(0, {
    timeout: ROW_BUDGET_MS,
  });
}

/** Кнопка «Создать» экрана: её отсутствие и есть признак находки. */
async function pressCreate(page: Page, screen: string): Promise<void> {
  const create = page.getByRole("link", { name: /Создать/ }).first();
  await expect(
    create,
    `на экране «${screen}» нет кнопки «Создать» — администратор облака не может завести запись, хотя край обслуживает мутацию публичной службой`,
  ).toBeVisible({ timeout: 30_000 });
  await create.click();
}

/** Нажать «Создать»/«Сохранить» формы и дождаться ответа края на мутацию. */
async function submitForm(
  page: Page,
  label: "Создать" | "Сохранить",
  method: string,
  pathPrefix: string,
) {
  const [res] = await Promise.all([
    page.waitForResponse(
      (r) =>
        r.request().method() === method &&
        new URL(r.url()).pathname.startsWith(pathPrefix),
      {
        timeout: 40_000,
      },
    ),
    page.getByRole("button", { name: label, exact: true }).last().click(),
  ]);
  return res;
}

/** Выбрать значение в поле-списке, открытом по его видимой подписи. */
async function choose(
  page: Page,
  shown: string,
  typed: string,
  option: RegExp,
): Promise<void> {
  // Поле-список открывается своим вводом: видимая подпись лежит ПОД ним, и
  // нажатие на неё перехватывает сам ввод — его и нажимаем, найдя по подписи
  // рядом (общий родитель подписи и ввода).
  const input = page
    .getByText(shown, { exact: true })
    .first()
    .locator("xpath=..")
    .getByRole("combobox");
  await expect(
    input,
    `поля-списка с подписью «${shown}» нет на форме`,
  ).toBeVisible({ timeout: 20_000 });
  await input.click();
  await page.keyboard.type(typed);
  await expect(
    page.getByRole("option", { name: option }).first(),
    `в списке нет варианта ${option}`,
  ).toBeAttached({
    timeout: 20_000,
  });
  await page.keyboard.press("Enter");
}

/** Завести регион экраном регионов и дождаться его строки в списке. */
async function createRegionOnScreen(
  page: Page,
  regionId: string,
): Promise<void> {
  await page.goto("/system/regions", { waitUntil: "domcontentloaded" });
  await pressCreate(page, "Регионы");
  await page.getByPlaceholder("region-1", { exact: true }).fill(regionId);
  const created = await submitForm(page, "Создать", "POST", "/geo/v1/regions");
  expect(
    created.status(),
    `край не принял создание региона: ${await created.text()}`,
  ).toBe(200);
  await expect(
    rowOf(page, regionId),
    "регион создан через экран, а строки в списке нет",
  ).toBeVisible({
    timeout: ROW_BUDGET_MS,
  });
}

test("регионы: администратор облака создаёт, правит и удаляет регион через экран", async ({
  page,
}, testInfo) => {
  // verifies #3094 — экран регионов на публичной RegionService (#3092): создание, правка, удаление.
  test.setTimeout(300_000);
  const regionId = `zz-e2e-${runTag()}`;
  await withCloudAdmin(
    testInfo,
    async (admin) => {
      await transferSession(admin.seed, page.context());
      const calls = edgeCalls(page);

      await createRegionOnScreen(page, regionId);

      await rowAction(page, regionId, "Редактировать");
      const country = page.getByPlaceholder("RU", { exact: true });
      await expect(
        country,
        "форма правки региона без поля кода страны",
      ).toBeVisible({ timeout: 30_000 });
      await country.fill("KZ");
      const updated = await submitForm(
        page,
        "Сохранить",
        "PATCH",
        `/geo/v1/regions/${regionId}`,
      );
      expect(
        updated.status(),
        `край не принял правку региона: ${await updated.text()}`,
      ).toBe(200);
      await page.goto("/system/regions", { waitUntil: "domcontentloaded" });
      await expect(
        rowOf(page, regionId),
        "правка кода страны не видна в строке региона",
      ).toContainText("KZ", {
        timeout: ROW_BUDGET_MS,
      });

      await rowAction(page, regionId, "Удалить");
      await confirmDelete(page, regionId, `регион ${regionId}`);

      // Исход — у края, а не только на экране: записи нет.
      expect(
        (await admin.seed.read(`/geo/v1/regions/${regionId}`)).status(),
        "регион удалён экраном, а край его читает",
      ).toBe(404);
      expect(
        calls.internal(),
        `страница ходила во внутреннее пространство путей:\n${calls.all().join("\n")}`,
      ).toEqual([]);
    },
    (admin) =>
      removeGeo(
        admin.seed,
        `/geo/v1/regions/${regionId}`,
        `регион ${regionId}`,
      ),
  );
});

test("зоны: администратор облака создаёт, правит и удаляет зону через экран", async ({
  page,
}, testInfo) => {
  // verifies #3094 — экран зон на публичной ZoneService (#3092): создание, правка состояния, удаление.
  test.setTimeout(300_000);
  const regionId = `zz-e2e-${runTag()}`;
  const zoneId = `${regionId}-a`;
  await withCloudAdmin(
    testInfo,
    async (admin) => {
      await transferSession(admin.seed, page.context());
      const calls = edgeCalls(page);

      // Регион зоны заводится тем же экраном регионов: отдельного пути заведения
      // у пробы нет, и тело мутации собирает форма консоли, а не проба.
      await createRegionOnScreen(page, regionId);

      await page.goto("/system/zones", { waitUntil: "domcontentloaded" });
      await pressCreate(page, "Зоны");
      await page.getByPlaceholder("region-1-a", { exact: true }).fill(zoneId);
      await choose(page, "Выбрать регион…", regionId, new RegExp(regionId));
      const created = await submitForm(
        page,
        "Создать",
        "POST",
        "/geo/v1/zones",
      );
      expect(
        created.status(),
        `край не принял создание зоны: ${await created.text()}`,
      ).toBe(200);
      const row = rowOf(page, zoneId);
      await expect(
        row,
        "зона создана через экран, а строки в списке нет",
      ).toBeVisible({ timeout: ROW_BUDGET_MS });
      // Свежая зона закрыта своим состоянием (умолчание службы — DOWN).
      await expect(
        row,
        "свежая зона не названа закрытой своим состоянием",
      ).toContainText("Зона выведена из обслуживания администратором", {
        timeout: ROW_BUDGET_MS,
      });

      // Правка: зона открывается своим состоянием; регион закрыт — причина
      // закрытости в строке меняется с зоны на регион.
      await rowAction(page, zoneId, "Редактировать");
      await choose(page, "DOWN — закрыт для размещения", "UP", /^UP/);
      const updated = await submitForm(
        page,
        "Сохранить",
        "PATCH",
        `/geo/v1/zones/${zoneId}`,
      );
      expect(
        updated.status(),
        `край не принял правку зоны: ${await updated.text()}`,
      ).toBe(200);
      await page.goto("/system/zones", { waitUntil: "domcontentloaded" });
      await expect(
        rowOf(page, zoneId),
        "правка состояния зоны не видна в строке",
      ).toContainText("Регион выведен из обслуживания администратором", {
        timeout: ROW_BUDGET_MS,
      });

      await rowAction(page, zoneId, "Удалить");
      await confirmDelete(page, zoneId, `зона ${zoneId}`);
      expect(
        (await admin.seed.read(`/geo/v1/zones/${zoneId}`)).status(),
        "зона удалена экраном, а край её читает",
      ).toBe(404);
      expect(
        calls.internal(),
        `страница ходила во внутреннее пространство путей:\n${calls.all().join("\n")}`,
      ).toEqual([]);
    },
    async (admin) => {
      await removeGeo(admin.seed, `/geo/v1/zones/${zoneId}`, `зона ${zoneId}`);
      await removeGeo(
        admin.seed,
        `/geo/v1/regions/${regionId}`,
        `регион ${regionId}`,
      );
    },
  );
});

interface AdminEntry {
  subjectId?: string;
  subjectEmail?: string;
}

/** Выдачи администратора кластера человеку `email` — по ответу края. */
async function grantsOf(seed: Seed, email: string): Promise<AdminEntry[]> {
  const res = await seed.read("/iam/v1/cluster/admins");
  expect(
    res.status(),
    `перечень администраторов кластера: ${res.status()} ${(await res.text()).slice(0, 200)}`,
  ).toBe(200);
  const body = (await res.json()) as { admins?: AdminEntry[] };
  return (body.admins ?? []).filter((a) => a.subjectEmail === email);
}

test("администраторы кластера: администратор облака выдаёт и снимает права через экран", async ({
  page,
}, testInfo) => {
  // verifies #3094 — экран администраторов кластера на публичной ClusterService (край #3093): выдача и снятие.
  test.setTimeout(300_000);
  const humanSeed = await newSeed(testInfo);
  const email = seedAddress("cluster-admin-grantee");
  try {
    await seedConfirmedHuman(humanSeed, email);
    await withCloudAdmin(
      testInfo,
      async (admin) => {
        await transferSession(admin.seed, page.context());
        const calls = edgeCalls(page);

        await page.goto("/system/cluster/admins", {
          waitUntil: "domcontentloaded",
        });
        // Имя кнопки начинается подписью значка — сверяется окончание.
        const grant = page.getByRole("button", {
          name: /Добавить администратора$/,
        });
        await expect(
          grant,
          "на экране администраторов кластера нет кнопки выдачи — край обслуживает её публичной службой",
        ).toBeVisible({ timeout: 30_000 });
        await grant.click();

        const modal = page.getByRole("dialog", {
          name: /Выдать права администратора кластера/,
        });
        await expect(modal, "окно выдачи прав не открылось").toBeVisible({
          timeout: 15_000,
        });
        // Почта набирается клавишами, как её набирает человек: поле с
        // подсказками ведёт свой выбор по вводу, а не по подставленному
        // значению (подставленное `fill` оставляет вариант невыбираемым —
        // проверено на том же компоненте в браузере).
        await modal.getByRole("combobox").click();
        await page.keyboard.type(email);
        // Вариант называет человека почтой — её и видит администратор в списке.
        const option = page.getByText(email, { exact: true }).first();
        await expect(
          option,
          `человека ${email} нет среди вариантов выдачи`,
        ).toBeVisible({ timeout: 30_000 });
        // Выбор — нажатием на вариант. Окно после выбора обязано остаться
        // открытым и назвать выбранного (строка «ID»).
        await option.click();
        await expect(
          modal.getByText("ID", { exact: true }),
          "после выбора человека окно выдачи не назвало его — выбор не состоялся либо окно закрылось",
        ).toBeVisible({ timeout: 15_000 });
        const [granted] = await Promise.all([
          page.waitForResponse(
            (r) =>
              r.request().method() === "POST" &&
              new URL(r.url()).pathname.startsWith("/iam/v1/cluster/admins"),
            {
              timeout: 40_000,
            },
          ),
          modal.getByRole("button", { name: "Выдать" }).click(),
        ]);
        expect(
          granted.status(),
          `край не принял выдачу: ${await granted.text()}`,
        ).toBe(200);
        const row = rowOf(page, email);
        await expect(
          row,
          "права выданы через экран, а строки человека в перечне нет",
        ).toBeVisible({ timeout: ROW_BUDGET_MS });

        const [entry] = await grantsOf(admin.seed, email);
        expect(
          entry?.subjectId ?? "",
          "край не называет выдачу человеку",
        ).not.toBe("");
        await row
          .getByTestId(`cluster-admins-revoke-${entry!.subjectId}`)
          .click();
        await page
          .getByRole("button", { name: "Отозвать", exact: true })
          .click();
        await expect(
          rowOf(page, email),
          "права сняты через экран, а строка человека осталась в перечне",
        ).toHaveCount(0, {
          timeout: ROW_BUDGET_MS,
        });
        expect(
          await grantsOf(admin.seed, email),
          "права сняты экраном, а край их называет",
        ).toEqual([]);
        expect(
          calls.internal(),
          `страница ходила во внутреннее пространство путей:\n${calls.all().join("\n")}`,
        ).toEqual([]);
      },
      async (admin) => {
        for (const g of await grantsOf(admin.seed, email)) {
          const op = await acceptedOperation(
            await admin.seed.api.delete(
              `/iam/v1/cluster/admins/${encodeURIComponent(g.subjectId ?? "")}`,
            ),
            `уборка: снятие прав ${email}`,
          );
          await operationSucceeded(
            readerOf(admin.seed),
            op,
            `уборка: снятие прав ${email}`,
          );
        }
      },
    );
  } finally {
    await humanSeed.dispose();
  }
});

test("регионы и администраторы кластера: арендатор без права получает отказ словами, а не «маршрута нет»", async ({
  page,
}) => {
  // verifies #3094 — близнец позиции регионов: различие одно — вызывающий не держит system_admin на кластере.
  test.setTimeout(180_000);
  await registerAndSignIn(page);
  const regionId = `zz-e2e-${runTag()}`;
  const calls = edgeCalls(page);

  await page.goto("/system/regions", { waitUntil: "domcontentloaded" });
  await pressCreate(page, "Регионы");
  await page.getByPlaceholder("region-1", { exact: true }).fill(regionId);
  const refused = await submitForm(page, "Создать", "POST", "/geo/v1/regions");
  expect(
    refused.status(),
    `ответ края арендатору без права: ${await refused.text()}`,
  ).toBe(403);
  await expect(
    page.getByText(FORBIDDEN_EXPLANATION, { exact: false }).first(),
    "отказ края не назван словами — объяснением вердикта AUTHZ_DENIED",
  ).toBeVisible({ timeout: 30_000 });
  // Экран администраторов кластера: перечня арендатору край не отдаёт — экран
  // говорит это словами и выдать права не предлагает.
  await page.goto("/system/cluster/admins", { waitUntil: "domcontentloaded" });
  await expect(
    page.getByText("Недостаточно прав для просмотра администраторов облака.", {
      exact: true,
    }),
    "экран администраторов кластера не назвал отказ края словами",
  ).toBeVisible({ timeout: 30_000 });
  await expect(
    page.getByRole("button", { name: /Добавить администратора$/ }),
    "арендатору без права экран предлагает выдачу прав",
  ).toHaveCount(0);

  expect(
    (await page.request.get(`/geo/v1/regions/${regionId}`)).status(),
    "отказ края, а регион заведён",
  ).toBe(404);
  expect(
    calls.internal(),
    `страница ходила во внутреннее пространство путей:\n${calls.all().join("\n")}`,
  ).toEqual([]);
});
