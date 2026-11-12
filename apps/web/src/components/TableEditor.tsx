import { useState } from "react";

import { ApiError, api, type DatabaseSchema, type QueryResult, type SchemaTable } from "@/lib/api";
import type { MutationState } from "@/lib/useApi";

const safeValue = (value: unknown) => (value === null || value === undefined ? "" : String(value));

export function TableEditor(props: {
	schema: DatabaseSchema;
	connectionId: string;
	tableKey: string;
	canEdit: boolean;
	csrf: string;
	rows: QueryResult | null;
	rowsError: string | null;
	offset: number;
	setOffset: (offset: number) => void;
	onMutated: () => void;
}) {
	const { schema, connectionId, tableKey, canEdit, csrf, rows, rowsError, offset, setOffset, onMutated } = props;
	const [schemaName, tableName] = tableKey.split(".");
	const entry = schema.schemas.find((s) => s.schema === schemaName);
	const table = entry?.tables.find((t) => t.name === tableName);

	// Editing needs a primary key so every affected row is unambiguous.
	const pkColumns =
		table?.constraints?.filter((c) => c.type === "PRIMARY KEY").flatMap((c) => c.columns) ?? [];
	const editable = canEdit && table !== undefined && pkColumns.length > 0;
	const mutation = useRowMutation(onMutated);

	if (!table) {
		return <div className="panel p-6 text-sm text-dim">Pick a table to browse its rows.</div>;
	}

	return (
		<div className="space-y-4">
			<div className="flex flex-wrap items-center justify-between gap-3">
				<h2 className="mono text-lg font-bold">{tableKey}</h2>
				{canEdit && !editable && (
					<span className="border border-warning/40 bg-warning/10 px-2 py-1 text-xs text-warning">
						No primary key — editing disabled, view only
					</span>
				)}
			</div>

			{editable && (
				<details className="panel">
					<summary className="cursor-pointer px-4 py-2 text-sm font-semibold hover:bg-plate-raised/60">
						+ Add row
					</summary>
					<InsertRowForm table={table} connectionId={connectionId} schemaName={schemaName} tableName={tableName} csrf={csrf} mutation={mutation} />
				</details>
			)}

			{mutation.error && <div className="error-note">{mutation.error}</div>}

			<div className="panel overflow-hidden">
				<div className="nameplate">Rows ({rows?.rows.length ?? 0})</div>
				{rowsError && <div className="border-t border-seam bg-error/10 px-4 py-3 text-sm text-error">{rowsError}</div>}
				{rows && (
					<>
						<div className="overflow-x-auto">
							<table className="w-full text-left text-xs">
								<thead className="bg-plate-raised text-dim">
									<tr>
										{rows.columns.map((column) => (
											<th key={column} className="mono px-3 py-2 font-semibold">
												{column}
											</th>
										))}
										{editable && <th className="px-3 py-2 font-semibold">Actions</th>}
									</tr>
								</thead>
								<tbody className="divide-y divide-seam">
									{rows.rows.map((row, rowIdx) => {
										const pk: Record<string, string> = {};
										pkColumns.forEach((name) => {
											const index = rows.columns.indexOf(name);
											pk[name] = safeValue(index >= 0 ? row[index] : null);
										});
										return (
											<tr key={rowIdx}>
												{row.map((value, idx) => (
													<td key={idx} className="mono max-w-xs truncate px-3 py-2">
														{value === null ? <span className="text-dim">NULL</span> : String(value)}
													</td>
												))}
												{editable && (
													<td className="px-3 py-2">
														<div className="flex items-center gap-2">
															<button
																type="button"
																className="text-xs text-error hover:underline"
																disabled={mutation.busy}
																onClick={() => {
																	if (!confirm("Delete this row?")) return;
																	void mutation.run(() =>
																		api(`/databases/${connectionId}/rows`, {
																			method: "POST",
																			body: { schema: schemaName, table: tableName, op: "delete", pk },
																			csrf,
																		}),
																	);
																}}
															>
																Delete
															</button>
															<details className="inline">
																<summary className="cursor-pointer text-dim hover:text-foreground">Edit</summary>
																<EditRowForm
																	pk={pk}
																	row={row}
																	columns={rows.columns}
																	pkColumns={pkColumns}
																	connectionId={connectionId}
																	schemaName={schemaName}
																	tableName={tableName}
																	csrf={csrf}
																	mutation={mutation}
																/>
															</details>
														</div>
													</td>
												)}
											</tr>
										);
									})}
									{rows.rows.length === 0 && (
										<tr>
											<td className="px-3 py-3 text-dim" colSpan={rows.columns.length + (editable ? 1 : 0)}>
												No rows.
											</td>
										</tr>
									)}
								</tbody>
							</table>
						</div>
						{(offset > 0 || rows.rows.length === 50) && (
							<div className="flex items-center justify-between border-t border-seam px-3 py-2 text-xs">
								{offset > 0 ? (
									<button type="button" className="text-dim underline" onClick={() => setOffset(Math.max(0, offset - 50))}>
										Previous
									</button>
								) : (
									<span />
								)}
								{rows.rows.length === 50 && (
									<button type="button" className="text-dim underline" onClick={() => setOffset(offset + 50)}>
										Next
									</button>
								)}
							</div>
						)}
					</>
				)}
			</div>
		</div>
	);
}

