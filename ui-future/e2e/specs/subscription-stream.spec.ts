// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
// Помощники берутся из ИСПОЛНЯЕМОГО набора: своей копии у пробы нет и не должно
// быть — вторая копия фикстур разошлась бы с первой молча. Проба приехала сюда из
// каталога ожидания, когда её условие было создано поставкой, и путь помощников —
// единственное, что переезд в ней изменил.
import { STREAM_PATH, createdResourceId, runTag, tenantWithProject, test } from "./fixtures";

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
 * Владелец журнала и его дешёвый предмет.
 *
 * Предмет выбран ЖУРНАЛИРУЕМЫЙ, а не просто дешёвый, и это различие стоило
 * прогона. Прежде здесь стояли `compute` и вид `compute.placement_group` —
 * группа размещения как «дешёвый предмет». Оба были неверны, и каждый по-своему:
 *
 *   — такого ВИДА нет: словарь compute закрыт в обе стороны и знает ровно
 *     `compute_instance`, поэтому поток отвергался ПРИ ОТКРЫТИИ, до всякого
 *     события («kinds: … is not a kind of this owner»);
 *   — такого ПРЕДМЕТА в журнале нет тоже: в журнал compute пишет единственный
 *     производитель (`instance_repo.go`, вид `Instance`), а группы размещения не
 *     журналируются вовсе. То есть починка одного лишь имени вида увела бы пробу
 *     из честного отказа в вечное ожидание события, которого никто не пишет, —
 *     отказ хуже исходного, потому что молчит.
 *
 * `vpc` + `vpc_network` держатся обеими сторонами: вид объявлен словарём
 * владельца, а строку журнала пишет создание сети (`network/create.go`, вид
 * `Network`, род `CREATED`) в той же writer-транзакции. Сеть остаётся дешёвой —
 * супернет ей для этого не нужен, поэтому соседние предметы не спорят за CIDR.
 */
