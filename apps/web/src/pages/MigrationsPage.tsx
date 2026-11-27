import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import CodeMirror from "@uiw/react-codemirror";
import { sql, PostgreSQL } from "@codemirror/lang-sql";

import { useSession } from "@/auth/session";
import {
	createMigration,
	deleteMigration,
	forwardMigration,
	listMigrations,
	revertBatches,
	rollbackMigration,
	submitMigration,
	updateMigration,
	type Approval,
	type Execution,
	type Migration,
	type MigrationAttempt,
	type MigrationStatus,
} from "@/lib/migrations";
import { useLoad, useMutation } from "@/lib/useApi";

const sqlExtensions = [sql({ dialect: PostgreSQL, upperCaseKeywords: true })];

const TEMPLATE_PLACEHOLDER =
	"UPDATE notes SET archived = true WHERE id > {{last_id}} AND id <= {{last_id}} + {{batch_size}} -- scope {{min_id}}..{{max_id}}";

function statusClass(status: MigrationStatus | string): string {
	if (status === "success") return "border-ok/40 bg-ok/10 text-ok";
	if (status === "failed") return "border-error/40 bg-error/10 text-error";
	if (status === "cancelled" || status === "rejected") return "border-dim/40 bg-dim/10 text-dim";
	return "border-warning/40 bg-warning/10 text-warning";
}

function StatusBadge({ status }: { status: string }) {
	return <span className={`border px-2 py-1 text-xs ${statusClass(status)}`}>{status.replace("_", " ")}</span>;
}

function AttemptRow({ attempt }: { attempt: MigrationAttempt }) {
	return (
		<li className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-dim">
			<span className="mono">attempt #{attempt.attempt}</span>
			<span className={`border px-1.5 py-0.5 ${statusClass(attempt.status)}`}>{attempt.status}</span>
			{(attempt.rows_done !== undefined || attempt.batches_done !== undefined) && (
				<span className="mono">
					{attempt.batches_done ?? 0} batches · {attempt.rows_done ?? 0} rows
				</span>
			)}
			{attempt.actor_username && <span>by {attempt.actor_username}</span>}
			{attempt.worker_id && <span className="mono">worker {attempt.worker_id}</span>}
			{attempt.error && <span className="text-error">{attempt.error}</span>}
		</li>
	);
}

function ExecutionRow({ execution }: { execution: Execution }) {
	const isBatch = execution.batches_done !== undefined || execution.rows_done !== undefined;
	return (
		<li className="flex flex-col gap-1 text-xs">
			<div className="flex flex-wrap items-center gap-x-2 gap-y-1">
				<span className="mono font-semibold text-brass-lit">{execution.kind.replace("_", " ")}</span>
				<span className={`border px-1.5 py-0.5 ${statusClass(execution.status)}`}>{execution.status}</span>
				{isBatch && (
					<span className="mono text-dim">
						{execution.batches_done ?? 0} batches · {execution.rows_done ?? 0} rows
					</span>
				)}
				<span className="text-dim">by {execution.actor_username || `#${execution.actor_id}`}</span>
				{execution.error && <span className="text-error">{execution.error}</span>}
			</div>
			{(execution.attempts?.length ?? 0) > 0 && (
				<ul className="ml-4 list-none space-y-1 border-l-2 border-seam pl-3">
					{execution.attempts!.map((attempt) => (
						<AttemptRow key={attempt.id} attempt={attempt} />
					))}
				</ul>
			)}
		</li>
	);
}

function ApprovalLine({ approval }: { approval: Approval }) {
	return (
		<li className="text-xs text-dim">
			{approval.group_name} — leader {approval.leader_username}: <span className="uppercase">{approval.status}</span>
			{approval.comment && <span> · “{approval.comment}”</span>}
		</li>
	);
}

interface FormState {
	sql_up: string;
	sql_down: string;
	irreversible: boolean;
	irreversible_reason: string;
	isBatch: boolean;
	batch_size: string;
	cursor_column: string;
	template: string;
}

const emptyForm: FormState = {
	sql_up: "",
	sql_down: "",
	irreversible: false,
	irreversible_reason: "",
	isBatch: false,
	batch_size: "",
	cursor_column: "",
	template: "",
};

