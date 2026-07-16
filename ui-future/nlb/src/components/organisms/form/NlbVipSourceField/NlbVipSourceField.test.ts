import { jest } from "@jest/globals";

// NlbVipSourceField тянет RefSelect → весь form-движок; для чистого логического
// теста хелперов мокаем RefSelect (в этом тесте компонент не рендерится).
jest.unstable_mockModule("@/components/organisms/form/RefSelect", () => ({ RefSelect: () => null }));

const {
  effectiveVipMode,
  buildVipSource,
  buildVipSourceOrNull,
  familyIpVersion,
  subnetPlacementMatches,
  linkAddressFilter,
} = await import("./NlbVipSourceField");

describe("NlbVipSourceField helpers", () => {
  it("effectiveVipMode — нормализует режим под схему", () => {
    // INTERNAL: {subnet, address}, default subnet.
    expect(effectiveVipMode("INTERNAL", undefined)).toBe("subnet");
    expect(effectiveVipMode("INTERNAL", "public")).toBe("subnet"); // невалидный → default
    expect(effectiveVipMode("INTERNAL", "address")).toBe("address");
    // EXTERNAL: {public, address}, default public.
    expect(effectiveVipMode("EXTERNAL", undefined)).toBe("public");
    expect(effectiveVipMode("EXTERNAL", "subnet")).toBe("public"); // невалидный → default
    expect(effectiveVipMode("EXTERNAL", "address")).toBe("address");
  });

  it("buildVipSource — ровно один кейс oneof на семейство", () => {
    expect(buildVipSource("INTERNAL", "subnet", { subnet_id: "sub-1" })).toEqual({ subnet_id: "sub-1" });
    expect(buildVipSource("INTERNAL", "address", { address_id: "adr-1" })).toEqual({ address_id: "adr-1" });
    expect(buildVipSource("EXTERNAL", "public", {})).toEqual({ public: {} });
    // Устаревший режим схлопывается в валидный дефолт схемы.
    expect(buildVipSource("EXTERNAL", "subnet", {})).toEqual({ public: {} });
  });

  it("buildVipSourceOrNull — пустое значение семейства → null (не шлём пустой id)", () => {
    // Задано значение → oneof, как buildVipSource.
    expect(buildVipSourceOrNull("INTERNAL", "subnet", { subnet_id: "sub-1" })).toEqual({ subnet_id: "sub-1" });
    expect(buildVipSourceOrNull("INTERNAL", "address", { address_id: "adr-1" })).toEqual({ address_id: "adr-1" });
    // Пустой выбор → null (семейство опускается, а не уходит как {address_id:""}).
    expect(buildVipSourceOrNull("INTERNAL", "address", { address_id: "" })).toBeNull();
    expect(buildVipSourceOrNull("INTERNAL", "subnet", { subnet_id: "" })).toBeNull();
    expect(buildVipSourceOrNull("INTERNAL", "address", undefined)).toBeNull();
    // public всегда валиден (VIP выделяет платформа).
    expect(buildVipSourceOrNull("EXTERNAL", "public", {})).toEqual({ public: {} });
  });

  it("familyIpVersion — семейство → enum IpVersion", () => {
    expect(familyIpVersion("v4")).toBe("IPV4");
    expect(familyIpVersion("v6")).toBe("IPV6");
  });

  it("subnetPlacementMatches — legacy без placement = ZONAL", () => {
    const zonal = subnetPlacementMatches("ZONAL");
    expect(zonal({ placement_type: "ZONAL" })).toBe(true);
    expect(zonal({})).toBe(true); // legacy → ZONAL
    expect(zonal({ placement_type: "REGIONAL" })).toBe(false);
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
