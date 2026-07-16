import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { NlbVipCell } from "./NlbVipCell";

// VipAddressLink (рендерится для v4/v6 id) использует useQuery + useParams → тесту
// нужны QueryClient + Router. Query падает без реального бэкенда (retry:false, без
// повторов/зависания), но initial render показывает сам id («пока не загрузилось — id»),
// что и проверяется. Тест никогда не исполнялся (был скрыт host-hang'ом в другом
// nlb-suite — jest --runInBand виснул на @ant-design/icons-импорте, kacho#7).
const withProviders = (ui: ReactNode) => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
};

describe("NlbVipCell", () => {
  it("renders both VIP address ids", () => {
    withProviders(
      <NlbVipCell v4AddressId="adr-v4-000000000000000" v6AddressId="adr-v6-000000000000000" />,
    );
    expect(screen.getByText("adr-v4-000000000000000")).toBeInTheDocument();
    expect(screen.getByText("adr-v6-000000000000000")).toBeInTheDocument();
  });

  it("renders a dash when no VIP is allocated yet", () => {
    withProviders(<NlbVipCell />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
