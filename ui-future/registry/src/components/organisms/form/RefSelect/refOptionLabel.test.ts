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
// vpc SecurityGroup, message CidrBlocks (security_group.proto:121). Поэтому
// запрет ниже сформулирован не по имени вообще, а по чтению этого имени С
// СТРОКИ РЕСУРСА (`row.<имя>`): вложенное `cidr_blocks.v4_cidr_blocks` правила
// не нарушает и гейт молчит.

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { refOptionExtra, refOptionHead } from "./refOptionLabel";

const COMPONENT = "src/components/organisms/form/RefSelect/RefSelect.tsx";

/** Исходник компонента без комментариев — гейт судит код, а не прозу о нём. */
function refSelectCode(): string {
  const raw = readFileSync(join(process.cwd(), COMPONENT), "utf8");
  return raw.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

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

  it("имя ресурса берётся из name, а у пользователя — из display_name", () => {
    expect(refOptionHead("subnets", { name: "sub-a" })).toBe("sub-a");
    expect(refOptionHead("users", { display_name: "Иван", email: "i@example.org" })).toBe("Иван");
  });

  it("компонент не читает снятые имена подсети со строки ресурса", () => {
    const code = refSelectCode();
    expect(code).not.toMatch(/row\.v4_cidr_blocks/);
    expect(code).not.toMatch(/row\.v6_cidr_blocks/);
  });
});
