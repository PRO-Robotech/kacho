// Подключение и отключение тома у машины. Предмет — ЧТО УХОДИТ НА КРАЙ.
//
// Ground truth: proto/kacho/cloud/compute/v1/instance_service.proto. У арма
// `disk` в `AttachedDiskSpec` ровно две ветки — `disk_spec` и `volume_id`; поля
// `disk_id` нет ни в ней, ни в `DetachInstanceDiskRequest`. Оба арма помечены
// «ровно один», поэтому имя, которого сообщение не несёт, оставляет НИ ОДНОЙ
// выбранной ветки: край разбирает тело с отбрасыванием неизвестного и молча
// теряет имя, не сказав о нём ни слова.
//
// Прежняя редакция читала `InstanceDetailPage.tsx` с диска и искала в тексте
// подстроки — то есть утверждала о символах файла: переписали бы ту же отправку
// другой формой записи, и проба осталась бы зелёной, ничего не проверив.
// Теперь диалоги ОТКРЫВАЮТСЯ и утверждается тело ушедшего запроса.

import { jest } from "@jest/globals";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";

const action = jest.fn<(path: string, body: unknown) => Promise<unknown>>();
const get = jest.fn<(path: string) => Promise<Record<string, unknown>>>();
const list = jest.fn<(path: string, q?: unknown) => Promise<Record<string, unknown>>>();

jest.unstable_mockModule("@shared/api/client", () => ({
  api: { action, get, list, create: jest.fn(), update: jest.fn(), delete: jest.fn() },
  ApiError: class ApiError extends Error {},
}));

// Карточка ресурса к предмету пробы отношения не имеет: она тянет весь граф
// консоли и собственные поллеры. Подменён ТРАНСПОРТ показа страницы, а диалоги
// подключения и отключения — настоящие.
jest.unstable_mockModule("@shared/components/organisms/ResourceDetailPage", () => ({
  ResourceDetailPage: ({ secondaryActions }: { secondaryActions?: (data: unknown) => React.ReactNode }) => (
    <div>{secondaryActions?.({
      id: "ins-1",
      boot_disk: { volume_id: "vol-1" },
      secondary_disks: [{ volume_id: "vol-2" }],
    })}</div>
  ),
}));

const { InstanceDetailPage } = await import("./InstanceDetailPage");
const { REGISTRY } = await import("@shared/lib/resource-registry");
const { contextApi } = await import("@shared/lib/context-store");

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/projects/prj-1/compute/instances/ins-1"]}>
        <Routes>
          <Route path="/projects/:projectId/compute/instances/:uid" element={<InstanceDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const bodyOf = (call: number) => action.mock.calls[call][1] as Record<string, unknown>;

beforeEach(() => {
  jest.clearAllMocks();
  // Выбор тома ограничен проектом: без выбранного проекта список не грузится
  // вовсе, и проба утверждала бы о пустом выборе.
  contextApi.setProject({ id: "prj-1", name: "проект", accountId: "acc-1" });
  action.mockResolvedValue({});
  get.mockResolvedValue({});
  list.mockResolvedValue({ volumes: [{ id: "vol-9", name: "данные" }] });
});

describe("подключение тома", () => {
  it("тело несёт ветку volume_id, а не имя, которого сообщение не знает", async () => {
    show();

    fireEvent.click(screen.getByRole("button", { name: /Подключить том/ }));
    // Ждём, пока список томов доедет: смена значения на ещё не загруженном
    // выборе ничего не выбирает, и кнопка осталась бы запертой.
    const option = await screen.findByText("данные");
    const picker = option.closest("select") as HTMLSelectElement;
    fireEvent.change(picker, { target: { value: "vol-9" } });
    await waitFor(() => expect((screen.getByRole("button", { name: "Подключить" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "Подключить" }));

    await waitFor(() => expect(action).toHaveBeenCalled());
    expect(action.mock.calls[0][0]).toContain(":attachDisk");
    const spec = bodyOf(0).attached_disk_spec as Record<string, unknown>;
    expect(spec.volume_id).toBe("vol-9");
    // Имя, которого нет в сообщении, оставляет НИ ОДНОЙ выбранной ветки арма:
    // край отбрасывает его молча, и подключение «проходит» ничего не сделав.
    expect(spec).not.toHaveProperty("disk_id");
    expect(bodyOf(0)).not.toHaveProperty("disk_id");
  });

  it("выбор тома предлагается из хранилища, а не из снятого дубля", () => {
    show();

    fireEvent.click(screen.getByRole("button", { name: /Подключить том/ }));

    // Значение поля задано в терминах идентификаторов тома хранилища: корректный
    // id снятого compute-дубля здесь имел бы верную форму и неверный смысл.
    expect(REGISTRY["volumes"]?.apiPath).toBe("/storage/v1/volumes");
    expect(REGISTRY["volumes"]?.scope).toBe("project");
    expect(list).toHaveBeenCalledWith("/storage/v1/volumes", expect.anything());
  });
});

describe("отключение тома", () => {
  it("тело несёт volume_id верхним полем", async () => {
    show();

    fireEvent.click(screen.getByRole("button", { name: /Отключить том/ }));
    fireEvent.click(screen.getByRole("button", { name: "Отключить" }));

    await waitFor(() => expect(action).toHaveBeenCalled());
    expect(action.mock.calls[0][0]).toContain(":detachDisk");
    expect(bodyOf(0).volume_id).toBe("vol-2");
    expect(bodyOf(0)).not.toHaveProperty("disk_id");
  });
});
