// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Ветвь контракта, достижимая из создания, выбирается ИЗ КОНСОЛИ — и ресурс
// создаётся именно с ней.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО ПРОБА БРАУЗЕРОМ, А НЕ МОДУЛЬНАЯ
//
// Модульные пробы (`oneof-form-coverage*.test.ts`) держат в руках объект
// реестра и говорят о нём точно, но не видят ни маршрутизации, ни того, КАКОЙ
// из реестров обслуживает открытый адрес. Ровно там дефект и жил: ветви
// проверки живости были заведены в реестре `shared`, а `/nlb/*` рисует модуль
// `nlb` со своим реестром — правка не доехала до пользователя, и все модульные
// пробы при этом были зелёными.
//
// Проба открывает продукт по адресу и смотрит на то же, на что смотрел бы
// человек: выбирает ветвь в форме и читает СОЗДАННЫЙ ресурс.
//
// ЧТО УТВЕРЖДАЕТСЯ — наблюдаемое: ресурс создан с выбранной ветвью. НЕ
// утверждается разметка: класс переименуют, компонент заменят — проба останется
// зелёной, а находка вернётся.

import { test, expect, type Page } from "@playwright/test";
import { runTag, tenantWithProject } from "./fixtures";

/** Регион для размещения группы целей. Условие пробы, а не её предмет. */
async function anyRegionId(page: Page): Promise<string> {
  const res = await page.request.get("/geo/v1/regions");
  expect(
    res.ok(),
    "справочник регионов недоступен — условие пробы не создано: группу целей " +
      "разместить негде, и о ветвях контракта такой прогон не говорит ничего",
  ).toBeTruthy();
  const body = (await res.json()) as { regions?: Array<{ id: string }> };
  const id = body.regions?.[0]?.id ?? "";
  expect(id, "справочник регионов ПУСТ — свежеподнятый стенд непригоден для размещаемых ресурсов").not.toBe("");
  return id;
}

/** Поле формы по подписи: контрол, стоящий справа от неё.
 *
 * ПОДПИСЬ СВЕРЯЕТСЯ ЦЕЛИКОМ. Поиск подстроки здесь ещё и НЕЧУВСТВИТЕЛЕН К
 * РЕГИСТРУ — таков `getByText` при `exact: false`, — и на форме балансировщика
 * это давало не то поле: подпись «Регион» находилась внутри слова
 * «региональный», которым подписан вариант РАЗМЕЩЕНИЯ, а размещение объявлено
 * раньше. Проба открывала список размещения и искала в нём регион; такого
 * варианта там нет, и ждать его можно бесконечно.
 *
 * Все подписи, которыми пользуется этот файл, объявлены в реестрах дословно —
 * поэтому точное сравнение здесь не строгость ради строгости, а единственный
 * способ адресовать поле однозначно.
 */
function поле(page: Page, подпись: string) {
  return page.locator(".ant-form-item", { has: page.getByText(подпись, { exact: true }) }).first();
}

/** Поле ВВОДА по подписи — среди полей с этой подписью берётся то, что вводом и
 * является.
 *
 * ЗАЧЕМ ОТДЕЛЬНО ОТ `поле`. Выбранный вариант списка остаётся в форме ВИДИМЫМ
 * ТЕКСТОМ, и подпись поля с ним совпадает: у цели вид называется вариантом
 * «Внешний адрес», а само значение — полем «Внешний адрес». Оба живут в одной
 * форме, список объявлен раньше — значит «первое поле с такой подписью» это
 * список вида, а не ввод. Заполнение ушло бы в строку поиска списка: молча, без
 * отказа, и упал бы через два шага совсем другой шаг — тот, который спрашивает
 * про созданную цель.
 *
 * Тот же класс есть и у соседа: вариант «Адрес в облаке» одноимёнен полю
 * «Адрес в облаке». Поэтому это помощник, а не правка одного места.
 */
function полеВвода(page: Page, подпись: string) {
  return page
    .locator(".ant-form-item")
    .filter({ has: page.getByText(подпись, { exact: true }) })
    .filter({ hasNot: page.locator(".ant-select") })
    .first();
}

