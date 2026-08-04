// Приписка строки поиска по системе читает поля ответа НАПРЯМУЮ, по имени. Имя,
// снятое с контракта, читается как `undefined` — без ошибки сборки и без отказа
// края: строка находится, а приписка молча пуста, и это неотличимо от «у ресурса
// этих данных нет».
//
// Тот же класс уже чинился в подписи RefSelect (`refOptionLabel`, пул адресов).
// Здесь — второй его экземпляр, в другом файле того же пакета: слитное
// `cidr_blocks` (тег 7) у AddressPool ЗАРЕЗЕРВИРОВАНО и расщеплено на
// `v4_cidr_blocks` (13) + `v6_cidr_blocks` (14), см.
// proto/kacho/cloud/vpc/v1/internal_address_pool_service.proto.

import { extractExtras } from "./SystemSearchPage";

describe("приписка поиска читает живые имена полей", () => {
  it("пул адресов: диапазоны берутся из разделённой пары v4/v6", () => {
    expect(
      extractExtras("address-pools", {
        zone_id: "ru-central1-a",
        kind: "EXTERNAL_PUBLIC",
        v4_cidr_blocks: ["10.0.0.0/24"],
        v6_cidr_blocks: ["2001:db8::/64"],
      }),
    ).toEqual({ zone: "ru-central1-a", kind: "EXTERNAL_PUBLIC", cidrs: "10.0.0.0/24,2001:db8::/64" });
  });

  it("пул адресов: одна семья — приписка всё равно есть", () => {
    expect(extractExtras("address-pools", { v4_cidr_blocks: ["10.1.0.0/16"] })).toEqual({ cidrs: "10.1.0.0/16" });
    expect(extractExtras("address-pools", { v6_cidr_blocks: ["2001:db8::/32"] })).toEqual({ cidrs: "2001:db8::/32" });
  });

  it("снятое слитное имя приписки НЕ даёт — иначе оно вернулось бы незаметно", () => {
    expect(extractExtras("address-pools", { cidr_blocks: ["10.0.0.0/24"] })).toEqual({});
  });

  it("пул без диапазонов — приписки диапазонов нет, а не пустая строка", () => {
    expect(extractExtras("address-pools", { zone_id: "ru-central1-a" })).toEqual({ zone: "ru-central1-a" });
  });

  // Положительный контроль соседней ветви: утверждение выше отрицательное, и без
  // него «всё пусто» тоже прошло бы.
  it("подсеть: первичный диапазон и дополнительные — по своим именам", () => {
    expect(
      extractExtras("subnets", { zone_id: "ru-central1-a", ipv4_cidr_primary: "10.0.0.0/24" }),
    ).toEqual({ zone: "ru-central1-a", cidrs: "10.0.0.0/24" });
  });
});
