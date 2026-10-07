// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Адреса церемоний личности — ОДНО объявление на консоль (приёмка F8, Р1, Р3).
//
// Консоль объявляет маршруты на все шесть и не спрашивает, какая посадка под
// ней: второй ручки «а вести ли церемонию самим» не заводится — она разошлась
// бы с раздачей молча. С приёмки F8-S3 консоль ВЕДЁТ все шесть: восстановление
// доступа стало экраном, и вид «не ведёт» снят вместе со своей страницей (Р1).
// Правило F8 Р3 остаётся правилом: появится адрес, которого консоль не ведёт, —
// вид и страница «такого адреса здесь нет» заводятся его изменением, а не
// замыкающим правилом на панель.
//
// ДО ПОДТВЕРЖДЕНИЯ АДРЕСА ПОЧТЫ открыты ровно экраны вида `screen` — вход,
// регистрация, выход и сам экран подтверждения (приёмка F6b, Р7). Всё прочее —
// каркас, параметры учётной записи, экран восстановления доступа — стоит за
// стражем подтверждённости, и перечень открытого выводится из этого же
// объявления, а не выписывается вторым.
//
// `/error` и `/consent` сюда НЕ входят: это адреса чужих потоков, и
// совместимость с их ссылками не нужна — они разделяют судьбу любого
// неизвестного адреса.

/** Все адреса церемоний, получающие маршрут. */
export const CEREMONY_ADDRESSES = [
  "/login",
  "/registration",
  "/logout",
  "/settings",
  "/recovery",
  "/verification",
] as const;

export type CeremonyAddress = (typeof CEREMONY_ADDRESSES)[number];

/**
 * Как консоль отвечает на каждый адрес церемонии — ОДНО решение, и его читает
 * маршрутизатор оболочки (условие C1). Тип ключа исчерпывающий: адрес,
 * добавленный в перечень выше и не решённый здесь, роняет сборку, а не уходит
 * замыкающим правилом на панель.
 *
 *   • `screen`         — экран церемонии вне каркаса, открытый и до
 *                        подтверждения адреса: у человека без сессии нет ни
 *                        проекта, ни разделов;
 *   • `guarded-screen` — экран вне каркаса ЗА стражем подтверждённости (приёмка
 *                        F8-S3, Р1): без сессии страж пропускает, и экран решает
 *                        сам; неподтверждённая сессия уходит на подтверждение
 *                        (F6b-15);
 *   • `in-shell`       — экран внутри каркаса: его открывает вошедший человек.
 *
 * `/verification` — экран подтверждения адреса почты (приёмка F6b, Р8): вне
 * каркаса, как вход и регистрация, — у человека без подтверждения нет ни
 * проекта, ни разделов. `/recovery` — экран восстановления доступа (приёмка
 * F8-S3): вне каркаса, но за стражем — F6b Р7 перечисляет его среди адресов за
 * стражем, и S3 это не меняет.
 */
export type CeremonyServing =
  | { kind: "screen"; screen: "login" | "registration" | "logout" | "verification" }
  | { kind: "guarded-screen"; screen: "recovery" }
  | { kind: "in-shell"; screen: "account-settings" };

export const CEREMONY_ROUTING: Readonly<Record<CeremonyAddress, CeremonyServing>> = {
  "/login": { kind: "screen", screen: "login" },
  "/registration": { kind: "screen", screen: "registration" },
  "/logout": { kind: "screen", screen: "logout" },
  "/settings": { kind: "in-shell", screen: "account-settings" },
  "/recovery": { kind: "guarded-screen", screen: "recovery" },
  "/verification": { kind: "screen", screen: "verification" },
};

/**
 * Адреса, открытые до подтверждения адреса почты, — экраны вне каркаса (Р7).
 * Выведены из `CEREMONY_ROUTING`: второй перечень тех же адресов разошёлся бы с
 * маршрутизатором молча — экран, добавленный вне каркаса, оказался бы за
 * стражем, либо страница за стражем — перед ним.
 */
export const OPEN_BEFORE_ADDRESS_CONFIRMATION: readonly CeremonyAddress[] = CEREMONY_ADDRESSES.filter(
  (address) => CEREMONY_ROUTING[address].kind === "screen",
);

/** Открыт ли путь до подтверждения адреса — экран вне каркаса либо его подпуть. */
export function isOpenBeforeAddressConfirmation(pathname: string): boolean {
  return OPEN_BEFORE_ADDRESS_CONFIRMATION.some((address) => pathname === address || pathname.startsWith(`${address}/`));
}

/** Экран параметров учётной записи — адрес из перечня, а не литерал у каждого читателя. */
export const ACCOUNT_SETTINGS_ADDRESS: CeremonyAddress = "/settings";

/** Куда уводит вход без адреса возврата и с отвергнутым: корень консоли. */
export const CONSOLE_ROOT = "/";

/**
 * Имя параметра адреса возврата — ОДНО у всех производителей и у читателя
 * (условие C10). Производители — кнопка «Войти» каркаса, переход на вход по
 * отказу `401`, выход, путь со страницы параметров, ссылки между экранами
 * входа, регистрации и восстановления, страж подтверждённости адреса и уход по отказу края
 * `EMAIL_NOT_VERIFIED` (приёмка F6b); читатель — `useReturnTo`. Второе написание имени
 * (`return_to`) дало бы адрес, который читатель не видит, и вход уводил бы на
 * корень молча.
 */
export const RETURN_TO_PARAM = "returnTo";

function withReturnTo(address: CeremonyAddress, returnTo?: string): string {
  if (!returnTo || returnTo === CONSOLE_ROOT) return address;
  return `${address}?${new URLSearchParams({ [RETURN_TO_PARAM]: returnTo }).toString()}`;
}

/** Адрес экрана входа с адресом возврата — ЕДИНСТВЕННАЯ его сборка. */
export function loginAddress(returnTo?: string): string {
  return withReturnTo("/login", returnTo);
}

/** Адрес экрана регистрации с адресом возврата — ЕДИНСТВЕННАЯ его сборка. */
export function registrationAddress(returnTo?: string): string {
  return withReturnTo("/registration", returnTo);
}

/**
 * Адрес экрана восстановления доступа с адресом возврата — ЕДИНСТВЕННАЯ его
 * сборка (приёмка F8-S3, Р4): её зовёт ссылка «Не получается войти?» экрана
 * входа. Адреса почты в нём нет и быть не может: адрес в адресе страницы оседал
 * бы в истории браузера и журналах раздачи — человек вводит его заново одним
 * полем.
 */
export function recoveryAddress(returnTo?: string): string {
  return withReturnTo("/recovery", returnTo);
}

/**
 * Адрес экрана подтверждения адреса почты с адресом возврата — ЕДИНСТВЕННАЯ его
 * сборка (приёмка F6b, Р8): её зовут страж каркаса и уход по отказу края
 * `EMAIL_NOT_VERIFIED`. Имя параметра — то же `returnTo`; читает его экран
 * только через `useReturnTo`.
 */
export function verificationAddress(returnTo?: string): string {
  return withReturnTo("/verification", returnTo);
}
