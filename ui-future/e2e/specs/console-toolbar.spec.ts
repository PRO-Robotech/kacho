// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { createdResourceId, runTag, tenantWithProject, test } from "./fixtures";

/**
 * Канон консоли, разделы 3 («Строка инструментов списка») и 4 («Таблица и строки
 * свойств»), — браузером, на развёрнутом стенде.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЭТИ ПРАВИЛА НЕ ПРОВЕРИТЬ МОДУЛЬНОЙ ПРОБОЙ
 *
 * Все они — про ГЕОМЕТРИЮ: что левее чего, одной ли высоты ручки, сходятся ли
 * правые края значков в столбец. Модульная проба монтирует компонент в jsdom,
 * где раскладки нет вовсе: `getBoundingClientRect` возвращает нули, и любое
 * утверждение о порядке или высоте там зеленеет при любом коде. Плюс половина
 * правил держится общим листом стилей и КОНТЕКСТОМ места (строка свойств против
 * ячейки таблицы) — то есть тем, что появляется только при сборке страницы
 * целиком.
 *
 * ПОЧЕМУ УТВЕРЖДЕНИЯ — ЧИСЛА, А НЕ КЛАССЫ
 *
 * «У ряда есть класс, задающий высоту» — утверждение о разметке: класс
 * переименуют, правило снимут, а проба останется зелёной. Здесь спрашивается
 * то же, что видит глаз: собрать высоты всех ручек ряда и потребовать одного
 * значения; собрать правые края значков и потребовать совпадения. Такое
 * утверждение умирает вместе с дефектом и ни с чем другим.
 *
 * ПОЧЕМУ ЛОКАТОРЫ — ПОДПИСИ И СЕМАНТИКА HTML
 *
 * Ручки находятся по тому, чем они названы пользователю («Столбцы», «Создать»,
 * подсказка поля поиска), строки свойств — по `dt`/`dd`, то есть по смыслу
 * разметки, а не по имени нашего класса. Единственное исключение оговорено на
 * месте: рамку ручки приходится искать обходом вверх, потому что видимую рамку
 * поля рисует его оболочка, а не сам `input`.
 */

const VIEWPORT = { width: 1440, height: 900 };

/** Ряд считается одним, пока ручки стоят на одной высоте: разброс верха ≤ 4 точек. */
const ROW_SPREAD = 4;

interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
  right: number;
  radius: string;
  label: string;
  what: string;
}

/**
 * Геометрия трёх ручек ряда: поиск · «Столбцы» · «Создать».
 *
 * Рамку поля ищем обходом вверх до первого предка с ненулевой границей: видимую
 * рамку поля поиска рисует его ОБОЛОЧКА, а `input` внутри границы не имеет
 * вовсе. Мерить `input` значило бы мерить не то, что видит глаз, — и правило
 * «высоту задаёт оболочка, не поле внутри» проверялось бы своим же нарушением.
 * Обход не знает ни одного имени класса и переживёт смену библиотеки.
 */
async function rowControls(page: Page): Promise<Record<string, Box | null>> {
  return page.evaluate(() => {
    const wrapper = (el: Element | null): Element | null => {
      let cur: Element | null = el;
      for (let i = 0; i < 4 && cur; i++) {
        if (parseFloat(getComputedStyle(cur).borderTopWidth) > 0) return cur;
        cur = cur.parentElement;
      }
      return el;
    };
    const capture = (el: Element | null | undefined, what: string): Box | null => {
      if (!el) return null;
      const b = el.getBoundingClientRect();
      const cs = getComputedStyle(el);
      return {
        x: Math.round(b.x),
        y: Math.round(b.y),
        w: Math.round(b.width),
        h: Math.round(b.height),
        right: Math.round(b.right),
        radius: cs.borderRadius,
        label: (el.textContent ?? "").replace(/\s+/g, " ").trim(),
        what,
      };
    };
    const button = (re: RegExp) =>
      Array.from(document.querySelectorAll("button")).find((b) => re.test(b.textContent ?? ""));
    const field = Array.from(document.querySelectorAll("input")).find((i) =>
      /оиск/.test(i.getAttribute("placeholder") ?? ""),
    );
    return {
      search: capture(wrapper(field ?? null), "оболочка поля поиска"),
      columns: capture(button(/Столбцы/), "кнопка «Столбцы»"),
      create: capture(button(/^\s*Создать\s*$/), "кнопка «Создать»"),
    } as Record<string, Box | null>;
  });
}

