import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { useState } from "react";

// Переход браузера подменяется МОДУЛЕМ, а не `window.location`: в jsdom тот не
// поддаётся ни `Object.defineProperty` («Cannot redefine property»), ни
// `jest.spyOn` («Cannot assign to read only property»). Обе попытки проверены —
// именно поэтому переход и живёт отдельным модулем (см. его шапку).
const reloadPage = jest.fn();
const goToServices = jest.fn();
jest.unstable_mockModule("./browser-navigation", () => ({
  SERVICES_PATH: "/dashboard",
  reloadPage,
  goToServices,
}));

const { ModuleErrorBoundary } = await import("./ModuleErrorBoundary");
const { ModuleUnavailablePanel } = await import("./ModuleUnavailablePanel");

const Boom = ({ fail }: { fail: boolean }) => {
  if (fail) throw new Error("remoteEntry.js: Failed to fetch");
  return <div data-testid="healthy-child">живой раздел</div>;
};

beforeEach(() => {
  jest.spyOn(console, "error").mockImplementation(() => undefined);
});
afterEach(() => {
  jest.restoreAllMocks();
  reloadPage.mockClear();
  goToServices.mockClear();
});

describe("экран «раздел недоступен»", () => {
  it("не выносит техническую причину на экран, но и не теряет её", () => {
    render(
      <ModuleErrorBoundary moduleLabel="Virtual Private Cloud">
        <Boom fail />
      </ModuleErrorBoundary>,
    );

    const panel = screen.getByTestId("module-unavailable");
    // Положительный контроль: причина ЕСТЬ. Без него «на экране её нет» было бы
    // верно и для панели, которой причину вовсе не передали, — то есть
    // утверждение зеленело бы на потерянной диагностике.
    expect(panel).toHaveAttribute("data-detail", "remoteEntry.js: Failed to fetch");
    expect(panel.textContent).not.toContain("remoteEntry");
    expect(panel.textContent).not.toContain("Failed to fetch");
    expect(panel.textContent).not.toContain("не загрузился");
  });

  it("говорит, что раздел временно недоступен и ведутся работы", () => {
    render(<ModuleUnavailablePanel moduleLabel="Virtual Private Cloud" />);

    const panel = screen.getByTestId("module-unavailable");
    expect(panel).toHaveTextContent("Раздел «Virtual Private Cloud» временно недоступен");
    expect(panel).toHaveTextContent("Ведутся технические работы");
  });

  it("без имени раздела заголовок обходится без него, а не выдумывает", () => {
    render(<ModuleUnavailablePanel moduleLabel="" />);

    expect(screen.getByTestId("module-unavailable")).toHaveTextContent("Раздел временно недоступен");
  });

  it("рисунок декоративен: смысл несёт текст, а не он", () => {
    const { container } = render(<ModuleUnavailablePanel moduleLabel="Storage" />);

    const art = container.querySelector("svg");
    expect(art).not.toBeNull();
    expect(art).toHaveAttribute("aria-hidden", "true");
    // Цвет берётся только из токенов: литерал не менялся бы вместе с темой.
    expect(art?.outerHTML).not.toMatch(/#[0-9a-fA-F]{3,8}\b|rgba?\(/);
  });
});

describe("повторная попытка", () => {
  it("владельца попытки НЕТ — повтор перезагружает страницу", async () => {
    const user = userEvent.setup();
    render(
      <ModuleErrorBoundary moduleLabel="Консоль Kachō">
        <Boom fail />
      </ModuleErrorBoundary>,
    );

    await user.click(screen.getByRole("button", { name: "Повторить" }));

    expect(reloadPage).toHaveBeenCalledTimes(1);
  });

  it("парный контроль: владелец попытки ЕСТЬ — страница не перезагружается", async () => {
    const user = userEvent.setup();
    const Host = () => {
      const [fail, setFail] = useState(true);
      return (
        <ModuleErrorBoundary moduleLabel="Storage" onRetry={() => setFail(false)}>
          <Boom fail={fail} />
        </ModuleErrorBoundary>
      );
    };

    render(<Host />);
    await user.click(screen.getByRole("button", { name: "Повторить" }));

    expect(screen.getByTestId("healthy-child")).toBeInTheDocument();
    expect(reloadPage).not.toHaveBeenCalled();
  });

  it("повторять нечего — кнопки нет, а уйти по-прежнему можно", () => {
    render(<ModuleUnavailablePanel moduleLabel="Registry" />);

    expect(screen.queryByRole("button", { name: "Повторить" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Все сервисы" })).toBeInTheDocument();
  });
});

describe("уход к списку сервисов", () => {
  it("по умолчанию — полным переходом браузера", async () => {
    const user = userEvent.setup();
    render(<ModuleUnavailablePanel moduleLabel="Load Balancer" />);

    await user.click(screen.getByRole("button", { name: "Все сервисы" }));

    expect(goToServices).toHaveBeenCalledTimes(1);
  });

  it("переход вызывающего сильнее умолчания", async () => {
    const user = userEvent.setup();
    const onGoHome = jest.fn();
    render(<ModuleUnavailablePanel moduleLabel="Load Balancer" onGoHome={onGoHome} />);

    await user.click(screen.getByRole("button", { name: "Все сервисы" }));

    expect(onGoHome).toHaveBeenCalledTimes(1);
    expect(goToServices).not.toHaveBeenCalled();
  });
});
