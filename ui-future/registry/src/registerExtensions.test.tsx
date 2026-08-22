// Доменное содержимое карточек реестра и репозитория приходит РЕЕСТРОМ
// РАСШИРЕНИЙ общей оболочки, а не второй копией этого реестра в модуле.
//
// Проба берёт расширение ТЕМ ЖЕ путём, каким его получает карточка: побочный
// эффект импорта регистрации, затем `detailExtension` ОБЩЕГО модуля. Поэтому она
// краснеет и когда регистрация перестанет исполняться (входная точка потеряет
// side-effect-импорт), и когда идентификатор спеки разойдётся с реестром
// ресурсов, — то есть ровно на тех двух способах, которыми доменные строки
// исчезают молча.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import type { DescItem, DetailExtCtx } from "@shared/components/organisms/ResourceDetailExtensions";

const ctx = (data: Record<string, unknown>, over: Partial<DetailExtCtx> = {}): DetailExtCtx => ({
  data,
  projectId: "prj-1",
  detailBase: "/projects/prj-1/registry/registries/reg-1",
  navigate: () => {},
  ...over,
});

async function extensionFor(specId: string) {
  await import("@/registerExtensions");
  const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");
  const ext = detailExtension(specId);
  if (!ext) throw new Error(`расширения карточки «${specId}» в общем реестре нет — предмет пробы отсутствует`);
  return ext;
}

const labels = (items: DescItem[]) => items.map((i) => i.label);

describe("доменные расширения карточек registry-remote", () => {
  it("реестр: строки Обзора приходят из общего реестра расширений", async () => {
    const ext = await extensionFor("registries");
    const rows = ext.overviewExtra?.(
      ctx({
        endpoint: "registry.kacho.local/reg-1",
        region_id: "ru-central1",
        placement_type: "REGIONAL",
        repository_count: 3,
        status: "ACTIVE",
      }),
    );
    expect(labels(rows ?? [])).toEqual([
      "Адрес",
      "Регион",
      "Размещение",
      "Видимость репозиториев по умолчанию",
      "Репозиториев",
      "Статус",
    ]);
  });

  it("реестр: адрес отдаётся общему значку копирования строки", async () => {
    const ext = await extensionFor("registries");
    const rows = ext.overviewExtra?.(ctx({ endpoint: "registry.kacho.local/reg-1" })) ?? [];
    // Адрес ПЕРЕНОСЯТ в команду `docker login`, поэтому копирование объявлено
    // строкой. Пустой адрес копировать нечем — кнопки быть не должно.
    expect(rows.find((r) => r.label === "Адрес")?.copy).toBe("registry.kacho.local/reg-1");
    const empty = ext.overviewExtra?.(ctx({})) ?? [];
    expect(empty.find((r) => r.label === "Адрес")?.copy).toBeUndefined();
  });

  it("реестр: «Репозиториев» показывает НОЛЬ, а не прочерк", async () => {
    // Ноль репозиториев — факт о реестре, а не отсутствие сведений: прочерк на
    // этом месте сообщал бы, что число неизвестно.
    const ext = await extensionFor("registries");
    const rows = ext.overviewExtra?.(ctx({ repository_count: 0 })) ?? [];
    expect(rows.find((r) => r.label === "Репозиториев")?.value).toBe("0");
  });

  it("реестр: действие шапки ведёт к выдаче доступа на ПРОЕКТЕ реестра", async () => {
    const ext = await extensionFor("registries");
    const seen: string[] = [];
    render(<>{ext.headerActions?.(ctx({}, { navigate: (to) => seen.push(to) }))}</>);
    fireEvent.click(screen.getByRole("button", { name: /Управление доступом/ }));
    expect(seen).toEqual(["/projects/prj-1/iam/access-bindings/create"]);
  });

  it("реестр: без проекта в контексте действия шапки нет", async () => {
    // Отдельного per-registry-object scope не существует: выдать доступ не на чем,
    // пока проект неизвестен, и кнопка, ведущая в никуда, хуже её отсутствия.
    const ext = await extensionFor("registries");
    expect(ext.headerActions?.(ctx({}, { projectId: null }))).toBeNull();
  });

  it("репозиторий: строки Обзора и размер общим форматированием", async () => {
    const ext = await extensionFor("repositories");
    const rows =
      ext.overviewExtra?.(ctx({ lifecycle: "DURABLE", visibility: "PRIVATE", tag_count: 2, size_bytes: "2048" })) ?? [];
    expect(labels(rows)).toEqual(["Класс", "Видимость", "Тегов", "Размер"]);
    expect(rows.find((r) => r.label === "Размер")?.value).toBe("2.0 KB");
    // Размера нет — прочерк, а не «0 B»: это разные утверждения о репозитории.
    const empty = ext.overviewExtra?.(ctx({})) ?? [];
    expect(empty.find((r) => r.label === "Размер")?.value).toBe("—");
  });
  it("входная точка модуля ИСПОЛНЯЕТ регистрацию, а не только объявляет её", async () => {
    // Утверждения выше импортируют регистрацию САМИ, поэтому о живой странице они
    // не говорят ничего: потеряй входная точка side-effect-импорт — они останутся
    // зелёными, а карточка лишится доменных строк.
    //
    // Здесь входная точка ЗАГРУЖАЕТСЯ, и утверждается ЕЁ СЛЕДСТВИЕ: после импорта
    // страницы общий реестр расширений знает спеки реестра и репозитория. Прежняя
    // редакция читала исходник страницы регулярным выражением — такая проба зелена,
    // пока файл существует: модуль не исполняется, ни один исход не утверждается,
    // а переехавший в другой файл импорт она объявила бы пропажей.
    //
    // Модули сбрасываются, чтобы загрузка была ПЕРВОЙ: без сброса регистрация уже
    // выполнена импортами выше, и проба зеленела бы, даже если страница потеряла
    // свой импорт целиком.
    jest.resetModules();
    await import("@/pages/RegistryPage/RegistryPage");
    const { detailExtension } = await import("@shared/components/organisms/ResourceDetailExtensions");

    // Утверждается ПЕРЕЧЕНЬ, а не по одному: сравнение множеств печатает разницу
    // целиком, поэтому отказ сразу говорит, какая спека потерялась. Пояснением
    // отказа здесь служит имя пробы — второй аргумент `expect` этот прогонщик не
    // принимает (это форма playwright, не jest).
    const registered = ["registries", "repositories"].filter((id) => detailExtension(id) !== undefined);
    expect(registered).toEqual(["registries", "repositories"]);
  });
});
