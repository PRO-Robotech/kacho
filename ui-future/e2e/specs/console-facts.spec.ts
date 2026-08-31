// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Locator, type Page } from "@playwright/test";
import { copyToast, createdResourceId, runTag, scopeIsReady, tenantWithProject, test } from "./fixtures";

/**
 * Пользовательские сценарии консоли: ФАКТЫ, МЕТКИ, ПУСТОЕ СОСТОЯНИЕ.
 *
 * Разделы 5-7 канона (`docs/console-design-canon.md`). Каждая проба здесь
 * утверждает то, что видит человек, — текст, число, положение на экране, — и
 * ничего не утверждает про имена классов и состав разметки: класс переименуют,
 * компонент заменят, а находка вернётся при зелёной пробе.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЭТО БРАУЗЕРОМ, А НЕ МОДУЛЬНОЙ ПРОБОЙ
 *
 * Три предмета этого файла модульной пробе недоступны by construction:
 *
 *   • «не прыгает между разделами» — свойство ДВУХ страниц, а модульная проба
 *     монтирует одну и о второй не знает ничего;
 *   • «ни одной ячейки с сырым `true`» — свойство всего списка на живых данных,
 *     а не той строки, которую проба сама себе подложила;
 *   • «ужимается значение, а не ключ» — следствие настоящей ширины колонки,
 *     которой в jsdom нет: там всё имеет нулевой размер.
 */

/** Разделы, пустые у свежего арендатора. Четыре модуля намеренно: предмет
 *  канона — переход МЕЖДУ разделами, и один модуль его не проверяет. */
const EMPTY_SECTIONS = [
  { name: "подсети", path: "vpc/subnets" },
  { name: "адреса", path: "vpc/addresses" },
  { name: "шлюзы", path: "vpc/gateways" },
  { name: "наборы префиксов", path: "vpc/cidr-groups" },
  { name: "виртуальные машины", path: "compute/instances" },
  { name: "тома", path: "storage/volumes" },
  { name: "балансировщики", path: "nlb/load-balancers" },
  { name: "реестры", path: "registry/registries" },
];

interface Zones {
  section: string;
  picture: { x: number; y: number; w: number; h: number };
  heading: { x: number; y: number; w: number; h: number };
  button: { x: number; y: number; w: number; h: number };
  panel: { x: number; y: number; w: number; h: number };
  lines: string[];
  title: string;
}

/** Целые точки: «совпадает до точки» — это и есть единица, которой меряет глаз. */
async function box(l: Locator, what: string): Promise<{ x: number; y: number; w: number; h: number }> {
  const b = await l.boundingBox();
  expect(b, `${what}: элемента нет на экране, мерить нечего`).not.toBeNull();
  return { x: Math.round(b!.x), y: Math.round(b!.y), w: Math.round(b!.width), h: Math.round(b!.height) };
}

/**
 * Открывает пустой раздел и снимает с него зоны экрана.
 *
 * Ждём УСЛОВИЕ — появление самого экрана состояния, — а не время: на медленном
 * стенде пауза дала бы красное при исправном продукте, на быстром зелёное при
 * сломанном.
 */
async function emptySection(page: Page, projectId: string, sec: { name: string; path: string }): Promise<Zones> {
  await page.goto(`/projects/${projectId}/${sec.path}`, { waitUntil: "domcontentloaded" });

  const panel = page.locator('[role="status"]').first();
  await expect(
    panel,
    `раздел «${sec.name}»: экрана пустого состояния нет. У свежего арендатора список пуст, ` +
      `и на его месте обязан стоять экран «смотреть не на что, вот следующий шаг»`,
  ).toBeVisible({ timeout: 30_000 });

  const heading = panel.getByRole("heading");
  await expect(heading, `раздел «${sec.name}»: у пустого экрана нет заголовка`).toBeVisible();

  // Рисунок — ПЕРВЫЙ svg панели. Что это именно рисунок, а не значок внутри
  // кнопки, доказывает его размер: холст пустого состояния 208×136 (канон, §7),
  // и он проверяется ниже как отдельное утверждение.
  const picture = panel.locator("svg").first();
  await expect(picture, `раздел «${sec.name}»: у пустого экрана нет рисунка`).toBeVisible();

  const button = panel.getByRole("button");
  await expect(
    button,
    `раздел «${sec.name}»: на пустом экране нет кнопки создания, хотя глагол создания у ресурса есть`,
  ).toBeVisible();

  const text = await panel.innerText();
  return {
    section: sec.name,
    picture: await box(picture, `рисунок раздела «${sec.name}»`),
    heading: await box(heading, `заголовок раздела «${sec.name}»`),
    button: await box(button, `кнопка раздела «${sec.name}»`),
    panel: await box(panel, `экран раздела «${sec.name}»`),
    lines: text
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean),
    title: (await heading.innerText()).trim(),
  };
}

