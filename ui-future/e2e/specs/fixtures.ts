// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, test as base, type BrowserContext, type Page, type Request } from "@playwright/test";
import { isProviderAddressText } from "../../shared/src/test/provider-address";
import { BUDGET_ATTACHMENT, noteRefusal, recordableRefusal, takeRefusals } from "./ceremony-budget";

/**
 * ЗАПИСЬ ТРАССЫ ПРИНАДЛЕЖИТ НАБОРУ, А НЕ ШТАТНОМУ `use.trace` (#1242).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ: ТРАССА ТЕРЯЛАСЬ РОВНО ТОГДА, КОГДА ОНА НУЖНА
 *
 * При `use.trace: "retain-on-failure"` прогонщик собирает файл трассы ОТДЕЛЬНЫМ
 * этапом, ПОСЛЕ пробы и после сноса рабочего процесса, и на этом этапе СЛИВАЕТ
 * два архива: свой (шаги пробы) и браузерный (действия, сеть, снимки страницы).
 * Слияние читает браузерный архив запись за записью — распаковывает каждую и
 * запаковывает заново.
 *
 * На node 26 распаковка достаточно крупной записи НЕ ЗАВЕРШАЕТСЯ. Замер
 * (те же входные архивы, снятые с прогона, та же функция слияния): node 22.23.2
 * — слияние 2217 мс, 895 533 байта; node 26.7.0 — не завершилось за 40 с,
 * записав 491 928 байт. Сужено до чтения одной записи: `trace.network`,
 * 16 626 277 байт, читается целиком на node 22 и останавливается на 15 004 590
 * байтах на node 26. Ни yazl, ни прогонщик в этом опыте не участвуют.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕМ ЭТО ОБХОДИЛОСЬ ДОРОЖЕ, ЧЕМ ВЫГЛЯДИТ
 *
 * У этапа сборки трассы СВОЙ бюджет, и он берётся из ПРОЕКТНОГО `timeout`
 * конфигурации, а не из предела, объявленного самой пробой. Поэтому проба с
 * `test.setTimeout(240_000)` получала «Test timeout of 90000ms exceeded» —
 * предел применялся не тот, что объявлен. Исход: этап обрывался на середине
 * записи, вложение `trace` в отчёт не попадало вовсе, файл оставался БЕЗ
 * центрального каталога (ни один просмотрщик его не открывает), и прогон платил
 * за это 90 секунд. Наблюдалось на двух прогонах подряд: разрывы 91.7 и 92.1 с
 * при собственной длительности упавшей пробы 1.0 и 1.2 с, файлы 242 351 и
 * 250 043 байта, оба обрезаны.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СДЕЛАНО ВМЕСТО
 *
 * Браузерная трасса останавливается ЗДЕСЬ, в разборе `page`, то есть ВНУТРИ
 * пробы — под тем пределом, который проба себе объявила. Сохранение идёт
 * штатным `tracing.stop({ path })`: при локальном браузере он складывает архив
 * из ФАЙЛОВ каталога трасс и ничего не распаковывает, поэтому останавливаться
 * там нечему. Замер на воспроизведении дефекта: разрыв после падения 92.1 с →
 * 1.4 с, файл открывается, 1513 записей.
 *
 * СЛОВО «ЛОКАЛЬНОМ» ЗДЕСЬ НЕСУЩЕЕ, А НЕ ОПИСАТЕЛЬНОЕ (#1288): у удалённого
 * браузера тот же вызов идёт другой полосой — дописыванием в готовый архив, а
 * оно распаковывает каждую его запись, то есть возвращает механизм #1242.
 * Держит это решение `remote-browser-policy.ts`; здесь оно не пересказывается.
 *
 * ЦЕНА НАЗВАНА: в архиве больше нет `test.trace` — дерева шагов самой пробы
 * (утверждения, их исходники). Остаётся то, ради чего трассу и включают:
 * действия в браузере, сеть, консоль, снимки страницы и кадры экрана. Текст
 * отказа и его место по-прежнему в отчёте прогона.
 *
 * ЧЕМ ДЕРЖИТСЯ. (1) Проверка ниже читает ДЕЙСТВУЮЩЕЕ значение `use.trace` из
 * проекта и роняет первую же пробу, если штатную запись включат обратно: два
 * механизма разом вернут слияние, а с ним и потерю. (2) Импорт `test` из
 * `@playwright/test` в пробах запрещён правилом линта — иначе проба тихо
 * останется без этой фикстуры.
 */