/** Строки свойств карточки: где стоит значение и где — кнопка копирования. */
async function propertyRows(page: Page) {
  return page.evaluate(() => {
    const rows: Array<{
      label: string;
      columnX: number;
      columnRight: number;
      valueX: number;
      valueRight: number;
      copyX: number | null;
      copyRight: number | null;
      iconW: number | null;
      iconH: number | null;
      copiesInRow: number;
    }> = [];
    // `dd` — семантика HTML («значение определения»), а не наше имя класса:
    // проба переживёт переименование стилей и умрёт вместе со строкой свойств.
    document.querySelectorAll("dd").forEach((dd) => {
      const dt = dd.previousElementSibling;
      const b = dd.getBoundingClientRect();
      const cs = getComputedStyle(dd);
      const first = dd.firstElementChild?.getBoundingClientRect();
      if (!first) return;
      // Кнопка копирования строки названа «Скопировать: <подпись>» — по этой
      // подписи её и находим, а не по классу.
      const copy = Array.from(dd.querySelectorAll("button")).filter((btn) =>
        /^Скопировать:/.test(btn.getAttribute("aria-label") ?? btn.getAttribute("title") ?? ""),
      );
      const box = copy[0]?.getBoundingClientRect();
      const svg = copy[0]?.querySelector("svg")?.getBoundingClientRect();
      rows.push({
        label: (dt?.textContent ?? "").trim(),
        columnX: Math.round(b.x + parseFloat(cs.paddingLeft) + parseFloat(cs.borderLeftWidth)),
        columnRight: Math.round(b.right - parseFloat(cs.paddingRight)),
        valueX: Math.round(first.x),
        valueRight: Math.round(first.right),
        copyX: box ? Math.round(box.x) : null,
        copyRight: box ? Math.round(box.right) : null,
        iconW: svg ? Math.round(svg.width) : null,
        iconH: svg ? Math.round(svg.height) : null,
        copiesInRow: copy.length,
      });
    });
    return rows;
  });
}

/** Сеть с двумя подсетями РАЗНОЙ длины имени — иначе «столбец» и «за значением» неразличимы. */
async function networkWithSubnets(page: Page, projectId: string): Promise<string> {
  const resp = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: `net-tb-${runTag()}`, ipv4CidrBlocks: ["10.79.0.0/16"] },
  });
  const netId = await createdResourceId(page, resp, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть под пробу");

  const zones = await page.request.get("/geo/v1/zones");
  expect(
    zones.ok(),
    "справочник зон недоступен — зональную подсеть создать негде. Это УСЛОВИЕ пробы, " +
      "а не её предмет: о ручках и строках свойств такой прогон не говорит ничего",
  ).toBeTruthy();
  const zone = ((await zones.json()) as { zones?: Array<{ id: string }> }).zones?.[0]?.id ?? "";
  expect(zone, "справочник зон ПУСТ — стенд непригоден для размещаемых ресурсов").not.toBe("");

  // Имена намеренно разной длины: если копирование в ячейке идёт ЗА значением,
  // значки в двух строках встанут на разном x; если бы оно стояло столбцом —
  // на одном. Две одинаковых по длине строки этого различить не могут.
  const names: Array<[string, string]> = [
    [`s-a-${runTag()}`, "10.79.1.0/24"],
    [`s-bbbb-much-longer-name-${runTag()}`, "10.79.2.0/24"],
  ];
  for (const [name, cidr] of names) {
    const resp2 = await page.request.post("/vpc/v1/subnets", {
      data: { projectId, networkId: netId, name: name, zoneId: zone, ipv4CidrPrimary: cidr },
    });
    await createdResourceId(page, resp2, "subnetId", (id) => `/vpc/v1/subnets/${id}`, "подсеть под пробу");
  }
  return netId;
}

