// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { test, expect, type Page, type Locator } from "@playwright/test";
import { tenantWithProject, runTag } from "./fixtures";

/**
 * Формы консоли и то, чего в продукте быть не должно.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЗДЕСЬ УТВЕРЖДАЕТСЯ И ПОЧЕМУ ИМЕННО ТАК
 *
 * Канон консоли (`docs/console-design-canon.md`, §8 «Формы» и §9 «Чего в
 * продукте не должно быть») формулирует правила о том, ЧТО ВИДИТ И ПОЛУЧАЕТ
 * пользователь. Поэтому здесь не проверяется ни один класс, ни одно имя
 * компонента и ни один проп: утверждение о разметке переживает свой предмет —
 * компонент заменят, класс переименуют, проба останется зелёной, а находка
 * вернётся. Утверждения ниже сделаны о положении на экране (координаты подписей
 * и сообщений), о тексте, который читает человек, и о запросах, которые
 * страница делает сама.
 *
 * ПОЧЕМУ ПОРЯДОК ПОЛЕЙ МЕРЯЕТСЯ ПО ВСЕМ ФОРМАМ СРАЗУ, А НЕ ПО ОДНОЙ
 *
 * Решение владельца — «имя → описание → метки, затем черта, затем поля самого
 * ресурса» — есть свойство ПРОДУКТА, а не отдельной формы: рука пользователя
 * идёт к одному и тому же месту на соседних формах, и разойтись им нельзя.
 * Проба на одной форме зеленела бы при семнадцати разошедшихся; поэтому обход
 * идёт по всем формам создания, доступным арендатору, и сверяет их ДРУГ С
 * ДРУГОМ, а не с выписанным здесь образцом.
 *
 * ПОЧЕМУ ОТРИЦАНИЯ СТОЯТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ
 *
 * «Кнопки „Обновить“ нет» одинаково зелено на исправной консоли и на пустой
 * странице, не загрузившей ничего. Каждое отрицание здесь сопровождается
 * измерением, доказывающим, что предмет вообще есть: у списка нашлись его
 * ручки, список ОПРАШИВАЕТСЯ САМ (это видно по повторным запросам без участия
 * человека), у формы нашлись поля ресурса, у таблицы — строки и меню действий.
 */

/** Общие поля, чей порядок владелец задал один на все формы создания. */
const COMMON_FIELDS = ["Имя", "Описание", "Метки"];

/**
 * Формы создания, доступные арендатору проекта.
 *
 * Перечень выписан, и цена этого названа: новый ресурс в него не попадёт сам, и
 * его форма останется без пробы. Вывести перечень из дерева проба не может —
 * реестр ресурсов живёт в модулях консоли, которые на стенде исполняются
 * СОБРАННЫМИ, а браузер их объявлений не показывает. Взамен обход намеренно
 * широк: пять модулей и семнадцать форм, то есть все, до которых у арендатора
 * есть адрес.
 */
const CREATE_FORMS = [
  "vpc/networks",
  "vpc/subnets",
  "vpc/addresses",
  "vpc/route-tables",
  "vpc/security-groups",
  "vpc/network-interfaces",
  "vpc/gateways",
  "vpc/cidr-groups",
  "compute/instances",
  "compute/placement-groups",
  "storage/volumes",
  "storage/snapshots",
  "storage/images",
  "nlb/load-balancers",
  "nlb/listeners",
  "nlb/target-groups",
  "registry/registries",
];

/** Подпись поля, как её читает человек: звёздочка обязательности — не часть имени. */
function fieldName(text: string): string {
  return text.replace(/[\s*\u00A0]+$/u, "").trim();
}

/**
 * formLabels — подписи полей формы СВЕРХУ ВНИЗ, как их видит глаз.
 *
 * Порядок берётся по вертикальной координате, а не по порядку в разметке: это
 * разные вещи (сетка вправе переставить), а канон говорит именно о том, что
 * читают сверху вниз. Внутрь попадают только подписи ПОЛЕЙ формы — переключатели
 * внутри поля («Публичный (авто)» у адреса балансировщика) тоже размечены как
 * подписи, но полем не являются и стояли бы в перечне лишними строками.
 */