export const test = base.extend<{ sourceAxisLedger: void }>({
  // Запись отказов полосы сдаётся В КАЖДОЙ пробе — и в той, что не берёт
  // `page`: её контексты и её посев тратят ту же ось (F8-41). Разбор идёт
  // последним, после разбора страницы, поэтому запись к нему полна.
  sourceAxisLedger: [
    async ({ browserName: _browser }, use, testInfo) => {
      await use();
      const refusals = takeRefusals(testInfo.testId);
      if (refusals.length > 0) {
        await testInfo.attach(BUDGET_ATTACHMENT, {
          body: JSON.stringify({ scenario: testInfo.title, refusals }),
          contentType: "application/json",
        });
      }
    },
    { auto: true },
  ],
  page: async ({ page }, use, testInfo) => {
    const declaredTraceMode = testInfo.project.use.trace;
    const traceMode = typeof declaredTraceMode === "string" ? declaredTraceMode : declaredTraceMode?.mode;
    if (traceMode && traceMode !== "off") {
      throw new Error(
        `use.trace = ${JSON.stringify(traceMode)}, а трассу пишет набор (см. комментарий в specs/fixtures.ts). ` +
          "Две записи разом возвращают слияние архивов прогонщиком — этап обрывается по бюджету, " +
          'файл трассы остаётся без центрального каталога и не открывается. Ожидается use.trace: "off".',
      );
    }

    const tracing = page.context().tracing;
    await tracing.start({ screenshots: true, snapshots: true, sources: true, title: testInfo.title });

    // Запись отказов полосы — вход сторожа бюджета оси источника (F8-41).
    const reading = watchRefusals(page.context(), testInfo.testId);

    await use(page);

    await reading.settled();

    // Держим трассу ровно на том же условии, что и штатный `retain-on-failure`:
    // проба прошла — архив не нужен и не пишется.
    if (testInfo.status === testInfo.expectedStatus) {
      await tracing.stop();
      return;
    }
    const tracePath = testInfo.outputPath("trace.zip");
    await tracing.stop({ path: tracePath });
    testInfo.attachments.push({ name: "trace", path: tracePath, contentType: "application/zip" });
  },
});

/**
 * Записывать отказы `401` полосы формы, полученные страницами контекста, за
 * пробой `testId` (приёмка F8, F8-41). Контекст страницы пробы записывает
 * фикстура; контекст, заведённый пробой самой, пишет она же этим вызовом —
 * иначе его отказы потратили бы ось мимо сторожа.
 */
export function watchRefusals(context: BrowserContext, testId: string) {
  const pending: Array<Promise<void>> = [];
  context.on("response", (res) => {
    if (res.status() !== 401) return;
    const path = new URL(res.url()).pathname;
    if (!path.startsWith("/iam/v1/auth/")) return;
    pending.push(
      res
        .text()
        .catch(() => "")
        .then((text) => {
          const r = recordableRefusal(path, res.status(), text);
          if (r) noteRefusal(testId, r);
        }),
    );
  });
  return { settled: () => Promise.allSettled(pending).then(() => undefined) };
}

/**
 * Общая фикстура проб консоли: завести арендатора и войти под ним.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ КАЖДАЯ ПРОБА ЗАВОДИТ СВОЕГО АРЕНДАТОРА
 *
 * Общий арендатор на все пробы означает, что выдача, сделанная одной, течёт в
 * ожидания другой: проба «этому не видно ничего» зеленеет или краснеет в
 * зависимости от порядка запуска. Своё имя на каждый прогон делает пробы
 * независимыми и позволяет гонять их параллельно, когда это понадобится.
 *
 * ПОЧЕМУ РЕГИСТРАЦИЯ ИДЁТ ЭКРАНОМ КОНСОЛИ (приёмка F8, F8-20)
 *
 * «Дано» набора строится НАШИМ экраном регистрации и НАШИМ глаголом: адрес и
 * пароль одной формой, сессия — печеньем службы. Прежде фикстура вела
 * двухшаговый поток чужого поставщика, и снятие чужого экрана убило бы весь
 * набор разом; теперь набор зависит от того экрана, который консоль и ведёт.
 */
