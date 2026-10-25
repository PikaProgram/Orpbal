/** @jsxImportSource react */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	Controls,
	Handle,
	Position,
	ReactFlow,
	ReactFlowProvider,
	type Edge,
	useNodesState,
	type Node,
	type NodeProps,
	useReactFlow,
} from "@xyflow/react";
import dagre from "@dagrejs/dagre";
import "@xyflow/react/dist/style.css";

import type { DatabaseSchema, SchemaTable } from "@/lib/api";

const TYPE_ABBREV: Record<string, string> = {
	"character varying": "varchar",
	"timestamp with time zone": "timestamptz",
	"timestamp without time zone": "timestamp",
	"double precision": "float8",
	"bit varying": "varbit",
};

const abbrev = (type: string) => TYPE_ABBREV[type] ?? type;

const NODE_W = 300;
const NODE_HEADER = 34;
const NODE_ROW = 24;

interface ColumnSpec {
	name: string;
	type: string;
	nullable: boolean;
	default: string;
	pk: boolean;
	// True when the user edited the default field; unknown defaults are left untouched in ALTER COLUMN.
	defaultChanged?: boolean;
}

type Change =
	| { kind: "createTable"; schema: string; table: string; columns: ColumnSpec[] }
	| { key: string; kind: "addColumn"; schema: string; table: string; column: ColumnSpec }
	| { key: string; kind: "editColumn"; schema: string; table: string; original: string; column: ColumnSpec }
	| { key: string; kind: "removeColumn"; schema: string; table: string; original: string }
	| { key: string; kind: "addForeignKey"; schema: string; table: string; column: string; toSchema: string; toTable: string; toColumn: string }
	| { key: string; kind: "removeForeignKey"; schema: string; table: string; constraint: string }
	| { key: string; kind: "addPrimaryKey"; schema: string; table: string; columns: string[] }
	| { key: string; kind: "removePrimaryKey"; schema: string; table: string; constraint: string };

interface TableNodeData extends Record<string, unknown> {
	schemaName: string;
	name: string;
	columns: { name: string; type: string; pk: boolean; fk: boolean; nullable: boolean; defaultValue: string; maxLen: number | null }[];
	height: number;
}
interface TableNode extends Node<TableNodeData, "tableNode"> {}

const q = (identifier: string) => `"${identifier.replace(/"/g, '""')}"`;
const ident = /^[a-zA-Z_][a-zA-Z0-9_]*$/;

// Defaults entered as plain literals are quoted unless they are numeric or boolean.
const defaultLiteral = (raw: string) => {
	const trimmed = raw.trim();
	if (!trimmed) return "";
	if (/^-?\d+(\.\d+)?$/.test(trimmed) || trimmed === "true" || trimmed === "false" || trimmed === "NULL") {
		return trimmed;
	}
	return `'${trimmed.replace(/'/g, "''")}'`;
};

const columnSQL = (column: ColumnSpec) => {
	const parts = [q(column.name), column.type];
	if (!column.nullable) parts.push("NOT NULL");
	if (column.default.trim()) parts.push(`DEFAULT ${defaultLiteral(column.default)}`);
	return parts.join(" ");
};

// -- DDL assembly -----------------------------------------------------------

function buildStatements(changes: Change[]): { sql: string; destructive: boolean }[] {
	const out: { sql: string; destructive: boolean }[] = [];
	for (const change of changes) {
		const path = `${q(change.schema)}.${q(change.table)}`;
		switch (change.kind) {
			case "createTable": {
				const defs = change.columns.map(columnSQL);
				const pkColumns = change.columns.filter((column) => column.pk).map((column) => q(column.name));
				if (pkColumns.length > 0) defs.push(`PRIMARY KEY (${pkColumns.join(", ")})`);
				out.push({ sql: `CREATE TABLE ${path} (\n  ${defs.join(",\n  ")}\n);`, destructive: false });
				break;
			}
			case "addColumn":
				out.push({ sql: `ALTER TABLE ${path} ADD COLUMN ${columnSQL(change.column)};`, destructive: false });
				break;
			case "editColumn": {
				if (change.column.name !== change.original && ident.test(change.column.name)) {
					out.push({ sql: `ALTER TABLE ${path} RENAME COLUMN ${q(change.original)} TO ${q(change.column.name)};`, destructive: false });
				}
				const alter: string[] = [];
				const col = q(change.column.name);
				if (change.column.type.trim()) alter.push(`TYPE ${change.column.type.trim()}`);
				alter.push(change.column.nullable ? "DROP NOT NULL" : "SET NOT NULL");
				if (change.column.defaultChanged) {
					if (change.column.default.trim()) {
						alter.push(`SET DEFAULT ${defaultLiteral(change.column.default)}`);
					} else {
						alter.push("DROP DEFAULT");
					}
				}
				out.push({ sql: `ALTER TABLE ${path} ALTER COLUMN ${col} ${alter.join(", ")};`, destructive: false });
				break;
			}
			case "removeColumn":
				out.push({ sql: `ALTER TABLE ${path} DROP COLUMN ${q(change.original)};`, destructive: true });
				break;
			case "addForeignKey": {
				const name = `${change.column}_fkey`;
				out.push({
					sql: `ALTER TABLE ${path} ADD CONSTRAINT ${q(name)} FOREIGN KEY (${q(change.column)}) REFERENCES ${q(change.toSchema)}.${q(change.toTable)} (${q(change.toColumn)});`,
					destructive: false,
				});
				break;
			}
			case "removeForeignKey":
				out.push({ sql: `ALTER TABLE ${path} DROP CONSTRAINT ${q(change.constraint)};`, destructive: true });
				break;
			case "addPrimaryKey": {
				const pkName = `${change.table}_pkey`;
				out.push({ sql: `ALTER TABLE ${path} DROP CONSTRAINT IF EXISTS ${q(pkName)};`, destructive: true });
				out.push({
					sql: `ALTER TABLE ${path} ADD CONSTRAINT ${q(pkName)} PRIMARY KEY (${change.columns.map(q).join(", ")});`,
					destructive: false,
				});
				break;
			}
			case "removePrimaryKey":
				out.push({ sql: `ALTER TABLE ${path} DROP CONSTRAINT ${q(change.constraint)};`, destructive: true });
				break;
		}
	}
	return out;
}

