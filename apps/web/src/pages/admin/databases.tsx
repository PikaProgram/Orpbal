import { useState, type SubmitEvent } from "react";

import { useSession } from "@/auth/session";
import { api, type DatabaseConnection } from "@/lib/api";
import { useAction, useLoad, useMutation, type MutationState } from "@/lib/useApi";

const engines = [
	{ value: "postgres", label: "PostgreSQL", port: 5432 },
	{ value: "mysql", label: "MySQL", port: 3306 },
];
const sslModes = ["disable", "require", "verify-ca", "verify-full"];

const buttonClass = "btn-brass px-3 py-1.5 text-xs";
const quietButtonClass = "btn-steel px-3 py-1.5 text-xs";
const inputClass = "field";

interface ConnectionForm {
	name: string;
	host: string;
	port: number;
	database_name: string;
	username: string;
	password: string;
	ssl_mode: string;
}

const emptyForm = {
	name: "",
	host: "",
	port: 5432,
	database_name: "",
	username: "",
	password: "",
	ssl_mode: "disable",
};

export function AdminConnectionsPage() {
	const { session } = useSession();
	const csrf = session?.csrf_token ?? "";
	const create = useAction();
	const [engine, setEngine] = useState("postgres");
	const [form, setForm] = useState<ConnectionForm>(emptyForm);

	const { data: connections, loading, error, reload } = useLoad(
		"admin/databases",
		() => api<DatabaseConnection[]>("/admin/databases").then((r) => r.data),
	);
	const mutation = useMutation(reload);

	const submitCreate = (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		void create.run(async () => {
			const body: Record<string, unknown> = { ...form, engine };
			if (!form.password) delete body.password;
			await api("/admin/databases", { method: "POST", body, csrf });
			setForm(emptyForm);
			reload();
		});
	};

	return (
		<div>
			<h1 className="text-2xl font-semibold tracking-tight">Database connections</h1>
			<p className="mb-6 text-sm text-zinc-400">
				Managed targets (PostgreSQL and MySQL). Creating or enabling a connection verifies it first; the engine cannot be changed afterwards.
			</p>

			{create.error && <p className="error-note">{create.error}</p>}
			{mutation.error && <p className="error-note">{mutation.error}</p>}

			<form onSubmit={submitCreate} className="mb-6 grid gap-3 panel p-4 sm:grid-cols-2 lg:grid-cols-4">
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Engine</span>
					<select
						className={inputClass}
						value={engine}
						onChange={(e) => {
							setEngine(e.target.value);
							const engineMeta = engines.find((entry) => entry.value === e.target.value);
							if (engineMeta) setForm((current) => ({ ...current, port: engineMeta.port }));
						}}
					>
						{engines.map((e) => (
							<option key={e.value} value={e.value}>
								{e.label}
							</option>
						))}
					</select>
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Name</span>
					<input required className={inputClass} value={form.name} onChange={(e) => setForm((c) => ({ ...c, name: e.target.value }))} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Host</span>
					<input required className={inputClass} value={form.host} onChange={(e) => setForm((c) => ({ ...c, host: e.target.value }))} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Port</span>
					<input
						type="number"
						min={1}
						max={65535}
						required
						className={inputClass}
						value={form.port}
						onChange={(e) => setForm((c) => ({ ...c, port: Number(e.target.value) }))}
					/>
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Database</span>
					<input required className={inputClass} value={form.database_name} onChange={(e) => setForm((c) => ({ ...c, database_name: e.target.value }))} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Username</span>
					<input required className={inputClass} value={form.username} onChange={(e) => setForm((c) => ({ ...c, username: e.target.value }))} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Password</span>
					<input type="password" className={inputClass} value={form.password} onChange={(e) => setForm((c) => ({ ...c, password: e.target.value }))} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">SSL mode</span>
					<select className={inputClass} value={form.ssl_mode} onChange={(e) => setForm((c) => ({ ...c, ssl_mode: e.target.value }))}>
						{sslModes.map((mode) => (
							<option key={mode} value={mode}>
								{mode}
							</option>
						))}
					</select>
				</label>
				<div className="flex items-end">
					<button type="submit" disabled={create.busy} className={buttonClass}>
						{create.busy ? "Adding…" : "Add connection"}
					</button>
				</div>
			</form>

			<div className="space-y-4">
				{(connections ?? []).map((connection) => (
					<ConnectionSection key={connection.id} connection={connection} csrf={csrf} mutation={mutation} />
				))}
				{loading && <p className="text-sm text-dim">Loading…</p>}
				{error && <p className="text-sm text-error">{error}</p>}
				{!loading && !error && (connections?.length ?? 0) === 0 && (
					<p className="panel p-6 text-sm text-zinc-400">No connections yet.</p>
				)}
			</div>
		</div>
	);
}

