// Same-origin redirect guard for auth hand-off targets (return_to /
// post_logout_redirect_uri). These arrive from the query string and are fed to
// react-router navigate() / the sign-in address; an unvalidated value is an
// open-redirect / re-phishing surface (CWE-601). We only ever navigate to a
// destination that resolves onto our own origin.

const DEFAULT_FALLBACK = "/";

/**
 * Resolve a caller-supplied redirect target to a safe, same-origin in-app path.
 *
 * Returns the `pathname + search + hash` of `raw` when it resolves onto the
 * current origin over http(s); otherwise returns `fallback` (default "/").
 * This rejects absolute cross-origin URLs, protocol-relative "//host",
 * backslash-obfuscated "/\\host", and non-http schemes such as "javascript:".
 *
 * The verdict is taken on the value the navigation receives, not on `raw`: the
 * navigation resolves the RETURNED path once more, and a normalised pathname
 * may begin with "//", which a second resolution reads as protocol-relative.
 * So the returned path is resolved again and must land on the same origin.
 */
export function safeInternalPath(raw: string | null | undefined, fallback: string = DEFAULT_FALLBACK): string {
  if (!raw) return fallback;
  const origin = typeof window !== "undefined" ? window.location.origin : "http://localhost";
  let url: URL;
  try {
    url = new URL(raw, origin);
  } catch {
    return fallback;
  }
  if (url.origin !== origin) return fallback;
  if (url.protocol !== "http:" && url.protocol !== "https:") return fallback;
  const path = `${url.pathname}${url.search}${url.hash}`;
  if (!path.startsWith("/") || path.startsWith("//") || path.startsWith("/\\")) return fallback;
  return new URL(path, origin).origin === origin ? path : fallback;
}

/**
 * Resolve the post-auth navigation target after a login/registration flow.
 *
 * Both `flowReturnTo` (from the flow response) and `queryReturnTo` (from the
 * `?return_to=` query param) are caller-supplied, so each is constrained to a
 * same-origin in-app path (CWE-601). The flow value takes precedence; when it is
 * absent or off-origin we drop to the query value, and finally to `fallback`.
 *
 * Shared by Login and Register so the same-origin guarantee cannot drift between
 * the two auth entry points.
 */
export function resolvePostAuthTarget(
  flowReturnTo: string | null | undefined,
  queryReturnTo: string | null | undefined,
  fallback: string = DEFAULT_FALLBACK,
): string {
  return safeInternalPath(flowReturnTo, safeInternalPath(queryReturnTo, fallback));
}