// Deduplicate edits: a later edit of the same column replaces the earlier
// one; a removal removes any add or edit of the same column.
function compose(changes: Change[]): Change[] {
	const result: Change[] = [];
	for (const change of changes) {
		switch (change.kind) {
			case "createTable":
				result.push(change);
				break;
			case "addColumn":
			case "editColumn":
			case "removeColumn": {
				if (change.kind !== "addColumn") {
					for (let i = result.length - 1; i >= 0; i--) {
						const previous = result[i];
						if (
							previous.kind !== "createTable" &&
							previous.schema === change.schema &&
							previous.table === change.table &&
							previous.key === change.key
						) {
							result.splice(i, 1);
						}
					}
				}
				if (change.kind === "removeColumn") {
					for (let i = result.length - 1; i >= 0; i--) {
						const previous = result[i];
						if (
							previous.kind !== "createTable" &&
							previous.kind !== "removeColumn" &&
							previous.schema === change.schema &&
							previous.table === change.table &&
							previous.key === change.key
						) {
							result.splice(i, 1);
						}
					}
				}
				result.push(change);
				break;
			}
			case "addForeignKey": {
				// One pending FK per column: a second overwrite replaces the first.
				for (let i = result.length - 1; i >= 0; i--) {
					const previous = result[i];
					if (previous.kind === "addForeignKey" && previous.key === change.key) result.splice(i, 1);
				}
				result.push(change);
				break;
			}
			case "removeForeignKey": {
				// Adding then removing the same FK leaves nothing.
				for (let i = result.length - 1; i >= 0; i--) {
					const previous = result[i];
					if (previous.kind === "addForeignKey" && previous.key === change.key) result.splice(i, 1);
				}
				result.push(change);
				break;
			}
			case "addPrimaryKey": {
				for (let i = result.length - 1; i >= 0; i--) {
					const previous = result[i];
					if (previous.kind === "addPrimaryKey" && previous.key === change.key) result.splice(i, 1);
				}
				result.push(change);
				break;
			}
			case "removePrimaryKey": {
				// Adding then removing the PK on a new table leaves nothing.
				for (let i = result.length - 1; i >= 0; i--) {
					const previous = result[i];
					if (previous.kind === "addPrimaryKey" && previous.key === change.key) result.splice(i, 1);
				}
				result.push(change);
				break;
			}
		}
	}
	return result;
}

// -- Graph ------------------------------------------------------------------

interface VisibleTable {
	key: string;
	schemaName: string;
	table: SchemaTable;
	columns: TableNodeData["columns"];
	height: number;
}

function prepareTables(schema: DatabaseSchema, visibleSchema: string): VisibleTable[] {
	const out: VisibleTable[] = [];
	for (const s of schema.schemas) {
		if (visibleSchema !== "all" && s.schema !== visibleSchema) continue;
		for (const table of s.tables) {
			const pkColumns = new Set(
				(table.constraints ?? []).filter((c) => c.type === "PRIMARY KEY").flatMap((c) => c.columns),
			);
			const fkColumns = new Set(
				(table.constraints ?? []).filter((c) => c.type === "FOREIGN KEY").flatMap((c) => c.columns),
			);
			const columns = table.columns.map((column) => ({
				name: column.name,
				type: abbrev(column.type),
				pk: pkColumns.has(column.name),
				fk: fkColumns.has(column.name),
				nullable: column.is_nullable,
				defaultValue: column.column_default ?? "",
				maxLen: column.character_maximum_length ?? null,
			}));
			out.push({
				key: `${s.schema}.${table.name}`,
				schemaName: s.schema,
				table,
				columns,
				height: NODE_HEADER + columns.length * NODE_ROW,
			});
		}
	}
	return out;
}