/**
 * ГЛАВНАЯ ПРОБА ФАЙЛА: рисунок и кнопка не прыгают при переходе между разделами.
 *
 * Владелец называл этот дефект трижды, и он ровно такой: текст меняется, а
 * вместе с ним съезжает картинка и кнопка, — переход между двумя соседними
 * разделами читается как переход в другой продукт. Зоны экрана объявлены
 * фиксированной высоты именно затем, чтобы этого не было (канон §7).
 *
 * ПОЧЕМУ РАВЕНСТВО КООРДИНАТ ЗДЕСЬ НЕ ТАВТОЛОГИЯ. Само по себе «на восьми
 * страницах всё совпало» зеленело бы и тогда, когда все восемь — одна и та же
 * страница: не загрузился модуль, промахнулся маршрут, вернулся общий экран
 * отказа. Поэтому рядом стоит обратное утверждение: заголовки ВСЕХ восьми
 * разделов различны. Пара «геометрия одна ↔ содержимое разное» и есть предмет.
 */
test("рисунок и кнопка пустого раздела стоят на одном месте во всех разделах", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  const captured: Zones[] = [];
  for (const sec of EMPTY_SECTIONS) captured.push(await emptySection(page, projectId, sec));

  const first = captured[0];

  // Холст рисунка — один ключ на все разделы (канон §7: 208×136).
  for (const zones of captured) {
    expect(
      { w: zones.picture.w, h: zones.picture.h },
      `раздел «${zones.section}»: холст рисунка ${zones.picture.w}×${zones.picture.h}, а канон объявляет 208×136 — ` +
        `рисунки разных разделов перестали быть одной серией`,
    ).toEqual({ w: 208, h: 136 });
  }

  for (const zones of captured.slice(1)) {
    expect(
      zones.picture,
      `рисунок прыгает: в разделе «${first.section}» он стоит на ${JSON.stringify(first.picture)}, ` +
        `в разделе «${zones.section}» — на ${JSON.stringify(zones.picture)}. Зоны экрана объявлены ` +
        `фиксированной высоты, значит разница может прийти только из содержимого — ` +
        `а содержимое двигать картинку не вправе`,
    ).toEqual(first.picture);

    expect(
      zones.button,
      `кнопка прыгает: в разделе «${first.section}» она стоит на ${JSON.stringify(first.button)}, ` +
        `в разделе «${zones.section}» — на ${JSON.stringify(zones.button)}. Именно этот прыжок читается ` +
        `как переход в другое место продукта`,
    ).toEqual(first.button);

    expect(
      zones.heading.y,
      `заголовок прыгает по вертикали: «${first.section}» — y=${first.heading.y}, ` +
        `«${zones.section}» — y=${zones.heading.y}`,
    ).toBe(first.heading.y);
  }

  // Положительный контроль: страницы РАЗНЫЕ. Без него совпадение координат
  // означало бы и «зоны держат место», и «мы восемь раз посмотрели на одно и то
  // же», а различить было бы нечем.
  const titles = captured.map((zones) => zones.title);
  expect(
    new Set(titles).size,
    `заголовки разделов не различаются (${JSON.stringify(titles)}): совпадение координат ` +
      `в таком прогоне ничего не доказывает — мерили одну и ту же страницу`,
  ).toBe(captured.length);
});

/**
 * Экран пустого раздела стоит по центру по ОБЕИМ осям (канон §7).
 *
 * Признак нарушения назван там же: экран прижат к левому верхнему углу. Это
 * отдельная мысль от предыдущей пробы: зоны могут держать высоту и при этом
 * весь столбец быть прижат к краю — тогда «не прыгает», но выглядит сбитой
 * вёрсткой.
 */
