import { jest } from "@jest/globals";

// NlbVipSourceField тянет RefSelect → весь form-движок; для чистого логического
// теста хелперов мокаем RefSelect (в этом тесте компонент не рендерится).
//
// Псевдоним — `@shared`, а не `@/`: проба переехала сюда вместе со своим
// предметом (#1471), а `@/` каждый модуль отображает НА СЕБЯ. В модуле, где он
// объявлен, подмена молча промахнулась бы мимо файла, который компонент реально
// импортирует; в модуле, где его нет вовсе (vpc/iam/system), суита не поднялась
// бы целиком — и это единственный из двух исходов, который видно.
jest.unstable_mockModule("@shared/components/organisms/form/RefSelect", () => ({ RefSelect: () => null }));

const { effectiveVipMode, buildVipSourceOrNull, familyIpVersion, subnetPlacementMatches, linkAddressFilter } =
  await import("./NlbVipSourceField");

describe("NlbVipSourceField helpers", () => {
  it("effectiveVipMode — нормализует режим под схему", () => {
    // {subnet, address}, default subnet.
    expect(effectiveVipMode("INTERNAL", undefined)).toBe("subnet");
    expect(effectiveVipMode("INTERNAL", "public")).toBe("subnet"); // невалидный → default
    expect(effectiveVipMode("INTERNAL", "address")).toBe("address");
    // EXTERNAL: {public, address}, default public.
    expect(effectiveVipMode("EXTERNAL", undefined)).toBe("public");
    expect(effectiveVipMode("EXTERNAL", "subnet")).toBe("public"); // невалидный → default
    expect(effectiveVipMode("EXTERNAL", "address")).toBe("address");
    // «Не задавать» законен при ОБЕИХ схемах: это отказ от семейства, а не
    // источник, поэтому схлопываться ему не во что.
    expect(effectiveVipMode("EXTERNAL", "off")).toBe("off");
    expect(effectiveVipMode("INTERNAL", "off")).toBe("off");
  });

  it("buildVipSourceOrNull — пустое значение семейства → null (не шлём пустой id)", () => {
    // Задано значение → ровно один кейс oneof.
    expect(buildVipSourceOrNull("INTERNAL", "subnet", { subnet_id: "sub-1" })).toEqual({ subnet_id: "sub-1" });
    expect(buildVipSourceOrNull("INTERNAL", "address", { address_id: "adr-1" })).toEqual({ address_id: "adr-1" });
    // Устаревший режим схлопывается в валидный дефолт схемы.
    expect(buildVipSourceOrNull("EXTERNAL", "subnet", {})).toEqual({ public: {} });
    // Пустой выбор → null (семейство опускается, а не уходит как {address_id:""}).
    expect(buildVipSourceOrNull("INTERNAL", "address", { address_id: "" })).toBeNull();
    expect(buildVipSourceOrNull("INTERNAL", "subnet", { subnet_id: "" })).toBeNull();
    expect(buildVipSourceOrNull("INTERNAL", "address", undefined)).toBeNull();
    // public всегда валиден (VIP выделяет платформа).
    expect(buildVipSourceOrNull("EXTERNAL", "public", {})).toEqual({ public: {} });
  });

  it("режим «не задавать» опускает семейство при ЛЮБОЙ схеме", () => {
    // Ровно тот исход, которого форме не хватало: у внешней схемы «публичный»
    // источник даёт всегда, поэтому без явного отказа оба семейства уезжали на
    // провод и балансировщик только на IPv4 был невыразим.
    expect(buildVipSourceOrNull("EXTERNAL", "off", {})).toBeNull();
    expect(buildVipSourceOrNull("INTERNAL", "off", {})).toBeNull();
    // Отказ сильнее оставшегося в виджете значения — иначе черновик, набранный
    // до отказа, продолжал бы уезжать телом.
    expect(buildVipSourceOrNull("INTERNAL", "off", { subnet_id: "sub-1", address_id: "adr-1" })).toBeNull();
    // (+) положительный контроль: без него «off → null» могло бы означать
    // «null при любом режиме».
    expect(buildVipSourceOrNull("EXTERNAL", "public", {})).toEqual({ public: {} });
  });

  it("familyIpVersion — семейство → enum IpVersion", () => {
    expect(familyIpVersion("v4")).toBe("IPV4");
    expect(familyIpVersion("v6")).toBe("IPV6");
  });

  it("subnetPlacementMatches — legacy без placement = ZONAL", () => {
    const zonal = subnetPlacementMatches("ZONAL", "ru-central1");
    expect(zonal({ placement_type: "ZONAL", region_id: "ru-central1" })).toBe(true);
    // legacy → ZONAL: размещение не объявлено, но регион известен.
    expect(zonal({ region_id: "ru-central1" })).toBe(true);
    expect(zonal({ placement_type: "REGIONAL", region_id: "ru-central1" })).toBe(false);
  });

  it("subnetPlacementMatches — чужой регион отвергается, и своего мало не бывает", () => {
    // Требование региона — ОТДЕЛЬНОЕ от трактовки размещения, и утверждается
    // отдельно: слитые в один кейс, они дали бы зелёное на любой из двух
    // причин отказа, и понять, какая сработала, было бы нельзя.
    const zonal = subnetPlacementMatches("ZONAL", "ru-central1");
    expect(zonal({ placement_type: "ZONAL", region_id: "ru-central2" })).toBe(false);
    // Регион у строки не пришёл — подтвердить совпадение нечем, и догадка здесь
    // означала бы связать ресурсы разных регионов.
    expect(zonal({ placement_type: "ZONAL" })).toBe(false);
    // Регион не выбран в форме — выбирать не из чего вовсе.
    expect(subnetPlacementMatches("ZONAL")({ placement_type: "ZONAL", region_id: "ru-central1" })).toBe(false);
  });

  it("linkAddressFilter — сфера + семейство", () => {
    const intV4 = linkAddressFilter("INTERNAL", "v4");
    expect(intV4({ internal_ipv4_address: { address: "10.0.0.1" } })).toBe(true);
    expect(intV4({ external_ipv4_address: { address: "1.1.1.1" } })).toBe(false);
    const extV6 = linkAddressFilter("EXTERNAL", "v6");
    expect(extV6({ external_ipv6_address: { address: "2001:db8::1" } })).toBe(true);
    expect(extV6({ internal_ipv6_address: { address: "fd00::1" } })).toBe(false);
  });
});
