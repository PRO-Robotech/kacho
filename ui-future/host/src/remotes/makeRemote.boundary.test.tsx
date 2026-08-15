import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { jest } from "@jest/globals";
import { MemoryRouter } from "react-router";
import { makeRemote, type RemotePageProps } from "./makeRemote";

// Предикат снятия #371: проба подменяет адрес точки входа модуля на
// неразрешимый (loader отклоняется — ровно то, чем оборачивается неудачная
// загрузка remoteEntry.js) и утверждает, что
//   (а) прочие разделы открываются,
//   (б) сломанный называет себя по имени,
//   (в) есть повторная попытка.
// Инъекция в обратную сторону — четвёртая проба: при ИСПРАВНОМ адресе экран
// отказа не показывается. Без неё «показывает всегда» было бы зелёным.

const Page = ({ context }: RemotePageProps) => (
  <div data-testid="remote-page">{context?.project?.id ?? "no-project"}</div>
);

const hostContext = { account: null, project: { id: "project-1", name: "p1" } } as never;

const renderRemote = (Remote: ReturnType<typeof makeRemote>) =>
  render(
    <MemoryRouter>
      <Remote context={hostContext} />
    </MemoryRouter>,
  );

beforeEach(() => {
  jest.spyOn(console, "error").mockImplementation(() => undefined);
});
afterEach(() => {
  jest.restoreAllMocks();
});

describe("makeRemote: граница отказа удалённого модуля", () => {
  it("неразрешимый адрес точки входа — раздел называет СЕБЯ по имени", async () => {
    const Broken = makeRemote(
      () => Promise.reject(new Error("Failed to fetch dynamically imported module")),
      (mod) => mod.default as never,
      "Virtual Private Cloud",
    );

    renderRemote(Broken);

    const panel = await screen.findByTestId("module-unavailable");
    expect(panel).toHaveAttribute("data-module-label", "Virtual Private Cloud");
    expect(panel).toHaveTextContent("Virtual Private Cloud");
  });

  it("отказ одного модуля НЕ уносит соседний — прочие разделы открываются", async () => {
    const Broken = makeRemote(
      () => Promise.reject(new Error("Failed to fetch dynamically imported module")),
      (mod) => mod.default as never,
      "Compute Cloud",
    );
    const Healthy = makeRemote(
      () => Promise.resolve({ default: Page }),
      (mod) => mod.default as never,
      "Storage",
    );

    render(
      <MemoryRouter>
        <Broken context={hostContext} />
        <Healthy context={hostContext} />
      </MemoryRouter>,
    );

    expect(await screen.findByTestId("remote-page")).toHaveTextContent("project-1");
    expect(screen.getByTestId("module-unavailable")).toHaveAttribute("data-module-label", "Compute Cloud");
  });

  it("повторная попытка перезагружает модуль (кэш отклонённого промиса не залипает)", async () => {
    const user = userEvent.setup();
    let attempt = 0;
    const Flaky = makeRemote(
      () => {
        attempt += 1;
        return attempt === 1
          ? Promise.reject(new Error("Failed to fetch dynamically imported module"))
          : Promise.resolve({ default: Page });
      },
      (mod) => mod.default as never,
      "Network Load Balancer",
    );

    renderRemote(Flaky);
    expect(await screen.findByTestId("module-unavailable")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Повторить" }));

    expect(await screen.findByTestId("remote-page")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
    expect(attempt).toBe(2);
  });

  it("инъекция в обратную сторону: при исправном адресе экрана отказа НЕТ", async () => {
    const Healthy = makeRemote(
      () => Promise.resolve({ default: Page }),
      (mod) => mod.default as never,
      "Virtual Private Cloud",
    );

    renderRemote(Healthy);

    expect(await screen.findByTestId("remote-page")).toBeInTheDocument();
    expect(screen.queryByTestId("module-unavailable")).not.toBeInTheDocument();
  });

  it("ошибка РЕНДЕРА внутри модуля тоже ловится границей, а не гасит консоль", async () => {
    const Exploding = () => {
      throw new Error("render blew up inside the remote");
    };
    const Broken = makeRemote(
      () => Promise.resolve({ default: Exploding }),
      (mod) => mod.default as never,
      "Container Registry",
    );

    renderRemote(Broken);

    expect(await screen.findByTestId("module-unavailable")).toHaveAttribute("data-module-label", "Container Registry");
  });
});
