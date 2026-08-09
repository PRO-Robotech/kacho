// Диспетчер inline-формы: по (spec.id, action) он выбирает, ЧЬЮ форму показать
// и что ей передать. Предмет пробы — именно выбор и проводка, потому что
// ошибиться здесь можно молча: пользователь получает generic-форму вместо
// доменной и не видит половины полей ресурса, либо контекст (сеть/подсеть/id)
// до формы не доезжает и она открывается «ниоткуда».
//
// Каждая доменная форма подменена узлом, который ПОКАЗЫВАЕТ своё имя и
// полученную проводку. Это наблюдаемое: показанный текст, а не факт вызова, —
// поэтому промах в проводке виден так же, как промах в ветке.

import { jest } from "@jest/globals";
import React from "react";
import { render, screen } from "@testing-library/react";
import { REGISTRY } from "@shared/lib/resource-registry";

type Wiring = Record<string, unknown>;

/** Заменитель доменной формы: печатает своё имя и полученную проводку. */
function marker(name: string, keys: string[]) {
  return (props: Wiring) =>
    React.createElement(
      "div",
      null,
      `${name}(${keys.map((k) => `${k}=${String(props[k] ?? "")}`).join(", ")})`,
    );
}

jest.unstable_mockModule("@shared/components/organisms/InlineResourceCreateForm", () => ({
  InlineResourceCreateForm: marker("generic-create", ["projectId", "title"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineResourceEditForm", () => ({
  InlineResourceEditForm: marker("generic-edit", ["projectId"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineSubnetCreateForm", () => ({
  InlineSubnetCreateForm: marker("subnet-create", ["projectId", "networkId"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineSubnetEditForm", () => ({
  InlineSubnetEditForm: marker("subnet-edit", ["projectId", "subnetId"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineSecurityGroupEditForm", () => ({
  InlineSecurityGroupEditForm: marker("sg-edit", ["projectId", "sgId"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineAddressPoolCreateForm", () => ({
  InlineAddressPoolCreateForm: marker("pool-create", []),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineAddressPoolEditForm", () => ({
  InlineAddressPoolEditForm: marker("pool-edit", ["poolId"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineNetworkInterfaceCreateForm", () => ({
  InlineNetworkInterfaceCreateForm: marker("nic-create", ["projectId", "subnetId"]),
}));
jest.unstable_mockModule("@shared/components/organisms/InlineNetworkInterfaceEditForm", () => ({
  InlineNetworkInterfaceEditForm: marker("nic-edit", ["projectId", "nicId"]),
}));

const { InlineResourceForm, registerInlineForm } = await import("./InlineResourceForm");

const noop = () => {};

function open(specId: string, over: Record<string, unknown> = {}) {
  return render(
    <>
      {InlineResourceForm({
        spec: REGISTRY[specId],
        action: "create",
        projectId: "prj-1",
        onCancel: noop,
        onSuccess: noop,
        ...over,
      })}
    </>,
  );
}

describe("InlineResourceForm — какую форму показывает диспетчер", () => {
  it("предпосылка: все названные ресурсы существуют в реестре консоли", () => {
    for (const id of ["subnets", "security-groups", "address-pools", "network-interfaces", "addresses", "networks"]) {
      expect(REGISTRY[id]).toBeDefined();
    }
  });

  it("подсеть создаётся доменной формой и получает сеть контекста", () => {
    open("subnets", { action: "create", networkId: "net-7" });

    expect(screen.getByText("subnet-create(projectId=prj-1, networkId=net-7)")).toBeInTheDocument();
  });

  it("подсеть правится доменной формой по своему id", () => {
    open("subnets", { action: "edit", id: "sub-9" });

    expect(screen.getByText("subnet-edit(projectId=prj-1, subnetId=sub-9)")).toBeInTheDocument();
  });

  it("группа безопасности правится доменной формой", () => {
    open("security-groups", { action: "edit", id: "sg-3" });

    expect(screen.getByText("sg-edit(projectId=prj-1, sgId=sg-3)")).toBeInTheDocument();
  });

  it("пул адресов и создаётся, и правится доменными формами", () => {
    open("address-pools", { action: "create" });
    expect(screen.getByText("pool-create()")).toBeInTheDocument();

    open("address-pools", { action: "edit", id: "ap-2" });
    expect(screen.getByText("pool-edit(poolId=ap-2)")).toBeInTheDocument();
  });

  it("сетевой интерфейс и создаётся, и правится доменными формами", () => {
    open("network-interfaces", { action: "create", subnetId: "sub-1" });
    expect(screen.getByText("nic-create(projectId=prj-1, subnetId=sub-1)")).toBeInTheDocument();

    open("network-interfaces", { action: "edit", id: "nic-4" });
    expect(screen.getByText("nic-edit(projectId=prj-1, nicId=nic-4)")).toBeInTheDocument();
  });

  it("адрес в контексте подсети открывается под своим заголовком, а не generic'ом", () => {
    open("addresses", { action: "create", subnetId: "sub-1" });

    expect(screen.getByText("generic-create(projectId=prj-1, title=Резервирование IP-адреса)")).toBeInTheDocument();
  });

  it("ресурс без доменной формы создаётся generic'ом", () => {
    open("networks", { action: "create", title: "Создание: Сеть" });

    expect(screen.getByText("generic-create(projectId=prj-1, title=Создание: Сеть)")).toBeInTheDocument();
  });

  it("generic-правка без загруженных данных не показывает пустую форму", () => {
    const { container } = open("networks", { action: "edit", id: "net-1" });

    expect(container).toBeEmptyDOMElement();
  });

  it("generic-правка с данными показывает generic-форму", () => {
    open("networks", { action: "edit", data: { id: "net-1", name: "frontend" } });

    expect(screen.getByText("generic-edit(projectId=prj-1)")).toBeInTheDocument();
  });
});

describe("InlineResourceForm — доменная форма, зарегистрированная приложением", () => {
  it("перекрывает и generic-ветку, и встроенную доменную", () => {
    registerInlineForm("subnets", "create", (p) => <div>app-subnet-create(projectId={p.projectId})</div>);

    open("subnets", { action: "create", networkId: "net-7" });

    expect(screen.getByText("app-subnet-create(projectId=prj-1)")).toBeInTheDocument();
    expect(screen.queryByText(/^subnet-create\(/)).not.toBeInTheDocument();
  });
});
