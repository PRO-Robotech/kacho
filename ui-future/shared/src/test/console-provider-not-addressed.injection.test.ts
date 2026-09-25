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

  // F3 проверки круга 2: обёртка построителя в исключённом `config.ts` и её вызов
  // из прод-файла. Изменён ровно один факт против близнеца — есть ли обёртка.
  const CONFIG_EXCUSE = {
    file: /^shared\/src\/lib\/config\.ts$/,
    reason: "ручки базы поставщика и их построители",
    covers: [
      "shared/src/lib/config.ts построитель адреса поставщика kratosUrl",
      "shared/src/lib/config.ts построитель адреса поставщика kratosUrl",
    ],
  };
  const CONFIG = lines(
    "export function kratosUrl(path: string): string {",
    "  return `${base}${path}`;",
    "}",
    "const base = kratosUrl.name;",
  );
  const WRAPPER = lines(CONFIG, 'export const loginFlowUrl = () => kratosUrl("/self-service/login/browser");');
  const CALLER = lines('import { loginFlowUrl } from "@shared/lib/config";', "window.location.assign(loginFlowUrl());");

  it("F8-38 · обёртка построителя в исключённом файле и её вызов извне — сверх перечня, красное с именем", () => {
    const census = providerCensusOf(
      [
        { file: "shared/src/lib/config.ts", text: WRAPPER },
        { file: "host/src/utils/auth.ts", text: CALLER },
      ],
      [CONFIG_EXCUSE],
    );
    expect(census.findings).toEqual([]);
    expect(census.excuseDrift).toEqual([
      "^shared\\/src\\/lib\\/config\\.ts$: отнесено 4, в перечне 2 · сверх перечня: " +
        "shared/src/lib/config.ts построитель адреса поставщика kratosUrl; " +
        "shared/src/lib/config.ts адрес поставщика «/self-service/login/browser»",
    ]);
  });

  it("F8-38 · близнец: тот же вызывающий без обёртки — перечень совпал, расхождения нет", () => {
    const census = providerCensusOf(
      [
        { file: "shared/src/lib/config.ts", text: CONFIG },
        {
          file: "host/src/utils/auth.ts",
          text: 'import { appOrigin } from "@shared/lib/config";\nexport const o = appOrigin;',
        },
      ],
      [CONFIG_EXCUSE],
    );
    expect(census.findings).toEqual([]);
    expect(census.excused).toHaveLength(2);
    expect(census.excuseDrift).toEqual([]);
  });

  it("F8-38 · построитель, позванный прямо из прод-файла, — находка с координатой, а не отнесённое", () => {
    const census = providerCensusOf(
      [
        { file: "shared/src/lib/config.ts", text: CONFIG },
        {
          file: "host/src/utils/auth.ts",
          text: lines('import { kratosUrl } from "@shared/lib/config";', 'window.location.assign(kratosUrl("/x"));'),
        },
      ],
      [CONFIG_EXCUSE],
    );
    expect(census.findings.map(formatFinding)).toEqual([
      "host/src/utils/auth.ts:2 построитель адреса поставщика kratosUrl",
    ]);
    expect(census.excuseDrift).toEqual([]);
  });

  it("F8-38 · перечень шире предмета — недостающее названо", () => {
    const census = providerCensusOf(
      [{ file: "shared/src/lib/config.ts", text: CONFIG }],
      [
        {
          ...CONFIG_EXCUSE,
          covers: [...CONFIG_EXCUSE.covers, "shared/src/lib/config.ts ручка базы поставщика VITE_KRATOS_URL"],
        },
      ],
    );
    expect(census.excuseDrift).toHaveLength(1);
    expect(census.excuseDrift[0]).toMatch(
      /нет в дереве: shared\/src\/lib\/config\.ts ручка базы поставщика VITE_KRATOS_URL$/,
    );
  });

  it("F8-38 · исключение, которому нечего исключать, — само находка; пустой обход — отказ", () => {
    const census = providerCensusOf(
      [{ file: "host/src/clean.ts", text: 'export const x = "/iam/v1/auth/me";' }],
      [{ file: /^host\/src\/clean\.ts$/, reason: "устаревшее послабление", covers: [] }],
    );
    expect(census.findings).toEqual([]);
    expect(census.staleExcuses).toHaveLength(1);
    expect(() => providerCensusOf([], [])).toThrow(/0 файлов/);
  });
});