async function formLabels(page: Page): Promise<Array<{ name: string; y: number; x: number }>> {
  return await page.evaluate(() => {
    const form = document.querySelector("form.ant-form");
    if (!form) return [];
    return Array.from(form.querySelectorAll(".ant-form-item-label label"))
      .map((l) => {
        const r = l.getBoundingClientRect();
        return { name: (l.textContent ?? "").trim(), y: Math.round(r.y), x: Math.round(r.x) };
      })
      .sort((a, b) => a.y - b.y);
  });
}

/** Форма создания загружена, когда у неё есть подпись первого поля и кнопка отправки. */
async function openCreateForm(page: Page, projectId: string, resource: string): Promise<void> {
  await page.goto(`/projects/${projectId}/${resource}/create`, { waitUntil: "domcontentloaded" });
  await expect(
    page.locator("form.ant-form"),
    `форма создания «${resource}» не отрисовалась: дальше проверялась бы пустая страница, ` +
      `а не порядок полей`,
  ).toBeVisible({ timeout: 45_000 });
  await expect
    .poll(async () => (await formLabels(page)).length, {
      message:
        `у формы создания «${resource}» не появилось ни одной подписи поля — вердикт о ` +
        `порядке полей на такой странице был бы вердиктом ни о чём`,
      timeout: 45_000,
    })
    .toBeGreaterThan(0);
}

/** Ручки, которыми на странице списка ЧТО-ТО делают: ими доказывается, что страница жива. */
function listControls(page: Page): Locator {
  return page
    .locator(".app-main")
    .getByRole("button")
    .filter({ hasText: /^(Создать|Столбцы|Пригласить|Выпустить)/ });
}

/**
 * refreshControls — любое предложение обновить список руками.
 *
 * Ловится ТРЕМЯ формами сразу, потому что кнопка бывает без подписи: текстом
 * («Обновить», «Обновить список»), доступным именем и рисунком круговой стрелки.
 * Проба, знающая одну форму, зеленела бы на двух других — а пользователь видит
 * их одинаково.
 */
function refreshControls(page: Page): Locator {
  return page.locator(
    '.app-main button:has-text("Обновить"), ' +
      '.app-main button[aria-label*="бнов"], ' +
      '.app-main button[title*="бнов"], ' +
      ".app-main button .anticon-reload",
  );
}

test("порядок полей един на всех формах создания: имя → описание → метки", async ({ page }) => {
  // verifies #925
  test.setTimeout(300_000);
  const { projectId } = await tenantWithProject(page);

  const captured: Array<{ resource: string; common: string[]; total: number }> = [];

  for (const resource of CREATE_FORMS) {
    await openCreateForm(page, projectId, resource);
    const labels = (await formLabels(page)).map((lbl) => fieldName(lbl.name));
    const common = labels.filter((lbl) => COMMON_FIELDS.includes(lbl));

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него «общие поля идут первыми» верно и для
    // формы, у которой вообще нет других полей, — то есть утверждение о порядке
    // делается там, где порядка не существует.
    expect(
      labels.length,
      `форма «${resource}»: поля ресурса не показаны (подписи: ${JSON.stringify(labels)}). ` +
        `Утверждать порядок «сначала общие, потом свои» не о чем`,
    ).toBeGreaterThan(common.length);

    // Общие поля стоят ПЕРВЫМИ — и именно в том порядке, что назвал владелец.
    expect(
      labels.slice(0, common.length),
      `форма «${resource}»: общие поля не первые. Сверху вниз: ${JSON.stringify(labels)}. ` +
        `Владелец назвал порядок «имя → описание → метки» до полей ресурса: рука идёт к ` +
        `одному месту на всех формах, и разойтись им нельзя`,
    ).toEqual(common);

    expect(
      common,
      `форма «${resource}»: общие поля переставлены между собой — ${JSON.stringify(common)}`,
    ).toEqual(COMMON_FIELDS.filter((lbl) => common.includes(lbl)));

    captured.push({ resource, common, total: labels.length });
  }

  // Сверка форм ДРУГ С ДРУГОМ, а не с образцом: расхождение между двумя формами
  // и есть та беда, о которой говорит правило.
  const sample = JSON.stringify(captured[0].common);
  for (const rec of captured) {
    expect(
      JSON.stringify(rec.common),
      `форма «${rec.resource}» открывается набором ${JSON.stringify(rec.common)}, а «${captured[0].resource}» — ` +
        `${sample}. Соседние формы говорят с пользователем по-разному`,
    ).toBe(sample);
  }

  console.log(
    `осмотрено форм создания: ${captured.length}; полей всего: ` +
      `${captured.reduce((s, c) => s + c.total, 0)}; начало у всех — ${sample}`,
  );
});

