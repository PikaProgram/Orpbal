import { Navigate } from "react-router-dom";

import { useSession } from "@/auth/session";

export function RootRedirect() {
	const { session } = useSession();
	// RequireAuth guarantees a session here.
	if (session === null) return <Navigate to="/login" replace />;
	return <Navigate to={session.user.is_platform_admin ? "/admin/users" : "/databases"} replace />;
}
