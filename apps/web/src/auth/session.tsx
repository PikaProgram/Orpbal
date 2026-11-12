import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { ApiError, type Session } from "@/lib/api";
import { loadSession, logOutSession } from "./session-ops";

interface SessionState {
	// null = not loaded yet or no session.
	session: Session | null;
	loaded: boolean;
}

interface SessionContextValue extends SessionState {
	/** Present once the session is known (including the no-session outcome). */
	isAuthed: boolean;
	/** Revoke the server session, then re-fetch (401 → signed out). */
	logOut: () => Promise<void>;
	/** Re-fetch the session (e.g. after a successful login). */
	refresh: () => void;
	/** Publish a session that the caller has just fetched (login). */
	setSession: (session: Session | null) => void;
}

const SessionContext = createContext<SessionContextValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
	const [state, setState] = useState<SessionState>({ session: null, loaded: false });
	const [nonce, setNonce] = useState(0);
	// Bumped by setSession/logOut so an in-flight bootstrap cannot clobber
	// a session published after it started.
	const epochRef = useRef(0);

	useEffect(() => {
		const epoch = epochRef.current;
		let cancelled = false;
		const apply = (session: Session | null) => {
			if (!cancelled && epochRef.current === epoch) setState({ session, loaded: true });
		};
		loadSession()
			.then(apply)
			.catch((error: unknown) => {
				apply(null);
				if (!(error instanceof ApiError) || error.status !== 401) {
					console.error("session bootstrap failed", error);
				}
			});
		return () => {
			cancelled = true;
		};
	}, [nonce]);

	const value = useMemo<SessionContextValue>(
		() => ({
			...state,
			isAuthed: state.session !== null,
			logOut: () =>
				logOutSession(state.session?.csrf_token).then(() => {
					epochRef.current += 1;
					setNonce((n) => n + 1);
				}),
			refresh: () => {
				epochRef.current += 1;
				setNonce((n) => n + 1);
			},
			setSession: (session) => {
				epochRef.current += 1;
				setState({ session, loaded: true });
			},
		}),
		[state],
	);

	return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
	const value = useContext(SessionContext);
	if (value === null) throw new Error("useSession must be used inside <SessionProvider>");
	return value;
}