test("экран пустого раздела центрирован по обеим осям", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  for (const sec of EMPTY_SECTIONS.slice(0, 4)) {
    const zones = await emptySection(page, projectId, sec);
    const centerX = zones.panel.x + zones.panel.w / 2;

    for (const [what, b] of [
      ["рисунок", zones.picture],
      ["заголовок", zones.heading],
      ["кнопка", zones.button],
    ] as const) {
      expect(
        Math.abs(b.x + b.w / 2 - centerX),
        `раздел «${sec.name}»: ${what} не по центру по горизонтали — его середина ${b.x + b.w / 2}, ` +
          `середина экрана ${centerX}`,
      ).toBeLessThanOrEqual(1);
    }

    // По вертикали: столбец содержимого стоит серединой на середине панели.
    // Меряется по крайним видимым зонам — от верха рисунка до низа последней
    // зоны, — потому что канон фиксирует именно зоны, а не отдельные элементы.
    const bottom = await page.evaluate(() => {
      const panelEl = document.querySelector('[role="status"]') as HTMLElement | null;
      if (!panelEl) return null;
      const children = Array.prototype.map.call(panelEl.children, (c: Element) => c.getBoundingClientRect()) as DOMRect[];
      return children.length === 0 ? null : Math.round(children[children.length - 1].bottom);
    });
    expect(bottom, `раздел «${sec.name}»: у экрана состояния нет ни одной зоны`).not.toBeNull();

    const contentMiddle = (zones.picture.y + bottom!) / 2;
    const panelMiddle = zones.panel.y + zones.panel.h / 2;
    expect(
      Math.abs(contentMiddle - panelMiddle),
      `раздел «${sec.name}»: экран не по центру по вертикали — середина содержимого ` +
        `${contentMiddle}, середина отведённой ему области ${panelMiddle}. ` +
        `Прижатый к верху экран состояния читается как сбившаяся вёрстка`,
    ).toBeLessThanOrEqual(3);
  }
});

/**
 * У каждого пустого раздела есть ОПИСАНИЕ, и это не повторённый заголовок.
 *
 * Канон §7: «Описание есть у каждого ресурса». Экран без описания отвечает
 * только «пусто» — то есть повторяет то, что и так видно, и не объясняет
 * предмета тому, кто попал сюда впервые.
 */
test("у каждого пустого раздела есть описание, а не один заголовок", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  let inspected = 0;
  for (const sec of EMPTY_SECTIONS) {
    const zones = await emptySection(page, projectId, sec);
    inspected += 1;

    const explanation = zones.lines.filter((s) => s !== zones.title && s.length >= 60);
    expect(
      explanation.length,
      `раздел «${sec.name}»: под заголовком «${zones.title}» нет объяснения предмета. ` +
        `Прочитано с экрана: ${JSON.stringify(zones.lines)}`,
    ).toBeGreaterThan(0);
  }

  // Перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного».
  expect(inspected, "не осмотрено ни одного раздела — вердикта такой прогон не даёт").toBe(
    EMPTY_SECTIONS.length,
  );
});

// ─────────────────────────────────────────────────────────────────────────────
// РАЗДЕЛ 5 КАНОНА — ФАКТЫ И СОСТОЯНИЯ
// ─────────────────────────────────────────────────────────────────────────────

/** Сеть с группой правил: группа по умолчанию заводится ВМЕСТЕ с сетью и
 *  безусловно (решение владельца в самом сервисе), поэтому одна сеть уже даёт
 *  строку с фактом «Группа по умолчанию». */
async function networkWithGroups(page: Page, projectId: string): Promise<{ netId: string; own: string }> {
  const network = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: `net-fct-${runTag()}`, ipv4CidrBlocks: ["10.79.0.0/16"] },
  });
  const netId = await createdResourceId(page, network, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть под факты");

  // Вторая группа — ЯВНАЯ: без неё видна только одна сторона факта, а канон
  // требует обе. Проба с одной стороной зеленела бы на продукте, у которого
  // вторая сторона печатается сырым `false`.
  const own = `sg-fct-${runTag()}`;
  const response = await page.request.post("/vpc/v1/securityGroups", {
    data: { projectId, networkId: netId, name: own },
  });
  await createdResourceId(
    page,
    response,
    "securityGroupId",
    (id) => `/vpc/v1/securityGroups/${id}`,
    "группа безопасности под факты",
  );
  return { netId, own };
}