test("общие поля отделены от полей ресурса чертой", async ({ page }) => {
  // verifies #925
  test.setTimeout(180_000);
  const { projectId } = await tenantWithProject(page);

  for (const resource of ["vpc/networks", "compute/instances", "nlb/listeners"]) {
    await openCreateForm(page, projectId, resource);
    const labels = await formLabels(page);
    const lastCommon = labels.filter((lbl) => COMMON_FIELDS.includes(fieldName(lbl.name))).at(-1);
    const firstOwn = labels.find((lbl) => !COMMON_FIELDS.includes(fieldName(lbl.name)));
    expect(lastCommon, `форма «${resource}»: общих полей нет вовсе`).toBeTruthy();
    expect(firstOwn, `форма «${resource}»: полей ресурса нет вовсе`).toBeTruthy();

    // Черта ищется НЕ по имени класса, а по тому, чем она является на экране:
    // тонкая горизонтальная линия во всю ширину формы. Так проба переживает
    // замену компонента и не переживает исчезновение самой линии.
    const divider = await page.evaluate(() => {
      const form = document.querySelector("form.ant-form");
      if (!form) return null;
      const formWidth = form.getBoundingClientRect().width;
      const lines = Array.from(form.querySelectorAll("div")).filter((d) => {
        const r = d.getBoundingClientRect();
        return r.height <= 2 && r.height > 0 && r.width > formWidth * 0.8;
      });
      return lines.length === 0
        ? null
        : { howMany: lines.length, y: Math.round(lines[0].getBoundingClientRect().y) };
    });

    expect(
      divider,
      `форма «${resource}»: между общими полями и полями ресурса нет разделительной черты — ` +
        `«как назвать» и «чем это будет» слились в один список`,
    ).not.toBeNull();

    expect(
      divider!.y,
      `форма «${resource}»: черта на y=${divider!.y} стоит не между «${fieldName(lastCommon!.name)}» ` +
        `(y=${lastCommon!.y}) и «${fieldName(firstOwn!.name)}» (y=${firstOwn!.y})`,
    ).toBeGreaterThan(lastCommon!.y);
    expect(divider!.y).toBeLessThan(firstOwn!.y);

    console.log(
      `${resource}: черта y=${divider!.y} между «${fieldName(lastCommon!.name)}» ` +
        `(${lastCommon!.y}) и «${fieldName(firstOwn!.name)}» (${firstOwn!.y})`,
    );
  }
});

