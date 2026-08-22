// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { test, expect, type Locator, type Page } from "@playwright/test";
import { tenantWithProject, createdResourceId, runTag } from "./fixtures";

/**
 * Заголовок страницы и путь к ней — канон консоли, разделы 1 и 2.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЭТО ПРОБЫ БРАУЗЕРОМ, А НЕ МОДУЛЬНЫЕ
 *
 * Предмет обоих разделов — то, что видно ПРИ ПЕРЕХОДЕ между страницами одного
 * ресурса: заголовок садится иначе, крошка повторяет заголовок, имя модуля
 * названо дважды. Модульная проба монтирует ОДНУ страницу и о переходе не знает
 * ничего: у неё нет ни второй страницы, ни маршрутизации, ни шапки приложения,
 * где живут крошки, ни второго сайдбара, который рисует хост. Утверждение
 * «положение совпадает» требует двух измерений на разных адресах — это ровно то,
 * чего модульная проба сделать не может by construction.
 *
 * ПОЧЕМУ ЧИСЛА НЕ ВЫПИСАНЫ, А СРАВНИВАЮТСЯ МЕЖДУ СОБОЙ
 *
 * Канон называет замер («y=80», «y=133») — это числа ЕГО окна. Кегль заголовка
 * задан `clamp(24px, 2vw, 32px)`, то есть зависит от ширины окна: в окне пробы
 * он 25.6px, и заголовок стоит на y=83. Выписанное число сделало бы пробу
 * функцией размера окна ранера, а не свойства продукта. Утверждается ОТНОШЕНИЕ:
 * четыре страницы одного ресурса совпадают между собой до точки.
 *
 * ЧТО ЗДЕСЬ НЕ УТВЕРЖДАЕТСЯ
 *
 * Ни одного имени класса, ни одной внутренней конструкции. Шапка находится по
 * тому, что её и делает шапкой на экране — по линии под заголовком: первый
 * предок заголовка, который рисует нижнюю границу. Такая привязка переживает
 * переименование компонента и умирает вместе с самой шапкой, а не с её кодом.
 */

/** Четыре страницы одного ресурса: список, форма создания, карточка, правка. */
interface Pages {
  list: string;
  create: string;
  card: string;
  edit: string;
  networkName: string;
}

/**
 * Сеть с ВЫНОСНЫМИ буквами в имени — «g», «y», «p», «q» уходят ниже базовой
 * линии. Имя ресурса подчиняется DNS-метке (строчная латиница), поэтому
 * кириллические «у» и «р» сюда не поставить; латинские выносные проверяют ту же
 * величину — высоту строки заголовка.
 */
async function resourceWithDescenders(page: Page, projectId: string): Promise<Pages> {
  const networkName = `gypq-net-${runTag()}`;
  const response = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: networkName, ipv4CidrBlocks: ["10.94.0.0/16"] },
  });
  const id = await createdResourceId(
    page,
    response,
    "networkId",
    (n) => `/vpc/v1/networks/${n}`,
    "сеть под пробу заголовка",
  );
  const base = `/projects/${projectId}/vpc/networks`;
  return {
    list: base,
    create: `${base}/create`,
    card: `${base}/${id}`,
    edit: `${base}/${id}/edit`,
    networkName,
  };
}

interface HeaderMeasurement {
  /** Заголовок: где стоит, каким кеглем набран, какой высоты его строка. */
  x: number;
  y: number;
  lineHeight: number;
  fontSize: number;
  text: string;
  /** Низ блока шапки — та самая линия под заголовком. */
  headerBottom: number;
  /** Видимый текст шапки ПОМИМО заголовка: содержимое правого слота. */
  neighbours: Array<{ text: string; top: number; bottom: number }>;
}

/**
 * Открывает адрес, дожидается ЗАГОЛОВКА С ОЖИДАЕМЫМ ТЕКСТОМ и меряет шапку.
 *
 * Ожидание — по условию, а не по времени, и условие выбрано содержательное:
 * дождаться «какого-нибудь h3» значило бы померить страницу, которая ещё не
 * знает своего предмета (карточка до ответа края показывает не имя ресурса).
 * Замер такого промежуточного состояния сравнивался бы с замером готовой
 * страницы — и расхождение читалось бы как дефект геометрии.
 */
async function openAndMeasure(page: Page, url: string, expectedTitle: string | RegExp): Promise<HeaderMeasurement> {
  await page.goto(url, { waitUntil: "domcontentloaded" });

  const title = page.locator("h3").first();
  await expect(
    title,
    `${url}: страница не назвала свой предмет заголовком — мерить нечего, ` +
      `и вердикта о геометрии такой прогон не даёт`,
  ).toHaveText(expectedTitle, { timeout: 40_000 });

  return measure(title);
}

