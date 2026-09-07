// Контракт-замок AccessBinding: карточка привязки читает у ресурса РОВНО те
// имена, которые сообщение ствола несёт.
//
// Ground truth — proto/kaname/cloud/iam/v1/access_binding.proto. У снятого имени
// ДВА разных класса, и проверка, знающая только про один, пропускает второй:
//
//   1. ЗАХОРОНЕНО (`reserved` вместе с именем) — `scope`, `scope_ref`,
//      `target_ref`, `selector`, `condition_id`, `builtin_condition`. Имя
//      выведено из оборота навсегда, номер не переиспользуется.
//   2. ПЕРЕИМЕНОВАНО — координата анкера была `resource_type`/`resource_id`,
//      стала `scope_type`/`scope_id`. Прежние имена в `reserved` НЕ попали: в
//      самом сообщении их просто нет, а на соседних RPC того же домена они
//      ЖИВЫ, помечены DEPRECATED и заполняются. Совпадение имён, разные
//      референты — поэтому запрет касается ровно карточки привязки.
//
// Почему это тест, а не вкусовщина. Имя, которого в сообщении нет, сервер не
// заполняет никогда: чтение возвращает undefined молча, поэтому запасная ветка
// на нём мертва по построению и при этом выглядит страховкой.
//
// ЧТО ЗДЕСЬ ИЗМЕНИЛОСЬ. Прежняя редакция читала с диска `registerExtensions.tsx`
// и шапку формы, разбирала их текст и утверждала о найденных подстроках — в том
// числе о СЛОВАХ комментариев. Такое утверждение говорит о символах файла:
// оно переживает переписывание той же логики другой формой записи и не может
// упасть на изменении того, что человек видит. Теперь расширение карточки
// ИСПОЛНЯЕТСЯ, а утверждается ОТРИСОВАННОЕ: живые координаты показаны, а
// ресурс, у которого заполнены только снятые имена, показывает прочерк —
// то есть запасной ветки на них нет.
//
// Сторона ствола по-прежнему читается с диска: `.proto` — контракт, который
// проба исполнить не может, и другого способа спросить у него нет.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { detailExtension } from "@shared/components/organisms/ResourceDetailExtensions";

import "./registerExtensions";

const here = path.dirname(fileURLToPath(import.meta.url));
const proto = readFileSync(path.resolve(here, "../../../proto/kaname/cloud/iam/v1/access_binding.proto"), "utf8");

/** Тело `message <name> { … }` со счётом скобок (вложенные enum/oneof не рвут). */
function messageBody(name: string): string {
  const at = proto.indexOf(`message ${name} {`);
  expect(at).toBeGreaterThan(-1);
  const open = proto.indexOf("{", at);
  let depth = 0;
  for (let i = open; i < proto.length; i += 1) {
    if (proto[i] === "{") depth += 1;
    else if (proto[i] === "}") {
      depth -= 1;
      if (depth === 0) return proto.slice(open + 1, i);
    }
  }
  throw new Error(`message ${name}: не закрыт`);
}

const body = messageBody("AccessBinding");

