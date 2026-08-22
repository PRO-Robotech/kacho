// The zone picker is where a tenant actually meets geo. Every placeable resource
// takes its zoneId from here, and a zone that is closed to placement looks
// exactly like an open one unless the option says so — the request then fails
// later, at Create, with no hint that the choice was the problem.
//
// The closed option stays selectable on purpose: the server decides what is
// allowed, and greying it out here would be the UI narrowing rights it does not
// own. It is labelled, not removed.

import { refOptionExtra, refOptionLabel, refTagLabel } from "./refOptionLabel";

describe("refOptionExtra — zones", () => {
  it("shows the region a zone belongs to", () => {
    expect(refOptionExtra("zones", { id: "ru-central1-a", region_id: "ru-central1" })).toBe("ru-central1");
  });

  it("marks a zone that is closed to placement", () => {
    const extra = refOptionExtra("zones", {
      id: "ru-central1-a",
      region_id: "ru-central1",
      open_for_placement: false,
    });
    expect(extra).toContain("ru-central1");
    expect(extra).toContain("закрыта для размещения");
  });

  it("adds no marker to an open zone", () => {
    expect(refOptionExtra("zones", { id: "ru-central1-a", region_id: "ru-central1", open_for_placement: true })).toBe(
      "ru-central1",
    );
  });

  it("adds no marker when the server said nothing about placement", () => {
    // Absent is not closed. Claiming otherwise would push the operator away from
    // a zone that may well be usable.
    expect(refOptionExtra("zones", { id: "ru-central1-a", region_id: "ru-central1" })).not.toContain("закрыта");
  });
});

describe("refOptionExtra — regions", () => {
  it("shows the region id and marks a closed one", () => {
    expect(refOptionExtra("regions", { id: "ru-central1" })).toBe("ru-central1");
    expect(refOptionExtra("regions", { id: "ru-central1", open_for_placement: false })).toContain(
      "закрыт для размещения",
    );
  });
});

// The registry addresses the same two geo lists under a second pair of spec ids
// (`compute-zones` / `compute-regions`), and those are the ids the pickers that
// matter reference: Instance.zone_id and the NLB region fields. Labelling only
// the `zones` id left the paragraph at the top of this file describing something
// no operator ever sees — the picker they use had no placement hint at all.
describe("refOptionExtra — the ids the placement pickers actually reference", () => {
  it("labels the zone picker Instance.zone_id uses", () => {
    expect(refOptionExtra("compute-zones", { id: "ru-central1-a", region_id: "ru-central1" })).toBe("ru-central1");
    expect(
      refOptionExtra("compute-zones", { id: "ru-central1-a", region_id: "ru-central1", open_for_placement: false }),
    ).toContain("закрыта для размещения");
  });

  it("labels the region picker the balancer fields use", () => {
    expect(refOptionExtra("compute-regions", { id: "ru-central1", open_for_placement: false })).toContain(
      "закрыт для размещения",
    );
  });
});

describe("refOptionExtra — unrelated resources are untouched", () => {
  it("names the address-pool ranges the way AddressPool carries them", () => {
    // Ground truth: vpc/v1/internal_address_pool_service.proto — AddressPool
    // `reserved 7; reserved "cidr_blocks"`, split into `v4_cidr_blocks = 13` and
    // `v6_cidr_blocks = 14`. Reading the retired name yields undefined without a
    // word, so the option showed no range at all; the pool list in the same
    // package already reads the split pair.
    expect(refOptionExtra("address-pools", { v4_cidr_blocks: ["10.0.0.0/24"], is_default: true })).toBe(
      "10.0.0.0/24 · default",
    );
    expect(refOptionExtra("address-pools", { v6_cidr_blocks: ["2001:db8::/64"] })).toBe("2001:db8::/64");
    expect(
      refOptionExtra("address-pools", { v4_cidr_blocks: ["10.0.0.0/24"], v6_cidr_blocks: ["2001:db8::/64"] }),
    ).toBe("10.0.0.0/24, 2001:db8::/64");
  });

  it("returns nothing for a resource with no hint", () => {
    expect(refOptionExtra("projects", { id: "prj-1" })).toBe("");
  });
});

