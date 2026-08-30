// Наблюдаемое: чем показано поле-ссылка в карточке машины — ссылкой или строкой.
//
// Правило 2 канона консоли: значение, которое есть идентификатор ДРУГОГО
// ресурса, показывается ссылкой (иконка типа + имя + переход), а не
// моноширинным текстом. Идентификатор человеку не адресован — Kachō адресует
// ресурсы неизменяемым `id`, а работает человек с именем.
//
// Проба утверждает АДРЕС, а не присутствие текста: ссылка, ведущая в маршрут,
// которого нет, выглядит рабочей и ломает диагностику на первом переходе.
//
// Двойной контроль обязателен: рядом с требованием ссылки стоит требование её
// ОТСУТСТВИЯ там, где значение ссылкой не является. Без второго утверждение
// зеленело бы на правке, которая превратила в ссылку каждую строку подряд.

import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import type { ReactNode } from "react";

import { detailExtension } from "@shared/components/organisms/ResourceDetailExtensions";
import "@/registerExtensions";

const INSTANCE = {
  id: "ins-1",
  project_id: "prj-1",
  instance_kind: "VM",
  zone_id: "zone-1",
  machine_type_id: "mt-1",
  service_account: { id: "sa-1" },
  status: "RUNNING",
  boot_source: { type: "storage.image", id: "img-1", materialized_volume: { volume_id: "vol-1" } },
};

const ext = detailExtension("compute-instances");
const rows = ext?.overviewExtra?.({ data: INSTANCE } as never) ?? [];

function hrefOfRow(label: string): string | null {
  const row = rows.find((r) => r.label === label);
  if (!row) throw new Error(`строки «${label}» в Обзоре нет`);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // Маршрут задаётся ОБРАЗЦОМ, а не одной строкой адреса: ссылка берёт проект
  // из параметров маршрута, и без совпадения `useParams` вернёт пусто — тогда
  // проба мерила бы собственный харнесс, а не продукт.
  const { container } = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/projects/prj-1/compute/instances/ins-1"]}>
        <Routes>
          <Route path="/projects/:projectId/compute/instances/:id" element={row.value as ReactNode} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return container.querySelector("a")?.getAttribute("href") ?? null;
}

describe("карточка машины: поля-ссылки показаны ссылками", () => {
  it(`перепись: строк Обзора ${rows.length}`, () => {
    // Пустой перечень сделал бы каждое утверждение ниже вакуумным: строка, которой
    // нет, ссылкой не является тривиально.
    expect(rows.length).toBeGreaterThan(5);
  });

  it("тип машины ведёт на карточку каталога типов", () => {
    expect(hrefOfRow("Тип машины")).toBe("/projects/prj-1/compute/machine-types/mt-1");
  });

  it("служебная учётка ведёт на её карточку в IAM", () => {
    // Ресурсы IAM проектом не сужаются — их адрес не project-scoped.
    expect(hrefOfRow("Сервисный аккаунт")).toBe("/iam/service-accounts/sa-1");
  });

  // Положительный контроль полосы: зона ссылкой была и остаётся.
  it("зона доступности ведёт на карточку глобального каталога", () => {
    expect(hrefOfRow("Зона доступности")).toBe("/system/zones/zone-1");
  });

  // Отрицательный контроль: значение, которое ссылкой не является, ею и не стало.
  it("вид машины ссылкой не является", () => {
    expect(hrefOfRow("Тип инстанса")).toBeNull();
  });
});
