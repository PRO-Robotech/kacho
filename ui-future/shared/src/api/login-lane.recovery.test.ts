// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { SIGNED_IN, installLane } from "@shared/test/lane-fake";
import { FormTokenHolder, LOGIN_LANE, loginLane } from "./login-lane";

// F8S3-17 (приёмка F8-S3, #2952) — клиент полосы знает два вида формы
// восстановления и шлёт РОВНО объявленные поля. Разбор тела у службы строгий
// (`DisallowUnknownFields`): лишнее поле обратило бы каждое завершение отказом
// `400`, недостающее — отказом о поле. Поэтому тело сверяется множеством ключей,
// а не «содержит».

const REQUEST_KEYS = ["csrfToken", "email"];
const COMPLETE_KEYS = ["code", "csrfToken", "email", "newPassword"];

/** Ключи тела сверх объявленных — пусто, когда тело ровно объявленное. */
function undeclared(body: Record<string, unknown> | null, declared: readonly string[]): string[] {
  return Object.keys(body ?? {}).filter((k) => !declared.includes(k)).sort();
}

/** Члены объединения `FormKind` в исходнике клиента — разбором, а не счётом строк. */
function formKinds(): string[] {
  const file = fileURLToPath(new URL("./login-lane.ts", import.meta.url));
  const sf = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true);
  const out: string[] = [];
  sf.forEachChild((n) => {
    if (!ts.isTypeAliasDeclaration(n) || n.name.text !== "FormKind" || !ts.isUnionTypeNode(n.type)) return;
    for (const t of n.type.types) {
      if (ts.isLiteralTypeNode(t) && ts.isStringLiteral(t.literal)) out.push(t.literal.text);
    }
  });
  return out;
}

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("F8S3-17 · клиент полосы и глаголы восстановления", () => {
  it("F8S3-17 · запрос кода: признак вида recovery, затем POST /iam/v1/auth/recovery телом ровно {email, csrfToken}", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery": { status: 200, body: {} } });
    await loginLane.requestRecovery(new FormTokenHolder("recovery"), { email: "a@kacho.local" });
    expect(lane.calls.map((c) => `${c.method} ${c.path}${c.query}`)).toEqual([
      "GET /iam/v1/auth/csrf?form=recovery",
      "POST /iam/v1/auth/recovery",
    ]);
    const [posted] = lane.of("POST", LOGIN_LANE.recovery);
    expect(posted.body).toEqual({ email: "a@kacho.local", csrfToken: "tok-recovery-1" });
    expect(undeclared(posted.body, REQUEST_KEYS)).toEqual([]);
  });

  it("F8S3-17 · завершение: признак вида recovery-complete, тело ровно {email, code, newPassword, csrfToken}, secondFactor нет", async () => {
    lane = installLane({ "POST /iam/v1/auth/recovery/complete": SIGNED_IN });
    const signed = await loginLane.completeRecovery(new FormTokenHolder("recovery-complete"), {
      email: "a@kacho.local",
      code: "ABCDE-FGHJK",
      newPassword: "new-password-1",
    });
    expect(signed.user.id).toBe("usr-1");
    expect(lane.calls.map((c) => `${c.method} ${c.path}${c.query}`)).toEqual([
      "GET /iam/v1/auth/csrf?form=recovery-complete",
      "POST /iam/v1/auth/recovery/complete",
    ]);
    const [posted] = lane.of("POST", LOGIN_LANE.recoveryComplete);
    expect(Object.keys(posted.body ?? {}).sort()).toEqual(COMPLETE_KEYS);
    expect(posted.body).not.toHaveProperty("secondFactor");
  });

  it("F8S3-17 · отрицательный контроль: подсаженное secondFactor сверка называет по имени; без подсадки — пусто", () => {
    const body = { email: "a@kacho.local", code: "c", newPassword: "p", csrfToken: "t" };
    expect(undeclared(body, COMPLETE_KEYS)).toEqual([]);
    expect(undeclared({ ...body, secondFactor: { code: "1" } }, COMPLETE_KEYS)).toEqual(["secondFactor"]);
  });

  it("F8S3-17 · FormKind несёт recovery и recovery-complete — видов на два больше базы", () => {
    // База — 10 видов на голове сборки (8 на 9b14ae7f01c из N12 и два вида входа
    // ключом, F8-S4); S3 добавляет ровно два.
    const kinds = formKinds();
    expect(kinds).toEqual(expect.arrayContaining(["recovery", "recovery-complete"]));
    expect(kinds).toHaveLength(12);
    expect(new Set(kinds).size).toBe(kinds.length);
  });

  it("пути глаголов — точные, как их объявляет служба и ретранслирует край", () => {
    expect([LOGIN_LANE.recovery, LOGIN_LANE.recoveryComplete]).toEqual([
      "/iam/v1/auth/recovery",
      "/iam/v1/auth/recovery/complete",
    ]);
  });
});