export interface Tenant {
  email: string;
  projectId: string;
}

/**
 * Адрес ЕДИНСТВЕННОЙ проекции потока изменений в браузер.
 *
 * ПОЧЕМУ ЗДЕСЬ, А НЕ В КАЖДОЙ ПРОБЕ (#1481). Адрес — свойство края, а не пробы.
 * Выписанный в трёх спеках, он правился в трёх местах, и пропуск одного не давал
 * ни красного, ни зелёного: спек с прежним адресом получал «путь не резолвится»
 * и объяснял это дефектом продукта. Один источник делает такой пропуск
 * невозможным by construction — сменить адрес мимо проб больше нельзя.
 *
 * ПОЧЕМУ СВОЙ, А НЕ ИМПОРТ ИЗ КОНСОЛИ. Адрес пинится ПОСЛОЙНО, и это уже
 * устройство дерева, а не выбор этой правки: край объявляет его константой
 * `subscriptionstream.Path`, консоль — своей константой в
 * `ui-future/shared/src/lib/subscription/hub.ts`, посадка — правилом nginx и
 * входом ingress (их согласие держит
 * `ui-future/deploy/subscription_stream_serving_test.go`). Слой сквозных
 * проб — ещё один такой пин, и он обязан быть СВОИМ: проба, берущая адрес у
 * проверяемого, перестаёт утверждать о нём что бы то ни было и следует за ним
 * молча.
 *
 * Прогон согласия слоёв при этом остаётся: адрес, разошедшийся с краем, даёт
 * заглушку одностраничного приложения вместо потока, и проба
 * `specs/subscription-stream-reachable.spec.ts` называет это отдельным
 * утверждением.
 */
export const STREAM_PATH = "/subscription/v1/events";

/** Пароль арендатора набора; тот же у посева «Дано» (`ceremony-seed.ts`). */
export const E2E_PASSWORD = "Kacho-E2E-2026!x";

/** Имя носителя сессии нашей службы — то, что браузер держит после входа. */
export const SESSION_COOKIE = "kaname_session";

/** уникальное имя прогона — из времени; коллизия по UNIQUE(name) иначе даёт 409 */
export function runTag(): string {
  return Date.now().toString(36) + Math.trunc(performance.now()).toString(36);
}

/** register проводит регистрацию до РАБОЧЕЙ СЕССИИ и отдаёт почту арендатора.
 *
 * Вынесено из `registerAndSignIn` без изменения его поведения: проект арендатора
 * добывается двумя разными способами (см. `tenantWithProject`), а вход — один и
 * тот же, и второй его копии заводить незачем.
 *
 * Шагов два, и каждый утверждает своё: экран регистрации отрисован консолью ·
 * после отправки у браузера есть носитель сессии. На обоих отказ, показанный
 * экраном, называется ЕГО текстом — а не симптомом «поля нет» либо «печенья нет».
 */
