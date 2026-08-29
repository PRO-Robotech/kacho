// VIP-адрес балансировщика — то, что видит человек в ячейке.
//
// Предмет пробы сменился вместе с предметом компонента. Прежде здесь стоял
// собственный `<Link>`, и проба утверждала только ТЕКСТ ячейки — то есть
// оставалась бы зелёной, если бы ссылка исчезла вовсе и адрес стал плоской
// надписью. Теперь ячейка рисует единственный вид ссылки консоли
// (`ResourceLink`), и утверждается ОБА свойства: что показано и куда ведёт.
//
// Усечение теперь несёт многоточие — признак того, что значение показано не
// целиком; полное лежит в подсказке. Прежняя редакция ждала обрезок без него,
// то есть строку, которую человек мог принять за настоящий идентификатор.

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import type { ReactNode } from "react";
import { NlbVipCell } from "./NlbVipCell";

// Ячейка резолвит адрес запросом и берёт проект из адреса страницы, поэтому ей
// нужны и клиент запросов, и маршрут С ПАРАМЕТРОМ: без параметра проекта адреса
// карточки не существует, и ссылка не рисуется — это законный исход, но не тот,
// о котором здесь речь. Ответа стенда нет (`retry: false`), поэтому показан
// усечённый идентификатор — «пока не загрузилось».
const atProject = (ui: ReactNode) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/nlb/load-balancers"]}>
        <Routes>
          <Route path="/projects/:projectId/nlb/load-balancers" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
};

describe("VIP-адрес балансировщика", () => {
  it("оба семейства показаны и КАЖДОЕ ведёт на карточку своего адреса", () => {
    atProject(<NlbVipCell v4AddressId="adr-v4-000000000000000" v6AddressId="adr-v6-000000000000000" />);

    // Многоточие — часть показанного: значение усечено, и это сказано вслух.
    const v4 = screen.getByText("adr-v4-00000…");
    const v6 = screen.getByText("adr-v6-00000…");

    // Ссылка, а не надпись: без этого утверждения проба зеленела бы на плоском
    // тексте — ровно на том дефекте, ради которого ячейка сведена к общему виду.
    expect(v4.closest("a")).toHaveAttribute("href", "/projects/prj-1/vpc/addresses/adr-v4-000000000000000");
    expect(v6.closest("a")).toHaveAttribute("href", "/projects/prj-1/vpc/addresses/adr-v6-000000000000000");
  });

  it("невыделенный VIP — прочерк, а не пустая ячейка", () => {
    // Положительный контроль отрицания выше: пустая ячейка выполнила бы «нет
    // чужого адреса» тривиально.
    atProject(<NlbVipCell />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("link")).toBeNull();
  });
});
