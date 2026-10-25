import { describe, expect, it } from "vitest";

// Inline the splitRowFields function (same logic as exported from actions/index.ts)
function splitRowFields(input: Record<string, unknown>) {
	const values: Record<string, string> = {};
	const pk: Record<string, string> = {};
	for (const [key, value] of Object.entries(input)) {
		if (key.startsWith("col.")) {
			const fieldName = key.slice(4);
			if (value !== undefined && value !== null && value !== "") {
				values[fieldName] = String(value);
			}
		} else if (key.startsWith("pk.")) {
			const fieldName = key.slice(3);
			if (value !== undefined && value !== null && value !== "") {
				pk[fieldName] = String(value);
			}
		}
	}
	return { values, pk };
}

describe("splitRowFields", () => {
	it("separates col. and pk. fields from input", () => {
		const input: Record<string, unknown> = {
			colLabel: "new label",
			colName: "new name",
			pkId: "42",
		};
		const { values, pk } = splitRowFields(input);
		expect(values).toEqual({ label: "new label", name: "new name" });
		expect(pk).toEqual({ id: "42" });
	});

	it("omits empty values", () => {
		const input: Record<string, unknown> = {
			colLabel: "",
			colName: "kept",
			pkId: "",
		};
		const { values, pk } = splitRowFields(input);
		expect(values).toEqual({ name: "kept" });
		expect(pk).toEqual({});
	});