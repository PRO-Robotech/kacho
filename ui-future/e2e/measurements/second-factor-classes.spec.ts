// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type TestInfo } from "@playwright/test";
import { noteRefusal, recordableRefusal } from "../specs/ceremony-budget";
import {
  LANE,
  SEED_PASSWORD,
  backupCodeOutside,
  codeOutsideWindow,
  newSeed,
  seedAddress,
  seedConfirmedHuman,
  seedSecondFactor,
  totpCode,
  type FormKind,
  type Seed,
} from "../specs/ceremony-seed";
import { test } from "../specs/fixtures";
import {
  AUTHENTICATION_FAILED,
  LOGIN_WITH_SECOND_FACTOR_FAILED,
} from "../specs/lane-texts";
import { conditionNotCreated } from "../specs/mail-receiver";
import { signedIn } from "../specs/session-lane";

/**
 * Ф12-33 — отказы неразличимы по времени, и у личности без фактора тоже.
 * ИЗМЕРИТЕЛЬНАЯ проба, форма Ф1-48, через край (kacho#1281).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СУДИТСЯ
 *
 *   Ф12 — `docs/engineering/acceptance/second-factor-totp-and-recovery-codes.md`
 *         (PRO-Robotech/kaname, ветка 296 @ 937cdade5)
 *         c6ea48973b048e0430bf3712e317aca39ac7df2e77dcb5e8d71e65c59aa33e2f — APPROVED.
 *
 * Одиннадцать классов отказа (Ф12-33 «Дано»), `N` обращений на класс, классы
 * чередуются по кругу (сдвиг порядка на каждом круге — дрейф стенда не ложится
 * на один класс). Критерий Ф1-48 на каждой паре: |медиана₁ − медиана₂| ≤
 * max(IQR₁, IQR₂). Пары — ровно названные «Тогда»: (1)(2), (1)(3), (2)(3);
 * (5)(6); (2)(7), (2)(8), (7)(8); (9)(10); (7)(11). Пары МЕЖДУ способами не
 * сравниваются — способ называет клиент.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ НЕ В `specs/` — СВОЙ ПРОЕКТ И СВОЙ БЮДЖЕТ (решение диспетчера волны 3101)
 *
 * Замер тратит `11 × N` отказов `401` на путях оси источника (F8-41). Набор
 * `specs/` исполняет конвейер одним прогоном со сторожем бюджета этой оси
 * (`scripts/ceremony-budget.ts`, величина — `console-e2e.yml`), и проба, лёгшая
 * туда, роняла бы сторожа на каждом прогоне ПО ПОСТРОЕНИЮ; прятать её отказы от
 * записи было бы маской. Поэтому каталог `measurements/` объявлен в
 * `playwright.config.ts` своим проектом, который существует только при ручке
 * `KACHO_CONSOLE_MEASURE=1`, и прогон пишет СВОЙ отчёт. Отказы проба записывает
 * тем же правилом счёта (`recordableRefusal`), и тот же сторож судит её прогон
 * своей величиной — числом отказов, которое замер объявляет (`MEASURED_REFUSALS`):
 *
 *   KACHO_CONSOLE_MEASURE=1 npx playwright test
 *   KACHO_CEREMONY_FAILURE_BUDGET=132 node scripts/ceremony-budget.ts results-measure.json
 *
 * Стенд — пространство стенда проб (`make -C deploy stand-ns-run NS=t<задача>-…`):
 * окно источника там 400 за 15m (`values.dev.yaml`), замер укладывается в него с
 * посевом; на общем ранере соседей нет, но размах там выше потолка годности.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЛИЧНОСТЕЙ МНОГО, А ОТКАЗОВ ПО ЧАСТОТЕ — НИ ОДНОГО (Ф12-33 «Когда»)
 *
 * Предел по адресу — `addressAttempts` за `addressWindow` (5 за 15m на стенде).
 * Счёт по адресу обнуляет только вход, доведённый кодом до всех факторов
 * (Ф12-46 «б»), поэтому у личностей с фактором каждый круг начинается УСПЕХОМ
 * кода своего шага, и отказов после него на круге не больше трёх. Личности без
 * фактора (`B`) и со строкой `pending` (`C`) успеха кода не имеют: `B` — своя на
 * каждый круг (три отказа), `C` — одна на четыре круга (четыре отказа). Отказ по
 * частоте здесь — красное с именем класса, а не «шум»: либо счёт не обнуляется
 * успехом кода, либо предел ниже объявленного.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ТРИ ИСХОДА
 *
 * Зелёный — критерий выполнен на всех парах. Красный — хотя бы одна пара
 * различима, она названа медианами и размахами. «Не выполнилось» — размах
 * любого класса выше потолка годности (`IQR_CEILING_MS`): стенд шумит сильнее
 * различимости, которую меряют (Ф1-50), и вердикта нет ни в одну сторону.
 */