export async function register(page: Page, email = `e2e-${runTag()}@kacho.local`): Promise<string> {
  await page.goto("/registration", { waitUntil: "domcontentloaded" });

  const address = page.getByRole("textbox", { name: "Адрес электронной почты" });
  await advanceOrNameRefusal(page, {
    advanced: () => address.isVisible(),
    where: "на шаге экрана регистрации",
    symptom:
      "экран регистрации консоли не отрисован на /registration, и отказа на экране тоже нет — " +
      "без него арендатор не заводится вовсе",
    tail:
      "Формы нет не потому, что она опоздала, — экран показал отказ. " +
      "Разбирать надо названный отказ, а не отсутствие формы",
  });
  await address.fill(email);
  await page.getByLabel("Пароль", { exact: true }).fill(E2E_PASSWORD);
  const answered = page
    .waitForResponse((r) => new URL(r.url()).pathname === REGISTER_PATH && r.request().method() === "POST", {
      timeout: 30_000,
    })
    .catch(() => null);
  await page.getByRole("button", { name: "Завести учётную запись", exact: true }).click();

  // Сессия обязана быть установлена, иначе всё дальнейшее меряет не то.
  //
  // ИСХОДОВ ЗДЕСЬ ТРИ, А НЕ ДВА (задача #1740): печенье сессии выдано · служба
  // ОТВЕРГЛА регистрацию и экран назвал отказ · ещё не готово. Прежде проба
  // различала два, и отказ читался как «не готово»: она ждала полный срок и
  // сообщала про отсутствующее печенье, тогда как причина стояла НА ТОМ ЖЕ
  // ЭКРАНЕ. Это класс `testing.md` §«Диагноз ставится по ТЕКСТУ отказа, а не по
  // имени упавшего шага».
  await advanceOrNameRefusal(page, {
    advanced: async () => (await page.context().cookies()).some((c) => c.name === SESSION_COOKIE && c.value !== ""),
    where: "на шаге выдачи сессии",
    symptom:
      "печенье сессии не установлено после регистрации, и отказа на экране тоже нет — " +
      "дальше проверялся бы неаутентифицированный доступ под видом аутентифицированного",
    tail:
      "Печенья сессии нет не потому, что оно опоздало, — регистрация отвергнута. " +
      "Разбирать надо названный отказ, а не ожидание печенья",
    answered,
  });

  return email;
}

/** Путь глагола регистрации — ответ на него фикстура читает, когда экран назвал отказ. */
const REGISTER_PATH = "/iam/v1/auth/register";

/**
 * advanceOrNameRefusal — ждёт признак ПРОДВИЖЕНИЯ, а если экран показал отказ,
 * падает ЕГО текстом, а не нашим симптомом.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ИСХОДОВ НА КАЖДОМ ШАГЕ ТРИ, А НЕ ДВА (задача #1740)
 *
 * Шаг оканчивается одним из трёх наблюдаемых состояний: продвинулись · служба
 * ОТВЕРГЛА форму и экран назвал отказ · ещё не готово. Прежде ожидание различало
 * два — «продвинулся» и «нет», — поэтому отказ читался как «не готово»: проба
 * ждала полный срок и сообщала про недостающий признак, тогда как причина
 * стояла НА ТОМ ЖЕ ЭКРАНЕ.
 *
 * ПОЧЕМУ ОЖИДАНИЕ ОДНО НА ОБА ШАГА, А НЕ ДВА ПОХОЖИХ
 *
 * Отказ представим на каждом шаге. Разведи полосы — и одна научится называть
 * причину, а другая нет: обе по отдельности защитимы, неверна их РАЗНИЦА
 * (`architecture.md` §«Параллельные полосы одного механизма обязаны сверяться
 * МЕЖДУ СОБОЙ»). Полоса одна — разойтись им нечем by construction.
 *
 * ПОЧЕМУ ПРИЗНАК ПРОДВИЖЕНИЯ СПРАШИВАЕТСЯ ПЕРВЫМ
 *
 * Распознаватель отказа встаёт на путь КАЖДОГО входа, поэтому его ложное
 * срабатывание остановило бы весь набор. Продвижение решает раньше: экран вправе
 * нести и признак продвижения, и прошлый отказ — и тогда верен первый.
 * Перестановка порядка роняет парные пробы `registration-refusal-named.spec.ts`,
 * и это их предмет.
 */
