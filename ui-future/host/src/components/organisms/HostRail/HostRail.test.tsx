import { render, screen } from "@testing-library/react";
import { HostRail } from ".";

describe("HostRail", () => {
  it("matches the unauthenticated original rail surface", async () => {
    render(<HostRail showReachability={false} />);

    expect(screen.getByRole("button", { name: "Kacho" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Все сервисы" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Поиск" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Compute Cloud" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Network Load Balancer" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Identity and Access Management" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Администрирование" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Войти" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Настройки" })).not.toBeInTheDocument();
  });

  it("enables dashboard launchers when project context exists", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/dashboard"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Virtual Private Cloud" })).not.toBeDisabled();
    expect(screen.getByRole("button", { name: "Compute Cloud" })).not.toBeDisabled();
    expect(screen.getByRole("button", { name: "Network Load Balancer" })).not.toBeDisabled();
  });

  it("switches to section navigation inside a federated module uri", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/vpc/networks"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Облачные сети" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Подсети" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Compute Cloud" })).not.toBeInTheDocument();
  });

  it("surfaces the compute MachineType resource item inside the compute section", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/compute/instances"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Виртуальные машины" })).toBeInTheDocument();
    // MachineType — новый ресурс редизайна (read-only sizing-каталог). Должен
    // появиться в rail рядом с Instance (иконка резолвится через antdIconBySpec).
    expect(screen.getByRole("button", { name: "Типы машин" })).toBeInTheDocument();
  });

  it("surfaces the storage Image resource item inside the storage section", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/projects/project-1/storage/volumes"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Тома" })).toBeInTheDocument();
    // Image (boot-image, STOR-1) — новый ресурс редизайна в домене Storage.
    expect(screen.getByRole("button", { name: "Образы" })).toBeInTheDocument();
  });

  it("switches to IAM section navigation on IAM routes", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/iam/accounts"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Аккаунты" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Проекты" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Пользователи" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Virtual Private Cloud" })).not.toBeInTheDocument();
  });

  it("matches IAM access bindings without also matching IAM access", async () => {
    render(
      <HostRail
        context={{
          account: { id: "acc-1", name: "Account" },
          project: { id: "project-1", name: "Project", accountId: "acc-1" },
        }}
        currentPath="/iam/access-bindings"
        showReachability={false}
      />,
    );

    expect(await screen.findByRole("button", { name: "Связки прав" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("button", { name: "Права доступа" })).not.toHaveAttribute("data-active", "true");
  });
});