test("незаполненное обязательное поле помечено НА САМОМ ПОЛЕ, а не строкой под формой", async ({
  page,
}) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  // Обработчик балансировщика взят намеренно: у него ДВА обязательных поля и
  // между ними стоит третье. Форма с единственным обязательным полем не
  // различает «отказ у своего поля» и «отказ где-то в форме» — там любое место
  // рядом, и утверждение о месте было бы вакуумным.
  await openCreateForm(page, projectId, "nlb/listeners");
  const form = page.locator("form.ant-form");

  // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ДО ДЕЙСТВИЯ: форма не краснеет, пока её не трогали.
  // Без него проба не отличила бы отказ, вызванный попыткой, от постоянного
  // украшения, висящего на форме всегда.
  await expect(
    form.locator("[role=alert]"),
    "форма показала отказ ДО первой попытки отправки: она обвиняет за незаполненное поле того, " +
      "кто её только что открыл",
  ).toHaveCount(0);

  await page.locator('button:has-text("Создать")').last().click();

  const errors = form.locator("[role=alert]");
  await expect(
    errors.first(),
    "отправка пустой формы не показала ни одного отказа: обязательность поля объявлена, " +
      "но не соблюдается — либо соблюдается молча, и пользователю нечего исправлять",
  ).toBeVisible({ timeout: 15_000 });

  const snapshot = await page.evaluate(() => {
    const form = document.querySelector("form.ant-form")!;
    const errors = Array.from(form.querySelectorAll("[role=alert]")).map((a) => {
      const r = a.getBoundingClientRect();
      return { text: (a.textContent ?? "").trim(), y: Math.round(r.y), x: Math.round(r.x) };
    });
    const labels = Array.from(form.querySelectorAll(".ant-form-item-label label")).map((l) => {
      const r = l.getBoundingClientRect();
      return { name: (l.textContent ?? "").trim(), y: Math.round(r.y), x: Math.round(r.x) };
    });
    const button = Array.from(document.querySelectorAll("button")).find(
      (b) => (b.textContent ?? "").trim() === "Создать",
    );
    return {
      errors,
      labels,
      buttonY: button ? Math.round(button.getBoundingClientRect().y) : -1,
      url: location.pathname,
    };
  });

  // Форма НЕ отправилась: иначе «отказ у поля» проверялся бы на странице,
  // которая уже ушла дальше.
  expect(
    snapshot.url,
    "форма с незаполненным обязательным полем всё-таки отправилась — отказ, который она " +
      "показала, ничего не остановил",
  ).toContain("/create");

  const labelOf = (name: string) => snapshot.labels.find((lbl) => fieldName(lbl.name) === name);
  const balancer = labelOf("Балансировщик");
  const protocol = labelOf("Протокол");
  expect(balancer, "поля «Балансировщик» на форме обработчика нет").toBeTruthy();
  expect(protocol, "поля «Протокол» на форме обработчика нет").toBeTruthy();

  const fieldError = snapshot.errors.find((err) => err.text.includes("Балансировщик"));
  expect(
    fieldError,
    `отказ не назвал поле по имени. Показано: ${JSON.stringify(snapshot.errors.map((err) => err.text))}`,
  ).toBeTruthy();

  // МЕСТО ОШИБКИ И МЕСТО ИСПРАВЛЕНИЯ СОВПАДАЮТ: сообщение стоит в строке своего
  // поля — ниже его подписи и ВЫШЕ подписи следующего поля.
  expect(
    fieldError!.y,
    `отказ «${fieldError!.text}» стоит на y=${fieldError!.y}, вне строки своего поля ` +
      `(«Балансировщик» y=${balancer!.y}, следующее поле «Протокол» y=${protocol!.y}). ` +
      `Пользователь читает претензию не там, где её исправляют`,
  ).toBeGreaterThan(balancer!.y);
  expect(fieldError!.y).toBeLessThan(protocol!.y);

  // …и в колонке ввода, а не полосой во всю ширину: строка под формой стоит
  // левее подписей и ниже последнего поля.
  expect(
    fieldError!.x,
    `отказ выровнен по колонке имён (x=${fieldError!.x}, подпись x=${balancer!.x}) — ` +
      `так выглядит полоса под формой, а не пометка на поле`,
  ).toBeGreaterThan(balancer!.x);

  expect(
    Math.abs(fieldError!.y - balancer!.y),
    `отказ ближе к кнопке отправки (y=${snapshot.buttonY}), чем к своему полю ` +
      `(y=${balancer!.y}) — это сводная строка под формой`,
  ).toBeLessThan(Math.abs(snapshot.buttonY - fieldError!.y));

  console.log(
    `отказов показано: ${snapshot.errors.length}; каждый в своей строке; ` +
      `${JSON.stringify(snapshot.errors.map((err) => `${err.text.slice(0, 40)}@${err.y}`))}`,
  );
});

