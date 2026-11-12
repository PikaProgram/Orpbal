import { useState, type SubmitEvent } from "react";

import { useSession } from "@/auth/session";
import { api, type User } from "@/lib/api";
import { useAction, useLoad, useMutation } from "@/lib/useApi";

const inputClass = "field";
const buttonClass = "btn-brass px-3 py-1.5 text-xs";
const quietButtonClass = "btn-steel px-3 py-1.5 text-xs";

export function AdminUsersPage() {
	const { session } = useSession();
	const csrf = session?.csrf_token ?? "";
	const { data: users, loading, error, reload } = useLoad("admin/users", () => api<User[]>("/admin/users").then((r) => r.data));
	const create = useAction();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [admin, setAdmin] = useState(false);
	const [resetPassword, setResetPassword] = useState<Record<number, string>>({});

	const mutation = useMutation(reload);

	const submitCreate = (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		void create.run(async () => {
			await api("/admin/users", {
				method: "POST",
				body: { username, password, is_platform_admin: admin },
				csrf,
			});
			setUsername("");
			setPassword("");
			setAdmin(false);
			reload();
		});
	};

	return (
		<div>
			<div className="mb-6 flex flex-wrap items-end justify-between gap-2">
				<div>
					<h1 className="text-2xl font-semibold tracking-tight">Users</h1>
					<p className="text-sm text-zinc-400">Platform accounts and their administrative flags.</p>
				</div>
			</div>

			{create.error && <p className="error-note">{create.error}</p>}
			{mutation.error && <p className="error-note">{mutation.error}</p>}

			<form onSubmit={submitCreate} className="mb-6 grid gap-3 panel p-4 sm:grid-cols-2 lg:grid-cols-5">
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Username</span>
					<input required className={inputClass} value={username} onChange={(e) => setUsername(e.target.value)} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Password</span>
					<input type="password" required className={inputClass} value={password} onChange={(e) => setPassword(e.target.value)} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Platform admin</span>
					<select className={inputClass} value={admin ? "true" : "false"} onChange={(e) => setAdmin(e.target.value === "true")}>
						<option value="false">No</option>
						<option value="true">Yes</option>
					</select>
				</label>
				<div className="flex items-end">
					<button type="submit" disabled={create.busy} className={buttonClass}>
						{create.busy ? "Creating…" : "Create user"}
					</button>
				</div>
			</form>

			<div className="panel overflow-hidden">
				<div className="overflow-x-auto">
					<table className="w-full text-left text-sm">
						<thead className="border-b-2 border-seam bg-plate-raised text-dim">
							<tr>
								<th className="px-4 py-3 font-medium">Username</th>
								<th className="px-4 py-3 font-medium">Status</th>
								<th className="px-4 py-3 font-medium">Platform admin</th>
								<th className="px-4 py-3 font-medium">Actions</th>
							</tr>
						</thead>
						<tbody className="divide-y divide-zinc-800 bg-zinc-950">
							{(users ?? []).map((user) => (
								<tr key={user.id}>
									<td className="px-4 py-3 font-medium">{user.username}</td>
									<td className="px-4 py-3">
										<span className={user.is_active ? "text-emerald-400" : "text-zinc-500"}>
											{user.is_active ? "Active" : "Inactive"}
										</span>
									</td>
									<td className="px-4 py-3">{user.is_platform_admin ? "Yes" : "No"}</td>
									<td className="px-4 py-3">
										<div className="flex flex-wrap items-center gap-2">
											<button
												type="button"
												disabled={mutation.busy}
												className={quietButtonClass}
												onClick={() =>
													void mutation.run(() =>
														api(`/admin/users/${user.id}`, {
															method: "PATCH",
															body: { is_active: !user.is_active },
															csrf,
														}),
													)
												}
											>
												{user.is_active ? "Deactivate" : "Activate"}
											</button>
											<button
												type="button"
												disabled={mutation.busy}
												className={quietButtonClass}
												onClick={() =>
													void mutation.run(() =>
														api(`/admin/users/${user.id}`, {
															method: "PATCH",
															body: { is_platform_admin: !user.is_platform_admin },
															csrf,
														}),
													)
												}
											>
												{user.is_platform_admin ? "Remove admin" : "Make admin"}
											</button>
											<div className="flex items-center gap-2">
												<input
													type="password"
													placeholder="New password"
													required
													className="field field-sm w-36"
													value={resetPassword[user.id] ?? ""}
													onChange={(e) => setResetPassword((current) => ({ ...current, [user.id]: e.target.value }))}
												/>
												<button
													type="button"
													disabled={mutation.busy || !(resetPassword[user.id] ?? "").length}
													className={quietButtonClass}
													onClick={() =>
														void mutation.run(async () => {
															await api(`/admin/users/${user.id}/password`, {
																method: "POST",
																body: { new_password: resetPassword[user.id] },
																csrf,
															});
															setResetPassword((current) => {
																const next = { ...current };
																delete next[user.id];
																return next;
															});
														})
													}
												>
													Reset
												</button>
											</div>
										</div>
									</td>
								</tr>
							))}
						</tbody>
					</table>
					{loading && <div className="px-4 py-3 text-sm text-dim">Loading…</div>}
					{error && <div className="px-4 py-3 text-sm text-error">{error}</div>}
				</div>
			</div>
		</div>
	);
}
