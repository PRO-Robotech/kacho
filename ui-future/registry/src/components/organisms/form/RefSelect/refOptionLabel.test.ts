// Подписи опций RefSelect — сверка с контрактом ствола, а не с прежней формой.
//
// Ground truth: proto/kacho/cloud/vpc/v1/subnet.proto. Subnet несёт
// `ipv4_cidr_primary` (неизменяемый первичный якорь) + `ipv4_cidr_blocks`
// (дополнительные диапазоны) и симметричную пару для v6. Прежние имена
// `v4_cidr_blocks` / `v6_cidr_blocks` подсети больше не принадлежат — чтение
// такого имени возвращает undefined БЕЗ ошибки, поэтому подпись опции просто
// становится пустой, и выбирающий подсеть человек не отличает одну от другой.
//
// Отдельно: `v4_cidr_blocks` остаётся ЖИВЫМ именем в другом месте контракта —
// у пула адресов (AddressPool, поля 13/14). Поэтому запрет сформулирован не по
// имени вообще, а по ПОВЕДЕНИЮ: строка в снятой форме подписи не даёт, строка
// в форме ствола — даёт, а пул по тем же именам подписывается по-прежнему.
//
// Прежняя редакция утверждала это же чтением исходника `RefSelect.tsx` и
// поиском в нём `row.v4_cidr_blocks`. У такого запрета НЕТ ПРОИЗВОДИТЕЛЯ:
// компонент строк ресурса не читает вовсе — он зовёт `refOptionHead`/
// `refOptionExtra` (RefSelect.tsx, единственные два обращения к строке), а те
// живут в другом файле. То есть проверка не могла покраснеть ни на каком
// состоянии дерева. Ниже то же свойство утверждается вызовом функции.

import { refOptionExtra, refOptionHead } from "./refOptionLabel";

describe("подписи опций RefSelect против контракта ствола", () => {
  it("подсеть подписывается первичным якорем и дополнительными диапазонами", () => {
    const extra = refOptionExtra("subnets", {
      ipv4_cidr_primary: "10.0.1.0/24",
      ipv4_cidr_blocks: ["10.0.2.0/24"],
      ipv6_cidr_primary: "",
      ipv6_cidr_blocks: [],
    });
    expect(extra).toContain("10.0.1.0/24");
    expect(extra).toContain("10.0.2.0/24");
  });

  it("подсеть в форме ствола не остаётся без подписи", () => {
    // Положительный контроль отрицания ниже: строка ствола ДОЛЖНА давать текст.
    expect(refOptionExtra("subnets", { ipv4_cidr_primary: "10.0.1.0/24" })).not.toBe("");
  });

  it("подсеть В СНЯТОЙ ФОРМЕ подписи не получает — снятые имена не читаются", () => {
    // Это и есть предмет: строка со старого сервера (или из устаревшей фикстуры)
    // не должна выглядеть подписанной. Пара с утверждением выше обязательна —
    // иначе «пусто» было бы верно и у функции, которая не умеет ничего.
    expect(
      refOptionExtra("subnets", {
        v4_cidr_blocks: ["10.0.2.0/24"],
        v6_cidr_blocks: ["fd00::/64"],
      }),
    ).toBe("");
  });

  it("те же имена у ПУЛА адресов по-прежнему читаются — запрет не по имени, а по месту", () => {
    // Контроль в другую сторону: у AddressPool `v4_cidr_blocks`/`v6_cidr_blocks`
    // живы, и запрет, сформулированный по имени вообще, отверг бы законное поле.
    const extra = refOptionExtra("address-pools", {
      v4_cidr_blocks: ["192.0.2.0/24"],
      v6_cidr_blocks: ["2001:db8::/48"],
      is_default: true,
    });
    expect(extra).toContain("192.0.2.0/24");
    expect(extra).toContain("2001:db8::/48");
    expect(extra).toContain("default");
  });

  it("пустая строка подписи не выдумывает", () => {
    // Граница: отсутствие данных — не повод показать «·» или мусор.
    expect(refOptionExtra("subnets", {})).toBe("");
    expect(refOptionExtra("address-pools", {})).toBe("");
    expect(refOptionExtra("что-то-незнакомое", { name: "x" })).toBe("");
  });

  it("имя ресурса берётся из name, а у пользователя — из display_name", () => {
    expect(refOptionHead("subnets", { name: "sub-a" })).toBe("sub-a");
    expect(refOptionHead("users", { display_name: "Иван", email: "i@example.org" })).toBe("Иван");
  });

  it("у пользователя без имени подпись спускается к почте, затем к идентификатору", () => {
    // Границы цепочки запасных значений: каждая ступень проверяется отдельно,
    // иначе «работает» верно лишь для первой.
    expect(refOptionHead("users", { email: "i@example.org", id: "usr-1" })).toBe("i@example.org");
    expect(refOptionHead("users", { id: "usr-1" })).toBe("usr-1");
    expect(refOptionHead("users", {})).toBe("");
    expect(refOptionHead("subnets", {})).toBe("");
  });
});
