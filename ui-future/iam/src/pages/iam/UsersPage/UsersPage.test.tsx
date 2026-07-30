import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const expectedExports = ["UsersPage", "InviteUserPage"] as const;

const source = readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "UsersPage.tsx"), "utf8");

describe("UsersPage", () => {
  it("declares its public component exports", () => {
    for (const exportName of expectedExports) {
      expect(source).toContain(exportName);
    }
  });

  it("reaches the block/unblock ACTIONS, not a maskable field", () => {
    // Запрет участию — действие с `:verb`, поэтому и метод ACTION: PATCH по
    // ресурсу здесь означал бы правку поля, у которой есть маска, а значит и
    // возможность «забыть поле» и выключить всех, кого коснулся.
    expect(source).toContain(":block");
    expect(source).toContain(":unblock");
    expect(source).toContain('method: "ACTION"');
    // Оба направления обязаны быть провязаны: односторонний контроль оставил бы
    // заблокированного без пути возврата прямо в консоли.
    expect(source).toContain("blockMut");
    expect(source).toContain("unblockMut");
    // Async через Operation, без собственного fetch-слоя.
    expect(source).toContain("useIamMutation");
  });

  it("offers the action that matches the current state, and none for a pending invite", () => {
    // Действие выбирается по состоянию: предлагать «запретить» уже
    // запрещённому — предлагать вызов, который ничего не меняет.
    expect(source).toContain('status === "BLOCKED"');
    expect(source).toContain('status === "PENDING"');
    // PENDING — недоступно и с НАЗВАННОЙ причиной. Живая кнопка здесь обещала бы
    // возможность, которой нет: backend такой вызов отвергает.
    expect(source).toContain("disabled");
    expect(source).toContain("Приглашение ещё не подтверждено");
  });

  it("warns, in plain words, that a self-block cannot be lifted by the person themselves", () => {
    // Самоблокировка не запрещается (запрет потребовал бы самодельной проверки
    // прав в консоли), но её цена названа ДО нажатия.
    expect(source).toContain("selfUserId");
    expect(source).toContain("Запретить участие СЕБЕ?");
    // Три вещи, которые обязаны быть сказаны прямо: снять самому нельзя;
    // восстановление пароля не поможет; вернёт кто-то уровнем выше.
    expect(source).toContain("Снять запрет самостоятельно будет НЕЛЬЗЯ");
    expect(source).toContain("восстановление пароля блокировку не снимает");
    expect(source).toContain("администратор аккаунта или администратор облака");
  });

  it("states the scope of a block, so the operator is not surprised by it", () => {
    // Две вещи, про которые спрашивают в инциденте: живой токен доживает срок, а
    // запрет принадлежит ЭТОМУ аккаунту и другие членства не трогает.
    expect(source).toContain("Уже выданный токен доживёт свой срок");
    expect(source).toContain("Участие в других аккаунтах не затрагивается");
  });

  it("does not second-guess the model about who may do it", () => {
    // Права живут в модели, а не в консоли: никакого клиентского гейта по роли —
    // отказ приходит от backend и показывается общим механизмом ошибок.
    expect(source).not.toContain("system_admin");
    expect(source).not.toContain("canBlock");
  });
});
