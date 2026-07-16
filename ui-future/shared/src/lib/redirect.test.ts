import { safeInternalPath, resolvePostAuthTarget } from "./redirect";

// jsdom serves window.location.origin as http://localhost.

describe("safeInternalPath", () => {
  it("passes through a same-origin relative path", () => {
    expect(safeInternalPath("/vpc/networks")).toBe("/vpc/networks");
  });

  it("preserves query string and hash of a relative path", () => {
    expect(safeInternalPath("/iam/users?flow=1#tab")).toBe("/iam/users?flow=1#tab");
  });

  it("collapses a same-origin absolute URL to its path", () => {
    expect(safeInternalPath("http://localhost/compute/instances")).toBe("/compute/instances");
  });

  it("falls back for a cross-origin absolute URL", () => {
    expect(safeInternalPath("https://evil.example/steal")).toBe("/");
  });

  it("falls back for a protocol-relative target", () => {
    expect(safeInternalPath("//evil.example")).toBe("/");
  });

  it("falls back for a backslash-obfuscated protocol-relative target", () => {
    expect(safeInternalPath("/\\evil.example")).toBe("/");
  });

  it("falls back for a javascript: scheme", () => {
    expect(safeInternalPath("javascript:alert(1)")).toBe("/");
  });

  it("falls back for null / empty input", () => {
    expect(safeInternalPath(null)).toBe("/");
    expect(safeInternalPath(undefined)).toBe("/");
    expect(safeInternalPath("")).toBe("/");
  });

  it("honours a custom fallback", () => {
    expect(safeInternalPath("https://evil.example", "/home")).toBe("/home");
  });

  it("falls back for a leading-whitespace absolute URL (parser trims the space)", () => {
    expect(safeInternalPath(" https://evil.example")).toBe("/");
  });

  it("keeps a bare token as a same-origin in-app path (not a cross-origin hop)", () => {
    // "evil.example" resolves against our own origin, so it is an in-app path,
    // never a cross-origin redirect — returning it is safe.
    expect(safeInternalPath("evil.example")).toBe("/evil.example");
  });
});

describe("resolvePostAuthTarget", () => {
  it("prefers a same-origin flow return_to", () => {
    expect(resolvePostAuthTarget("/iam/users", "/dashboard", "/")).toBe("/iam/users");
  });

  it("falls back to the query return_to when the flow value is absent", () => {
    expect(resolvePostAuthTarget(null, "/vpc/networks", "/")).toBe("/vpc/networks");
  });

  it("falls back to the query return_to when the flow value is off-origin", () => {
    // The flow-supplied value is caller-controlled; an off-origin one must not
    // become an open redirect — we drop to the (already-safe) query value.
    expect(resolvePostAuthTarget("https://evil.example/steal", "/vpc/networks", "/")).toBe("/vpc/networks");
  });

  it("falls back to the fallback when both return_to sources are absent", () => {
    expect(resolvePostAuthTarget(null, null, "/dashboard")).toBe("/dashboard");
  });

  it("neutralizes an off-origin flow return_to to the fallback (Register: no query source)", () => {
    // Register passes null for the query source, so an off-origin flow return_to
    // must resolve to the post-registration fallback, never off-origin.
    expect(resolvePostAuthTarget("//evil.example", null, "/dashboard")).toBe("/dashboard");
    expect(resolvePostAuthTarget("https://evil.example/x", null, "/dashboard")).toBe("/dashboard");
  });

  it("neutralizes a javascript: scheme flow return_to", () => {
    expect(resolvePostAuthTarget("javascript:alert(1)", null, "/dashboard")).toBe("/dashboard");
  });

  it("passes a same-origin absolute flow return_to through as its path", () => {
    expect(resolvePostAuthTarget("http://localhost/iam/access", null, "/dashboard")).toBe("/iam/access");
  });
});
