import { describe, expect, it } from "vitest";

describe("actionResults logic", () => {
	it("succeeded is true when any action has ok === true", () => {
		const actions = [{ ok: true }, { ok: false }];
		const results = actions.map((action) => ({ data: { ok: action.ok } }));
		const succeeded = results.some((r) => r.data?.ok === true);
		expect(succeeded).toBe(true);
	});

	it("succeeded is false when all actions have ok === false", () => {
		const actions = [{ ok: false }, { ok: false }];
		const results = actions.map((action) => ({ data: { ok: action.ok } }));
		const succeeded = results.some((r) => r.data?.ok === true);
		expect(succeeded).toBe(false);
	});

	it("error is the first non-null error from failing actions", () => {
		const actions = [{ ok: false, error: "bad input" }, { ok: false }];
		const results = actions.map((action) => ({
			data: action.ok === false ? { ok: false, error: action.error ?? null } : { ok: true },
		}));
		const error =
			results
				.map((r) => (r.data?.ok === false ? r.data.error : null))
				.find(Boolean) ?? null;
		expect(error).toBe("bad input");
	});
});