async function advanceOrNameRefusal(
  page: Page,
  step: {
    advanced: () => Promise<boolean>;
    where: string;
    symptom: string;
    tail: string;
    /** Ответ глагола этого шага, если он был, — из него берётся код отказа. */
    answered?: Promise<{ text(): Promise<string> } | null>;
  },
): Promise<void> {
  let refusal = "";
  await expect
    .poll(
      async () => {
        if (await step.advanced()) return "продвинулись";
        refusal = await identityRefusalOnPage(page);
        return refusal === "" ? "ждём" : "отказ";
      },
      { message: step.symptom, timeout: 30_000 },
    )
    .not.toBe("ждём");

  if (refusal !== "") {
    const response = step.answered ? await step.answered : null;
    const parsed = response ? identityRefusalFromText(await response.text().catch(() => "")) : "";
    throw new Error(
      `регистрация ОТВЕРГНУТА службой ${step.where}: ${refusal}` +
        `${parsed ? ` (ответ глагола: ${parsed})` : ""}. ${step.tail}`,
    );
  }
}

/**
 * identityRefusalFromText — разбор отказа НАШЕЙ службы из ТЕКСТА её ответа.
 *
 * Форма — `google.rpc.Status` `{code, message, details}`, которую полоса формы
 * отдаёт на каждый отказ. Возвращает «<код> · <сообщение>» либо пустую строку,
 * если текст отказом не является.
 *
 * ПОЧЕМУ ПО ТЕКСТУ. Разбор судит величины ответа, а не его вид: код и сообщение
 * — контракт отказа, а разметка вокруг — нет.
 *
 * ПОЧЕМУ ТРЕБУЮТСЯ ОБЕ ВЕЛИЧИНЫ. Код без сообщения не говорит, что случилось;
 * сообщение без кода не говорит, что это отказ службы. Разбор без обеих отказом
 * не признаётся — иначе первая же страница со словом «error» стала бы «отказом».
 */
export function identityRefusalFromText(text: string): string {
  let body: unknown;
  try {
    body = JSON.parse(text);
  } catch {
    return "";
  }
  if (!body || typeof body !== "object") return "";
  const { code, message, details } = body as { code?: unknown; message?: unknown; details?: unknown };
  if (typeof code !== "number" || typeof message !== "string" || !Array.isArray(details)) return "";
  return `${code} · ${message}`;
}

/**
 * identityRefusalOnPage — отказ, СНЯТЫЙ С ЭКРАНА: текст предупреждения, которым
 * экран церемонии называет отказ службы дословно. Пустая строка — отказа на
 * экране нет.
 */
export async function identityRefusalOnPage(page: Page): Promise<string> {
  const alerts = page.getByRole("alert");
  if ((await alerts.count().catch(() => 0)) === 0) return "";
  return (
    await alerts
      .first()
      .innerText({ timeout: 2_000 })
      .catch(() => "")
  ).trim();
}

/** registerAndSignIn проводит регистрацию до рабочей сессии и отдаёт проект арендатора. */
export async function registerAndSignIn(page: Page): Promise<Tenant> {
  const email = await register(page);

  // Проект арендатора заводится сам; без него адресовать модули нечем.
  const projectId = await expect
    .poll(
      async () => {
        const res = await page.request.get("/iam/v1/projects");
        if (!res.ok()) return "";
        const body = (await res.json()) as { projects?: Array<{ id: string }> };
        return body.projects?.[0]?.id ?? "";
      },
      {
        message:
          "проект арендатора не появился: край признаёт личность, но решение о " +
          "правах принять не по чему — так выглядит незаписанная модель прав",
        timeout: 45_000,
      },
    )
    .not.toBe("");

  const res = await page.request.get("/iam/v1/projects");
  const body = (await res.json()) as { projects: Array<{ id: string }> };
  void projectId;
  return { email, projectId: body.projects[0].id };
}