function layoutTables(tables: VisibleTable[], edges: { from: string; to: string }[], saved: Record<string, { x: number; y: number }>): TableNode[] {
	const g = new dagre.graphlib.Graph();
	g.setGraph({ rankdir: "LR", nodesep: 60, ranksep: 120, marginx: 60, marginy: 60 });
	g.setDefaultEdgeLabel(() => ({}));
	for (const table of tables) g.setNode(table.key, { width: NODE_W, height: table.height });
	for (const edge of edges) g.setEdge(edge.from, edge.to);
	dagre.layout(g);
	return tables.map((table) => {
		const node = g.node(table.key);
		return {
			id: table.key,
			type: "tableNode" as const,
			position: saved[table.key] ?? { x: node.x - NODE_W / 2, y: node.y - table.height / 2 },
			data: {
				schemaName: table.schemaName,
				name: table.table.name,
				columns: table.columns,
				height: table.height,
			},
		};
	});
}

function TableNode({ data }: NodeProps<TableNode>) {
	return (
		<div
			className="overflow-hidden rounded-md border border-seam bg-zinc-950/95 text-xs shadow-lg"
			style={{ width: NODE_W }}
		>
			{/* Table-level handles keep edges valid when the column pair is unknown. */}
			<Handle id="left" type="target" position={Position.Left} className="!bg-dim/40 !border-0 !w-2 !h-2" />
			<div className="flex items-center justify-between border-b border-seam bg-zinc-900 px-2 py-1.5">
				<span className="mono truncate font-bold text-foreground">{data.name}</span>
				<span className="text-[10px] text-dim">{data.schemaName}</span>
			</div>
			<div style={{ height: data.height - NODE_HEADER }}>
				{data.columns.map((column, index) => {
					// Absolute positioning resolves against the node container, so the
					// row-center offset is measured from the node top.
					const rowCenter = NODE_HEADER + index * NODE_ROW + NODE_ROW / 2;
					return (
						<div
							key={column.name}
							className="flex items-center gap-1 px-2"
							style={{ height: NODE_ROW }}
						>
							<Handle id={`col-${column.name}-left`} type="target" position={Position.Left} style={{ top: rowCenter }} className="!bg-dim/40 !border-0 !w-1.5 !h-1.5" />
							{column.pk && <span className="text-amber-400" title="primary key">🔑</span>}
							{column.fk && <span className="text-sky-400" title="foreign key">→</span>}
							<span className="mono truncate text-foreground">{column.name}</span>
							<span className="mono ml-auto shrink-0 text-[10px] text-dim">
								{column.type}
								{column.maxLen ? `(${column.maxLen})` : ""}
								{!column.nullable ? <span className="text-error">*</span> : null}
							</span>
							<Handle id={`col-${column.name}-right`} type="source" position={Position.Right} style={{ top: rowCenter }} className="!bg-dim/40 !border-0 !w-1.5 !h-1.5" />
						</div>
					);
				})}
			</div>
			<Handle id="right" type="source" position={Position.Right} className="!bg-dim/40 !border-0 !w-2 !h-2" />
		</div>
	);
}

const nodeTypes = { tableNode: TableNode };