/** Открывает список и дожидается СТРОКИ ИНСТРУМЕНТОВ, а не «страница ответила». */
async function listWithControls(page: Page, path: string): Promise<void> {
  await page.setViewportSize(VIEWPORT);
  await page.goto(path, { waitUntil: "domcontentloaded" });
  await expect(
    page.getByRole("button", { name: /Столбцы/ }),
    `${path}: строка инструментов не появилась. Пустой список показывает приглашение ` +
      `создать первый ресурс и ручек не несёт — значит предмет пробы не создан, ` +
      `и это условие, а не вердикт о порядке ручек`,
  ).toBeVisible({ timeout: 45_000 });
}

test("порядок ручек списка: сузить, показать, сделать", async ({ page }) => {
  // verifies #925
  //
  // Канон §3: сужение (отборы, затем поиск) → выбор показываемого («Столбцы») →
  // действие («Создать»). Признак нарушения назван самим каноном: «Создать» в
  // начале ряда. Порядок читается слева направо, поэтому и утверждается он
  // сравнением ЛЕВЫХ КРАЁВ, а не порядком узлов в разметке: переставить узлы,
  // не сдвинув ручку (`order`, `row-reverse`, портал), — обычное дело, и проба
  // по разметке этого не увидела бы.
  const { projectId } = await tenantWithProject(page);
  await networkWithSubnets(page, projectId);
  await listWithControls(page, `/projects/${projectId}/vpc/networks`);

  const controls = await rowControls(page);
  for (const name of ["поиск", "столбцы", "создать"]) {
    expect(controls[name], `в строке инструментов не нашлось ручки «${name}» — сравнивать порядок не с чем`).not.toBeNull();
  }
  const search = controls.search!;
  const columns = controls.columns!;
  const create = controls.create!;

  // Сначала — что ряд ОДИН. Без этого сравнение по горизонтали ничего не значит:
  // ручка, уехавшая этажом выше, бывает «левее» чего угодно.
  const tops = [search.y, columns.y, create.y];
  expect(
    Math.max(...tops) - Math.min(...tops),
    `ручки стоят не одним рядом: поиск y=${search.y}, «Столбцы» y=${columns.y}, «Создать» y=${create.y}. ` +
      `Порядок слева направо имеет смысл только внутри одной строки`,
  ).toBeLessThanOrEqual(ROW_SPREAD);

  expect(
    search.x,
    `поиск стоит правее «Столбцов» (поиск x=${search.x}, «Столбцы» x=${columns.x}): ` +
      `сужение обязано идти ПЕРЕД выбором того, что показывать`,
  ).toBeLessThan(columns.x);

  expect(
    columns.x,
    `«Создать» стоит левее «Столбцов» (x=${create.x} против ${columns.x}): действие, ИЗМЕНЯЮЩЕЕ набор, ` +
      `обязано стоять после всех, которые набор только показывают. «Создать» в начале ряда — ` +
      `признак нарушения, названный каноном §3 дословно`,
  ).toBeLessThan(create.x);
});

