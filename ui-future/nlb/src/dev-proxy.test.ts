// Dev-прокси обязан покрывать каждый домен, к которому приложение обращается.
//
// Приложение — отдельный remote со своим dev-сервером: запрос к домену, для
// которого в `vite.config.ts` нет записи прокси, до края не доходит вовсе. Отказ
// при этом МОЛЧАЛИВЫЙ на уровне замысла: реестр честно объявляет apiPath, форма
// честно рисует список, и только сам список всегда пуст — что неотличимо от
// «у тенанта нет таких ресурсов».
//
// Проба declarative: читает ОБЪЯВЛЕНИЯ (реестр ресурсов + конфиг vite), а не
// поднятый сервер, — поэтому не может пропуститься из-за недоступного стенда.
// Предпосылка проверяется тут же: если в конфиге вообще не нашлось записей
// прокси, это находка, а не «ноль расхождений».

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { REGISTRY } from "./lib/resource-registry";

const VITE_CONFIG = "vite.config.ts";

/** Первый сегмент пути — домен, по которому vite решает, куда проксировать. */
function domainOf(apiPath: string): string {
  return "/" + apiPath.replace(/^\//, "").split("/")[0];
}

function proxiedDomains(): string[] {
  const raw = readFileSync(join(process.cwd(), VITE_CONFIG), "utf8");
  const proxy = raw.slice(raw.indexOf("proxy: {"));
  return [...proxy.matchAll(/"(\/[^"]+)":\s*\{/g)].map((m) => m[1]);
}

describe("dev-прокси покрывает домены, которые приложение адресует", () => {
  const proxied = proxiedDomains();

  it("в конфиге вообще есть записи прокси", () => {
    // Предпосылка гейта: без неё «все домены покрыты» означало бы «я не нашёл
    // ни конфига, ни доменов» — ноль осмотренного, выданный за ноль находок.
    expect(proxied.length).toBeGreaterThan(0);
  });

  it("реестр ресурсов непуст и называет домены", () => {
    const domains = new Set(Object.values(REGISTRY).map((s) => domainOf(s.apiPath)));
    expect(domains.size).toBeGreaterThan(0);
  });

  it("каждый домен из реестра ресурсов проксируется", () => {
    const domains = [...new Set(Object.values(REGISTRY).map((s) => domainOf(s.apiPath)))].sort();
    const missing = domains.filter((d) => !proxied.some((p) => p === d || p.startsWith(d + "/")));
    expect(missing).toEqual([]);
  });
});