/**
 * tenantWithProject — арендатор со СВОИМ проектом, добытым независимо от того,
 * попала ли его строка на первую страницу списка.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ НЕ ХВАТАЕТ `registerAndSignIn`
 *
 * Тот берёт проект первой строкой `GET /iam/v1/projects`. Список курсорный и
 * фильтруется по правам ПОСТРАНИЧНО: на стенде, где накопились чужие строки,
 * первая страница может целиком состоять из недоступных — тогда ответ выглядит
 * как «проектов нет», хотя свой проект есть и читается по идентификатору.
 * Наблюдалось 2026-08-15 на стенде показа: `{"projects":[],"nextPageToken":"…"}`
 * — пустая страница С ПРОДОЛЖЕНИЕМ, то есть не «пусто», а «не на этой странице».
 *
 * ПОЧЕМУ ЗАПАСНОЙ ПУТЬ НЕ ЯВЛЯЕТСЯ МАСКОЙ
 *
 * Он не подменяет ответ края и ничего не прощает: аккаунт заводится ПУБЛИЧНЫМ
 * API, а его проект признаётся годным только после того, как прочитан по своему
 * адресу (`GET /iam/v1/projects/<id>` → 200). Идентификатор, взятый из метаданных
 * операции и не подтверждённый чтением, был бы фантомом — операция чеканит его
 * ДО того, как асинхронная часть могла отказать.
 */
export async function tenantWithProject(page: Page): Promise<Tenant> {
  const email = await register(page);

  const own = await firstProjectId(page);
  if (own !== "") return { email, projectId: own };

  const res = await page.request.post("/iam/v1/accounts", { data: { name: `acc${runTag()}` } });
  const body = (await res.json()) as { metadata?: { defaultProjectId?: string } };
  expect(
    res.status(),
    `аккаунт не заведён: край ответил ${res.status()} — условие проб не создано, ` +
      `и всё дальнейшее меряло бы не консоль`,
  ).toBe(200);

  const projectId = body.metadata?.defaultProjectId ?? "";
  expect(projectId, "создание аккаунта не назвало проект по умолчанию").not.toBe("");

  await expect
    .poll(async () => (await page.request.get(`/iam/v1/projects/${projectId}`)).status(), {
      message:
        `проект ${projectId} не читается по своему адресу: идентификатор из метаданных ` +
        `операции без подтверждения чтением — фантом, и пробы шли бы по несуществующему ресурсу`,
      timeout: 60_000,
    })
    .toBe(200);

  return { email, projectId };
}

async function firstProjectId(page: Page): Promise<string> {
  for (let i = 0; i < 8; i++) {
    const res = await page.request.get("/iam/v1/projects");
    if (res.ok()) {
      const body = (await res.json()) as { projects?: Array<{ id: string }> };
      const id = body.projects?.[0]?.id ?? "";
      if (id) return id;
    }
    await new Promise((r) => setTimeout(r, 2_000));
  }
  return "";
}

/**
 * createdResourceId доводит асинхронную мутацию до ПРОВЕРЕННОГО ресурса.
 *
 * Идентификатор из `metadata` сам по себе ничего не доказывает: он выделяется до
 * асинхронной части и остаётся в ответе даже тогда, когда та отказала. Поэтому
 * здесь он подтверждается чтением ресурса по его собственному адресу — это
 * сильнее проверки `op.error` и не зависит от того, какой службе принадлежит
 * запись операции.
 *
 * ОТВЕТ ПРИНИМАЕТСЯ ПО ФОРМЕ, А НЕ ПО ПРОИСХОЖДЕНИЮ. Мутацию рождают два разных
 * пути пробы, и типы ответов у них РАЗНЫЕ: запрос, отправленный самой пробой
 * (`page.request.post` → `APIResponse`), и ответ, ПЕРЕХВАЧЕННЫЙ у страницы,
 * когда мутацию отправила форма (`page.waitForResponse` → `Response`). Помощнику
 * нужны от обоих ровно два метода, поэтому он их и требует. Привязка к одному из
 * двух классов заставила бы завести вторую копию этой проверки для форм — и
 * вторая копия разошлась бы с первой молча, как это уже случалось с помощниками
 * адресации.
 */
export async function createdResourceId(
  page: Page,
  response: { status(): number; text(): Promise<string> },
  metadataField: string,
  addressOf: (id: string) => string,
  subject: string,
): Promise<string> {
  const text = await response.text();
  expect(
    response.status(),
    `${subject}: край отверг создание — ${response.status()} ${text.slice(0, 300)}. ` +
      `Это УСЛОВИЕ пробы, а не её предмет: вердикта о консоли такой прогон не даёт`,
  ).toBe(200);

  const body = JSON.parse(text) as { metadata?: Record<string, string> };
  const id = body.metadata?.[metadataField] ?? "";
  expect(id, `${subject}: операция не назвала ${metadataField}`).not.toBe("");

  await expect
    .poll(async () => (await page.request.get(addressOf(id))).status(), {
      message: `${subject}: ресурс ${id} не читается по своему адресу — операция вернула фантом`,
      timeout: 60_000,
    })
    .toBe(200);

  return id;
}

