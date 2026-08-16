// Ветвь контракта, достижимая из создания, обязана быть выразима формой (#375).
//
// ПРЕДМЕТ. Возможность, объявленная контрактом и покрытая типами, но не имеющая
// выражения в форме, не работает НИ ПРИ КАКОМ вводе. Это хуже отсутствия: она
// задокументирована, её ищут и о ней спрашивают. Класс тихий — код собирается,
// каждая сторона по отдельности выглядит исправно, а расходятся они в третьем
// месте.
//
// ИСТОЧНИК ИСТИНЫ — дерево контракта. Ветви читаются из `.proto`, а не
// выписываются здесь: добавленная в контракт ветвь роняет эту пробу сама, не
// дожидаясь, пока кто-нибудь вспомнит про форму.
//
// ЧТО ЗНАЧИТ «ВЫРАЗИМА». У формы есть поле, чьё имя ведёт в эту ветвь
// (`health_check.http.path` → ветвь `http`). Ветвь без единого поля не
// выразима: выбрать её пользователю нечем.
//
// Проба несёт: перепись прочитанного, контроль в обе стороны и самоистечение —
// запись про ветвь, которой в контракте больше нет, объявляется находкой.

import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

import { REGISTRY } from "./resource-registry";
import type { FormField } from "./form-schema";

const REPO_ROOT = resolve(process.cwd(), "../..");
const PROTO = join(REPO_ROOT, "proto", "kacho", "cloud");

/**
 * Ветви `oneof <name>` внутри `message <msg>` — по тексту контракта.
 *
 * Разбор нарочно узкий: ищется именно объявленное сообщение и именно
 * поимённая группа внутри него. Широкий разбор («любой oneof в файле») собрал бы
 * ветви соседних сообщений и молча раздул бы ожидание.
 */
function oneofBranches(relPath: string, message: string, oneofName: string): string[] {
  const text = readFileSync(join(PROTO, relPath), "utf8");
  const msgStart = text.search(new RegExp(`^message\\s+${message}\\s*\\{`, "m"));
  if (msgStart < 0) throw new Error(`в ${relPath} нет сообщения ${message} — контракт разошёлся с этой пробой`);
  const oneofStart = text.indexOf(`oneof ${oneofName}`, msgStart);
  if (oneofStart < 0) throw new Error(`в ${message} нет группы ${oneofName} — контракт разошёлся с этой пробой`);
  const open = text.indexOf("{", oneofStart);
  const close = text.indexOf("}", open);
  const body = text
    .slice(open + 1, close)
    .split("\n")
    .map((l) => l.replace(/\/\/.*$/, "").trim())
    .filter(Boolean);
  const branches: string[] = [];
  for (const line of body) {
    // `TcpOptions tcp = 6;` — тип, имя, номер. `option (...)` веткой не является.
    const m = /^[A-Za-z_][\w.]*\s+([a-z_][\w]*)\s*=\s*\d+\s*;/.exec(line);
    if (m) branches.push(m[1]);
  }
  return branches;
}

/** Все имена полей формы ресурса — включая скрытые: они тоже уезжают в тело. */
function fieldNames(specId: string): string[] {
  const fields: FormField[] = REGISTRY[specId]?.fields ?? [];
  return fields.map((f) => f.name);
}

/** Ветви, у которых нет ни одного поля формы под своим префиксом. */
function unexpressible(specId: string, prefix: string, branches: string[]): string[] {
  const names = fieldNames(specId);
  return branches.filter((b) => !names.some((n) => n === `${prefix}.${b}` || n.startsWith(`${prefix}.${b}.`)));
}

// ── объём осмотренного ───────────────────────────────────────────────────────

describe("объём осмотренного — ветви читаются из контракта, а не из этого файла", () => {
  it("контракт проверки живости прочитан, и группа в нём найдена", () => {
    const branches = oneofBranches("loadbalancer/v1/health_check.proto", "HealthCheck", "options");
    // Ровно четыре — если контракт заведёт пятую, ожидание ниже покраснеет само.
    expect(branches).toEqual(["tcp", "http", "https", "grpc"]);
  });

  it("контракт статического маршрута прочитан, и обе группы в нём найдены", () => {
    expect(oneofBranches("vpc/v1/route_table.proto", "StaticRoute", "next_hop")).toEqual([
      "next_hop_address",
      "gateway_id",
    ]);
    expect(oneofBranches("vpc/v1/route_table.proto", "StaticRoute", "destination")).toEqual(["destination_prefix"]);
  });

  it("разбор различает — контроль в обе стороны", () => {
    // Несуществующее сообщение обязано быть ОТКАЗОМ, а не пустым списком:
    // пустой список означал бы «ветвей нет» и зеленил бы всё сразу.
    expect(() => oneofBranches("vpc/v1/route_table.proto", "НетТакого", "next_hop")).toThrow(/нет сообщения/);
    expect(() => oneofBranches("vpc/v1/route_table.proto", "StaticRoute", "нет_такой")).toThrow(/нет группы/);
  });

  it("сопоставитель формы различает — контроль в обе стороны", () => {
    // Ветвь, поля под которую заведомо нет, обязана быть названа невыразимой.
    expect(unexpressible("target-groups", "health_check", ["заведомо_нет"])).toEqual(["заведомо_нет"]);
    // А та, под которую поле есть, — не обязана.
    expect(unexpressible("target-groups", "health_check", ["tcp"])).toEqual([]);
  });
});

