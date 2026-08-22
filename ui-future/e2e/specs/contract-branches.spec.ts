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

import { test, expect, type Locator, type Page } from "@playwright/test";
import { createdResourceId, runTag, tenantWithProject } from "./fixtures";

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

/** Ряд формы по подписи: БЛИЖАЙШИЙ `.ant-form-item` над ней.
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
 *
 * РЯД БЕРЁТСЯ БЛИЖАЙШИЙ, А НЕ ПЕРВЫЙ ПОПАВШИЙСЯ (#600). Поле «во всю ширину»
 * (составной виджет, редактор правил, карточка списка) само лежит в
 * `.ant-form-item` — без подписи, одной обёрткой, — а внутри рисует СВОИ ряды.
 * Тогда «первый `.ant-form-item`, содержащий текст» это ВНЕШНЯЯ обёртка, и всё,
 * что ищется внутри неё, находится в соседнем семействе: у балансировщика отказ
 * от IPv6 попадал в строку IPv4, оба семейства оставались не заданы, форма
 * справедливо отказывалась отправлять — а проба ждала ответа края и падала по
 * пределу времени, ничего не сказав о ветви, ради которой написана.
 *
 * Ось `ancestor` идёт в ОБРАТНОМ порядке документа, поэтому `[1]` — ближайший
 * предок, а не самый внешний. Разметку это не изобретает: `.ant-form-item` —
 * ряд, который форма рисует и так, и других признаков ряда у неё нет.
 */
function row(page: Page, label: string) {
  return page
    .getByText(label, { exact: true })
    .locator('xpath=ancestor::*[contains(concat(" ", normalize-space(@class), " "), " ant-form-item ")][1]')
    .first();
}

/** Подполе составного виджета — по СВЯЗАННОЙ с ним подписи (`<label for>`).
 *
 * ЗАЧЕМ ОТДЕЛЬНО ОТ `row`. Строка составного списка (цель группы, спецификация
 * интерфейса) рисуется обычными `div` — своего `.ant-form-item` у подполя нет
 * BY CONSTRUCTION, и адресовать его рядом нельзя ни при какой правке помощника.
 * Единственный `.ant-form-item` с текстом «Внешний адрес» — ВНЕШНЕЕ поле
 * «Цели»; прежний помощник его же и отсекал (в нём есть список вида), получал
 * пустой набор и ждал 240 с до предела пробы.
 *
 * Признак настоящий, а не выдуманный под пробу: подпись подполя — `<label for>`
 * на своём вводе, то есть его ДОСТУПНОЕ ИМЯ. Прежде у ввода внутри строки
 * доступного имени не было вовсе (читающий с экрана слышал «поле ввода»);
 * связь заведена в `ArrayItemField` и заперта модульной пробой
 * `shared/src/components/organisms/form/FormField/FormField.subfield-label.test.tsx`.
 *
 * Побочно снимается и вторая двусмысленность, ради которой заводился отдельный
 * помощник: выбранный вариант списка остаётся в форме видимым текстом, и у цели
 * вид называется вариантом «Внешний адрес», а значение — полем «Внешний адрес».
 * Доступное имя их различает: у списка оно «Чем названа цель».
 *
 * `.first()` — на случай нескольких строк: подписи у них одинаковы, а вводы
 * разные (идентификаторы не совпадают, это заперто той же модульной пробой).
 */
