import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { useState } from "react";
import { ModuleErrorBoundary, withModuleBoundary } from ".";

// Проба границы отказа (#371). Предмет — не «компонент существует», а три
// утверждения, которые пользователь читает с экрана:
//   (а) отказ ОДНОГО поддерева не уносит соседнее;
//   (б) экран отказа называет раздел ПО ИМЕНИ;
//   (в) есть повторная попытка, и после неё исправное поддерево рисуется.
// Инъекция в обе стороны: исправный ребёнок НЕ показывает экран отказа —
// иначе «показывает всегда» было бы неотличимо от «показывает при отказе».

const Boom = ({ fail }: { fail: boolean }) => {
  if (fail) throw new Error("remoteEntry.js: Failed to fetch");
  return <div data-testid="healthy-child">живой раздел</div>;
};

// Консоль шумит стеком React при каждом пойманном отказе — глушим только на
// время этих проб, чтобы вердикт читался.
beforeEach(() => {
  jest.spyOn(console, "error").mockImplementation(() => undefined);
});
afterEach(() => {
  jest.restoreAllMocks();
});

describe("ModuleErrorBoundary", () => {
  it("положительный контроль: исправное поддерево рисуется, экрана отказа нет", () => {
    render(
      <ModuleErrorBoundary moduleLabel="Virtual Private Cloud">
        <Boom fail={false} />
      </ModuleErrorBoundary>,
    );

    expect(screen.getByTestId("healthy-child")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
  });

  it("называет отказавший раздел ПО ИМЕНИ", () => {
    render(
      <ModuleErrorBoundary moduleLabel="Virtual Private Cloud">
        <Boom fail />
      </ModuleErrorBoundary>,
    );

    const panel = screen.getByTestId("module-unavailable");
    expect(panel).toHaveAttribute("data-module-label", "Virtual Private Cloud");
    expect(panel).toHaveTextContent("Virtual Private Cloud");
  });

  it("отказ одного поддерева не уносит соседнее", () => {
    render(
      <div>
        <ModuleErrorBoundary moduleLabel="Compute Cloud">
          <Boom fail />
        </ModuleErrorBoundary>
        <ModuleErrorBoundary moduleLabel="Virtual Private Cloud">
          <Boom fail={false} />
        </ModuleErrorBoundary>
      </div>,
    );

    expect(screen.getByTestId("module-unavailable")).toHaveAttribute("data-module-label", "Compute Cloud");
    expect(screen.getByTestId("healthy-child")).toBeInTheDocument();
  });

  it("кнопка повторной попытки сбрасывает границу и зовёт onRetry", async () => {
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
    expect(screen.getByTestId("module-unavailable")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Повторить" }));

    expect(screen.getByTestId("healthy-child")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
  });

  it("withModuleBoundary оборачивает страницу модуля её собственной границей", () => {
    const Page = ({ fail }: { fail: boolean }) => <Boom fail={fail} />;
    const Guarded = withModuleBoundary(Page, "Container Registry");

    render(<Guarded fail />);

    expect(screen.getByTestId("module-unavailable")).toHaveAttribute("data-module-label", "Container Registry");
  });

  it("withModuleBoundary сохраняет исправный рендер и пробрасывает props", () => {
    const Page = ({ fail }: { fail: boolean }) => <Boom fail={fail} />;
    const Guarded = withModuleBoundary(Page, "Container Registry");

    render(<Guarded fail={false} />);

    expect(screen.getByTestId("healthy-child")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
  });
});