async function measure(title: Locator): Promise<HeaderMeasurement> {
  return title.evaluate((el) => {
    // Шапка — первый предок, рисующий линию под заголовком. Место под линию
    // зарезервировано всегда: там, где её роль исполняет полоса вкладок, она
    // прозрачна, но по-прежнему занимает свою точку — иначе карточка поднимала
    // бы содержимое относительно списка.
    let header: Element | null = el.parentElement;
    while (header && getComputedStyle(header).borderBottomWidth === "0px") header = header.parentElement;
    if (!header) throw new Error("под заголовком нет блока с нижней границей — шапку не по чему опознать");

    const r = el.getBoundingClientRect();
    const neighbours: Array<{ text: string; top: number; bottom: number }> = [];
    header.querySelectorAll("*").forEach((n) => {
      if (n === el || n.contains(el) || el.contains(n)) return;
      const txt = (n.textContent ?? "").trim();
      if (!txt || n.children.length > 0) return;
      const rr = n.getBoundingClientRect();
      if (rr.width === 0 && rr.height === 0) return;
      neighbours.push({ text: txt, top: Math.round(rr.top), bottom: Math.round(rr.bottom) });
    });

    return {
      x: Math.round(r.x),
      y: Math.round(r.y),
      lineHeight: Math.round(r.height),
      fontSize: parseFloat(getComputedStyle(el).fontSize),
      text: (el.textContent ?? "").trim(),
      headerBottom: Math.round(header.getBoundingClientRect().bottom),
      neighbours,
    };
  });
}

// ─────────────────────────────────────────────────────────────────────────────

test("заголовок ресурса стоит на одной вертикали и одном кегле на всех его страницах", async ({ page }) => {
  // verifies #925
  //
  // Признак нарушения из канона: «заголовок садится на 2–4 точки иначе при
  // переходе между страницами». Замер до правки — 78 · 74 · 76 · 74 у четырёх
  // страниц одного ресурса; глазом это видно только в переходе, поэтому проба
  // открывает все четыре подряд и сравнивает их между собой.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);

  const list = await openAndMeasure(page, pages.list, "Облачные сети");
  const create = await openAndMeasure(page, pages.create, "Создать облачную сеть");
  const card = await openAndMeasure(page, pages.card, pages.networkName);
  const edit = await openAndMeasure(page, pages.edit, pages.networkName);

  const all = { list, create, card, edit };
  for (const [name, meas] of Object.entries(all)) {
    expect(meas.x, `${name}: заголовок стоит на x=${meas.x} против ${list.x} у списка — при переходе текст дёргается по горизонтали`).toBe(
      list.x,
    );
    expect(meas.y, `${name}: заголовок стоит на y=${meas.y} против ${list.y} у списка — при переходе текст прыгает по вертикали`).toBe(
      list.y,
    );
    expect(meas.fontSize, `${name}: кегль заголовка ${meas.fontSize} против ${list.fontSize} у списка — страницы набраны разным шрифтом`).toBe(
      list.fontSize,
    );
  }
});

test("линия под шапкой стоит на одной высоте, чем бы правый слот ни заполнили", async ({ page }) => {
  // verifies #925
  //
  // Норма канона: «геометрия шапки не зависит от содержимого правого слота и от
  // наличия линии». Утверждение без положительного контроля здесь было бы
  // вакуумным: сравнить две ПУСТЫЕ шапки легко, и совпадение ничего не скажет.
  // Поэтому проба сперва показывает, что слоты РАЗНЫЕ — у списка в шапке есть
  // ручки, у формы создания их нет вовсе, — и только потом требует совпадения
  // низа блока.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);

  const list = await openAndMeasure(page, pages.list, "Облачные сети");
  const create = await openAndMeasure(page, pages.create, "Создать облачную сеть");
  const card = await openAndMeasure(page, pages.card, pages.networkName);

  // Положительный контроль: слоты действительно различаются наполнением.
  expect(
    list.neighbours.length,
    "в шапке списка нет ни одной ручки — сравнивать нечего, и совпадение низа " +
      "доказывало бы лишь то, что обе шапки пусты",
  ).toBeGreaterThan(0);
  expect(
    create.neighbours.map((n) => n.text),
    "в шапке формы создания появилось содержимое правого слота — предмет пробы исчез",
  ).toEqual([]);

  expect(
    create.headerBottom,
    `форма: линия под шапкой на y=${create.headerBottom} против ${list.headerBottom} у списка — ` +
      `пустой правый слот сжал шапку, и содержимое страницы поехало вверх`,
  ).toBe(list.headerBottom);
  expect(
    card.headerBottom,
    `карточка: низ шапки ${card.headerBottom} против ${list.headerBottom} у списка — ` +
      `место под линию не зарезервировано там, где её роль исполняют вкладки`,
  ).toBe(list.headerBottom);
});

