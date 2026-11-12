import { useState, type SubmitEvent } from "react";

import { useSession } from "@/auth/session";
import { api, type Group, type GroupDetail, type User } from "@/lib/api";
import { useAction, useLoad, useMutation, type MutationState } from "@/lib/useApi";

const buttonClass = "btn-brass px-3 py-1.5 text-xs";
const quietButtonClass = "btn-steel px-3 py-1.5 text-xs";

export function AdminGroupsPage() {
	const { session } = useSession();
	const csrf = session?.csrf_token ?? "";
	const create = useAction();
	const [name, setName] = useState("");
	const [description, setDescription] = useState("");

	const { data, loading, error, reload } = useLoad("admin/groups", async () => {
		const [groups, users] = await Promise.all([
			api<Group[]>("/admin/groups").then((r) => r.data),
			api<User[]>("/admin/users").then((r) => r.data),
		]);
		const details = await Promise.all(groups.map((group) => api<GroupDetail>(`/admin/groups/${group.id}`).then((r) => r.data)));
		return { groups, users, details: new Map(details.map((detail) => [detail.id, detail])) };
	});

	const mutation = useMutation(reload);

	const submitCreate = (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		void create.run(async () => {
			await api("/admin/groups", { method: "POST", body: { name, description }, csrf });
			setName("");
			setDescription("");
			reload();
		});
	};

	return (
		<div>
			<h1 className="text-2xl font-semibold tracking-tight">Groups</h1>
			<p className="mb-6 text-sm text-zinc-400">
				Database access is granted to groups. A user in several groups receives the highest grant.
			</p>

			{create.error && <p className="error-note">{create.error}</p>}
			{mutation.error && <p className="error-note">{mutation.error}</p>}

			<form onSubmit={submitCreate} className="mb-6 grid gap-3 panel p-4 sm:grid-cols-3">
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Name</span>
					<input required minLength={2} maxLength={64} className="field" value={name} onChange={(e) => setName(e.target.value)} />
				</label>
				<label className="block text-sm">
					<span className="mb-1 block text-zinc-400">Description</span>
					<input className="field" value={description} onChange={(e) => setDescription(e.target.value)} />
				</label>
				<div className="flex items-end">
					<button type="submit" disabled={create.busy} className={buttonClass}>
						{create.busy ? "Creating…" : "Create group"}
					</button>
				</div>
			</form>

			<div className="space-y-4">
				{(data?.groups ?? []).map((group) => (
					<GroupSection key={group.id} group={group} detail={data?.details.get(group.id)} users={data?.users ?? []} csrf={csrf} mutation={mutation} />
				))}
				{loading && <p className="text-sm text-dim">Loading…</p>}
				{error && <p className="text-sm text-error">{error}</p>}
				{!loading && !error && (data?.groups.length ?? 0) === 0 && (
					<p className="panel p-6 text-sm text-zinc-400">No groups yet.</p>
				)}
			</div>
		</div>
	);
}

function GroupSection(props: {
	group: Group;
	detail?: GroupDetail;
	users: User[];
	csrf: string;
	mutation: MutationState;
}) {
	const { group, detail, users, csrf, mutation } = props;
	const [editName, setEditName] = useState(group.name);
	const [editDescription, setEditDescription] = useState(group.description);
	const [addUserId, setAddUserId] = useState("");

	const members = detail?.members ?? [];
	const memberIds = new Set(members.map((member) => member.id));
	const available = users.filter((user) => !memberIds.has(user.id));

	const patch = (fn: () => Promise<unknown>) => {
		void mutation.run(async () => {
			await fn();
			// Re-sync the edit form after a save.
			if (detail) {
				setEditName(detail.name);
				setEditDescription(detail.description);
			}
		});
	};

	return (
		<section className="panel p-4">
			<div className="mb-3 flex flex-wrap items-start justify-between gap-3">
				<div>
					<h2 className="text-lg font-medium">{group.name}</h2>
					<p className="text-sm text-zinc-400">{group.description || "No description"}</p>
				</div>
				<div className="text-xs text-zinc-500">
					{members.length} member{members.length === 1 ? "" : "s"} · {detail?.grants.length ?? 0} grant
					{(detail?.grants.length ?? 0) === 1 ? "" : "s"}
				</div>
			</div>

			<div className="mb-3 flex flex-wrap gap-2">
				{members.map((member) => (
					<span key={member.id} className="flex items-center gap-1 border border-seam bg-plate-raised px-2 py-1 text-xs">
						{member.username}
						<button
							type="button"
							disabled={mutation.busy}
							className="text-red-400 hover:text-red-300"
							onClick={() =>
								void mutation.run(() =>
									api(`/admin/groups/${group.id}/members/${member.id}`, { method: "DELETE", csrf }),
								)
							}
						>
							Remove
						</button>
					</span>
				))}
				{members.length === 0 && <span className="text-xs text-zinc-500">No members</span>}
			</div>

			<div className="mb-3 flex flex-wrap items-end gap-2">
				<label className="block text-xs">
					<span className="mb-1 block text-zinc-400">Add member</span>
					<select className="field field-sm" value={addUserId} onChange={(e) => setAddUserId(e.target.value)}>
						{available.map((user) => (
							<option key={user.id} value={user.id}>
								{user.username}
							</option>
						))}
					</select>
				</label>
				<button
					type="button"
					disabled={mutation.busy || available.length === 0}
					className={quietButtonClass}
					onClick={() =>
						void mutation.run(() =>
							api(`/admin/groups/${group.id}/members/${addUserId}`, { method: "PUT", csrf }).then(() =>
								setAddUserId(""),
							),
						)
					}
				>
					Add
				</button>
			</div>

			<details className="border-t-2 border-seam pt-3">
				<summary className="cursor-pointer text-xs text-zinc-400">Edit group</summary>
				<div className="mt-3 grid gap-2 sm:grid-cols-3">
					<input
						required
						className="field"
						value={editName}
						onChange={(e) => setEditName(e.target.value)}
					/>
					<input
						className="field"
						value={editDescription}
						onChange={(e) => setEditDescription(e.target.value)}
					/>
					<div>
						<button
							type="button"
							disabled={mutation.busy}
							className={quietButtonClass}
							onClick={() =>
								patch(() =>
									api(`/admin/groups/${group.id}`, {
										method: "PATCH",
										body: { name: editName, description: editDescription },
										csrf,
									}),
								)
							}
						>
							Save
						</button>
					</div>
				</div>
			</details>

			<button
				type="button"
				disabled={mutation.busy}
				onClick={() => {
					if (confirm("Delete this group and all its grants?")) {
						void mutation.run(() => api(`/admin/groups/${group.id}`, { method: "DELETE", csrf }));
					}
				}}
				className="mt-3 text-xs text-red-400 hover:text-red-300"
			>
				Delete group
			</button>
		</section>
	);
}
