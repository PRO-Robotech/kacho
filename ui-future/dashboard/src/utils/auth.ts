import { CEREMONY_ADDRESSES, loginAddress } from "@shared/pages/auth/ceremony-addresses";

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

export function redirectToLogin(): void {
  if (isCeremonyRoute(window.location.pathname)) {
    return;
  }
  window.location.assign(loginUrl());
}

function currentReturnTo(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

/** На экране церемонии уводить на вход нечего: он и есть вход либо его соседи. */
function isCeremonyRoute(pathname: string): boolean {
  return CEREMONY_ADDRESSES.some((address) => pathname === address || pathname.startsWith(`${address}/`));
}