const OWNER = "vpc";
const KIND = "vpc_network";

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
    /**
     * СОСТОЯНИЕ СВЯЗИ, А НЕ ТОЛЬКО КАДРЫ (#2016).
     *
     * Прежде здесь копились кадры и одна строка отказа, и отказ пробы звучал
     * «событий 0» — то есть НАЗЫВАЛ ЧИСЛО И МОЛЧАЛ О СОЕДИНЕНИИ. Под этим
     * молчанием неразличимы три разные беды: строку не отправили, строку не
     * довезли, и мы не дождались. Разбор поэтому начинался с догадки, а гонку
     * догадкой не поймать: она проходит и падает на одной ревизии.
     *
     * Считается ровно то, чем эти три различаются:
     *
     *   — `opened` больше единицы означает ПЕРЕОТКРЫТИЕ: между закрытием и
     *     новым соединением было окно, и это уже другой разговор, чем молчащий
     *     открытый поток;
     *   — `errors` растёт и на временном обрыве, который браузер лечит сам, —
     *     поэтому он считается ОТДЕЛЬНО от терминального закрытия;
     *   — отметки времени отвечают на «когда поток в последний раз что-то
     *     сказал»: открытый поток, молчащий с самого открытия, и открытый
     *     поток, только что принёсший чужое событие, — разные состояния.
     */
    const store: {
      frames: StreamFrame[];
      failure: string;
      opened: number;
      errors: number;
      openedAtMs: number;
      lastFrameAtMs: number;
      lastErrorAtMs: number;
    } = { frames: [], failure: "", opened: 0, errors: 0, openedAtMs: 0, lastFrameAtMs: 0, lastErrorAtMs: 0 };
    (window as unknown as Record<string, unknown>).__kachoStream = store;

    (window as unknown as Record<string, unknown>).__kachoStreamOpen = (url: string) => {
      const source = new EventSource(url);
      // Ссылка на само соединение: `readyState` спрашивается У ЖИВОГО ОБЪЕКТА в
      // момент разбора, а не восстанавливается из накопленных признаков.
      (window as unknown as Record<string, unknown>).__kachoStreamSource = source;
      (window as unknown as Record<string, unknown>).__kachoStreamClose = () => source.close();
      const take = (name: string) => (evt: Event) => {
        const ev = evt as MessageEvent<string>;
        store.frames.push({ id: ev.lastEventId ?? "", event: name, data: ev.data });
        store.lastFrameAtMs = Date.now();
        if (name === "opened") {
          store.opened += 1;
          if (store.openedAtMs === 0) store.openedAtMs = Date.now();
        }
      };
      source.addEventListener("opened", take("opened"));
      source.addEventListener("event", take("event"));
      source.onerror = () => {
        // Разрыв — штатное событие потока, а не отказ: браузер переподключится
        // сам. Записываем его отдельно от кадров, чтобы «поток молчал» было
        // отличимо от «поток не открылся».
        store.errors += 1;
        store.lastErrorAtMs = Date.now();
        if (source.readyState === EventSource.CLOSED) store.failure = "поток закрыт краем";
      };
    };

    /** Состояние связи глазами страницы — читается в момент разбора отказа. */
    (window as unknown as Record<string, unknown>).__kachoStreamState = () => {
      const src = (window as unknown as Record<string, EventSource | undefined>).__kachoStreamSource;
      const last = store.frames.length > 0 ? store.frames[store.frames.length - 1] : undefined;
      return {
        // -1 означает «соединение не открывали вовсе» — это НЕ одно из трёх
        // состояний `EventSource`, и путать его с «закрыто» нельзя.
        readyState: src === undefined ? -1 : src.readyState,
        opened: store.opened,
        errors: store.errors,
        failure: store.failure,
        frames: store.frames.length,
        events: store.frames.filter((f) => f.event === "event").length,
        lastEventId: last === undefined ? "" : last.id,
        openedAtMs: store.openedAtMs,
        lastFrameAtMs: store.lastFrameAtMs,
        lastErrorAtMs: store.lastErrorAtMs,
        nowMs: Date.now(),
      };
    };

    /**
     * Возобновление С НАЗВАННОЙ ПОЗИЦИИ. `EventSource` заголовков не ставит,
     * поэтому здесь запрос и разбор кадров вручную — та же полоса края, тот же
     * заголовок, что браузер шлёт сам при переподключении.
     *
     * ─────────────────────────────────────────────────────────────────────────
     * ЧИТАЕТ ДО ПРЕДМЕТА, А НЕ ДО ЧИСЛА КАДРОВ (#1540)
     *
     * Здесь стоял счётчик кадров: «дай мне три штуки — служебное открытие и два
     * события». Он держался допущения «одно создание = одно событие», и
     * допущение было ЛОЖНО: создание сети объявлялось тогда ТРЕМЯ строками вида
     * `vpc_network` — сама сеть, затем по `UPDATED` на каждое достроенное
     * умолчание. Поэтому «три кадра» выбирались служебным открытием и ПЕРВЫМИ
     * ДВУМЯ событиями ПЕРВОЙ сети — а до второй чтение не доходило вовсе, и
     * проба объявляла потерянным предмет, которого она не дочитала.
     *
     * СЕГОДНЯ ЧИСЛО ДРУГОЕ, И ЭТО ЛУЧШЕЕ ДОКАЗАТЕЛЬСТВО, ЧТО ЕГО ЗДЕСЬ БЫТЬ НЕ
     * ДОЛЖНО. Владелец журнала объявляет собранную сеть ОДИН раз (#1548): на
     * одно создание приходится по строке на каждый заведённый ресурс — сеть,
     * группа безопасности по умолчанию, таблица маршрутов по умолчанию, — то
     * есть три строки ТРЁХ РАЗНЫХ видов, из которых виду `vpc_network`
     * принадлежит ровно одна. Проба подписана на один этот вид (`KIND` выше),
     * значит на одно создание ей приходит один кадр вместо трёх.
     *
     * Ловушка для следующего читателя: число «три» уцелело, а его ПРЕДМЕТ
     * сменился — прежде это были три строки о сети, теперь три строки о трёх
     * разных ресурсах. Совпадение чисел здесь ничего не значит.
     *
     * Свойство «по строке на ресурс» держит
     * `services/vpc/internal/repo/network_create_journal_rows_integration_test.go`
     * (`TestIntegration_NetworkCreate_OneJournalRowPerResource`) — на стороне
     * владельца, а не здесь. У пробы предикат другой и он ниже: предмет, а не число.
     *
     * Перемежаемость давало ДЕЛЕНИЕ ОТВЕТА НА ПОРЦИИ: условие счётчика
     * проверяется между чтениями порции, а разбор внутри порции достаёт все
     * готовые кадры разом. Приедь весь хвост журнала одной порцией — кадров
     * оказывалось семь, предмет находился, проба зеленела. Разойдись порции по
     * границе — чтение прекращалось на четвёртом кадре. Ни то ни другое от
     * ветки не зависит, поэтому красное блуждало по веткам, предмета не
     * касавшимся.
     *
     * ЖДЁМ УСЛОВИЕ, О КОТОРОМ СУДИМ: чтение идёт, пока в собранном не окажется
     * КАЖДЫЙ названный предмет, либо пока не выйдет бюджет. Числа кадров проба
     * больше не знает — и знать не должна: сколько строк журнала рождает одно
     * создание, решает владелец журнала, и завтра он вправе решить иначе.
     *
     * ─────────────────────────────────────────────────────────────────────────
     * РАЗРЫВ — ШТАТНОЕ СОБЫТИЕ, И ЗДЕСЬ ОН ТЕПЕРЬ ОБРАБОТАН
     *
     * Прежнее чтение на конце потока возвращало собранное, и вызывающий читал
     * это как «предмета нет». Но предмет пробы — ПОЗИЦИЯ, а не одно соединение:
     * контракт обещает, что клиент, назвавший свою позицию, получит всё, что
     * после неё. Поэтому конец потока здесь — повод открыть его заново С
     * ПОСЛЕДНЕЙ ПОЛУЧЕННОЙ ПОЗИЦИИ, ровно как делает сам браузер в `EventSource`.
     *
     * Утверждение это НЕ ослабляет: строку, которую край пропустил, повторное
     * открытие не воскрешает — окно чтения у края `(позиция, устоявшееся]`, а
     * позиция берётся МАКСИМАЛЬНАЯ из полученных. Пропущенное остаётся
     * пропущенным, бюджет выходит, проба краснеет.
     *
     * ОТКАЗ КРАЯ НАЗЫВАЕТСЯ ОТДЕЛЬНО от потери предмета: «край не дал потока» и
     * «поток потерял событие» — разные исходы, и подавать первый как второй
     * значит обвинять поток в том, чего он не делал.
     */
    (window as unknown as Record<string, unknown>).__kachoStreamResume = async (
      url: string,
      lastEventId: string,
      wantIds: string[],
      budgetMs: number,
    ): Promise<{ frames: StreamFrame[]; refusal: string }> => {
      const got: StreamFrame[] = [];
      // Позиция, с которой возобновляемся. Растёт по МАКСИМУМУ полученного:
      // возобновиться с меньшей значило бы просить край повторить уже отданное.
      let position = lastEventId;
      let refusal = "";
      const until = Date.now() + budgetMs;
      const collected = () => wantIds.every((id) => got.some((f) => f.data.includes(id)));

      while (!collected() && Date.now() < until) {
        const controller = new AbortController();
        const deadline = setTimeout(() => controller.abort(), Math.max(1, until - Date.now()));
        try {
          const res = await fetch(url, {
            headers: { "Last-Event-ID": position },
            signal: controller.signal,
          });
          if (!res.ok || !res.body) {
            refusal = `край отказал в возобновлении с позиции ${position}: ${res.status}`;
            break;
          }
          const reader = res.body.getReader();
          const decoder = new TextDecoder();
          let buf = "";
          while (!collected() && Date.now() < until) {
            const chunk = await reader.read();
            // Конец потока — не конец чтения: возобновимся с текущей позиции.
            if (chunk.done) break;
            buf += decoder.decode(chunk.value, { stream: true });
            let cut = buf.indexOf("\n\n");
            while (cut >= 0) {
              const frame = buf.slice(0, cut);
              buf = buf.slice(cut + 2);
              // Двоеточие первым знаком — служебный кадр поддержания связи; он
              // не событие и позицию не двигает.
              if (!frame.startsWith(":")) {
                const f: StreamFrame = { id: "", event: "message", data: "" };
                for (const line of frame.split("\n")) {
                  if (line.startsWith("id:")) f.id = line.slice(3).trim();
                  else if (line.startsWith("event:")) f.event = line.slice(6).trim();
                  else if (line.startsWith("data:")) f.data += line.slice(5).trim();
                }
                got.push(f);
                if (f.id !== "") position = f.id;
              }
              cut = buf.indexOf("\n\n");
            }
          }
          reader.cancel().catch(() => undefined);
        } catch {
          // Обрыв по бюджету — законный конец чтения: вернём собранное.
        } finally {
          clearTimeout(deadline);
        }
        if (!collected() && Date.now() < until) {
          // Пауза между ПЕРЕОТКРЫТИЯМИ, а не ожидание предмета временем.
          // Условие выхода остаётся предметным; пауза лишь не даёт молча
          // закрывающемуся краю принять на себя сотни соединений в секунду —
          // ровно то, ради чего браузер и выдерживает свою паузу перед
          // переподключением `EventSource`.
          await new Promise((resolve) => setTimeout(resolve, 250));
        }
      }
      return { frames: got, refusal };
    };
  });
}

