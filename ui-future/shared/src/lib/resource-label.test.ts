import { REGISTRY } from "@shared/lib/resource-registry";
import { createActionLabel } from "@shared/lib/resource-label";

/**
 * «Создать» управляет ВИНИТЕЛЬНЫМ падежом — и берёт его из объявления ресурса.
 *
 * # Класс
 *
 * Подпись собиралась как `` `Создать ${spec.singular.toLowerCase()}` ``, то есть
 * из ИМЕНИТЕЛЬНОГО падежа: «Создать виртуальная машина», «Создать облачная сеть»,
 * «Создать целевая группа». Для мужского и среднего рода винительный совпадает с
 * именительным — поэтому половина подписей выглядела исправной, и класс держался
 * ровно на этом: он виден не везде.
 *
 * # Почему НЕ подошёл `genitive`, который в объявлении уже был
 *
 * `genitive` — родительный падеж, и он собран для другой конструкции («Обзор
 * шлюза», «Операции сети»). Подставить его сюда значило бы получить «Создать
 * виртуальной машины». Предикат, которым это меряется, — счёт объявлений падежей
 * во всех пяти реестрах: `grep -rhoE '^\s+(genitive|accusative):' --include
 * resource-registry.tsx . | sort | uniq -c`. На ветке до правки: `genitive` 31,
 * `accusative` 0.
 */

describe("подпись действия склоняет имя ресурса", () => {
  it("винительный падеж берётся из объявления", () => {
    expect(createActionLabel(REGISTRY["compute-instances"])).toBe("Создать виртуальную машину");
    expect(createActionLabel(REGISTRY["networks"])).toBe("Создать облачную сеть");
    expect(createActionLabel(REGISTRY["target-groups"])).toBe("Создать целевую группу");
  });

  // Положительный контроль: там, где винительный совпадает с именительным,
  // подпись обязана остаться прежней — иначе «склонение работает» означало бы
  // лишь, что подпись стала другой.
  it("не портит имена, у которых падежи совпадают", () => {
    expect(createActionLabel(REGISTRY["subnets"])).toBe("Создать подсеть");
    expect(createActionLabel(REGISTRY["gateways"])).toBe("Создать шлюз");
  });

  it("одушевлённое имя склоняется как одушевлённое", () => {
    expect(createActionLabel(REGISTRY["users"])).toBe("Создать пользователя");
  });

  it("подпись задаётся вызывающим, когда она не про «создать»", () => {
    expect(createActionLabel(REGISTRY["subnets"], "Добавить")).toBe("Добавить подсеть");
  });
});

describe("каждый ресурс объявляет винительный падеж", () => {
  const ids = Object.keys(REGISTRY);

  it("прочитан непустой реестр", () => {
    expect(ids.length).toBeGreaterThan(20);
  });

  it("ни одного ресурса без объявленного падежа", () => {
    const missing = ids.filter((id) => !REGISTRY[id].accusative);
    expect(missing).toEqual([]);
  });

  // Отрицание в паре с положительным: без этого «ни одного без падежа» было бы
  // выполнено и пустым реестром, и полем, объявленным пустой строкой.
  it("падеж непуст и отличается от именительного там, где обязан", () => {
    expect(REGISTRY["compute-instances"].accusative).toBe("виртуальную машину");
    expect(REGISTRY["compute-instances"].accusative).not.toBe(
      REGISTRY["compute-instances"].singular.toLowerCase(),
    );
  });
});