function formFromMigration(migration: Migration): FormState {
	return {
		sql_up: migration.sql_up,
		sql_down: migration.sql_down ?? "",
		irreversible: migration.irreversible,
		irreversible_reason: migration.irreversible_reason ?? "",
		isBatch: migration.batch !== null && migration.batch !== undefined,
		batch_size: migration.batch ? String(migration.batch.batch_size) : "",
		cursor_column: migration.batch?.cursor_column ?? "",
		template: migration.batch?.template ?? "",
	};
}

function AuthoringForm(props: {
	title: string;
	form: FormState;
	setForm: (update: (current: FormState) => FormState) => void;
	busy: boolean;
	error: string | null;
	onSubmit: () => void;
	onCancel: () => void;
}) {
	const { title, form, setForm, busy, error, onSubmit, onCancel } = props;
	const patch = (key: keyof FormState, value: string | boolean) => setForm((current) => ({ ...current, [key]: value }));
	const batchSize = Number(form.batch_size);
	const batchValid =
		!form.isBatch ||
		(Number.isInteger(batchSize) && batchSize >= 1 && form.cursor_column.trim() !== "" && form.template.trim() !== "");

	return (
		<form
			className="panel p-4"
			onSubmit={(event) => {
				event.preventDefault();
				if (form.sql_up.trim() && batchValid) onSubmit();
			}}
		>
			<div className="nameplate -mx-4 -mt-4 mb-3">{title}</div>
			<label className="block text-sm">
				<span className="mb-1 block text-dim">Up SQL (required)</span>
				<CodeMirror
					value={form.sql_up}
					extensions={sqlExtensions}
					onChange={(value) => patch("sql_up", value)}
					className="cm-field w-full"
					height="auto"
					basicSetup={{ lineNumbers: true, foldGutter: true }}
				/>
			</label>
			<label className="mt-3 block text-sm">
				<span className="mb-1 block text-dim">Down SQL (optional; needed for rollback)</span>
				<CodeMirror
					value={form.sql_down}
					extensions={sqlExtensions}
					onChange={(value) => patch("sql_down", value)}
					className="cm-field w-full"
					height="auto"
					basicSetup={{ lineNumbers: true, foldGutter: true }}
				/>
			</label>
			<div className="mt-3 flex flex-wrap items-center gap-4 text-sm">
				<label className="flex items-center gap-2">
					<input
						type="checkbox"
						checked={form.irreversible}
						onChange={(e) => patch("irreversible", e.target.checked)}
					/>
					Irreversible (no rollback possible)
				</label>
				<label className="flex items-center gap-2">
					<input type="checkbox" checked={form.isBatch} onChange={(e) => patch("isBatch", e.target.checked)} />
					Batch migration
				</label>
			</div>
			{form.irreversible && (
				<label className="mt-3 block text-sm">
					<span className="mb-1 block text-dim">Why is it irreversible? (required)</span>
					<input
						className="field"
						value={form.irreversible_reason}
						onChange={(e) => patch("irreversible_reason", e.target.value)}
					/>
				</label>
			)}
			{form.isBatch && (
				<div className="mt-3 grid gap-3 sm:grid-cols-2">
					<label className="block text-sm">
						<span className="mb-1 block text-dim">Batch size (rows per batch)</span>
						<input
							type="number"
							min={1}
							className="field"
							value={form.batch_size}
							onChange={(e) => patch("batch_size", e.target.value)}
						/>
					</label>
					<label className="block text-sm">
						<span className="mb-1 block text-dim">Cursor column (e.g. id)</span>
						<input
							className="field"
							value={form.cursor_column}
							onChange={(e) => patch("cursor_column", e.target.value)}
						/>
					</label>
					<label className="block text-sm sm:col-span-2">
						<span className="mb-1 block text-dim">
							Batch SQL template — placeholders: {"{{last_id}}"}, {"{{batch_size}}"}, {"{{min_id}}"}, {"{{max_id}}"}
						</span>
						<textarea
							className="field mono min-h-20"
							placeholder={TEMPLATE_PLACEHOLDER}
							value={form.template}
							onChange={(e) => patch("template", e.target.value)}
						/>
					</label>
				</div>
			)}
			{error && <p className="mt-2 text-sm text-error">{error}</p>}
			<div className="mt-3 flex items-center gap-2">
				<button type="submit" disabled={busy || !form.sql_up.trim() || !batchValid} className="btn-brass px-3 py-2 text-xs">
					{busy ? "Saving..." : "Save draft"}
				</button>
				<button type="button" className="px-2 py-2 text-xs text-dim underline" onClick={onCancel}>
					Cancel
				</button>
			</div>
		</form>
	);
}

