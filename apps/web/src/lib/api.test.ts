import { describe, expect, it } from "vitest";

vi.stubGlobal(
	"fetch",
	async (input: RequestInfo, init?: RequestInit) => {
		const url = typeof input === "string" ? input : input?.url ?? "";
		if (url.includes("/auth/session")) {
			return new Response(
				JSON.stringify({
					status: 200,
					data: { user: { id: 1, username: "testuser" } },
					setCookies: [{ name: "session", value: "abc123", httpOnly: true, path: "/", maxAge: 3600 }],
				}),
				{ status: 200, headers: { "content-type": "application/json" } }
			);
		}
		if (url.includes("/databases")) {
			return new Response(
				JSON.stringify({
					status: 200,
					data: {
						schemas: [
							{
								tables: [
									{
										name: "test_table",
										columns: [{ name: "id", type: "int" }, { name: "label", type: "text" }],
										indexes: [],
										constraints: [{ type: "PRIMARY KEY", name: "pk_test_table" }],
									},
								],
							},
						],
					},
					setCookies: [],
				}),
				{ status: 200, headers: { "content-type": "application/json" } }
			);
		}
		// Default success response
		return new Response(
			JSON.stringify({ status: 200, data: null, setCookies: [] }),
			{ status: 200, headers: { "content-type": "application/json" } }
		);
	}
);

describe("api fetch mock", () => {
	it("returns session data via stubbed fetch", async () => {
		const res = await globalThis.fetch("/auth/session", { method: "GET" });
		const json = await res.json();
		expect(json.status).toBe(200);
		expect(json.data).toBeDefined();
		expect(json.data.user).toEqual({ id: 1, username: "testuser" });
	});

	it("returns database schema via stubbed fetch", async () => {
		const res = await globalThis.fetch("/databases/1/schema", { method: "GET" });
		const json = await res.json();
		expect(json.status).toBe(200);
		expect(Array.isArray(json.data?.schemas)).toBe(true);
	});

	it("handles unknown path gracefully via stubbed fetch", async () => {
		const res = await globalThis.fetch("/some/unknown/path", { method: "GET" });
		const json = await res.json();
		expect(json.status).toBe(200);
		expect(json.data).toBeNull();
	});