/** Тексты всех ячеек страницы. Таблица списка разложена на ДВЕ таблицы (шапка и
 *  тело), поэтому спрашивается страница целиком, а не первая найденная. */
async function cells(page: Page): Promise<string[]> {
  return page.evaluate(() =>
    Array.prototype.map
      .call(document.querySelectorAll("td"), (c: Element) => (c as HTMLElement).innerText.trim())
      .filter((t) => (t as string).length > 0),
  ) as Promise<string[]>;
}

/**
 * Ни одна ячейка списка не показывает сырое `true` / `false`.
 *
 * Канон §5: «Булево не показывается сырым. `true` в ячейке — находка». Сырое
 * значение отвечает на вопрос, которого никто не задавал: рядом с подписью «По
 * умолчанию» оно не говорит ни что группу назначают автоматически, ни что её
 * назначают явно.
 *
 * ОТРИЦАНИЕ СТОИТ В ПАРЕ С ПОЛОЖИТЕЛЬНЫМ. «Сырого `true` нет» зеленеет на
 * пустой странице, на не загрузившемся модуле и на списке без единой булевой
 * колонки. Поэтому в том же прогоне утверждается, что булевы факты на экране
 * ЕСТЬ и читаются словами предмета.
 */
test("ни одна ячейка списка не показывает сырое true или false", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);
  await networkWithGroups(page, projectId);

  const lists = [
    { name: "группы безопасности", path: `/projects/${projectId}/vpc/security-groups` },
    { name: "таблицы маршрутов", path: `/projects/${projectId}/vpc/route-tables` },
    { name: "облачные сети", path: `/projects/${projectId}/vpc/networks` },
    { name: "зоны", path: "/system/zones" },
    { name: "регионы", path: "/system/regions" },
  ];

  let readCount = 0;
  for (const list of lists) {
    await page.goto(list.path, { waitUntil: "domcontentloaded" });
    await expect
      .poll(async () => (await cells(page)).length, {
        message:
          `список «${list.name}» не отдал ни одной ячейки. Это УСЛОВИЕ пробы: на пустом списке ` +
          `утверждение «сырого true нет» истинно by construction и не значит ничего`,
        timeout: 30_000,
      })
      .toBeGreaterThan(0);

    const texts = await cells(page);
    readCount += texts.length;

    const raw = texts.filter((t) => /^(true|false)$/i.test(t));
    expect(
      raw,
      `список «${list.name}»: в ячейках стоит сырое булево ${JSON.stringify(raw)} — ` +
        `служебное слово вместо факта о ресурсе`,
    ).toEqual([]);

    // «Да»/«Нет» — та же болезнь другими словами: ответ вместо следствия.
    const yesNo = texts.filter((t) => /^(да|нет)$/i.test(t));
    expect(
      yesNo,
      `список «${list.name}»: ячейка отвечает «${yesNo.join(", ")}» — это ответ на вопрос, ` +
        `которого читатель не задавал, а не следствие`,
    ).toEqual([]);
  }

  // Положительный контроль: булевы факты на экране были, и они читаются словами.
  await page.goto(`/projects/${projectId}/vpc/security-groups`, { waitUntil: "domcontentloaded" });
  await expect
    .poll(async () => (await cells(page)).filter((t) => t === "Группа по умолчанию").length, {
      message:
        "в списке групп безопасности нет ячейки «Группа по умолчанию»: значит булевой колонки " +
        "на экране не было вовсе, и отрицание выше зеленело на её отсутствии",
      timeout: 30_000,
    })
    .toBeGreaterThan(0);

  expect(readCount, "не прочитано ни одной ячейки — вердикта такой прогон не даёт").toBeGreaterThan(0);
  console.log(`[перепись] ячеек прочитано: ${readCount}, списков: ${lists.length}`);
});

