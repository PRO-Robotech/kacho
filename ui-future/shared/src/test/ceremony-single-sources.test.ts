// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { fileURLToPath } from "node:url";
import { KEPT_ACROSS_PRINCIPALS, PRINCIPAL_BOUND_STATE } from "@shared/lib/principal-state";
import {
  browserStoreUsers,
  ceremonySecretSinks,
  formatCensusFinding,
  functionParsesJsonItself,
  importsFrom,
  sessionAddressReaders,
  sessionPredicateCallers,
  type Source,
} from "./ceremony-census";
import { codeSources } from "./provider-address-census";

/**
 * Единственные источники церемоний личности в дереве консоли (приёмка F8):
 *
 *   • C7  — «есть ли сессия» спрашивает ОДИН читатель ответа края, и стражи
 *           (экран входа, экран параметров, каркас, контекст личности) зовут его;
 *   • C17 — секрет заведения второго фактора и запасные коды не оседают ни в
 *           хранилище браузера, ни в адресе, ни в журнале, ни в кэше запросов;
 *   • C14 — у каждого хранилища браузера, которым пользуется консоль, решено,
 *           чьё оно: привязанное к человеку снимается после выхода, остальное
 *           названо перечнем;
 *   • C3  — разборщик отказа `google.rpc.Status` один у экранов и у
 *           распознавателя сквозного набора.
 *
 * Перепись печатает знаменатель — число прочитанных файлов — и краснеет на
 * пустом обходе. Способность упасть держат инъекции ниже, на подсаженном тексте.
 */

const uiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

function isProductFile(rel: string): boolean {
  return /\.(ts|tsx)$/.test(rel) && !/\.test\./.test(rel) && !rel.startsWith("e2e/") && !/(^|\/)test\//.test(rel);
}

const product = codeSources(uiRoot, isProductFile);

/** Файлы экранов и клиента церемоний — где живут секрет заведения и запасные коды. */
const CEREMONY_FILES =
  /^(shared\/src\/pages\/auth\/|shared\/src\/components\/molecules\/auth\/|shared\/src\/api\/login-lane\.ts$|host\/src\/components\/organisms\/AccountPanel\/)/;
const ceremony = product.filter((s) => CEREMONY_FILES.test(s.file));

/** Единственный читатель ответа края о сессии. */
const SESSION_READER = "shared/src/api/login-lane.ts";

/** Стражи и спрашивающие «есть ли сессия» — все обязаны звать единственного читателя. */
const SESSION_ASKERS = [
  "shared/src/pages/auth/LoginPage.tsx",
  "shared/src/pages/auth/AccountSettingsPage.tsx",
  "shared/src/contexts/AuthContext.tsx",
  "host/src/utils/session.ts",
];

describe("единственные источники церемоний личности", () => {
  const readers = sessionAddressReaders(product);
  const callers = sessionPredicateCallers(product);
  process.stdout.write(
    `\n[C7] прод-файлов прочитано ${product.length} · читателей адреса ответа о сессии ${new Set(readers.map((r) => r.file)).size}` +
      ` · спрашивающих «есть ли сессия» ${new Set(callers.map((c) => c.file)).size}: ` +
      `${[...new Set(callers.map((c) => c.file))].sort().join(", ")}\n`,
  );

  it("C7 · знаменатель обхода — прод-дерево консоли прочитано", () => {
    expect(product.length).toBeGreaterThan(300);
    expect(ceremony.length).toBeGreaterThan(8);
  });

  it("C7 · адрес ответа края о сессии называет ОДИН файл — единственный читатель", () => {
    expect([...new Set(readers.map((r) => r.file))]).toEqual([SESSION_READER]);
  });

  it("C7 · каждый страж спрашивает единственного читателя, а не свою копию", () => {
    const asking = new Set(callers.map((c) => c.file));
    expect(SESSION_ASKERS.filter((f) => !asking.has(f))).toEqual([]);
  });

  it("C17 · экраны церемоний не кладут секрет ни в хранилище, ни в адрес, ни в журнал, ни в кэш", () => {
    const found = ceremonySecretSinks(ceremony).map(formatCensusFinding);
    process.stdout.write(`\n[C17] файлов церемоний прочитано ${ceremony.length} · мест оседания ${found.length}\n`);
    expect(found).toEqual([]);
  });

  it("C14 · у каждого хранилища браузера решено, чьё оно, и решение не пережило свой предмет", () => {
    const users = browserStoreUsers(product);
    const declared = new Set([...PRINCIPAL_BOUND_STATE, ...KEPT_ACROSS_PRINCIPALS].flatMap((s) => s.files));
    // Снимающий привязанное — сам объявитель перечня; решать «чьё» ему незачем.
    const undeclared = [...users.keys()]
      .filter((f) => !declared.has(f) && f !== "shared/src/lib/principal-state.ts")
      .sort();
    const stale = [...declared].filter((f) => !users.has(f)).sort();
    process.stdout.write(
      `\n[C14] прод-файлов с хранилищем браузера ${users.size} · привязанных к человеку записей ` +
        `${PRINCIPAL_BOUND_STATE.length} · оставляемых ${KEPT_ACROSS_PRINCIPALS.length}\n`,
    );
    expect({ undeclared, stale }).toEqual({ undeclared: [], stale: [] });
    // Ключ привязанной записи назван в её файлах литералом — снимается ровно то, что пишут.
    for (const s of PRINCIPAL_BOUND_STATE) {
      for (const f of s.files) {
        expect([f, product.find((p) => p.file === f)?.text.includes(`"${s.key}"`)]).toEqual([f, true]);
      }
    }
  });

  it("C3 · распознаватель отказа набора разбирает тело ТЕМ ЖЕ разборщиком, что экраны", () => {
    const [fixtures] = codeSources(uiRoot, (rel) => rel === "e2e/specs/fixtures.ts");
    expect(fixtures?.file).toBe("e2e/specs/fixtures.ts");
    expect(importsFrom(fixtures, "parseRpcStatus", "shared/src/api/rpc-status")).toBe(true);
    expect(functionParsesJsonItself(fixtures, "identityRefusalFromText")).toBe(false);
    const [lane] = product.filter((p) => p.file === SESSION_READER);
    expect(importsFrom(lane, "parseRpcStatus", "./rpc-status")).toBe(true);
  });
});

