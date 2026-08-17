// Ветви контракта, достижимые из создания, — против формы МОДУЛЯ `nlb` (#375).
//
// Маршрут `/projects/:projectId/nlb/*` обслуживает этот модуль и его реестр
// (`host/src/App.tsx` → `NlbRemote` → `nlb/NlbPage` → `@/lib/resource-registry`).
// Поэтому спрашивать надо ЕГО: те же ветви, заведённые в `@shared`, до
// пользователя не доезжают — реестр `shared` обслуживает vpc/iam/system.
//
// Общий разбор контракта — `@shared/test/oneof-branch-coverage`: два места об
// одном предмете разошлись бы на первой же новой ветви.

import { REGISTRY } from "./resource-registry";
import { oneofBranches, unexpressibleBranches } from "@shared/test/oneof-branch-coverage";

describe("объём осмотренного — ветви читаются из контракта, а не из этого файла", () => {
  it("контракт проверки живости прочитан, и группа в нём найдена", () => {
    expect(oneofBranches("loadbalancer/v1/health_check.proto", "HealthCheck", "options")).toEqual([
      "tcp",
      "http",
      "https",
      "grpc",
    ]);
  });

  it("контракт цели прочитан, и группа в нём найдена", () => {
    expect(oneofBranches("loadbalancer/v1/target_group.proto", "Target", "identity")).toEqual([
      "instance_id",
      "nic_id",
      "ip_ref",
      "external_ip",
    ]);
  });

  it("разбор различает — контроль в обе стороны", () => {
    expect(() => oneofBranches("loadbalancer/v1/health_check.proto", "НетТакого", "options")).toThrow(/нет сообщения/);
    expect(() => oneofBranches("loadbalancer/v1/health_check.proto", "HealthCheck", "нет_такой")).toThrow(/нет группы/);
  });

  it("сопоставитель формы различает — контроль в обе стороны", () => {
    const spec = REGISTRY["target-groups"];
    expect(unexpressibleBranches(spec, "health_check", ["заведомо_нет"])).toEqual(["заведомо_нет"]);
    expect(unexpressibleBranches(spec, "health_check", ["tcp"])).toEqual([]);
  });
});

