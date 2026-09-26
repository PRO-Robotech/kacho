import { CEREMONY_ADDRESSES, loginAddress } from "@shared/pages/auth/ceremony-addresses";
import { tabExiting } from "@shared/pages/auth/tab-exit";

/**
 * Адрес экрана входа КОНСОЛИ с текущим адресом возврата.
 *
 * Вход ведёт консоль (приёмка F8): кнопка «Войти» и отказ `401` уводят на её
 * экран, а не на поток чужого приложения. Сборка адреса — одна на консоль
 * (`loginAddress`), здесь только текущий адрес возврата.
 */
export function loginUrl(returnTo = currentReturnTo()): string {
  return loginAddress(returnTo);
}

/**
 * Увести вкладку на экран входа по отказу `401` — с адресом возврата.
 *
 * Не уводит, пока вкладка уходит выходом (`tab-exit.ts`, условие C14): после
 * гашения сессии `401` получает каждое чтение прежней страницы, и переход по
 * нему нёс бы её адрес возврата — следующий человек входом попадал бы на неё, —
 * а браузер, исполняя последний переход, отменил бы переход выхода.
 *
 * `go` — переход документа; пробы подставляют свой, потому что переход окна
 * тестовое окружение не исполняет.
 */
export function redirectToLogin(go: (to: string) => void = (to) => window.location.assign(to)): void {
  if (isCeremonyRoute(window.location.pathname) || tabExiting()) {
    return;
  }
  go(loginUrl());
}

function currentReturnTo(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

/** На экране церемонии уводить на вход нечего: он и есть вход либо его соседи. */
function isCeremonyRoute(pathname: string): boolean {
  return CEREMONY_ADDRESSES.some((address) => pathname === address || pathname.startsWith(`${address}/`));
}
