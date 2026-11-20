import { z } from "zod";

import { ApiError, api } from "@/lib/api";

// ---- Schemas mirroring the M2 section of apps/server/api/openapi.yaml ----

export const migrationStatusSchema = z.enum([
	"draft",
	"pending_review",
	"approved",
	"queued",
	"running",
	"success",
	"failed",
	"partial",
	"cancelled",
]);
export type MigrationStatus = z.infer<typeof migrationStatusSchema>;

export const executionKindSchema = z.enum(["up", "rollback", "forward", "retry", "batch_revert"]);
export type ExecutionKind = z.infer<typeof executionKindSchema>;

export const approvalStatusSchema = z.enum(["pending", "approved", "rejected"]);
export type ApprovalStatus = z.infer<typeof approvalStatusSchema>;

export const jobStatusSchema = z.enum(["queued", "running", "success", "failed", "cancelled", "cancelling", "partial"]);
export type JobStatus = z.infer<typeof jobStatusSchema>;

export const batchConfigSchema = z.object({
	batch_size: z.number().int().min(1),
	cursor_column: z.string(),
	batch_table: z.string().nullable().optional(),
	min_id: z.number().int().optional(),
	max_id: z.number().int().optional(),
	template: z.string(),
});
export type BatchConfig = z.infer<typeof batchConfigSchema>;

export const executionSchema = z.object({
	id: z.number().int(),
	migration_id: z.number().int(),
	connection_id: z.number().int(),
	kind: executionKindSchema,
	status: migrationStatusSchema,
	actor_id: z.number().int(),
	actor_username: z.string(),
	sql_executed: z.string().optional(),
	error: z.string().nullable().optional(),
	reverted_execution_id: z.number().int().nullable().optional(),
	batch_cursor: z.number().int().nullable().optional(),
	batches_done: z.number().int().optional(),
	rows_done: z.number().int().optional(),
	created_at: z.string(),
	finished_at: z.string().nullable().optional(),
	snapshots: z
		.array(z.object({ phase: z.enum(["before", "after"]), captured_at: z.string().optional() }))
		.optional(),
});
export type Execution = z.infer<typeof executionSchema>;

export const approvalMigrationSummarySchema = z.object({
	id: z.number().int(),
	version: z.number().int(),
	sql_up: z.string(),
	status: migrationStatusSchema,
	author_username: z.string(),
});
export type ApprovalMigrationSummary = z.infer<typeof approvalMigrationSummarySchema>;

export const approvalSchema = z.object({
	id: z.number().int(),
	migration_id: z.number().int(),
	migration: approvalMigrationSummarySchema.nullable().optional(),
	group_id: z.number().int(),
	group_name: z.string(),
	leader_id: z.number().int(),
	leader_username: z.string(),
	status: approvalStatusSchema,
	decided_by_id: z.number().int().nullable().optional(),
	decided_at: z.string().nullable().optional(),
	comment: z.string().nullable().optional(),
	created_at: z.string(),
});
export type Approval = z.infer<typeof approvalSchema>;

export const migrationSchema = z.object({
	id: z.number().int(),
	connection_id: z.number().int(),
	version: z.number().int(),
	sql_up: z.string(),
	sql_down: z.string().nullable(),
	irreversible: z.boolean(),
	irreversible_reason: z.string().nullable(),
	status: migrationStatusSchema,
	author_id: z.number().int(),
	author_username: z.string(),
	batch: batchConfigSchema.nullable().optional(),
	created_at: z.string(),
	updated_at: z.string(),
	executions: z.array(executionSchema),
	approvals: z.array(approvalSchema),
});
export type Migration = z.infer<typeof migrationSchema>;

export const jobSchema = z.object({
	id: z.number().int(),
	execution_id: z.number().int(),
	migration_id: z.number().int(),
	connection_id: z.number().int(),
	status: jobStatusSchema,
	attempts: z.number().int().optional(),
	batch_cursor: z.number().int().nullable().optional(),
	batches_done: z.number().int().optional(),
	rows_done: z.number().int().optional(),
	error: z.string().nullable().optional(),
	created_at: z.string(),
	started_at: z.string().nullable().optional(),
	finished_at: z.string().nullable().optional(),
});
export type Job = z.infer<typeof jobSchema>;

