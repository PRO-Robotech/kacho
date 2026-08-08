import { render } from "@testing-library/react";
import { JsonView } from ".";

describe("JsonView", () => {
  it("печатает объект с отступом в два пробела", () => {
    const data = { id: "net-1", cidrBlocks: ["10.0.0.0/16"] };
    const { container } = render(<JsonView data={data} />);

    // Сравнение по `textContent`, а не через `getByText`: тот схлопывает пробелы,
    // то есть именно отступ — предмет утверждения — из сравнения и выпал бы.
    expect(container.querySelector("code")?.textContent).toBe(JSON.stringify(data, null, 2));
    expect(container.querySelector("code")?.textContent).toContain('\n  "id": "net-1"');
  });

  it("печатает null как значение, а не как пустоту", () => {
    const { container } = render(<JsonView data={null} />);

    expect(container.querySelector("code")).toHaveTextContent("null");
  });

  it("не роняет разметку на значении, которое JSON не представляет", () => {
    // JSON.stringify(undefined) === undefined: <code> остаётся пустым, но
    // компонент обязан отрисоваться, а не бросить.
    const { container } = render(<JsonView data={undefined} />);

    expect(container.querySelector("pre")).toBeInTheDocument();
    expect(container.querySelector("code")).toHaveTextContent("");
  });
});
