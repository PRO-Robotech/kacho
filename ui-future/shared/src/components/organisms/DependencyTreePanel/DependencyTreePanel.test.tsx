// Панель связей в окне удаления. Удаление через границу сервиса не каскадит
// (ban #4), и часть детей УДЕРЖИВАЕТ родителя: сервер ответит отказом. Панель —
// единственное место, где это видно ДО нажатия. Существенны: иерархия
// сохраняется, дети сгруппированы по типу со счётчиком, удерживающие помечены и
// вынесены в предупреждение, а «связей нет» отличимо от «связи не загрузились».
//
// antd переопределён локально: общий заменитель подменяет `Tree` и `Alert`
// пустыми div'ами, которые своих данных не рисуют, — на нём проба зеленела бы
// при любом дереве.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { antdStub } from "@shared/test/antd-stub";

interface Node {
  key: string;
  title?: React.ReactNode;
  children?: Node[];
}

function renderNodes(nodes: Node[] = []): React.ReactNode {
  return nodes.map((n) =>
    React.createElement("li", { key: n.key }, n.title, n.children ? React.createElement("ul", null, renderNodes(n.children)) : null),
  );
}

jest.unstable_mockModule("antd", () => ({
  ...antdStub(),
  Tree: ({ treeData }: { treeData?: Node[] }) => React.createElement("ul", null, renderNodes(treeData ?? [])),
  Alert: ({ message, description }: { message?: React.ReactNode; description?: React.ReactNode }) =>
    React.createElement("div", { role: "alert" }, message, description),
}));

const { DependencyTreePanel } = await import("./DependencyTreePanel");

type DepNode = Parameters<typeof DependencyTreePanel>[0]["nodes"][number];

function renderPanel(nodes: DepNode[], opts: { loading?: boolean; error?: string | null } = {}) {
  return render(
    <MemoryRouter>
      <DependencyTreePanel nodes={nodes} loading={opts.loading ?? false} error={opts.error ?? null} />
    </MemoryRouter>,
  );
}

// `children` — обязательное поле узла (`mkNode` подставляет пустой массив),
// поэтому фикстура его несёт: узел без него не производится ни одним резолвером,
// и проба на таком узле мерила бы не продукт, а собственную выдумку.
const subnet = (over: Partial<DepNode> = {}): DepNode => ({
  key: "sub-1",
  id: "sub-1",
  name: "frontend",
  resourceId: "subnets",
  routeSegment: "vpc/subnets",
  projectId: "prj-1",
  blocks: false,
  children: [],
  ...over,
});

const address = (over: Partial<DepNode> = {}): DepNode => ({
  key: "addr-1",
  id: "addr-1",
  name: "vip",
  resourceId: "addresses",
  routeSegment: "vpc/addresses",
  projectId: "prj-1",
  blocks: false,
  children: [],
  ...over,
});

describe("DependencyTreePanel", () => {
  it("пустое дерево прямо говорит, что удалять можно", () => {
    renderPanel([]);
    expect(screen.getByText("Зависимых ресурсов нет — можно удалять.")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("отказ загрузки отличим от «связей нет»", () => {
    // Это разные факты: во втором случае удалять можно, в первом — неизвестно.
    renderPanel([], { error: "PERMISSION_DENIED: no path" });
    expect(screen.getByRole("alert")).toHaveTextContent("Не удалось загрузить связи");
    expect(screen.getByRole("alert")).toHaveTextContent("PERMISSION_DENIED: no path");
    expect(screen.queryByText("Зависимых ресурсов нет — можно удалять.")).not.toBeInTheDocument();
  });

  it("во время загрузки не утверждает ни того, ни другого", () => {
    renderPanel([], { loading: true });
    expect(screen.queryByText("Зависимых ресурсов нет — можно удалять.")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("группирует детей по типу и считает их", () => {
    renderPanel([subnet(), subnet({ key: "sub-2", id: "sub-2", name: "backend" })]);

    expect(screen.getByText("Подсети")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "frontend" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "backend" })).toBeInTheDocument();
  });

  it("сохраняет иерархию: адрес показан ПОД своей подсетью", () => {
    renderPanel([subnet({ children: [address()] })]);

    const subnetItem = screen.getByRole("link", { name: "frontend" }).closest("li")!;
    expect(subnetItem.querySelector("ul")).not.toBeNull();
    expect(subnetItem).toHaveTextContent("vip");
  });

  it("адреса под подсетью названы внутренними, а не публичными", () => {
    // Резолвер отбирает сюда ТОЛЬКО приватные адреса подсети; общее название
    // ресурса («Публичные IP-адреса») здесь ввело бы в заблуждение.
    renderPanel([address()]);
    expect(screen.getByText("Внутренние адреса")).toBeInTheDocument();
  });

  it("удерживающие удаление вынесены в предупреждение", () => {
    renderPanel([subnet({ blocks: true })]);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Сначала удалите помеченные ⚠ ресурсы — иначе удаление будет отклонено.",
    );
  });

  it("без удерживающих предупреждения нет", () => {
    // Положительный контроль к предыдущему: без него «предупреждения нет»
    // означало бы лишь, что панель не разобрана.
    renderPanel([subnet({ blocks: false })]);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("ссылка на связанный ресурс открывается в новой вкладке", () => {
    // Окно удаления модальное: переход в той же вкладке потерял бы его вместе
    // с уже введённым подтверждением.
    renderPanel([subnet()]);
    const link = screen.getByRole("link", { name: "frontend" });
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("href", "/projects/prj-1/vpc/subnets/sub-1");
  });

  it("узел без проекта показывает имя без ссылки, а не пустоту", () => {
    renderPanel([subnet({ projectId: undefined })]);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("frontend")).toBeInTheDocument();
  });
});
