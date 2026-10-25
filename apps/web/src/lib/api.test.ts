import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, api } from "./api";

function stubFetch(response: Response) {
	const fetchMock = vi.fn(async () => response);
	vi.stubGlobal("fetch", fetchMock);
	return fetchMock;
}

afterEach(() => {
	vi.unstubAllGlobals();
});

describe("api", () => {
	it("parses a JSON success body and returns status plus data", async () => {
		stubFetch(
			new Response(JSON.stringify({ id: 7 }), {
				status: 200,
				headers: { "content-type": "application/json" },
			}),
		);

		const result = await api<{ id: number }>("/databases/7");

		expect(result).toEqual({ status: 200, data: { id: 7 }, setCookies: [] });
	});

	it("returns plain text bodies unchanged when they are not JSON", async () => {
		stubFetch(new Response("not json", { status: 200, headers: { "content-type": "text/plain" } }));

		const result = await api<string>("/health");

		expect(result.data).toBe("not json");
	});

	it("throws ApiError carrying the server error code, message and fields", async () => {
		stubFetch(
			new Response(
				JSON.stringify({
					error: { code: "invalid_input", message: "bad sql", fields: { sql: "must be a SELECT" } },
				}),
				{ status: 400, headers: { "content-type": "application/json" } },
			),
		);

		await expect(api("/databases/7/migrations", { method: "POST", body: { sql: "DROP TABLE x" } })).rejects.toMatchObject({
			name: "ApiError",
			status: 400,
			code: "invalid_input",
			message: "bad sql",
			fields: { sql: "must be a SELECT" },
		});
	});

	it("falls back to a generic ApiError when the body has no error detail", async () => {
		stubFetch(new Response(null, { status: 403 }));

		const error = await api("/admin/users").catch((caught: unknown) => caught);

		expect(error).toBeInstanceOf(ApiError);
		expect((error as ApiError).status).toBe(403);
		expect((error as ApiError).code).toBe("request_failed");
		expect((error as ApiError).message).toBe("Request failed with status 403");
	});

	it("sends the path under /api with the configured method and CSRF header", async () => {
		const fetchMock = stubFetch(new Response("{}", { status: 200 }));

		await api("/databases/7/migrations/2", { method: "PATCH", csrf: "token-123", body: { sql: "SELECT 1" } });

		const [url, init] = fetchMock.mock.calls[0] as unknown as [URL, RequestInit];
		expect(url.pathname).toBe("/api/databases/7/migrations/2");
		expect(init.method).toBe("PATCH");
		expect((init.headers as Headers).get("X-CSRF-Token")).toBe("token-123");
		expect(init.body).toBe(JSON.stringify({ sql: "SELECT 1" }));
	});
});