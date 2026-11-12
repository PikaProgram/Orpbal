import { useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { sql, PostgreSQL } from "@codemirror/lang-sql";

import {
	api,
	type ExplainNode,
	type ExplainResult,
	type Migration,
	type MigrationDiff,
} from "@/lib/api";

const sqlExtensions = [sql({ dialect: PostgreSQL, upperCaseKeywords: true })];

const statusClass = (status: string) =>
	status === "success"
		? "border-ok/40 bg-ok/10 text-ok"
		: status === "failed"
			? "border-error/40 bg-error/10 text-error"
			: "border-warning/40 bg-warning/10 text-warning";

export function SqlEditor(props: {
	connectionId: string;
	canEdit: boolean;
	csrf: string;
	migrations: Migration[] | null;
	migrationsError: string | null;
	editMigrationId: number;
	diffId: number;
	diff: MigrationDiff | null;
	diffError: string | null;
	tableKey: string;
	setParam: (key: string, value: string | null) => void;
	onMutated: () => void;
}) {
	const { connectionId, canEdit, csrf, migrations, migrationsError, editMigrationId, diffId, diff, diffError, setParam, onMutated } =
		props;

	const [createSql, setCreateSql] = useState("");
	const [createBusy, setCreateBusy] = useState(false);
	const [createError, setCreateError] = useState<string | null>(null);

	// Runner state.
	const [runnerSql, setRunnerSql] = useState("");
	const [runnerBusy, setRunnerBusy] = useState(false);
	const [runnerOutput, setRunnerOutput] = useState<RunnerOutput | null>(null);

	const [saveError, setSaveError] = useState<string | null>(null);

	const submitCreate = () => {
		if (!createSql.trim()) return;
		setCreateBusy(true);
		setCreateError(null);
		api(`/databases/${connectionId}/migrations`, { method: "POST", body: { sql: createSql }, csrf })
			.then(() => {
				setCreateSql("");
				onMutated();
			})
			.catch((error: unknown) => setCreateError(messageOf(error)))
			.finally(() => setCreateBusy(false));
	};

	const runQuery = async (mode: "run" | "explain" | "explain-analyze") => {
		if (!runnerSql.trim()) return;
		setRunnerBusy(true);
		setRunnerOutput({ kind: "status", text: mode === "run" ? "Running…" : mode === "explain" ? "Planning…" : "Running EXPLAIN ANALYZE…" });
		try {
			if (mode === "run") {
				const result = await api<{ columns: string[]; rows: (unknown | null)[][] }>(`/databases/${connectionId}/query`, {
					method: "POST",
					csrf,
					body: { sql: runnerSql, limit: 10000 },
				});
				setRunnerOutput({ kind: "table", columns: result.data.columns, rows: result.data.rows });
			} else {
				const result = await api<ExplainResult>(`/databases/${connectionId}/explain`, {
					method: "POST",
					csrf,
					body: { sql: runnerSql, analyze: mode === "explain-analyze" },
				});
				setRunnerOutput({ kind: "explain", result: result.data });
			}
		} catch (error) {
			setRunnerOutput({ kind: "error", message: messageOf(error) });
		} finally {
			setRunnerBusy(false);
		}
	};

	return (
		<div className="space-y-4">
			{migrationsError && (
				<div className="border border-error/40 bg-error/10 px-4 py-3 text-sm text-error">{migrationsError}</div>
			)}

			{canEdit && (
				<div className="panel p-4">
					<span className="mb-1 block text-sm text-dim">New migration SQL</span>
					<CodeMirror
						value={createSql}
						extensions={sqlExtensions}
						onChange={(value) => setCreateSql(value)}
						className="cm-field w-full"
						height="auto"
						basicSetup={{ lineNumbers: true, foldGutter: true }}
					/>
					{createError && <p className="mt-2 text-sm text-error">{createError}</p>}
					<button type="button" disabled={createBusy} className="btn-brass mt-3 px-3 py-2 text-xs" onClick={submitCreate}>
						{createBusy ? "Creating…" : "Create migration"}
					</button>
				</div>
			)}

			<section className="panel">
				<div className="nameplate">Runner (read-only)</div>
				<div className="p-4">
					<CodeMirror
						value={runnerSql}
						extensions={sqlExtensions}
						onChange={(value) => setRunnerSql(value)}
						className="cm-field w-full"
						height="auto"
						placeholder="SELECT * FROM notes LIMIT 5;"
						basicSetup={{ lineNumbers: true, foldGutter: true }}
					/>
					<div className="mt-3 flex flex-wrap items-center gap-2">
						<button type="button" disabled={runnerBusy} className="btn-brass px-3 py-2 text-xs" onClick={() => void runQuery("run")}>
							Run query
						</button>
						<button
							type="button"
							disabled={runnerBusy}
							className="border border-seam px-3 py-2 text-xs text-brass-lit hover:border-brass"
							onClick={() => void runQuery("explain")}
						>
							Explain
						</button>
						<button
							type="button"
							disabled={runnerBusy}
							className="border border-seam px-3 py-2 text-xs text-brass-lit hover:border-brass"
							onClick={() => void runQuery("explain-analyze")}
						>
							Explain Analyze
						</button>
						<span className="text-xs text-dim">
							EXPLAIN ANALYZE runs inside a read-only transaction; results are discarded.
						</span>
					</div>
					<div className="mt-3 overflow-x-auto">
						<RunnerOutputView output={runnerOutput} />
					</div>
				</div>
			</section>

			<section className="panel overflow-hidden">
				<div className="nameplate">Migrations</div>
				<div className="p-4">
					{saveError && <div className="mb-3 text-sm text-error">{saveError}</div>}
					{migrations === null && <p className="text-sm text-dim">Loading…</p>}
					{migrations !== null && migrations.length === 0 && (
						<p className="text-sm text-dim">No migrations recorded for this database.</p>
					)}
					{migrations !== null && migrations.length > 0 && (
						<div className="space-y-2">
							{migrations.map((migration) => (
								<MigrationCard
									key={migration.id}
									migration={migration}
									connectionId={connectionId}
									csrf={csrf}
									canEdit={canEdit}
									isEditing={editMigrationId === migration.id}
									setParam={setParam}
									onMutated={onMutated}
									setSaveError={setSaveError}
								/>
							))}
						</div>
					)}
				</div>
			</section>

			{diffId > 0 && (
				<section className="panel p-4">
					<div className="mb-2 text-xs font-bold">
						Schema diff {diff?.status === "success" ? "(before → after)" : ""}
					</div>
					{diffError && <div className="text-sm text-error">{diffError}</div>}
					{diff && diff.changes.length === 0 && <p className="text-sm text-dim">No schema changes recorded.</p>}
					{diff && diff.changes.length > 0 && (
						<ul className="mono space-y-1 text-xs">
							{diff.changes.map((change) => (
								<li key={`${change.object_type}:${change.object_id}`}>
									<span
										className={`mr-2 inline-block w-14 text-center ${
											change.action === "added" ? "text-ok" : change.action === "removed" ? "text-error" : "text-warning"
										}`}
									>
										{change.action}
									</span>
									<span className="text-foreground">{change.object_type}</span> {change.object_id}
									{change.detail && <span className="text-dim"> — {change.detail}</span>}
								</li>
							))}
						</ul>
					)}
					{diff && diff.status !== "success" && (
						<p className="text-sm text-dim">Diff is only available for executed migrations.</p>
					)}
				</section>
			)}
		</div>
	);
}

function messageOf(error: unknown): string {
	return error instanceof Error ? error.message : "Request failed";
}

type RunnerOutput =
	| { kind: "status"; text: string }
	| { kind: "table"; columns: string[]; rows: (unknown | null)[][] }
	| { kind: "explain"; result: ExplainResult }
	| { kind: "error"; message: string };

function RunnerOutputView({ output }: { output: RunnerOutput | null }) {
	if (output === null) return <span />;
	switch (output.kind) {
		case "status":
			return <p className="text-sm text-dim">{output.text}</p>;
		case "error":
			return <div className="border border-error/40 bg-error/10 px-4 py-3 text-sm text-error">{output.message}</div>;
		case "table":
			if (output.columns.length === 0) return <p className="text-sm text-dim">No result set.</p>;
			return (
				<table className="w-full text-left text-xs">
					<thead className="bg-plate-raised text-dim">
						<tr>
							{output.columns.map((name) => (
								<th key={name} className="mono px-3 py-2 font-semibold">
									{name}
								</th>
							))}
						</tr>
					</thead>
					<tbody className="divide-y divide-seam">
						{output.rows.map((row, idx) => (
							<tr key={idx}>
								{row.map((value, colIdx) => (
									<td key={colIdx} className="mono max-w-xs truncate px-3 py-2">
										{value === null ? "NULL" : String(value)}
									</td>
								))}
							</tr>
						))}
					</tbody>
				</table>
			);
		case "explain":
			return <ExplainView result={output.result} />;
	}
}

function ExplainView({ result }: { result: ExplainResult }) {
	if (result.nodes && result.nodes.length > 0) {
		return (
			<div className="mono space-y-2 text-xs">
				{result.nodes.map((node, idx) => (
					<ExplainNodeView key={idx} node={node} />
				))}
			</div>
		);
	}
	const columns = result.columns ?? [];
	const rows = result.rows ?? [];
	if (columns.length === 0) return <p className="text-sm text-dim">No plan returned.</p>;
	return (
		<table className="w-full text-left text-xs">
			<thead className="bg-plate-raised text-dim">
				<tr>
					{columns.map((name) => (
						<th key={name} className="mono px-3 py-2 font-semibold">
							{name}
						</th>
					))}
				</tr>
			</thead>
			<tbody className="divide-y divide-seam">
				{rows.map((row, idx) => (
					<tr key={idx}>
						{row.map((value, colIdx) => (
							<td key={colIdx} className="mono max-w-xs truncate px-3 py-2">
								{value === null ? "NULL" : String(value)}
							</td>
						))}
					</tr>
				))}
			</tbody>
		</table>
	);
}

function ExplainNodeView({ node }: { node: ExplainNode }) {
	const actual =
		node.actual_total_time === null || node.actual_total_time === undefined
			? ""
			: ` · actual ${(node.actual_startup_time ?? 0).toFixed(3)}..${node.actual_total_time} ms · rows ${
					node.actual_rows ?? "?"
				} · loops ${node.actual_loops ?? "?"}`;
	const details: string[] = [];
	if (node.index_cond) details.push(`Index Cond: ${node.index_cond}`);
	if (node.filter) details.push(`Filter: ${node.filter}`);
	if (node.sort_key?.length) details.push(`Sort Key: ${node.sort_key.join(", ")}`);
	return (
		<div className="border-l border-seam pl-3">
			<div>
				<span className="font-semibold text-brass-lit">{node.node_type}</span>
				{node.relation && <span className="text-foreground"> on {node.relation}</span>}
				{node.alias && node.alias !== node.relation && <span className="text-dim"> ({node.alias})</span>}
				{node.index && <span className="text-foreground"> using {node.index}</span>}
				{node.join_type && <span className="text-dim">[{node.join_type}]</span>}
			</div>
			<div className="text-dim">
				cost {node.startup_cost.toFixed(2)}..{node.total_cost.toFixed(2)} · rows {node.plan_rows} · width {node.plan_width}
				{actual && <span className="text-ok">{actual}</span>}
			</div>
			{details.length > 0 && <div className="text-dim">{details.join(" · ")}</div>}
			{node.children?.length ? (
				<div className="mt-1 space-y-1">
					{node.children.map((child, idx) => (
						<ExplainNodeView key={idx} node={child} />
					))}
				</div>
			) : null}
		</div>
	);
}

function MigrationCard(props: {
	migration: Migration;
	connectionId: string;
	csrf: string;
	canEdit: boolean;
	isEditing: boolean;
	setParam: (key: string, value: string | null) => void;
	onMutated: () => void;
	setSaveError: (error: string | null) => void;
}) {
	const { migration, connectionId, csrf, canEdit, isEditing, setParam, onMutated, setSaveError } = props;
	const [editorSql, setEditorSql] = useState(migration.sql);
	const [explainState, setExplainState] = useState<RunnerOutput | null>(null);
	const [busy, setBusy] = useState(false);

	const explain = async (analyze: boolean) => {
		// When the inline editor is open its contents win over the stored SQL.
		const sqlText = isEditing ? editorSql : migration.sql;
		if (!sqlText.trim()) {
			setExplainState({ kind: "error", message: "SQL is empty." });
			return;
		}
		setBusy(true);
		setExplainState({ kind: "status", text: analyze ? "Running EXPLAIN ANALYZE…" : "Planning…" });
		try {
			const result = await api<ExplainResult>(`/databases/${connectionId}/explain`, {
				method: "POST",
				csrf,
				body: { sql: sqlText, analyze },
			});
			setExplainState({ kind: "explain", result: result.data });
		} catch (error) {
			setExplainState({ kind: "error", message: messageOf(error) });
		} finally {
			setBusy(false);
		}
	};

	const execute = () => {
		setBusy(true);
		api(`/databases/${connectionId}/migrations/${migration.id}/execute`, { method: "POST", csrf })
			.then(() => onMutated())
			.catch((error: unknown) => setSaveError(messageOf(error)))
			.finally(() => setBusy(false));
	};

	const saveEdit = () => {
		if (!editorSql.trim()) return;
		setBusy(true);
		setSaveError(null);
		api(`/databases/${connectionId}/migrations/${migration.id}`, {
			method: "PATCH",
			body: { sql: editorSql },
			csrf,
		})
			.then(() => onMutated())
			.catch((error: unknown) => setSaveError(messageOf(error)))
			.finally(() => setBusy(false));
	};

	return (
		<div className="border border-seam bg-zinc-950">
			<div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
				<div className="flex flex-wrap items-center gap-2 text-xs">
					<span className="mono font-bold">v{migration.version}</span>
					<span className={`border px-2 py-1 ${statusClass(migration.status)}`}>{migration.status}</span>
					<span className="text-dim">{migration.executor_username || `#${migration.executor_id}`}</span>
					{migration.status !== "pending" && <span className="mono text-dim">{migration.executed_at}</span>}
				</div>
				<div className="flex items-center gap-2">
					{migration.status === "success" && (
						<button
							type="button"
							className="text-xs text-dim underline hover:text-foreground"
							onClick={() => setParam("diff", String(migration.id))}
						>
							View diff
						</button>
					)}
					{canEdit && migration.status !== "success" && (
						<>
							{migration.status === "pending" && (
								<button type="button" disabled={busy} className="text-xs text-brass-lit hover:underline" onClick={execute}>
									Execute
								</button>
							)}
							<button
								type="button"
								className="text-xs text-dim underline hover:text-foreground"
								onClick={() =>
									setParam("edit", isEditing ? null : String(migration.id))
								}
							>
								{isEditing ? "Close editor" : migration.status === "failed" ? "Fix" : "Edit"}
							</button>
						</>
					)}
				</div>
			</div>
			<div className="px-3 pb-3">
				{canEdit && isEditing && migration.status !== "success" ? (
					<div className="grid gap-2">
						<CodeMirror
							value={editorSql}
							extensions={sqlExtensions}
							onChange={(value) => setEditorSql(value)}
							className="cm-field w-full"
							height="auto"
							basicSetup={{ lineNumbers: true, foldGutter: true }}
						/>
						<div className="flex items-center gap-2">
							<button type="button" disabled={busy} className="btn-brass px-2 py-1 text-xs" onClick={saveEdit}>
								Save
							</button>
							<button type="button" className="px-2 py-1 text-xs text-dim underline" onClick={() => setParam("edit", null)}>
								Cancel
							</button>
						</div>
					</div>
				) : (
					<pre className="mono max-h-40 overflow-auto whitespace-pre-wrap text-xs text-dim">{migration.sql}</pre>
				)}
				{canEdit && migration.status === "pending" && (
					<div className="mt-2 flex flex-wrap items-center gap-2 border-t border-seam pt-2">
						<span className="text-xs text-dim">Execution plan:</span>
						<button type="button" disabled={busy} className="text-xs text-brass-lit hover:underline" onClick={() => void explain(false)}>
							Explain
						</button>
						<button type="button" disabled={busy} className="text-xs text-brass-lit hover:underline" onClick={() => void explain(true)}>
							Explain Analyze
						</button>
						<span className="text-xs text-dim">Only SELECT/WITH statements can be planned; DDL executes without a plan.</span>
					</div>
				)}
				{explainState !== null && (
					<div className="mt-2 overflow-x-auto">
						<RunnerOutputView output={explainState} />
					</div>
				)}
			</div>
		</div>
	);
}