test("кнопка отправки называет действие, предмет назван заголовком над ней", async ({ page }) => {
  // verifies #925
  test.setTimeout(180_000);
  const { projectId } = await tenantWithProject(page);

  for (const resource of [
    "vpc/networks",
    "vpc/route-tables",
    "compute/instances",
    "storage/volumes",
    "nlb/load-balancers",
  ]) {
    await openCreateForm(page, projectId, resource);

    const title = (await page.locator("h3.ant-typography").first().textContent())?.trim() ?? "";
    const buttonLabel =
      (await page.locator('button:has-text("Создать")').last().textContent())?.trim() ?? "";

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: предмет назван — но заголовком, а не кнопкой.
    // Без него «кнопка не называет предмет» зеленело бы и на странице, где
    // предмет не назван нигде.
    expect(
      title.length,
      `форма «${resource}»: заголовок не назвал предмет («${title}»). Тогда кнопка «Создать» ` +
        `не говорит, что именно создаётся, — и коротка она не по канону, а по недосмотру`,
    ).toBeGreaterThan("Создать".length);

    expect(
      buttonLabel,
      `форма «${resource}»: кнопка подписана «${buttonLabel}» — она повторяет предмет, уже ` +
        `названный заголовком «${title}» в двадцати точках выше`,
    ).toBe("Создать");

    console.log(`${resource}: заголовок «${title}» · кнопка «${buttonLabel}»`);
  }
});

test("поля страницы одни на списке, карточке и форме", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);
  const name = `net-pad-${runTag()}`;

  const createResp = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: name, ipv4CidrBlocks: ["10.61.0.0/16"] },
  });
  expect(
    createResp.status(),
    `сеть для пробы не создана: край ответил ${createResp.status()}. Это УСЛОВИЕ пробы, а не её ` +
      `предмет — карточку не на чем открыть`,
  ).toBe(200);

  const id = await expect
    .poll(
      async () => {
        const res = await page.request.get(`/vpc/v1/networks?projectId=${projectId}`);
        if (!res.ok()) return "";
        const b = (await res.json()) as { networks?: Array<{ id: string; name: string }> };
        return b.networks?.find((n) => n.name === name)?.id ?? "";
      },
      { message: "созданная сеть не читается по списку — карточку открывать нечем", timeout: 60_000 },
    )
    .not.toBe("");
  void id;
  const list = (await (
    await page.request.get(`/vpc/v1/networks?projectId=${projectId}`)
  ).json()) as { networks: Array<{ id: string; name: string }> };
  const netId = list.networks.find((n) => n.name === name)!.id;

  const measurements: Array<{ pageName: string; title: string; x: number; y: number }> = [];
  for (const [pageName, url] of [
    ["список", `/projects/${projectId}/vpc/networks`],
    ["форма", `/projects/${projectId}/vpc/networks/create`],
    ["карточка", `/projects/${projectId}/vpc/networks/${netId}`],
  ] as const) {
    await page.goto(url, { waitUntil: "domcontentloaded" });
    const h = page.locator("h3.ant-typography").first();
    await expect(h, `${pageName}: заголовок страницы не отрисовался`).toBeVisible({
      timeout: 45_000,
    });
    const box = await h.boundingBox();
    expect(box, `${pageName}: у заголовка нет геометрии`).not.toBeNull();
    measurements.push({
      pageName,
      title: ((await h.textContent()) ?? "").trim(),
      x: Math.round(box!.x),
      y: Math.round(box!.y),
    });
  }

  // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: это три РАЗНЫЕ страницы. Совпадение полей у трёх
  // копий одной страницы не значило бы ничего.
  const titles = new Set(measurements.map((meas) => meas.title));
  expect(
    titles.size,
    `открылись не три разные страницы, а ${titles.size}: ${JSON.stringify([...titles])}`,
  ).toBe(3);

  for (const meas of measurements.slice(1)) {
    expect(
      { x: meas.x, y: meas.y },
      `${meas.pageName}: заголовок стоит в ${meas.x}×${meas.y}, а на списке — ${measurements[0].x}×${measurements[0].y}. ` +
        `Поля страницы разъехались: при переходе между страницами одного ресурса текст дёргается`,
    ).toEqual({ x: measurements[0].x, y: measurements[0].y });
  }

  console.log(`поля страницы: ${measurements.map((meas) => `${meas.pageName} ${meas.x}×${meas.y}`).join(" · ")}`);
});

/**
 * Списки, чей собственный опрос проба измеряет.
 *
 * Адрес края назван рядом с адресом страницы намеренно: правило канона —
 * УСЛОВНОЕ («кнопки „Обновить“ там, где список опрашивается сам»), и без замера
 * самого опроса проба обвиняла бы страницы, которым кнопка положена. Здесь
 * условие проверяется, а не предполагается.
 */
