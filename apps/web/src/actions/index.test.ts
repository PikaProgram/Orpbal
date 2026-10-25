import { describe, expect, it } from "vitest";

import { splitRowFields } from "./index";

describe("splitRowFields", () => {
	it("splits col.* values from pk.* identifiers", () => {
		const { values, pk } = splitRowFields({
			"col.label": "new label",
			"col.name": "new name",
			"pk.id": "42",
		});

		expect(values).toEqual({ label: "new label", name: "new name" });
		expect(pk).toEqual({ id: "42" });
	});

	it("omits empty column values but keeps primary keys as posted", () => {
		const { values, pk } = splitRowFields({
			"col.label": "",
			"col.name": "kept",
			"pk.id": "",
		});

		expect(values).toEqual({ name: "kept" });
		expect(pk).toEqual({ id: "" });
	});

	it("ignores fields without a col./pk. prefix", () => {
		const { values, pk } = splitRowFields({ csrf_token: "t", label: "x" });

		expect(values).toEqual({});
		expect(pk).toEqual({});
	});
});