function RevertBatchesForm(props: {
	migration: Migration;
	csrf: string;
	busy: boolean;
	onDone: () => void;
	onError: (message: string) => void;
}) {
	const { migration, csrf, busy, onDone, onError } = props;
	const [executionId, setExecutionId] = useState("");
	const [sqlDown, setSqlDown] = useState("");
	const [batchSize, setBatchSize] = useState("");

	const partials = migration.executions.filter((e) => e.status === "partial");

	const submit = () => {
		const size = Number(batchSize);
		const execution = partials.find((e) => String(e.id) === executionId) ?? partials[0];
		if (!execution || !sqlDown.trim() || !Number.isInteger(size) || size < 1) return;
		revertBatches(
			String(migration.connection_id),
			migration.id,
			{ execution_id: execution.id, sql_down: sqlDown, batch_size: size, cursor_column: migration.batch?.cursor_column ?? "id" },
			csrf,
		)
			.then(onDone)
			.catch((error: unknown) => onError(error instanceof Error ? error.message : "Request failed"));
	};

	return (
		<div className="mt-2 border border-seam bg-zinc-950 p-3">
			<div className="mb-2 text-xs font-bold text-brass-lit">Revert batches (compensation)</div>
			{partials.length === 0 && <p className="text-xs text-dim">No partial execution to compensate.</p>}
			{partials.length > 0 && (
				<div className="grid gap-2 sm:grid-cols-2">
					<label className="block text-xs">
						<span className="mb-1 block text-dim">Partial execution</span>
						<select className="field" value={executionId || String(partials[0].id)} onChange={(e) => setExecutionId(e.target.value)}>
							{partials.map((e) => (
								<option key={e.id} value={e.id}>
									#{e.id} — {e.batches_done ?? 0} batches done
								</option>
							))}
						</select>
					</label>
					<label className="block text-xs">
						<span className="mb-1 block text-dim">Batch size</span>
						<input type="number" min={1} className="field" value={batchSize} onChange={(e) => setBatchSize(e.target.value)} />
					</label>
					<label className="block text-xs sm:col-span-2">
						<span className="mb-1 block text-dim">Compensation SQL ({"{{last_id}}"}, {"{{batch_size}}"}, {"{{min_id}}"}, {"{{max_id}}"})</span>
						<textarea
							className="field mono min-h-16"
							placeholder={TEMPLATE_PLACEHOLDER}
							value={sqlDown}
							onChange={(e) => setSqlDown(e.target.value)}
						/>
					</label>
					<div className="sm:col-span-2">
						<button
							type="button"
							disabled={busy || !sqlDown.trim() || !batchSize}
							className="btn-brass px-3 py-2 text-xs"
							onClick={submit}
						>
							Submit batch revert for review
						</button>
					</div>
				</div>
			)}
		</div>
	);
}