// ── сами ветви ───────────────────────────────────────────────────────────────

describe("каждая ветвь проверки живости выразима формой группы целей", () => {
  it("ни одна ветвь не осталась без поля", () => {
    const branches = oneofBranches("loadbalancer/v1/health_check.proto", "HealthCheck", "options");
    expect(unexpressible("target-groups", "health_check", branches)).toEqual([]);
  });

  it("и поля непустых ветвей доходят до тела запроса", () => {
    // Ветвь `http` несёт пять полей контракта; форма, знающая одно из них
    // (порт), выразила бы ветвь формально и не дала бы задать ни путь, ни коды.
    const names = fieldNames("target-groups");
    for (const f of ["path", "expected_codes", "host", "headers"]) {
      expect(names).toContain(`health_check.http.${f}`);
      expect(names).toContain(`health_check.https.${f}`);
    }
    expect(names).toContain("health_check.grpc.service_name");
  });

  it("выбранная ветвь остаётся в теле ОДНА", () => {
    // Группа взаимоисключающая (`exactly_one`): две заполненные ветви — отказ
    // сервера. Форма держит поля всех ветвей, поэтому лишние обязана срезать.
    const spec = REGISTRY["target-groups"];
    const body = spec.sanitize!({
      _health_check_protocol: "http",
      health_check: {
        tcp: { port: 80 },
        http: { port: 8080, path: "/healthz" },
        https: { port: 443 },
        grpc: { service_name: "svc" },
        interval: "2s",
      },
    }) as { health_check: Record<string, unknown> };
    expect(Object.keys(body.health_check).sort()).toEqual(["http", "interval"]);
    expect(body.health_check.http).toEqual({ port: 8080, path: "/healthz" });
    expect(body).not.toHaveProperty("_health_check_protocol");
  });

  it("ветвь без выбора остаётся прежней — tcp, как и было", () => {
    // Положительный контроль на умолчание: без него «срезает лишнее» могло бы
    // означать «срезает всё».
    const spec = REGISTRY["target-groups"];
    const body = spec.sanitize!({
      health_check: { tcp: { port: 80 }, interval: "2s" },
    }) as { health_check: Record<string, unknown> };
    expect(Object.keys(body.health_check).sort()).toEqual(["interval", "tcp"]);
  });
});

describe("маршрут на шлюз доезжает до тела запроса", () => {
  const spec = REGISTRY["route-tables"];

  it("строка со шлюзом не выбрасывается как недозаполненная", () => {
    // Прежде полнота строки считалась по адресу, поэтому маршрут на шлюз
    // выбрасывался молча: форма принимала ввод, ничего не говорила и
    // отправляла таблицу без него.
    const out = spec.sanitize!({
      static_routes: [{ destination_prefix: "0.0.0.0/0", gateway_id: "gw-1" }],
    }) as { static_routes: unknown[] };
    expect(out.static_routes).toEqual([{ destination_prefix: "0.0.0.0/0", gateway_id: "gw-1" }]);
  });

  it("строка с адресом по-прежнему доезжает — положительный контроль", () => {
    const out = spec.sanitize!({
      static_routes: [{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" }],
    }) as { static_routes: unknown[] };
    expect(out.static_routes).toHaveLength(1);
  });

  it("недозаполненные строки обеих ветвей по-прежнему выбрасываются", () => {
    // Отрицание в паре с положительным: без него «не выбрасывает шлюз» могло бы
    // означать «не выбрасывает ничего», и в тело уехали бы пустые строки.
    const out = spec.sanitize!({
      static_routes: [
        { destination_prefix: "", gateway_id: "gw-1" },
        { destination_prefix: "0.0.0.0/0", gateway_id: "" },
        { destination_prefix: "0.0.0.0/0", next_hop_address: "" },
      ],
    }) as { static_routes: unknown[] };
    expect(out.static_routes).toEqual([]);
  });
});
