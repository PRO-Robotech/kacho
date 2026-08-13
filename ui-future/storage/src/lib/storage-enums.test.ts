import { STATUS_REASON_TEXT, sizeLimitFacts, statusReasonText, volumeTransientHint } from "./storage-enums";

/**
 * Тексты, которые storage произносит сам: причина состояния, границы размера,
 * подпись к промежуточному состоянию тома.
 *
 * Общий словарь ТИПА ДИСКА здесь не перепроверяется — он живёт в
 * `@shared/lib/storage-disk-type` и закреплён своей пробой рядом с ним. Два
 * места об одном предмете разошлись бы на первом же уточнении формулировки.
 */

describe("причина состояния — правило 9: нет причины ⇒ нет строки", () => {
  it("«причины нет» и «поля нет» отвечают одинаково — null, а не прочерк", () => {
    // Прочерк на месте причины читается как «причина есть, но мы её не знаем».
    // Контракт называет оба случая одним и тем же: ресурс в штатном состоянии.
    expect(statusReasonText("STATUS_REASON_UNSPECIFIED")).toBeNull();
    expect(statusReasonText("")).toBeNull();
    expect(statusReasonText(undefined)).toBeNull();
    expect(statusReasonText(null)).toBeNull();
  });

  it("каждое значение словаря отвечает на вопрос «ждать или действовать»", () => {
    // Ровно ради этой разницы причина и заведена: один status не отличает
    // «бэкенд не ответил, система вернётся» от «отказал по существу».
    expect(STATUS_REASON_TEXT.BACKEND_UNAVAILABLE).toMatch(/времен/i);
    expect(STATUS_REASON_TEXT.BACKEND_REJECTED).toMatch(/не пройдёт/i);
    expect(STATUS_REASON_TEXT.BACKEND_CAPACITY_EXHAUSTED).toMatch(/нет места/i);
  });

  it("ни одна фраза не называет физику плоскости данных", () => {
    // Причина адресована арендатору: он не управляет пулами, узлами и репликами,
    // и упоминание их было бы утечкой инфраструктуры в интерфейс. Закрытый
    // словарь закрывает канал по значениям — тексты обязаны держать ту же линию.
    const forbidden = /пул|узл|osd|реплик|ceph|namespace|кластер/i;
    for (const [token, text] of Object.entries(STATUS_REASON_TEXT)) {
      expect({ token, leaks: forbidden.test(text) }).toEqual({ token, leaks: false });
    }
  });

  it("незнакомое значение показывается как есть, а не глотается", () => {
    // Словарь мог пополниться раньше консоли. Промолчать о том, что ресурс не в
    // порядке, — хуже, чем показать сырой токен.
    expect(statusReasonText("BACKEND_ON_FIRE")).toBe("BACKEND_ON_FIRE");
  });
});

describe("промежуточное состояние тома объясняется, конечное — нет", () => {
  it("том рождается в состоянии создания — окно названо словами", () => {
    // Между успешным ответом на Create и работающим томом есть окно: пригодным
    // том делает сверщик. Значок называет состояние, но не отвечает, надо ли
    // что-то делать.
    expect(volumeTransientHint("CREATING")).toMatch(/готовит/i);
    expect(volumeTransientHint("MIGRATING")).toMatch(/исходном типе/i);
  });

  it("у конечного состояния строки нет вовсе (правило 9)", () => {
    // Не прочерк, а отсутствие: «Available» самодостаточен, и подпись под ним
    // была бы шумом на каждой карточке.
    expect(volumeTransientHint("AVAILABLE")).toBeNull();
    expect(volumeTransientHint("IN_USE")).toBeNull();
    expect(volumeTransientHint("ERROR")).toBeNull();
    expect(volumeTransientHint(undefined)).toBeNull();
  });
});

describe("границы размера — ноль означает «класс не сужает», а не «ноль байт»", () => {
  it("класс без единой границы не показывает секцию вовсе", () => {
    // Пустой массив — сигнал не рисовать секцию. Три прочерка на месте трёх
    // границ читались бы как «границы есть, но мы их не знаем».
    expect(sizeLimitFacts(undefined)).toEqual([]);
    expect(sizeLimitFacts({})).toEqual([]);
    expect(sizeLimitFacts({ min_size_bytes: "0", max_size_bytes: "0", size_step_bytes: "0" })).toEqual([]);
  });

  it("нулевая граница названа фразой, а не «0 B»", () => {
    // «0 B» утверждало бы о классе неправду: что он допускает том нулевого
    // размера. Отсутствие границы отличается от границы, равной нулю.
    const facts = sizeLimitFacts({ min_size_bytes: String(10 * 1024 ** 3), max_size_bytes: "0", size_step_bytes: "0" });
    expect(facts.map((f) => f.label)).toEqual(["Наименьший размер", "Наибольший размер", "Шаг размера"]);
    expect(facts[0].text).not.toMatch(/^0/);
    expect(facts[1].text).toMatch(/не сужает сверху/i);
    expect(facts[2].text).toMatch(/кратность не требуется/i);
  });

  it("объявленные границы показываются человекочитаемым размером", () => {
    const facts = sizeLimitFacts({
      min_size_bytes: String(10 * 1024 ** 3),
      max_size_bytes: String(4096 * 1024 ** 3),
      size_step_bytes: String(1024 ** 3),
    });
    // int64 приходит СТРОКОЙ — разбор строки обязателен, иначе граница молча
    // становится NaN и печатается прочерком.
    expect(facts[0].text).toMatch(/10/);
    expect(facts[1].text).toMatch(/4/);
    expect(facts[2].text).toMatch(/^Кратно /);
  });
});