/**
 * Обе стороны факта названы СЛЕДСТВИЕМ, а не истинностью.
 *
 * Канон §5 приводит эту пару дословно: «Группа по умолчанию» / «Назначается
 * явно». Проба, видящая только одну сторону, ничего не говорит о второй —
 * а сырым печатается обычно именно вторая, потому что её реже открывают.
 */
test("обе стороны булева факта названы следствием", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);
  const { own } = await networkWithGroups(page, projectId);

  await page.goto(`/projects/${projectId}/vpc/security-groups`, { waitUntil: "domcontentloaded" });
  await expect
    .poll(async () => (await cells(page)).filter((t) => t === own).length, {
      message: `в списке нет явной группы «${own}» — предмет пробы не создан`,
      timeout: 30_000,
    })
    .toBeGreaterThan(0);

  const texts = await cells(page);
  expect(
    texts,
    `сторона «истина» не названа следствием. Прочитано: ${JSON.stringify(texts)}`,
  ).toContain("Группа по умолчанию");
  expect(
    texts,
    `сторона «ложь» не названа следствием — а печатается сырым чаще именно она. ` +
      `Прочитано: ${JSON.stringify(texts)}`,
  ).toContain("Назначается явно");
});

/**
 * Тон следует СМЫСЛУ, а не истинности (канон §5).
 *
 * Признак нарушения назван там же: «Свободен» выглядит выключенным, а «Удаление
 * разрешено» приглушено — хотя это единственная из двух сторон, о которой стоит
 * знать.
 *
 * ПОЧЕМУ ЭТО НЕ ТАВТОЛОГИЯ И ЧТО ИМЕННО СРАВНИВАЕТСЯ. Берутся два факта ОДНОЙ
 * строки, оба со стороны «ложь»: адрес не занят (`used=false`) и защиты от
 * удаления нет (`deletion_protection=false`). Если тон назначает истинность —
 * тон у них ОДИН, и проба краснеет. Если тон назначает смысл — «Свободен»
 * штатное положение, а «Удаление разрешено» то, о чём стоит знать, и тона
 * разные. Утверждение не может быть истинным by construction: два цвета
 * приходят из двух РАЗНЫХ объявлений колонок.
 */