function PageInner(props: {
	schema: DatabaseSchema;
	connectionId: string;
	canEdit: boolean;
	csrf: string;
}) {
	const { schema, connectionId, canEdit, csrf } = props;
	const flow = useReactFlow();

	const [schemaFilter, setSchemaFilter] = useState("all");
	const [search, setSearch] = useState("");
	const [selected, setSelected] = useState<string | null>(null);
	const [changes, setChanges] = useState<Change[]>([]);
	const [review, setReview] = useState(false);
	const [newTable, setNewTable] = useState(false);
	const [saveError, setSaveError] = useState<string | null>(null);
	const [layoutResetKey, setLayoutResetKey] = useState(0);

	const tables = useMemo(() => prepareTables(schema, schemaFilter), [schema, schemaFilter]);
	const tableByKey = useMemo(() => new Map(tables.map((t) => [t.key, t])), [tables]);

	const fkEdges = useMemo(() => {
		const edges: { from: string; to: string; sourceHandle: string; targetHandle: string }[] = [];
		const columnNames = (table: VisibleTable) => new Set(table.columns.map((column) => column.name));
		for (const s of schema.schemas) {
			if (schemaFilter !== "all" && s.schema !== schemaFilter) continue;
			for (const table of s.tables) {
				for (const constraint of table.constraints ?? []) {
					if (constraint.type !== "FOREIGN KEY" || !constraint.referenced_table) continue;
					const fromKey = `${s.schema}.${table.name}`;
					// The referenced table may live in another schema; resolve by name.
					const referenced = tableByKey.get(`${s.schema}.${constraint.referenced_table}`) ??
						tables.find((candidate) => candidate.table.name === constraint.referenced_table);
					if (!referenced) continue;
					const toKey = referenced.key;
					const hasColumns = constraint.columns.length > 0;
					const sourceColumn = hasColumns ? constraint.columns[0] : undefined;
					const targetColumn = constraint.referenced_columns?.[0];
					// Column→itself self-references draw nothing useful; skip them.
					if (toKey === fromKey && targetColumn === sourceColumn) continue;
					edges.push({
						from: fromKey,
						to: toKey,
						// Attach at the exact FK/referenced rows when both exist.
						sourceHandle: sourceColumn && columnNames(tableByKey.get(fromKey)!).has(sourceColumn) ? `col-${sourceColumn}-right` : "right",
						targetHandle: targetColumn && columnNames(referenced).has(targetColumn) ? `col-${targetColumn}-left` : "left",
					});
				}
			}
		}
		return edges;
	}, [schema, schemaFilter, tableByKey, tables]);

	const [nodes, setNodes, onNodesChange] = useNodesState<TableNode>([]);
	const [edges, setEdges] = useState<Edge[]>([]);
	// onNodeDragStop fires before the change-driven state commit; persist from
	// the render-provided latest list instead of the stale closure.
	const nodesRef = useRef<TableNode[]>([]);
	nodesRef.current = nodes;

	useEffect(() => {
		let saved: Record<string, { x: number; y: number }> = {};
		if (layoutResetKey >= 0) {
			try {
				const stored = localStorage.getItem(`orpbal:schema-layout:${connectionId}`);
				if (stored) saved = JSON.parse(stored) as typeof saved;
			} catch {
				saved = {};
			}
		}
		const next = layoutTables(tables, fkEdges, saved);
		setNodes(next);
		setEdges(
			fkEdges.map((edge) => ({
				id: `${edge.from}->${edge.to}->${edge.sourceHandle}->${edge.targetHandle}`,
				source: edge.from,
				target: edge.to,
				sourceHandle: edge.sourceHandle,
				targetHandle: edge.targetHandle,
				type: "smoothstep",
				markerEnd: { type: "arrowclosed" as const, width: 16, height: 16 },
				style: { stroke: "var(--fg, #888)", strokeWidth: 1.5, opacity: 0.7 },
			})),
		);
	}, [tables, fkEdges, layoutResetKey]);

	const persistPositions = useCallback(
		(current: TableNode[]) => {
			const saved: Record<string, { x: number; y: number }> = {};
			for (const node of current) saved[node.id] = { x: node.position.x, y: node.position.y };
			try {
				localStorage.setItem(`orpbal:schema-layout:${connectionId}`, JSON.stringify(saved));
			} catch {
				// localStorage unavailable: layout stays session-only.
			}
		},
		[connectionId],
	);

	const composed = useMemo(() => compose(changes), [changes]);
	const statements = useMemo(() => (review ? buildStatements(composed) : []), [review, composed]);
	const dropCount = statements.filter((s) => s.destructive).length;

	const selectedTable = selected ? tableByKey.get(selected) : undefined;

	const jumpTo = (key: string) => {
		const node = nodes.find((candidate) => candidate.id === key);
		if (!node) return;
		flow.setCenter(node.position.x + NODE_W / 2, node.position.y + node.data.height / 2, { zoom: 1, duration: 400 });
	};

	const clearChanges = () => {
		setChanges([]);
		setReview(false);
	};

	return (
		<div className="panel relative h-[75vh] overflow-hidden" data-connection={connectionId}>
			<div className="nameplate">SchemaGraph</div>

			<ReactFlow
				nodes={
					search.length === 0
						? nodes
						: nodes.filter((node) => node.id.toLowerCase().includes(search.toLowerCase()))
				}
				edges={
					search.length === 0
						? edges
						: edges.filter(
								(edge) =>
									edge.target.toLowerCase().includes(search.toLowerCase()) &&
									edge.source.toLowerCase().includes(search.toLowerCase()),
							)
				}
				nodeTypes={nodeTypes}
				onNodesChange={onNodesChange}
				fitView
				proOptions={{ hideAttribution: true }}
				minZoom={0.1}
				maxZoom={2}
				onNodeClick={(_, node) => setSelected(node.id)}
				onNodeDragStop={() => persistPositions(nodesRef.current)}
				onPaneClick={() => setSelected(null)}
			>
				<Controls position="bottom-right" />
			</ReactFlow>

			<div className="absolute bottom-3 left-3 z-10 flex flex-wrap items-center gap-2 bg-zinc-950/80 p-2 text-xs" style={{ border: "1px solid var(--seam, #333)" }}>
				<select
					className="field"
					value={schemaFilter}
					onChange={(event) => setSchemaFilter(event.target.value)}
					aria-label="Filter by schema"
				>
					<option value="all">All schemas</option>
					{schema.schemas.map((s) => (
						<option key={s.schema} value={s.schema}>
							{s.schema}
						</option>
					))}
				</select>
				<input
					className="field w-40"
					placeholder="Find table…"
					value={search}
					onChange={(event) => setSearch(event.target.value)}
					onKeyDown={(event) => {
						if (event.key === "Enter") {
							const hit = nodes.find((node) => node.id.toLowerCase().includes(search.toLowerCase()));
							if (hit) jumpTo(hit.id);
						}
					}}
				/>
				<button
					type="button"
					className="btn-brass px-2 py-1"
					onClick={() => {
						const hit = nodes.find((node) => node.id.toLowerCase().includes(search.toLowerCase()));
						if (hit) jumpTo(hit.id);
					}}
				>
					Go
				</button>
				<button
					type="button"
					className="text-dim underline decoration-dotted hover:text-foreground"
					onClick={() => {
						try {
							localStorage.removeItem(`orpbal:schema-layout:${connectionId}`);
						} catch {
							// ignore
						}
						setLayoutResetKey((key) => key + 1);
					}}
				>
					Reset layout
				</button>
				{canEdit && (
					<button type="button" className="btn-brass px-2 py-1" onClick={() => setNewTable(true)}>
						New table
					</button>
				)}
				{canEdit && composed.length > 0 && (
					<button type="button" className="btn-brass px-2 py-1" onClick={() => setReview(true)}>
						Review changes ({composed.length})
					</button>
				)}
			</div>

			{selectedTable && selected && (
				<TableDrawer
					table={selectedTable}
					allTables={tables}
					canEdit={canEdit}
					setChanges={setChanges}
					composedKeys={new Set(composed.map((change) => (change.kind === "createTable" ? "" : change.key)))}
					onClose={() => setSelected(null)}
					saveError={saveError}
					setSaveError={setSaveError}
				/>
			)}

			{newTable && canEdit && (
				<NewTableModal
					schemas={schema.schemas.map((s) => s.schema)}
					onClose={() => setNewTable(false)}
					onCreate={(change) => {
						setChanges((current) => [...current, change]);
						setNewTable(false);
						setReview(true);
					}}
				/>
			)}

			{review && (
				<ReviewModal
					statements={statements}
					dropCount={dropCount}
					connectionId={connectionId}
					csrf={csrf}
					onClose={() => setReview(false)}
					onClear={clearChanges}
				/>
			)}
		</div>
	);
}