test("заголовок называет предмет: тип на списке, имя экземпляра на карточке, действие на форме", async ({ page }) => {
  // verifies #925
  //
  // Канон: заголовок называет ПРЕДМЕТ. Признак нарушения — «Создание» без
  // предмета и «Список» как заголовок: оба стоят одинаковыми на каждой странице
  // своего вида, то есть не различают ничего.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);

  const list = await openAndMeasure(page, pages.list, /\S/);
  expect(
    list.text,
    `список назвал себя «${list.text}» — заголовок списка обязан называть ТИП во множественном`,
  ).toBe("Облачные сети");

  const create = await openAndMeasure(page, pages.create, /\S/);
  expect(
    create.text,
    `форма назвала себя «${create.text}» — заголовок формы обязан называть действие ВМЕСТЕ с предметом`,
  ).toBe("Создать облачную сеть");

  const card = await openAndMeasure(page, pages.card, /\S/);
  expect(
    card.text,
    `карточка назвала себя «${card.text}» вместо имени экземпляра «${pages.networkName}» — ` +
      `заголовок называет вид ресурса там, где обязан называть сам ресурс`,
  ).toBe(pages.networkName);
});

test("над заголовком нет надзаголовка, а сам заголовок на месте", async ({ page }) => {
  // verifies #925
  //
  // Отрицание в паре с положительным: «надзаголовка нет» в одиночку зеленеет на
  // странице, которая не отрисовала вообще ничего. Поэтому сперва утверждается,
  // что заголовок виден и назвал предмет, и лишь затем — что выше него в шапке
  // не стоит ни одной строки.
  //
  // Здесь стоял надзаголовок-родитель: «VPC» над списком, «ОБЛАЧНАЯ СЕТЬ» над
  // карточкой, прописными и с чёрточкой. Родителя называют крошки и подсветка
  // рейла; четвёртое место занимало строку над каждым заголовком продукта.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);

  for (const [name, url, expected] of [
    ["список", pages.list, "Облачные сети"],
    ["форма создания", pages.create, "Создать облачную сеть"],
    ["карточка", pages.card, pages.networkName],
  ] as const) {
    const meas = await openAndMeasure(page, url, expected);

    // Положительный контроль: предмет назван — значит есть чему предшествовать.
    expect(meas.text, `${name}: заголовок пуст, отрицание ниже утверждало бы о пустой странице`).not.toBe("");

    const above = meas.neighbours.filter((n) => n.bottom <= meas.y);
    expect(
      above.map((n) => n.text),
      `${name}: над заголовком «${meas.text}» стоит строка — надзаголовок вернулся и занял ` +
        `строку над каждым заголовком продукта`,
    ).toEqual([]);
  }
});

test("раздел назван один раз: на списке крошки не повторяют заголовок, на карточке повторения нет", async ({
  page,
}) => {
  // verifies #925
  //
  // Крошки называют ПУТЬ, заголовок — ПРЕДМЕТ. На списке они говорили одно и то
  // же слово: «… / Облачные сети» в крошках и «Облачные сети» заголовком двадцатью
  // точками ниже и вчетверо крупнее. На карточке заголовок — имя экземпляра, и
  // звено раздела ведёт назад, к списку: там оно обязано остаться.
  //
  // Обе стороны утверждаются на ОДНОМ и том же ресурсе: проба, проверяющая
  // только снятие звена, зеленела бы и на крошках, снятых везде.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);
  const breadcrumb = page.locator(".context-breadcrumb").first();

  const list = await openAndMeasure(page, pages.list, "Облачные сети");
  const listPath = await breadcrumb.innerText();
  expect(
    listPath,
    `список: крошки «${listPath.replace(/\n/g, " / ")}» пусты — сравнивать не с чем, ` +
      `и отрицание ниже ничего не утверждает`,
  ).toContain("VPC");
  expect(
    listPath,
    `список: крошки повторяют заголовок «${list.text}» — раздел назван дважды на одной странице`,
  ).not.toContain(list.text);

  const card = await openAndMeasure(page, pages.card, pages.networkName);
  const cardPath = await breadcrumb.innerText();
  expect(
    cardPath,
    `карточка: в крошках нет раздела «Облачные сети» — путь назад к списку оборван, ` +
      `а повторения с заголовком «${card.text}» здесь и не было`,
  ).toContain("Облачные сети");
});

