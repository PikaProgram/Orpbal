import type { AstroCookies, AstroGlobal } from "astro";

import { ApiError, api, type Session } from "./api";

export const SESSION_COOKIE = "orpbal_session";

const SAME_SITE: Record<string, "lax" | "strict" | "none"> = {
	lax: "lax",
	strict: "strict",
	none: "none",
};

export function applySetCookies(cookies: AstroCookies, setCookies: string[]): void {
	for (const raw of setCookies) {
		const [pair, ...attributes] = raw.split(";");
		const separator = pair.indexOf("=");
		if (separator < 0) continue;

		const name = pair.slice(0, separator).trim();
		const value = pair.slice(separator + 1).trim();
		const options: {
			path: string;
			maxAge?: number;
			httpOnly?: boolean;
			secure?: boolean;
			sameSite?: "lax" | "strict" | "none";
		} = { path: "/" };

		for (const attribute of attributes) {
			const [rawKey, rawValue = ""] = attribute.split("=");
			const key = rawKey.trim().toLowerCase();
			if (key === "path") options.path = rawValue.trim() || "/";
			if (key === "max-age") options.maxAge = Number(rawValue.trim());
			if (key === "httponly") options.httpOnly = true;
			if (key === "secure") options.secure = true;
			if (key === "samesite") options.sameSite = SAME_SITE[rawValue.trim().toLowerCase()] ?? "lax";
		}

		if (options.maxAge !== undefined && options.maxAge <= 0) {
			cookies.delete(name, { path: options.path });
		} else {
			cookies.set(name, value, options);
		}
	}
}

export function sessionCookieHeader(cookies: AstroCookies): string | undefined {
	const value = cookies.get(SESSION_COOKIE)?.value;
	return value ? `${SESSION_COOKIE}=${value}` : undefined;
}

export async function readSession(cookies: AstroCookies): Promise<Session | null> {
	const cookie = sessionCookieHeader(cookies);
	if (!cookie) return null;
	try {
		const result = await api<Session>("/auth/session", { cookie });
		applySetCookies(cookies, result.setCookies);
		return result.data;
	} catch (error) {
		if (error instanceof ApiError && error.status === 401) {
			cookies.delete(SESSION_COOKIE, { path: "/" });
			return null;
		}
		throw error;
	}
}

export interface AuthContext {
	session: Session;
	cookie: string;
}

export async function requireSession(Astro: AstroGlobal): Promise<AuthContext | Response> {
	const session = await readSession(Astro.cookies);
	if (!session) return Astro.redirect("/login");
	const cookie = sessionCookieHeader(Astro.cookies);
	if (!cookie) return Astro.redirect("/login");
	return { session, cookie };
}

export async function requireAdmin(Astro: AstroGlobal): Promise<AuthContext | Response> {
	const auth = await requireSession(Astro);
	if (auth instanceof Response) return auth;
	if (!auth.session.user.is_platform_admin) return Astro.redirect("/databases");
	return auth;
}