// -- Drawer -----------------------------------------------------------------

function TableDrawer(props: {
	table: VisibleTable;
	allTables: VisibleTable[];
	canEdit: boolean;
	setChanges: (updater: (current: Change[]) => Change[]) => void;
	composedKeys: Set<string>;
	onClose: () => void;
	saveError: string | null;
	setSaveError: (error: string | null) => void;
}) {
	const { table, allTables, canEdit, setChanges, composedKeys, onClose, saveError, setSaveError } = props;
	const [editingColumn, setEditingColumn] = useState<string | null>(null);
	const [showAddColumn, setShowAddColumn] = useState(false);
	const [showAddFK, setShowAddFK] = useState(false);
	const [showAddPK, setShowAddPK] = useState(false);

	const primaryKey = (table.table.constraints ?? []).find((constraint) => constraint.type === "PRIMARY KEY");
	const foreignKeys = (table.table.constraints ?? []).filter((constraint) => constraint.type === "FOREIGN KEY");

	const push = (change: Change) => {
		if (!ident.test(table.schemaName) || !ident.test(table.table.name)) {
			setSaveError("Only plain identifiers can be edited through the form.");
			return;
		}
		setSaveError(null);
		setChanges((current) => [...current, change]);
	};

	const pendingFor = (name: string) =>
		composedKeys.has(`${table.schemaName}.${table.table.name}.${name}`);

	return (
		<aside className="absolute right-2 top-2 bottom-2 z-20 w-[360px] max-w-[90%] overflow-y-auto border border-seam bg-zinc-950/95 p-4 text-sm" style={{ boxShadow: "-8px 0 12px rgba(0,0,0,0.35)" }}>
			<div className="mb-3 flex items-center justify-between">
				<div>
					<h3 className="mono text-base font-bold text-foreground">{table.table.name}</h3>
					<p className="mono text-xs text-dim">{table.schemaName}</p>
				</div>
				<button type="button" onClick={onClose} className="text-dim hover:text-foreground" aria-label="Close drawer">✕</button>
			</div>

			{saveError && <p className="mb-2 text-xs text-error">{saveError}</p>}

			<section className="mb-4">
				<h4 className="mb-1 text-xs font-bold uppercase tracking-wide text-dim">Columns</h4>
				<div className="divide-y divide-seam/60 border-y border-seam/60">
					{table.columns.map((column) => (
						<div key={column.name} className="py-2">
							<div className="flex items-center gap-2">
								{column.pk && <span title="Primary key">🔑</span>}
								{column.fk && <span title="Foreign key">→</span>}
								<span className="mono font-semibold">{column.name}</span>
								<span className="mono ml-auto text-xs text-dim">
									{column.type}
									{column.maxLen ? `(${column.maxLen})` : ""}
									{column.nullable ? " ?" : ""}
								</span>
								{canEdit && (
									<>
										<button type="button" className="text-xs text-dim underline hover:text-foreground" onClick={() => setEditingColumn(editingColumn === column.name ? null : column.name)}>
											edit
										</button>
										<button
											type="button"
											className="text-xs text-error/90 underline hover:text-error"
											onClick={() =>
												push({
													kind: "removeColumn",
													key: `${table.schemaName}.${table.table.name}.${column.name}`,
													schema: table.schemaName,
													table: table.table.name,
													original: column.name,
												})
											}
										>
											drop
										</button>
									</>
								)}
							</div>
							{pendingFor(column.name) && <p className="mt-1 text-xs text-ok">pending change</p>}
							{canEdit && editingColumn === column.name && (
								<ColumnEditor
									initial={{
										name: column.name,
										type: column.type,
										nullable: column.nullable,
										default: column.defaultValue,
										pk: false,
									}}
									onSave={(spec) => {
										push({
											kind: "editColumn",
											key: `${table.schemaName}.${table.table.name}.${column.name}`,
											schema: table.schemaName,
											table: table.table.name,
											original: column.name,
											column: spec,
										});
										setEditingColumn(null);
									}}
									onCancel={() => setEditingColumn(null)}
								/>
							)}
						</div>
					))}
				</div>
				{canEdit && (
					<button type="button" className="mt-2 text-xs text-dim underline hover:text-foreground" onClick={() => setShowAddColumn(!showAddColumn)}>
						+ Add column
					</button>
				)}
				{canEdit && showAddColumn && (
					<ColumnEditor
						initial={{ name: "", type: "text", nullable: true, default: "", pk: false }}
						isNew
						onSave={(spec) => {
							push({
								kind: "addColumn",
								key: `${table.schemaName}.${table.table.name}.${spec.name}`,
								schema: table.schemaName,
								table: table.table.name,
								column: spec,
							});
							setShowAddColumn(false);
						}}
						onCancel={() => setShowAddColumn(false)}
					/>
				)}
			</section>

			<section className="mb-4">
				<h4 className="mb-1 text-xs font-bold uppercase tracking-wide text-dim">Primary key</h4>
				<p className="mono text-xs">
					{primaryKey ? primaryKey.columns.join(", ") : "none"}
				</p>
				{canEdit && (
					<div className="mt-1 flex gap-2">
						<button type="button" className="text-xs text-dim underline hover:text-foreground" onClick={() => setShowAddPK(!showAddPK)}>
							+ Set PK
						</button>
						{primaryKey && (
							<button
								type="button"
								className="text-xs text-error/90 underline hover:text-error"
								onClick={() =>
									push({
										kind: "removePrimaryKey",
										key: `${table.schemaName}.${table.table.name}.pk`,
										schema: table.schemaName,
										table: table.table.name,
										constraint: primaryKey.name,
									})
								}
							>
								drop PK
							</button>
						)}
					</div>
				)}
				{canEdit && showAddPK && (
					<PickColumns
						columns={table.columns.map((column) => column.name)}
						onSubmit={(picked) => {
							push({
								kind: "addPrimaryKey",
								key: `${table.schemaName}.${table.table.name}.pk`,
								schema: table.schemaName,
								table: table.table.name,
								columns: picked,
							});
							setShowAddPK(false);
						}}
						onCancel={() => setShowAddPK(false)}
					/>
				)}
			</section>

			<section className="mb-4">
				<h4 className="mb-1 text-xs font-bold uppercase tracking-wide text-dim">Foreign keys</h4>
				{foreignKeys.length === 0 ? (
					<p className="text-xs text-dim">none</p>
				) : (
					<ul className="space-y-1 text-xs">
						{foreignKeys.map((constraint) => (
							<li key={constraint.name} className="flex items-center gap-2">
								<span className="mono">{constraint.columns.join(", ")} → {constraint.referenced_table}</span>
								{canEdit && (
									<button
										type="button"
										className="text-error/90 underline hover:text-error"
										onClick={() =>
											push({
												kind: "removeForeignKey",
												key: `${table.schemaName}.${table.table.name}.${constraint.columns[0]}.fkey`,
												schema: table.schemaName,
												table: table.table.name,
												constraint: constraint.name,
											})
										}
									>
										drop
									</button>
								)}
							</li>
						))}
					</ul>
				)}
				{canEdit && (
					<button type="button" className="mt-1 text-xs text-dim underline hover:text-foreground" onClick={() => setShowAddFK(!showAddFK)}>
						+ Add foreign key
					</button>
				)}
				{canEdit && showAddFK && (
					<AddFKForm
						columns={table.columns.map((column) => column.name)}
						allTables={allTables}
						onSubmit={(column, toTable, toColumn) => {
							push({
								kind: "addForeignKey",
								key: `${table.schemaName}.${table.table.name}.${column}.fkey`,
								schema: table.schemaName,
								table: table.table.name,
								column,
								toSchema: toTable.schemaName,
								toTable: toTable.table.name,
								toColumn,
							});
							setShowAddFK(false);
						}}
						onCancel={() => setShowAddFK(false)}
					/>
				)}
			</section>

			<section>
				<h4 className="mb-1 text-xs font-bold uppercase tracking-wide text-dim">Indexes</h4>
				<ul className="space-y-1 text-xs">
					{table.table.indexes?.map((index) => (
						<li key={index.name} className="mono text-dim">
							{index.name} ({index.columns.join(", ")})
						</li>
					))}
				</ul>
			</section>
		</aside>
	);
}