/** Выбор в выпадающем списке по видимому тексту варианта.
 *
 * ВАРИАНТ БЕРЁТСЯ ПО КЛАССУ ВАРИАНТА, А НЕ ПО ТЕКСТУ ВНУТРИ СПИСКА. Рядом с
 * настоящими вариантами список держит их НЕВИДИМОЕ ЗЕРКАЛО для доступности:
 * `<div role="listbox" style="height:0;width:0;overflow:hidden">` с тремя
 * `role="option"` вокруг активного (`OptionList` в `@rc-component/select`,
 * ветвь `virtual`). У зеркала нет класса, его текст — ЗНАЧЕНИЕ варианта, и в
 * дереве оно стоит РАНЬШЕ настоящего списка.
 *
 * Поэтому «первый совпавший по тексту» попадал именно в зеркало, а щелчок ждал
 * видимости у элемента нулевого размера — то есть свойства, которого у него нет
 * by construction. Ожидание не кончалось ничем: три пробы этого файла съели по
 * 240 с каждая и вместе с ними весь бюджет шага, после чего отчёта не осталось
 * ВООБЩЕ — вердикта не получила ни одна из 28 проб.
 *
 * Отсюда же и предел у щелчка: неограниченное ожидание превращает промах одной
 * пробы в «не выполнилось» у всей суиты. Названный отказ за 20 с — это не
 * послабление, а разница между находкой и потерей прогона.
 */
async function выбрать(page: Page, подпись: string, вариант: RegExp) {
  await поле(page, подпись).locator(".ant-select").first().click();
  const пункт = page
    .locator(".ant-select-dropdown:visible")
    .locator(".ant-select-item-option")
    .filter({ hasText: вариант })
    .first();
  await expect(
    пункт,
    `в списке «${подпись}» нет варианта ${вариант} — выбрать нечего, и о ветви такой прогон не говорит ничего`,
  ).toBeVisible({ timeout: 20_000 });
  await пункт.click({ timeout: 20_000 });
}

/** Группа целей, прочитанная по имени из списка проекта. */
async function группаПоИмени(page: Page, projectId: string, имя: string) {
  const res = await page.request.get(`/nlb/v1/targetGroups?projectId=${projectId}`);
  if (!res.ok()) return null;
  const body = (await res.json()) as { targetGroups?: Array<Record<string, unknown>> };
  return body.targetGroups?.find((g) => g.name === имя) ?? null;
}

/** Открыть форму создания группы целей и заполнить общую часть. */
async function начатьСозданиеГруппы(page: Page, projectId: string, имя: string, regionId: string) {
  await page.goto(`/projects/${projectId}/nlb/target-groups`, { waitUntil: "domcontentloaded" });

  const создать = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(создать, "на странице групп целей нет элемента создания").toBeVisible({ timeout: 30_000 });
  await создать.click();

  const имяПоле = поле(page, "Имя").locator("input").first();
  await expect(имяПоле, "форма создания группы целей не предложила поле имени").toBeVisible({ timeout: 20_000 });
  await имяПоле.fill(имя);

  await выбрать(page, "Регион", new RegExp(regionId));
}

