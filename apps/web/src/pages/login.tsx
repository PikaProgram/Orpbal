import { useState, type SubmitEvent } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { useSession } from "@/auth/session";
import { ApiError, api, type Session } from "@/lib/api";

export function LoginPage() {
	const navigate = useNavigate();
	const location = useLocation();
	const { setSession } = useSession();
	const [username, setUsername] = useState("");
	const [password, setPassword] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const from = (location.state as { from?: string } | null)?.from;

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setBusy(true);
		setError(null);
		try {
			// The response sets the httpOnly session cookie; fetch the
			// session once and publish it so the guards let us through.
			await api("/auth/login", { method: "POST", body: { username, password } });
			const result = await api<Session>("/auth/session");
			setSession(result.data);
			navigate(from && from.startsWith("/") ? from : "/", { replace: true });
		} catch (err) {
			setError(err instanceof ApiError ? err.message : "Check your username and password.");
			setBusy(false);
		}
	};

	return (
		<div className="mx-auto max-w-md">
			<div className="panel">
				<div className="nameplate">Operator sign-in</div>
				<div className="p-6">
					<h1 className="mb-2 text-2xl font-bold tracking-tight">Sign in</h1>
					<p className="mb-6 text-sm text-dim">Use the account created by a Platform Admin.</p>

					{error && (
						<p className="mb-4 border-2 border-alert/60 bg-alert/10 px-3 py-2 text-sm text-alert">
							{error}
						</p>
					)}

					<form onSubmit={submit} className="space-y-4">
						<label className="block text-sm">
							<span className="mb-1 block text-dim">Username</span>
							<input
								name="username"
								autoComplete="username"
								required
								autoFocus
								className="field"
								value={username}
								onChange={(e) => setUsername(e.target.value)}
							/>
						</label>
						<label className="block text-sm">
							<span className="mb-1 block text-dim">Password</span>
							<input
								type="password"
								name="password"
								autoComplete="current-password"
								required
								className="field"
								value={password}
								onChange={(e) => setPassword(e.target.value)}
							/>
						</label>
						<button type="submit" disabled={busy} className="btn-brass w-full px-4 py-2 text-sm">
							{busy ? "Signing in…" : "Sign in"}
						</button>
					</form>
				</div>
			</div>
		</div>
	);
}
