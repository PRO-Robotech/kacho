// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { runTag, tenantWithProject, test } from "./fixtures";

/**
 * Браузер читает поток изменений ЧЕРЕЗ КРАЙ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ БРАУЗЕРОМ, А НЕ ПРОБОЙ НА GO
 *
 * Проба на Go доказывает кадрирование и возобновление — и не может доказать
 * ровно того, ради чего заведена проекция: что ПОТРЕБИТЕЛЬ её потребит. У
 * браузера свои условия, и каждое из них уже роняло чужие потоковые ручки:
 *
 *   — личность едет ПЕЧЕНЬЕМ сессии, а не заголовком: `EventSource` заголовков
 *     не ставит вовсе, поэтому полоса, требующая `Authorization`, браузеру
 *     недоступна by construction;
 *   — посредник буферизует ответ, и события копятся до закрытия потока;
 *   — возобновление стандартом делает САМ браузер (`Last-Event-ID`), и оно
 *     работает либо не работает вне зависимости от того, что об этом думает
 *     сервер.
 *
 * Ни одно из трёх не наблюдаемо изнутри процесса края.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЭТИ ПРОБЫ НЕ УТВЕРЖДАЮТ
 *
 * Они не утверждают, что СПИСОК консоли обновляется потоком: списки сегодня
 * опрашиваются каждые 3–5 с, и проба, наблюдающая такой список, зеленела бы от
 * опроса — то есть была бы вакуумной. Снятие опроса и разбор потока страницей —
 * задача #1021, и её проба обязана глушить опрос (`page.route`), иначе
 * унаследует ту же вакуумность.
 *
 * Здесь утверждается ПРОЕКЦИЯ: страница, ничего не перезагружая, узнаёт об
 * изменении, которого не делала.
 */

/**
 * Владелец журнала, вид его предмета и адрес потока.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ИМЕННО ЭТОТ ВЛАДЕЛЕЦ И ЭТОТ ВИД (#1426)
 *
 * Требований к предмету потока ДВА, и второе забывают:
 *
 *   1. вид принадлежит словарю владельца — иначе подписка отвергается
 *      `INVALID_ARGUMENT` ещё до открытия потока;
 *   2. предмет ДЕЙСТВИТЕЛЬНО ПОПАДАЕТ В ЖУРНАЛ — иначе поток откроется,
 *      служебное сообщение придёт, и дальше будет молчание, неотличимое от
 *      «изменений не было». Отказ этот тихий, и проба на нём висит до бюджета.
 *
 * Здесь прежде стояла группа размещения compute, не отвечавшая НИ ОДНОМУ:
 * написание (`compute.placement_group`) не совпадало ни с одним живым, а её
 * репозиторий в журнал не пишет вовсе — ни на одном пути.
 *
 * ПОЧЕМУ НЕ МАШИНА, хотя compute журналирует именно её. Единственный вид,
 * который пишет репозиторий compute, — `Instance`, и создать её этой пробе
 * нечем: `CreateInstance` требует `machineTypeId`, а тип машины заводится
 * ТОЛЬКО административным глаголом (`InternalMachineTypeService`). Подъём
 * стенда сеет каталог geo и каталог блочного хранения (`make dev-up` зовёт
 * `seed-geo` и `seed-storage`), но типов машин не заводит НИ ОДНА цель, и
 * образа загрузки среди посеянного нет — сеются классы дисков. Проба на
 * машине была бы красной на СОЗДАНИИ, то есть по причине, к предмету потока
 * отношения не имеющей.
 *
 * Группа целей nlb отвечает обоим требованиям: её вид объявлен словарём
 * владельца, её создание ПИШЕТ строку журнала, а нужны ей только проект,
 * регион и проверка живости. Регион на стенде есть тем же посевом `seed-geo`;
 * что этого хватает — не рассуждение: ровно так группу целей создаёт
 * ИСПОЛНЯЕМАЯ проба `specs/contract-branches.spec.ts`.
 *
 * Написание вида — ТИП ОБЪЕКТА модели прав, единственное на всё дерево
 * (`docs/architecture/subscription-kind-vocabulary.md`, #1393). Слово, которым
 * владелец записал строку в свой журнал, наружу не выходит; весь словарь
 * приезжает полем `knownKinds` служебного сообщения открытия.
 */
