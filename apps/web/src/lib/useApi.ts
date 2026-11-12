import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError } from "@/lib/api";

function message(error: unknown): string {
	return error instanceof ApiError ? error.message : "Request failed";
}

/** Loads data for a key; re-runs when the key changes or reload() is called. */
export function useLoad<T>(key: string, fetcher: () => Promise<T>) {
	const [state, setState] = useState<{ data: T | null; loading: boolean; error: string | null }>({
		data: null,
		loading: true,
		error: null,
	});
	const [tick, setTick] = useState(0);
	const fetcherRef = useRef(fetcher);
	fetcherRef.current = fetcher;

	useEffect(() => {
		if (key === "") return; // empty key = do not load
		let cancelled = false;
		setState((s) => ({ ...s, loading: true, error: null }));
		fetcherRef.current()
			.then((data) => {
				if (!cancelled) setState({ data, loading: false, error: null });
			})
			.catch((error: unknown) => {
				if (!cancelled) setState({ data: null, loading: false, error: message(error) });
			});
		return () => {
			cancelled = true;
		};
	}, [key, tick]);

	const reload = useCallback(() => setTick((t) => t + 1), []);
	return { ...state, reload };
}

export interface MutationState {
	busy: boolean;
	error: string | null;
	run: (fn: () => Promise<unknown>) => void;
}

/** Runs a mutating request; reports busy/error and reloads on success. */
export function useMutation(reload: () => void): MutationState {
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const run = useCallback(
		async (fn: () => Promise<unknown>) => {
			setBusy(true);
			setError(null);
			try {
				await fn();
				reload();
			} catch (err) {
				setError(message(err));
			} finally {
				setBusy(false);
			}
		},
		[reload],
	);

	return { run, busy, error };
}

/** A one-shot async action (e.g. login) with busy/error state. */
export function useAction() {
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const run = useCallback(async (fn: () => Promise<unknown>) => {
		setBusy(true);
		setError(null);
		try {
			await fn();
			return true;
		} catch (err) {
			setError(message(err));
			return false;
		} finally {
			setBusy(false);
		}
	}, []);
	return { run, busy, error, clearError: () => setError(null) };
}
