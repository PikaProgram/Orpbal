import type { AstroCookies } from "astro";
import { describe, expect, it } from "vitest";

import { SESSION_COOKIE, applySetCookies, sessionCookieHeader } from "./session";

type SetCall = { name: string; value: string; options: Record<string, unknown> };

function fakeCookies() {
	const jar = new Map<string, string>();
	const sets: SetCall[] = [];
	const deletes: string[] = [];
	const cookies = {
		get: (name: string) => (jar.has(name) ? { value: jar.get(name) } : undefined),
		set: (name: string, value: string, options: Record<string, unknown>) => {
			sets.push({ name, value, options });
			jar.set(name, value);
		},
		delete: (name: string) => {
			deletes.push(name);
			jar.delete(name);
		},
	} as unknown as AstroCookies;
	return { cookies, sets, deletes };
}

describe("sessionCookieHeader", () => {
	it("returns undefined when the session cookie is absent", () => {
		expect(sessionCookieHeader(fakeCookies().cookies)).toBeUndefined();
	});

	it("returns the request header form of the session cookie", () => {
		const { cookies } = fakeCookies();
		applySetCookies(cookies, [`${SESSION_COOKIE}=abc123; Path=/; HttpOnly`]);

		expect(sessionCookieHeader(cookies)).toBe(`${SESSION_COOKIE}=abc123`);
	});
});

describe("applySetCookies", () => {
	it("translates attributes into cookie options", () => {
		const { cookies, sets } = fakeCookies();

		applySetCookies(cookies, ["orpbal_session=abc; Path=/app; HttpOnly; Secure; SameSite=Strict; Max-Age=3600"]);

		expect(sets).toEqual([
			{
				name: "orpbal_session",
				value: "abc",
				options: { path: "/app", httpOnly: true, secure: true, sameSite: "strict", maxAge: 3600 },
			},
		]);
	});

	it("deletes the cookie when max-age is zero or negative", () => {
		const { cookies, deletes, sets } = fakeCookies();

		applySetCookies(cookies, ["orpbal_session=; Path=/; Max-Age=0", "other=; Path=/; Max-Age=-1"]);

		expect(deletes).toEqual(["orpbal_session", "other"]);
		expect(sets).toEqual([]);
	});

	it("skips malformed Set-Cookie values", () => {
		const { cookies, sets, deletes } = fakeCookies();

		applySetCookies(cookies, ["garbage-without-equals"]);

		expect(sets).toEqual([]);
		expect(deletes).toEqual([]);
	});
});