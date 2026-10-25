export interface User {
	id: number;
	username: string;
	is_platform_admin: boolean;
	is_active: boolean;
	created_at: string;
	updated_at: string;
}

export interface Session {
	user: User;
	csrf_token: string;
}

export type AccessLevel = "view_only" | "view_edit";

export interface MyDatabase {
	connection_id: number;
	name: string;
	host: string;
	database_name: string;
	access_level: AccessLevel;
}

export interface Group {
	id: number;
	name: string;
	description: string;
	created_at: string;
	updated_at: string;
}

export interface Grant {
	id: number;
	group_id: number;
	connection_id: number;
	access_level: AccessLevel;
	created_at: string;
	updated_at: string;
}

export interface GroupDetail extends Group {
	members: User[];
	grants: Grant[];
}

export interface DatabaseConnection {
	id: number;
	name: string;
	engine: string;
	host: string;
	port: number;
	database_name: string;
	username: string;
	ssl_mode: string;
	is_enabled: boolean;
	has_password: boolean;
	created_at: string;
	updated_at: string;
}

export interface ApiOptions {
	method?: string;
	body?: unknown;
	cookie?: string;
	csrf?: string;
}

export interface ApiResult<T> {
	status: number;
	data: T;
	setCookies: string[];
}

export class ApiError extends Error {
	status: number;
	code: string;
	fields?: Record<string, string>;

	constructor(status: number, code: string, message: string, fields?: Record<string, string>) {
		super(message);
		this.name = "ApiError";
		this.status = status;
		this.code = code;
		this.fields = fields;
	}
}

export async function api<T>(path: string, options: ApiOptions = {}): Promise<ApiResult<T>> {
	const base = process.env.SERVER_URL || import.meta.env.SERVER_URL || "http://localhost:8080";
	const headers = new Headers({ Accept: "application/json" });
	if (options.body !== undefined) {
		headers.set("Content-Type", "application/json");
	}
	if (options.cookie) {
		headers.set("Cookie", options.cookie);
	}
	if (options.csrf) {
		headers.set("X-CSRF-Token", options.csrf);
	}

	const response = await fetch(new URL(`/api${path}`, base), {
		method: options.method ?? "GET",
		headers,
		body: options.body === undefined ? undefined : JSON.stringify(options.body),
		redirect: "manual",
	});

	const getSetCookie = (response.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie;
	const setCookies = getSetCookie ? getSetCookie.call(response.headers) : [];
	const text = await response.text();
	let parsed: unknown;
	if (text) {
		try {
			parsed = JSON.parse(text);
		} catch {
			parsed = text;
		}
	}

	if (!response.ok) {
		const detail = (parsed as { error?: { code?: string; message?: string; fields?: Record<string, string> } } | undefined)?.error;
		throw new ApiError(
			response.status,
			detail?.code ?? "request_failed",
			detail?.message ?? `Request failed with status ${response.status}`,
			detail?.fields,
		);
	}

	return { status: response.status, data: parsed as T, setCookies };
}

export interface SchemaColumn {
	name: string;
	type: string;
	is_nullable: boolean;
	ordinal_position: number;
	column_default?: string | null;
	character_maximum_length?: number | null;
}

export interface SchemaConstraint {
	name: string;
	type: string;
	columns: string[];
	referenced_table?: string | null;
	referenced_columns?: string[] | null;
}

export interface SchemaIndex {
	name: string;
	columns: string[];
	is_unique: boolean;
}

export interface SchemaTable {
	name: string;
	columns: SchemaColumn[];
	constraints?: SchemaConstraint[];
	indexes?: SchemaIndex[];
}

export interface SchemaTables {
	schema: string;
	tables: SchemaTable[];
}

export interface DatabaseSchema {
	schemas: SchemaTables[];
}

export interface QueryResult {
	columns: string[];
	rows: (unknown | null)[][];
}

export interface Migration {
	id: number;
	connection_id: number;
	version: number;
	sql: string;
	executor_id: number;
	executor_username: string;
	status: "pending" | "success" | "failed";
	executed_at: string;
}

export interface CreateMigrationRequest {
	sql: string;
}

export interface TableCount {
	schema: string;
	table: string;
	columns: number;
	estimate: number | null;
}

export interface MutationResult {
	affected: number;
}

export interface SchemaChange {
	object_type: "table" | "column" | "constraint" | "index";
	object_id: string;
	action: "added" | "removed" | "changed";
	detail?: string | null;
}

export interface MigrationDiff {
	migration_id: number;
	status: "pending" | "success" | "failed";
	changes: SchemaChange[];
}

export interface ExecuteQueryRequest {
	sql: string;
	limit?: number;
	offset?: number;
}
