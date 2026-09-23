// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { requestStepUp } from "@shared/api/step-up";
import { installLane, refusal } from "@shared/test/lane-fake";

// Окно повышения ведёт церемонию НАШИМ глаголом (`POST /iam/v1/auth/step-up`),
// не покидая консоли, и отпускает отвергнутый запрос только тогда, когда
// служба подтвердила предъявление (приёмка F8, S2).

const { StepUpModal } = await import("./StepUpModal");

const CEREMONY = {
  status: 200,
  body: {
    session: { expiresAt: "2026-09-24T00:00:00Z", assuranceLevel: "2", emailVerified: false },
    assurance: { level: "2", level2Reachable: true, missingForLevel2: [] },
  },
};

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

const dialog = () => screen.getByRole("dialog");

describe("окно повышения", () => {
  it("по вызову края (уровень 2) предлагает ТОЛЬКО второй фактор и зовёт наш глагол", async () => {
    lane = installLane({ "POST /iam/v1/auth/step-up": CEREMONY });
    render(<StepUpModal />);
    let granted: boolean | null = null;
    act(() => {
      void requestStepUp("2").then((ok) => {
        granted = ok;
      });
    });
    await screen.findByRole("dialog");
    expect(screen.queryByLabelText("Паролем")).toBeNull();
    fireEvent.click(screen.getByLabelText("Запасной код"));
    fireEvent.change(screen.getByLabelText("Код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(granted).toBe(true));
    expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toEqual({
      method: "lookup_secret",
      code: "ABCDEFGHJK",
      csrfToken: "tok-step-up-1",
    });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(lane.calls.every((c) => c.path.startsWith("/iam/v1/auth/"))).toBe(true);
  });

  it("на просьбу свежести годится и пароль — ветвь пароля по умолчанию", async () => {
    lane = installLane({ "POST /iam/v1/auth/step-up": CEREMONY });
    render(<StepUpModal />);
    let granted: boolean | null = null;
    act(() => {
      void requestStepUp().then((ok) => {
        granted = ok;
      });
    });
    await screen.findByRole("dialog");
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить" }));
    await waitFor(() => expect(granted).toBe(true));
    expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toEqual({
      method: "password",
      password: "secret",
      csrfToken: "tok-step-up-1",
    });
  });

  it("F8-36 · второго фактора нет: текст службы и путь к заведению, окно не закрывается молча", async () => {
    lane = installLane({
      "POST /iam/v1/auth/step-up": refusal(400, 9, "second factor is not enrolled", "SECOND_FACTOR_NOT_ENROLLED"),
    });
    render(<StepUpModal />);
    let settled = false;
    act(() => {
      void requestStepUp("2").then(() => {
        settled = true;
      });
    });
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByLabelText("Запасной код"));
    fireEvent.change(screen.getByLabelText("Код"), { target: { value: "ABCDEFGHJK" } });
    fireEvent.click(screen.getByRole("button", { name: "Подтвердить" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("second factor is not enrolled");
    const link = screen.getByRole<HTMLAnchorElement>("link", { name: "Настроить второй фактор" });
    expect(new URL(link.href).pathname).toBe("/settings");
    expect(dialog()).toBeInTheDocument();
    // Обещание не разрешено: действие без поручительства не повторяется.
    expect(settled).toBe(false);
  });

  it("отмена отвергает просьбу — вызывающий отдаёт исходный отказ как есть", async () => {
    lane = installLane({});
    render(<StepUpModal />);
    let granted: boolean | null = null;
    act(() => {
      void requestStepUp("2").then((ok) => {
        granted = ok;
      });
    });
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Отменить" }));
    await waitFor(() => expect(granted).toBe(false));
    expect(lane.of("POST", "/iam/v1/auth/step-up")).toHaveLength(0);
  });

  it("снятое окно на просьбы не отвечает — мёртвое окно не поручится за живой запрос", async () => {
    const { unmount } = render(<StepUpModal />);
    unmount();
    expect(await requestStepUp("2")).toBe(false);
  });
});