/**
 * Перепись обращений консоли — прибор сценариев церемонии (приёмка F8, Р6).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕМ ОНА ОТЛИЧАЕТСЯ ОТ `apiCalls` НИЖЕ, И ПОЧЕМУ ТОТ ЗДЕСЬ НЕ ГОДИТСЯ
 *
 * `apiCalls` пишет ОТВЕТЫ и только те, в чьём пути есть `/v1/`. У поставщика
 * личности `/v1/` нет ни в одном адресе, поэтому отрицание «ни одного адреса
 * поставщика» на нём тождественно истинно: оно не краснеет ни на переходе на его
 * поток выхода, ни на запросе к нему, ни на чтении его сессии. Для отрицания
 * такой прибор непригоден, а два прибора на одно наблюдение не заводятся.
 *
 * ЧТО ПИШЕТСЯ. Каждое обращение, которое ВЫПУСТИЛИ страницы контекста браузера,
 * — в момент выпуска, без фильтра по пути: метод, происхождение, путь, строка
 * запроса, вид (переход документа либо запрос страницы) и исход (код ответа
 * либо «ответа нет»). Слушатель стоит на КОНТЕКСТЕ, а не на странице: окна,
 * открытого страницей, слушатель страницы не видит, а шаг перенаправления и
 * такое окно — тоже обращения консоли.
 *
 * ЧЕГО ОНА НЕ ВИДИТ — названо, чтобы «ноль» не читался шире сказанного.
 * Обращения КОНТЕКСТА ЗАПРОСОВ (`page.request`, `context.request`,
 * `request.newContext()`) событий страниц не порождают: посев «Дано», фикстура
 * и оснастка ходят именно им, и отрицание о поставщике по ним держит не эта
 * перепись, а статическая перепись набора. Значка вкладки браузер за собой
 * тоже не записывает.
 */
export interface CeremonyCall {
  method: string;
  origin: string;
  path: string;
  /** Строка запроса с ведущим `?` либо пустая. */
  query: string;
  kind: "документ" | "запрос";
  /** Код ответа; «ответа нет» — обращение не получило ответа вовсе; «ждём» — ещё идёт. */
  outcome: number | "ответа нет" | "ждём";
}

/**
 * Адрес поставщика личности распознаётся ОДНИМ распознавателем на обе переписи
 * приёмки F8 — эту и статическую (`shared/src/test/provider-address.ts`): все
 * формы, в которых дерево консоли этот адрес выпускало (Р6 п. 2).
 */
export { isProviderAddressText };

export interface CeremonyCensus {
  readonly calls: readonly CeremonyCall[];
  /** Обращения по методу, пути и (необязательно) строке запроса — в порядке выпуска. */
  matching(method: string, path: string, query?: string): CeremonyCall[];
  /** Обращения, распознанные как адрес поставщика личности. */
  providerCalls(): CeremonyCall[];
  /** Вся перепись строками — для текста падения. */
  describe(): string;
}

export function formatCall(c: CeremonyCall): string {
  return `${c.method} ${c.origin}${c.path}${c.query} [${c.kind} → ${c.outcome}]`;
}

