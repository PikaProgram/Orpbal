import { useSession } from "@/auth/session";
import { api, type DatabaseConnection, type Grant, type Group } from "@/lib/api";
import { useLoad, useMutation } from "@/lib/useApi";

const selectClass = "field field-sm";

export function AdminGrantsPage() {
	const { session } = useSession();
	const csrf = session?.csrf_token ?? "";

	const { data, loading, error, reload } = useLoad("admin/grants", async () => {
		const [groups, connections, grants] = await Promise.all([
			api<Group[]>("/admin/groups").then((r) => r.data),
			api<DatabaseConnection[]>("/admin/databases").then((r) => r.data),
			api<Grant[]>("/admin/grants").then((r) => r.data),
		]);
		return { groups, connections, grants: new Map(grants.map((grant) => [`${grant.group_id}:${grant.connection_id}`, grant])) };
	});

	const mutation = useMutation(reload);

	return (
		<div>
			<h1 className="text-2xl font-semibold tracking-tight">Grants</h1>
			<p className="mb-6 text-sm text-zinc-400">
				Set each group's access per connection. A user in several groups receives the highest level.
			</p>

			{mutation.error && <p className="error-note">{mutation.error}</p>}

			{loading && <p className="text-sm text-dim">Loading…</p>}
			{error && <p className="text-sm text-error">{error}</p>}

			{!loading && !error && data && (data.groups.length === 0 || data.connections.length === 0 ? (
				<p className="panel p-6 text-sm text-zinc-400">
					Create at least one group and one connection before assigning grants.
				</p>
			) : (
				<div className="panel overflow-x-auto">
					<table className="w-full text-left text-sm">
						<thead className="border-b-2 border-seam bg-plate-raised text-dim">
							<tr>
								<th className="px-4 py-3 font-medium">Group</th>
								{data.connections.map((connection) => (
									<th key={connection.id} className="px-4 py-3 font-medium">
										{connection.name}
										<span className="mono block text-xs font-normal text-zinc-500">
											{connection.host}/{connection.database_name}
										</span>
									</th>
								))}
							</tr>
						</thead>
						<tbody className="divide-y divide-zinc-800 bg-zinc-950">
							{data.groups.map((group) => (
								<tr key={group.id}>
									<td className="px-4 py-3 font-medium">{group.name}</td>
									{data.connections.map((connection) => {
										const grant = data.grants.get(`${group.id}:${connection.id}`);
										return (
											<td key={connection.id} className="px-4 py-3">
												<div className="flex flex-wrap items-center gap-2">
													<select
														className={selectClass}
														value={grant?.access_level ?? "view_only"}
														disabled={mutation.busy}
														onChange={(e) =>
															void mutation.run(() =>
																api(`/admin/groups/${group.id}/grants/${connection.id}`, {
																	method: "PUT",
																	body: { access_level: e.target.value },
																	csrf,
																}),
															)
														}
													>
														<option value="view_only">View Only</option>
														<option value="view_edit">View + Edit</option>
													</select>
												</div>
												{grant && (
													<button
														type="button"
														disabled={mutation.busy}
														className="mt-1 text-xs text-red-400 hover:text-red-300"
														onClick={() =>
															void mutation.run(() =>
																api(`/admin/groups/${group.id}/grants/${connection.id}`, { method: "DELETE", csrf }),
															)
														}
													>
														Remove
													</button>
												)}
											</td>
										);
									})}
								</tr>
							))}
						</tbody>
					</table>
				</div>
			))}
		</div>
	);
}
