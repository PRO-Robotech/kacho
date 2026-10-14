// Сброс второго фактора на строке пользователя — объявление глагола строки и
// вердикт консоли по отказу «нечего сбрасывать» (приёмка F8r, S1; kacho#3063).
//
// Предмет — то, что человек прочтёт ДО нажатия и ПОСЛЕ отказа. Тексты — часть
// контракта экрана (Р2, Р6) и утверждаются дословно: перефразированное
// подтверждение называло бы другую цену, а перефразированный отказ — другую
// причину.
//
// Видимость пункта консоль НЕ судит (Р3): пункт есть на каждой строке и ни на
// одной не выключен, право решает край. Поэтому утверждается и обратное —
// у соседнего глагола той же строки (`participation`) выключение по состоянию
// есть: без этого близнеца «без disabledReason» зеленело бы и на механизме,
// который выключения не умеет вовсе.

import { apiErrorFromBody } from "@shared/api/client";
import { errorText } from "./error-presentation";
import { REGISTRY } from "./resource-registry";
import type { RowVerbState } from "./resource-spec";

const EMAIL = "u@kacho.local";
const ROW = (status: string) => ({ id: "usr-7", email: EMAIL, invite_status: status });

function resetVerb() {
  const verb = (REGISTRY.users.rowVerbs ?? []).find((v) => v.key === "reset-second-factor");
  if (!verb) throw new Error("у пользователя нет глагола строки «reset-second-factor»");
  return verb;
}

/** Поля, которые `RowVerbState` объявляет; сверх них пункт ничего не несёт (Р3 (в)). */
const DECLARED_FIELDS = new Set<keyof RowVerbState>([
  "label",
  "icon",
  "disabledReason",
  "path",
  "confirmTitle",
  "confirmText",
  "okText",
  "danger",
  "progressTitle",
  "body",
  "succeededText",
  "failedText",
]);

describe("F8r-21 · подтверждение называет цену дословно; вариант над собой", () => {
  it("F8r-21 · над чужой строкой — тексты Р2 и путь глагола", () => {
    // verifies #3063
    const state = resetVerb().resolve(ROW("ACTIVE"), { selfId: "usr-9" });
    expect(state).not.toBeNull();
    expect({
      label: state!.label,
      confirmTitle: state!.confirmTitle,
      confirmText: state!.confirmText,
      okText: state!.okText,
      danger: state!.danger,
      path: state!.path,
    }).toEqual({
      label: "Сбросить второй фактор",
      confirmTitle: "Сбросить второй фактор?",
      confirmText:
        `У «${EMAIL}» будет снят второй фактор и запасные коды, все его сессии завершатся. ` +
        "Войти он сможет паролем или ключом доступа и заново настроить второй фактор в настройках учётной записи.",
      okText: "Сбросить",
      danger: true,
      path: "/iam/v1/users/usr-7:resetSecondFactor",
    });
  });

  it("F8r-21 · над своей строкой — вариант «над собой»", () => {
    const state = resetVerb().resolve(ROW("ACTIVE"), { selfId: "usr-7" });
    expect(state).not.toBeNull();
    expect(state!.label).toBe("Сбросить второй фактор");
    expect(state!.confirmTitle).toBe("Сбросить СВОЙ второй фактор?");
    expect(state!.confirmText.startsWith("Все ваши сессии, включая эту, завершатся.")).toBe(true);
    expect(state!.okText).toBe("Сбросить");
    expect(state!.danger).toBe(true);
    expect(state!.path).toBe("/iam/v1/users/usr-7:resetSecondFactor");
    // Близнец: чужой вариант этой фразы не несёт — иначе ветки не различались бы.
    const other = resetVerb().resolve(ROW("ACTIVE"), { selfId: "usr-9" });
    expect(other!.confirmText).not.toContain("включая эту");
  });

  it("F8r-21 · пункт есть и включён при любом состоянии строки — консоль исход службы не предугадывает", () => {
    for (const status of ["PENDING", "ACTIVE", "BLOCKED"]) {
      const state = resetVerb().resolve(ROW(status), {});
      expect({ status, present: state !== null, disabledReason: state?.disabledReason }).toEqual({
        status,
        present: true,
        disabledReason: undefined,
      });
    }
    // Одно-фактный близнец: соседний глагол той же строки для PENDING выключен с причиной.
    const participation = (REGISTRY.users.rowVerbs ?? []).find((v) => v.key === "participation");
    expect(participation?.resolve(ROW("PENDING"), {})?.disabledReason ?? "").not.toBe("");
  });

  it("F8r-21 · пункт не несёт полей сверх RowVerbState — подсказки на включённом пункте нет", () => {
    const state = resetVerb().resolve(ROW("ACTIVE"), {});
    const extra = Object.keys(state ?? {}).filter((k) => !DECLARED_FIELDS.has(k as keyof RowVerbState));
    expect(extra).toEqual([]);
    expect(state?.disabledReason).toBeUndefined();
  });

  it("F8r-21 · исход назван словами действия (Р5)", () => {
    const state = resetVerb().resolve(ROW("ACTIVE"), {});
    expect({ succeeded: state?.succeededText, failed: state?.failedText }).toEqual({
      succeeded: "Второй фактор сброшен",
      failed: "Не удалось сбросить второй фактор",
    });
  });
});

describe("F8r-22 · отказ «нечего сбрасывать» имеет вердикт консоли", () => {
  it("F8r-22 · SECOND_FACTOR_NOT_ENROLLED назван по-русски (Р6)", () => {
    // verifies #3063
    const refused = apiErrorFromBody(
      400,
      "Bad Request",
      JSON.stringify({
        code: 9,
        message: "second factor is not enrolled",
        details: [
          {
            "@type": "type.googleapis.com/google.rpc.ErrorInfo",
            reason: "SECOND_FACTOR_NOT_ENROLLED",
            domain: "kaname.cloud.iam.v1",
          },
        ],
      }),
    );
    expect(errorText(refused)).toBe("У пользователя не настроен второй фактор — сбрасывать нечего.");
  });

  it("F8r-22 · близнец: тот же код без признака — проза производителя, а не вердикт", () => {
    // Без этого близнеца утверждение выше зеленело бы на разборе, который
    // подменяет ЛЮБОЙ отказ 400 этой фразой.
    const bare = apiErrorFromBody(
      400,
      "Bad Request",
      JSON.stringify({ code: 9, message: "second factor is not enrolled", details: [] }),
    );
    expect(errorText(bare)).not.toBe("У пользователя не настроен второй фактор — сбрасывать нечего.");
  });
});
