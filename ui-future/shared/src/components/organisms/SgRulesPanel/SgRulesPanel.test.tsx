// Правила группы безопасности. Каждая операция уходит на край ОДНИМ вызовом
// правки набора, и форма тела здесь решающая: правка — это «снять по id и
// добавить заново», поэтому потерянный `deletion_rule_ids` молча ДВОИТ правило,
// а лишний — снимает чужое. Отдельный предмет — пустой набор: он означает
// «трафик заблокирован», и сказать это обязано само окно.
//
// `antd` переопределён локально: общий заменитель рисует `Dropdown` пустым
// узлом (пункты меню недостижимы) и не даёт добраться до подтверждения
// удаления — на нём проба зеленела бы при любом составе меню.

import { jest } from "@jest/globals";
import React from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@shared/api/client";
import { antdStub } from "@shared/test/antd-stub";

interface MenuItem {
  key?: string;
  label?: React.ReactNode;
  type?: string;
  disabled?: boolean;
  onClick?: () => void;
}

interface ConfirmConfig {
  title?: React.ReactNode;
  content?: React.ReactNode;
  onOk?: () => unknown;
}

const confirms: ConfirmConfig[] = [];

jest.unstable_mockModule("antd", () => {
  const base = antdStub();
  const ModalRoot = base.Modal as unknown as React.FC<{ open?: boolean; children?: React.ReactNode }>;
  return {
    ...base,
    Modal: Object.assign(ModalRoot, {
      confirm: (cfg: ConfirmConfig) => {
        confirms.push(cfg);
      },
      destroyAll: () => {},
    }),
    Dropdown: ({ children, menu }: React.PropsWithChildren<{ menu?: { items?: MenuItem[] } }>) =>
      React.createElement(
        "span",
        null,
        children,
        (menu?.items ?? [])
          .filter((it) => it.type !== "divider")
          .map((it, i) =>
            React.createElement(
              "button",
              { key: it.key ?? i, type: "button", disabled: it.disabled, onClick: () => it.onClick?.() },
              it.label,
            ),
          ),
      ),
  };
});

// Слот шапки — портал: вне карточки ресурса он не рисует НИЧЕГО, и действия
// панели были бы недостижимы. Заменитель рисует их на месте — это тот же
// контракт «показать в шапке», выполненный доступным пробе способом.
jest.unstable_mockModule("@shared/components/organisms/DetailShell", () => ({
  HeaderSlotPortal: ({ children }: React.PropsWithChildren) =>
    React.createElement("div", { "data-testid": "header-slot" }, children),
}));

const update = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const toastError = jest.fn();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { update, get: jest.fn(), list: jest.fn(), create: jest.fn(), delete: jest.fn(), action: jest.fn() },
  ApiError,
}));

jest.unstable_mockModule("@shared/lib/toast", () => ({
  toast: { error: toastError, success: jest.fn(), info: jest.fn(), loading: jest.fn(), dismiss: jest.fn() },
}));

const { SgRulesPanel } = await import("./SgRulesPanel");
type Rule = Parameters<typeof SgRulesPanel>[0]["rules"][number];

const RULES: Rule[] = [
  {
    id: "sgr-1",
    direction: "INGRESS",
    protocol_name: "TCP",
    ports: { from_port: 80, to_port: 80 },
    cidr_blocks: { v4_cidr_blocks: ["0.0.0.0/0"] },
    description: "http",
  },
  {
    id: "sgr-2",
    direction: "EGRESS",
    protocol_number: 47,
    security_group_id: "sg-9",
  },
];

function show(rules: Rule[] = RULES) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SgRulesPanel sgId="sg-1" projectId="prj-1" rules={rules} networkId="net-1" />
    </QueryClientProvider>,
  );
}

const rowOf = (text: string) => screen.getByText(text).closest("tr")!;
/** Действия панели живут в шапке; в строках есть одноимённый пункт меню. */
const headerAction = (name: RegExp | string) => within(screen.getByTestId("header-slot")).getByRole("button", { name });
const boxes = () => screen.getAllByRole("checkbox");

beforeEach(() => {
  jest.clearAllMocks();
  confirms.length = 0;
  update.mockResolvedValue({});
});