const OWNER = "nlb";
const KIND = "nlb_target_group";
const STREAM = "/subscription/v1/events";

/** Кадр потока в том виде, в каком его видит страница. */
interface Frame {
  id: string;
  event: string;
  data: string;
}

/**
 * Читатель потока, живущий В СТРАНИЦЕ.
 *
 * Он ставится один раз и обслуживает обе пробы: первая открывает поток
 * `EventSource`-ом (той самой формой, что доступна консоли), вторая — запросом
 * с заголовком возобновления, потому что назвать позицию на ПЕРВОМ соединении
 * `EventSource` не умеет, а предмет второй пробы — именно названная позиция.
 */
async function installStreamReader(page: Page): Promise<void> {
  await page.addInitScript(() => {
    interface StreamFrame {
      id: string;
      event: string;
      data: string;
    }
    const store: { frames: StreamFrame[]; failure: string } = { frames: [], failure: "" };
    (window as unknown as Record<string, unknown>).__kachoStream = store;

    (window as unknown as Record<string, unknown>).__kachoStreamOpen = (url: string) => {
      const source = new EventSource(url);
      (window as unknown as Record<string, unknown>).__kachoStreamClose = () => source.close();
      const take = (name: string) => (evt: Event) => {
        const ev = evt as MessageEvent<string>;
        store.frames.push({ id: ev.lastEventId ?? "", event: name, data: ev.data });
      };
      source.addEventListener("opened", take("opened"));
      source.addEventListener("event", take("event"));
      source.onerror = () => {
        // Разрыв — штатное событие потока, а не отказ: браузер переподключится
        // сам. Записываем его отдельно от кадров, чтобы «поток молчал» было
        // отличимо от «поток не открылся».
        if (source.readyState === EventSource.CLOSED) store.failure = "поток закрыт краем";
      };
    };

    /**
     * Возобновление С НАЗВАННОЙ ПОЗИЦИИ. `EventSource` заголовков не ставит,
     * поэтому здесь запрос и разбор кадров вручную — та же полоса края, тот же
     * заголовок, что браузер шлёт сам при переподключении.
     */
    (window as unknown as Record<string, unknown>).__kachoStreamResume = async (
      url: string,
      lastEventId: string,
      wantFrames: number,
      budgetMs: number,
    ): Promise<StreamFrame[]> => {
      const got: StreamFrame[] = [];
      const controller = new AbortController();
      const deadline = setTimeout(() => controller.abort(), budgetMs);
      try {
        const res = await fetch(url, {
          headers: { "Last-Event-ID": lastEventId },
          signal: controller.signal,
        });
        if (!res.ok || !res.body) return got;
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = "";
        while (got.length < wantFrames) {
          const chunk = await reader.read();
          if (chunk.done) break;
          buf += decoder.decode(chunk.value, { stream: true });
          let cut = buf.indexOf("\n\n");
          while (cut >= 0) {
            const frame = buf.slice(0, cut);
            buf = buf.slice(cut + 2);
            const f: StreamFrame = { id: "", event: "message", data: "" };
            for (const line of frame.split("\n")) {
              if (line.startsWith("id:")) f.id = line.slice(3).trim();
              else if (line.startsWith("event:")) f.event = line.slice(6).trim();
              else if (line.startsWith("data:")) f.data += line.slice(5).trim();
            }
            // Двоеточие первым знаком — служебный кадр поддержания связи; он
            // не событие и в счёт не идёт.
            if (!frame.startsWith(":")) got.push(f);
            cut = buf.indexOf("\n\n");
          }
        }
        reader.cancel().catch(() => undefined);
      } catch {
        // Обрыв по бюджету — законный конец чтения: вернём собранное.
      } finally {
        clearTimeout(deadline);
      }
      return got;
    };
  });
}