// Тип машины — ресурс ОБЩЕГО реестра (`machine-types`), и поле `machineTypeId`
// объявлено ссылкой на него там же. Приписки у него не было, поэтому дропдаун
// показывал одни имена: типы различаются размером, а размер — единственное, ради
// чего их выбирают. Подпись жила в копии одного модуля, то есть остальные
// показывали список неразличимых строк.
describe("refOptionExtra — тип машины называет размер", () => {
  it("показывает vCPU, память и семейство", () => {
    expect(refOptionExtra("machine-types", { effective_resources: { v_cpu: 2, memory_mib: "4096" }, family: "STANDARD" })).toBe(
      "2 vCPU · 4 ГиБ · STANDARD",
    );
  });

  it("память приходит строкой — int64 на wire сериализуется строкой", () => {
    // Число здесь — не косметика: `Number("4096")/1024` даёт 4, а конкатенация
    // строки дала бы «4096/1024» либо NaN, и подпись молча опустела бы.
    expect(refOptionExtra("machine-types", { effective_resources: { v_cpu: 1, memory_mib: "1024" } })).toContain("1 ГиБ");
  });

  it("неназванного размера не выдумывает", () => {
    // Пустой ответ лучше нуля: «0 vCPU» — утверждение о типе, которого сервер не
    // делал.
    expect(refOptionExtra("machine-types", {})).toBe("");
    expect(refOptionExtra("machine-types", { effective_resources: { memory_mib: 0 } })).toBe("");
  });

  it("нечисловое число ядер в подпись НЕ попадает", () => {
    // Ответ края нетипизирован, поэтому «не пусто» здесь ничего не гарантирует:
    // объект непуст и подставился бы как `[object Object]` — размер, которого
    // сервер не называл. Проверка «!= null» этот случай пропускала.
    for (const bad of [{}, { cores: 2 }, [], true]) {
      const got = refOptionExtra("machine-types", { effective_resources: { v_cpu: bad, memory_mib: "1024" } });
      expect(got).not.toContain("object");
      expect(got).not.toContain("vCPU");
    }
  });

  it("строковое число ядер в подпись попадает — положительный контроль", () => {
    // Без него запрет выше выполнялся бы реализацией, которая не показывает
    // число ядер никогда. int64 на wire — строка, и это законная форма.
    expect(refOptionExtra("machine-types", { effective_resources: { v_cpu: "8", memory_mib: "1024" } })).toContain(
      "8 vCPU",
    );
  });

  it("соседний ресурс приписку типа машины не получает (контроль)", () => {
    expect(refOptionExtra("projects", { effective_resources: { v_cpu: 2 } })).toBe("");
  });
});

// Подпись ВАРИАНТА в списке и подпись ВЫБРАННОГО значения (фишки внутри поля) —
// разные предметы, и различие это не косметическое. В списке человек выбирает
// по имени; в фишке имя ресурса-адреса («reserved-front») о выборе не говорит
// ничего, а ширину поля съедает. Решение владельца 2026-08-21: в фишке —
// только адрес.
describe("refTagLabel — фишка адреса называет адрес", () => {
  const ADDR = { id: "adr-a", name: "reserved-front", internal_ipv4_address: { address: "10.0.0.5" } };

  it("имя ресурса в фишку не попадает", () => {
    expect(refTagLabel("addresses", ADDR)).toBe("10.0.0.5");
  });

  it("в списке то же значение подписано ИМЕНЕМ и адресом (положительный контроль)", () => {
    // Без этой стороны «в фишке нет имени» выполнялось бы правилом, которое не
    // показывает имя нигде, — и выбирать стало бы не из чего.
    expect(refOptionLabel("addresses", ADDR)).toBe("reserved-front · 10.0.0.5");
  });

  it("внешний адрес читается так же, как внутренний", () => {
    expect(refTagLabel("addresses", { id: "adr-x", name: "vip", external_ipv4_address: { address: "203.0.113.7" } })).toBe(
      "203.0.113.7",
    );
  });

  it("края не назвал адреса — остаётся имя, а не пустая фишка", () => {
    // Фишка без подписи читается как сбой поля, а не как ресурс, о котором
    // сервер ничего не сказал.
    expect(refTagLabel("addresses", { id: "adr-y", name: "reserved-front" })).toBe("reserved-front");
  });

  it("нет ни имени, ни адреса — остаётся идентификатор", () => {
    expect(refTagLabel("addresses", { id: "adr-z" })).toBe("adr-z");
  });
});

// Сужение намеренно перечислением, а не признаком «у ресурса есть приписка»:
// приписка группы безопасности — сеть, зоны — регион, типа машины — размер, и
// ни одна из них имени не заменяет.
describe("refTagLabel — прочие ресурсы подписаны именем", () => {
  it("группа безопасности остаётся именем, а не сетью", () => {
    expect(refTagLabel("security-groups", { id: "sg-1", name: "web", network_id: "net-12345678" })).toBe("web");
  });

  it("зона остаётся именем, а не регионом", () => {
    expect(refTagLabel("zones", { id: "ru-central1-a", name: "ru-central1-a", region_id: "ru-central1" })).toBe(
      "ru-central1-a",
    );
  });

  it("безымянный ресурс подписан идентификатором", () => {
    expect(refTagLabel("security-groups", { id: "sg-2" })).toBe("sg-2");
  });
});

describe("refOptionLabel — подпись варианта", () => {
  it("без приписки — одно имя, без разделителя", () => {
    expect(refOptionLabel("projects", { id: "prj-1", name: "prod" })).toBe("prod");
  });

  it("безымянный ресурс подписан идентификатором", () => {
    expect(refOptionLabel("projects", { id: "prj-1" })).toBe("prj-1");
  });
});
