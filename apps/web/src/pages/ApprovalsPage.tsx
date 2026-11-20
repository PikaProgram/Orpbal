import { useState } from "react";

import { useSession } from "@/auth/session";
import { approveApproval, listApprovals, rejectApproval, type Approval, type MigrationStatus } from "@/lib/migrations";
import { useLoad } from "@/lib/useApi";

function StatusBadge({ status }: { status: MigrationStatus | string }) {
	const cls =
		status === "success" || status === "approved"
			? "border-ok/40 bg-ok/10 text-ok"
			: status === "failed"
				? "border-error/40 bg-error/10 text-error"
				: "border-warning/40 bg-warning/10 text-warning";
	return <span className={`border px-2 py-1 text-xs ${cls}`}>{status.replace("_", " ")}</span>;
}

function ApprovalCard(props: { approval: Approval; csrf: string; onDecided: (id: number) => void }) {
	const { approval, csrf, onDecided } = props;
	const [busy, setBusy] = useState(false);
	const [rejecting, setRejecting] = useState(false);
	const [comment, setComment] = useState("");
	const [error, setError] = useState<string | null>(null);

	const decide = (fn: () => Promise<unknown>) => {
		setBusy(true);
		setError(null);
		fn()
			.then(() => onDecided(approval.id))
			.catch((err: unknown) => setError(err instanceof Error ? err.message : "Request failed"))
			.finally(() => setBusy(false));
	};

	return (
		<div className="border border-seam bg-zinc-950">
			<div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
				<div className="flex flex-wrap items-center gap-2 text-xs">
					<span className="mono font-bold text-foreground">v{approval.migration?.version ?? approval.migration_id}</span>
					{approval.migration && <StatusBadge status={approval.migration.status} />}
					<span className="text-dim">group {approval.group_name}</span>
					<span className="text-dim">
						by {approval.migration?.author_username ?? approval.leader_username}
					</span>
					<span className="mono text-dim">{approval.created_at}</span>
				</div>
				<div className="flex items-center gap-2">
					<button
						type="button"
						disabled={busy}
						className="btn-brass px-3 py-1.5 text-xs"
						onClick={() => decide(() => approveApproval(approval.id, csrf))}
					>
						Approve
					</button>
					<button
						type="button"
						disabled={busy}
						className="btn-steel px-3 py-1.5 text-xs"
						onClick={() => setRejecting((open) => !open)}
					>
						Reject
					</button>
				</div>
			</div>
			<div className="px-3 pb-3">
				{approval.migration && (
					<pre className="mono max-h-24 overflow-auto whitespace-pre-wrap text-xs text-dim">{approval.migration.sql_up}</pre>
				)}
				{error && <p className="text-sm text-error">{error}</p>}
				{rejecting && (
					<div className="mt-2 flex flex-wrap items-end gap-2 border-t border-seam pt-2">
						<label className="min-w-56 flex-1 text-xs">
							<span className="mb-1 block text-dim">Reason returned to the author (optional)</span>
							<input className="field" value={comment} onChange={(e) => setComment(e.target.value)} />
						</label>
						<button
							type="button"
							disabled={busy}
							className="btn-steel px-3 py-1.5 text-xs"
							onClick={() => decide(() => rejectApproval(approval.id, csrf, comment.trim() ? comment.trim() : undefined))}
						>
							Confirm reject
						</button>
					</div>
				)}
			</div>
		</div>
	);
}

export function ApprovalsPage() {
	const { session } = useSession();
	const csrf = session?.csrf_token ?? "";
	// The backend reports which approvals belong to the current user; the
	// status filter keeps the inbox focused on decisions still owed.
	const { data: approvals, loading, error, reload } = useLoad("approvals:pending", () => listApprovals("pending"));

	const [removed, setRemoved] = useState<Record<number, true>>({});

	const onDecided = (id: number) => {
		// Optimistically drop the row; the next full load re-syncs.
		setRemoved((current) => ({ ...current, [id]: true }));
		reload();
	};

	const visible = (approvals ?? []).filter((approval) => !removed[approval.id]);

	return (
		<div>
			<div className="mb-6">
				<h1 className="text-2xl font-semibold tracking-tight">Approvals</h1>
				<p className="text-sm text-dim">Migrations submitted for your groups' review.</p>
			</div>

			{loading && <p className="text-sm text-dim">Loading...</p>}
			{error && <div className="error-note">{error}</div>}
			{!loading && !error && visible.length === 0 && (
				<div className="panel p-6 text-sm text-dim">No pending approvals. You are all caught up.</div>
			)}
			{visible.length > 0 && (
				<div className="space-y-2">
					{visible.map((approval) => (
						<ApprovalCard key={approval.id} approval={approval} csrf={csrf} onDecided={onDecided} />
					))}
				</div>
			)}
		</div>
	);
}
