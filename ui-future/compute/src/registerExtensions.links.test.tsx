// Ссылка на чужой ресурс — всегда ссылка (правило 2 канона консоли), #406.
//
// ПРЕДМЕТ. Строки «Обзора» карточки машины показывали два идентификатора чужих
// ресурсов моноширинным текстом: тип машины и служебную учётку. У обоих в
// консоли есть своя карточка и свой резолвимый адрес
// (`@shared/lib/service-prefix`: `machine-types` → compute, `service-accounts`
// → iam), то есть переход был возможен и не предлагался. Рядом, в этом же
// перечне строк, зона и загрузочный том уже показаны ссылками — то есть в
// ОДНОЙ карточке жили два вида одного предмета.
//
// ЧТО УТВЕРЖДАЕТСЯ — наблюдаемое: у строки есть якорь с адресом. Проба,
// проверяющая имя компонента, закрепила бы способ и пережила бы переезд
// реализации; адрес переживает только вместе с переходом.

import { jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

jest.unstable_mockModule("@/index.css", () => ({}));
jest.unstable_mockModule("@/typography.css", () => ({}));

await import("@/registerExtensions");
const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");

const DATA: Record<string, unknown> = {
  id: "ins-1",
  zone_id: "ru-central1-a",
  machine_type_id: "mt-standard-2",
  service_account: { id: "sa-42" },
  boot_source: { type: "storage.image", id: "img-7" },
  effective_resources: { v_cpu: 2, memory_mib: "4096" },
  status: "RUNNING",
};

const realFetch = globalThis.fetch;
beforeEach(() => {
  // Имя чужого ресурса подтягивается запросом; для утверждения об АДРЕСЕ оно не
  // нужно, поэтому край отвечает пустым конвертом — ссылка обязана быть и без имени.
  globalThis.fetch = (() =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve("{}"),
    } as Response)) as typeof fetch;
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

/** Строки «Обзора», которые домен добавляет к карточке машины. */
function overviewRows() {
  const ext = detailExtension("compute-instances");
  if (!ext?.overviewExtra) throw new Error("расширение обзора машины не зарегистрировано");
  return ext.overviewExtra({ data: DATA, projectId: "prj-1" } as never);
}

function renderRow(label: string) {
  const row = overviewRows().find((r) => r.label === label);
  if (!row) throw new Error(`строки «${label}» в обзоре нет`);
  return renderValue(row.value);
}

// Карточка машины живёт по адресу `/projects/:projectId/compute/instances/:uid`,
// и project-scoped ссылка берёт проект ИЗ ПАРАМЕТРА МАРШРУТА. Голый роутер его
// не даёт, поэтому ссылка не строилась бы и без дефекта — проба утверждала бы
// про свою оснастку, а не про продукт.
function renderValue(value: unknown) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/projects/prj-1/compute/instances/ins-1"]}>
        <Routes>
          <Route path="/projects/:projectId/compute/instances/:uid" element={<>{value as never}</>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("обзор машины: идентификатор чужого ресурса показан ССЫЛКОЙ", () => {
  it("перечень строк непуст — «ноль находок» не должно означать «ноль прочитанного»", () => {
    expect(overviewRows().length).toBeGreaterThan(5);
  });

  it("«Тип машины» ведёт на карточку типа машины", () => {
    renderRow("Тип машины");
    expect(screen.getByRole("link")).toHaveAttribute("href", "/projects/prj-1/compute/machine-types/mt-standard-2");
  });

  it("«Сервисный аккаунт» ведёт на карточку служебной учётки", () => {
    renderRow("Сервисный аккаунт");
    expect(screen.getByRole("link")).toHaveAttribute("href", "/iam/service-accounts/sa-42");
  });

  it("«Зона доступности» ведёт на карточку зоны — сосед, который ссылкой БЫЛ", () => {
    // Положительный контроль: без него утверждения выше зеленели бы на карточке,
    // где ссылок нет вовсе, — и «стало ссылкой» было бы неотличимо от «проба
    // ищет не там».
    renderRow("Зона доступности");
    expect(screen.getByRole("link")).toHaveAttribute("href", "/system/zones/ru-central1-a");
  });

  it("без идентификатора ссылки НЕТ — прочерк, а не переход в никуда", () => {
    // Обратная сторона правила 5: ссылка без адреса обещает переход, которого
    // нет. Утверждение держит эту половину, иначе «всегда ссылка» читалось бы
    // как «ссылка даже на пустоту».
    const ext = detailExtension("compute-instances");
    const rows = ext!.overviewExtra!({ data: { id: "ins-2" }, projectId: "prj-1" } as never);
    const row = rows.find((r) => r.label === "Тип машины")!;
    renderValue(row.value);
    expect(screen.queryByRole("link")).toBeNull();
  });
});