/** Кадры, собранные страницей на текущий момент. */
async function frames(page: Page): Promise<Frame[]> {
  return page.evaluate(
    () => ((window as unknown as Record<string, { frames: Frame[] }>).__kachoStream?.frames ?? []) as Frame[],
  );
}

/** Состояние связи глазами страницы. */
interface StreamState {
  readyState: number;
  opened: number;
  errors: number;
  failure: string;
  frames: number;
  events: number;
  lastEventId: string;
  openedAtMs: number;
  lastFrameAtMs: number;
  lastErrorAtMs: number;
  nowMs: number;
}

async function streamState(page: Page): Promise<StreamState> {
  return page.evaluate(
    () => (window as unknown as Record<string, () => StreamState>).__kachoStreamState(),
  );
}

/**
 * Состояние связи СЛОВАМИ — эта строка идёт в отказ пробы.
 *
 * Отказ, называющий одно число полученных событий, не даёт разобрать гонку: он
 * одинаков и когда поток оборвался, и когда он открыт и молчит. Поэтому здесь
 * называется всё, чем эти случаи различаются.
 */
function describeStream(s: StreamState): string {
  const ready =
    { [-1]: "НЕ ОТКРЫВАЛОСЬ", 0: "соединяется", 1: "ОТКРЫТО", 2: "ЗАКРЫТО" }[s.readyState] ??
    `неизвестно (${s.readyState})`;
  const ago = (t: number) => (t === 0 ? "никогда" : `${Math.round((s.nowMs - t) / 1000)} с назад`);
  return [
    `соединение: ${ready}`,
    `открытий потока: ${s.opened}` +
      (s.opened > 1 ? " — край ПЕРЕОТКРЫВАЛ поток, между соединениями было окно" : ""),
    `ошибок связи: ${s.errors}` + (s.errors > 0 ? ` (последняя ${ago(s.lastErrorAtMs)})` : ""),
    s.failure !== "" ? `край: ${s.failure}` : "",
    `кадров получено: ${s.frames}, из них событий: ${s.events}`,
    `последняя позиция: ${s.lastEventId === "" ? "нет" : s.lastEventId}`,
    `поток открылся ${ago(s.openedAtMs)}, последний кадр ${ago(s.lastFrameAtMs)}`,
  ]
    .filter((line) => line !== "")
    .join("; ");
}