/** Отправить форму и дождаться ответа края на создание группы. */
async function отправить(page: Page) {
  const [ответ] = await Promise.all([
    page.waitForResponse((r) => r.url().includes("/nlb/v1/targetGroups") && r.request().method() === "POST", {
      timeout: 40_000,
    }),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
  expect(ответ.status(), `создание группы целей отвергнуто краем: ${(await ответ.text()).slice(0, 300)}`).toBe(200);
}

test("группа целей создаётся с проверкой живости по HTTP — ветвь выбрана из формы", async ({ page }) => {
  // verifies #375 — три ветви из четырёх (`http`, `https`, `grpc`) не были
  // выразимы формой ТОГО модуля, который рисует `/nlb/*`: проверка умела только
  // TCP, а путь, коды ответа, заголовки и имя службы объявлены контрактом.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const regionId = await anyRegionId(page);
  const имя = `tg-http-${runTag()}`;

  await начатьСозданиеГруппы(page, projectId, имя, regionId);

  await выбрать(page, "HC: протокол", /HTTP —/);
  await поле(page, "HC: путь").locator("input").first().fill("/healthz");

  await отправить(page);

  await expect
    .poll(async () => Boolean(await группаПоИмени(page, projectId, имя)), {
      message: "группа целей не появилась в списке после принятого запроса",
      timeout: 60_000,
    })
    .toBe(true);

  const группа = (await группаПоИмени(page, projectId, имя)) as Record<string, unknown>;
  const hc = (группа.healthCheck ?? {}) as Record<string, unknown>;
  expect(
    hc.http,
    "группа создана с проверкой живости НЕ по HTTP: ветвь, выбранная в форме, до контракта не доехала. " +
      `Что приехало: ${JSON.stringify(hc)}`,
  ).toBeTruthy();
  expect((hc.http as Record<string, unknown>).path, "путь проверки не доехал — ветвь выражена формально").toBe(
    "/healthz",
  );
  expect(hc.tcp, "в теле оказались ДВЕ ветви проверки живости — взаимоисключающая группа этого не допускает").toBe(
    undefined,
  );
});

test("группа целей создаётся СРАЗУ с целью — второй заход не требуется", async ({ page }) => {
  // verifies #375 — `CreateTargetGroupRequest` несёт `targets`, а форма их не
  // предлагала: группу приходилось создавать пустой и наполнять вторым
  // действием. Контракт обещал один заход, консоль требовала два.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const regionId = await anyRegionId(page);
  const имя = `tg-tgt-${runTag()}`;
  const адрес = "203.0.113.7";

  await начатьСозданиеГруппы(page, projectId, имя, regionId);

  await поле(page, "Цели").getByRole("button", { name: /Добавить/ }).first().click();
  await выбрать(page, "Чем названа цель", /Внешний адрес/);
  await полеВвода(page, "Внешний адрес").locator("input").first().fill(адрес);

  await отправить(page);

  await expect
    .poll(
      async () => {
        const g = await группаПоИмени(page, projectId, имя);
        const targets = (g?.targets as Array<Record<string, unknown>> | undefined) ?? [];
        return targets.map((t) => (t.externalIp as Record<string, unknown> | undefined)?.address ?? "").join(",");
      },
      {
        message:
          "созданная группа целей не несёт цели, заданной в форме создания. Контракт принимает " +
          "`targets` при создании; если их там нет, форма отправила группу без них, и пользователю " +
          "остаётся второй заход — тот самый, которого контракт не требует",
        timeout: 60_000,
      },
    )
    .toBe(адрес);
});

// ─────────────────────────────────────────────────────────────────────────────
// ОТКАЗ ОТ ВЕТВИ — тоже исход, и он тоже обязан быть достижим из формы.
//
// Пробы выше спрашивают «выбранная ветвь доехала?». Ниже — обратное и не менее
// важное: «от ветви, которой контракт не требует, можно отказаться?» и «ветвь,
// объявленная контрактом несоздаваемой, действительно не создаётся?». Обе
// находки пережили зелёные модульные пробы своих модулей.

/** Балансировщик, прочитанный по имени из списка проекта. */
async function балансировщикПоИмени(page: Page, projectId: string, имя: string) {
  const res = await page.request.get(`/nlb/v1/networkLoadBalancers?projectId=${projectId}`);
  if (!res.ok()) return null;
  const body = (await res.json()) as { networkLoadBalancers?: Array<Record<string, unknown>> };
  return body.networkLoadBalancers?.find((b) => b.name === имя) ?? null;
}

test("внешний балансировщик создаётся ТОЛЬКО на IPv4 — от семейства можно отказаться", async ({ page }) => {
  // verifies #543 — у формы модуля, который рисует `/nlb/*`, варианта «не
  // задавать это семейство» не было вовсе. При внешнем размещении режим
  // «публичный» даёт источник БЕЗУСЛОВНО и стоял умолчанием у обоих семейств,
  // поэтому на провод уезжали оба, и балансировщик только на IPv4 — ресурс,
  // который сервис принимает («хотя бы одно семейство»), — был невыразим.
  // Общий реестр такой вариант несёт, но `/nlb/*` рисует не его.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const regionId = await anyRegionId(page);
  const имя = `lb-v4only-${runTag()}`;

  await page.goto(`/projects/${projectId}/nlb/load-balancers`, { waitUntil: "domcontentloaded" });
  const создать = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(создать, "на странице балансировщиков нет элемента создания").toBeVisible({ timeout: 30_000 });
  await создать.click();

  const имяПоле = поле(page, "Имя").locator("input").first();
  await expect(имяПоле, "форма создания балансировщика не предложила поле имени").toBeVisible({ timeout: 20_000 });
  await имяПоле.fill(имя);
  await выбрать(page, "Регион", new RegExp(regionId));

  // Отказ от IPv6 — то, ради чего проба и написана. Кнопка режима живёт в
  // строке своего семейства и выбирается так же, как её выбрал бы человек.
  const строкаV6 = page.locator(".ant-form-item", { has: page.getByText("IPv6 Адрес", { exact: true }) }).first();
  const отказV6 = строкаV6.getByText("Не задавать", { exact: false }).first();
  await expect(
    отказV6,
    "в строке IPv6 нет способа отказаться от семейства: при внешнем размещении оба " +
      "предложенных режима дают источник, поэтому балансировщик только на IPv4 не собрать",
  ).toBeVisible({ timeout: 20_000 });
  await отказV6.click();

  const [ответ] = await Promise.all([
    page.waitForResponse((r) => r.url().includes("/nlb/v1/networkLoadBalancers") && r.request().method() === "POST", {
      timeout: 40_000,
    }),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
  expect(ответ.status(), `создание балансировщика отвергнуто краем: ${(await ответ.text()).slice(0, 300)}`).toBe(200);

  // Наблюдаемое — СОЗДАННЫЙ ресурс. Сперва ждём, пока заявленное семейство
  // материализуется в связанный адрес: без этого «шестого нет» вакуумно —
  // до саги нет ни одного, при любом выборе.
  await expect
    .poll(async () => ((await балансировщикПоИмени(page, projectId, имя))?.v4AddressId as string) ?? "", {
      message:
        "заявленное семейство IPv4 не материализовалось в связанный адрес: без него " +
        "утверждение «IPv6 отсутствует» ничего не значит — до саги отсутствуют оба",
      timeout: 120_000,
    })
    .not.toBe("");

  const балансировщик = (await балансировщикПоИмени(page, projectId, имя)) as Record<string, unknown>;
  expect(
    (балансировщик.v6AddressId as string) ?? "",
    "балансировщик создан С IPv6, хотя от семейства отказались в форме: отказ до контракта " +
      `не доехал. Что приехало: ${JSON.stringify(балансировщик.v6AddressId)}`,
  ).toBe("");
});

test("вид «контейнер» машины не создаёт — отказ приходит и называет вид", async ({ page }) => {
  // verifies #540 — контракт объявляет `InstanceKind.CONTAINER` несоздаваемым и
  // обещает синхронный отказ ПО ИМЕНИ ПОЛЯ. Отказ при этом висел на другом поле
  // (источник ОС), поэтому пара «вид контейнер + образ ХРАНИЛИЩА» проходила
  // проверку целиком и создавала машину: вид «контейнер» с корневой файловой
  // системой из образа диска — ресурс, не описываемый ни одной ветвью модели.
  // Консоль эту пару и слала: вид предлагался без условий, а клиентская проверка
  // стерегла только источник.
  //
  // Утверждается ИСХОД, а не слой, который отказал: машины с этим именем не
  // существует. Проба остаётся верной, если отказ переедет между консолью и краем.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const имя = `vm-ctr-${runTag()}`;

  await page.goto(`/projects/${projectId}/compute/instances/create`, { waitUntil: "domcontentloaded" });

  const имяПоле = поле(page, "Имя").locator("input").first();
  await expect(имяПоле, "форма создания машины не предложила поле имени").toBeVisible({ timeout: 30_000 });
  await имяПоле.fill(имя);
  await выбрать(page, "Тип инстанса", /CONTAINER/);

  await page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click();

  await expect(
    page.getByText(/Вид «контейнер» пока не создаётся/).first(),
    "отказ по виду не показан: вид объявлен несоздаваемым, а форма его принимает — " +
      "арендатор узнаёт об этом отказом на запрос, который не мог пройти",
  ).toBeVisible({ timeout: 20_000 });

  const списокНесётИмя = async (): Promise<string> => {
    const res = await page.request.get(`/compute/v1/instances?projectId=${projectId}`);
    if (!res.ok()) return `список машин недоступен: ${res.status()}`;
    const body = (await res.json()) as { instances?: Array<Record<string, unknown>> };
    return (body.instances ?? []).some((i) => i.name === имя) ? "создана" : "не создана";
  };
  expect(
    await списокНесётИмя(),
    "машина вида «контейнер» создана: вид объявлен контрактом несоздаваемым, а отказ его " +
      "не назвал — значит связки «вид ↔ источник ОС» на пути создания нет",
  ).toBe("не создана");

  // (+) положительный контроль на ту же ось: отказ различает вид, а не
  // срабатывает безусловно. Без него «контейнер не создался» могло бы означать
  // «форма не создаёт ничего».
  await выбрать(page, "Тип инстанса", /VM/);
  await page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click();
  await expect(
    page.getByText(/Вид «контейнер» пока не создаётся/),
    "отказ по виду показан и для VM — он срабатывает безусловно, а не различает вид",
  ).toBeHidden({ timeout: 20_000 });
});
