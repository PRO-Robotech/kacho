import { ENTITIES } from "@shared/lib/entity-names";
import { REGISTRY } from "@shared/lib/resource-registry";
import {
  genderOfLabel,
  mutationFailureText,
  mutationPendingText,
  mutationSuccessText,
  subjectOfSpec,
} from "@shared/lib/mutation-signal";

/**
 * Единая форма сигнала об исходе мутации.
 *
 * # Класс
 *
 * Сообщение складывалось в каждом месте отдельно, и формы разошлись: «создан» ·
 * «готово» · «— готово» · молчание. Плюс причастие было ВПАЯНО в мужском роде
 * (`` `${spec.singular} создан` ``), из-за чего женские и средние имена читались
 * как «Облачная сеть создан», «Виртуальная машина создан», «Зона удалён».
 *
 * Класс держался ровно на том же, на чём держался класс винительного падежа
 * (`resource-label.test.ts`): у мужского рода форма совпадает с «правильной»,
 * поэтому половина сообщений выглядела исправной.
 */

describe("сигнал об исходе — единая форма", () => {
  const network = { label: "Облачная сеть", gender: "f" as const, name: "net-1" };
  const volume = { label: "Том", gender: "m" as const, name: "vol-1" };

  it("успех согласован по роду", () => {
    expect(mutationSuccessText("create", network)).toBe("Облачная сеть net-1 создана");
    expect(mutationSuccessText("create", volume)).toBe("Том vol-1 создан");
    expect(mutationSuccessText("update", network)).toBe("Облачная сеть net-1 обновлена");
    expect(mutationSuccessText("delete", network)).toBe("Облачная сеть net-1 удалена");
    expect(mutationSuccessText("delete", volume)).toBe("Том vol-1 удалён");
  });

  it("отказ называет причину края дословно", () => {
    expect(mutationFailureText("create", network, "Region rgn-1 not found")).toBe(
      "Облачная сеть net-1 не создана: Region rgn-1 not found",
    );
    expect(mutationFailureText("delete", volume, "volume is attached")).toBe(
      "Том vol-1 не удалён: volume is attached",
    );
  });

  // Положительный контроль к предыдущему: без него «причина подставляется» было
  // бы выполнено и реализацией, которая дописывает своё поверх ответа края.
  it("к причине ничего не дописывается", () => {
    const reason = "Illegal argument cidr_block";
    expect(mutationFailureText("update", network, reason).endsWith(reason)).toBe(true);
  });

  it("пустая причина не даёт висящего двоеточия", () => {
    expect(mutationFailureText("create", network, "")).toBe("Облачная сеть net-1 не создана");
    expect(mutationFailureText("create", network, "   ")).toBe("Облачная сеть net-1 не создана");
  });

  it("без имени экземпляра сообщение остаётся связным", () => {
    expect(mutationSuccessText("create", { label: "Зона", gender: "f" })).toBe("Зона создана");
    expect(mutationFailureText("create", { label: "Зона", gender: "f", name: null }, "нет прав")).toBe(
      "Зона не создана: нет прав",
    );
  });

  it("сигнал о приёме по роду не склоняется", () => {
    expect(mutationPendingText("create", network)).toBe("Облачная сеть net-1 создаётся…");
    expect(mutationPendingText("delete", volume)).toBe("Том vol-1 удаляется…");
  });
});

describe("род объявлен, а не выведен", () => {
  it("берётся из словаря, породившего подпись", () => {
    expect(genderOfLabel("Облачная сеть")).toBe("f");
    expect(genderOfLabel("Том")).toBe("m");
    expect(genderOfLabel("Роль")).toBe("f");
  });

  /**
   * Три имени, на которых ошибается ЛЮБАЯ догадка по окончанию, — ради них род и
   * объявляется. «Шлюз» и «Роль» кончаются на согласную, род разный; «Тип диска»
   * кончается на «а», но склоняется опорное слово и род мужской.
   */
  it("имена, на которых ошибается вывод по окончанию", () => {
    expect(genderOfLabel("Шлюз")).toBe("m");
    expect(genderOfLabel("Роль")).toBe("f");
    expect(genderOfLabel("Тип диска")).toBe("m");
  });

  it("подпись не из словаря род не выдумывает", () => {
    expect(genderOfLabel("Нечто, чего в словаре нет")).toBeUndefined();
  });
});

describe("каждая сущность и каждый ресурс несут объявленный род", () => {
  const entityKeys = Object.keys(ENTITIES);
  const specIds = Object.keys(REGISTRY);

  // Перепись: «ноль без рода» обязано быть отличимо от «ноль прочитанного».
  it("прочитаны непустые словарь и реестр", () => {
    expect(entityKeys.length).toBeGreaterThan(25);
    expect(specIds.length).toBeGreaterThan(20);
  });

  it("ни одной сущности без рода", () => {
    const missing = entityKeys.filter((k) => !["m", "f", "n"].includes(ENTITIES[k as keyof typeof ENTITIES].gender));
    expect(missing).toEqual([]);
  });

  /**
   * Ради этого утверждения запасной род в `subjectOfSpec` и не опасен: он есть,
   * чтобы экран не падал на подписи вне словаря, но приземлиться незамеченным не
   * может — здесь падает КАЖДАЯ спека, чья подпись в словаре не резолвится.
   */
  it("подпись каждой спеки резолвится в словаре — запасной род не используется", () => {
    const unresolved = specIds.filter((id) => genderOfLabel(REGISTRY[id].singular) === undefined);
    expect(unresolved).toEqual([]);
  });

  // Отрицание в паре с положительным: без этого «все резолвятся» было бы
  // выполнено и реестром, у которого все подписи мужского рода.
  it("реестр реально несёт не только мужской род", () => {
    const genders = new Set(specIds.map((id) => genderOfLabel(REGISTRY[id].singular)));
    expect(genders.has("f")).toBe(true);
    expect(genders.has("m")).toBe(true);
  });

  it("подлежащее спеки собирается с объявленным родом", () => {
    expect(subjectOfSpec(REGISTRY["networks"], "net-1")).toEqual({
      label: "Облачная сеть",
      gender: "f",
      name: "net-1",
    });
    expect(mutationSuccessText("create", subjectOfSpec(REGISTRY["networks"], "net-1"))).toBe(
      "Облачная сеть net-1 создана",
    );
  });
});
