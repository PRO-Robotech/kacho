import { abandonTabExit, beginTabExit, tabExiting } from "./tab-exit";

// Метка выхода одна на ВКЛАДКУ, а не на копию `@shared`: выход ведёт панель
// каркаса, а отказ `401` получает клиент модуля, собранный со своей копией.
// Другая копия здесь — тот же ключ реестра символов, поставленный мимо этого
// модуля.

const KEY = Symbol.for("kacho.console.tab-exit");
const registry = globalThis as unknown as Record<symbol, unknown>;

afterEach(() => {
  delete registry[KEY];
});

describe("C14 · метка выхода вкладки общая у копий @shared", () => {
  it("метку, поставленную другой копией, эта копия видит", () => {
    expect(tabExiting()).toBe(false);
    registry[KEY] = true;
    expect(tabExiting()).toBe(true);
  });

  it("метку, поставленную этой копией, другая копия видит, и отказ выхода её снимает", () => {
    beginTabExit();
    expect(registry[KEY]).toBe(true);
    abandonTabExit();
    expect(registry[KEY]).toBeUndefined();
    expect(tabExiting()).toBe(false);
  });
});
