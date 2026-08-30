// Общий реестр несёт ПОЛНЫЕ спеки блочного хранения, а не бедные цели ссылок.
//
// Предмет (#1466). Спека снимка жила ТОЛЬКО в реестре домена storage, и от этой
// разности из одного элемента зависели сразу два форка: `ResourceShell` домена
// (общая оболочка резолвит спеку ПО МАРШРУТУ в общем реестре — карточка снимка
// осталась бы без спеки, то есть без страницы) и сам реестр домена.
//
// НАБЛЮДАЕМОЕ. `RefNameLink` резолвит спеку в ОБЩЕМ реестре. На карточке образа
// колонка «Источник» ссылается на снимок либо на том — и на снимке ссылка
// вырождалась в плоский идентификатор, тогда как на томе в той же колонке
// работала. Один и тот же вид поля читался двумя разными способами.
//
// НАПРАВЛЕНИЕ СВЕДЕНИЯ. Перенос — не «взять общее»: по трём спекам из шести
// богаче был ДОМЕН (у общего они стояли целями ссылок — без полей формы и без
// глаголов), и сведение в обратную сторону сняло бы у арендатора создание тома,
// снимка и правку образа МОЛЧА. Поэтому проба судит не наличие ключа, а
// ПОЛНОТУ: у ресурса, который домен даёт создавать, обязаны быть и глагол, и
// поля формы, и шаблон.
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { REGISTRY } from "./resource-registry";
import { RefNameLink } from "@shared/components/molecules/RefNameLink";

const realFetch = globalThis.fetch;

beforeEach(() => {
  globalThis.fetch = () =>
    Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify({ snapshots: [{ id: "snap-1", name: "ночной-срез" }] })),
    } as Response);
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

function show(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

// Ресурсы блочного хранения, которые арендатор СОЗДАЁТ из консоли. Перечень
// именной, а не выведенный из самого реестра: вывод из предмета суда сделал бы
// утверждение вакуумным — исчезнет спека, исчезнет и требование к ней.
const CREATABLE = ["volumes", "snapshots", "images"] as const;

describe("общий реестр: спеки блочного хранения", () => {
  it("прочитал реестр (молчаливый ноль — не пропуск)", () => {
    expect(Object.keys(REGISTRY).length).toBeGreaterThan(25);
  });

  it("несёт спеку снимка", () => {
    expect(REGISTRY.snapshots?.apiPath).toBe("/storage/v1/snapshots");
    expect(REGISTRY.snapshots?.payloadKey).toBe("snapshots");
  });

  it.each(CREATABLE)("спека «%s» полная: глагол создания, поля формы, шаблон", (id) => {
    const spec = REGISTRY[id];
    expect({
      id,
      create: spec?.ops?.create,
      fields: (spec?.fields ?? []).length > 0,
      template: typeof spec?.template === "function",
    }).toEqual({ id, create: true, fields: true, template: true });
  });

  it("класс диска остаётся справочником — создавать его арендатор не может (контроль)", () => {
    // Обратная сторона: «полная» не значит «всем всё разрешено». Каталог типов
    // заводит администратор облака, и глагол создания здесь был бы неправдой.
    expect(REGISTRY["disk-types"]?.ops?.create).toBe(false);
    expect(REGISTRY["disk-types"]?.apiPath).toBe("/storage/v1/diskTypes");
  });

  it("ссылка на снимок резолвится именем, а не остаётся идентификатором", async () => {
    show(<RefNameLink specId="snapshots" refId="snap-1" projectId="prj-1" />);

    await waitFor(() => expect(screen.getByText("ночной-срез")).toBeInTheDocument());
  });

  it("спека, которой в реестре нет, ссылкой не становится (контроль)", () => {
    // Без этого «ссылка резолвится» означало бы лишь, что резолвит вообще всё.
    show(<RefNameLink specId="teleporters" refId="tp-1" projectId="prj-1" />);

    expect(screen.getByText("tp-1")).toBeInTheDocument();
  });
});