describe("каждая ветвь проверки живости выразима формой группы целей", () => {
  it("ни одна ветвь не осталась без поля", () => {
    const branches = oneofBranches("loadbalancer/v1/health_check.proto", "HealthCheck", "options");
    expect(unexpressibleBranches(REGISTRY["target-groups"], "health_check", branches)).toEqual([]);
  });

  it("и поля непустых ветвей доходят до тела запроса", () => {
    // Ветвь `http` несёт пять полей контракта; форма, знающая одно из них
    // (порт), выразила бы ветвь формально и не дала бы задать ни путь, ни коды.
    const names = (REGISTRY["target-groups"].fields ?? []).map((f) => f.name);
    for (const f of ["path", "expected_codes", "host", "headers"]) {
      expect(names).toContain(`health_check.http.${f}`);
      expect(names).toContain(`health_check.https.${f}`);
    }
    expect(names).toContain("health_check.grpc.service_name");
  });

  it("выбранная ветвь остаётся в теле ОДНА", () => {
    const body = REGISTRY["target-groups"].sanitize!({
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
    const body = REGISTRY["target-groups"].sanitize!({
      health_check: { tcp: { port: 80 }, interval: "2s" },
    }) as { health_check: Record<string, unknown> };
    expect(Object.keys(body.health_check).sort()).toEqual(["interval", "tcp"]);
  });
});

describe("балансировщик: каждая ветвь источника VIP выразима формой этого модуля", () => {
  const spec = REGISTRY["load-balancers"];

  function тело(mode: string, placement: string, fam: Record<string, unknown> = {}): Record<string, unknown> {
    return spec.sanitize!({
      project_id: "prj-1",
      region_id: "reg-1",
      placement,
      vip_source: { _v4_mode: mode, v4: fam, _v6_mode: "subnet", v6: {} },
    }) as Record<string, unknown>;
  }

  function ветви(body: Record<string, unknown>, at: string): string[] {
    const node = body[at];
    return node && typeof node === "object" ? Object.keys(node as Record<string, unknown>) : [];
  }

  it("перечень контракта совпадает с перечнем, который даёт форма", () => {
    const contract = oneofBranches("loadbalancer/v1/network_load_balancer.proto", "VipSource", "source");
    const форма: Record<string, Record<string, unknown>> = {
      public: тело("public", "EXTERNAL_REGIONAL"),
      subnet_id: тело("subnet", "INTERNAL_REGIONAL", { subnet_id: "sub-1" }),
      address_id: тело("address", "EXTERNAL_REGIONAL", { address_id: "adr-1" }),
    };
    const выразимо = contract.filter((branch) => ветви(форма[branch] ?? {}, "v4_source").includes(branch));
    expect(выразимо).toEqual(contract);
  });

  it("семейство с незаполненной ссылкой в тело не уезжает — отрицание в паре с положительным", () => {
    // Без него «ветвь доезжает» могло бы означать «доезжает всегда, в том числе
    // пустой». Ветвь ссылки без ссылки — не источник: сервер отверг бы запрос.
    const body = тело("address", "EXTERNAL_REGIONAL", {});
    expect(body).not.toHaveProperty("v4_source");
  });

  // ───────────────────────────────────────────────────────────────────────────
  // ОТКАЗ ОТ СЕМЕЙСТВА — тоже исход, и он обязан быть выразим.
  //
  // Сервер требует источник хотя бы для ОДНОГО семейства
  // (`services/nlb/.../vip_source.go`, «at least one ip family»), то есть
  // балансировщик только на IPv4 — законный ресурс. Форма этого модуля сказать
  // «не задавать это семейство» не давала: у внешнего размещения режимы были
  // только «публичный» и «линк адреса», а «публичный» — умолчание — источник
  // даёт ВСЕГДА. Значит оба семейства уезжали на провод, и отказаться было
  // нечем. Общий реестр такой вариант несёт («Не задавать это семейство»), но
  // `/nlb/*` рисует ЭТОТ модуль — тот же форк, что и у ветвей проверки живости.
  it("семейство можно НЕ ЗАДАВАТЬ — внешний балансировщик только на IPv4", () => {
    const body = spec.sanitize!({
      project_id: "prj-1",
      region_id: "reg-1",
      placement: "EXTERNAL_REGIONAL",
      vip_source: { _v4_mode: "public", v4: {}, _v6_mode: "off", v6: {} },
    }) as Record<string, unknown>;

    expect(body).toHaveProperty("v4_source");
    expect(body).not.toHaveProperty("v6_source");
  });

  it("и наоборот — только на IPv6, отказом от IPv4", () => {
    // Зеркало: без него «не задавать» могло бы означать «не задавать шестое»,
    // то есть свойство одного слота, а не режима.
    const body = spec.sanitize!({
      project_id: "prj-1",
      region_id: "reg-1",
      placement: "EXTERNAL_REGIONAL",
      vip_source: { _v4_mode: "off", v4: {}, _v6_mode: "public", v6: {} },
    }) as Record<string, unknown>;

    expect(body).not.toHaveProperty("v4_source");
    expect(body).toHaveProperty("v6_source");
  });

  it("умолчание формы не задаёт ОБА семейства сразу", () => {
    // Умолчание — то, что получает арендатор, не тронувший переключатель.
    // Пока оно шлёт оба, «отказаться можно» остаётся свойством, до которого
    // надо догадаться дойти.
    const body = spec.sanitize!({
      ...(spec.template({ projectId: "prj-1" }) as Record<string, unknown>),
      region_id: "reg-1",
    }) as Record<string, unknown>;

    expect(body).not.toHaveProperty("v6_source");
  });

  it("отказаться от ОБОИХ нельзя — форма говорит это сама, до отправки", () => {
    // Положительный контроль к трём отрицаниям выше: «опускает семейство» не
    // должно означать «опускает всё и отправляет тело, которое сервер отвергнет».
    const err = spec.validate!({
      placement: "EXTERNAL_REGIONAL",
      vip_source: { _v4_mode: "off", v4: {}, _v6_mode: "off", v6: {} },
    });
    expect(err).toMatch(/хотя бы для одного семейства/);
  });
});

describe("цели группы задаются при СОЗДАНИИ, а не только после него", () => {
  it("ни одна ветвь идентичности цели не осталась без поля", () => {
    const branches = oneofBranches("loadbalancer/v1/target_group.proto", "Target", "identity");
    expect(unexpressibleBranches(REGISTRY["target-groups"], "targets", branches)).toEqual([]);
  });

  it("выбранная ветвь идентичности уезжает в тело ОДНА", () => {
    const body = REGISTRY["target-groups"].sanitize!({
      targets: [
        { _identity_kind: "instance_id", instance_id: "ins-1", nic_id: "nic-1", weight: 5 },
        { _identity_kind: "external_ip", external_ip: { address: "203.0.113.7" }, instance_id: "ins-2" },
      ],
    }) as { targets: Array<Record<string, unknown>> };
    expect(body.targets).toEqual([{ instance_id: "ins-1", weight: 5 }, { external_ip: { address: "203.0.113.7" } }]);
  });

  it("пустой перечень целей не уезжает вовсе — положительный контроль", () => {
    // Пустой массив на проводе означал бы «целей нет», и это верно; но группа
    // без целей — законный исход создания, и лишний ключ в теле её не описывает.
    const body = REGISTRY["target-groups"].sanitize!({ targets: [] }) as Record<string, unknown>;
    expect(body).not.toHaveProperty("targets");
  });

  it("недозаполненная строка цели выбрасывается, заполненная — нет", () => {
    // Отрицание в паре с положительным: без него «выбрасывает пустые» могло бы
    // означать «выбрасывает все».
    const body = REGISTRY["target-groups"].sanitize!({
      targets: [
        { _identity_kind: "instance_id", instance_id: "" },
        { _identity_kind: "nic_id", nic_id: "nic-9" },
      ],
    }) as { targets: Array<Record<string, unknown>> };
    expect(body.targets).toEqual([{ nic_id: "nic-9" }]);
  });
});
