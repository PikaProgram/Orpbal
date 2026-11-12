import { useState } from "react";

import { api, ApiError, type DatabaseSchema, type SchemaTable, type TableCount } from "@/lib/api";

function columnConstraints(table: SchemaTable, columnName: string): string[] {
	return (table.constraints ?? [])
		.filter((c) => c.columns.includes(columnName))
		.map((c) => c.type);
}

export function TablesList(props: { schema: DatabaseSchema; connectionId: string; counts: TableCount[] | null; csrf: string }) {
	const { schema, connectionId, counts, csrf } = props;
	const publicEntry = schema.schemas.find((entry) => entry.schema === "public");
	const tables: SchemaTable[] = publicEntry?.tables ?? [];

	const estimateFor = (name: string): string => {
		const count = counts?.find((c) => c.table === name);
		if (!count?.estimate) return "—";
		return `${count.estimate} (est.)`;
	};

	return (
		<div className="space-y-4">
			{tables.length === 0 && <div className="panel p-6 text-sm text-dim">No tables in the public schema.</div>}
			{tables.map((table) => (
				<TablePanel
					key={table.name}
					table={table}
					connectionId={connectionId}
					csrf={csrf}
					estimate={estimateFor(table.name)}
				/>
			))}
		</div>
	);
}

function TablePanel(props: { table: SchemaTable; connectionId: string; csrf: string; estimate: string }) {
	const { table, connectionId, csrf, estimate } = props;
	const [countText, setCountText] = useState<string | null>(null);

	const countRows = async () => {
		// Exact row counts are fetched on demand — the list shows the free
		// pg_class estimate, the click pays for a real COUNT(*).
		setCountText("counting…");
		try {
			const result = await api<{ rows: (unknown | null)[][] }>(`/databases/${connectionId}/query`, {
				method: "POST",
				csrf,
				body: { sql: `SELECT COUNT(*) FROM "public"."${table.name}"`, limit: 1 },
			});
			setCountText(`${result.data.rows[0]?.[0] ?? "?"} rows (exact)`);
		} catch (error) {
			setCountText(error instanceof ApiError && error.status === 403 ? "not allowed" : "count failed");
		}
	};

	return (
		<div className="panel">
			<details>
				<summary className="grid cursor-pointer grid-cols-[1fr_auto_auto] items-center gap-3 px-4 py-3 hover:bg-plate-raised/60">
					<span className="mono font-semibold">{table.name}</span>
					<span className="text-xs text-dim">{table.columns.length} columns</span>
					<span className="mono text-xs text-dim">{countText ?? estimate}</span>
				</summary>
				<div className="border-t border-seam px-4 py-3">
					<div className="mb-2 flex items-center gap-2 text-xs">
						<button type="button" className="btn-brass px-3 py-1 text-xs" onClick={() => void countRows()}>
							Count rows exactly
						</button>
					</div>
					<div className="overflow-x-auto">
						<table className="w-full text-left text-xs">
							<thead className="bg-plate-raised text-dim">
								<tr>
									<th className="px-3 py-2 font-semibold">Column</th>
									<th className="px-3 py-2 font-semibold">Type</th>
									<th className="px-3 py-2 font-semibold">Nullable</th>
									<th className="px-3 py-2 font-semibold">Constraints</th>
								</tr>
							</thead>
							<tbody className="divide-y divide-seam">
								{table.columns.map((column) => (
									<tr key={column.name}>
										<td className="mono px-3 py-2 font-semibold">{column.name}</td>
										<td className="mono px-3 py-2">{column.type}</td>
										<td className="px-3 py-2">{column.is_nullable ? "yes" : "no"}</td>
										<td className="px-3 py-2">{columnConstraints(table, column.name).join(", ") || "—"}</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
				</div>
			</details>
		</div>
	);
}