/**
 * ЧЕЙ ЭТО ОТКАЗ: строки не было — или строку не довезли.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЭТО НЕ ПОВТОР ПРОБЫ. Вердикт первичного утверждения уже вынесен и НЕ
 * отменяется: что бы здесь ни выяснилось, проба падает. Здесь перечитывается
 * журнал С ПОЗИЦИИ ДО СОЗДАНИЯ — вторым соединением, у которого свой курсор, —
 * чтобы отказ назвал ВИНОВНИКА, а не только число.
 *
 * Разбор ставит один вопрос, на который живой поток ответить не может:
 *
 *   — перечитывание ПРИНОСИТ предмет ⇒ строка в журнале ЕСТЬ и вызывающему
 *     ВИДНА. Значит отправлено, а открытый поток не довёз: предмет в ДОСТАВКЕ;
 *   — перечитывание НЕ приносит ⇒ строки нет либо она вызывающему не видна,
 *     при том что сам ресурс уже читается по своему адресу (это подтвердил
 *     `createdResourceId` до сюда). Предмет у ВЛАДЕЛЬЦА ЖУРНАЛА;
 *   — перечитывание отказано краем ⇒ вердикта нет ВОВСЕ: это условие разбора,
 *     а не его исход, и подавать его как вывод о потоке нельзя.
 *
 * Бюджет здесь короткий намеренно: разбор не вправе съесть срок пробы.
 */
