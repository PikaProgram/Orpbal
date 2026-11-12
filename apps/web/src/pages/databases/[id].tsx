import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";

import { useSession } from "@/auth/session";
import {
	api,
	type DatabaseSchema,
	type Migration,
	type MigrationDiff,
	type MyDatabase,
	type QueryResult,
	type TableCount,
} from "@/lib/api";
import { TablesList } from "@/components/TablesList";
import { TableEditor } from "@/components/TableEditor";
import { useLoad } from "@/lib/useApi";

// The graph (xyflow) and SQL editor (CodeMirror) are the heaviest tab
// components; load them only when their tab is active.
const SchemaGraph = lazy(() =>
	import("@/components/SchemaGraph").then((m) => ({ default: m.SchemaGraph })),
);
const SqlEditor = lazy(() =>
	import("@/components/SqlEditor").then((m) => ({ default: m.SqlEditor })),
);

const TABS = [
	{ id: "schema", label: "Schema Visualizer" },
	{ id: "tables", label: "Tables" },
	{ id: "editor", label: "Table Editor" },
	{ id: "sql", label: "SQL Editor" },
] as const;

type TabId = (typeof TABS)[number]["id"];

const safeName = (value: string) => /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(value);

export function DatabaseDetailPage() {
	const { session } = useSession();
	const { id = "" } = useParams();
	const [searchParams, setSearchParams] = useSearchParams();

	const tab = (searchParams.get("tab") as TabId | null) ?? "schema";
	const tableKey = searchParams.get("table") ?? "";
	const editMigrationId = Number(searchParams.get("edit") ?? "");
	const diffId = Number(searchParams.get("diff") ?? "");
	const dataOffset = Math.max(0, Number(searchParams.get("offset") ?? "") || 0);

	const setParam = useCallback(
		(key: string, value: string | null) => {
			const next = new URLSearchParams(searchParams);
			if (value === null) next.delete(key);
			else next.set(key, value);
			setSearchParams(next, { replace: true });
		},
		[searchParams, setSearchParams],
	);

	// Access list first, then the schema (mirrors the old page: an
	// unauthorized connection reports "not found", not a schema error).
	const base = useLoad(id, async () => {
		const mine = await api<MyDatabase[]>("/me/databases").then((r) => r.data);
		const database = mine.find((db) => String(db.connection_id) === id);
		if (!database) return { mine, schema: null, schemaError: false };
		try {
			const schema = await api<DatabaseSchema>(`/databases/${id}/schema`).then((r) => r.data);
			return { mine, schema, schemaError: false };
		} catch {
			return { mine, schema: null, schemaError: true };
		}
	});

	const database = base.data?.mine.find((db) => String(db.connection_id) === id) ?? null;
	const canEdit = database?.access_level === "view_edit";
	const csrf = session?.csrf_token ?? "";
	const schema = base.data?.schema ?? null;

	const [outcomeError, setOutcomeError] = useState<string | null>(null);

	const [counts, setCounts] = useState<TableCount[] | null>(null);
	const [migrations, setMigrations] = useState<Migration[] | null>(null);
	const [migrationsError, setMigrationsError] = useState<string | null>(null);
	const [diff, setDiff] = useState<MigrationDiff | null>(null);
	const [diffError, setDiffError] = useState<string | null>(null);
	const [rows, setRows] = useState<QueryResult | null>(null);
	const [rowsError, setRowsError] = useState<string | null>(null);

	const [editorSchema, editorTable] = tableKey.split(".");
	const editorKnown =
		schema !== null &&
		safeName(editorSchema) &&
		safeName(editorTable) &&
		schema.schemas.some((s) => s.schema === editorSchema && s.tables.some((t) => t.name === editorTable));

	// Tables tab: planner estimates for the list (exact counts are on-demand).
	useEffect(() => {
		if (!schema || tab !== "tables") return;
		let cancelled = false;
		api<TableCount[]>(`/databases/${id}/table-counts`)
			.then((r) => r.data)
			.then((data) => {
				if (!cancelled) setCounts(data);
			})
			.catch(() => {
				if (!cancelled) setCounts(null);
			});
		return () => {
			cancelled = true;
		};
	}, [id, schema, tab, base.reload]);

	// Editor tab: one page of rows for the selected table.
	useEffect(() => {
		if (!editorKnown || tab !== "editor") {
			setRows(null);
			setRowsError(null);
			return;
		}
		let cancelled = false;
		const loadRows = async (offset: number) => {
			try {
				const result = await api<QueryResult>(`/databases/${id}/query`, {
					method: "POST",
					csrf,
					body: { sql: `SELECT * FROM "${editorSchema}"."${editorTable}"`, limit: 50, offset },
				});
				if (!cancelled) {
					setRows(result.data);
					setRowsError(null);
				}
			} catch {
				if (!cancelled) {
					setRows(null);
					setRowsError("Failed to load rows for this table.");
				}
			}
		};
		void loadRows(dataOffset);
		return () => {
			cancelled = true;
		};
	}, [id, tab, editorKnown, editorSchema, editorTable, dataOffset, csrf]);

	// SQL tab: migration history plus the requested diff.
	useEffect(() => {
		if (!database || tab !== "sql") return;
		let cancelled = false;
		api<Migration[]>(`/databases/${id}/migrations`)
			.then((r) => r.data)
			.then((data) => {
				if (!cancelled) {
					setMigrations(data);
					setMigrationsError(null);
				}
			})
			.catch(() => {
				if (!cancelled) {
					setMigrations([]);
					setMigrationsError("Failed to load migration history.");
				}
			});
		return () => {
			cancelled = true;
		};
	}, [id, database, tab]);

	useEffect(() => {
		if (tab !== "sql" || !diffId || !database) {
			setDiff(null);
			setDiffError(null);
			return;
		}
		let cancelled = false;
		api<MigrationDiff>(`/databases/${id}/migrations/${diffId}/diff`)
			.then((r) => r.data)
			.then((data) => {
				if (!cancelled) {
					setDiff(data);
					setDiffError(null);
				}
			})
			.catch(() => {
				if (!cancelled) {
					setDiff(null);
					setDiffError("Failed to load the schema diff for this migration.");
				}
			});
		return () => {
			cancelled = true;
		};
	}, [id, tab, diffId, database]);

	// After a mutation succeeds, refresh everything the current tab shows.
	const onMutated = useMemo(
		() => () => {
			setOutcomeError(null);
			base.reload();
			void (async () => {
				try {
					const [countsData, migrationsData] = await Promise.all([
						api<TableCount[]>(`/databases/${id}/table-counts`).then((r) => r.data).catch(() => null),
						api<Migration[]>(`/databases/${id}/migrations`).then((r) => r.data).catch(() => null),
					]);
					setCounts(countsData);
					setMigrations(migrationsData);
				} catch {
					// reload() already covers the critical paths
				}
				if (editorKnown && tab === "editor") {
					try {
						const rowsData = await api<QueryResult>(`/databases/${id}/query`, {
							method: "POST",
							csrf,
							body: { sql: `SELECT * FROM "${editorSchema}"."${editorTable}"`, limit: 50, offset: dataOffset },
						});
						setRows(rowsData.data);
						setRowsError(null);
					} catch {
						setRowsError("Failed to load rows for this table.");
					}
				}
				if (diffId && tab === "sql") {
					try {
						const diffData = await api<MigrationDiff>(`/databases/${id}/migrations/${diffId}/diff`);
						setDiff(diffData.data);
						setDiffError(null);
					} catch {
						setDiffError("Failed to load the schema diff for this migration.");
					}
				}
			})();
		},
		[base.reload, id, editorKnown, tab, editorSchema, editorTable, dataOffset, csrf, diffId],
	);

	const accessLabel = database?.access_level === "view_edit" ? "View + Edit" : "View Only";

	return (
		<div>
			<div className="mb-4 flex flex-wrap items-center justify-between gap-3">
				<div>
					<h1 className="text-2xl font-bold tracking-tight">{database?.name ?? "Database"}</h1>
					<p className="mono text-sm text-dim">
						{database?.host} / {database?.database_name}
					</p>
				</div>
				{database && (
					<span
						className={`border px-2 py-1 text-xs ${
							database.access_level === "view_edit"
								? "border-ok/40 bg-ok/10 text-ok"
								: "border-dim/40 bg-dim/10 text-dim"
						}`}
					>
						{accessLabel}
					</span>
				)}
			</div>

			{database === null && !base.loading && (
				<div className="mb-4 border border-error/40 bg-error/10 px-4 py-3 text-sm text-error">
					Database not found or you do not have access to it.
				</div>
			)}

			{database !== null && base.data?.schemaError === true && (
				<div className="mb-4 border border-error/40 bg-error/10 px-4 py-3 text-sm text-error">
					Failed to load schema: the database may be unreachable or access was denied.
				</div>
			)}

			{outcomeError && (
				<div className="mb-4 border border-error/40 bg-error/10 px-4 py-3 text-sm text-error">{outcomeError}</div>
			)}

			{database && (
				<nav className="mb-4 flex flex-wrap gap-1 overflow-x-auto border-b border-seam">
					{TABS.map((entry) => (
						<button
							key={entry.id}
							type="button"
							onClick={() => setParam("tab", entry.id)}
							className={`-mb-px whitespace-nowrap border-b-2 px-3 py-2 text-sm ${
								tab === entry.id
									? "border-brass font-semibold text-brass-lit"
									: "border-transparent text-dim hover:text-foreground"
							}`}
						>
							{entry.label}
						</button>
					))}
				</nav>
			)}

			{database && schema === null && base.loading && <p className="text-sm text-dim">Loading schema…</p>}

			{database && schema && tab === "schema" && (
				<Suspense fallback={<p className="text-sm text-dim">Loading…</p>}>
					<SchemaGraph schema={schema} connectionId={id} canEdit={canEdit} csrf={csrf} />
				</Suspense>
			)}

			{database && schema && tab === "tables" && (
				<TablesList schema={schema} connectionId={id} counts={counts} csrf={csrf} />
			)}

			{database && schema && tab === "editor" && (
				<div className="grid gap-4 lg:grid-cols-[260px_1fr]">
					<aside className="panel max-h-[70vh] overflow-auto p-3">
						<div className="nameplate mb-2">Tables</div>
						{schema.schemas.map((entry) => (
							<div key={entry.schema} className="mb-3">
								<div className="mono mb-1 text-xs font-bold text-brass-lit">{entry.schema}</div>
								<ul className="space-y-1">
									{entry.tables.map((table) => {
										const key = `${entry.schema}.${table.name}`;
										const active = key === tableKey;
										return (
											<li key={key}>
												<button
													type="button"
													onClick={() => setParam("table", key)}
													className={`mono block w-full px-2 py-1 text-left text-xs ${
														active ? "bg-brass/20 text-brass-lit" : "text-dim hover:text-foreground"
													}`}
												>
													{table.name}
												</button>
											</li>
										);
									})}
									{entry.tables.length === 0 && <li className="px-2 py-1 text-xs text-dim">No tables</li>}
								</ul>
							</div>
						))}
					</aside>
					<main className="min-w-0">
						{!editorKnown && (
							<div className="panel p-6 text-sm text-dim">Pick a table from the list to browse its rows.</div>
						)}
						{editorKnown && (
							<TableEditor
								schema={schema}
								connectionId={id}
								tableKey={tableKey}
								canEdit={canEdit}
								csrf={csrf}
								rows={rows}
								rowsError={rowsError}
								offset={dataOffset}
								setOffset={(offset) => setParam("offset", String(offset))}
								onMutated={onMutated}
							/>
						)}
					</main>
				</div>
			)}

			{database && tab === "sql" && (
				<Suspense fallback={<p className="text-sm text-dim">Loading…</p>}>
					<SqlEditor
						connectionId={id}
						canEdit={canEdit}
						csrf={csrf}
						migrations={migrations}
						migrationsError={migrationsError}
						editMigrationId={editMigrationId}
						diffId={diffId}
						diff={diff}
						diffError={diffError}
						tableKey={tableKey}
						setParam={setParam}
						onMutated={onMutated}
					/>
				</Suspense>
			)}
		</div>
	);
}
