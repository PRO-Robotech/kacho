// Окружение проб dashboard — НАДСТРОЙКА над общим, а не третья посадка (#626).
// Разбор предмета — в шапке `host/src/test/setup.ts`: он тот же.
import "@shared/test/setup";
import { setBaselineNetwork } from "@shared/test/issuance-guard";

// Сеть в пробах dashboard не ходит никуда: невыполненный запрос обязан
// ОТКАЗАТЬ, а не повиснуть, иначе проба умирает по времени и вердикта не
// оставляет.
//
// Заглушка встаёт СЕТЬЮ ПОД стражем исполнения мест выпуска (общее окружение,
// `issuance-guard.ts`), а не на место `fetch` окна: иначе она сняла бы страж со
// всех проб пакета.
setBaselineNetwork(globalThis, () => Promise.reject(new Error("fetch mock not implemented")));
