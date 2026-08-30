// Ссылка на потребителя показывает ИМЯ, а `owned` называет последствие удаления.
//
// Предмет (#1467). Общий `ResourceReference` беднее контракта: контракт
// `kacho.cloud.reference` объявляет `Referrer{type,id,name}` и
// `Reference{referrer,type,owned}`, а общее объявление несло только `{type,id}` и
// свободную строку вместо перечисления. Оба недостающих поля СЕРВЕР УЖЕ ШЛЁТ —
// том отдаёт `Name: a.InstanceName` и `Owned: a.AutoDelete`, адрес отдаёт то же
// самое, — то есть значения приезжают на клиент и выбрасываются на границе типа.
//
// Наблюдаемое. Кросс-модульный тип потребителя (точечный `compute.instance`)
// намеренно минует `REFERRER_SPEC` — резолв имени запросом там невозможен, у
// модуля нет чужого ресурса в своём реестре. Значит ЕДИНСТВЕННОЕ, чем человек
// может узнать потребителя, — это `name`, приехавшее в самом ответе. Без него в
// строке «Используется» стоит машинный идентификатор.
//
// `owned` отвечает на вопрос «что случится при удалении»: вложение, заказанное
// потребителем неявно, уедет вместе с ним; посторонний потребитель — нет.
//
// Отрицательные контроли обязательны и их два: без `name` запасная ветка
// по-прежнему показывает идентификатор (выдумывать имя нельзя), а без `owned`
// пометки нет — proto3 не отличает ложь от отсутствия, и утверждать «не
// удалится» мы не вправе.
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { ReferrerLink } from "./spec-columns";
import type { ResourceReference } from "@shared/api/types";

function show(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

// Форма типа проверяется КОМПИЛЯТОРОМ, а не чтением файла: гейт по тексту
// объявления зеленел бы на комментарии, где те же имена и стоят.
const wire: ResourceReference = {
  referrer: { type: "compute.instance", id: "ins-0000000000000001", name: "web-01" },
  type: "USED_BY",
  owned: true,
};

describe("потребитель: имя и владение (контракт reference)", () => {
  it("тип поля несёт имя референта, владение и перечисление вида ссылки", () => {
    expect(wire.referrer?.name).toBe("web-01");
    expect(wire.owned).toBe(true);
    expect(wire.type).toBe("USED_BY");
  });

  it("кросс-модульный потребитель показан ИМЕНЕМ, а не идентификатором", () => {
    show(<ReferrerLink projectId="prj-1" referrer={wire.referrer} />);

    expect(screen.getByText("web-01")).toBeInTheDocument();
    expect(screen.queryByText("ins-0000000000000001")).toBeNull();
  });

  it("без имени остаётся идентификатор — выдумывать имя нельзя (контроль)", () => {
    show(<ReferrerLink projectId="prj-1" referrer={{ type: "compute.instance", id: "ins-2" }} />);

    expect(screen.getByText("ins-2")).toBeInTheDocument();
  });

  it("владение названо последствием удаления, а не словом «да»", () => {
    show(<ReferrerLink projectId="prj-1" referrer={wire.referrer} owned={wire.owned} />);

    expect(screen.getByText(/удалится вместе/i)).toBeInTheDocument();
  });

  it("без владения пометки нет — ложь и отсутствие неразличимы (контроль)", () => {
    show(<ReferrerLink projectId="prj-1" referrer={wire.referrer} />);

    expect(screen.getByText("web-01")).toBeInTheDocument();
    expect(screen.queryByText(/удалится вместе/i)).toBeNull();
  });
});
