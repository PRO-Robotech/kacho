import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import type { ResourceSpec } from "@shared/lib/resource-registry";
import type { VpcDetailShell as VpcDetailShellExport, VpcListShell as VpcListShellExport } from "./VpcShell";

jest.unstable_mockModule("@/components/organisms/ResourceListPage", () => ({
  ResourceListPage: (p: Record<string, unknown>) => (
    <div
      data-testid="list-page"
      data-spec={String(p.spec && (p.spec as ResourceSpec).id)}
      data-parent-field={String(p.parentField)}
      data-parent-param={String(p.parentParam)}
      data-panel-forms={String(p.panelForms)}
    />
  ),
}));

jest.unstable_mockModule("@shared/components/organisms/ResourceDetailPage", () => ({
  ResourceDetailPage: (p: Record<string, unknown>) => (
    <div
      data-testid="detail-page"
      data-spec={String(p.spec && (p.spec as ResourceSpec).id)}
      data-param-key={String(p.paramKey)}
    />
  ),
}));

jest.unstable_mockModule("@shared/components/organisms/ResourceFormModal", () => ({
  ResourceFormModal: ({ projectId }: { projectId: string }) => (
    <div data-testid="form-modal" data-project={projectId} />
  ),
}));

let VpcListShell: typeof VpcListShellExport;
let VpcDetailShell: typeof VpcDetailShellExport;

const spec = { id: "subnets" } as ResourceSpec;

const renderAt = (path: string, pattern: string, node: React.ReactNode) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path={pattern} element={node} />
      </Routes>
    </MemoryRouter>,
  );

describe("VpcListShell", () => {
  beforeAll(async () => {
    ({ VpcListShell, VpcDetailShell } = await import("./VpcShell"));
  });

  it("показывает список и передаёт ему родительскую привязку без искажений", () => {
    renderAt(
      "/projects/prj-7/vpc/networks/net-1/subnets",
      "/projects/:projectId/vpc/networks/:networkId/subnets",
      <VpcListShell spec={spec} parentField="network_id" parentParam="networkId" panelForms />,
    );

    const list = screen.getByTestId("list-page");
    expect(list).toHaveAttribute("data-spec", "subnets");
    expect(list).toHaveAttribute("data-parent-field", "network_id");
    expect(list).toHaveAttribute("data-parent-param", "networkId");
    expect(list).toHaveAttribute("data-panel-forms", "true");
  });

  it("в проекте монтирует модалку форм и отдаёт ей идентификатор проекта из адреса", () => {
    renderAt(
      "/projects/prj-7/vpc/subnets",
      "/projects/:projectId/vpc/subnets",
      <VpcListShell spec={spec} panelForms={false} />,
    );

    expect(screen.getByTestId("form-modal")).toHaveAttribute("data-project", "prj-7");
  });

  it("вне проекта модалку НЕ монтирует — ей нечего было бы создавать", () => {
    renderAt("/vpc/subnets", "/vpc/subnets", <VpcListShell spec={spec} panelForms={false} />);

    expect(screen.getByTestId("list-page")).toBeInTheDocument();
    expect(screen.queryByTestId("form-modal")).not.toBeInTheDocument();
  });
});

describe("VpcDetailShell", () => {
  beforeAll(async () => {
    ({ VpcDetailShell } = await import("./VpcShell"));
  });

  it("показывает карточку ресурса и передаёт имя параметра адреса", () => {
    renderAt(
      "/projects/prj-7/vpc/subnets/sub-1",
      "/projects/:projectId/vpc/subnets/:uid",
      <VpcDetailShell spec={spec} paramKey="uid" />,
    );

    const detail = screen.getByTestId("detail-page");
    expect(detail).toHaveAttribute("data-spec", "subnets");
    expect(detail).toHaveAttribute("data-param-key", "uid");
  });

  it("в проекте монтирует модалку форм, вне проекта — нет", () => {
    renderAt(
      "/projects/prj-7/vpc/subnets/sub-1",
      "/projects/:projectId/vpc/subnets/:uid",
      <VpcDetailShell spec={spec} />,
    );
    expect(screen.getByTestId("form-modal")).toHaveAttribute("data-project", "prj-7");

    renderAt("/vpc/subnets/sub-1", "/vpc/subnets/:uid", <VpcDetailShell spec={spec} />);
    expect(screen.getAllByTestId("detail-page")).toHaveLength(2);
    expect(screen.getAllByTestId("form-modal")).toHaveLength(1);
  });
});
