// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import path from "node:path";
import { expect } from "@playwright/test";
import { registerAndSignIn, test } from "./fixtures";

/**
 * Внутреннего пространства путей на публичном входе НЕТ — даже для того, у кого
 * действующая сессия (kacho#3053, запрет ban06).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРИЗНАК
 *
 * Через публичный вход консоли запрос во внутреннее пространство путей получал
 * отказ аутентификации — `401`, а не отсутствие маршрута. Такой ответ не
 * различает «маршрута на внешнем входе нет» и «маршрут есть, закрыт
 * аутентификацией»; вызывающий с сессией получал `403`, подробности которого
 * называли внутренний метод. Запрет «внутреннее — только на внутреннем порту»
 * снаружи стенда не доказывался ничем.
 *
 * ЧТО УТВЕРЖДАЕТСЯ
 *
 * Для каждой внутренней пары (метод, шаблон) из таблицы маршрутов дерева —
 * её производит экстрактор из аннотаций `google.api.http`, перечень здесь не
 * выписан — ответ публичного входа с действующей сессией побайтно равен ответу
 * на то, чего нет:
 *   - путь, которого нет ни у одного публичного шаблона, — как путь, которого
 *     нет вовсе (`404`, NOT_FOUND);
 *   - путь, публичный под другим методом, — как метод, которого нет ни у одного
 *     биндинга, на том же пути (`501`).
 * Тело ответа не называет внутренний метод.
 *
 * Исключаются ВЫВОДОМ (число печатается): пары, которые публичный шаблон под тем
 * же методом сопоставляет сам, — там маршрут есть и он публичный.
 *
 * Близнец «тот же запрос на внутреннем порту — не 404» из браузера недостижим по
 * построению: его держат пробы края на уровне кода
 * (gateway/internal/restmux/external_route_gate*_test.go), а на кластере —
 * прогон изнутри (kacho#3066). Методы без REST-правила в таблицу маршрутов не
 * попадают; их держат те же пробы края.
 */

interface Route {
  method: string;
  template: string;
  fqn: string;
}

