import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/lib/api";
import { loadSession, logOutSession } from "./session-ops";

function stubFetch(response: Response) {
	const fetchMock = vi.fn(async () => response);
	vi.stubGlobal("fetch", fetchMock);
	return fetchMock;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe("loadSession", () => {
	it("returns the session on success", async () => {
		stubFetch(new Response(JSON.stringify({ user: { id: 1, username: "a", is_platform_admin: true, is_active: true }, csrf_token: "t" }), { status: 200 }));

		const session = await loadSession();
		expect(session).toEqual({ user: { id: 1, username: "a", is_platform_admin: true, is_active: true }, csrf_token: "t" });
	});

	it("resolves null on 401 (no session)", async () => {
		stubFetch(new Response(JSON.stringify({ error: { code: "unauthorized", message: "authentication required" } }), { status: 401 }));

		expect(await loadSession()).toBeNull();
	});

	it("throws for non-401 failures", async () => {
		stubFetch(new Response(JSON.stringify({ error: { code: "boom", message: "nope" } }), { status: 500 }));

		await expect(loadSession()).rejects.toThrow(ApiError);
	});
});

describe("logOutSession", () => {
	it("POSTs /api/auth/logout with the CSRF token", async () => {
		const fetchMock = stubFetch(new Response(null, { status: 204 }));

		await logOutSession("csrf-123");

		const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
		expect(url).toBe("/api/auth/logout");
		expect(init.method).toBe("POST");
		expect((init.headers as Headers).get("X-CSRF-Token")).toBe("csrf-123");
	});

	it("POSTs without a CSRF token when none is available", async () => {
		const fetchMock = stubFetch(new Response(null, { status: 204 }));

		await logOutSession(undefined);

		const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
		expect((init.headers as Headers).get("X-CSRF-Token")).toBeNull();
	});
});
