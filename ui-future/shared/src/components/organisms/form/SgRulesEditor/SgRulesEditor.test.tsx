// Редактор правил группы безопасности. Предмет — что человек ВИДИТ и какой
// набор правил получается после его действий:
//
//  1. пустой набор подписан «трафик блокируется»: молчаливая пустота читается
//     как «правила ещё не загрузились», и группу отдают в работу открытой на
//     вид, закрытой на деле (или наоборот — её создают заново);
//  2. быстрый пресет добавляет ИМЕННО то правило, которое обещает подпись.
//     Кнопка «HTTPS», кладущая 80-й порт, — обещание без исполнения, и заметно
//     оно только на разборе отказа доступа;
//  3. удаление снимает ТО правило, на котором нажали, а не последнее: снятие
//     соседа тихо открывает или закрывает не тот трафик;
//  4. сводка по направлениям считает то, что в наборе есть.
//
// `Collapse` общего стенда-заменителя пунктов не рисует: на нём и подпись
// правила, и кнопка удаления были бы недостижимы, а утверждения о них —
// истинными при любом наборе. Здесь он переопределён.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { antdStub } from "@shared/test/antd-stub";

interface PanelItem {
  key: string;
  label?: React.ReactNode;
  extra?: React.ReactNode;
  children?: React.ReactNode;
}

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Collapse: ({ items }: { items?: PanelItem[] }) =>
    React.createElement(
      "ul",
      { "data-testid": "rules" },
      (items ?? []).map((it) =>
        React.createElement(
          "li",
          { key: it.key },
          React.createElement("span", null, it.label as React.ReactNode),
          it.extra as React.ReactNode,
        ),
      ),
    ),
}));

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
  const onChange = jest.fn((next: Record<string, unknown>) => {
    current = next;
  });
  render(<SgRulesEditor pathPrefix="" value={{ rules }} onChange={onChange} path="rules" />);
  return { onChange, latest: () => (current.rules as Rule[]) ?? [] };
}

const rows = () => [...screen.getByTestId("rules").querySelectorAll("li")];
const ingress = (port: number): Rule => ({
  direction: "INGRESS",
  _protocol_mode: "name",
  protocol_name: "TCP",
  ports: { from_port: port, to_port: port },
  _target_kind: "cidr",
  cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
});

describe("SgRulesEditor", () => {
  it("пустой набор подписан запретом, а не пустотой", () => {
    show([]);

    expect(screen.getByText("— пусто, трафик блокируется (default-deny) —")).toBeInTheDocument();
    expect(screen.queryByTestId("rules")).not.toBeInTheDocument();
  });

  it("сводка направлений считает то, что в наборе есть", () => {
    show([ingress(80), { ...ingress(443), direction: "EGRESS" }]);

    expect(screen.getByText("↓ 1 вход.")).toBeInTheDocument();
    expect(screen.getByText("↑ 1 исх.")).toBeInTheDocument();
    expect(rows()).toHaveLength(2);
  });

  it("пресет добавляет ровно то правило, которое обещает подпись", () => {
    const { latest } = show([]);

    fireEvent.click(screen.getByRole("button", { name: "HTTPS" }));

    const added = latest();
    expect(added).toHaveLength(1);
    expect(added[0].direction).toBe("INGRESS");
    expect(added[0].protocol_name).toBe("TCP");
    expect(added[0].ports).toEqual({ from_port: 443, to_port: 443 });
  });

  it("разные пресеты дают разные правила — контроль в обратную сторону", () => {
    // Без него предыдущее утверждение зеленело бы на пресете, кладущем всегда
    // одно и то же.
    const { latest } = show([]);
    fireEvent.click(screen.getByRole("button", { name: "SSH" }));
    expect(latest()[0].ports).toEqual({ from_port: 22, to_port: 22 });
  });

  it("«Добавить правило» кладёт заготовку, а не готовое правило", () => {
    const { latest } = show([]);

    fireEvent.click(screen.getByRole("button", { name: "Добавить правило" }));

    expect(latest()).toHaveLength(1);
    expect(latest()[0].ports).toBeUndefined();
  });

  it("удаление снимает то правило, на котором нажали", () => {
    const { latest } = show([ingress(22), ingress(443)]);

    // Кнопка удаления живёт в дополнении панели — нажимаем ПЕРВУЮ.
    const del = rows()[0].querySelector("button")!;
    fireEvent.click(del);

    const left = latest();
    expect(left).toHaveLength(1);
    // Снятие соседа открыло бы или закрыло не тот трафик, и заметно это было бы
    // только при разборе доступа.
    expect(left[0].ports).toEqual({ from_port: 443, to_port: 443 });
  });

  it("сводка правила называет направление, протокол, порт и цель", () => {
    show([ingress(443)]);

    const text = rows()[0].textContent ?? "";
    expect(text).toContain("INGRESS");
    expect(text).toContain("tcp");
    expect(text).toContain("443");
    expect(text).toContain("0.0.0.0/0");
  });
});
