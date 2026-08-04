// Хлебные крошки хоста против поверхности ствола.
//
// Хост держит СВОЮ карту меток (по Module Federation он не импортирует реестры
// remote'ов), поэтому карта расходится с продуктом молча: снятый ресурс остаётся
// подписанным, а пришедший — нет, и пользователь видит «Раздел» вместо имени.
//
// Ground truth — proto ствола:
//   • `/compute/v1/disks` снят, блочное хранение принадлежит storage
//     (`/storage/v1/volumes`) → ключ `disks` больше не адресует ничего;
//   • ресурса anycast-пулов адресов нет НИ В ОДНОЙ ревизии (ни в стволе, ни в
//     замороженном main) — ключ `anycast-address-pools` описывает то, чего в
//     продукте не было никогда;
//   • `/storage/v1/volumes` и `/compute/v1/machineTypes` в стволе ЕСТЬ, и хост
//     маршрутизирует `/projects/:id/storage/*` и `/projects/:id/compute/*` —
//     значит метки для них обязаны быть.

import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { HostBreadcrumb } from ".";
import type { HostContext } from "../../../utils";

const here = path.dirname(fileURLToPath(import.meta.url));
const source = readFileSync(path.join(here, "HostBreadcrumb.tsx"), "utf8");

function mapKeys(name: string): string[] {
  const start = source.indexOf(`const ${name}`);
  const end = source.indexOf("\n};", start);
  expect(start).toBeGreaterThan(-1);
  expect(end).toBeGreaterThan(start);
  return [...source.slice(start, end).matchAll(/^ {2}"?([a-z][a-z-]*)"?:/gm)].map((m) => m[1]);
}

const jsonResponse = (body: unknown) =>
  Promise.resolve({ ok: true, text: () => Promise.resolve(JSON.stringify(body)), statusText: "OK" } as Response);

const emptyContext: HostContext = { account: null, project: null };

describe("HostBreadcrumb — метки против поверхности ствола", () => {
  const resources = mapKeys("RESOURCE_LABELS");
  const modules = mapKeys("MODULE_LABELS");

  it("объём осмотренного назван: обе карты распарсены", () => {
    expect(resources.length).toBeGreaterThanOrEqual(20);
    expect(modules.length).toBeGreaterThanOrEqual(4);
  });

  it("не подписывает ресурс, снятый со ствола или не существовавший в нём", () => {
    for (const retired of ["disks", "anycast-address-pools"]) {
      expect(resources).not.toContain(retired);
    }
  });

  it("подписывает ресурсы и модули, которые ствол несёт, а хост маршрутизирует", () => {
    for (const live of ["volumes", "machine-types", "registries"]) {
      expect(resources).toContain(live);
    }
    for (const mod of ["storage", "registry"]) {
      expect(modules).toContain(mod);
    }
  });
});

describe("HostBreadcrumb — крошка на живых адресах", () => {
  beforeEach(() => {
    jest.spyOn(global, "fetch").mockImplementation(() => jsonResponse({ accounts: [] }));
  });

  afterEach(() => {
    jest.restoreAllMocks();
    window.history.pushState(null, "", "/");
  });

  const at = (pathname: string) => {
    window.history.pushState(null, "", pathname);
    return render(<HostBreadcrumb context={emptyContext} onChange={jest.fn()} />);
  };

  it("на странице томов показывает «Хранилище / Тома», а не «STORAGE / Раздел»", () => {
    at("/projects/prj-1/storage/volumes");
    expect(screen.getByText("Хранилище")).toBeInTheDocument();
    expect(screen.getByText("Тома")).toBeInTheDocument();
    expect(screen.queryByText("Раздел")).not.toBeInTheDocument();
  });

  it("на адресах администрирования подписывает раздел, а не отдаёт «Раздел»", () => {
    // Каждый из трёх адресов рекламируется оболочкой: «Поиск» — рейлом,
    // остальные два — навигацией system-remote'а.
    for (const [pathname, label] of [
      ["/system/search", "Поиск"],
      ["/system/cluster/admins", "Cluster admins"],
      ["/system/tokens/user-tokens", "Токены и ключи"],
    ] as const) {
      const view = at(pathname);
      expect(screen.getByText(label)).toBeInTheDocument();
      expect(screen.queryByText("Раздел")).not.toBeInTheDocument();
      view.unmount();
    }
  });
});
