// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { EDGE_AUTHN_FAILED, EDGE_CREDENTIAL_STATE_UNKNOWN, EDGE_UNANSWERED_MESSAGE } from "./edge-answers";

// Ответы края, которые пробы консоли подставляют (`edge-answers.ts`), сверены с
// кодом ПРОИЗВОДИТЕЛЯ, а не с памятью автора пробы: рукописная копия ответа
// пережила бы смену формы края, и пробы консоли зеленели бы на ответе, которого
// край больше не производит (так пробы и утверждали `error_description=` после
// приёмки KA1, Р2). Здесь копия краснеет, как только разойдётся с литералом
// производителя.

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../../../..");

function source(rel: string): string {
  return readFileSync(path.join(repoRoot, rel), "utf8");
}

/** Значение строкового литерала Go `name = <литерал>` — сырого либо в кавычках. */
function goLiteral(src: string, name: string): string {
  const m = new RegExp(`\\b${name}\\s*=\\s*(?:\`([^\`]*)\`|"((?:[^"\\\\]|\\\\.)*)")`).exec(src);
  if (!m) throw new Error(`литерал ${name} не найден у производителя`);
  return m[1] ?? (JSON.parse(`"${m[2]}"`) as string);
}

describe("ответы края в пробах консоли — копия производителя (KA1, Р1 и Р2)", () => {
  it("Р2 · тело и вызов отказа `401` побайтово те, что пишет `authnrefusal.WriteHTTP`", () => {
    const src = source("gateway/internal/authnrefusal/authnrefusal.go");
    expect(src).toMatch(/w\.WriteHeader\(http\.StatusUnauthorized\)/);
    expect(EDGE_AUTHN_FAILED.status).toBe(401);
    expect(EDGE_AUTHN_FAILED.text).toBe(goLiteral(src, "body"));
    expect(EDGE_AUTHN_FAILED.headers["WWW-Authenticate"]).toBe(goLiteral(src, "Challenge"));
    expect(EDGE_AUTHN_FAILED.headers["WWW-Authenticate"]).not.toContain("error_description");
  });

  it("Р1 · ответ `503` на молчание авторитета — текст и форма `writeCredentialStateUnknown`", () => {
    const src = source("gateway/internal/middleware/credential_state_unknown.go");
    expect(EDGE_UNANSWERED_MESSAGE).toBe(goLiteral(src, "credentialStateUnknownReason"));
    expect(src).toMatch(/writeHTTPServiceUnavailable\(w, credentialStateUnknownReason\)/);
    const writer = source("gateway/internal/middleware/auth_revocation.go");
    expect(writer).toMatch(
      /func writeHTTPServiceUnavailable[\s\S]*?http\.StatusServiceUnavailable[\s\S]*?json\.NewEncoder\(w\)\.Encode\(map\[string\]any\{/,
    );
    expect(EDGE_CREDENTIAL_STATE_UNKNOWN.status).toBe(503);
    expect(EDGE_CREDENTIAL_STATE_UNKNOWN.headers["WWW-Authenticate"]).toBeUndefined();
    // `json.Encoder` словаря пишет ключи по алфавиту и перевод строки в конце.
    expect(EDGE_CREDENTIAL_STATE_UNKNOWN.text).toBe(
      `${JSON.stringify({ code: 14, message: goLiteral(src, "credentialStateUnknownReason") })}\n`,
    );
  });
});

// Шапка разборщика (`api/rpc-status.ts`) объясняет, почему «поля `details` нет»
// и «`details` пуст» — одно значение, и называет для этого писателей края. Писатель,
// которого край больше не производит, делает объяснение ложным, а терпимость
// разборщика — подпорой без производителя (так шапка пережила снятого
// писателя `401`, клавшего глагол и причины в `metadata`). Здесь каждый писатель края, названный шапкой, обязан
// быть объявлен в дереве края, и шапка обязана назвать того, кто тело без
// `details` действительно пишет.
describe("шапка разборщика отказа называет живых писателей края", () => {
  function gatewayFuncs(): Set<string> {
    const out = new Set<string>();
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const p = path.join(dir, e.name);
        if (e.isDirectory()) walk(p);
        else if (e.name.endsWith(".go") && !e.name.endsWith("_test.go")) {
          for (const m of readFileSync(p, "utf8").matchAll(/^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(/gm)) {
            out.add(`${path.basename(path.dirname(p))}.${m[1]}`);
            out.add(m[1]);
          }
        }
      }
    };
    walk(path.join(repoRoot, "gateway"));
    return out;
  }

  it("каждый писатель края в шапке объявлен в дереве края; тело без `details` — `writeCredentialStateUnknown`", () => {
    const header = source("ui-future/shared/src/api/rpc-status.ts").split("\nexport ")[0];
    // Писатели службы доступа живут в другом продукте (`kaname`) — их шапка
    // называет с пакетом `loginlanehttp`, и дерево края о них не судит.
    const named = [...header.matchAll(/`((?:[a-z]\w*\.)?[wW]rite\w*)`/g)]
      .map((m) => m[1])
      .filter((n) => !n.startsWith("loginlanehttp."));
    const funcs = gatewayFuncs();
    expect(funcs.size).toBeGreaterThan(0);
    expect(named.length).toBeGreaterThan(0);
    expect(named.filter((n) => !funcs.has(n))).toEqual([]);
    expect(named).toEqual(expect.arrayContaining(["authnrefusal.WriteHTTP", "writeCredentialStateUnknown"]));
  });
});