test("тон факта следует смыслу, а не истинности", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);
  // Готовность области — ДО создания ресурсов: ожидание тратится на то, что всё
  // равно должно произойти, а к моменту перехода каркас уже знает проект.
  await scopeIsReady(page, projectId);

  // Адрес берётся РЕГИОНАЛЬНЫЙ — без зоны, — и это не мелочь размещения.
  //
  // Публичный адрес выделяется из полосы: зональный из полосы своей зоны,
  // региональный из полосы `zone_id IS NULL`. Стенд конвейера гарантирует ТОЛЬКО
  // вторую — её пригодность он проверяет отдельным шагом до проб. Зональной
  // полосы там нет, и просивший её адрес не создавался: операция возвращала
  // идентификатор (он чеканится ДО асинхронной части), а ресурс по нему не
  // читался никогда.
  //
  // Проба меряет ТОН ФАКТОВ, а не размещение: региональный адрес несёт те же
  // «Свободен» и «Удаление разрешено». Опираться на условие, которое стенд
  // создаёт и проверяет, — не послабление, а отказ от требования, которого
  // никто не обещал.
  const address = await page.request.post("/vpc/v1/addresses", {
    data: { projectId, name: `adr-tone-${runTag()}`, externalIpv4AddressSpec: {} },
  });
  await createdResourceId(page, address, "addressId", (id) => `/vpc/v1/addresses/${id}`, "публичный адрес под тон");

  await page.goto(`/projects/${projectId}/vpc/addresses`, { waitUntil: "domcontentloaded" });

  // СНАЧАЛА дожидаемся, что страница списка ОТКРЫЛАСЬ, и только потом требуем
  // от неё фактов. Это не терпимость к медленному стенду, а разделение двух
  // разных отказов.
  //
  // Заход по прямому адресу застаёт каркас в момент, когда он ещё читает
  // доступные арендатору области: пока их нет, модули объявлены недоступными и
  // содержимого на экране нет вообще. Проба, требующая факт сразу, падает с
  // сообщением «в списке адресов нет факта „Свободен"» — то есть обвиняет
  // отрисовку факта в том, что страница не открылась. Наблюдалось на стенде
  // конвейера: снимок показал дашборд с отключёнными кнопками модулей, ноль
  // строк таблицы и ни одной ссылки на проект.
  //
  // Ждём УСЛОВИЕ (заголовок раздела), а не время, и говорим о нём отдельно:
  // если страница не откроется, отказ назовёт именно это.
  await expect(
    page.getByRole("heading", { name: "IP-адреса" }).first(),
    "страница списка адресов не открылась по прямому адресу: каркас не отдал " +
      "область арендатора, и содержимого на экране нет вовсе — о тоне фактов " +
      "такой прогон не говорит ничего",
  ).toBeVisible({ timeout: 60_000 });

  const free = page.getByText("Свободен", { exact: true }).first();
  const allowed = page.getByText("Удаление разрешено", { exact: true }).first();
  await expect(
    free,
    "в списке адресов нет факта «Свободен» — свежевыделенный адрес никем не занят, и эта сторона " +
      "обязана быть на экране",
  ).toBeVisible({ timeout: 30_000 });
  await expect(
    allowed,
    "в списке адресов нет факта «Удаление разрешено» — защита от удаления по умолчанию снята",
  ).toBeVisible({ timeout: 30_000 });

  const colorOf = (l: Locator) => l.evaluate((el) => getComputedStyle(el as HTMLElement).color);
  const colorFree = await colorOf(free);
  const colorAllowed = await colorOf(allowed);

  expect(
    colorAllowed,
    `оба факта этой строки пришли из ЛЖИ, и тон у них совпал (${colorFree}). Значит тон назначает ` +
      `истинность, а не смысл: «Свободен» — штатное положение адреса, «Удаление разрешено» — ` +
      `единственная из двух сторон, о которой стоит знать, и выглядеть одинаково они не вправе`,
  ).not.toBe(colorFree);
});

// ─────────────────────────────────────────────────────────────────────────────
// РАЗДЕЛ 6 КАНОНА — МЕТКИ
// ─────────────────────────────────────────────────────────────────────────────

/** Значение метки берётся предельной длины (63 знака — потолок края). Тогда
 *  «значение не помещается» — свойство ПОСТРОЕНИЯ, а не удачной ширины окна:
 *  ряд меток ограничен по ширине, и 63 моноширинных знака в него не входят ни
 *  при каком разумном размере колонки. */
const LABEL_VALUE = "prod-eu-central-primary-cluster-0123456789-abcdefghij-klmnopqr";
const LABEL_KEY = "env";