/** Обращений на класс; нижняя граница Ф1-48 — 10. */
const N = 12;
/** Классов отказа (Ф12-33 «Дано»). */
const CLASSES = 11;
/** Отказов, которые замер тратит на оси источника: это и есть величина его бюджета. */
const MEASURED_REFUSALS = N * CLASSES;
/** Потолок годности замера (Ф1-50): размах выше — стенд шумит сильнее измеряемого. */
const IQR_CEILING_MS = 250;
/** Кругов на одну личность `C`: 4 отказа ниже предела 5 по адресу. */
const C_ROUNDS = 4;

const WRONG_PASSWORD = `${SEED_PASSWORD}-not-it`;

type ClassId =
  "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" | "10" | "11";

/** Пары критерия — ровно названные «Тогда» Ф12-33. */
const PAIRS: ReadonlyArray<readonly [ClassId, ClassId]> = [
  ["1", "2"],
  ["1", "3"],
  ["2", "3"],
  ["5", "6"],
  ["2", "7"],
  ["2", "8"],
  ["7", "8"],
  ["9", "10"],
  ["7", "11"],
];

/** Текст отказа класса: вход без поля — свой текст (Ф3-02), с полем — один на все причины. */
const EXPECTED_TEXT: Partial<Record<ClassId, string>> = {
  "1": AUTHENTICATION_FAILED,
  "2": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "3": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "4": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "7": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "8": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "9": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "10": LOGIN_WITH_SECOND_FACTOR_FAILED,
  "11": LOGIN_WITH_SECOND_FACTOR_FAILED,
};

interface Sample {
  ms: number;
  status: number;
  code: unknown;
  message: unknown;
}

/**
 * Одно обращение: признак формы берётся ДО отсчёта, мерится только POST глагола.
 * Отказ идёт в запись сторожа бюджета тем же правилом, что у посева.
 */
async function timed(
  seed: Seed,
  path: string,
  kind: FormKind,
  body: Record<string, unknown>,
): Promise<Sample> {
  const csrfToken = await seed.formToken(kind);
  const t0 = performance.now();
  const res = await seed.api.post(path, { data: { ...body, csrfToken } });
  const text = await res.text();
  const ms = performance.now() - t0;
  const refused = recordableRefusal(path, res.status(), text);
  if (refused) noteRefusal(test.info().testId, refused);
  let parsed: { code?: unknown; message?: unknown } = {};
  try {
    parsed = JSON.parse(text) as typeof parsed;
  } catch {
    // Тело не JSON — утверждение ниже назовёт его статусом.
  }
  return {
    ms,
    status: res.status(),
    code: parsed.code,
    message: parsed.message,
  };
}

function stats(xs: readonly number[]): { median: number; iqr: number } {
  const s = [...xs].sort((a, b) => a - b);
  const q = (p: number) => s[Math.floor((s.length - 1) * p)];
  return { median: q(0.5), iqr: q(0.75) - q(0.25) };
}

const stepOf = (ms: number) => Math.floor(ms / 30_000);

/** Дождаться шага кода новее `after`: успех кода своего шага возможен один раз. */
async function nextStepAfter(after: number): Promise<number> {
  await expect
    .poll(() => stepOf(Date.now()), {
      message: "шаг кода не сменился",
      timeout: 45_000,
      intervals: [500],
    })
    .toBeGreaterThan(after);
  return stepOf(Date.now());
}

interface Human {
  email: string;
  password: string;
  secret: string;
  backupCodes: string[];
}

async function seedPerson(
  testInfo: TestInfo,
  seeds: Seed[],
  tag: string,
  factor: "active" | "pending" | "none",
): Promise<Human> {
  const seed = await newSeed(testInfo);
  seeds.push(seed);
  const human = await seedConfirmedHuman(seed, seedAddress(`f12-33-${tag}`));
  if (factor === "active") {
    const f = await seedSecondFactor(seed);
    return {
      email: human.email,
      password: human.password,
      secret: f.secret,
      backupCodes: f.backupCodes,
    };
  }
  if (factor === "pending") {
    // Строка `pending` (Ф12-13): заведение без подтверждения.
    const res = await seed.submit(LANE.enroll, "second-factor", {});
    expect(
      res.status(),
      `посев C: заведение фактора без подтверждения отвергнуто — ${res.status()}`,
    ).toBe(200);
  }
  return {
    email: human.email,
    password: human.password,
    secret: "",
    backupCodes: [],
  };
}

