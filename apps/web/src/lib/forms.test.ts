import type { AstroGlobal } from "astro";
import { describe, expect, it } from "vitest";

import { actionResults } from "./forms";

type ActionResult = { data?: { ok: boolean; error?: string }; error?: unknown } | null;

// Astro hands back one stored result per action, in the order the page asks
// for them, so the mock shifts the scripted results.
function astroWith(resultQueue: ActionResult[]): AstroGlobal {
	return {
		getActionResult: () => resultQueue.shift() ?? null,
	} as unknown as AstroGlobal;
}

const anyAction = {} as Parameters<AstroGlobal["getActionResult"]>[0];

describe("actionResults", () => {
	it("reports success and no error when the action succeeded", () => {
		const result = actionResults(astroWith([{ data: { ok: true } }]), [anyAction]);

		expect(result).toEqual({ succeeded: true, error: null });
	});

	it("still surfaces a failure message when another action succeeded", () => {
		const result = actionResults(astroWith([{ data: { ok: false, error: "nope" } }, { data: { ok: true } }]), [
			anyAction,
			anyAction,
		]);

		expect(result).toEqual({ succeeded: true, error: "nope" });
	});

	it("surfaces the first field error from a failed action", () => {
		const result = actionResults(astroWith([{ data: { ok: false, error: "username is taken" } }]), [anyAction]);

		expect(result).toEqual({ succeeded: false, error: "username is taken" });
	});

	it("falls back to a generic message when the action threw", () => {
		const result = actionResults(astroWith([{ error: new Error("boom") }]), [anyAction]);

		expect(result).toEqual({ succeeded: false, error: "Check the form values and try again." });
	});

	it("returns no error when no action ran", () => {
		const result = actionResults(astroWith([null, null]), [anyAction, anyAction]);

		expect(result).toEqual({ succeeded: false, error: null });
	});
});