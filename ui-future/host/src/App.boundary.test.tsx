import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { ModuleErrorBoundary } from "@shared/components/organisms/ModuleErrorBoundary";

/**
 * Корневая граница консоли (#371, п.1 — «на корне приложения»).
 *
 * ПРОВЕРЯЕТСЯ ПОВЕДЕНИЕ, А НЕ ТЕКСТ. Прежняя редакция читала `App.tsx` с диска и
 * искала в нём подстроку с именем границы. Это проверка ФОРМЫ: она зеленела бы на
 * закомментированной границе и краснела бы на переносе строки — и гейт дерева
 * (`internal/repohygiene`, «проба не читает свой модуль как текст») справедливо
 * её отверг.
 *
 * Настоящая проверка: каркас подменён бросающим, и рендерится сам `App`. Граница
 * обязана поймать отказ и показать свой экран — тогда «объявлено» и «работает»
 * это одно утверждение, а не два разных.
 */

// Каркас host'а — то, что рендерится ВНУТРИ корневой границы. Подменяя его на
// бросающий, мы роняем именно то, ради чего граница стоит: отказ самого каркаса.
jest.unstable_mockModule("./components", () => ({
  HostShell: () => {
    throw new Error("отказ каркаса консоли");
  },
}));

const { default: App } = await import("./App");
const { stubNetwork } = await import("@shared/test/network-stub");

beforeEach(() => {
  jest.spyOn(console, "error").mockImplementation(() => undefined);
  // Каркас монтируется только на «адрес подтверждён» (приёмка F6b, Р7): без
  // такого ответа края бросающему каркасу было бы не с чего начать.
  stubNetwork(() =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      headers: { get: () => null },
      text: () =>
        Promise.resolve(
          JSON.stringify({
            user: { id: "usr-1", email: "a@kacho.local", displayName: "a", subjectType: "user", permissions: [] },
            session: { expiresAt: "2026-09-24T00:00:00Z", assuranceLevel: "1", emailVerified: true },
          }),
        ),
    } as unknown as Response),
  );
});
afterEach(() => {
  jest.restoreAllMocks();
});

describe("корневая граница отказа консоли", () => {
  it("отказ каркаса пойман границей App, а не снёс экран", async () => {
    render(<App />);

    expect(await screen.findByTestId("module-unavailable")).toHaveAttribute("data-module-label", "Консоль Kachō");
  });

  it("граница ловит отказ и называет модуль", () => {
    const Boom = () => {
      throw new Error("корневой отказ");
    };

    render(
      <ModuleErrorBoundary moduleLabel="Консоль Kachō">
        <Boom />
      </ModuleErrorBoundary>,
    );

    expect(screen.getByTestId("module-unavailable")).toHaveAttribute("data-module-label", "Консоль Kachō");
  });

  it("инъекция в обратную сторону: исправное дерево экрана отказа не показывает", () => {
    render(
      <ModuleErrorBoundary moduleLabel="Консоль Kachō">
        <div data-testid="healthy-root">консоль</div>
      </ModuleErrorBoundary>,
    );

    expect(screen.getByTestId("healthy-root")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
  });
});
