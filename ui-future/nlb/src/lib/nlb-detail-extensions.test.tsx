// Доменные расширения раздела доезжают до ОБЩЕГО реестра — того самого, у
// которого общая оболочка карточки спрашивает расширение по `spec.id`.
//
// Предмет заведён вместе со сведением форка. Прежде раздел нёс свою копию
// оболочки и свой реестр расширений, поэтому вопрос «а видит ли общая оболочка
// доменные строки NLB» не возникал: она их и не читала. Теперь оболочка общая,
// а связывает её с разделом ровно одно — побочное действие импорта
// `lib/nlb-detail-extensions`. Снимут импорт из точки входа либо из бареля —
// карточки NLB молча потеряют доменные строки «Обзора», панель целей и вкладку
// «Целевые группы»: ни сборка, ни типы про это не скажут.
//
// Поэтому спрашивается ИМЕННО общий `detailExtension` (`@shared/…`), а не
// местный барель: барель импортирует регистрацию сам, и проба через него была
// бы истинна даже тогда, когда продукт её не подключил.
//
// Отрицание стоит в паре с положительным: «незнакомый ресурс расширения не
// имеет» на пустом реестре было бы тривиально верным.

import "@/lib/nlb-detail-extensions";

import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import { detailExtension } from "@shared/components/organisms/ResourceDetailExtensions";

const NLB_SPEC_IDS = ["load-balancers", "listeners", "target-groups"] as const;

describe("расширения карточек NLB — в общем реестре", () => {
  it.each(NLB_SPEC_IDS)("%s несёт строки «Обзора»", (specId) => {
    const ext = detailExtension(specId);
    expect(ext?.overviewExtra).toBeDefined();
  });

  it("незнакомый раздел расширения не получает", () => {
    expect(detailExtension("no-such-resource")).toBeUndefined();
  });

  it("целевая группа несёт панель целей под «Обзором»", () => {
    expect(detailExtension("target-groups")?.overviewBelow).toBeDefined();
  });
});

describe("балансировщик — вкладка «Целевые группы»", () => {
  function lbTabs() {
    const ext = detailExtension("load-balancers");
    if (!ext?.extraTabs) throw new Error("у load-balancers нет extraTabs");
    return ext.extraTabs({
      data: { id: "nlb-1" },
      projectId: "prj-1",
      detailBase: "/projects/prj-1/nlb/load-balancers/nlb-1",
      navigate: () => {},
    });
  }

  it("объявлена ровно одна, с адресуемым идентификатором вкладки", () => {
    const tabs = lbTabs();
    expect(tabs).toHaveLength(1);
    expect(tabs[0].id).toBe("target-groups");
    expect(tabs[0].label).toBe("Целевые группы");
  });

  it("рисует содержимое, а не пустоту", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>{lbTabs()[0].render()}</MemoryRouter>
      </QueryClientProvider>,
    );

    // Ни одного листенера у пробы нет, поэтому наблюдаемо ровно то, что вкладка
    // говорит в этом случае: где привязка задаётся. Пустой узел прошёл бы
    // проверку «отрисовалось» и провалился здесь.
    expect(screen.getByText(/Группа задаётся на листенере/)).toBeInTheDocument();
  });
});