/** Имена ОБЪЯВЛЕННЫХ полей сообщения (значения enum — SCREAMING, не совпадут). */
const declaredFields = [
  ...new Set(
    [
      ...body.matchAll(
        /^[ \t]*(?:repeated[ \t]+)?[A-Za-z][A-Za-z0-9_.]*(?:<[^>]*>)?[ \t]+([a-z][a-z0-9_]*)[ \t]*=[ \t]*\d+[ \t]*[;[]/gm,
      ),
    ].map((m) => m[1]),
  ),
];

/** Имена из `reserved "a", "b";` — захоронения (первый класс). */
const retiredNames = (() => {
  const out = new Set<string>();
  for (const m of body.matchAll(/reserved\s+((?:"[a-z_]+"\s*,?\s*)+);/g)) {
    for (const n of m[1].matchAll(/"([a-z_]+)"/g)) out.add(n[1]);
  }
  return [...out];
})();

const ext = detailExtension("access-bindings");

function showOverview(data: Record<string, unknown>) {
  const rows =
    ext?.overviewExtra?.({ data, projectId: null, detailBase: "/iam/access-bindings/acb-1", navigate: () => {} }) ?? [];
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, enabled: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <dl>
          {rows.map((r, i) => (
            <div key={i}>
              <dt>{r.label}</dt>
              <dd>{r.value}</dd>
            </div>
          ))}
        </dl>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return rows;
}

const valueOf = (label: string) => screen.getByText(label).parentElement?.querySelector("dd")?.textContent ?? "";

describe("ствол: два класса снятого имени различимы", () => {
  it("объём осмотренного назван: сколько полей и захоронений прочитано", () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: замолчи любой
    // из двух разборов — утверждения ниже стали бы вакуумными.
    expect(declaredFields).toEqual(
      expect.arrayContaining(["id", "role_id", "scope_type", "scope_id", "status", "target", "deletion_protection"]),
    );
    expect(declaredFields.length).toBeGreaterThanOrEqual(15);
    expect(retiredNames).toEqual(
      expect.arrayContaining(["condition_id", "builtin_condition", "scope", "scope_ref", "target_ref", "selector"]),
    );
  });

  it("захоронённое имя полем не является, переименованное — не захоронено", () => {
    for (const name of retiredNames) expect(declaredFields).not.toContain(name);
    // Переименование: имени нет НИ среди полей, НИ в `reserved`. Именно поэтому
    // перечень запретного нельзя выводить из одного лишь `reserved`.
    for (const renamed of ["resource_type", "resource_id"]) {
      expect(declaredFields).not.toContain(renamed);
      expect(retiredNames).not.toContain(renamed);
    }
    // Контроль в обратную сторону: имя, в которое координата переехала, живо.
    expect(declaredFields).toContain("scope_type");
    expect(declaredFields).toContain("scope_id");
  });
});

describe("карточка привязки: что она показывает", () => {
  it("расширение карточки зарегистрировано — иначе утверждения ниже вакуумны", () => {
    expect(ext?.overviewExtra).toBeDefined();
  });

  it("живые координаты анкера показаны", () => {
    showOverview({
      id: "acb-1",
      role_id: "rol-1",
      scope_type: "iam.account",
      scope_id: "acc-1",
      target: { all_in_scope: {} },
      status: "ACTIVE",
    });

    expect(valueOf("Область (scopeType)")).toContain("iam.account");
    expect(valueOf("Якорь (scopeId)")).toContain("acc-1");
    expect(screen.getByText("Цель")).toBeInTheDocument();
  });

  it("ресурс, у которого заполнены ТОЛЬКО снятые имена, показывает прочерк", () => {
    // Это и есть запрет на запасную ветку: прочитай карточка `resource_type`/
    // `resource_id`/`scope`, здесь показались бы их значения — а сервер этих
    // полей не заполняет никогда, то есть ветка мертва и лишь выглядит
    // страховкой.
    showOverview({
      id: "acb-1",
      role_id: "rol-1",
      resource_type: "iam.account",
      resource_id: "acc-СНЯТОЕ",
      scope: "iam.account",
      target: { all_in_scope: {} },
      status: "ACTIVE",
    });

    expect(valueOf("Область (scopeType)")).not.toContain("iam.account");
    expect(valueOf("Якорь (scopeId)")).not.toContain("acc-СНЯТОЕ");
  });

  it("защита от удаления читается в обеих проекциях имени края", () => {
    // Край отдаёт camelCase, домен объявляет snake_case — карточка обязана
    // понимать обе, и это НЕ запасная ветка на снятое имя, а две проекции
    // ЖИВОГО поля.
    showOverview({ id: "acb-1", deletion_protection: true, target: { all_in_scope: {} } });
    expect(valueOf("Защита от удаления")).toContain("Удаление запрещено");
  });

  it("защита от удаления названа СЛЕДСТВИЕМ, а не ответом «Да»/«Нет»", () => {
    // «Да» рядом с подписью «Защита от удаления» не говорит ни что защита
    // включена, ни что удалить нельзя: читателю приходится достраивать смысл
    // самому (канон консоли, правило 5 — булево не показывается сырым).
    //
    // Утверждаются ОБЕ стороны: односторонняя проба зеленела бы на разметке,
    // где ложь по-прежнему рисуется словом «Нет».
    showOverview({ id: "acb-1", deletionProtection: false, target: { all_in_scope: {} } });
    const shown = valueOf("Защита от удаления");
    expect(shown).toContain("Удаление разрешено");
    expect(shown).not.toContain("Да");
    expect(shown).not.toContain("Нет");
  });
});