function MigrationCard(props: {
	migration: Migration;
	csrf: string;
	onMutated: () => void;
}) {
	const { migration, csrf, onMutated } = props;
	const [expanded, setExpanded] = useState(false);
	const [editing, setEditing] = useState(false);
	const [form, setForm] = useState<FormState>(emptyForm);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [showRevert, setShowRevert] = useState(false);

	const run = (fn: () => Promise<unknown>) => {
		setBusy(true);
		setError(null);
		return fn()
			.then(() => onMutated())
			.catch((err: unknown) => setError(err instanceof Error ? err.message : "Request failed"))
			.finally(() => setBusy(false));
	};

	const saveDraft = () => {
		const payload = {
			sql_up: form.sql_up,
			sql_down: form.sql_down.trim() ? form.sql_down : undefined,
			irreversible: form.irreversible,
			irreversible_reason: form.irreversible && form.irreversible_reason.trim() ? form.irreversible_reason : undefined,
			batch: form.isBatch
				? {
						batch_size: Number(form.batch_size),
						cursor_column: form.cursor_column.trim(),
						template: form.template,
					}
				: undefined,
		};
		setBusy(true);
		setError(null);
		const request =
			editing && migration.status === "draft" && migration.id
				? updateMigration(String(migration.connection_id), migration.id, payload, csrf)
				: createMigration(String(migration.connection_id), payload, csrf);
		request
			.then(() => {
				setEditing(false);
				setForm(emptyForm);
				onMutated();
			})
			.catch((err: unknown) => setError(err instanceof Error ? err.message : "Request failed"))
			.finally(() => setBusy(false));
	};

	const pendingApprovals = migration.approvals.filter((a) => a.status === "pending");
	const hasDown = Boolean(migration.sql_down && migration.sql_down.trim());

	return (
		<div className="border border-seam bg-zinc-950">
			<div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
				<div className="flex flex-wrap items-center gap-2 text-xs">
					<span className="mono font-bold text-foreground">v{migration.version}</span>
					<StatusBadge status={migration.status} />
					<span className="text-dim">by {migration.author_username || `#${migration.author_id}`}</span>
					{migration.irreversible && (
						<span
							className="border border-error/40 bg-error/10 px-1.5 py-0.5 text-error"
							title={migration.irreversible_reason ?? "Irreversible migration"}
						>
							irreversible
						</span>
					)}
					{migration.batch && (
						<span className="mono text-dim">batch of {migration.batch.batch_size} by {migration.batch.cursor_column}</span>
					)}
				</div>
				<div className="flex flex-wrap items-center gap-2">
					{migration.executions.length > 0 && (
						<button
							type="button"
							className="text-xs text-dim underline hover:text-foreground"
							onClick={() => setExpanded((open) => !open)}
						>
							{expanded ? "Hide history" : `History (${migration.executions.length})`}
						</button>
					)}
					{migration.status === "draft" && (
						<>
							<button
								type="button"
								className="text-xs text-brass-lit hover:underline"
								onClick={() => {
									setForm(formFromMigration(migration));
									setEditing(true);
								}}
							>
								Edit
							</button>
							<button type="button" disabled={busy} className="text-xs text-brass-lit hover:underline" onClick={() => run(() => submitMigration(String(migration.connection_id), migration.id, csrf))}>
								Submit for review
							</button>
							<button
								type="button"
								disabled={busy}
								className="text-xs text-error hover:underline"
								onClick={() => run(() => deleteMigration(String(migration.connection_id), migration.id, csrf))}
							>
								Delete
							</button>
						</>
					)}
					{migration.status === "success" && (
						<span title={migration.irreversible || !hasDown ? "This migration cannot be rolled back" : undefined}>
							<button
								type="button"
								disabled={busy || migration.irreversible || !hasDown}
								className="text-xs text-brass-lit enabled:hover:underline disabled:cursor-not-allowed disabled:text-dim disabled:no-underline"
								onClick={() => run(() => rollbackMigration(String(migration.connection_id), migration.id, csrf))}
							>
								Rollback
							</button>
						</span>
					)}
					{migration.status === "cancelled" && (
						<button
							type="button"
							disabled={busy}
							className="text-xs text-brass-lit hover:underline"
							onClick={() => run(() => forwardMigration(String(migration.connection_id), migration.id, csrf))}
						>
							Forward
						</button>
					)}
					{migration.status === "partial" && (
						<>
							{/* Forward doubles as retry: the backend chooses kind=retry
							    when the migration is failed/partial. */}
							<button
								type="button"
								disabled={busy}
								className="text-xs text-brass-lit hover:underline"
								onClick={() => run(() => forwardMigration(String(migration.connection_id), migration.id, csrf))}
							>
								Resume (retry)
							</button>
							<button
								type="button"
								className="text-xs text-dim underline hover:text-foreground"
								onClick={() => setShowRevert((open) => !open)}
							>
								Revert batches
							</button>
						</>
					)}
				</div>
			</div>
			<div className="px-3 pb-3">
				<pre className="mono max-h-40 overflow-auto whitespace-pre-wrap text-xs text-dim">{migration.sql_up}</pre>
				{error && <p className="mt-2 text-sm text-error">{error}</p>}
				{migration.status === "pending_review" && pendingApprovals.length > 0 && (
					<ul className="mt-2 space-y-1 border-t border-seam pt-2">
						<li className="text-xs font-bold text-dim">Approvals awaiting:</li>
						{pendingApprovals.map((approval) => (
							<ApprovalLine key={approval.id} approval={approval} />
						))}
					</ul>
				)}
				{expanded && (
					<ul className="mt-2 space-y-1 border-t border-seam pt-2">
						{migration.executions.map((execution) => (
							<ExecutionRow key={execution.id} execution={execution} />
						))}
					</ul>
				)}
				{editing && migration.status === "draft" && (
					<div className="mt-2">
						<AuthoringForm
							title={`Edit draft v${migration.version}`}
							form={form}
							setForm={setForm}
							busy={busy}
							error={error}
							onSubmit={saveDraft}
							onCancel={() => {
								setEditing(false);
								setForm(emptyForm);
							}}
						/>
					</div>
				)}
				{showRevert && migration.status === "partial" && (
					<RevertBatchesForm
						migration={migration}
						csrf={csrf}
						busy={busy}
						onDone={() => {
							setShowRevert(false);
							onMutated();
						}}
						onError={(message) => setError(message)}
					/>
				)}
			</div>
		</div>
	);
}

