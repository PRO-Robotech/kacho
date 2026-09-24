// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { formatFinding, isProviderAddressText, providerAddressesIn, providerCensusOf } from "./provider-address-census";

/**
 * F8-38 — подсаженное обращение к поставщику перепись краснит С КООРДИНАТОЙ,
 * по одному каждого вида, и хотя бы одно — через построитель, а не литералом;
 * на снятой подсадке — снова ноль. Рядом законные близнецы: путь НАШЕГО
 * глагола и комментарий о поставщике находками не являются.
 */

const lines = (...xs: string[]) => xs.join("\n");

describe("F8-38 · перепись обращений к поставщику способна упасть", () => {
  it("F8-38 · переход на адрес потока поставщика — находка с координатой", () => {
    const f = providerAddressesIn(
      "host/src/Planted.tsx",
      lines(
        "export function go() {",
        '  window.location.assign("/.ory/kratos/public/self-service/login/browser");',
        "}",
      ),
    );
    expect(f.map(formatFinding)).toEqual([
      "host/src/Planted.tsx:2 адрес поставщика «/.ory/kratos/public/self-service/login/browser»",
    ]);
  });

  it("F8-38 · запрос к потоку поставщика шаблонной строкой — находка", () => {
    const f = providerAddressesIn(
      "shared/src/planted.ts",
      lines("export const out = (base: string) =>", "  fetch(`${base}/self-service/logout/browser`);"),
    );
    expect(f.map((x) => x.line)).toEqual([2]);
  });

  it("F8-38 · чтение сессии поставщика с чужим происхождением — находка", () => {
    const f = providerAddressesIn(
      "shared/src/planted.ts",
      'export const s = () => fetch("https://idp.example/sessions/whoami");',
    );
    expect(f).toHaveLength(1);
  });

  it("F8-38 · обращение ЧЕРЕЗ ПОСТРОИТЕЛЬ адреса, без литерала — находка", () => {
    const f = providerAddressesIn(
      "shared/src/planted.ts",
      lines('import { kratosUrl } from "@shared/lib/config";', "export const s = (p: string) => fetch(kratosUrl(p));"),
    );
    expect(f.map(formatFinding)).toContain("shared/src/planted.ts:2 построитель адреса поставщика kratosUrl");
  });

  it("F8-38 · импорт клиента поставщика — находка", () => {
    const f = providerAddressesIn(
      "shared/src/planted.ts",
      'import { kratos } from "@shared/lib/kratos";\nexport default kratos;',
    );
    expect(f.map((x) => x.what)).toContain("импорт клиента поставщика @shared/lib/kratos");
  });

  it("F8-38 · близнецы: путь нашего глагола и комментарий о поставщике — не находки", () => {
    const f = providerAddressesIn(
      "shared/src/twin.ts",
      lines(
        "// прежде здесь был переход на /.ory/kratos/public/self-service/logout/browser",
        'export const out = () => fetch("/iam/v1/auth/logout", { method: "POST" });',
        'export const me = () => fetch("/iam/v1/auth/me");',
      ),
    );
    expect(f).toEqual([]);
  });

  it("F8-38 · распознаватель знает все формы адреса поставщика и не узнаёт путей полосы", () => {
    // Контроль в обе стороны на входах дерева (приёмка F8, Р6 п. 2).
    for (const provider of [
      "/.ory/kratos/public",
      "/.ory/hydra/public",
      "/oauth2",
      "/oauth2/auth",
      "/.ory/kratos/public/self-service/login/browser",
      "/.ory/kratos/public/sessions/whoami",
      "https://idp.example/self-service/settings/browser",
      "/sessions/whoami",
    ]) {
      expect([provider, isProviderAddressText(provider)]).toEqual([provider, true]);
    }
    for (const ours of [
      "/iam/v1/auth/login",
      "/iam/v1/auth/logout",
      "/iam/v1/auth/password",
      "/iam/v1/auth/csrf",
      "/iam/v1/auth/register",
      "/iam/v1/auth/recovery",
      "/iam/v1/auth/recovery/complete",
      "/iam/v1/auth/second-factor",
      "/iam/v1/auth/second-factor/enroll",
      "/iam/v1/auth/second-factor/confirm",
      "/iam/v1/auth/second-factor/remove",
      "/iam/v1/auth/second-factor/backup-codes",
      "/iam/v1/auth/step-up",
    ]) {
      expect([ours, isProviderAddressText(ours)]).toEqual([ours, false]);
    }
  });

  it("F8-38 · исключение, которому нечего исключать, — само находка; пустой обход — отказ", () => {
    const census = providerCensusOf(
      [{ file: "host/src/clean.ts", text: 'export const x = "/iam/v1/auth/me";' }],
      [{ file: /^host\/src\/clean\.ts$/, reason: "устаревшее послабление" }],
    );
    expect(census.findings).toEqual([]);
    expect(census.staleExcuses).toHaveLength(1);
    expect(() => providerCensusOf([], [])).toThrow(/0 файлов/);
  });
});
