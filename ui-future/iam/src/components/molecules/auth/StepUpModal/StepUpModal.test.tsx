import { act, render, screen } from "@testing-library/react";
import { requestStepUp } from "@shared/api/step-up";

// Окно модуля — ТО ЖЕ окно продукта (`@shared/components/molecules/auth/StepUpModal`),
// переэкспортом, а не копией. Поведение окна судит его собственная проба рядом
// с ним; здесь закреплено то, что относится к модулю: смонтированное в модуле
// окно отвечает на просьбу клиента API и перестаёт отвечать, когда снято.

const { StepUpModal } = await import("./StepUpModal");

describe("StepUpModal модуля iam", () => {
  it("смонтированное окно открывается по просьбе клиента API", async () => {
    render(<StepUpModal />);

    act(() => {
      void requestStepUp("2");
    });

    expect(await screen.findByRole("dialog")).toHaveTextContent("Подтверждение действия");
  });

  it("снятое окно на просьбы не отвечает — мёртвое окно не поручится за живой запрос", async () => {
    const { unmount } = render(<StepUpModal />);
    unmount();

    expect(await requestStepUp("2")).toBe(false);
  });
});
