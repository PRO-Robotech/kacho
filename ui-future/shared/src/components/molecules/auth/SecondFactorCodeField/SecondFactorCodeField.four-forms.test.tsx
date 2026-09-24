// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { requestStepUp } from "@shared/api/step-up";
import { SESSION, SIGNED_IN, installLane, type LaneAnswer } from "@shared/test/lane-fake";

// Ветвь «код из приложения» ЧЕТЫРЁХ форм, принимающих код (приёмка F8, Р9 —
// цена средства оси 4; F8-30, F8-31, F8-33, F8-35).
//
// После посева браузер предъявляет второй фактор только запасным кодом: код по
// времени дважды одним человеком не предъявляется. Поэтому ветвь кода из
// приложения в браузере не проходит, и держит её ЭТА проба: выбор «Код из
// приложения» кладёт в тело `method` = `totp`, выбор «Запасной код» —
// `lookup_secret`, у каждой из четырёх форм и в обе стороны — иначе отрицание
// тождественно.

const { LoginPage } = await import("@shared/pages/auth/LoginPage");
const { AccountSettingsPage } = await import("@shared/pages/auth/AccountSettingsPage");
const { StepUpModal } = await import("@shared/components/molecules/auth/StepUpModal");

const CEREMONY: LaneAnswer = {
  status: 200,
  body: { session: SESSION, assurance: { level: "2", level2Reachable: true, missingForLevel2: [] } },
};
const ENROLLED: LaneAnswer = {
  status: 200,
  body: { totp: { enrolled: true, confirmedAt: "x" }, backupCodes: { remaining: 10, total: 10 } },
};

const CHOICES = [
  { label: "Код из приложения", code: "123456", method: "totp" },
  { label: "Запасной код", code: "ABCDEFGHJK", method: "lookup_secret" },
] as const;

let lane: ReturnType<typeof installLane> | null = null;
afterEach(() => lane?.restore());

describe("четыре формы предъявления кода называют способ выбором человека", () => {
  for (const choice of CHOICES) {
    it(`F8-33 · вход: «${choice.label}» кладёт secondFactor.method = ${choice.method}`, async () => {
      lane = installLane({ "POST /iam/v1/auth/login": SIGNED_IN });
      render(
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage leave={() => undefined} />
        </MemoryRouter>,
      );
      await screen.findByRole("form", { name: "Вход в консоль" });
      fireEvent.click(screen.getByLabelText("Подтвердить вторым фактором"));
      fireEvent.click(screen.getByLabelText(choice.label));
      fireEvent.change(screen.getByLabelText("Код"), { target: { value: choice.code } });
      fireEvent.click(screen.getByRole("button", { name: "Войти" }));
      await waitFor(() => expect(lane!.of("POST", "/iam/v1/auth/login")).toHaveLength(1));
      expect(lane.of("POST", "/iam/v1/auth/login")[0].body?.secondFactor).toEqual({
        method: choice.method,
        code: choice.code,
      });
    });

    it(`F8-35 · повышение: «${choice.label}» кладёт method = ${choice.method}`, async () => {
      lane = installLane({ "POST /iam/v1/auth/step-up": CEREMONY });
      render(<StepUpModal />);
      act(() => {
        void requestStepUp("2");
      });
      const dialog = await screen.findByRole("dialog");
      fireEvent.click(within(dialog).getByLabelText(choice.label));
      fireEvent.change(within(dialog).getByLabelText("Код"), { target: { value: choice.code } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Подтвердить" }));
      await waitFor(() => expect(lane!.of("POST", "/iam/v1/auth/step-up")).toHaveLength(1));
      expect(lane.of("POST", "/iam/v1/auth/step-up")[0].body).toMatchObject({
        method: choice.method,
        code: choice.code,
      });
    });

    for (const form of [
      { id: "F8-30", open: "Снять второй фактор", send: "Снять", path: "/iam/v1/auth/second-factor/remove" },
      {
        id: "F8-31",
        open: "Выпустить новые запасные коды",
        send: "Выпустить коды",
        path: "/iam/v1/auth/second-factor/backup-codes",
      },
    ]) {
      it(`${form.id} · ${form.send.toLowerCase()}: «${choice.label}» кладёт method = ${choice.method}`, async () => {
        lane = installLane({
          "GET /iam/v1/auth/me": SIGNED_IN,
          "GET /iam/v1/auth/second-factor": ENROLLED,
          [`POST ${form.path}`]: { status: 200, body: { backupCodes: ["0A1B2C3D4E"], session: SESSION } },
        });
        render(
          <MemoryRouter initialEntries={["/settings"]}>
            <AccountSettingsPage />
          </MemoryRouter>,
        );
        const factor = await screen.findByRole("region", { name: "Второй фактор" });
        fireEvent.click(await within(factor).findByRole("button", { name: form.open }));
        fireEvent.click(within(factor).getByLabelText(choice.label));
        fireEvent.change(within(factor).getByLabelText("Код"), { target: { value: choice.code } });
        fireEvent.click(within(factor).getByRole("button", { name: form.send }));
        await waitFor(() => expect(lane!.of("POST", form.path)).toHaveLength(1));
        expect(lane.of("POST", form.path)[0].body).toMatchObject({ method: choice.method, code: choice.code });
      });
    }
  }
});