// -- Small forms ------------------------------------------------------------

function ColumnEditor(props: {
	initial: ColumnSpec;
	isNew?: boolean;
	onSave: (spec: ColumnSpec) => void;
	onCancel: () => void;
}) {
	const { initial, isNew, onSave, onCancel } = props;
	const [spec, setSpec] = useState<ColumnSpec>(initial);
	const [error, setError] = useState<string | null>(null);
	const defaultChanged = spec.default !== initial.default;
	return (
		<div className="mt-2 grid gap-2 border border-seam bg-zinc-900 p-2">
			<input
				className="field"
				placeholder="column name"
				value={spec.name}
				autoFocus
				onChange={(event) => setSpec({ ...spec, name: event.target.value })}
			/>
			<div className="flex gap-2">
				<input
					className="field grow"
					placeholder="type"
					list="orpbal-pg-types"
					value={spec.type}
					onChange={(event) => setSpec({ ...spec, type: event.target.value })}
				/>
				<input
					className="field w-20"
					placeholder="len"
					onChange={(event) => {
						const len = Number(event.target.value);
						setSpec({ ...spec, type: `varchar(${len > 0 ? len : 255})` });
					}}
				/>
			</div>
			<input
				className="field"
				placeholder="default value"
				value={spec.default}
				onChange={(event) => setSpec({ ...spec, default: event.target.value })}
			/>
			<label className="flex items-center gap-2 text-xs text-dim">
				<input type="checkbox" checked={!spec.nullable} onChange={(event) => setSpec({ ...spec, nullable: !event.target.checked })} />
				NOT NULL
			</label>
			<div className="flex gap-2">
				<button
					type="button"
					className="btn-brass px-2 py-1 text-xs"
					onClick={() => {
						if (!ident.test(spec.name.trim())) {
							setError("Column name must be letters, digits or underscores.");
							return;
						}
						if (spec.type.trim().length === 0) {
							setError("A data type is required.");
							return;
						}
						onSave({ ...spec, name: spec.name.trim(), type: spec.type.trim(), defaultChanged });
					}}
				>
					{isNew ? "Add" : "Save"}
				</button>
				<button type="button" className="text-xs text-dim underline hover:text-foreground" onClick={onCancel}>
					cancel
				</button>
			</div>
			{error && <p className="text-xs text-error">{error}</p>}
		</div>
	);
}