async function replayFrom(
  page: Page,
  url: string,
  position: string,
  wantId: string,
): Promise<{ found: boolean; frames: number; refusal: string }> {
  const out = await page.evaluate(
    async (arg: { url: string; from: string; wantIds: string[] }) =>
      (
        window as unknown as Record<
          string,
          (
            u: string,
            id: string,
            wantIds: string[],
            budget: number,
          ) => Promise<{ frames: Frame[]; refusal: string }>
        >
      ).__kachoStreamResume(arg.url, arg.from, arg.wantIds, 20_000),
    { url, from: position, wantIds: [wantId] },
  );
  return {
    found: out.frames.some((f) => f.data.includes(wantId)),
    frames: out.frames.length,
    refusal: out.refusal,
  };
}

/**
 * Дождаться события о предмете — а не дождавшись, НАЗВАТЬ СОСТОЯНИЕ.
 *
 * Три беды, которые прежний отказ сваливал в «событий 0», здесь разделены и
 * каждая названа своим именем. Предикат снятия задачи #2016 — ровно это: по
 * тексту отказа отличимо «не отправлено» от «не доехало» и от «не дождались».
 */
async function expectStreamEvent(
  page: Page,
  opts: { url: string; created: string; positionBefore: string; timeout: number; subject: string },
): Promise<void> {
  const count = async () =>
    (await frames(page)).filter((f) => f.event === "event" && f.data.includes(opts.created)).length;

  try {
    await expect.poll(count, { timeout: opts.timeout }).toBeGreaterThanOrEqual(1);
    return;
  } catch {
    // Вердикт уже вынесен — дальше только разбор, и он его не отменяет.
  }

  const state = await streamState(page);
  const replay = await replayFrom(page, opts.url, opts.positionBefore, opts.created);
  // Живой поток мог принести предмет ПОКА ШЁЛ РАЗБОР: тогда беда не в продукте,
  // а в отведённом бюджете, и сказать об этом обязано именно сообщение отказа.
  const late = await count();

  let verdict: string;
  if (replay.refusal !== "") {
    verdict =
      `ВИНОВНИК НЕ УСТАНОВЛЕН: перечитать журнал с позиции «${opts.positionBefore}» не удалось — ` +
      `${replay.refusal}. Это УСЛОВИЕ разбора, а не вердикт о потоке: о судьбе строки такой ` +
      `прогон не говорит ничего.`;
  } else if (late > 0) {
    verdict =
      `НЕ ДОЖДАЛИСЬ: поток довёз предмет ПОЗЖЕ отведённых ${Math.round(opts.timeout / 1000)} с — ` +
      `к концу разбора событий о нём ${late}. Предмет в ВЕЛИЧИНЕ БЮДЖЕТА, а не в продукте; ` +
      `бюджет поднимать только ЗАМЕРОМ доставки, а не «на всякий случай».`;
  } else if (replay.found) {
    verdict =
      `НЕ ДОЕХАЛО: перечитывание с позиции «${opts.positionBefore}» предмет ПРИНЕСЛО ` +
      `(кадров ${replay.frames}). Строка в журнале есть и вызывающему видна — значит она ` +
      `отправлена, а ОТКРЫТЫЙ поток её не довёз. Предмет в ДОСТАВКЕ` +
      (state.opened > 1 || state.errors > 0
        ? ": связь рвалась (см. состояние выше), и окно переоткрытия — первое, что надо смотреть."
        : ": связь не рвалась ни разу, поток всё это время был открыт — значит строку " +
          "пропустил сам открытый поток, а не разрыв.");
  } else {
    verdict =
      `НЕ ОТПРАВЛЕНО: перечитывание с позиции «${opts.positionBefore}» предмета НЕ принесло ` +
      `(кадров ${replay.frames}), при том что сам ресурс уже читается по своему адресу. ` +
      `Значит строки журнала нет вовсе либо она вызывающему не видна. Предмет у ВЛАДЕЛЬЦА ` +
      `ЖУРНАЛА (запись строки в той же транзакции, построчное сужение по правам), а не в доставке.`;
  }

  throw new Error(
    `${opts.subject}: страница не получила события о предмете ${opts.created}, созданном другим ` +
      `клиентом, за ${Math.round(opts.timeout / 1000)} с.\n` +
      `  СОСТОЯНИЕ: ${describeStream(state)}\n` +
      `  ВЕРДИКТ: ${verdict}`,
  );
}