function subfield(page: Page, label: string) {
  return page.getByLabel(label, { exact: true }).first();
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
async function pickFrom(page: Page, opener: Locator, label: string, option: RegExp) {
  await opener.click();
  const item = page
    .locator(".ant-select-dropdown:visible")
    .locator(".ant-select-item-option")
    .filter({ hasText: option })
    .first();
  await expect(
    item,
    `в списке «${label}» нет варианта ${option} — выбрать нечего, и о ветви такой прогон не говорит ничего`,
  ).toBeVisible({ timeout: 20_000 });
  await item.click({ timeout: 20_000 });
}

/** Выбор в списке ПОЛЯ формы (подпись слева, контрол справа). */
async function pick(page: Page, label: string, option: RegExp) {
  await pickFrom(page, row(page, label).locator(".ant-select").first(), label, option);
}

/** Выбор в списке ПОДПОЛЯ составного виджета (мелкая подпись сверху).
 *
 * Щёлкается ТОТ ЖЕ элемент, что и у поля формы, — обёртка списка: доступное имя
 * ведёт к полю ввода ВНУТРИ неё, а открывает список обработчик обёртки. Путь
 * общий с `pick`, поэтому новизна здесь ровно одна — способ адресации.
 */
async function pickSubfield(page: Page, label: string, option: RegExp) {
  const select = subfield(page, label).locator(
    'xpath=ancestor::div[contains(concat(" ", normalize-space(@class), " "), " ant-select ")][1]',
  );
  await pickFrom(page, select, label, option);
}

/** Группа целей, прочитанная по имени из списка проекта. */
async function targetGroupByName(page: Page, projectId: string, name: string) {
  const res = await page.request.get(`/nlb/v1/targetGroups?projectId=${projectId}`);
  if (!res.ok()) return null;
  const body = (await res.json()) as { targetGroups?: Array<Record<string, unknown>> };
  return body.targetGroups?.find((g) => g.name === name) ?? null;
}

/** Открыть форму создания группы целей и заполнить общую часть. */
async function startGroupCreation(page: Page, projectId: string, name: string, regionId: string) {
  await page.goto(`/projects/${projectId}/nlb/target-groups`, { waitUntil: "domcontentloaded" });

  const createControl = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(createControl, "на странице групп целей нет элемента создания").toBeVisible({ timeout: 30_000 });
  await createControl.click();

  const nameField = row(page, "Имя").locator("input").first();
  await expect(nameField, "форма создания группы целей не предложила поле имени").toBeVisible({ timeout: 20_000 });
  await nameField.fill(name);

  await pick(page, "Регион", new RegExp(regionId));
}

/** Отправить форму и дождаться ответа края на создание группы. */
async function submit(page: Page) {
  const [response] = await Promise.all([
    page.waitForResponse((r) => r.url().includes("/nlb/v1/targetGroups") && r.request().method() === "POST", {
      timeout: 40_000,
    }),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
  expect(response.status(), `создание группы целей отвергнуто краем: ${(await response.text()).slice(0, 300)}`).toBe(200);
}

test("группа целей создаётся с проверкой живости по HTTP — ветвь выбрана из формы", async ({ page }) => {
  // verifies #375 — три ветви из четырёх (`http`, `https`, `grpc`) не были
  // выразимы формой ТОГО модуля, который рисует `/nlb/*`: проверка умела только
  // TCP, а путь, коды ответа, заголовки и имя службы объявлены контрактом.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const regionId = await anyRegionId(page);
  const name = `tg-http-${runTag()}`;

  await startGroupCreation(page, projectId, name, regionId);

  await pick(page, "HC: протокол", /HTTP —/);
  await row(page, "HC: путь").locator("input").first().fill("/healthz");

  await submit(page);

  await expect
    .poll(async () => Boolean(await targetGroupByName(page, projectId, name)), {
      message: "группа целей не появилась в списке после принятого запроса",
      timeout: 60_000,
    })
    .toBe(true);

  const group = (await targetGroupByName(page, projectId, name)) as Record<string, unknown>;
  const hc = (group.healthCheck ?? {}) as Record<string, unknown>;
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
  const name = `tg-tgt-${runTag()}`;
  const address = "203.0.113.7";

  await startGroupCreation(page, projectId, name, regionId);

  await row(page, "Цели").getByRole("button", { name: /Добавить/ }).first().click();
  // Вид цели и её значение — РАЗНЫЕ контролы строки, и адресуются по своим
  // подписям, а не по порядку в разметке.
  await pickSubfield(page, "Чем названа цель", /Внешний адрес/);
  await subfield(page, "Внешний адрес").fill(address);

  await submit(page);

  await expect
    .poll(
      async () => {
        const g = await targetGroupByName(page, projectId, name);
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
    .toBe(address);
});

// ─────────────────────────────────────────────────────────────────────────────
// ОТКАЗ ОТ ВЕТВИ — тоже исход, и он тоже обязан быть достижим из формы.
//
// Пробы выше спрашивают «выбранная ветвь доехала?». Ниже — обратное и не менее
// важное: «от ветви, которой контракт не требует, можно отказаться?» и «ветвь,
// объявленная контрактом несоздаваемой, действительно не создаётся?». Обе
// находки пережили зелёные модульные пробы своих модулей.

/** Балансировщик, прочитанный по имени из списка проекта. */
async function balancerByName(page: Page, projectId: string, name: string) {
  const res = await page.request.get(`/nlb/v1/networkLoadBalancers?projectId=${projectId}`);
  if (!res.ok()) return null;
  const body = (await res.json()) as { networkLoadBalancers?: Array<Record<string, unknown>> };
  return body.networkLoadBalancers?.find((b) => b.name === name) ?? null;
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
  const name = `lb-v4only-${runTag()}`;

  await page.goto(`/projects/${projectId}/nlb/load-balancers`, { waitUntil: "domcontentloaded" });
  const createControl = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(createControl, "на странице балансировщиков нет элемента создания").toBeVisible({ timeout: 30_000 });
  await createControl.click();

  const nameField = row(page, "Имя").locator("input").first();
  await expect(nameField, "форма создания балансировщика не предложила поле имени").toBeVisible({ timeout: 20_000 });
  await nameField.fill(name);
  // Размещение объявляется ЯВНО: от него зависит, какие режимы источника форма
  // вообще предлагает («публичный» есть только у внешней схемы). Полагаться на
  // умолчание значило бы проверять другой ресурс всякий раз, когда умолчание
  // сменят, — и заголовок пробы про ВНЕШНИЙ балансировщик перестал бы быть про
  // то, что она делает.
  await pick(page, "Размещение", /^EXTERNAL_REGIONAL/);
  await pick(page, "Регион", new RegExp(regionId));

  // Режим семейства выбирается в СВОЕЙ строке. Строка адресуется ближайшим
  // рядом над своей подписью: секция «Источник VIP» — поле во всю ширину, то
  // есть сама лежит в `.ant-form-item`, и «первый ряд с текстом IPv6» это её
  // внешняя обёртка, внутри которой первой лежит строка IPv4 (#600).
  //
  // Кнопка режима берётся ПО КЛАССУ КНОПКИ, а не по тексту в строке: выбранный
  // режим объясняет себя тут же соседним текстом («Публичный VIP выделяется
  // платформой автоматически»), и поиск по тексту нашёл бы объяснение наравне с
  // кнопкой — то есть щелчок зависел бы от того, что уже выбрано.
  const mode = (label: string, option: RegExp) =>
    row(page, label).locator(".ant-segmented-item-label").filter({ hasText: option }).first();

  // IPv4 задаётся ПОЛОЖИТЕЛЬНО, а не умолчанием. Иначе утверждение «создан
  // только на IPv4» держалось бы на шаблоне формы: смени он режим — и проба
  // перестала бы отправлять что-либо вовсе, а причину показала бы пределом
  // времени у ожидания ответа края, то есть следствием вместо причины.
  const sourceV4 = mode("IPv4 Адрес", /^Публичный/);
  await expect(
    sourceV4,
    "в строке IPv4 нет режима «Публичный (авто)»: внешнему балансировщику неоткуда " +
      "взять VIP, и утверждать про отказ от IPv6 не на чем — не задано ни одно семейство",
  ).toBeVisible({ timeout: 20_000 });
  await sourceV4.click();

  // Отказ от IPv6 — то, ради чего проба и написана. Сперва семейство ЗАДАЁТСЯ,
  // и только потом снимается: шаблон формы ставит IPv6 в «Не задавать» сам,
  // поэтому щелчок по умолчанию ничего не доказывал бы — «шестого нет» вышло бы
  // верным и при кнопке, которая не работает вовсе.
  const sourceV6 = mode("IPv6 Адрес", /^Публичный/);
  await expect(
    sourceV6,
    "в строке IPv6 нет режима «Публичный (авто)» — задать семейство нечем, и снять " +
      "его потом было бы нечего: проба про отказ проверяла бы умолчание",
  ).toBeVisible({ timeout: 20_000 });
  await sourceV6.click();

  const declineV6 = mode("IPv6 Адрес", /^Не задавать/);
  await expect(
    declineV6,
    "в строке IPv6 нет способа отказаться от семейства: при внешнем размещении оба " +
      "предложенных режима дают источник, поэтому балансировщик только на IPv4 не собрать",
  ).toBeVisible({ timeout: 20_000 });
  await declineV6.click();

  // Выбор ПРИНЯТ формой, а не только нажат: строка сообщает, что семейство не
  // задаётся. Без этого щелчок мимо кнопки был бы неотличим от щелчка по ней, и
  // отказ пришёл бы позже — пределом времени у ожидания ответа края.
  await expect(
    row(page, "IPv6 Адрес").getByText(/не задаётся/),
    "строка IPv6 не подтвердила отказ от семейства: выбор режима не принят формой",
  ).toBeVisible({ timeout: 20_000 });

  const [response] = await Promise.all([
    page.waitForResponse((r) => r.url().includes("/nlb/v1/networkLoadBalancers") && r.request().method() === "POST", {
      timeout: 40_000,
    }),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
  // СНАЧАЛА — состоялось ли создание ВООБЩЕ, и только потом вопрос о семействах.
  //
  // Порядок здесь не косметика. `200` на POST означает лишь, что операция
  // ПРИНЯТА: идентификатор ресурса чеканится ДО асинхронной части и приезжает в
  // `metadata` даже тогда, когда та отказала. Если сага падает (внешний VIP
  // берётся из зоне-независимого пула EXTERNAL_PUBLIC, и без него аллокация
  // отвечает «could not allocate load balancer address»), отложенная компенсация
  // сносит заготовку — балансировщика с этим именем не существует НИКОГДА.
  //
  // Прежняя редакция шла сразу к опросу `v4AddressId` и в этом случае ждала
  // 120 с пустоты, после чего обвиняла НЕ ТО: «заявленное семейство IPv4 не
  // материализовалось» читается как дефект ветви контракта, тогда как не
  // состоялось само создание, и о выборе семейств такой прогон не говорит
  // ничего. `createdResourceId` спрашивает ресурс по его СОБСТВЕННОМУ адресу и
  // называет фантом фантомом — то же утверждение, верная атрибуция и на минуту
  // раньше.
  const lbId = await createdResourceId(
    page,
    response,
    "networkLoadBalancerId",
    (id) => `/nlb/v1/networkLoadBalancers/${id}`,
    "внешний балансировщик только на IPv4",
  );

  // Наблюдаемое — СОЗДАННЫЙ ресурс. Ждём, пока заявленное семейство
  // материализуется в связанный адрес: без этого «шестого нет» вакуумно —
  // до саги отсутствуют оба.
  await expect
    .poll(async () => ((await balancerByName(page, projectId, name))?.v4AddressId as string) ?? "", {
      message:
        `балансировщик ${lbId} создан, но заявленное семейство IPv4 не материализовалось ` +
        "в связанный адрес: без него утверждение «IPv6 отсутствует» ничего не значит — " +
        "до саги отсутствуют оба",
      timeout: 120_000,
    })
    .not.toBe("");

  const balancer = (await balancerByName(page, projectId, name)) as Record<string, unknown>;
  expect(
    (balancer.v6AddressId as string) ?? "",
    "балансировщик создан С IPv6, хотя от семейства отказались в форме: отказ до контракта " +
      `не доехал. Что приехало: ${JSON.stringify(balancer.v6AddressId)}`,
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
  const name = `vm-ctr-${runTag()}`;

  await page.goto(`/projects/${projectId}/compute/instances/create`, { waitUntil: "domcontentloaded" });

  const nameField = row(page, "Имя").locator("input").first();
  await expect(nameField, "форма создания машины не предложила поле имени").toBeVisible({ timeout: 30_000 });
  await nameField.fill(name);

  // ОСТАЛЬНОЕ ОБЯЗАТЕЛЬНОЕ ЗАПОЛНЯЕТСЯ, иначе предмет пробы недостижим.
  //
  // Форма называет ВСЕ незаполненные обязательные поля до отправки — зону, тип
  // машины и образ, — и делает это раньше, чем доходит до вида. Оставив их
  // пустыми, проба меряла бы не связку «вид ↔ источник ОС», а собственную
  // недозаполненность: отказ по виду не показывался бы никогда, ни на
  // сломанном продукте, ни на починенном.
  //
  // Прежняя редакция заполняла только имя и была зелёной, пока формы не
  // проверяли своих обязательных полей вовсе: тогда запрос уходил на край и
  // отказ приходил оттуда. Предмет пробы — исход («машины с этим именем нет»)
  // и наличие внятного отказа — остался прежним; изменилось, кто отказывает.
  await pick(page, "Тип инстанса", /CONTAINER/);

  // Нажатие оставлено, но отказ по виду виден и БЕЗ него: он относится к форме
  // целиком и показывается сразу. Прежде он приходил тостом из отправки, а до
  // отправки форма не доходила, пока оставалось незаполненное обязательное
  // поле, — и на стенде без образов проба была неисполнима вовсе.
  await page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click();

  await expect(
    page.getByText(/Вид «контейнер» пока не создаётся/).first(),
    "отказ по виду не показан: вид объявлен несоздаваемым, а форма его принимает — " +
      "арендатор узнаёт об этом отказом на запрос, который не мог пройти",
  ).toBeVisible({ timeout: 20_000 });

  const listCarriesName = async (): Promise<string> => {
    const res = await page.request.get(`/compute/v1/instances?projectId=${projectId}`);
    if (!res.ok()) return `список машин недоступен: ${res.status()}`;
    const body = (await res.json()) as { instances?: Array<Record<string, unknown>> };
    return (body.instances ?? []).some((i) => i.name === name) ? "создана" : "не создана";
  };
  expect(
    await listCarriesName(),
    "машина вида «контейнер» создана: вид объявлен контрактом несоздаваемым, а отказ его " +
      "не назвал — значит связки «вид ↔ источник ОС» на пути создания нет",
  ).toBe("не создана");

  // (+) положительный контроль на ту же ось: отказ различает вид, а не
  // срабатывает безусловно. Без него «контейнер не создался» могло бы означать
  // «форма не создаёт ничего».
  await pick(page, "Тип инстанса", /VM/);
  await page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click();
  await expect(
    page.getByText(/Вид «контейнер» пока не создаётся/),
    "отказ по виду показан и для VM — он срабатывает безусловно, а не различает вид",
  ).toBeHidden({ timeout: 20_000 });
});
