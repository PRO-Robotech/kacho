// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { newSeed } from "./ceremony-seed";
import { registerAndSignIn, test } from "./fixtures";
import { conditionNotCreated } from "./mail-receiver";

/**
 * Раздел «Система» предлагает мутацию регионов, зон и пулов адресов, и край её
 * обслуживает (kacho#3091, kacho#3094, продолжение #2692).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРИЗНАК
 *
 * Консоль одна на обе посадки — внешнюю (арендаторскую) и внутреннюю
 * (операторскую). До kacho#3094 она узнавала посадку пробой края и на внешней
 * снимала кнопки мутаций; после ADM-1 S1 проба посадки отвечала неверно, и
 * кнопка обещала действие, на которое край отвечал «маршрута нет» (kacho#3091).
 *
 * С kacho#3094 все три экрана стоят на ПУБЛИЧНЫХ службах (geo — регионы и зоны,
 * `AddressPoolService` — пулы адресов): консоль посадку больше не спрашивает,
 * ветви «раздел недоступен на этой посадке» в ней нет, и кнопка мутации на
 * экране есть всегда. Поэтому предмет пробы — согласие двух сторон одного
 * обещания: кнопка на экране И ответ края на мутацию, которую она отправила бы.
 * «Маршрута нет» на публичной мутации — это потерянная служба, а не посадка.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ И ОТКУДА БЕРЁТСЯ ОЖИДАНИЕ
 *
 *   - край обслуживает мутацию каждого экрана: аноним получает на неё отказ
 *     входа (401), а не «маршрута нет» (404). Тело пустое, носителя нет — до
 *     службы запрос не доходит, ничего не заводится;
 *   - арендатор видит на каждом экране кнопку этой мутации. Решение о показе
 *     судит не права: отказ в правах остаётся отказом в правах и называется
 *     словами (его утверждает `system-admin-public.spec.ts`).
 *
 * Обе стороны утверждаются положительно и на непустом предмете: экран сначала
 * обязан отрисоваться подписью, которую рисует только модуль раздела, и только
 * после этого судится кнопка.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДПОСЫЛКА «401 = ОБСЛУЖИВАЕТСЯ» (kacho#3109)
 *
 * Отказ входа 401 выдаёт и слой входа, отвечающий ДО маршрутизации: на такой
 * посадке аноним получил бы 401 и на путь, которого нет, и проба зеленела бы при
 * потерянной службе. Поэтому рядом с каждой мутацией экрана, на той же посадке и
 * в том же прогоне, спрашивается её отсутствующий близнец — тот же домен, та же
 * форма пути, коллекции нет (`absentTwinOf`). Он обязан дать 404. Не дал —
 * утверждение о мутации на этой посадке не судится: исход «условие не
 * создано» (не выполнилось), а не зелёное. Оба статуса печатаются аннотацией.
 * Решение — `judgeMutationServed`; его же судит инъекция в конце файла.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ПРОБА ОСТАВЛЯЕТ
 *
 * Ничего. Арендатора, заведённого `registerAndSignIn`, снимает разбор пробы
 * (`tenantRemoval` в `fixtures.ts`, путь — `tenant-removal.ts`, kacho#3106):
 * проект, аккаунт, личность — и на падении пробы тоже. Мутации экранов
 * анониму отвергаются до службы и ничего не заводят.
 */

/** Экран раздела и мутация, которую его кнопка отправила бы краю. */
interface Surface {
  name: string;
  address: string;
  mutation: string;
  /** Кнопка мутации на этом экране. */
  action: (page: Page) => ReturnType<Page["getByRole"]>;
  /** Подпись, которую рисует только сам модуль раздела: признак, что экран отрисован. */
  rendered: (page: Page) => ReturnType<Page["getByText"]>;
}

/** Исход сверки одного экрана. */
const SERVED = "обслуживается";
const PRECONDITION_UNMET = "предпосылка не выполнена";
const SERVICE_LOST = "служба потеряна";

interface Judged {
  outcome: typeof SERVED | typeof PRECONDITION_UNMET | typeof SERVICE_LOST;
  text: string;
}

/**
 * Отсутствующий близнец мутации: тот же домен и версия, последний сегмент —
 * коллекция, которой нет ни у одной службы. Форма пути та же, отличие одно.
 */
function absentTwinOf(mutation: string): string {
  return mutation.replace(/[^/]+$/, "zzNoSuchCollection");
}

/**
 * Решение по одному экрану: `absent` — ответ анониму на отсутствующий близнец,
 * `served` — на мутацию экрана. Порядок вопросов значим: без годной предпосылки
 * ответ на мутацию не различает «обслуживается» и «маршрута нет».
 */
function judgeMutationServed(
  s: Pick<Surface, "name" | "mutation">,
  absent: number,
  served: number,
): Judged {
  const twin = absentTwinOf(s.mutation);
  if (absent !== 404) {
    return {
      outcome: PRECONDITION_UNMET,
      text:
        `условие не создано: на этой посадке аноним на отсутствующий путь ${twin} получил ${absent}, а не 404 — ` +
        `ответ ${served} на мутацию экрана «${s.name}» (${s.mutation}) не отличает обслуживаемую службу от ` +
        "потерянной (вход отвечает раньше маршрутизации?); утверждение о мутации не судится",
    };
  }
  if (served !== 401) {
    return {
      outcome: SERVICE_LOST,
      text:
        `край ответил анониму на мутацию экрана «${s.name}» (${s.mutation}) ${served}, а отсутствующий ${twin} — 404: ` +
        "её обслуживает публичная служба, ждался отказ входа 401 (404 — служба потеряна: кнопка на экране " +
        "обещала бы «маршрута нет»)",
    };
  }
  return { outcome: SERVED, text: "" };
}

