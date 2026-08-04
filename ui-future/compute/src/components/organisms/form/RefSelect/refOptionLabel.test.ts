// Подпись опции читает поля РЕСУРСА — значит она утверждение о контракте, и
// протухает она молча.
//
// Поле, переименованное в стволе, здесь не даёт ни ошибки, ни предупреждения:
// `row.<старое_имя>` — просто undefined, приписка становится пустой, и опция
// выглядит как ресурс без адреса. Отличить «у подсети нет CIDR» от «мы читаем
// имя, которого больше нет» на экране нельзя.
//
// Ground truth — proto ствола:
//   Subnet (proto/kacho/cloud/vpc/v1/subnet.proto): `reserved 10, 11` — прежние
//   v4_cidr_blocks/v6_cidr_blocks сняты; вместо них immutable-якорь
//   ipv4_cidr_primary/ipv6_cidr_primary + дополнительные ipv4_cidr_blocks/
//   ipv6_cidr_blocks.
//   Zone (proto/kacho/cloud/geo/v1/zone.proto): `reserved 3; reserved "status"`
//   — публичный status снят; единственный действующий признак размещения —
//   open_for_placement (+ placement_blocked_reason).
//   MachineType (proto/kacho/cloud/compute/v1/machine_type.proto): family +
//   effective_resources{v_cpu, memory_mib, gpus, gpu_type}.

import { extraInfoFor, headLabelFor } from "./refOptionLabel";

describe("extraInfoFor — subnets читают поля CIDR по стволу", () => {
  it("показывает первичный якорь IPv4", () => {
    expect(extraInfoFor("subnets", { ipv4_cidr_primary: "10.20.0.0/24" })).toContain("10.20.0.0/24");
  });

  it("показывает первичный якорь IPv6", () => {
    expect(extraInfoFor("subnets", { ipv6_cidr_primary: "2001:db8::/64" })).toContain("2001:db8::/64");
  });

  it("показывает дополнительные диапазоны рядом с якорем", () => {
    const extra = extraInfoFor("subnets", {
      ipv4_cidr_primary: "10.20.0.0/24",
      ipv4_cidr_blocks: ["10.20.1.0/24"],
    });
    expect(extra).toContain("10.20.0.0/24");
    expect(extra).toContain("10.20.1.0/24");
  });

  it("снятые имена не читаются — строка, которой в стволе нет, подписи не даёт", () => {
    // Отрицание с положительным контролем выше: если бы функция читала оба
    // набора имён, этот кейс прошёл бы вместе с предыдущими и ничего не отличал.
    expect(extraInfoFor("subnets", { v4_cidr_blocks: ["10.9.9.0/24"], v6_cidr_blocks: ["2001:db8:9::/64"] })).toBe("");
  });
});

describe("extraInfoFor — zones несут признак доступности размещения", () => {
  it("показывает регион зоны — авторитетное поле ответа geo", () => {
    expect(extraInfoFor("zones", { id: "ru-central1-a", region_id: "ru-central1" })).toBe("ru-central1");
  });

  it("помечает зону, закрытую для размещения", () => {
    const extra = extraInfoFor("zones", {
      id: "ru-central1-a",
      region_id: "ru-central1",
      open_for_placement: false,
    });
    expect(extra).toContain("ru-central1");
    expect(extra).toContain("закрыта для размещения");
  });

  it("открытую зону не помечает", () => {
    expect(extraInfoFor("zones", { id: "ru-central1-a", region_id: "ru-central1", open_for_placement: true })).toBe(
      "ru-central1",
    );
  });

  it("молчание сервера о размещении не превращается в «закрыта»", () => {
    expect(extraInfoFor("zones", { id: "ru-central1-a", region_id: "ru-central1" })).not.toContain("закрыта");
  });
});

describe("extraInfoFor — свои случаи compute сохраняются", () => {
  it("тип машины показывает размер и семейство", () => {
    const extra = extraInfoFor("machine-types", {
      effective_resources: { v_cpu: 2, memory_mib: "4096" },
      family: "STANDARD",
    });
    expect(extra).toContain("2 vCPU");
    expect(extra).toContain("4 ГиБ");
    expect(extra).toContain("STANDARD");
  });

  it("ресурс без приписки её и не получает", () => {
    expect(extraInfoFor("projects", { id: "prj-1" })).toBe("");
  });
});

describe("headLabelFor", () => {
  it("у пользователя имени нет — берётся отображаемое имя или почта", () => {
    expect(headLabelFor("users", { display_name: "Ада", email: "a@example.test" })).toBe("Ада");
    expect(headLabelFor("users", { email: "a@example.test" })).toBe("a@example.test");
  });

  it("у остальных ресурсов — name", () => {
    expect(headLabelFor("zones", { name: "Зона A" })).toBe("Зона A");
  });
});