function PickColumns(props: {
	columns: string[];
	onSubmit: (picked: string[]) => void;
	onCancel: () => void;
}) {
	const [picked, setPicked] = useState<string[]>([]);
	return (
		<div className="mt-2 grid gap-2 border border-seam bg-zinc-900 p-2">
			{props.columns.map((name) => (
				<label key={name} className="flex items-center gap-2 text-xs">
					<input
						type="checkbox"
						checked={picked.includes(name)}
						onChange={(event) =>
							setPicked(event.target.checked ? [...picked, name] : picked.filter((value) => value !== name))
						}
					/>
					<span className="mono">{name}</span>
				</label>
			))}
			<div className="flex gap-2">
				<button type="button" className="btn-brass px-2 py-1 text-xs" disabled={picked.length === 0} onClick={() => props.onSubmit(picked)}>
					Set
				</button>
				<button type="button" className="text-xs text-dim underline" onClick={props.onCancel}>
					cancel
				</button>
			</div>
		</div>
	);
}

function AddFKForm(props: {
	columns: string[];
	allTables: VisibleTable[];
	onSubmit: (column: string, toTable: VisibleTable, toColumn: string) => void;
	onCancel: () => void;
}) {
	const [column, setColumn] = useState(props.columns[0] ?? "");
	const [tableKey, setTableKey] = useState(props.allTables[0]?.key ?? "");
	const target = props.allTables.find((table) => table.key === tableKey);
	const [toColumn, setToColumn] = useState(target?.columns[0]?.name ?? "");
	return (
		<div className="mt-2 grid gap-2 border border-seam bg-zinc-900 p-2">
			<select className="field" value={column} onChange={(event) => setColumn(event.target.value)}>
				{props.columns.map((name) => (
					<option key={name}>{name}</option>
				))}
			</select>
			<select
				className="field"
				value={tableKey}
				onChange={(event) => {
					setTableKey(event.target.value);
					const next = props.allTables.find((table) => table.key === event.target.value);
					setToColumn(next?.columns[0]?.name ?? "");
				}}
			>
				{props.allTables.map((table) => (
					<option key={table.key} value={table.key}>
						{table.key}
					</option>
				))}
			</select>
			{target && (
				<select className="field" value={toColumn} onChange={(event) => setToColumn(event.target.value)}>
					{target.columns.map((column) => (
						<option key={column.name}>{column.name}</option>
					))}
				</select>
			)}
			<div className="flex gap-2">
				<button
					type="button"
					className="btn-brass px-2 py-1 text-xs"
					onClick={() => target && toColumn && props.onSubmit(column, target, toColumn)}
				>
					Add FK
				</button>
				<button type="button" className="text-xs text-dim underline" onClick={props.onCancel}>
					cancel
				</button>
			</div>
		</div>
	);
}

