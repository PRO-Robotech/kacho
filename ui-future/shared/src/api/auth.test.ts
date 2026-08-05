// Direction lock for the auth client's case conversion.
//
// The transport contract is asymmetric and easy to get backwards: the Kachō REST
// surface speaks camelCase, the UI speaks snake_case. So a REQUEST body is
// snake→camel and a RESPONSE is camel→snake. Applying the response transformer to
// a request produces keys no message has; the edge parses with DiscardUnknown and
// drops them without a word.

import { authApi } from "./auth";

type Captured = { url: string; init: RequestInit };

function captureFetch(payload: unknown): { calls: Captured[] } {
  const calls: Captured[] = [];
  globalThis.fetch = ((url: string, init: RequestInit) => {
    calls.push({ url: String(url), init });
    return Promise.resolve({
      ok: true,
      status: 200,
      statusText: "OK",
      text: () => Promise.resolve(JSON.stringify(payload)),
    } as unknown as Response);
  }) as unknown as typeof fetch;
  return { calls };
}

describe("authApi", () => {
  const originalFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("reads the session without sending a body", () => {
    // These are reads. No request body exists on this surface, so no request-side
    // conversion may be configured for one either — a body branch with no caller
    // is an untested claim about the wire.
    const { calls } = captureFetch({ user: { id: "usr-1", subject_type: "user" } });
    return authApi.me().then(() => {
      expect(calls).toHaveLength(1);
      expect(calls[0].url).toBe("/iam/v1/auth/me");
      expect(calls[0].init.method).toBe("GET");
      expect(calls[0].init.body).toBeUndefined();
      expect(calls[0].init.credentials).toBe("include");
    });
  });

  it("adapts the whoami response camelCase → snake_case", () => {
    const { calls } = captureFetch({
      subject: "user:usr-1",
      userId: "usr-1",
      displayName: "Ada",
      systemAdmin: true,
      clusterViewer: false,
      accounts: [{ accountId: "acc-1", accountName: "main", roles: ["admin"] }],
    });
    return authApi.whoami().then((who) => {
      expect(calls[0].init.method).toBe("GET");
      expect(who.user_id).toBe("usr-1");
      expect(who.display_name).toBe("Ada");
      expect(who.system_admin).toBe(true);
      expect(who.accounts).toEqual([{ account_id: "acc-1", account_name: "main", roles: ["admin"] }]);
    });
  });
});
