import { describe, it, expect, vi } from "vitest";

// These assert the behaviours that made the browser sign-in loop fail, so a regression is
// caught here rather than by a person staring at a sign-in screen.
//
// The module under test holds session state at module scope (csrfToken, browserSessionMode),
// so every case imports a FRESH instance via vi.resetModules(); otherwise state leaks between
// cases and the suite would assert the wrong branch.

type Route = { status: number; json?: unknown };

function installFetch(queue: Route[]) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  const fn = vi.fn(async (input: unknown, init: RequestInit = {}) => {
    calls.push({ url: String(input), init });
    const route = queue.shift() ?? { status: 200, json: {} };
    return {
      ok: route.status >= 200 && route.status < 300,
      status: route.status,
      json: async () => {
        if (route.json === undefined) throw new Error("no JSON body");
        return route.json;
      },
      text: async () => JSON.stringify(route.json ?? null),
    } as unknown as Response;
  });
  (globalThis as unknown as { fetch: unknown }).fetch = fn;
  return calls;
}

async function freshApi() {
  vi.resetModules();
  return await import("./api");
}

const IDENTITY = {
  id: "mdil",
  org_id: "xsama",
  role: "admin" as const,
  name: "Mohammed Ismail",
};

const headersOf = (init: RequestInit) => init.headers as Record<string, string>;

describe("browser session detection", () => {
  it("treats ONLY a 401 refusal as local mode", async () => {
    installFetch([{ status: 401 }]);
    const mod = await freshApi();
    expect(await mod.getSession()).toBeNull();
    // No session token should have been invented for local mode.
    expect(mod.getToken()).toBe("");
  });

  it("returns the authenticated payload from /auth/session", async () => {
    installFetch([
      {
        status: 200,
        json: {
          authenticated: true,
          identity: IDENTITY,
          csrf_token: "csrf-value",
          expires_at: "2026-09-18T19:46:59Z",
        },
      },
    ]);
    const mod = await freshApi();
    const s = await mod.getSession();
    // This is the contract the panel depends on; a renamed field would sign the user in and
    // still leave the UI on the SSO screen.
    expect(s?.authenticated).toBe(true);
    expect(s?.identity?.id).toBe("mdil");
    expect(s?.identity?.org_id).toBe("xsama");
    expect(s?.identity?.role).toBe("admin");
    expect(s?.csrf_token).toBe("csrf-value");
  });

  it("does NOT report a server error as local mode", async () => {
    installFetch([{ status: 500 }]);
    const mod = await freshApi();
    // Reporting a failure as "not configured" would show a development token form in a
    // deployment that has real sign-in, inviting a shared secret as a credential.
    await expect(mod.getSession()).rejects.toThrow();
  });

  it("does NOT report a non-JSON body as local mode", async () => {
    installFetch([{ status: 200 }]); // json() throws
    const mod = await freshApi();
    await expect(mod.getSession()).rejects.toThrow();
  });

  it("clears a stale development token once a session is in play", async () => {
    installFetch([
      { status: 200, json: { authenticated: true, identity: IDENTITY, csrf_token: "c" } },
    ]);
    const mod = await freshApi();
    mod.setToken("stale-dev-token");
    expect(mod.getToken()).toBe("stale-dev-token");
    await mod.getSession();
    // The server checks a bearer token BEFORE the cookie, so a leftover token would fail
    // verification and 401 every request even though the cookie is valid.
    expect(mod.getToken()).toBe("");
  });
});

describe("request authentication headers", () => {
  it("uses the cookie session after detection: CSRF header, no bearer", async () => {
    const calls = installFetch([
      { status: 200, json: { authenticated: true, identity: IDENTITY, csrf_token: "tok" } },
      { status: 200, json: { ok: true } },
    ]);
    const mod = await freshApi();
    await mod.getSession();
    await mod.api("/tasks", { method: "POST", body: "{}" });

    const h = headersOf(calls[1].init);
    expect(h["Authorization"]).toBeUndefined();
    expect(h["X-CSRF-Token"]).toBe("tok");
    expect(calls[1].init.credentials).toBe("same-origin");
  });

  it("uses the bearer token in local mode, with no CSRF header", async () => {
    const calls = installFetch([{ status: 401 }, { status: 200, json: { id: "mdil" } }]);
    const mod = await freshApi();
    const s = await mod.getSession();
    expect(s).toBeNull();
    mod.setToken("dev-token");
    await mod.api("/me");

    const h = headersOf(calls[1].init);
    expect(h["Authorization"]).toBe("Bearer dev-token");
    // A bearer token is set explicitly by the caller, so it needs no CSRF proof.
    expect(h["X-CSRF-Token"]).toBeUndefined();
  });
});

describe("loginUrl", () => {
  it("encodes the return path", async () => {
    const mod = await freshApi();
    expect(mod.loginUrl("/tasks")).toBe("/auth/login?return_to=%2Ftasks");
    expect(mod.loginUrl()).toBe("/auth/login?return_to=%2F");
  });
});

describe("logout", () => {
  it("returns the provider end-session URL and clears local state", async () => {
    installFetch([
      { status: 200, json: { authenticated: true, identity: IDENTITY, csrf_token: "tok" } },
      { status: 200, json: { logout_url: "https://auth.xsama.org/oidc/session/end" } },
    ]);
    const mod = await freshApi();
    await mod.getSession();
    const url = await mod.logout();
    expect(url).toBe("https://auth.xsama.org/oidc/session/end");
    expect(mod.getToken()).toBe("");
  });

  it("still clears local state when the server call fails", async () => {
    installFetch([{ status: 401 }, { status: 500 }]);
    const mod = await freshApi();
    await mod.getSession();
    mod.setToken("dev-token");
    const url = await mod.logout();
    // Leaving the token behind would let the next request look authenticated on this page
    // while the server still holds a live session.
    expect(url).toBe("");
    expect(mod.getToken()).toBe("");
  });
});
