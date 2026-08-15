import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ModuleErrorBoundary } from "@shared/components/organisms/ModuleErrorBoundary";

/**
 * Корневая граница консоли (#371, п.1 — «на корне приложения»).
 *
 * Утверждение декларативное: `App.tsx` обязан ОБЪЯВЛЯТЬ корневую границу. Проба,
 * которая роняла бы сам `App`, здесь невозможна честно — уронить его можно только
 * подменив один из его же узлов, и тогда она проверяла бы подмену, а не корень.
 * Поэтому объявление читается из дерева, а СПОСОБНОСТЬ границы поймать отказ
 * доказывается рядом настоящим рендером — иначе «объявлено» было бы неотличимо
 * от «работает».
 */

const appSource = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "App.tsx"), "utf8");

beforeEach(() => {
  jest.spyOn(console, "error").mockImplementation(() => undefined);
});
afterEach(() => {
  jest.restoreAllMocks();
});

describe("корневая граница отказа консоли", () => {
  it("App объявляет ModuleErrorBoundary на корне", () => {
    expect(appSource).toContain("ModuleErrorBoundary");
    expect(appSource).toMatch(/<ModuleErrorBoundary moduleLabel="Консоль Kachō">/);
  });

  it("граница, которую объявляет App, действительно ловит отказ", () => {
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
