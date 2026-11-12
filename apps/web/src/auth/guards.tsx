import { Navigate, Outlet, useLocation } from "react-router-dom";

import { useSession } from "@/auth/session";

function Spinner() {
	return (
		<div className="flex h-screen items-center justify-center text-sm text-dim">
			Loading…
		</div>
	);
}

/** Redirects to /login until the session is known; renders children when authed. */
export function RequireAuth() {
	const { session, loaded } = useSession();
	const location = useLocation();
	if (!loaded) return <Spinner />;
	if (session === null) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
	return <Outlet />;
}

/** Requires a platform admin; non-admins land on /databases. */
export function RequireAdmin() {
	const { session, loaded } = useSession();
	if (!loaded) return <Spinner />;
	if (session === null) return <Navigate to="/login" replace />;
	if (!session.user.is_platform_admin) return <Navigate to="/databases" replace />;
	return <Outlet />;
}

/** Only for unauthenticated visitors; already signed in → role-based home. */
export function GuestOnly() {
	const { session, loaded } = useSession();
	if (!loaded) return <Spinner />;
	if (session !== null) {
		return <Navigate to={session.user.is_platform_admin ? "/admin/users" : "/databases"} replace />;
	}
	return <Outlet />;
}