const SURFACES: Surface[] = [
  {
    // Регионы и зоны — на публичной службе geo (kacho#3094, #3092). Экрана
    // администраторов кластера здесь нет: его перечень край отдаёт только
    // держателю права, и арендатор видит отказ словами — это решение прав, а не
    // посадки (его утверждает system-admin-public.spec.ts).
    name: "регионы",
    address: "/system/regions",
    mutation: "/geo/v1/regions",
    action: (page) => page.getByRole("link", { name: /Создать/ }),
    rendered: (page) => page.getByText("Зоны", { exact: true }).first(),
  },
  {
    name: "зоны",
    address: "/system/zones",
    mutation: "/geo/v1/zones",
    action: (page) => page.getByRole("link", { name: /Создать/ }),
    rendered: (page) => page.getByText("Регионы", { exact: true }).first(),
  },
  {
    // Пулы адресов — их мутации с ADM-1 S1 обслуживает публичная
    // `AddressPoolService` (kacho#3091).
    name: "пулы адресов",
    address: "/system/address-pools",
    mutation: "/vpc/v1/addressPools",
    action: (page) => page.getByRole("link", { name: /Создать/ }),
    rendered: (page) => page.getByText("Регионы", { exact: true }).first(),
  },
];

test("раздел «Система» показывает мутацию регионов, зон и пулов адресов, и край её обслуживает", async ({
  page,
}, testInfo) => {
  // verifies #3091 — ложное «плоскость есть» после ADM-1 S1: кнопка мутации обязана вести в обслуживаемую мутацию.
  // verifies #3094 — экраны регионов и зон на публичной службе geo: кнопки на любой посадке.
  test.setTimeout(240_000);

  // Ответ края на мутацию каждого экрана — до входа, анонимно.
  const seed = await newSeed(testInfo);
  try {
    for (const s of SURFACES) {
      const twin = absentTwinOf(s.mutation);
      const absent = await seed.api.post(twin, { data: {} });
      testInfo.annotations.push({
        type: "предпосылка: отсутствующий путь",
        description: `${s.name}: POST ${twin} → ${absent.status()}`,
      });
      const res = await seed.api.post(s.mutation, { data: {} });
      testInfo.annotations.push({
        type: "мутация экрана",
        description: `${s.name}: POST ${s.mutation} → ${res.status()}`,
      });
      const judged = judgeMutationServed(s, absent.status(), res.status());
      if (judged.outcome === PRECONDITION_UNMET)
        conditionNotCreated(judged.text);
      expect(
        res.status(),
        `${judged.text} ${(await res.text()).slice(0, 300)}`,
      ).toBe(401);
    }
  } finally {
    await seed.dispose();
  }

  await registerAndSignIn(page);

  for (const s of SURFACES) {
    await page.goto(s.address, { waitUntil: "domcontentloaded" });
    await expect(
      s.rendered(page),
      `экран «${s.name}» не отрисовался — условие пробы не создано`,
    ).toBeVisible({
      timeout: 60_000,
    });
    await expect(
      s.action(page),
      `край обслуживает мутацию экрана «${s.name}» (${s.mutation}), а кнопки мутации на экране нет`,
    ).toBeVisible({ timeout: 30_000 });
  }
});

test("предпосылка «отсутствующий путь отличим от обслуживаемого» судит ответ 401 на мутацию экрана", () => {
  // verifies #3109 — «401 = обслуживается» не судится без предпосылки: край, отвечающий 401 и на отсутствующий путь, роняет её.
  for (const s of SURFACES) {
    const absent = absentTwinOf(s.mutation);

    // Инъекция: край отвечает отказом входа и на путь, которого нет (вход перед
    // маршрутизацией). 401 на мутацию экрана тогда ничего не различает.
    const masked = judgeMutationServed(s, 401, 401);
    expect(
      masked.outcome,
      `«${s.name}»: 401 на отсутствующий ${absent} обязан ронять предпосылку`,
    ).toBe(PRECONDITION_UNMET);
    expect(
      masked.text,
      `«${s.name}»: отказ предпосылки обязан назвать отсутствующий путь`,
    ).toContain(absent);

    // Законный близнец — отличается одним фактом: отсутствующий путь даёт 404.
    expect(
      judgeMutationServed(s, 404, 401).outcome,
      `«${s.name}»: годная предпосылка и 401 — обслуживается`,
    ).toBe(SERVED);

    // При годной предпосылке 404 на мутацию экрана — потерянная служба, а не посадка.
    expect(
      judgeMutationServed(s, 404, 404).outcome,
      `«${s.name}»: 404 на мутацию экрана — служба потеряна`,
    ).toBe(SERVICE_LOST);
  }
});