export function MigrationsPage() {
	const { session } = useSession();
	const { id = "" } = useParams();
	const csrf = session?.csrf_token ?? "";

	const [hideRollback, setHideRollback] = useState(false);
	const [creating, setCreating] = useState(false);
	const [form, setForm] = useState<FormState>(emptyForm);

	const { data: migrations, loading, error, reload } = useLoad(`migrations:${id}:${hideRollback}`, () =>
		listMigrations(id, { hideRollback }),
	);
	const mutation = useMutation(reload);

	const submitCreate = () => {
		const payload = {
			sql_up: form.sql_up,
			sql_down: form.sql_down.trim() ? form.sql_down : undefined,
			irreversible: form.irreversible,
			irreversible_reason: form.irreversible && form.irreversible_reason.trim() ? form.irreversible_reason : undefined,
			batch: form.isBatch
				? { batch_size: Number(form.batch_size), cursor_column: form.cursor_column.trim(), template: form.template }
				: undefined,
		};
		void mutation.run(async () => {
			await createMigration(id, payload, csrf);
			setForm(emptyForm);
			setCreating(false);
		});
	};

	return (
		<div>
			<div className="mb-4 flex flex-wrap items-center justify-between gap-3">
				<div>
					<h1 className="text-2xl font-semibold tracking-tight">Migrations</h1>
					<p className="text-sm text-dim">Versioned schema and data migrations for database #{id}.</p>
				</div>
				<button type="button" className="btn-brass px-3 py-2 text-xs" onClick={() => setCreating((open) => !open)}>
					{creating ? "Close form" : "New migration"}
				</button>
			</div>

			<div className="mb-4 flex flex-wrap items-center gap-4">
				<label className="flex items-center gap-2 text-sm">
					<input
						type="checkbox"
						checked={hideRollback}
						onChange={(e) => setHideRollback(e.target.checked)}
					/>
					Hide executions cancelled by rollback
				</label>
				<Link to={`/databases/${id}`} className="text-sm text-dim underline hover:text-foreground">
					← Back to database
				</Link>
			</div>

			{creating && (
				<div className="mb-4">
					<AuthoringForm
						title="New migration"
						form={form}
						setForm={setForm}
						busy={mutation.busy}
						error={mutation.error}
						onSubmit={submitCreate}
						onCancel={() => {
							setCreating(false);
							setForm(emptyForm);
						}}
					/>
				</div>
			)}

			{loading && <p className="text-sm text-dim">Loading...</p>}
			{error && <div className="error-note">{error}</div>}
			{!loading && !error && migrations !== null && migrations.length === 0 && (
				<div className="panel p-6 text-sm text-dim">No migrations recorded for this database.</div>
			)}
			{!loading && migrations !== null && migrations.length > 0 && (
				<div className="space-y-2">
					{migrations.map((migration) => (
						<MigrationCard key={migration.id} migration={migration} csrf={csrf} onMutated={reload} />
					))}
				</div>
			)}
		</div>
	);
}