async function networkWithLabel(page: Page, projectId: string): Promise<void> {
  const response = await page.request.post("/vpc/v1/networks", {
    data: {
      projectId,
      name: `net-lbl-${runTag()}`,
      ipv4CidrBlocks: ["10.81.0.0/16"],
      labels: { [LABEL_KEY]: LABEL_VALUE, tier: "backend" },
    },
  });
  await createdResourceId(page, response, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть с меткой");
}

interface Part {
  text: string;
  x: number;
  y: number;
  w: number;
  visible: number;
  total: number;
  weight: number;
  background: string;
}

/** Метка на экране: обе её части с их размерами и оформлением. */
async function labelParts(page: Page): Promise<Part[]> {
  const label = page.locator(`[title="Скопировать ${LABEL_KEY}=${LABEL_VALUE}"]`).first();
  await expect(
    label,
    `в списке нет метки ${LABEL_KEY}=${LABEL_VALUE}: предмет пробы не создан либо метки не показываются`,
  ).toBeVisible({ timeout: 30_000 });
  return label.evaluate((el) =>
    Array.prototype.map.call(el.children, (c: Element) => {
      const r = c.getBoundingClientRect();
      const s = getComputedStyle(c as HTMLElement);
      return {
        text: (c as HTMLElement).innerText.trim(),
        x: Math.round(r.x),
        y: Math.round(r.y),
        w: Math.round(r.width),
        visible: (c as HTMLElement).clientWidth,
        total: (c as HTMLElement).scrollWidth,
        weight: Number(s.fontWeight),
        background: s.backgroundColor,
      };
    }),
  ) as Promise<Part[]>;
}

/**
 * Ключ метки виден ОТДЕЛЬНО от значения (канон §6).
 *
 * Метки ищут по ключу — «что тут по env», «есть ли owner», — и ключ обязан
 * находиться сразу. Одной строкой `env=dev` он не находится: `team=networking`
 * и `teamnet=working` на беглый взгляд одинаковы.
 */
test("ключ метки виден отдельно от значения и набран заметнее", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);
  await networkWithLabel(page, projectId);
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const parts = await labelParts(page);
  expect(
    parts.map((part) => part.text),
    "метка нарисована не двумя частями: ключ и значение слиты в одну строку, и глаз не " +
      "отличает, где кончается ключ",
  ).toEqual([LABEL_KEY, LABEL_VALUE]);

  const [key, value] = parts;
  expect(
    key.x + key.w,
    `ключ стоит не слева от значения: ключ занимает [${key.x}, ${key.x + key.w}], ` +
      `значение начинается на ${value.x}`,
  ).toBeLessThanOrEqual(value.x + 1);
  expect(
    Math.abs(key.y - value.y),
    `ключ и значение стоят на разных строках (y=${key.y} против y=${value.y}) — ` +
      `ряд меток обязан идти одной строкой, иначе список идёт лесенкой`,
  ).toBeLessThanOrEqual(1);

  expect(
    key.weight,
    `ключ набран не заметнее значения (вес ${key.weight} против ${value.weight}): ` +
      `сначала «про что», потом «какое»`,
  ).toBeGreaterThan(value.weight);
  expect(
    key.background,
    `у ключа нет собственной заливки (${key.background}) — именно она отделяет его от значения ` +
      `вместо знака равенства, который приходится читать`,
  ).not.toBe(value.background);
});

/**
 * Ужимается ЗНАЧЕНИЕ, а не ключ (канон §6).
 *
 * Обрезанное значение остаётся понятным, обрезанный ключ — нет: по нему метку и
 * узнают. Пара утверждений неделима: «значение обрезано» без «ключ цел»
 * зеленело бы на метке, обрезанной целиком.
 */
test("при нехватке ширины ужимается значение метки, а не её ключ", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);
  await networkWithLabel(page, projectId);
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const [key, value] = await labelParts(page);

  expect(
    value.total,
    `значение метки (${LABEL_VALUE.length} знаков) поместилось целиком: видно ${value.visible} ` +
      `при полной ширине ${value.total}. Тогда проба ничего не проверяет — ряд меток обязан ` +
      `быть уже предельного значения, иначе ужимать нечего`,
  ).toBeGreaterThan(value.visible);

  expect(
    key.total,
    `ужался КЛЮЧ: видно ${key.visible} при полной ширине ${key.total}. Обрезанный ключ ` +
      `перестаёт отвечать на вопрос «про что эта метка»`,
  ).toBeLessThanOrEqual(key.visible + 1);
});

/**
 * В буфер уходит МАШИННАЯ форма `ключ=значение` (канон §6).
 *
 * На экране ключ и значение разведены нарочно, а вставляют их в фильтр или в
 * вызов слитно. Проба утверждает обе половины: что на экране слитной формы НЕТ
 * и что в буфер попадает именно она.
 */
test("клик по метке кладёт в буфер машинную форму ключ=значение", async ({ page }) => {
  // verifies #925
  
  const { projectId } = await tenantWithProject(page);
  await networkWithLabel(page, projectId);
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const label = page.locator(`[title="Скопировать ${LABEL_KEY}=${LABEL_VALUE}"]`).first();
  await expect(label, "метки нет на экране — предмет пробы не создан").toBeVisible({ timeout: 30_000 });

  const visibleText = (await label.innerText()).replace(/\s+/g, " ").trim();
  expect(
    visibleText,
    `на экране метка показана слитной формой «${visibleText}» — тогда разведение ключа и значения ` +
      `не состоялось, и копировать отдельную форму незачем`,
  ).not.toContain("=");

  await label.click();
  // Читается то, что продукт СКАЗАЛ: подпись успеха несёт саму строку и
  // показывается только при состоявшемся копировании (помощник возвращает
  // исход, на отказе подпись другая). Системный буфер не спрашивается — вне
  // защищённого контекста его нет, и проба утверждала бы о посадке стенда.
  await expect(
    copyToast(page, `${LABEL_KEY}=${LABEL_VALUE}`),
    "клик по метке не дал машинной формы: вставлять метку в фильтр или в вызов " +
      "пришлось бы, набирая знак равенства руками",
  ).toBeVisible({ timeout: 15_000 });
});

