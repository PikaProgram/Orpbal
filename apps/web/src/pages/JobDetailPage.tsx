import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";

import { useSession } from "@/auth/session";
import { cancelJob, getJob, type Job } from "@/lib/migrations";

const TERMINAL: Record<string, true> = { success: true, failed: true, cancelled: true, partial: true };

function statusClass(status: string): string {
	if (status === "success" || status === "partial") return "border-ok/40 bg-ok/10 text-ok";
	if (status === "failed") return "border-error/40 bg-error/10 text-error";
	if (status === "cancelled" || status === "cancelling") return "border-dim/40 bg-dim/10 text-dim";
	return "border-warning/40 bg-warning/10 text-warning";
}

export function JobDetailPage() {
	const { id = "", jobId = "" } = useParams();
	const { session } = useSession();
	const csrf = session?.csrf_token ?? "";

	const [job, setJob] = useState<Job | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [cancelBusy, setCancelBusy] = useState(false);

	useEffect(() => {
		if (!jobId) return;
		let cancelled = false;
		let timer: number | undefined;

		const poll = () => {
			getJob(jobId)
				.then((data) => {
					if (cancelled) return;
					setJob(data);
					setError(null);
					if (!TERMINAL[data.status]) {
						timer = window.setTimeout(poll, 2000);
					}
				})
				.catch((err: unknown) => {
					if (cancelled) return;
					setError(err instanceof Error ? err.message : "Request failed");
					timer = window.setTimeout(poll, 2000);
				});
		};

		poll();
		return () => {
			cancelled = true;
			if (timer !== undefined) window.clearTimeout(timer);
		};
	}, [jobId]);

	const cancel = () => {
		setCancelBusy(true);
		cancelJob(jobId, csrf)
			.then(setJob)
			.catch((err: unknown) => setError(err instanceof Error ? err.message : "Request failed"))
			.finally(() => setCancelBusy(false));
	};

	const isBatch = job?.batch_cursor !== null && job?.batch_cursor !== undefined;
	const running = job !== null && !TERMINAL[job.status];

	return (
		<div>
			<div className="mb-4 flex flex-wrap items-center justify-between gap-3">
				<div>
					<h1 className="text-2xl font-semibold tracking-tight">Job #{jobId}</h1>
					<p className="text-sm text-dim">
						Migration v{job?.migration_id ?? "…"} execution #{job?.execution_id ?? "…"} on database #{id}.
					</p>
				</div>
				<Link to={`/databases/${id}/migrations`} className="text-sm text-dim underline hover:text-foreground">
					← Back to migrations
				</Link>
			</div>

			{error && <div className="error-note">{error}</div>}
			{job === null && !error && <p className="text-sm text-dim">Loading...</p>}

			{job !== null && (
				<div className="panel p-4">
					<div className="flex flex-wrap items-center gap-3">
						<span className={`border px-2 py-1 text-xs ${statusClass(job.status)}`}>{job.status}</span>
						<span className="text-xs text-dim">attempts: {job.attempts ?? 1}</span>
						{running && <span className="text-xs text-dim">Polling every 2s…</span>}
						{running && job.status === "running" && (
							<button type="button" disabled={cancelBusy} className="btn-steel px-3 py-1.5 text-xs" onClick={cancel}>
								{cancelBusy ? "Cancelling…" : "Cancel"}
							</button>
						)}
					</div>
					<dl className="mt-4 grid gap-2 text-sm sm:grid-cols-2">
						<div>
							<dt className="text-dim">Created</dt>
							<dd className="mono">{job.created_at}</dd>
						</div>
						{job.started_at && (
							<div>
								<dt className="text-dim">Started</dt>
								<dd className="mono">{job.started_at}</dd>
							</div>
						)}
						{job.finished_at && (
							<div>
								<dt className="text-dim">Finished</dt>
								<dd className="mono">{job.finished_at}</dd>
							</div>
						)}
						{isBatch && (
							<>
								<div>
									<dt className="text-dim">Batches done</dt>
									<dd className="mono">{job.batches_done ?? 0}</dd>
								</div>
								<div>
									<dt className="text-dim">Rows done</dt>
									<dd className="mono">{job.rows_done ?? 0}</dd>
								</div>
								<div>
									<dt className="text-dim">Batch cursor</dt>
									<dd className="mono">{job.batch_cursor}</dd>
								</div>
							</>
						)}
					</dl>
					{job.error && (
						<div className="mt-3 border border-error/40 bg-error/10 px-4 py-3 text-sm text-error">{job.error}</div>
					)}
				</div>
			)}
		</div>
	);
}