function NewTableModal(props: {
	schemas: string[];
	onClose: () => void;
	onCreate: (change: Change) => void;
}) {
	const [schemaName, setSchemaName] = useState(props.schemas[0] ?? "public");
	const [tableName, setTableName] = useState("");
	const [rows, setRows] = useState<ColumnSpec[]>([
		{ name: "id", type: "serial", nullable: false, default: "", pk: true },
		{ name: "", type: "text", nullable: true, default: "", pk: false },
	]);
	const [error, setError] = useState<string | null>(null);

	return (
		<div className="absolute inset-0 z-30 grid place-items-center bg-black/60 p-4">
			<div className="panel w-full max-w-[560px] p-4">
				<h3 className="nameplate">New table</h3>
				<div className="mb-3 grid gap-2">
					<div className="flex gap-2">
						<select className="field w-32" value={schemaName} onChange={(event) => setSchemaName(event.target.value)}>
							{props.schemas.map((name) => (
								<option key={name}>{name}</option>
							))}
						</select>
						<input
							className="field grow"
							placeholder="table name"
							value={tableName}
							onChange={(event) => setTableName(event.target.value)}
						/>
					</div>
					{rows.map((row, index) => (
						<div key={index} className="flex gap-1">
							<input
								className="field"
								placeholder={`column ${index + 1} name`}
								value={row.name}
								onChange={(event) =>
									setRows(rows.map((candidate, i) => (i === index ? { ...candidate, name: event.target.value } : candidate)))
								}
							/>
							<input
								className="field w-28"
								list="orpbal-pg-types"
								value={row.type}
								onChange={(event) =>
									setRows(rows.map((candidate, i) => (i === index ? { ...candidate, type: event.target.value } : candidate)))
								}
							/>
							<label className="flex items-center gap-1 text-xs text-dim">
								<input
									type="checkbox"
									checked={row.pk}
									onChange={(event) =>
										setRows(
											rows.map((candidate, i) =>
												i === index
													? { ...candidate, pk: event.target.checked, nullable: event.target.checked ? false : candidate.nullable }
													: candidate,
											),
										)
									}
								/>
								PK
							</label>
							<label className="flex items-center gap-1 text-xs text-dim">
								<input
									type="checkbox"
									checked={row.nullable}
									onChange={(event) =>
										setRows(rows.map((candidate, i) => (i === index ? { ...candidate, nullable: event.target.checked } : candidate)))
									}
								/>
								NULL
							</label>
							<button
								type="button"
								className="text-xs text-error/80 hover:text-error"
								onClick={() => setRows(rows.filter((_, i) => i !== index))}
							>
								✕
							</button>
						</div>
					))}
					<button
						type="button"
						className="text-xs text-dim underline hover:text-foreground"
						onClick={() => setRows([...rows, { name: "", type: "text", nullable: true, default: "", pk: false }])}
					>
						+ column
					</button>
				</div>
				<datalist id="orpbal-pg-types">
					{["text", "varchar", "integer", "bigint", "boolean", "timestamptz", "date", "numeric", "uuid", "jsonb", "serial"].map((type) => (
						<option key={type} value={type} />
					))}
				</datalist>
				{error && <p className="mb-2 text-xs text-error">{error}</p>}
				<div className="flex justify-end gap-2">
					<button type="button" className="text-xs text-dim underline" onClick={props.onClose}>
						cancel
					</button>
					<button
						type="button"
						className="btn-brass px-3 py-1 text-xs"
						onClick={() => {
							if (!ident.test(tableName.trim())) {
								setError("Table name must be letters, digits or underscores.");
								return;
							}
							const columns = rows.filter((row) => ident.test(row.name.trim()));
							if (columns.length === 0) {
								setError("At least one valid column is required.");
								return;
							}
							props.onCreate({
								kind: "createTable",
								schema: schemaName,
								table: tableName.trim(),
								columns: columns.map((row) => ({ ...row, name: row.name.trim(), type: row.type.trim() })),
							});
						}}
					>
						Compose migration
					</button>
				</div>
			</div>
		</div>
	);
}

function ReviewModal(props: {
	statements: { sql: string; destructive: boolean }[];
	dropCount: number;
	connectionId: string;
	csrf: string;
	onClose: () => void;
	onClear: () => void;
}) {
	const { statements, dropCount, connectionId, csrf, onClose, onClear } = props;
	const sqlText = statements.map((statement) => statement.sql).join("\n\n");
	return (
		<div className="absolute inset-0 z-30 grid place-items-center bg-black/60 p-4">
			<div className="panel w-full max-w-[640px] p-4">
				<div className="mb-3 flex items-center justify-between">
					<h3 className="nameplate">Review migration</h3>
					{dropCount > 0 && (
						<span className="border border-error/40 bg-error/10 px-2 py-1 text-xs text-error">
							{dropCount} destructive statement{dropCount > 1 ? "s" : ""}
						</span>
					)}
				</div>
				<div className="mono max-h-[45vh] overflow-auto border border-seam bg-zinc-950 p-3 text-xs whitespace-pre-wrap">
					{sqlText}
				</div>
				<p className="mt-2 text-xs text-dim">
					Runs via a new pending migration in the SQL editor; nothing is executed until you run it there. Close keeps the composed changes; discard clears them.
				</p>
				<div className="mt-3 flex justify-end">
					<button type="button" className="text-xs text-dim underline" onClick={onClose}>
						close
					</button>
				</div>
				<form method="POST" action={`?_action=createMigration`} className="mt-3 flex items-center justify-end gap-2">
					<input type="hidden" name="csrf_token" value={csrf} />
					<input type="hidden" name="id" value={connectionId} />
					<input type="hidden" name="sql" value={sqlText} />
					<button type="button" className="text-xs text-dim underline" onClick={onClear}>
						discard all
					</button>
					<button type="submit" className="btn-brass px-3 py-1 text-xs">
						Create migration
					</button>
				</form>
			</div>
		</div>
	);
}

export function SchemaGraph(props: { schema: DatabaseSchema; connectionId: string; canEdit: boolean; csrf: string }) {
	return (
		<ReactFlowProvider>
			<PageInner {...props} />
		</ReactFlowProvider>
	);
}