test("ручки списка — одной высоты и одного радиуса", async ({ page }) => {
  // verifies #925
  //
  // Канон §3: одна высота и один радиус у всех ручек (32 и 8); признак
  // нарушения — четыре разные высоты в одном ряду. Мерим ОБОЛОЧКУ каждой ручки
  // (см. `rowControls`): правило прямо говорит, что высоту задаёт оболочка поля,
  // а не поле внутри, — и проба, померившая `input`, зеленела бы на том самом
  // дефекте, ради которого написана.
  const { projectId } = await tenantWithProject(page);
  await networkWithSubnets(page, projectId);
  await listWithControls(page, `/projects/${projectId}/vpc/networks`);

  const controls = await rowControls(page);
  const presentControls = Object.entries(controls).filter(([, box]) => box !== null) as Array<[string, Box]>;
  expect(
    presentControls.length,
    "в строке инструментов меньше двух ручек — «одна высота у всех» на одной ручке истинно даром",
  ).toBeGreaterThanOrEqual(2);

  const listing = presentControls.map(([name, box]) => `${name} (${box.what}) h=${box.h} r=${box.radius}`).join(", ");

  const heights = [...new Set(presentControls.map(([, box]) => box.h))];
  expect(
    heights,
    `в одном ряду ${heights.length} разных высот: ${listing}. Полоса читается как случайно ` +
      `составленная — каждая ручка принесла свою высоту вместо общей`,
  ).toHaveLength(1);
  expect(heights[0], `высота ручек ${heights[0]} вместо канонических 32: ${listing}`).toBe(32);

  const radii = [...new Set(presentControls.map(([, box]) => box.radius))];
  expect(radii, `в одном ряду ${radii.length} разных радиуса: ${listing}`).toHaveLength(1);
  expect(radii[0], `радиус ручек ${radii[0]} вместо канонических 8px: ${listing}`).toBe("8px");
});

test("кнопка называет действие, а не предмет страницы", async ({ page }) => {
  // verifies #925
  //
  // Канон §3: «Создать», а не «Создать таблицу маршрутов»; признак
  // нарушения — подпись повторяет заголовок страницы в двадцати точках левее.
  // Утверждение ПАРНОЕ: мало потребовать, чтобы на кнопке не было предмета —
  // надо убедиться, что предмет на странице ВООБЩЕ назван. Без второй половины
  // проба зеленела бы на странице без заголовка, то есть на худшем исходе.
  const { projectId } = await tenantWithProject(page);
  await networkWithSubnets(page, projectId);
  await listWithControls(page, `/projects/${projectId}/vpc/networks`);

  const title = (await page.getByRole("heading").first().innerText()).trim();
  expect(
    title,
    "у страницы списка нет заголовка — тогда предмет не назван нигде, и короткая подпись " +
      "кнопки означала бы не «предмет назван рядом», а «предмет не назван вовсе»",
  ).not.toBe("");

  // Подпись читается ТЕКСТОМ кнопки, а не её доступным именем: значок antd
  // вносит в доступное имя своё «plus», и утверждение о подписи спорило бы с
  // оформлением значка, а не с текстом, который видит глаз.
  const label = (await page.getByRole("button", { name: /Создать/ }).first().innerText())
    .replace(/\s+/g, " ")
    .trim();

  expect(
    label,
    `подпись кнопки — «${label}»: она называет ПРЕДМЕТ, хотя предмет уже назван заголовком ` +
      `«${title}» той же страницы. Кнопка обязана называть действие`,
  ).toBe("Создать");

  expect(
    label.toLowerCase(),
    `подпись кнопки «${label}» повторяет заголовок страницы «${title}»`,
  ).not.toContain(title.toLowerCase());
});

