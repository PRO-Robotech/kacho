// Ветви контракта, достижимые из создания, — против формы реестра `@shared` (#375).
//
// Этот реестр обслуживает vpc/iam/system. Модули `compute`, `nlb`, `registry`,
// `storage` несут СВОИ реестры и спрашиваются своими пробами: правка здесь до
// них не доезжает, и именно так три ветви проверки живости прожили в дереве
// «исправленными» — в `@shared` они были, а `/nlb/*` рисует модуль `nlb`.
//
// Разбор контракта — общий (`@shared/test/oneof-branch-coverage`): два места об
// одном предмете разошлись бы на первой же новой ветви.

import { REGISTRY, SG_RULE_TARGET_FIELD, sanitizeSgRule } from "./resource-registry";
import { oneofBranches, unexpressibleBranches } from "@shared/test/oneof-branch-coverage";

describe("объём осмотренного — ветви читаются из контракта, а не из этого файла", () => {
  it("контракт проверки живости прочитан, и группа в нём найдена", () => {
    // Ровно четыре — если контракт заведёт пятую, ожидание ниже покраснеет само.
    expect(oneofBranches("loadbalancer/v1/health_check.proto", "HealthCheck", "options")).toEqual([
      "tcp",
      "http",
      "https",
      "grpc",
    ]);
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

  it("разбор видит ветви этих групп — иначе «ветвей нет» зеленит всё", () => {
    // Разбор, не видящий ветвей, возвращал бы для группы пустой список, то есть
    // утверждал «ветвей нет», и сверка проходила бы при любой форме.
    //
    // ЗАГОЛОВОК СУЖЕН, И ЭТО НЕ ОСЛАБЛЕНИЕ. Прежде проба называлась «ветвь,
    // объявленная С ОПЦИЕЙ»: у этих ветвей стоял хвост `[(length) = "<=50"]`, а
    // сама группа несла `option (exactly_one) = true`. Семейство снято
    // (kacho#1255), и ветвей `oneof` с хвостом опции в дереве НОЛЬ — предмет
    // прежнего заголовка исчез, и оставить его значило бы утверждать шире, чем
    // проба проверяет.
    //
    // Способность разбора переживать хвост опции проверяется там, где для неё
    // можно построить вход: internal/repohygiene/consoleoneofbranchprobe_injection_test.go
    // — на синтетике, с ЖИВЫМИ формами хвоста (`deprecated`, `secret_bearing`).
    expect(oneofBranches("vpc/v1/address_service.proto", "InternalIpv4AddressSpec", "scope")).toEqual(["subnet_id"]);
    expect(oneofBranches("vpc/v1/address_service.proto", "InternalIpv6AddressSpec", "scope")).toEqual(["subnet_id"]);
  });

  it("разбор читает ОБЪЯВЛЕНИЕ группы, а не упоминание её имени в комментарии", () => {
    // У `SecurityGroupRuleSpec` комментарий о снятой ветви называет `oneof
    // target` на 35 строк раньше самой группы, и поиск подстрокой уводил разбор
    // в соседнюю группу `protocol`. Проба при этом краснела — но не о том.
    expect(oneofBranches("vpc/v1/security_group_service.proto", "SecurityGroupRuleSpec", "target")).toEqual([
      "cidr_blocks",
      "security_group_id",
      "cidr_group_id",
    ]);
    expect(oneofBranches("vpc/v1/security_group_service.proto", "SecurityGroupRuleSpec", "protocol")).toEqual([
      "protocol_name",
      "protocol_number",
    ]);
  });

  it("сопоставитель формы различает — контроль в обе стороны", () => {
    // Ветвь, поля под которую заведомо нет, обязана быть названа невыразимой.
    expect(unexpressibleBranches(REGISTRY["target-groups"], "health_check", ["заведомо_нет"])).toEqual(["заведомо_нет"]);
    // А та, под которую поле есть, — не обязана.
    expect(unexpressibleBranches(REGISTRY["target-groups"], "health_check", ["tcp"])).toEqual([]);
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

describe("цели группы задаются при СОЗДАНИИ, а не только после него", () => {
  it("ни одна ветвь идентичности цели не осталась без поля", () => {
    const branches = oneofBranches("loadbalancer/v1/target_group.proto", "Target", "identity");
    expect(unexpressibleBranches(REGISTRY["target-groups"], "targets", branches)).toEqual([]);
  });

  it("выбранная ветвь идентичности уезжает в тело ОДНА", () => {
    const body = REGISTRY["target-groups"].sanitize!({
      targets: [
        { _identity_kind: "ip_ref", ip_ref: { subnet_id: "sub-1", address: "10.0.0.5" }, nic_id: "nic-1", weight: 7 },
      ],
    }) as { targets: Array<Record<string, unknown>> };
    expect(body.targets).toEqual([{ ip_ref: { subnet_id: "sub-1", address: "10.0.0.5" }, weight: 7 }]);
  });

  it("пустой перечень целей не уезжает вовсе — положительный контроль", () => {
    const body = REGISTRY["target-groups"].sanitize!({ targets: [] });
    expect(body).not.toHaveProperty("targets");
  });

  it("недозаполненная строка цели выбрасывается, заполненная — нет", () => {
    // Отрицание в паре с положительным: без него «выбрасывает пустые» могло бы
    // означать «выбрасывает все». Полнота считается ПО ВЫБРАННОЙ ВЕТВИ — так же,
    // как у статического маршрута, где счёт по одному полю молча терял шлюз.
    const body = REGISTRY["target-groups"].sanitize!({
      targets: [
        { _identity_kind: "ip_ref", ip_ref: { subnet_id: "sub-1" } },
        { _identity_kind: "instance_id", instance_id: "ins-9" },
      ],
    }) as { targets: Array<Record<string, unknown>> };
    expect(body.targets).toEqual([{ instance_id: "ins-9" }]);
  });
});

describe("зеркало: форма не называет ветви, которой у контракта НЕТ", () => {
  it("перечень целей правила совпадает с группой контракта — по составу", () => {
    // Направление, обратное всему остальному в этом файле. Ветвь, снятая с
    // контракта, но оставшаяся в форме, тоже не работает ни при каком вводе:
    // край выбрасывает незнакомый ключ молча, и правило уезжает без цели.
    const contract = oneofBranches("vpc/v1/security_group_service.proto", "SecurityGroupRuleSpec", "target");
    expect(Object.values(SG_RULE_TARGET_FIELD).sort()).toEqual([...contract].sort());
  });

  it("вид цели вне перечня не оставляет НИ ОДНОЙ ветви — «неизвестно» не значит «что было»", () => {
    // `predefined` был четвёртым видом цели и снят вместе со своей ветвью
    // контракта. Черновик, пришедший с таким видом, не вправе уехать с чужой
    // ветвью: тело с двумя целями сервер отвергает целиком, а тело с не той
    // целью — принимает, и правило означает не то, что показано.
    const out = sanitizeSgRule({
      direction: "INGRESS",
      _target_kind: "predefined",
      cidr_blocks: { v4_cidr_blocks: ["10.0.0.0/8"] },
      security_group_id: "sg-1",
      cidr_group_id: "cg-1",
    });
    for (const field of Object.values(SG_RULE_TARGET_FIELD)) expect(out).not.toHaveProperty(field);
  });

  it("известный вид цели по-прежнему оставляет СВОЮ ветвь — положительный контроль", () => {
    const out = sanitizeSgRule({
      direction: "INGRESS",
      _target_kind: "cidr-group",
      cidr_blocks: { v4_cidr_blocks: ["10.0.0.0/8"] },
      cidr_group_id: "cg-1",
    });
    expect(out.cidr_group_id).toBe("cg-1");
    expect(out).not.toHaveProperty("cidr_blocks");
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