test("список адресов показывает ВНУТРЕННИЙ адрес, а не приветственный экран", async ({ page }) => {
  // verifies #927
  //
  // Отбор в списке отбрасывал строки без внешнего адреса и НЕ считался сужением,
  // поэтому страница уходила в приветственное состояние: консоль утверждала
  // «адресов нет» там, где край ответил «есть». Модульная проба видит логику
  // отбора, но не видит края — а нашли дефект именно браузером, и закрывает его
  // та проба, которая смотрит на то же, на что смотрел нашедший.
  const { projectId } = await tenantWithProject(page);
  await scopeIsReady(page, projectId);

  // Внутренний адрес берётся ИЗ ПОДСЕТИ, поэтому цепочка обязательна: сеть с
  // супернетом (без него нарезать не из чего) → зональная подсеть → адрес.
  // Полоса внешних адресов здесь ни при чём — проба не зависит от посева стенда.
  const network = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: `net-927-${runTag()}`, ipv4CidrBlocks: ["10.91.0.0/16"] },
  });
  const netId = await createdResourceId(page, network, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть под внутренний адрес");

  const zones = await page.request.get("/geo/v1/zones");
  expect(zones.ok(), "справочник зон недоступен — зональную подсеть создать негде. Это УСЛОВИЕ пробы, а не её предмет").toBeTruthy();
  const zone = ((await zones.json()) as { zones?: Array<{ id: string }> }).zones?.[0]?.id ?? "";
  expect(zone, "справочник зон ПУСТ — стенд непригоден для размещаемых ресурсов").not.toBe("");

  const subnet = await page.request.post("/vpc/v1/subnets", {
    data: { projectId, networkId: netId, name: `sub-927-${runTag()}`, zoneId: zone, ipv4CidrPrimary: "10.91.7.0/24" },
  });
  const subnetId = await createdResourceId(page, subnet, "subnetId", (id) => `/vpc/v1/subnets/${id}`, "подсеть под внутренний адрес");

  const address = await page.request.post("/vpc/v1/addresses", {
    data: { projectId, name: `adr-int-${runTag()}`, internalIpv4AddressSpec: { subnetId } },
  });
  const addressId = await createdResourceId(page, address, "addressId", (id) => `/vpc/v1/addresses/${id}`, "внутренний адрес");

  // Значение адреса выделяет сервер — спрашиваем его, а не угадываем.
  const created = await page.request.get(`/vpc/v1/addresses/${addressId}`);
  expect(created.ok(), "созданный внутренний адрес не читается").toBeTruthy();
  const ip = ((await created.json()) as { internalIpv4Address?: { address?: string } }).internalIpv4Address?.address ?? "";
  expect(ip, "край не назвал значение внутреннего адреса — сверять на странице нечего").not.toBe("");

  await page.goto(`/projects/${projectId}/vpc/addresses`, { waitUntil: "domcontentloaded" });

  // Сперва — что страница ОТКРЫЛАСЬ, и только потом требование факта: иначе
  // отказ обвинит отрисовку строки в том, что каркас ещё читает области.
  await expect(
    page.getByRole("heading", { name: "IP-адреса" }).first(),
    "страница списка адресов не открылась по прямому адресу: каркас не отдал область арендатора, " +
      "и содержимого на экране нет вовсе — о показе адреса такой прогон не говорит ничего",
  ).toBeVisible({ timeout: 60_000 });

  await expect(page.getByText(ip).first(), "внутренний адрес не показан в списке — консоль молчит о том, что край вернул").toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("Зарезервируйте первый IP-адрес"), "список ушёл в приветственное состояние при непустом ответе края").toHaveCount(0);
});
