// Whether a resource has any row action at all.
//
// A table that always appends the actions column gives a read-only resource an
// empty column with a menu button that opens nothing. The predicate has to agree
// with the menu itself: the same "can this be moved" list decides both, so it is
// declared once and read by both.
//
// The move dialog is a stub that prints the REST call it would make. Offering it
// for a resource whose API has no such verb advertises an operation that does
// not exist, so every domain that owns resources declares them move-incapable.

import { REGISTRY } from "@shared/lib/resource-registry";
import { resourceHasRowActions } from "./RowActionsMenu";

describe("resourceHasRowActions", () => {
  it("is false for a read-only catalog resource", () => {
    const diskTypes = REGISTRY["disk-types"];
    expect(diskTypes.ops).toEqual({ create: false, update: false, delete: false });
    expect(resourceHasRowActions(diskTypes)).toBe(false);
  });

  it("is true when the resource can be updated or deleted", () => {
    expect(resourceHasRowActions(REGISTRY.networks)).toBe(true);
  });

  it("is true for a move-capable resource even without update or delete", () => {
    // Ресурс берётся НАСТОЯЩИЙ — тот, чей глагол объявлен контрактом
    // (`/nlb/v1/targetGroups/{id}:move`). Прежде здесь стоял выдуманный id, и
    // он проходил лишь потому, что перечень был устроен ИСКЛЮЧЕНИЯМИ: движимым
    // считался всякий, кого не назвали. После разворота признака (#583)
    // выдуманный id движимым не является — что и есть смысл разворота.
    const readOnlyButMovable = { ...REGISTRY["disk-types"], id: "target-groups" };
    expect(resourceHasRowActions(readOnlyButMovable)).toBe(true);
  });

  it("каталоги только для чтения столбца действий не получают — все, а не часть", () => {
    // СЛЕДСТВИЕ разворота признака (#583), названное здесь прямо, потому что оно
    // видно арендатору. Прежде столбец действий получал всякий ресурс, которого
    // не назвали в перечне исключений, — и у каталога он держал ровно два пункта:
    // просмотр и заглушку перемещения. Заглушка снята, просмотр остаётся по
    // ссылке в имени (правило 5 `ui.md`), поэтому столбца у каталога больше нет.
    //
    // Соседи по классу вели себя так ВСЕГДА: `disk-types`, `regions`, `zones`
    // стояли в прежнем перечне и столбца не получали. Разворот убрал не
    // возможность, а расхождение — эти четверо были исключением по недосмотру.
    for (const id of ["images", "machine-types", "compute-regions", "compute-zones"]) {
      const spec = REGISTRY[id];
      expect({ id, ops: spec.ops }).toEqual({
        id,
        ops: { create: spec.ops.create, update: false, delete: false },
      });
      expect({ id, hasActions: resourceHasRowActions(spec) }).toEqual({ id, hasActions: false });
    }
  });

  it("выдуманный ресурс перемещаемым НЕ считается — умолчание запрещающее", () => {
    // Отрицательный близнец к предыдущему: пара ловит возврат прежнего
    // умолчания, при котором новый ресурс получал заглушку перемещения сам.
    const unknown = { ...REGISTRY["disk-types"], id: "some-movable-thing" };
    expect(resourceHasRowActions(unknown)).toBe(false);
  });

  it("counts the domain-declared move-incapable resources", () => {
    // Перечень перемещаемых — РАЗРЕШАЮЩИЙ и выводится из контрактов (#583),
    // поэтому ни один из этих ресурсов пункта не получает: глагол `:move`
    // объявлен только у балансировщика и целевой группы. Прежде здесь стоял
    // перечень исключений, и каждое из этих имён приходилось выписывать руками.
    for (const id of [
      "compute-instances",
      "volumes",
      "snapshots",
      "disk-types",
      "registries",
      "repositories",
      "tags",
    ]) {
      const spec = { ...REGISTRY["disk-types"], id };
      expect({ id, hasActions: resourceHasRowActions(spec) }).toEqual({ id, hasActions: false });
    }
  });
});
