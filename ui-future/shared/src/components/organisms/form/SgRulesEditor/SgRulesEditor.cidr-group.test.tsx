// Цель правила группы безопасности — ВЕТВЬ `oneof target`, и ветвей в стволе
// три: набор блоков, другая группа безопасности и НАБОР ПРЕФИКСОВ
// (`cidr_group_id`, tag 11). Редактор знал две из трёх, поэтому третья была
// невыразима из консоли вовсе: правило, ради которого набор префиксов и заведён
// (список правится один раз, а не копируется в двадцать правил), нельзя было ни
// создать, ни увидеть.
//
// Набор целей сужен ТИПОМ, а не только списком опций: непредставимый выбор
// нельзя отправить, тогда как скрытый — можно, придя из уже сохранённого
// правила. Поэтому проба утверждает обе стороны — что выбор ЕСТЬ в форме и что
// вычистка тела оставляет ровно выбранную ветвь.
//
// Ground truth: proto/kacho/cloud/vpc/v1/security_group.proto,
// `oneof target { cidr_blocks = 8; security_group_id = 9; cidr_group_id = 11; }`
// с `option (exactly_one) = true` — правило без цели и правило с двумя целями
// сервис отвергает.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { RuleBody } = await import("./SgRulesEditor");
type RuleExt = Parameters<typeof RuleBody>[0]["rule"];
const { sanitizeSgRule } = await import("@shared/lib/resource-registry");


function show(rule: RuleExt) {
  const onChange = jest.fn<(next: RuleExt) => void>();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/vpc/security-groups/sg-1"]}>
        <RuleBody rule={rule} onChange={onChange} editingNetworkId="net-1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return onChange;
}

/** Селект «Источник» — тот, среди опций которого есть «CIDR-блоки». */
function targetSelect(): HTMLSelectElement {
  const sel = [...document.querySelectorAll("select")].find((s) =>
    [...s.options].some((o) => o.textContent === "CIDR-блоки"),
  );
  if (!sel) throw new Error("селект выбора цели правила не найден");
  return sel as HTMLSelectElement;
}

describe("цель правила — три ветви, а не две", () => {
  it("форма предлагает набор префиксов наравне с блоками и группой", () => {
    show({ direction: "INGRESS", _target_kind: "cidr", cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] } });

    const values = [...targetSelect().options].map((o) => o.value).filter((v) => v !== "");
    expect(values).toEqual(["cidr", "sg", "cidr-group"]);
  });

  it("выбор набора префиксов гасит обе прежние ветви", () => {
    const onChange = show({
      direction: "INGRESS",
      _target_kind: "cidr",
      cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
    });

    fireEvent.change(targetSelect(), { target: { value: "cidr-group" } });

    const next = onChange.mock.calls[0][0];
    expect(next._target_kind).toBe("cidr-group");
    expect(next.cidr_blocks).toBeUndefined();
    expect(next.security_group_id).toBeUndefined();
    expect(next).toHaveProperty("cidr_group_id");
  });

  it("выбор ветви ссылки на набор предлагается СПИСКОМ, а не свободной строкой", () => {
    show({ direction: "INGRESS", _target_kind: "cidr-group", cidr_group_id: "" });

    // `RefSelect` рисует настоящий список вариантов (в заменителе antd —
    // `<select>` с подписью-плейсхолдером первой опцией); свободный ввод означал
    // бы, что оператор обязан знать идентификатор набора наизусть.
    expect(screen.getByText("Выберите набор префиксов")).toBeInTheDocument();
    // Контроль в обратную сторону: у ветви ссылки на группу безопасности без
    // контекста сети редактор ДЕЙСТВИТЕЛЬНО деградирует до свободного ввода, и
    // проба обязана эти два случая различать.
    expect(screen.queryByPlaceholderText("UUID другой SG")).not.toBeInTheDocument();
  });

  it("вычистка тела оставляет ровно выбранную ветвь", () => {
    const out = sanitizeSgRule({
      direction: "INGRESS",
      _target_kind: "cidr-group",
      cidr_group_id: "cdg-1",
      cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
      security_group_id: "sg-9",
    });

    expect(out.cidr_group_id).toBe("cdg-1");
    expect(out).not.toHaveProperty("cidr_blocks");
    expect(out).not.toHaveProperty("security_group_id");
    expect(out).not.toHaveProperty("_target_kind");
  });

  it("прежние две ветви вычищаются так же — контроль в обратную сторону", () => {
    // Без него утверждение выше зеленело бы на вычистке, снимающей ссылку на
    // набор всегда.
    const asSg = sanitizeSgRule({ _target_kind: "sg", security_group_id: "sg-9", cidr_group_id: "cdg-1" });
    expect(asSg.security_group_id).toBe("sg-9");
    expect(asSg).not.toHaveProperty("cidr_group_id");

    const asCidr = sanitizeSgRule({
      _target_kind: "cidr",
      cidr_blocks: { v4_cidr_blocks: ["10.0.0.0/8"] },
      cidr_group_id: "cdg-1",
    });
    expect(asCidr.cidr_blocks).toEqual({ v4_cidr_blocks: ["10.0.0.0/8"] });
    expect(asCidr).not.toHaveProperty("cidr_group_id");
  });

  it("ветвь выводится из сохранённого правила, когда дискриминатора формы нет", () => {
    // Правило приезжает с сервера без служебных ключей формы; без вывода по
    // заполненной ветви редактор показал бы «CIDR-блоки» на правиле, которое
    // ссылается на набор, и первое же сохранение сменило бы цель.
    const out = sanitizeSgRule({ cidr_group_id: "cdg-1" });
    expect(out.cidr_group_id).toBe("cdg-1");
  });
});
