import { jest } from "@jest/globals";
import { beginTabExit, subscribeTabExit, tabExiting } from "./tab-exit";
import type * as TabExitModule from "./tab-exit";

// Метка выхода одна на ВКЛАДКУ, а не на копию `@shared`: выход ведёт панель
// каркаса, а отказ `401` получает клиент модуля, собранный со своей копией.
// Другая копия здесь — второй экземпляр модуля, загруженный мимо реестра этого.

const KEY = Symbol.for("kacho.console.tab-exit");
const registry = globalThis as unknown as Record<symbol, unknown>;

afterEach(() => {
  delete registry[KEY];
});

async function otherCopy(): Promise<typeof TabExitModule> {
  let other: typeof TabExitModule | null = null;
  await jest.isolateModulesAsync(async () => {
    other = await import("./tab-exit");
  });
  if (other === null) throw new Error("вторая копия модуля не загрузилась");
  return other;
}

describe("C14 · метка выхода вкладки общая у копий @shared", () => {
  it("вторая копия модуля — действительно другой экземпляр (условие проб ниже)", async () => {
    const other = await otherCopy();
    expect(other.beginTabExit).not.toBe(beginTabExit);
  });

  it("метку, поставленную другой копией, эта копия видит, и второго выхода не начинает", async () => {
    const other = await otherCopy();
    expect(tabExiting()).toBe(false);
    const exit = other.beginTabExit();
    expect(exit).not.toBeNull();
    expect(tabExiting()).toBe(true);
    expect(beginTabExit()).toBeNull();
  });

  it("метку, поставленную этой копией, другая копия видит, и отказ выхода её снимает", async () => {
    const other = await otherCopy();
    const exit = beginTabExit();
    expect(other.tabExiting()).toBe(true);
    exit?.abandon();
    expect(other.tabExiting()).toBe(false);
    expect(tabExiting()).toBe(false);
  });

  it("метку прежней формы (`true`, без владельца) эта копия видит, и снять её выходу нечем", () => {
    registry[KEY] = true;
    expect(tabExiting()).toBe(true);
    expect(beginTabExit()).toBeNull();
  });
});

describe("C14 · метка принадлежит выходу, который её поставил", () => {
  it("пока выход идёт, второй не начинается", () => {
    const first = beginTabExit();
    expect(first).not.toBeNull();
    expect(beginTabExit()).toBeNull();
    expect(tabExiting()).toBe(true);
  });

  it("отказ выхода снимает только свою метку: прежний выход чужую не снимает", () => {
    const first = beginTabExit();
    first?.abandon();
    expect(tabExiting()).toBe(false);

    const second = beginTabExit();
    expect(second).not.toBeNull();
    // Повторный отказ прежнего выхода — метка уже не его.
    first?.abandon();
    expect(tabExiting()).toBe(true);
    second?.abandon();
    expect(tabExiting()).toBe(false);
  });

  it("следящие узнают о постановке и снятии метки, снявшие слежку — нет", () => {
    const seen: boolean[] = [];
    const off = subscribeTabExit(() => seen.push(tabExiting()));
    const exit = beginTabExit();
    exit?.abandon();
    off();
    beginTabExit();
    expect(seen).toEqual([true, false]);
  });
});