test("Ф12-33 · одиннадцать классов отказа неразличимы по времени (критерий Ф1-48)", async ({
  browserName: _b,
}, testInfo) => {
  // verifies #1281 — Ф12-33 (Ф12 c6ea4897…), измерительная, форма Ф1-48.
  test.setTimeout(40 * 60_000);
  const seeds: Seed[] = [];
  try {
    // Дано: A1, A2 — с фактором (успех кода на каждом круге обнуляет счёт по
    // адресу); D — с фактором, глагол церемонии под сессией; B — без фактора,
    // своя на круг; C — со строкой `pending`, одна на C_ROUNDS кругов.
    const a1 = await seedPerson(testInfo, seeds, "a1", "active");
    const a2 = await seedPerson(testInfo, seeds, "a2", "active");
    const d = await seedPerson(testInfo, seeds, "d", "active");
    const bs: Human[] = [];
    for (let r = 0; r < N; r++)
      bs.push(await seedPerson(testInfo, seeds, `b${r}`, "none"));
    const cs: Human[] = [];
    for (let i = 0; i < Math.ceil(N / C_ROUNDS); i++)
      cs.push(await seedPerson(testInfo, seeds, `c${i}`, "pending"));

    // Дверь входа: контекст без сессии, общий всем отказам входа — форма
    // запроса одна, отличие пары — один факт (личность либо поле).
    const door = await newSeed(testInfo);
    seeds.push(door);

    const samples = new Map<ClassId, Sample[]>();
    const add = (c: ClassId, s: Sample) =>
      samples.set(c, [...(samples.get(c) ?? []), s]);
    const login = (body: Record<string, unknown>) =>
      timed(door, LANE.login, "login", body);

    const started = performance.now();
    let step = stepOf(Date.now());
    for (let round = 0; round < N; round++) {
      step = await nextStepAfter(step);
      const at = Date.now();
      // Успехи круга — до замеров: обнуляют счёт по адресу и кладут шаг,
      // который (4) и (6) затем повторяют.
      const c4code = totpCode(a1.secret, at);
      for (const [who, code] of [
        [a1, c4code],
        [a2, totpCode(a2.secret, at)],
      ] as const) {
        // Свой контекст на каждый успех: дверь отказов остаётся без сессии.
        const winner = await newSeed(testInfo);
        seeds.push(winner);
        const ok = await timed(winner, LANE.login, "login", {
          email: who.email,
          password: who.password,
          secondFactor: { method: "totp", code },
        });
        expect(
          ok.status,
          `круг ${round + 1}: вход ${who.email} кодом своего шага — условие замера`,
        ).toBe(200);
      }
      const dSession = await signedIn(
        testInfo,
        d.email,
        d.password,
        `D круг ${round + 1}`,
      );
      seeds.push(dSession);
      const c6code = totpCode(d.secret, at);
      const raised = await timed(dSession, LANE.stepUp, "step-up", {
        method: "totp",
        code: c6code,
      });
      expect(
        raised.status,
        `круг ${round + 1}: повышение D кодом своего шага — условие замера`,
      ).toBe(200);

      const wrongCode = codeOutsideWindow(a1.secret, at);
      const wrongBackup = backupCodeOutside(a1.backupCodes);
      const b = bs[round];
      const c = cs[Math.floor(round / C_ROUNDS)];
      const measured: Array<[ClassId, () => Promise<Sample>]> = [
        ["1", () => login({ email: a2.email, password: WRONG_PASSWORD })],
        [
          "2",
          () =>
            login({
              email: a2.email,
              password: WRONG_PASSWORD,
              secondFactor: { method: "totp", code: wrongCode },
            }),
        ],
        [
          "3",
          () =>
            login({
              email: a1.email,
              password: a1.password,
              secondFactor: { method: "totp", code: wrongCode },
            }),
        ],
        [
          "4",
          () =>
            login({
              email: a1.email,
              password: a1.password,
              secondFactor: { method: "totp", code: c4code },
            }),
        ],
        [
          "5",
          () =>
            timed(dSession, LANE.stepUp, "step-up", {
              method: "totp",
              code: codeOutsideWindow(d.secret, at),
            }),
        ],
        [
          "6",
          () =>
            timed(dSession, LANE.stepUp, "step-up", {
              method: "totp",
              code: c6code,
            }),
        ],
        [
          "7",
          () =>
            login({
              email: b.email,
              password: WRONG_PASSWORD,
              secondFactor: { method: "totp", code: wrongCode },
            }),
        ],
        [
          "8",
          () =>
            login({
              email: c.email,
              password: WRONG_PASSWORD,
              secondFactor: { method: "totp", code: wrongCode },
            }),
        ],
        [
          "9",
          () =>
            login({
              email: a1.email,
              password: WRONG_PASSWORD,
              secondFactor: { method: "lookup_secret", code: wrongBackup },
            }),
        ],
        [
          "10",
          () =>
            login({
              email: b.email,
              password: WRONG_PASSWORD,
              secondFactor: { method: "lookup_secret", code: wrongBackup },
            }),
        ],
        [
          "11",
          () =>
            login({
              email: b.email,
              password: b.password,
              secondFactor: { method: "totp", code: wrongCode },
            }),
        ],
      ];
      // Чередование: порядок сдвигается на каждом круге.
      for (let i = 0; i < measured.length; i++) {
        const [cls, call] = measured[(i + round) % measured.length];
        const s = await call();
        expect(
          s.status,
          `класс (${cls}), круг ${round + 1}: отказ по частоте — Ф12-33 требует ни одного (${JSON.stringify(s)})`,
        ).not.toBe(429);
        expect(
          { status: s.status, code: s.code },
          `класс (${cls}), круг ${round + 1}: отказ — 401, code 16`,
        ).toEqual({ status: 401, code: 16 });
        const text = EXPECTED_TEXT[cls];
        if (text)
          expect(
            s.message,
            `класс (${cls}), круг ${round + 1}: текст отказа`,
          ).toBe(text);
        add(cls, s);
      }
    }
    const tookMs = performance.now() - started;

    const table = new Map<ClassId, { median: number; iqr: number }>();
    for (const [cls, ss] of samples) table.set(cls, stats(ss.map((s) => s.ms)));
    // (5) и (6) — один глагол и одна форма: тексты равны между собой.
    const ceremonyTexts = new Set(
      [...(samples.get("5") ?? []), ...(samples.get("6") ?? [])].map((s) =>
        String(s.message),
      ),
    );
    console.log(
      `[Ф12-33] N=${N} на класс · классов ${table.size} · T=${(tookMs / 1000).toFixed(1)} с · отказов ${MEASURED_REFUSALS}\n` +
        [...table.entries()]
          .sort((x, y) => Number(x[0]) - Number(y[0]))
          .map(
            ([c, s]) =>
              `  (${c}) медиана ${s.median.toFixed(1)} мс · размах ${s.iqr.toFixed(1)} мс`,
          )
          .join("\n"),
    );
    expect(
      ceremonyTexts.size,
      `(5) и (6) — один текст отказа: ${[...ceremonyTexts].join(" | ")}`,
    ).toBe(1);

    const noisy = [...table.entries()].filter(
      ([, s]) => s.iqr > IQR_CEILING_MS,
    );
    if (noisy.length) {
      conditionNotCreated(
        `не выполнилось (Ф1-50): размах выше потолка годности ${IQR_CEILING_MS} мс у ` +
          noisy.map(([c, s]) => `(${c}) ${s.iqr.toFixed(1)} мс`).join(", "),
      );
    }

    const verdict = PAIRS.map(([x, y]) => {
      const a = table.get(x)!;
      const b = table.get(y)!;
      const diff = Math.abs(a.median - b.median);
      const bound = Math.max(a.iqr, b.iqr);
      return {
        pair: `(${x})/(${y})`,
        diff: Number(diff.toFixed(1)),
        bound: Number(bound.toFixed(1)),
        ok: diff <= bound,
      };
    });
    console.log(
      `[Ф12-33] критерий Ф1-48 по парам:\n${verdict.map((v) => `  ${v.pair} |Δмедиан| ${v.diff} ≤ ${v.bound} — ${v.ok ? "да" : "НЕТ"}`).join("\n")}`,
    );
    expect(
      verdict.filter((v) => !v.ok),
      "пары, различимые по времени (Ф12-33 «Тогда»)",
    ).toEqual([]);
  } finally {
    // Снятие — и на падении: контексты запросов закрываются.
    await Promise.all(seeds.map((s) => s.dispose()));
  }
});
