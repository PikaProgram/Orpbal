import { Link } from "react-router-dom";

import { api, type MyDatabase } from "@/lib/api";
import { useLoad } from "@/lib/useApi";

export function DatabasesPage() {
	const { data: databases, loading, error } = useLoad("databases", () => api<MyDatabase[]>("/me/databases").then((r) => r.data));

	return (
		<div>
			<h1 className="mb-2 text-2xl font-bold tracking-tight">Databases</h1>
			<p className="mb-6 max-w-2xl text-sm text-dim">
				Connections available through your groups. Effective access is the highest grant you hold.
			</p>

			{loading && <p className="text-sm text-dim">Loading…</p>}
			{error && <div className="panel p-6 text-sm text-error">{error}</div>}

			{!loading && databases !== null && (
				(databases.length === 0 ? (
					<div className="panel p-6 text-sm text-dim">
						No database connections are available to your groups yet.
					</div>
				) : (
					<div className="panel overflow-hidden">
						<div className="nameplate">Connected lines</div>
						<div className="overflow-x-auto">
							<table className="w-full text-left text-sm">
								<thead className="bg-plate-raised text-dim">
									<tr>
										<th className="px-4 py-3 font-semibold">Name</th>
										<th className="px-4 py-3 font-semibold">Host</th>
										<th className="px-4 py-3 font-semibold">Database</th>
										<th className="px-4 py-3 font-semibold">Access</th>
									</tr>
								</thead>
								<tbody className="divide-y-2 divide-seam bg-zinc-950">
									{databases.map((database) => (
										<tr key={database.connection_id}>
											<td className="px-4 py-3 font-semibold">
												<Link
													className="text-accent hover:underline"
													to={`/databases/${database.connection_id}`}
												>
													{database.name}
												</Link>
											</td>
											<td className="mono px-4 py-3">{database.host}</td>
											<td className="mono px-4 py-3">{database.database_name}</td>
											<td className="px-4 py-3">
												{database.access_level === "view_edit" ? (
													<span className="border border-ok/40 bg-ok/10 px-2 py-1 text-xs text-ok">
														View + Edit
													</span>
												) : (
													<span className="border border-dim/40 bg-dim/10 px-2 py-1 text-xs text-dim">
														View Only
													</span>
												)}
											</td>
										</tr>
									))}
								</tbody>
							</table>
						</div>
					</div>
				))
			)}
		</div>
	);
}
