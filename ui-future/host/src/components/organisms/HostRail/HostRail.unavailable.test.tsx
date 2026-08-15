import { render, screen } from "@testing-library/react";
import { jest } from "@jest/globals";
import { HostRail } from ".";
// Имя недоступного раздела берётся ОТТУДА ЖЕ, откуда его берёт рейл: предмет
// пробы — пометка недоступности, а не подпись. Литерал здесь означал бы третье
// место, где написано имя раздела, и краснел бы на правке подписи, не имеющей
// к отказу модуля никакого отношения.
import { moduleLabelOf } from "../../../remotes/moduleCatalog";

/**
 * Тихая форма отказа модуля (#371, п.3).
 *
 * `HostRail` собирает разделы через `Promise.allSettled`: отклонившийся
 * `import("<remote>/navigation")` отдавал ПУСТОЙ список, и раздел МОЛЧА исчезал
 * из меню. Со стороны пользователя это неотличимо от «такого сервиса нет» —
 * отказ выглядит как отсутствие возможности.
 *
 * Норма: раздел ОСТАЁТСЯ в меню под своим именем и помечен недоступным.
 *
 * Вход подаётся подменой порта загрузки навигации на отклоняющийся — то есть
 * ровно тем, чем это выглядит в браузере (неразрешимый адрес точки входа).
 * Инъекция в обратную сторону — первая проба: при исправных адресах пометки
 * недоступности нет ни у кого.
 */

const context = {
  account: { id: "acc-1", name: "Account" },
  project: { id: "project-1", name: "Project", accountId: "acc-1" },
};

const VPC_SECTION = {
  DASHBOARD_NAVIGATION: [
    {
      key: "vpc",
      segment: "vpc",
      icon: "network",
      label: "Virtual Private Cloud",
      landingPath: "vpc/networks",
      requiresProject: true,
      items: [{ key: "vpc-networks", icon: "network", label: "Облачные сети", path: "vpc/networks" }],
    },
  ],
};

/** Один модуль не грузится, остальные исправны. */
const oneRemoteDown = (down: string) => (remote: string) =>
  remote === down
    ? Promise.reject(new Error("Failed to fetch dynamically imported module"))
    : Promise.resolve(remote === "vpc" ? VPC_SECTION : {});

beforeEach(() => {
  jest.spyOn(console, "error").mockImplementation(() => undefined);
  jest.spyOn(console, "warn").mockImplementation(() => undefined);
});
afterEach(() => {
  jest.restoreAllMocks();
});

describe("HostRail: раздел упавшего модуля остаётся в меню", () => {
  it("инъекция в обратную сторону: все адреса исправны — пометки недоступности нет", async () => {
    render(<HostRail context={context} currentPath="/projects/project-1/dashboard" showReachability={false} />);

    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).toBeInTheDocument();
    expect(document.querySelectorAll("[data-unavailable]")).toHaveLength(0);
  });

  it("неразрешимый адрес одного модуля: раздел ВИДЕН по имени и помечен недоступным", async () => {
    render(
      <HostRail
        context={context}
        currentPath="/projects/project-1/dashboard"
        showReachability={false}
        loadNavigation={oneRemoteDown("storage")}
      />,
    );

    const broken = await screen.findByRole("button", { name: "Storage" });
    expect(broken).toHaveAttribute("data-unavailable", "true");
    expect(broken).toBeDisabled();
  });

  it("прочие разделы при этом остаются рабочими", async () => {
    render(
      <HostRail
        context={context}
        currentPath="/projects/project-1/dashboard"
        showReachability={false}
        loadNavigation={oneRemoteDown("storage")}
      />,
    );

    const vpc = await screen.findByRole("button", { name: "Virtual Private Cloud" });
    expect(vpc).not.toHaveAttribute("data-unavailable");
    expect(vpc).not.toBeDisabled();
  });

  it("недоступный раздел объявляет причину, а не только гасит кнопку", async () => {
    render(
      <HostRail
        context={context}
        currentPath="/projects/project-1/dashboard"
        showReachability={false}
        loadNavigation={oneRemoteDown("nlb")}
      />,
    );

    const broken = await screen.findByRole("button", { name: moduleLabelOf("nlb") });
    expect(broken).toHaveAttribute("title", "Раздел недоступен: модуль не загрузился");
  });
});