describe("инъекции: каждая перепись умеет упасть и не падает на законном близнеце", () => {
  const planted = (file: string, ...lines: string[]): Source => ({ file, text: lines.join("\n") });

  it("C7 · второй читатель ответа о сессии находится с координатой, спрашивающий — нет", () => {
    const readers = sessionAddressReaders([
      planted("host/src/utils/me.ts", "export const who = () => fetch('/iam/v1/auth/me');"),
      planted("host/src/utils/ok.ts", "// читатель — '/iam/v1/auth/me' в клиенте полосы", "sessionIdentity();"),
    ]);
    expect(readers.map(formatCensusFinding)).toEqual([
      "host/src/utils/me.ts:1 адрес ответа края о сессии «/iam/v1/auth/me»",
    ]);
    expect(sessionPredicateCallers([planted("a.ts", "void sessionIdentity();")])).toHaveLength(1);
  });

  it("C17 · хранилище, адрес и журнал с секретом находятся; чужое свойство с тем же именем — нет", () => {
    const found = ceremonySecretSinks([
      planted(
        "shared/src/pages/auth/Planted.tsx",
        "localStorage.setItem('codes', codes.join());",
        "window.sessionStorage.setItem('s', secret);",
        "history.replaceState(null, '', `?secret=${secret}`);",
        "console.log(secret);",
        "const q = useQuery({ queryKey: ['codes'] });",
      ),
    ]).map(formatCensusFinding);
    expect(found).toEqual([
      "shared/src/pages/auth/Planted.tsx:1 постоянное хранилище браузера",
      "shared/src/pages/auth/Planted.tsx:2 хранилище вкладки",
      "shared/src/pages/auth/Planted.tsx:3 запись адреса страницы (replaceState)",
      "shared/src/pages/auth/Planted.tsx:4 журнал консоли браузера",
      "shared/src/pages/auth/Planted.tsx:5 кэш запросов (useQuery)",
    ]);
    const twin = ceremonySecretSinks([
      planted("shared/src/pages/auth/Twin.tsx", "const p = { console: 1 };", "props.console;", "// localStorage"),
    ]);
    expect(twin).toEqual([]);
  });

  it("C14 · хранилище в файле без решения — находка; решение без хранилища — находка", () => {
    const users = browserStoreUsers([
      planted("vpc/src/new.ts", "window.localStorage.setItem('k', v);"),
      planted("vpc/src/twin.ts", "const x = cfg.localStorage;"),
    ]);
    expect([...users.keys()]).toEqual(["vpc/src/new.ts"]);
  });

  it("C3 · распознаватель со своим JSON.parse — находка; через общий разборщик — нет", () => {
    const own = planted(
      "e2e/specs/fixtures.ts",
      "export function identityRefusalFromText(t) { return JSON.parse(t); }",
    );
    const shared = planted(
      "e2e/specs/fixtures.ts",
      'import { parseRpcStatus } from "../../shared/src/api/rpc-status";',
      "export function identityRefusalFromText(t) { return parseRpcStatus(t); }",
    );
    expect(functionParsesJsonItself(own, "identityRefusalFromText")).toBe(true);
    expect(importsFrom(own, "parseRpcStatus", "shared/src/api/rpc-status")).toBe(false);
    expect(functionParsesJsonItself(shared, "identityRefusalFromText")).toBe(false);
    expect(importsFrom(shared, "parseRpcStatus", "shared/src/api/rpc-status")).toBe(true);
  });
});