export function ceremonyCensus(context: BrowserContext): CeremonyCensus {
  const calls: CeremonyCall[] = [];
  const byRequest = new Map<Request, CeremonyCall>();
  context.on("request", (r) => {
    let u: URL;
    try {
      u = new URL(r.url());
    } catch {
      return;
    }
    const call: CeremonyCall = {
      method: r.method(),
      origin: u.origin,
      path: u.pathname,
      query: u.search,
      kind: r.isNavigationRequest() ? "документ" : "запрос",
      outcome: "ждём",
    };
    calls.push(call);
    byRequest.set(r, call);
  });
  context.on("response", (res) => {
    const call = byRequest.get(res.request());
    if (call) call.outcome = res.status();
  });
  context.on("requestfailed", (r) => {
    const call = byRequest.get(r);
    if (call) call.outcome = "ответа нет";
  });
  return {
    calls,
    matching: (method, path, query) =>
      calls.filter((c) => c.method === method && c.path === path && (query === undefined || c.query === query)),
    providerCalls: () => calls.filter((c) => isProviderAddressText(c.path)),
    describe: () => (calls.length === 0 ? "  (обращений нет)" : calls.map((c) => `  ${formatCall(c)}`).join("\n")),
  };
}

/** apiCalls собирает коды ответов API, которые страница сделала сама. */
export function apiCalls(page: Page): string[] {
  const seen: string[] = [];
  page.on("response", (r) => {
    const u = new URL(r.url()).pathname;
    if (/\/v1\//.test(u)) seen.push(`${r.status()} ${u}`);
  });
  return seen;
}

/**
 * Что продукт СКАЗАЛ о копировании — вместо чтения системного буфера.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ НЕ БУФЕР И НЕ ПЕРЕХВАТ `navigator.clipboard`
 *
 * Ни того, ни другого в конвейере не существует. Консоль там поднята как
 * `http://console.kacho.local:<порт>` — это НЕ защищённый контекст, и
 * `navigator.clipboard` равен `undefined`: читать нечего и подменять нечего.
 * Локально стенд стоит на `localhost`, который защищённым контекстом считается,
 * поэтому обе прежние редакции пробы зеленели у меня и краснели там.
 *
 * Наблюдаемое, которое есть в ОБЕИХ посадках, — подпись, которую продукт
 * показывает после копирования. Она несёт саму строку («Скопировано: env=prod»),
 * то есть отвечает на тот же вопрос: КАКОЕ значение ушло в буфер.
 *
 * ЭТО НЕ ОСЛАБЛЕНИЕ. Подпись успеха продукт показывает ТОЛЬКО когда копирование
 * действительно состоялось: помощник (`@shared/lib/clipboard`) возвращает исход,
 * и на отказе рисуется другая подпись. Проба, ждущая успеха, краснеет и когда
 * буфер недоступен, и когда ушло не то значение.
 */
export function copyToast(page: Page, value: string) {
  return page.getByText(`Скопировано: ${value}`).first();
}

/**
 * Дождаться, что КАРКАС знает область арендатора.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ ОТДЕЛЬНОЕ ОЖИДАНИЕ, ЕСЛИ ПРОЕКТ УЖЕ СОЗДАН
 *
 * Проект, полученный от края, и область, которую показывает каркас, — разные
 * вещи. Каркас читает доступные арендатору области через права, а они
 * материализуются в ограниченном окне. Пока их нет, модули объявлены
 * недоступными и содержимого на экране нет ВООБЩЕ: заход по прямому адресу
 * оставляет человека на сводке.
 *
 * Проба, не дождавшаяся этого, падает НИЖЕ по потоку — на утверждении о
 * содержимом страницы, — и называет невиновного: «нет такого факта» вместо
 * «страница не открылась».
 *
 * Ждать этого ДО создания ресурсов выгоднее вдвойне: время ожидания тратится
 * на то, что всё равно должно произойти, а к моменту перехода область уже есть.
 */
export async function scopeIsReady(page: Page, projectId: string): Promise<void> {
  await page.goto(`/projects/${projectId}/dashboard`, { waitUntil: "domcontentloaded" });
  await expect
    .poll(
      async () => {
        const button = page.getByRole("button", { name: "Virtual Private Cloud" }).first();
        if ((await button.count()) === 0) return false;
        return await button.isEnabled();
      },
      {
        message:
          `каркас не показал область проекта ${projectId}: модули остаются недоступными, ` +
          `и любая страница под этим адресом откроется пустой. Права арендатора ` +
          `материализуются в ограниченном окне — здесь оно не закрылось`,
        timeout: 120_000,
      },
    )
    .toBe(true);
}