const ROUTE_TABLE = "gateway/internal/middleware/rest_route_table_gen.go";
const ROW = /\{Method: "([A-Z]+)", Template: "([^"]+)", FQN: "([^"]+)"\}/g;
const UNSERVED_METHOD = "TRACE";
// Близнец пути, публичного под другим методом, — метод, которого на этом пути нет
// ни у одного шаблона. TRACE для этого не годится на стенде: раздача консоли
// отвечает на него сама, не доводя до края.
const TWIN_METHODS = ["PUT", "PATCH", "DELETE", "POST", "GET"];
const ABSENT_RESOURCE = "kachoProbeAbsentResource";

function readRoutes(repoRoot: string): { routes: Route[]; rowLines: number } {
  const text = readFileSync(path.join(repoRoot, ROUTE_TABLE), "utf8");
  const routes = [...text.matchAll(ROW)].map((m) => ({ method: m[1], template: m[2], fqn: m[3] }));
  // Второе выражение того же предмета: строки таблицы по их началу. Разошлись —
  // разборщик не знает формы записи, и его число не о дереве.
  const rowLines = text.split("\n").filter((l) => l.trimStart().startsWith("{Method:")).length;
  return { routes, rowLines };
}

function isInternalService(fqn: string): boolean {
  const svc = fqn.slice(0, fqn.indexOf("/")).split(".").pop() ?? "";
  return svc.endsWith("InternalService") || (svc.startsWith("Internal") && svc.endsWith("Service"));
}

interface Seg {
  prefix: string;
  suffix: string;
  wild: boolean;
  rest: boolean;
}

function segments(template: string): Seg[] {
  return template
    .replace(/^\//, "")
    .split("/")
    .map((s) => {
      const open = s.indexOf("{");
      const close = s.lastIndexOf("}");
      if (open < 0 || close < open) return { prefix: s, suffix: "", wild: false, rest: false };
      return { prefix: s.slice(0, open), suffix: s.slice(close + 1), wild: true, rest: s.slice(open, close).includes("**") };
    });
}

function segMatches(s: Seg, actual: string): boolean {
  if (!s.wild) return s.prefix === actual;
  return actual.length > s.prefix.length + s.suffix.length && actual.startsWith(s.prefix) && actual.endsWith(s.suffix);
}

/** Сопоставление конкретного пути шаблону — та же семантика, что у таблицы края. */
function matches(template: string, concrete: string): boolean {
  const tmpl = segments(template);
  const parts = concrete.replace(/^\//, "").split("/");
  for (let i = 0; i < tmpl.length; i++) {
    const t = tmpl[i];
    if (t.rest) {
      const tail = tmpl.slice(i + 1);
      const remaining = parts.slice(i);
      if (remaining.length < tail.length + 1) return false;
      const covered = remaining.slice(0, remaining.length - tail.length);
      if (!tail.every((ts, k) => !ts.rest && segMatches(ts, remaining[covered.length + k]))) return false;
      const joined = covered.join("/");
      return joined.length > t.prefix.length + t.suffix.length && joined.startsWith(t.prefix) && joined.endsWith(t.suffix);
    }
    if (i >= parts.length || !segMatches(t, parts[i])) return false;
  }
  return parts.length === tmpl.length;
}

function concretePath(template: string): string {
  return template.replace(/\{[^}]*\}/g, "probeid");
}

interface Shot {
  status: number;
  contentType: string;
  body: string;
}

test("внутреннее пространство путей на публичном входе отвечает «маршрута нет» и с действующей сессией", async ({
  page,
}) => {
  // verifies #3053
  const repoRoot = path.resolve(path.dirname(test.info().project.testDir), "..", "..");
  const { routes, rowLines } = readRoutes(repoRoot);
  expect(routes.length, `строк таблицы маршрутов ${rowLines}, разобрано ${routes.length} — разборщик слеп к форме записи`).toBe(rowLines);
  expect(routes.length, "таблица маршрутов пуста — вердикта нет").toBeGreaterThan(0);

  const publicRoutes = routes.filter((r) => !isInternalService(r.fqn));
  const internal = routes.filter((r) => isInternalService(r.fqn));
  for (const r of routes) {
    expect(r.method, `метод ${UNSERVED_METHOD} появился в контракте (${r.fqn}) — близнец больше не «то, чего нет»`).not.toBe(UNSERVED_METHOD);
  }

  await registerAndSignIn(page);
  const ask = async (method: string, url: string): Promise<Shot> => {
    const res = await page.request.fetch(url, { method, data: "{}", failOnStatusCode: false });
    return { status: res.status(), contentType: res.headers()["content-type"] ?? "", body: await res.text() };
  };

  // Положительный контроль: сессия действующая — публичное чтение проходит.
  const own = await ask("GET", "/iam/v1/projects");
  expect(own.status, `сессия не действует: GET своих проектов -> ${own.status} ${own.body.slice(0, 200)}`).toBe(200);

  let excluded = 0;
  const spaces = new Set<string>();
  const findings: string[] = [];
  let asked = 0;
  for (const r of internal) {
    const url = concretePath(r.template);
    if (publicRoutes.some((p) => p.method === r.method && matches(p.template, url))) {
      excluded++;
      continue;
    }
    const domain = url.split("/")[1];
    spaces.add(domain);
    const otherMethod = publicRoutes.some((p) => matches(p.template, url));
    let twin: Shot;
    if (otherMethod) {
      const free = TWIN_METHODS.find((m) => !routes.some((x) => x.method === m && matches(x.template, url)));
      if (free === undefined) {
        findings.push(`${r.fqn} (${r.method}): у пути нет метода-близнеца — сверять не с чем`);
        continue;
      }
      twin = await ask(free, url);
    } else {
      twin = await ask(r.method, `/${domain}/v1/${ABSENT_RESOURCE}`);
    }
    const got = await ask(r.method, url);
    asked++;
    const want = otherMethod ? 501 : 404;
    const same = got.status === twin.status && got.contentType === twin.contentType && got.body === twin.body;
    if (!same || got.status !== want || got.body.includes(r.fqn)) {
      // В отчёте — метод и вид исхода; адреса стенда сюда не попадают.
      findings.push(`${r.fqn} (${r.method}): ${got.status} ${got.body.slice(0, 160)} | близнец: ${twin.status} ${twin.body.slice(0, 160)}`);
    }
  }
  console.log(
    `[внутренние пространства] пар в таблице ${internal.length} · опрошено ${asked} в ${spaces.size} пространствах · ` +
      `исключено выводом (публичный шаблон под тем же методом) ${excluded} · находок ${findings.length}`,
  );
  expect(asked, "перечень внутренних пар после исключений пуст — пробе нечего утверждать").toBeGreaterThan(0);
  expect(findings, "внутреннее пространство на публичном входе отвечает не «маршрута нет»").toEqual([]);
});
