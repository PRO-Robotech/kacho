// Окружение проб dashboard — НАДСТРОЙКА над общим, а не третья посадка (#626).
// Разбор предмета — в шапке `host/src/test/setup.ts`: он тот же.
import "@shared/test/setup";

// Сеть в пробах dashboard не ходит никуда: невыполненный запрос обязан
// ОТКАЗАТЬ, а не повиснуть, иначе проба умирает по времени и вердикта не
// оставляет.
Object.defineProperty(global, "fetch", {
  writable: true,
  value: () => Promise.reject(new Error("fetch mock not implemented")),
});