function ConnectionSection(props: { connection: DatabaseConnection; csrf: string; mutation: MutationState }) {
	const { connection, csrf, mutation } = props;
	const [editing, setEditing] = useState(false);
	const [edit, setEdit] = useState<ConnectionForm | null>(null);

	const startEdit = () =>
		setEdit({
			name: connection.name,
			host: connection.host,
			port: connection.port,
			database_name: connection.database_name,
			username: connection.username,
			password: "",
			ssl_mode: connection.ssl_mode,
		});

	const save = async () => {
		if (edit === null) return;
		const body: Record<string, unknown> = { ...edit };
		if (!edit.password) delete body.password;
		await api(`/admin/databases/${connection.id}`, { method: "PATCH", body, csrf });
	};

	return (
		<section className="panel p-4">
			<div className="mb-3 flex flex-wrap items-start justify-between gap-3">
				<div>
					<h2 className="text-lg font-medium">{connection.name}</h2>
					<p className="mono text-xs text-zinc-400">
						{connection.host}:{connection.port}/{connection.database_name} · {connection.username}
					</p>
				</div>
				<div className="flex items-center gap-2 text-xs">
					<span className={connection.is_enabled ? "text-emerald-400" : "text-zinc-500"}>
						{connection.is_enabled ? "Enabled" : "Disabled"}
					</span>
					<span className="uppercase text-zinc-500">{connection.engine}</span>
					<span className="text-zinc-500">SSL: {connection.ssl_mode}</span>
					<span className="text-zinc-500">{connection.has_password ? "Password stored" : "No password"}</span>
				</div>
			</div>

			<div className="mb-2">
				<button
					type="button"
					disabled={mutation.busy}
					className={quietButtonClass}
					onClick={() =>
						void mutation.run(() =>
							api(`/admin/databases/${connection.id}`, {
								method: "PATCH",
								body: { is_enabled: !connection.is_enabled },
								csrf,
							}),
						)
					}
				>
					{connection.is_enabled ? "Disable" : "Enable"}
				</button>
			</div>

			<div className="border-t-2 border-seam pt-3">
				{editing ? (
					<form
						className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4"
						onSubmit={(event) => {
							event.preventDefault();
							void mutation.run(async () => {
								await save();
								setEditing(false);
								setEdit(null);
							});
						}}
					>
						<input required className={inputClass} value={edit?.name ?? connection.name} onChange={(e) => setEdit((c) => (c ? { ...c, name: e.target.value } : c))} />
						<input required className={inputClass} value={edit?.host ?? connection.host} onChange={(e) => setEdit((c) => (c ? { ...c, host: e.target.value } : c))} />
						<input
							type="number"
							min={1}
							max={65535}
							required
							className={inputClass}
							value={edit?.port ?? connection.port}
							onChange={(e) => setEdit((c) => (c ? { ...c, port: Number(e.target.value) } : c))}
						/>
						<input
							required
							className={inputClass}
							value={edit?.database_name ?? connection.database_name}
							onChange={(e) => setEdit((c) => (c ? { ...c, database_name: e.target.value } : c))}
						/>
						<input
							required
							className={inputClass}
							value={edit?.username ?? connection.username}
							onChange={(e) => setEdit((c) => (c ? { ...c, username: e.target.value } : c))}
						/>
						<input
							type="password"
							placeholder="Leave blank to keep"
							className={inputClass}
							value={edit?.password ?? ""}
							onChange={(e) => setEdit((c) => (c ? { ...c, password: e.target.value } : c))}
						/>
						<select
							className={inputClass}
							value={edit?.ssl_mode ?? connection.ssl_mode}
							onChange={(e) => setEdit((c) => (c ? { ...c, ssl_mode: e.target.value } : c))}
						>
							{sslModes.map((mode) => (
								<option key={mode} value={mode}>
									{mode}
								</option>
							))}
						</select>
						<div className="flex items-end gap-2">
							<button type="submit" disabled={mutation.busy} className={quietButtonClass}>
								Save changes
							</button>
							<button
								type="button"
								className="text-xs text-dim underline"
								onClick={() => {
									setEditing(false);
									setEdit(null);
								}}
							>
								Cancel
							</button>
						</div>
					</form>
				) : (
					<button
						type="button"
						className="cursor-pointer text-xs text-zinc-400"
						onClick={() => {
							setEditing(true);
							startEdit();
						}}
					>
						Edit connection
					</button>
				)}
			</div>
		</section>
	);
}