/**
 * Создать предмет ОТ ИМЕНИ ДРУГОГО КЛИЕНТА — минуя открытую страницу.
 *
 * Идентификатор возвращается только ПОДТВЕРЖДЁННЫМ. `200` на мутацию означает
 * «операция принята», а не «ресурс есть»: идентификатор чеканится в `metadata`
 * ДО асинхронной части и приезжает в ответе даже тогда, когда та отказала.
 *
 * Цена неподтверждённого здесь — не фантом, а АТРИБУЦИЯ ОТКАЗА, и она ложится
 * ровно на предмет этих проб. Сорвись создание асинхронно — строки в журнале не
 * будет, события не будет, и упадёт ожидание события сообщением «страница не
 * получила события о предмете, созданном другим клиентом»: обвинён ПОТОК,
 * которого дефект не касается, а шаг, предмет не создавший, промолчит. Разбор
 * уйдёт в проекцию края на весь свой срок.
 *
 * Подтверждает `createdResourceId` — чтением ресурса по его собственному адресу.
 * Это сильнее опроса операции: запись операции принадлежит службе-владельцу и
 * говорит о ходе мутации, а чтение говорит о самом предмете — том единственном,
 * ради которого проба сюда пришла.
 */
async function createNetwork(page: Page, projectId: string, name: string): Promise<string> {
  // Супернет НЕ объявляется намеренно: подсети из этой сети никто не режет, а
  // непересекающиеся блоки пришлось бы раздавать вручную каждому предмету —
  // соседние создания в одном проекте спорили бы за CIDR и роняли пробу поводом,
  // к потоку не относящимся.
  const res = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name },
  });
  return createdResourceId(
    page,
    res,
    "networkId",
    (id) => `/vpc/v1/networks/${id}`,
    `создание предмета потока «${name}»`,
  );
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
  await page.goto(`/projects/${projectId}/vpc/networks`);
  const loadsAtStart = loads;

  const url = `${STREAM_PATH}?owner=${OWNER}&projectId=${projectId}&kinds=${KIND}`;
  await page.evaluate(
    (u) => (window as unknown as Record<string, (s: string) => void>).__kachoStreamOpen(u),
    url,
  );

  await expect
    .poll(async () => (await frames(page)).filter((f) => f.event === "opened").length, {
      message:
        `край не открыл поток на ${STREAM_PATH}: служебное сообщение открытия обязано ` +
        `прийти ПЕРВЫМ и ВСЕГДА, в том числе когда событий нет вовсе`,
      timeout: 30_000,
    })
    .toBe(1);

  // Позиция ДО создания: с неё разбор перечитает журнал, если события не будет.
  // Взять её после создания значило бы просить край повторить то, о чём спор.
  const positionBefore = (await frames(page)).at(-1)?.id ?? "";
  const created = await createNetwork(page, projectId, `net-stream-${runTag()}`);

  await expectStreamEvent(page, {
    url,
    created,
    positionBefore,
    timeout: 60_000,
    subject: "проекция потока в браузер",
  });

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
  await page.goto(`/projects/${projectId}/vpc/networks`);

  const tag = runTag();
  const url = `${STREAM_PATH}?owner=${OWNER}&projectId=${projectId}&kinds=${KIND}`;
  await page.evaluate(
    (u) => (window as unknown as Record<string, (s: string) => void>).__kachoStreamOpen(u),
    url,
  );
  await expect
    .poll(async () => (await frames(page)).filter((f) => f.event === "opened").length, {
      message: `край не открыл поток на ${STREAM_PATH}`,
      timeout: 30_000,
    })
    .toBe(1);

  const positionBefore = (await frames(page)).at(-1)?.id ?? "";
  const first = await createNetwork(page, projectId, `net-resume-a-${tag}`);
  // Первый предмет здесь — УСЛОВИЕ пробы (нужна позиция, с которой возобновляться),
  // а не её предмет. Тем важнее, чтобы его отказ называл виновника: иначе разбор
  // уйдёт в возобновление, до которого дело не дошло.
  await expectStreamEvent(page, {
    url,
    created: first,
    positionBefore,
    timeout: 60_000,
    subject: "якорь возобновления (условие пробы, не её предмет)",
  });

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
  const second = await createNetwork(page, projectId, `net-resume-b-${tag}`);
  const third = await createNetwork(page, projectId, `net-resume-c-${tag}`);

  // Читаем ДО ПРЕДМЕТА: названы оба идентификатора, а не число кадров. Сколько
  // строк журнала рождает одно создание — дело владельца журнала (у сети их
  // сегодня три: сама сеть, группа безопасности по умолчанию, таблица
  // маршрутов по умолчанию), и проба об этом числе не судит.
  const resumed = await page.evaluate(
    async (arg: { url: string; from: string; wantIds: string[] }) =>
      (
        window as unknown as Record<
          string,
          (
            u: string,
            id: string,
            wantIds: string[],
            budget: number,
          ) => Promise<{ frames: Frame[]; refusal: string }>
        >
      ).__kachoStreamResume(arg.url, arg.from, arg.wantIds, 60_000),
    { url, from: anchor.id, wantIds: [second, third] },
  );

  // ОТКАЗ КРАЯ — не потеря события. Утверждается ПЕРВЫМ, иначе «потока не
  // дали» подавалось бы как «поток потерял предмет»: обвинён механизм, до
  // которого дело не дошло.
  expect(
    resumed.refusal,
    "край не дал потока на возобновление — это УСЛОВИЕ пробы, а не её предмет: " +
      "о сохранности событий такой прогон не говорит ничего",
  ).toBe("");

  const payload = resumed.frames.map((f) => f.data).join("\n");
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
    resumed.frames.filter((f) => f.event === "event" && f.data.includes(first)).length,
    `возобновление с позиции ${anchor.id} принесло предмет ${first}, который эта позиция ` +
      `ПОКРЫВАЕТ: клиент, ведущий состояние, применил бы его дважды`,
  ).toBe(0);
});