/** Кадры, собранные страницей на текущий момент. */
async function frames(page: Page): Promise<Frame[]> {
  return page.evaluate(
    () => ((window as unknown as Record<string, { frames: Frame[] }>).__kachoStream?.frames ?? []) as Frame[],
  );
}

/**
 * Регион размещения — из общего справочника geo.
 *
 * Отказ и пустой справочник разделены намеренно: первое означает недоступную
 * зависимость, второе — свежеподнятый стенд, на котором размещаемый ресурс
 * создать негде. Оба — УСЛОВИЕ пробы, а не её предмет, и текст это говорит.
 */
async function anyRegionId(page: Page): Promise<string> {
  const res = await page.request.get("/geo/v1/regions");
  expect(
    res.ok(),
    "справочник регионов недоступен — условие пробы не создано: предмет потока " +
      "разместить негде, и о потоке такой прогон не говорит ничего",
  ).toBeTruthy();
  const body = (await res.json()) as { regions?: Array<{ id: string }> };
  const id = body.regions?.[0]?.id ?? "";
  expect(id, "справочник регионов ПУСТ — стенд непригоден для размещаемых ресурсов").not.toBe("");
  return id;
}

/**
 * Создать предмет ОТ ИМЕНИ ДРУГОГО КЛИЕНТА — минуя открытую страницу.
 *
 * Запросом, а не формой: предмет пробы — что страница узнаёт об изменении,
 * КОТОРОГО НЕ ДЕЛАЛА. Создание через открытую страницу это утверждение
 * обнулило бы.
 *
 * Тело — наименьшее, которое край принимает: проект, регион, порт группы и
 * проверка живости (она обязательна и обязана нести ровно одну ветвь протокола;
 * величины взяты у эталона `services/nlb/internal/domain/health_check_test.go`,
 * а не подобраны).
 */
async function createTargetGroup(
  page: Page,
  projectId: string,
  regionId: string,
  name: string,
): Promise<string> {
  const res = await page.request.post("/nlb/v1/targetGroups", {
    data: {
      projectId,
      regionId,
      name,
      port: 8080,
      healthCheck: {
        interval: "2s",
        timeout: "1s",
        unhealthyThreshold: 2,
        healthyThreshold: 2,
        tcp: { port: 8080 },
      },
    },
  });
  const text = await res.text();
  expect(
    res.status(),
    `создание предмета потока: край отверг — ${res.status()} ${text.slice(0, 300)}. ` +
      `Это УСЛОВИЕ пробы, а не её предмет`,
  ).toBe(200);
  const body = JSON.parse(text) as { metadata?: Record<string, string> };
  const id = body.metadata?.targetGroupId ?? "";
  expect(id, "создание предмета потока: операция не назвала targetGroupId").not.toBe("");
  return id;
}

test("страница узнаёт об изменении, сделанном другим клиентом, не перезагружаясь", async ({ page }) => {
  // verifies #1020 — КРАСНАЯ до фикса задачи: проекции потока на крае нет вовсе,
  // и `/subscription/v1/events` отвечает как несуществующий путь.
  test.setTimeout(240_000);

  await installStreamReader(page);
  const { projectId } = await tenantWithProject(page);

  // Счётчик загрузок документа: утверждение «без перезагрузки» обязано быть
  // проверяемым, а не подразумеваться из того, что проба не звала `goto`.
  let loads = 0;
  page.on("load", () => {
    loads += 1;
  });
  const regionId = await anyRegionId(page);
  await page.goto(`/projects/${projectId}/nlb/target-groups`, { waitUntil: "domcontentloaded" });
  const loadsAtStart = loads;

  const url = `${STREAM}?owner=${OWNER}&projectId=${projectId}&kinds=${KIND}`;
  await page.evaluate(
    (u) => (window as unknown as Record<string, (s: string) => void>).__kachoStreamOpen(u),
    url,
  );

  await expect
    .poll(async () => (await frames(page)).filter((f) => f.event === "opened").length, {
      message:
        `край не открыл поток на ${STREAM}: служебное сообщение открытия обязано ` +
        `прийти ПЕРВЫМ и ВСЕГДА, в том числе когда событий нет вовсе`,
      timeout: 30_000,
    })
    .toBe(1);

  const created = await createTargetGroup(page, projectId, regionId, `tg-stream-${runTag()}`);

  await expect
    .poll(
      async () =>
        (await frames(page)).filter((f) => f.event === "event" && f.data.includes(created)).length,
      {
        message:
          `страница не получила события о предмете ${created}, созданном другим клиентом: ` +
          `ради этого проекция и заводится`,
        timeout: 60_000,
      },
    )
    .toBeGreaterThanOrEqual(1);

  expect(
    loads,
    "страница перезагружалась: тогда утверждение о потоке ничего не значит — " +
      "новое состояние приехало бы и обычным чтением",
  ).toBe(loadsAtStart);
});