const LISTS = [
  { url: "vpc/networks", edge: "/vpc/v1/networks" },
  { url: "vpc/subnets", edge: "/vpc/v1/subnets" },
  { url: "vpc/security-groups", edge: "/vpc/v1/securityGroups" },
  { url: "compute/instances", edge: "/compute/v1/instances" },
  { url: "compute/machine-types", edge: "/compute/v1/machineTypes" },
  { url: "storage/volumes", edge: "/storage/v1/volumes" },
  { url: "nlb/load-balancers", edge: "/nlb/v1/networkLoadBalancers" },
];

const LISTS_OUTSIDE_PROJECT = [
  { url: "/iam/roles", edge: "/iam/v1/roles" },
  { url: "/system/zones", edge: "/geo/v1/zones" },
];

test("список, который опрашивается сам, не предлагает «Обновить»", async ({ page }) => {
  // verifies #925
  test.setTimeout(300_000);
  const { projectId } = await tenantWithProject(page);

  let requests: string[] = [];
  page.on("request", (r) => {
    requests.push(new URL(r.url()).pathname);
  });

  const urls = [
    ...LISTS.map((rec) => ({ ...rec, full: `/projects/${projectId}/${rec.url}` })),
    ...LISTS_OUTSIDE_PROJECT.map((rec) => ({ ...rec, full: rec.url })),
  ];

  for (const rec of urls) {
    requests = [];
    await page.goto(rec.full, { waitUntil: "domcontentloaded" });

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ 1: страница жива и её ручки на месте. Без него
    // «кнопки „Обновить“ нет» зеленело бы на пустой странице.
    await expect(
      listControls(page).first(),
      `${rec.url}: на странице нет ни одной ручки списка — она не загрузилась, и вердикт ` +
        `«кнопки „Обновить“ нет» был бы вердиктом о пустоте`,
    ).toBeVisible({ timeout: 45_000 });

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ 2 и одновременно ПРЕДПОСЫЛКА ПРАВИЛА: список
    // опрашивает край САМ. Ждём именно этого условия — второго запроса, которого
    // никто руками не просил, — а не отмеренного времени.
    await expect
      .poll(() => requests.filter((u) => u === rec.edge).length, {
        message:
          `${rec.url}: за отведённое время край ${rec.edge} запрошен меньше двух раз. Список себя ` +
          `не опрашивает — значит правило «кнопки „Обновить“ быть не должно» к нему не относится, ` +
          `и проба обязана это сказать, а не молча зеленеть`,
        // Бюджет заметно больше промежутка опроса (он секундный): под нагрузкой
        // сборки модулей окно в полминуты однажды истекло, и «не выполнилось»
        // пришло бы как «красное». Утверждение от этого не слабеет — требуется
        // по-прежнему ВТОРОЙ запрос, которого никто руками не просил.
        timeout: 60_000,
      })
      .toBeGreaterThanOrEqual(2);

    const controls = refreshControls(page);
    const howMany = await controls.count();
    const labels = howMany
      ? await controls.evaluateAll((els) =>
          els.map(
            (e) =>
              (e.textContent ?? "").trim() ||
              e.getAttribute("aria-label") ||
              e.getAttribute("title") ||
              "кнопка со стрелкой обновления",
          ),
        )
      : [];

    expect(
      howMany,
      `${rec.url}: список опрашивает ${rec.edge} сам, и при этом предлагает обновить его руками ` +
        `(${JSON.stringify(labels)}). Ручка предлагает то, что и так происходит, а её ` +
        `присутствие говорит обратное`,
    ).toBe(0);

    console.log(
      `${rec.url}: ${rec.edge} опрошен ${requests.filter((u) => u === rec.edge).length}×, ` +
        `ручек обновления ${howMany}`,
    );
  }

  console.log(`осмотрено списков: ${urls.length}`);
});

