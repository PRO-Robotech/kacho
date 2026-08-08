import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { DeleteConfirmStub } from ".";

describe("DeleteConfirmStub", () => {
  const props = {
    onOpenChange: jest.fn(),
    resourceLabel: "Network",
    name: "frontend",
    apiPath: "/vpc/v1/networks/net-1",
  };

  it("называет ресурс, который просили удалить", () => {
    render(<DeleteConfirmStub open {...props} />);

    expect(screen.getByText("Network:")).toBeInTheDocument();
    expect(screen.getByText("frontend")).toBeInTheDocument();
  });

  it("даёт готовый запрос вместо действия — иначе окно бесполезно", () => {
    // Окно объясняет, что консоль удаление не выполняет; единственная его польза —
    // назвать точный запрос. Пропади путь — окно превратится в отказ без выхода.
    render(<DeleteConfirmStub open {...props} />);

    expect(screen.getByText("DELETE /vpc/v1/networks/net-1")).toBeInTheDocument();
  });

  it("объясняет, почему кнопки удаления нет, и куда идти вместо неё", () => {
    const { container } = render(<DeleteConfirmStub open {...props} />);

    expect(container).toHaveTextContent("UI не выполняет destructive-операции");
    expect(container).toHaveTextContent("Удаляйте через REST API");
    expect(container).toHaveTextContent("kachoctl");
  });

  it("путь подставляется, а не берётся из шаблона", () => {
    render(<DeleteConfirmStub open {...props} apiPath="/vpc/v1/subnets/sub-9" name="back" resourceLabel="Subnet" />);

    expect(screen.getByText("DELETE /vpc/v1/subnets/sub-9")).toBeInTheDocument();
    expect(screen.getByText("Subnet:")).toBeInTheDocument();
    expect(screen.getByText("back")).toBeInTheDocument();
  });
});
