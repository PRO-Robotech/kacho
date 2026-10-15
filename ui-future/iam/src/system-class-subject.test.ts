// Системный класс субъекта в консоли (kacho#3238; приёмка kaname#829
// «anonymous-and-authenticated-are-subject-classes», S4: SC-50…53).
//
// Утверждается ЗНАЧЕНИЕ, которое отдают общие помощники выдачи: тело запроса,
// отбор ролей, которые форма вправе предложить классу, и перевод отказа сервера
// к полю формы. Что видит человек на стенде, держит проба браузером
// `e2e/specs/system-class-subject.spec.ts`; здесь — то, на чём она стоит.

import {
  buildCreateAccessBindingBody,
  isSystemClassSubjectType,
  roleGrantsReadsOnly,
  systemClassLabel,
  targetFieldRefusal,
  SUBJECT_TYPE_ENUM,
  SYSTEM_CLASSES,
  type Role,
} from "@shared/api/iam";
import { apiErrorFromBody } from "@shared/api/client";

/** Отказ края в той форме, в какой его разбирает клиент консоли. */
function edgeRefusal(status: number, code: number, message: string, field?: string) {
  const details = field
    ? [{ "@type": "type.googleapis.com/google.rpc.BadRequest", fieldViolations: [{ field, description: message }] }]
    : [];
  return apiErrorFromBody(status, "", JSON.stringify({ code, message, details }));
}

describe("SC-50: класс уходит видом SUBJECT_TYPE_SYSTEM_CLASS", () => {
  it("тело выдачи несёт класс именем значения перечисления и именем класса", () => {
    const body = buildCreateAccessBindingBody({
      subjects: [{ type: SUBJECT_TYPE_ENUM.system_class, id: "authenticated" }],
      roleId: "rol-read",
      scopeTier: "PROJECT",
      scopeId: "prj-1",
      targetResources: [{ type: "vpc.network", id: "enp00000000000000001" }],
    });
    expect(body.subjects).toEqual([{ type: "SUBJECT_TYPE_SYSTEM_CLASS", id: "authenticated" }]);
  });

  it("классов ровно два, в порядке выбора, и у каждого — подпись приёмки", () => {
    expect(SYSTEM_CLASSES).toEqual(["authenticated", "anonymous"]);
    expect(systemClassLabel("authenticated")).toBe("Вошедшие (любой аутентифицированный)");
    expect(systemClassLabel("anonymous")).toBe("Аноним (без входа)");
    // Близнец: незнакомое имя показывается как есть, а не выдаётся за класс.
    expect(systemClassLabel("everyone")).toBe("everyone");
  });

  it("вид класса узнаётся во всех написаниях провода, вид человека — нет", () => {
    for (const t of ["system_class", "SYSTEM_CLASS", "SUBJECT_TYPE_SYSTEM_CLASS"]) {
      expect(isSystemClassSubjectType(t)).toBe(true);
    }
    for (const t of ["user", "SUBJECT_TYPE_USER", "group", ""]) {
      expect(isSystemClassSubjectType(t)).toBe(false);
    }
  });
});

describe("Р4: классу предлагаются только роли чтения", () => {
  const role = (verbs: string[] | undefined): Role => ({ id: "rol-1", name: "r", effectiveVerbs: verbs }) as Role;

  it("роль, дающая только get и list, — предлагается", () => {
    expect(roleGrantsReadsOnly(role(["get", "list"]))).toBe(true);
    expect(roleGrantsReadsOnly(role(["get"]))).toBe(true);
  });

  it("роль с пишущим глаголом или с * — не предлагается", () => {
    expect(roleGrantsReadsOnly(role(["get", "list", "update"]))).toBe(false);
    expect(roleGrantsReadsOnly(role(["*"]))).toBe(false);
    expect(roleGrantsReadsOnly(role(["get", "delete*"]))).toBe(false);
  });

  it("роль, чьи глаголы не прочитаны, — не предлагается: незнание не равно «только чтение»", () => {
    expect(roleGrantsReadsOnly(role(undefined))).toBe(false);
    expect(roleGrantsReadsOnly(undefined)).toBe(false);
  });

  it("глаголы в написании проекции (snake_case) читаются так же", () => {
    expect(roleGrantsReadsOnly({ id: "rol-1", name: "r", effective_verbs: ["list"] } as Role)).toBe(true);
  });
});

describe("SC-52: отказ сервера о цели относится к полю цели дословно", () => {
  const text =
    "Illegal argument target: a grant to system class anonymous names its objects (allInScope is not accepted)";

  it("нарушение поля target отдаёт текст сервера без перевода", () => {
    expect(targetFieldRefusal(edgeRefusal(400, 3, text, "target"))).toBe(text);
  });

  it("нарушение другого поля к цели не относится", () => {
    expect(targetFieldRefusal(edgeRefusal(400, 3, "Illegal argument role_id", "role_id"))).toBeNull();
  });

  it("отказ без нарушений поля к цели не относится", () => {
    expect(targetFieldRefusal(edgeRefusal(403, 7, "permission denied"))).toBeNull();
    expect(targetFieldRefusal(new Error("boom"))).toBeNull();
  });
});