function parse<T>(schema: z.ZodType<T>, data: unknown, what: string): T {
	const result = schema.safeParse(data);
	if (!result.success) {
		throw new ApiError(500, "invalid_response", `Unexpected ${what} payload from the server.`);
	}
	return result.data;
}

export interface MigrationPayload {
	sql_up: string;
	sql_down?: string;
	irreversible?: boolean;
	irreversible_reason?: string;
	batch?: {
		batch_size: number;
		cursor_column: string;
		template: string;
	};
}

export interface BatchRevertPayload {
	execution_id: number;
	sql_down: string;
	batch_size: number;
	cursor_column: string;
	template?: string;
}

export function listMigrations(connectionId: string, opts: { hideRollback?: boolean } = {}): Promise<Migration[]> {
	const query = opts.hideRollback ? "?hide_rollback=true" : "";
	return api<unknown>(`/databases/${connectionId}/migrations${query}`).then((r) =>
		parse(z.array(migrationSchema), r.data, "migration history"),
	);
}

export function createMigration(connectionId: string, payload: MigrationPayload, csrf: string): Promise<Migration> {
	return api<unknown>(`/databases/${connectionId}/migrations`, { method: "POST", body: payload, csrf }).then((r) =>
		parse(migrationSchema, r.data, "migration"),
	);
}

export function updateMigration(
	connectionId: string,
	migrationId: number,
	payload: MigrationPayload,
	csrf: string,
): Promise<Migration> {
	return api<unknown>(`/databases/${connectionId}/migrations/${migrationId}`, {
		method: "PATCH",
		body: payload,
		csrf,
	}).then((r) => parse(migrationSchema, r.data, "migration"));
}

export function deleteMigration(connectionId: string, migrationId: number, csrf: string): Promise<void> {
	return api<null>(`/databases/${connectionId}/migrations/${migrationId}`, { method: "DELETE", csrf }).then(() => undefined);
}

export function submitMigration(connectionId: string, migrationId: number, csrf: string): Promise<Migration> {
	return api<unknown>(`/databases/${connectionId}/migrations/${migrationId}/submit`, { method: "POST", csrf }).then((r) =>
		parse(migrationSchema, r.data, "migration"),
	);
}

export function rollbackMigration(connectionId: string, migrationId: number, csrf: string): Promise<Migration> {
	return api<unknown>(`/databases/${connectionId}/migrations/${migrationId}/rollback`, { method: "POST", csrf }).then((r) =>
		parse(migrationSchema, r.data, "migration"),
	);
}

export function forwardMigration(connectionId: string, migrationId: number, csrf: string): Promise<Migration> {
	return api<unknown>(`/databases/${connectionId}/migrations/${migrationId}/forward`, { method: "POST", csrf }).then((r) =>
		parse(migrationSchema, r.data, "migration"),
	);
}

export function revertBatches(
	connectionId: string,
	migrationId: number,
	payload: BatchRevertPayload,
	csrf: string,
): Promise<Migration> {
	return api<unknown>(`/databases/${connectionId}/migrations/${migrationId}/revert-batches`, {
		method: "POST",
		body: payload,
		csrf,
	}).then((r) => parse(migrationSchema, r.data, "migration"));
}

export function listApprovals(status?: ApprovalStatus): Promise<Approval[]> {
	const query = status ? `?status=${encodeURIComponent(status)}` : "";
	return api<unknown>(`/approvals${query}`).then((r) => parse(z.array(approvalSchema), r.data, "approvals"));
}

export function approveApproval(approvalId: number, csrf: string): Promise<Approval> {
	return api<unknown>(`/approvals/${approvalId}/approve`, { method: "POST", csrf }).then((r) =>
		parse(approvalSchema, r.data, "approval"),
	);
}

export function rejectApproval(approvalId: number, csrf: string, comment?: string): Promise<Approval> {
	return api<unknown>(`/approvals/${approvalId}/reject`, {
		method: "POST",
		body: comment ? { comment } : {},
		csrf,
	}).then((r) => parse(approvalSchema, r.data, "approval"));
}

export function getJob(jobId: string): Promise<Job> {
	return api<unknown>(`/jobs/${jobId}`).then((r) => parse(jobSchema, r.data, "job"));
}

export function cancelJob(jobId: string, csrf: string): Promise<Job> {
	return api<unknown>(`/jobs/${jobId}/cancel`, { method: "POST", csrf }).then((r) => parse(jobSchema, r.data, "job"));
}
