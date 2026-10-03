// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
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
