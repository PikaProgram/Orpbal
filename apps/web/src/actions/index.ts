import { defineAction } from "astro:actions";
import type { ActionAPIContext } from "astro:actions";
import { z } from "zod";

import { ApiError, api } from "@/lib/api";
import { SESSION_COOKIE, applySetCookies, sessionCookieHeader } from "@/lib/session";

async function call<T>(context: ActionAPIContext, path: string, method: string, body?: unknown, csrf?: string) {
	const result = await api<T>(path, {
		method,
		body,
		cookie: sessionCookieHeader(context.cookies),
		csrf,
	});
	applySetCookies(context.cookies, result.setCookies);
	return result;
}

async function run(action: () => Promise<void>) {
	try {
		await action();
		return { ok: true as const };
	} catch (error) {
		if (!(error instanceof ApiError)) {
			console.error(error);
		}
		return { ok: false as const, error: error instanceof ApiError ? error.message : "Request failed" };
	}
}

const csrf = z.string().min(1);
const id = z.coerce.number().int().positive();
const sslMode = z.enum(["disable", "require", "verify-ca", "verify-full"]).default("disable");
const accessLevel = z.enum(["view_only", "view_edit"]);

export const server = {
	login: defineAction({
		accept: "form",
		input: z.object({
			username: z.string().min(1),
			password: z.string().min(1),
		}),
		handler: (input, context) =>
			run(async () => {
				await call(context, "/auth/login", "POST", {
					username: input.username,
					password: input.password,
				});
			}),
	}),

	logout: defineAction({
		accept: "form",
		input: z.object({ csrf_token: csrf }),
		handler: (input, context) =>
			run(async () => {
				await call(context, "/auth/logout", "POST", undefined, input.csrf_token);
				context.cookies.delete(SESSION_COOKIE, { path: "/" });
			}),
	}),

	changePassword: defineAction({
		accept: "form",
		input: z.object({
			current_password: z.string().min(1),
			new_password: z.string().min(1),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					"/auth/password",
					"POST",
					{
						current_password: input.current_password,
						new_password: input.new_password,
					},
					input.csrf_token,
				);
			}),
	}),

	createUser: defineAction({
		accept: "form",
		input: z.object({
			username: z.string().min(1),
			password: z.string().min(1),
			is_platform_admin: z.enum(["false", "true"]).default("false"),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					"/admin/users",
					"POST",
					{
						username: input.username,
						password: input.password,
						is_platform_admin: input.is_platform_admin === "true",
					},
					input.csrf_token,
				);
			}),
	}),

	setUserActive: defineAction({
		accept: "form",
		input: z.object({
			id,
			active: z.enum(["false", "true"]),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/users/${input.id}`,
					"PATCH",
					{ is_active: input.active === "true" },
					input.csrf_token,
				);
			}),
	}),

	setUserAdmin: defineAction({
		accept: "form",
		input: z.object({
			id,
			admin: z.enum(["false", "true"]),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/users/${input.id}`,
					"PATCH",
					{ is_platform_admin: input.admin === "true" },
					input.csrf_token,
				);
			}),
	}),

	resetUserPassword: defineAction({
		accept: "form",
		input: z.object({
			id,
			new_password: z.string().min(1),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/users/${input.id}/password`,
					"POST",
					{ new_password: input.new_password },
					input.csrf_token,
				);
			}),
	}),

	createGroup: defineAction({
		accept: "form",
		input: z.object({
			name: z.string().min(1),
			description: z.string().optional().default(""),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					"/admin/groups",
					"POST",
					{ name: input.name, description: input.description },
					input.csrf_token,
				);
			}),
	}),

	updateGroup: defineAction({
		accept: "form",
		input: z.object({
			id,
			name: z.string().min(1),
			description: z.string().optional().default(""),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/groups/${input.id}`,
					"PATCH",
					{ name: input.name, description: input.description },
					input.csrf_token,
				);
			}),
	}),

	deleteGroup: defineAction({
		accept: "form",
		input: z.object({ id, csrf_token: csrf }),
		handler: (input, context) =>
			run(async () => {
				await call(context, `/admin/groups/${input.id}`, "DELETE", undefined, input.csrf_token);
			}),
	}),

	addGroupMember: defineAction({
		accept: "form",
		input: z.object({ group_id: id, user_id: id, csrf_token: csrf }),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/groups/${input.group_id}/members/${input.user_id}`,
					"PUT",
					undefined,
					input.csrf_token,
				);
			}),
	}),

	removeGroupMember: defineAction({
		accept: "form",
		input: z.object({ group_id: id, user_id: id, csrf_token: csrf }),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/groups/${input.group_id}/members/${input.user_id}`,
					"DELETE",
					undefined,
					input.csrf_token,
				);
			}),
	}),

	createConnection: defineAction({
		accept: "form",
		input: z.object({
			name: z.string().min(1),
			host: z.string().min(1),
			port: z.coerce.number().int().min(1).max(65535).default(5432),
			database_name: z.string().min(1),
			username: z.string().min(1),
			password: z.string().optional().default(""),
			ssl_mode: sslMode,
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				const body: Record<string, unknown> = {
					name: input.name,
					host: input.host,
					port: input.port,
					database_name: input.database_name,
					username: input.username,
					ssl_mode: input.ssl_mode,
				};
				if (input.password) body.password = input.password;
				await call(context, "/admin/databases", "POST", body, input.csrf_token);
			}),
	}),

	updateConnection: defineAction({
		accept: "form",
		input: z.object({
			id,
			name: z.string().min(1),
			host: z.string().min(1),
			port: z.coerce.number().int().min(1).max(65535),
			database_name: z.string().min(1),
			username: z.string().min(1),
			password: z.string().optional().default(""),
			ssl_mode: sslMode,
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				const body: Record<string, unknown> = {
					name: input.name,
					host: input.host,
					port: input.port,
					database_name: input.database_name,
					username: input.username,
					ssl_mode: input.ssl_mode,
				};
				if (input.password) body.password = input.password;
				await call(context, `/admin/databases/${input.id}`, "PATCH", body, input.csrf_token);
			}),
	}),

	setConnectionEnabled: defineAction({
		accept: "form",
		input: z.object({
			id,
			enabled: z.enum(["false", "true"]),
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/databases/${input.id}`,
					"PATCH",
					{ is_enabled: input.enabled === "true" },
					input.csrf_token,
				);
			}),
	}),

	setGrant: defineAction({
		accept: "form",
		input: z.object({
			group_id: id,
			connection_id: id,
			access_level: accessLevel,
			csrf_token: csrf,
		}),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/groups/${input.group_id}/grants/${input.connection_id}`,
					"PUT",
					{ access_level: input.access_level },
					input.csrf_token,
				);
			}),
	}),

	deleteGrant: defineAction({
		accept: "form",
		input: z.object({ group_id: id, connection_id: id, csrf_token: csrf }),
		handler: (input, context) =>
			run(async () => {
				await call(
					context,
					`/admin/groups/${input.group_id}/grants/${input.connection_id}`,
					"DELETE",
					undefined,
					input.csrf_token,
				);
			}),
	}),
};
