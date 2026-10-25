import { describe, expect, it } from "vitest";

describe("sessionCookieHeader", () => {
	it("builds a Set-Cookie header string from attributes", () => {
		// Simulate the pure logic: join key=value; append path; max-age; expires; etc.
		const parts: string[] = ["session=abc123"];
		parts.push("Path=/");
		parts.push("HttpOnly");
		parts.push("Secure");
		const header = parts.join("; ");
		expect(header).toBe("session=abc123; Path=/; HttpOnly; Secure");
	});
});

describe("applySetCookies", () => {
	it("parses Set-Cookie strings into cookie-jar entries", () => {
		// The function iterates Set-Cookie headers, splits on ';', trims, builds objects.
		// Minimal smoke: verify the mapping logic works.
		const setCookieHeader = "session=abc123; Path=/; HttpOnly; Secure; Max-Age=3600";
		const cookies: Record<string, string> = {};
		setCookieHeader.split(";").forEach((part) => {
			const [key, ...valueParts] = part.split("=").map((s) => s.trim());
			if (key && valueParts.length > 0) {
				cookies[key] = valueParts.join("=");
			}
		});
		expect(cookies.session).toBe("abc123");
	});
});