describe("SgRulesPanel — список", () => {
  it("пустой набор объясняет, что это запрет, а не «ничего не настроено»", () => {
    show([]);

    expect(screen.getByText("Правил нет — трафик блокируется (default-deny).")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("направление показано словом, а не машинным значением", () => {
    show();

    expect(screen.getByText("Входящий")).toBeInTheDocument();
    expect(screen.getByText("Исходящий")).toBeInTheDocument();
    expect(screen.queryByText("INGRESS")).not.toBeInTheDocument();
  });

  it("протокол по номеру подписан номером, а не пустотой", () => {
    show();

    expect(screen.getByText("proto 47")).toBeInTheDocument();
  });

  it("правило без портов и без описания показывает прочерки, а не «undefined»", () => {
    show();

    const cells = [...rowOf("proto 47").querySelectorAll("td")].map((td) => td.textContent);
    expect(cells[3]).toBe("—");
    expect(cells[6]).toBe("—");
  });

  it("источник назван вместе с его типом", () => {
    show();

    expect(within(rowOf("http")).getByText("CIDR")).toBeInTheDocument();
    expect(within(rowOf("http")).getByText("0.0.0.0/0")).toBeInTheDocument();
    expect(within(rowOf("proto 47")).getByText("SG")).toBeInTheDocument();
    expect(within(rowOf("proto 47")).getByText("sg-9")).toBeInTheDocument();
  });

  it("массовое удаление закрыто, пока ничего не выбрано", () => {
    show();

    expect(headerAction(/^Удалить$/)).toBeDisabled();
  });

  it("выбор считается в подписи кнопки", () => {
    show();

    fireEvent.click(boxes()[1]);

    expect(headerAction("Удалить (1)")).toBeEnabled();
  });

  it("выбор всех выбирает все правила разом", () => {
    show();

    fireEvent.click(boxes()[0]);

    expect(headerAction("Удалить (2)")).toBeInTheDocument();
  });
});

describe("SgRulesPanel — удаление", () => {
  it("удаление спрашивает подтверждение и до него на край не ходит", () => {
    show();

    fireEvent.click(boxes()[1]);
    fireEvent.click(headerAction("Удалить (1)"));

    expect(confirms).toHaveLength(1);
    expect(confirms[0].title).toBe("Удалить выбранные правила (1)");
    expect(update).not.toHaveBeenCalled();
  });

  it("подтверждённое массовое удаление снимает ровно выбранные id", async () => {
    show();

    fireEvent.click(boxes()[1]);
    fireEvent.click(headerAction("Удалить (1)"));
    await confirms[0].onOk!();

    expect(update).toHaveBeenCalledWith("/vpc/v1/securityGroups/sg-1/rules", { deletion_rule_ids: ["sgr-1"] });
  });

  it("удаление одного правила из его меню снимает только его", async () => {
    show();

    fireEvent.click(within(rowOf("proto 47")).getByRole("button", { name: "Удалить" }));
    expect(confirms[0].title).toBe("Удалить правило");
    await confirms[0].onOk!();

    expect(update).toHaveBeenCalledWith("/vpc/v1/securityGroups/sg-1/rules", { deletion_rule_ids: ["sgr-2"] });
  });

  it("отказ края показан кодом и текстом", async () => {
    update.mockRejectedValue(new ApiError(400, "FAILED_PRECONDITION", null, "rule is referenced"));
    show();

    fireEvent.click(within(rowOf("proto 47")).getByRole("button", { name: "Удалить" }));
    await confirms[0].onOk!();

    await waitFor(() =>
      expect(toastError).toHaveBeenCalledWith("Правило группы безопасности: FAILED_PRECONDITION: rule is referenced"),
    );
  });
});

describe("SgRulesPanel — правка и добавление", () => {
  it("добавление открывает пустое правило, а не правку существующего", () => {
    show();

    fireEvent.click(headerAction(/Добавить правило/));

    expect(screen.getByText("Новое правило")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("новое правило уходит одним добавлением, без снятия чужого", async () => {
    show();

    fireEvent.click(headerAction(/Добавить правило/));
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    const body = update.mock.calls[0][1] as Record<string, unknown>;
    expect(body).not.toHaveProperty("deletion_rule_ids");
    expect((body.addition_rule_specs as unknown[]).length).toBe(1);
  });

  it("правка существующего снимает ЕГО и добавляет заново — иначе правило раздвоится", async () => {
    show();

    fireEvent.click(within(rowOf("http")).getByRole("button", { name: "Редактировать" }));
    expect(screen.getByText("Редактирование правила")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    const body = update.mock.calls[0][1] as Record<string, unknown>;
    expect(body.deletion_rule_ids).toEqual(["sgr-1"]);
    expect((body.addition_rule_specs as Record<string, unknown>[])[0]).not.toHaveProperty("id");
  });

  it("отмена правки возвращает список и на край ничего не шлёт", () => {
    show();

    fireEvent.click(headerAction(/Добавить правило/));
    fireEvent.click(screen.getByRole("button", { name: "Отменить" }));

    expect(update).not.toHaveBeenCalled();
    expect(screen.getByText("Входящий")).toBeInTheDocument();
  });
});