test("имени модуля во втором сайдбаре нет, а перечень его ресурсов есть", async ({ page }) => {
  // verifies #925
  //
  // Модуль назван иконкой рейла и крошками. Здесь его имя стояло прописными
  // («VIRTUAL PRIVATE CLOUD») с линией под ним — то есть набрано так, что
  // привлекало внимание сильнее самого перечня, ради которого колонка заведена.
  //
  // Утверждается ВИДИМЫЙ текст. Доступное имя панели (`aria-label`) модуль
  // называть обязано — иначе перечень ссылок останется без имени для тех, кто
  // не видит рейла; предмет правила — надпись на экране, а не имя для чтения
  // вслух.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);

  await openAndMeasure(page, pages.list, "Облачные сети");

  const panel = page.locator('nav[aria-label^="Ресурсы:"]').first();
  await expect(panel, "второго сайдбара нет вовсе — утверждать о его содержимом нечего").toBeVisible({
    timeout: 30_000,
  });

  // Положительный контроль: панель показывает то, ради чего заведена.
  const visibleText = await panel.innerText();
  expect(
    visibleText,
    "во втором сайдбаре нет перечня ресурсов модуля — отрицание ниже зеленело бы на пустой колонке",
  ).toContain("Облачные сети");

  const moduleName = (await panel.getAttribute("aria-label"))?.replace(/^Ресурсы:\s*/, "") ?? "";
  expect(moduleName, "панель не назвала модуль даже доступным именем — сравнивать не с чем").not.toBe("");

  // СВЕРКА БЕЗ ОГЛЯДКИ НА РЕГИСТР, И ЭТО НЕ ПРИДИРКА К ФОРМЕ ЗАПИСИ.
  //
  // Первая редакция сравнивала видимый текст с доступным именем дословно и
  // ОСТАЛАСЬ ЗЕЛЁНОЙ на возвращённом дефекте: имя набиралось `text-transform:
  // uppercase`, `innerText` отдаёт его уже преобразованным («VIRTUAL PRIVATE
  // CLOUD»), а доступное имя приходит как есть («Virtual Private Cloud»). То
  // есть проба не поймала бы РОВНО ТУ форму, которая в продукте и стояла:
  // надпись прописными. Регистр здесь — оформление, а не другое слово.
  const upper = (t: string) => t.toUpperCase().replace(/\s+/g, " ");
  expect(
    upper(visibleText),
    `второй сайдбар печатает имя модуля «${moduleName}» — модуль назван третий раз ` +
      `(иконкой рейла, крошками и здесь), и набран он заметнее перечня, ради которого колонка стоит`,
  ).not.toContain(upper(moduleName));
  expect(
    upper(visibleText),
    "второй сайдбар печатает короткое имя модуля отдельной строкой — тот же надзаголовок, только сбоку",
  ).not.toMatch(/(^| )VPC( |$)/);
});

test("строка заголовка вмещает шрифт целиком и не выходит за свою шапку", async ({ page }) => {
  // verifies #925
  //
  // Канон: «высота строки заголовка вмещает шрифт целиком (1.35 от кегля)»;
  // признак нарушения — «нижние хвосты букв срезаны или наползают на вкладки».
  // Имя ресурса выбрано с выносными буквами («g», «y», «p», «q»), иначе проба
  // мерила бы строку, у которой ничего не свисает, и была бы зелена при любой
  // высоте.
  //
  // Утверждаются ДВЕ величины, и вторая — про наползание. Высота строки одна не
  // закрывает вопрос: строка может быть достаточной и всё равно выходить за
  // блок шапки, если блок посчитан отдельно от неё.
  test.setTimeout(240_000);
  const { projectId } = await tenantWithProject(page);
  const pages = await resourceWithDescenders(page, projectId);

  const card = await openAndMeasure(page, pages.card, pages.networkName);

  expect(
    /[gypq]/.test(card.text),
    `в заголовке «${card.text}» нет ни одной выносной буквы — проба мерила бы строку, ` +
      `которой нечем вылезти, и молчала бы при любой её высоте`,
  ).toBe(true);

  const ratio = card.lineHeight / card.fontSize;
  expect(
    ratio,
    `строка заголовка ${card.lineHeight} при кегле ${card.fontSize} — это ${ratio.toFixed(2)} ` +
      `от кегля. Ниже базовой линии шрифт уводит до 0.22 кегля, поэтому при таком множителе ` +
      `хвосты «${card.text}» выходят за свою строку и наползают на то, что стоит ниже`,
  ).toBeGreaterThanOrEqual(1.3);

  expect(
    card.y + card.lineHeight,
    `низ строки заголовка ${card.y + card.lineHeight} против низа шапки ${card.headerBottom} — ` +
      `заголовок выходит за свой блок и наползает на полосу вкладок`,
  ).toBeLessThanOrEqual(card.headerBottom);
});
