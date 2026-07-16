const LOGIN_PATH = "/.ory/kratos/public/self-service/login/browser";

export function loginUrl(returnTo = currentReturnTo()): string {
  const qs = returnTo ? `?return_to=${encodeURIComponent(returnTo)}` : "";
  return `${LOGIN_PATH}${qs}`;
}

export function redirectToLogin(): void {
  if (isAuthRoute(window.location.pathname)) {
    return;
  }
  window.location.assign(loginUrl());
}

function currentReturnTo(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

function isAuthRoute(pathname: string): boolean {
  return (
    pathname.startsWith("/login") ||
    pathname.startsWith("/.ory/kratos/public/self-service/") ||
    pathname.startsWith("/auth/")
  );
}