/** Small hook shared by the row forms so their error state is one place. */
function useRowMutation(onMutated: () => void): MutationState {
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const run = async (fn: () => Promise<unknown>) => {
		setBusy(true);
		setError(null);
		try {
			await fn();
			onMutated();
		} catch (err) {
			setError(err instanceof ApiError ? err.message : "Request failed");
		} finally {
			setBusy(false);
		}
	};
	return { busy, error, run: (fn) => void run(fn) };
}

function InsertRowForm(props: {
	table: SchemaTable;
	connectionId: string;
	schemaName: string;
	tableName: string;
	csrf: string;
	mutation: MutationState;
}) {
	const { table, connectionId, schemaName, tableName, csrf, mutation } = props;
	const [values, setValues] = useState<Record<string, string>>({});

	return (
		<form
			className="grid gap-3 border-t border-seam p-4 sm:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]"
			onSubmit={(event) => {
				event.preventDefault();
				const clean = Object.fromEntries(Object.entries(values).filter(([, v]) => v !== ""));
				if (Object.keys(clean).length === 0) return;
				void mutation.run(() =>
					api(`/databases/${connectionId}/rows`, {
						method: "POST",
						body: { schema: schemaName, table: tableName, op: "insert", values: clean },
						csrf,
					}),
				);
			}}
		>
			{table.columns.map((column) => (
				<label key={column.name} className="block">
					<span className="mono mb-1 block text-xs text-dim">
						{column.name} · {column.type}
						{column.is_nullable ? "" : " · required"}
					</span>
					<input
						type="text"
						className="field w-full"
						value={values[column.name] ?? ""}
						onChange={(e) => setValues((current) => ({ ...current, [column.name]: e.target.value }))}
					/>
				</label>
			))}
			<div className="sm:col-span-full">
				<button type="submit" disabled={mutation.busy} className="btn-brass px-3 py-2 text-xs">
					{mutation.busy ? "Inserting…" : "Insert row"}
				</button>
			</div>
		</form>
	);
}

function EditRowForm(props: {
	pk: Record<string, string>;
	row: (unknown | null)[];
	columns: string[];
	pkColumns: string[];
	connectionId: string;
	schemaName: string;
	tableName: string;
	csrf: string;
	mutation: MutationState;
}) {
	const { pk, row, columns, pkColumns, connectionId, schemaName, tableName, csrf, mutation } = props;
	const [values, setValues] = useState<Record<string, string>>(() => {
		const initial: Record<string, string> = {};
		columns.forEach((name, index) => {
			if (!pkColumns.includes(name)) initial[name] = safeValue(row[index]);
		});
		return initial;
	});

	return (
		<form
			className="mt-2 grid gap-2 border border-seam bg-plate p-2 sm:w-96"
			onSubmit={(event) => {
				event.preventDefault();
				const clean = Object.fromEntries(Object.entries(values).filter(([, v]) => v !== ""));
				if (Object.keys(clean).length === 0 || Object.keys(pk).length === 0) return;
				void mutation.run(() =>
					api(`/databases/${connectionId}/rows`, {
						method: "POST",
						body: { schema: schemaName, table: tableName, op: "update", pk, values: clean },
						csrf,
					}),
				);
			}}
		>
			{columns
				.filter((name) => !pkColumns.includes(name))
				.map((name) => {
					return (
						<label key={name} className="block">
							<span className="mono block text-xs text-dim">{name}</span>
							<input
								type="text"
								className="field w-full"
								value={values[name] ?? ""}
								onChange={(e) => setValues((current) => ({ ...current, [name]: e.target.value }))}
							/>
						</label>
					);
				})}
			<button type="submit" disabled={mutation.busy} className="btn-brass px-2 py-1 text-xs">
				Save
			</button>
		</form>
	);
}
