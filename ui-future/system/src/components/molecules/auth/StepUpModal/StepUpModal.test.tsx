// StepUpModal модуля — контракт перехвата: окно отвечает на просьбу клиента API,
// перестаёт отвечать, когда снято, и держит вызывающего в ожидании до ответа
// человека.
//
// Окно модуля — ТО ЖЕ окно продукта (переэкспорт `@shared/components/molecules/auth/StepUpModal`),
// и его поведение подробно судит проба рядом с ним. Здесь закреплено то, что
// относится к модулю: смонтированное в модуле окно — единственный путь, которым
// его клиент отвечает на отказ «нужна повторная проверка». Не ответит окно —
// запрос не будет повторён никогда; ответит снятое — отвечать возьмётся мёртвый
// компонент.

import { act, render, screen } from "@testing-library/react";
import { requestStepUp } from "@shared/api/step-up";

const { StepUpModal } = await import("./StepUpModal");

describe("StepUpModal модуля system — жизненный цикл", () => {
  it("до просьбы окно закрыто", () => {
    render(<StepUpModal />);

    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("по просьбе клиента API открывается и объясняет, чем подтверждать", async () => {
    render(<StepUpModal />);

    act(() => {
      void requestStepUp("2");
    });

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Подтверждение действия");
    // Уровень «2» поднимается только вторым фактором — окно так и говорит.
    expect(dialog).toHaveTextContent("подтверждается вторым фактором");
    expect(screen.getByLabelText("Запасной код")).toBeInTheDocument();
  });

  it("просьба ждёт ответа человека — обещание само не разрешается", async () => {
    render(<StepUpModal />);

    let settled = false;
    act(() => {
      void requestStepUp("2").then(() => {
        settled = true;
      });
    });
    await screen.findByRole("dialog");

    expect(settled).toBe(false);
  });

  it("снятое окно на просьбы не отвечает", async () => {
    const view = render(<StepUpModal />);
    view.unmount();

    expect(await requestStepUp("2")).toBe(false);
  });
});
