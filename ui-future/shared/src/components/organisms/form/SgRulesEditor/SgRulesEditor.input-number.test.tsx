// Диапазон портов правила — ЧИСЛОВЫМИ полями, и число доезжает до правила.
//
// ЧТО ИМЕННО ДЕРЖИТСЯ. Настоящий `InputNumber` зовёт `onChange` с ЧИСЛОМ (или
// `null` на пустоте), а не с событием DOM. Продукт на это и рассчитывает:
// `onChange={(v) => set({ from_port: v === null ? undefined : Number(v) })}`.
// Заменитель, отдающий событие, сделал бы весь путь «ввёл порт → правило его
// получило» недостижимым: `Number(<событие>)` — это `NaN`, а `NaN` в правиле
// неотличим от незаданного порта ровно до разбора отказа доступа.
//
// А проходной <div> не даёт даже поля: вводить некуда, и утверждение о том,
// какой порт в правиле, зеленело бы на форме без единого числового поля.
// Поэтому ниже — и положительный контроль присутствия полей, и утверждение о
// том, ЧТО именно в правиле оказалось.
//
// Инъекция в обе стороны: `InputNumber: Component` в заменителе — красное;
// заменитель на месте — молчание.
//
// Поля ищутся по типу поля ввода, а не по подписи: подпись `Form.Item` с полем
// не связана (`htmlFor` настоящий antd здесь не проставляет), и доступного
// имени у числового поля нет ни у настоящего, ни у заменителя.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";

jest.unstable_mockModule("antd", () => antdStub());

const { SgRulesEditor } = await import("./SgRulesEditor");

interface Rule {
  direction?: string;
  protocol_name?: string;
  ports?: { from_port?: number; to_port?: number };
  cidr_blocks?: { v4_cidr_blocks?: string[] };
  _target_kind?: string;
  _protocol_mode?: string;
  _ports_any?: boolean;
}

function show(rules: Rule[]) {
  let current: Record<string, unknown> = { rules };
  render(
    <SgRulesEditor
      pathPrefix=""
      value={{ rules }}
      onChange={(next: Record<string, unknown>) => {
        current = next;
      }}
      path="rules"
    />,
  );
  return { latest: () => (current.rules as Rule[]) ?? [] };
}

const ingress = (from: number, to: number): Rule => ({
  direction: "INGRESS",
  _protocol_mode: "name",
  protocol_name: "TCP",
  ports: { from_port: from, to_port: to },
  _target_kind: "cidr",
  cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
});

/** Раскрыть единственную панель правила: поля лежат внутри неё. */
function openTheRule() {
  fireEvent.click(screen.getAllByRole("button", { expanded: false })[0]);
}

const numberFields = () => Array.from(document.querySelectorAll<HTMLInputElement>('input[type="number"]'));

describe("SgRulesEditor — диапазон портов числовыми полями", () => {
  it("раскрытое правило показывает ДВА числовых поля с текущими границами", () => {
    // Положительный контроль: без него утверждения ниже проходили бы на форме,
    // где вводить нечего вовсе.
    show([ingress(80, 80)]);
    openTheRule();

    const fields = numberFields();
    expect(fields).toHaveLength(2);
    expect(fields.map((f) => f.value)).toEqual(["80", "80"]);
  });

  it("введённая верхняя граница попадает в правило ЧИСЛОМ, а не событием", () => {
    const { latest } = show([ingress(80, 80)]);
    openTheRule();

    fireEvent.change(numberFields()[1], { target: { value: "443" } });

    const ports = latest()[0]?.ports;
    expect(ports?.to_port).toBe(443);
    // Именно число: `Number(<событие>)` дал бы NaN, и правило выглядело бы
    // заданным, не будучи им.
    expect(Number.isNaN(ports?.to_port as number)).toBe(false);
    // Соседняя граница не задета — правится та ячейка, в которой печатают.
    expect(ports?.from_port).toBe(80);
  });

  it("очищенное поле снимает границу, а не подставляет ноль — контроль в обратную сторону", () => {
    const { latest } = show([ingress(80, 443)]);
    openTheRule();

    fireEvent.change(numberFields()[0], { target: { value: "" } });

    expect(latest()[0]?.ports?.from_port).toBeUndefined();
  });
});