test("в строках списка нет флажков и группового удаления", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  // Сеть заводится ради ТРЕТЬЕГО списка — собственного списка арендатора. Два
  // общих каталога (роли, зоны) показывают чужие строки, и на них проба
  // утверждала бы об устройстве таблицы, которую этот арендатор не наполнял.
  const name = `net-bulk-${runTag()}`;
  const createResp = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: name, ipv4CidrBlocks: ["10.62.0.0/16"] },
  });
  expect(
    createResp.status(),
    `сеть для пробы не создана: край ответил ${createResp.status()}. Это УСЛОВИЕ пробы, а не её предмет`,
  ).toBe(200);

  // ЧИТАЕМЫЕ КАТАЛОГИ СЮДА НЕ ВХОДЯТ НАМЕРЕННО. У списка, где удаления нет
  // by construction (типы машин, типы дисков), не бывает и группового удаления,
  // поэтому «флажков нет» там верно всегда и ничего не сторожит. Проба стоит на
  // списках, где строку УДАЛЯЮТ, — там снятие групповых действий и есть решение.
  for (const url of ["/iam/roles", "/system/zones", `/projects/${projectId}/vpc/networks`]) {
    await page.goto(url, { waitUntil: "domcontentloaded" });

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: строки есть, и у строки есть СВОЁ меню действий —
    // тот самый путь, которым удаляют по одной. На пустой таблице «флажков нет»
    // не значит ничего.
    await expect
      .poll(async () => await page.locator(".app-main tbody tr").count(), {
        message: `${url}: таблица пуста — утверждать об устройстве её строк не о чем`,
        timeout: 45_000,
      })
      .toBeGreaterThan(0);

    const rowCount = await page.locator(".app-main tbody tr").count();
    const menuCount = await page.locator(".app-main tbody .anticon-more").count();
    expect(
      menuCount,
      `${url}: у строк нет собственного меню действий (строк ${rowCount}). Тогда удалять по одной ` +
        `нечем, и снятие групповых действий оставило бы пользователя вовсе без удаления`,
    ).toBeGreaterThan(0);

    expect(
      await page.locator('.app-main tbody input[type="checkbox"]').count(),
      `${url}: в строках таблицы стоят флажки. Групповое снятие — самая дорогая ошибка списка: ` +
        `оно необратимо, а подтверждение называет число, которое читатель и так видел неверно`,
    ).toBe(0);

    expect(
      await page
        .locator(".app-main")
        .getByRole("button")
        .filter({ hasText: /выделенн|выбранн/i })
        .count(),
      `${url}: на странице есть действие над выделенными строками`,
    ).toBe(0);

    expect(
      await page.locator(".app-main").getByText(/Выделено\s*:/i).count(),
      `${url}: на странице есть счётчик выделенных строк`,
    ).toBe(0);

    console.log(`${url}: строк ${rowCount}, меню действий ${menuCount}, флажков 0`);
  }
});

test("темы документации показаны текстом: живых ссылок в никуда нет", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  const pages = [
    { url: `/projects/${projectId}/vpc/networks`, topic: "Облачные сети и подсети" },
    { url: `/projects/${projectId}/compute/instances`, topic: "Документация" },
    { url: `/projects/${projectId}/storage/volumes`, topic: "Документация" },
    { url: "/iam/users", topic: "Управление доступом" },
  ];

  for (const rec of pages) {
    await page.goto(rec.url, { waitUntil: "domcontentloaded" });

    // ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: тема на странице ЕСТЬ. Без него «ссылок в никуда
    // нет» зеленело бы на странице, где нет и самих тем.
    await expect(
      page.getByText(rec.topic, { exact: false }).first(),
      `${rec.url}: темы документации не показаны вовсе — утверждать, что они не ссылки, не о чем`,
    ).toBeVisible({ timeout: 45_000 });

    const dead = await page.evaluate(() => {
      const main = document.querySelector(".app-main") ?? document.body;
      return Array.from(main.querySelectorAll("a"))
        .filter((a) => {
          const h = a.getAttribute("href");
          return h === null || h === "" || h === "#";
        })
        .map((a) => (a.textContent ?? "").trim().slice(0, 40));
    });

    expect(
      dead,
      `${rec.url}: на странице есть якоря, которые выглядят ссылками и никуда не ведут: ` +
        `${JSON.stringify(dead)}. Адресов у документации в дереве нет ни одного, поэтому темы ` +
        `показываются текстом`,
    ).toEqual([]);

    console.log(`${rec.url}: тема «${rec.topic}» показана, мёртвых якорей 0`);
  }
});