test("вкладка карточки повторяет порядок ручек списка", async ({ page }) => {
  // verifies #925
  //
  // Канон §3, последняя строка: тот же порядок ручек — на вкладках карточки;
  // признак нарушения — «на списке „Создать“ справа, на вкладке слева». Вкладка
  // собирает свой ряд ИЗ ДРУГИХ МЕСТ (поиск и «Столбцы» уезжают в слот у имени
  // ресурса, «Создать» — в слот действий), поэтому совпадение порядка здесь не
  // следует из порядка на списке и обязано утверждаться отдельно.
  const { projectId } = await tenantWithProject(page);
  const netId = await networkWithSubnets(page, projectId);

  await page.setViewportSize(VIEWPORT);
  await page.goto(`/projects/${projectId}/vpc/networks/${netId}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("tab", { name: /Подсети/ }).first().click();
  await expect(
    page.getByRole("button", { name: /Столбцы/ }),
    "на вкладке «Подсети» не появилась строка инструментов — вкладка с пустым списком " +
      "показывает приглашение и ручек не несёт: предмет пробы не создан",
  ).toBeVisible({ timeout: 45_000 });

  const controls = await rowControls(page);
  for (const name of ["поиск", "столбцы", "создать"]) {
    expect(controls[name], `на вкладке карточки не нашлось ручки «${name}»`).not.toBeNull();
  }
  const { search, columns, create } = controls as Record<string, Box>;

  const tops = [search.y, columns.y, create.y];
  expect(
    Math.max(...tops) - Math.min(...tops),
    `ручки вкладки стоят не одним рядом: поиск y=${search.y}, «Столбцы» y=${columns.y}, «Создать» y=${create.y}`,
  ).toBeLessThanOrEqual(ROW_SPREAD);

  expect(
    search.x,
    `на вкладке поиск правее «Столбцов» (${search.x} против ${columns.x}) — порядок ручек ` +
      `на вкладке разошёлся с порядком на списке`,
  ).toBeLessThan(columns.x);
  expect(
    columns.x,
    `на вкладке «Создать» левее «Столбцов» (${create.x} против ${columns.x}): на списке она справа, ` +
      `на вкладке слева — ровно тот признак нарушения, который канон называет дословно`,
  ).toBeLessThan(create.x);
});

test("ручки вкладки карточки — той же высоты, что на списке", async ({ page }) => {
  // verifies #925
  //
  // Продолжение предыдущей пробы: одинаков обязан быть не только ПОРЯДОК ручек,
  // но и сами ручки. Ряд, собранный из трёх слотов, — то же самое место работы:
  // пользователь не знает, что поиск приехал одним путём, а «Создать» другим, и
  // читает разнобой высот как случайно составленную полосу (канон §3, признак
  // нарушения — разные высоты в одном ряду).
  //
  // Проба отделена от порядка намеренно: порядок и высота ломаются по разным
  // причинам, и слитое утверждение назвало бы виновником то из двух, что
  // упало первым.
  const { projectId } = await tenantWithProject(page);
  const netId = await networkWithSubnets(page, projectId);

  await page.setViewportSize(VIEWPORT);
  await page.goto(`/projects/${projectId}/vpc/networks/${netId}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("tab", { name: /Подсети/ }).first().click();
  await expect(page.getByRole("button", { name: /Столбцы/ })).toBeVisible({ timeout: 45_000 });

  const controls = await rowControls(page);
  const presentControls = Object.entries(controls).filter(([, box]) => box !== null) as Array<[string, Box]>;
  expect(presentControls.length, "на вкладке меньше двух ручек — утверждать об одной высоте не о чем").toBeGreaterThanOrEqual(2);

  const listing = presentControls.map(([name, box]) => `${name} (${box.what}) h=${box.h} r=${box.radius}`).join(", ");
  const heights = [...new Set(presentControls.map(([, box]) => box.h))];

  expect(
    heights,
    `в ряду ручек вкладки карточки ${heights.length} разных высот: ${listing}. ` +
      `На списке те же ручки стоят одной высотой — значит правило до вкладки не доехало`,
  ).toHaveLength(1);
  expect(
    heights[0],
    `высота ручек вкладки ${heights[0]} вместо канонических 32: ${listing}`,
  ).toBe(32);
});

test("значение строки свойств стоит слева, одной колонкой", async ({ page }) => {
  // verifies #925
  //
  // Канон §4, первая строка: значение стоит слева, у левого края колонки.
  // Всегда. Утверждается двумя числами: (а) все значения начинаются на одном x —
  // иначе колонки нет; (б) этот x совпадает с левым краем СОДЕРЖИМОГО ячейки —
  // иначе значения выстроились в колонку, но не у левого края (по центру или
  // справа тоже «одинаково»). Одного (а) не хватило бы: право-выровненный
  // столбец прошёл бы его целиком.
  const { projectId } = await tenantWithProject(page);
  const netId = await networkWithSubnets(page, projectId);

  await page.setViewportSize(VIEWPORT);
  await page.goto(`/projects/${projectId}/vpc/networks/${netId}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("dd").first()).toBeVisible({ timeout: 45_000 });

  const rows = await propertyRows(page);
  expect(
    rows.length,
    "на карточке меньше трёх строк свойств — «одной колонкой» на такой выборке истинно даром",
  ).toBeGreaterThanOrEqual(3);

  const lefts = [...new Set(rows.map((row) => row.valueX))];
  expect(
    lefts,
    `значения начинаются на ${lefts.length} разных отступах (${lefts.join(", ")}): ` +
      rows.map((row) => `«${row.label}» x=${row.valueX}`).join("; ") +
      `. Колонки значений нет — глазу не за что зацепиться`,
  ).toHaveLength(1);

  for (const row of rows) {
    expect(
      Math.abs(row.valueX - row.columnX),
      `значение строки «${row.label}» стоит на x=${row.valueX} при левом крае колонки ${row.columnX} ` +
        `(отступ ${row.valueX - row.columnX} точек): значение обязано стоять У ЛЕВОГО КРАЯ, а не отодвинутым от него`,
    ).toBeLessThanOrEqual(2);
  }
});

test("копирование в строке свойств — столбцом у правого края", async ({ page }) => {
  // verifies #925
  //
  // Канон §4: копирование — ОДНА кнопка на строку, столбцом у правого края,
  // рисунок 13×13, правый край общий. «Проверяется замером, а не глазом» —
  // дословно; здесь замер и стоит.
  //
  // Правых краёв мало собрать — надо ещё потребовать, чтобы этот общий край
  // был краем КОЛОНКИ. Три значка, случайно оказавшиеся на одном x посреди
  // строки, дали бы то же совпадение и столбцом не были бы.
  const { projectId } = await tenantWithProject(page);
  const netId = await networkWithSubnets(page, projectId);

  await page.setViewportSize(VIEWPORT);
  await page.goto(`/projects/${projectId}/vpc/networks/${netId}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("dd").first()).toBeVisible({ timeout: 45_000 });

  const rows = await propertyRows(page);
  const withCopy = rows.filter((row) => row.copyRight !== null);
  expect(
    withCopy.length,
    `строк с копированием ${withCopy.length} — «правый край общий» требует хотя бы двух: ` +
      `на одной строке любое расположение образует «столбец»`,
  ).toBeGreaterThanOrEqual(2);

  for (const row of withCopy) {
    expect(
      row.copiesInRow,
      `в строке «${row.label}» кнопок копирования ${row.copiesInRow}: канон требует ОДНУ на строку`,
    ).toBe(1);
  }

  const edges = [...new Set(withCopy.map((row) => row.copyRight))];
  expect(
    edges,
    `значки копирования стоят на ${edges.length} разных правых краях (${edges.join(", ")}): ` +
      withCopy.map((row) => `«${row.label}» right=${row.copyRight}`).join("; ") +
      `. Столбец действий не читается столбцом — знак гуляет по строке вслед за длиной значения`,
  ).toHaveLength(1);

  for (const row of withCopy) {
    expect(
      Math.abs(row.copyRight! - row.columnRight),
      `значок строки «${row.label}» стоит правым краем на ${row.copyRight} при правом крае колонки ` +
        `${row.columnRight}: общий край есть, но он не край колонки — значит значки просто совпали`,
    ).toBeLessThanOrEqual(2);
  }

  for (const row of withCopy) {
    expect(
      [row.iconW, row.iconH],
      `рисунок значка в строке «${row.label}» — ${row.iconW}×${row.iconH} вместо 13×13: ` +
        `правые края сведены, но рисунки разной величины снова разбивают столбец`,
    ).toEqual([13, 13]);
  }
});

test("одно значение, два места: в ячейке копирование при значении, в строке свойств — у правого края", async ({
  page,
}) => {
  // verifies #925
  //
  // ПАРА ПРОТИВОПОЛОЖНЫХ УТВЕРЖДЕНИЙ ОБ ОДНОМ И ТОМ ЖЕ.
  //
  // Канон §4 говорит две вещи, которые по отдельности звучат как противоречие:
  // в строке свойств копирование уезжает столбцом к правому краю, а в ячейке
  // таблицы — наоборот, принадлежит значению и вправо не уезжает. Различие
  // принадлежит МЕСТУ, а не компоненту: одно и то же значение, попав в строку
  // свойств, обязано подчиниться ей, ничего о ней не зная.
  //
  // Поэтому проба берёт ОДИН И ТОТ ЖЕ идентификатор одной и той же сети и
  // смотрит на него в обоих местах. Две отдельные пробы, каждая про своё место,
  // зеленели бы и тогда, когда различие исчезло: они не спрашивают про
  // ПРОТИВОПОЛОЖНОСТЬ, а она здесь и есть предмет.
  const { projectId } = await tenantWithProject(page);
  const netId = await networkWithSubnets(page, projectId);

  // ── МЕСТО ПЕРВОЕ: ячейка таблицы. Копирование идёт ЗА значением.
  await listWithControls(page, `/projects/${projectId}/vpc/subnets`);

  const cells = await page.evaluate(() => {
    const rows: Array<{ valueRight: number; copyX: number; cellRight: number }> = [];
    document.querySelectorAll("tbody tr").forEach((tr) => {
      const td = tr.querySelector("td");
      if (!td) return;
      const copyBtn = Array.from(td.querySelectorAll("button")).find((b) =>
        /Скопировать имя/.test(b.getAttribute("aria-label") ?? b.getAttribute("title") ?? ""),
      );
      const value = td.querySelector("a");
      if (!copyBtn || !value) return;
      rows.push({
        valueRight: Math.round(value.getBoundingClientRect().right),
        copyX: Math.round(copyBtn.getBoundingClientRect().x),
        cellRight: Math.round(td.getBoundingClientRect().right),
      });
    });
    return rows;
  });

  expect(
    cells.length,
    "в колонке имени меньше двух строк со значком копирования — «идёт за значением» неотличимо " +
      "от «стоит столбцом»: на одной строке это одно и то же",
  ).toBeGreaterThanOrEqual(2);

  for (const cell of cells) {
    expect(
      cell.copyX - cell.valueRight,
      `в ячейке таблицы значок копирования оторвался от значения: значение кончается на ` +
        `${cell.valueRight}, значок начинается на ${cell.copyX} (разрыв ${cell.copyX - cell.valueRight} точек). ` +
        `Столбца действий в ячейке нет, и уехавший вправо значок оторвался бы от своего значения`,
    ).toBeLessThanOrEqual(12);
  }

  const leftsInCells = [...new Set(cells.map((cell) => cell.copyX))];
  expect(
    leftsInCells.length,
    `значки в ячейках выстроились в СТОЛБЕЦ (все на x=${leftsInCells[0]}) при именах разной длины: ` +
      `в ячейке копирование обязано следовать за значением, а не занимать общий слот`,
  ).toBeGreaterThanOrEqual(2);

  // ── МЕСТО ВТОРОЕ: строка свойств. То же копирование — у правого края, ДАЛЕКО от значения.
  await page.goto(`/projects/${projectId}/vpc/networks/${netId}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("dd").first()).toBeVisible({ timeout: 45_000 });

  const rows = await propertyRows(page);
  const shortest = rows
    .filter((row) => row.copyX !== null)
    .find((row) => row.valueRight < row.columnRight - 60);
  expect(
    shortest,
    "среди строк свойств не нашлось ни одной, где значение заметно короче колонки: " +
      "на значении во всю ширину «у правого края» и «за значением» совпадают, и различить их нечем",
  ).toBeDefined();

  expect(
    shortest!.copyX! - shortest!.valueRight,
    `в строке свойств «${shortest!.label}» значок прилип к значению: значение кончается на ` +
      `${shortest!.valueRight}, значок начинается на ${shortest!.copyX}. В строке свойств ` +
      `копирование — ОБЩИЙ столбец у правого края, а не спутник значения: место здесь решает иначе, чем ячейка`,
  ).toBeGreaterThan(12);

  expect(
    Math.abs(shortest!.copyRight! - shortest!.columnRight),
    `в строке свойств «${shortest!.label}» значок не дошёл до правого края колонки ` +
      `(${shortest!.copyRight} против ${shortest!.columnRight})`,
  ).toBeLessThanOrEqual(2);
});