test("возобновление с позиции не теряет событий и не повторяет полученного", async ({ page }) => {
  // verifies #1020 — КРАСНАЯ до фикса задачи.
  //
  // Разрыв — штатное событие, а не отказ. Предмет пробы: позиция, названная
  // клиентом, отдаёт ВСЁ, что после неё, и НИЧЕГО из того, что она покрывает.
  // Обе половины утверждаются вместе: проверка одной потери зеленела бы на
  // сервере, который просто отдаёт журнал заново.
  test.setTimeout(240_000);

  await installStreamReader(page);
  const { projectId } = await tenantWithProject(page);
  const regionId = await anyRegionId(page);
  await page.goto(`/projects/${projectId}/nlb/target-groups`, { waitUntil: "domcontentloaded" });

  const tag = runTag();
  const url = `${STREAM}?owner=${OWNER}&projectId=${projectId}&kinds=${KIND}`;
  await page.evaluate(
    (u) => (window as unknown as Record<string, (s: string) => void>).__kachoStreamOpen(u),
    url,
  );
  await expect
    .poll(async () => (await frames(page)).filter((f) => f.event === "opened").length, {
      message: `край не открыл поток на ${STREAM}`,
      timeout: 30_000,
    })
    .toBe(1);

  const first = await createTargetGroup(page, projectId, regionId, `tg-resume-a-${tag}`);
  await expect
    .poll(
      async () =>
        (await frames(page)).filter((f) => f.event === "event" && f.data.includes(first)).length,
      { message: `поток не принёс первого предмета ${first}`, timeout: 60_000 },
    )
    .toBeGreaterThanOrEqual(1);

  const seen = await frames(page);
  const anchor = seen.filter((f) => f.event === "event" && f.data.includes(first)).at(-1)!;
  expect(
    anchor.id,
    "событие пришло без позиции: возобновиться с него нечем, и `Last-Event-ID` браузер не пошлёт",
  ).not.toBe("");

  // РАЗРЫВ на середине — и пока связи нет, мир меняется дважды.
  await page.evaluate(() =>
    (window as unknown as Record<string, () => void>).__kachoStreamClose(),
  );
  const second = await createTargetGroup(page, projectId, regionId, `tg-resume-b-${tag}`);
  const third = await createTargetGroup(page, projectId, regionId, `tg-resume-c-${tag}`);

  const resumed = await page.evaluate(
    async ([u, id]) =>
      (
        window as unknown as Record<
          string,
          (u: string, id: string, want: number, budget: number) => Promise<Frame[]>
        >
      ).__kachoStreamResume(u, id, 3, 60_000),
    [url, anchor.id] as const,
  );

  const payload = resumed.map((f) => f.data).join("\n");
  expect(
    payload.includes(second),
    `возобновление с позиции ${anchor.id} потеряло предмет ${second}, ` +
      `записанный ПОСЛЕ неё: разрыв соединения не вправе терять события`,
  ).toBe(true);
  expect(
    payload.includes(third),
    `возобновление с позиции ${anchor.id} потеряло предмет ${third}`,
  ).toBe(true);
  expect(
    resumed.filter((f) => f.event === "event" && f.data.includes(first)).length,
    `возобновление с позиции ${anchor.id} принесло предмет ${first}, который эта позиция ` +
      `ПОКРЫВАЕТ: клиент, ведущий состояние, применил бы его дважды`,
  ).toBe(0);
});
