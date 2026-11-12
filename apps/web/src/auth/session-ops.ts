import { ApiError, api, type Session } from "@/lib/api";

/**
 * Loads the current session. A 401 means "no session" and resolves to
 * null; any other failure propagates.
 */
export async function loadSession(): Promise<Session | null> {
	try {
		const result = await api<Session>("/auth/session");
		return result.data;
	} catch (error) {
		if (error instanceof ApiError && error.status === 401) return null;
		throw error;
	}
}

export async function logOutSession(csrf?: string): Promise<void> {
	await api("/auth/logout", { method: "POST", csrf });
}
